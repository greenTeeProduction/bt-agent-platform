package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
)

// selectorOrderingTree/seedSelectorStats/routerChildNames mirror the
// unexported helpers of the same name in internal/gardener/evolve_v2_test.go,
// duplicated here (package main cannot import unexported test helpers from
// another package) so this test can pin the same learned-ordering behavior
// through the production entry points in cmd/bt-gardener rather than the
// already-covered internal evolveTreeV2 call.
func selectorOrderingTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "Router",
				Children: []evolution.SerializableNode{
					{Type: "Sequence", Name: "Cheap"},
					{Type: "Sequence", Name: "Reliable"},
					{Type: "AlwaysSucceed", Name: "Fallback"},
				},
			},
		},
	}
}

func seedSelectorStats(t *testing.T, path string) {
	t.Helper()
	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	rec := func(child, outcome string, n int) {
		for i := 0; i < n; i++ {
			so.Record("Router", evolution.NodeExecutionRecord{NodeName: child, Outcome: outcome})
		}
	}
	rec("Cheap", "success", 2)
	rec("Cheap", "failure", 8) // 0.20 success rate
	rec("Reliable", "success", 9)
	rec("Reliable", "failure", 1)  // 0.90 success rate
	rec("Fallback", "success", 10) // 1.00 success rate — guard keeps it last anyway
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("SaveSelectorStats: %v", err)
	}
}

func routerChildNames(t *testing.T, tree *evolution.SerializableNode) []string {
	t.Helper()
	for i := range tree.Children {
		if tree.Children[i].Name == "Router" {
			names := make([]string, len(tree.Children[i].Children))
			for j, c := range tree.Children[i].Children {
				names[j] = c.Name
			}
			return names
		}
	}
	t.Fatalf("Router selector not found in tree")
	return nil
}

// TestWireSelectorOrdering_EnablesLearnedOrderingPass pins the production
// wiring gap: DefaultEvolveV2Config() leaves SelectorOrdering off and
// gardener.Config.SelectorStatsPath empty by design (opt-in — see
// internal/gardener/evolve_v2.go), and nothing in cmd/bt-gardener/main.go
// previously flipped either on, so the tested milestone-4 learned-ordering
// pass in evolveTreeV2 only ever ran inside evolve_v2_test.go. The production
// wiring helper must set both.
func TestWireSelectorOrdering_EnablesLearnedOrderingPass(t *testing.T) {
	metricsDir := t.TempDir()
	cfg := gardener.Config{}

	cfg, v2Cfg := wireSelectorOrdering(cfg, metricsDir)

	if !v2Cfg.SelectorOrdering {
		t.Error("v2Cfg.SelectorOrdering = false — production wiring must enable the milestone-4 learned-ordering pass")
	}
	if cfg.SelectorStatsPath == "" {
		t.Fatal("cfg.SelectorStatsPath is empty — production wiring must point at a durable telemetry file, or applyLearnedSelectorOrdering is a permanent no-op")
	}
	if dir := filepath.Dir(cfg.SelectorStatsPath); dir != metricsDir {
		t.Errorf("SelectorStatsPath = %q, want a file under metricsDir %q", cfg.SelectorStatsPath, metricsDir)
	}
}

// TestGardenerRollbackTool_CallRestoresSnapshot pins milestone 3/3 of the "Q2
// Evolvability" program: milestone 2's Registry.RollbackTree must be reachable
// through a langchain tool — GardenerRollbackTool, mirroring the
// GardenerStatusTool/GardenerRunCycleTool pattern (main.go:31-82) — instead of
// staying internal-only, so an operator or LLM agent can trigger rollback via
// gardener_rollback.
func TestGardenerRollbackTool_CallRestoresSnapshot(t *testing.T) {
	treeDir := t.TempDir()
	snapshotDir := t.TempDir()

	original := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "Original"}},
	}
	if _, err := evolution.SnapshotTree(original, "rollback_target", snapshotDir); err != nil {
		t.Fatalf("SnapshotTree failed: %v", err)
	}

	// Persist an already-mutated tree file under treeDir — simulating a bad
	// mutation that evolveTreeV2 already applied and saved — so the registry
	// loads with the mutated state, not the pre-mutation snapshot.
	mutated := &evolution.SerializableNode{Type: "Action", Name: "Mutated"}
	data, err := json.MarshalIndent(mutated, "", "  ")
	if err != nil {
		t.Fatalf("marshal mutated tree: %v", err)
	}
	treeFile := filepath.Join(treeDir, "tree-rollback_target.json")
	if err := os.WriteFile(treeFile, data, 0644); err != nil {
		t.Fatalf("write mutated tree file: %v", err)
	}

	registry := gardener.NewRegistry(treeDir)

	tool := &GardenerRollbackTool{registry: registry, snapshotDir: snapshotDir}
	if got, want := tool.Name(), "gardener_rollback"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}

	if _, err := tool.Call(context.Background(), "rollback_target"); err != nil {
		t.Fatalf("tool.Call: %v", err)
	}

	var restored gardener.TreeEntry
	for _, e := range registry.List() {
		if e.Name == "rollback_target" {
			restored = e
		}
	}
	if restored.Tree == nil || restored.Tree.Name != "Root" || len(restored.Tree.Children) != 1 || restored.Tree.Children[0].Name != "Original" {
		t.Fatalf("gardener_rollback tool did not restore the in-memory tree, got %+v", restored.Tree)
	}

	onDiskData, err := os.ReadFile(treeFile)
	if err != nil {
		t.Fatalf("reading rolled-back tree file: %v", err)
	}
	var onDisk evolution.SerializableNode
	if err := json.Unmarshal(onDiskData, &onDisk); err != nil {
		t.Fatalf("unmarshal rolled-back tree: %v", err)
	}
	if onDisk.Name != "Root" || len(onDisk.Children) != 1 || onDisk.Children[0].Name != "Original" {
		t.Errorf("gardener_rollback tool did not durably persist the restored tree, got %+v", onDisk)
	}
}

// TestGardenerDeactivateAllTool_CallDeactivatesAllTrees pins the gap:
// Registry.DeactivateAll (internal/gardener/gardener.go) is a fully tested
// evolution kill switch with no way to trigger it short of restarting the
// daemon. Mirroring the GardenerRollbackTool pattern (main.go), a
// GardenerDeactivateAllTool must expose it to the langchain agent under the
// name "gardener_deactivate_all", reporting how many trees were active
// before the call, and leaving every registry entry inactive afterward.
func TestGardenerDeactivateAllTool_CallDeactivatesAllTrees(t *testing.T) {
	treeDir := t.TempDir()
	registry := gardener.NewRegistry(treeDir)

	activeBefore := 0
	for _, e := range registry.List() {
		if e.Active {
			activeBefore++
		}
	}
	if activeBefore == 0 {
		t.Fatal("precondition: registry must have at least one active tree")
	}

	tool := &GardenerDeactivateAllTool{registry: registry}
	if got, want := tool.Name(), "gardener_deactivate_all"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}

	out, err := tool.Call(context.Background(), "")
	if err != nil {
		t.Fatalf("tool.Call: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("tool.Call output is not valid JSON: %v (%s)", err, out)
	}
	if got, ok := resp["deactivated"].(float64); !ok || int(got) != activeBefore {
		t.Errorf("response deactivated = %v, want %d", resp["deactivated"], activeBefore)
	}

	for _, e := range registry.List() {
		if e.Active {
			t.Errorf("tree %q still active after gardener_deactivate_all", e.Name)
		}
	}
}

// TestGardenerRunCycleTool_CallAppliesLearnedSelectorOrdering pins the second
// half of the same gap: even once the daemon's timer-driven cycle uses a
// SelectorOrdering-enabled EvolveV2Config, the langchain gardener_run_cycle
// tool built its own gardener.DefaultEvolveV2Config() fresh on every call
// (the original GardenerRunCycleTool.Call), silently disabling the pass for
// every MCP-triggered cycle. The tool must reuse the daemon's wired config
// instead of constructing a disabled default.
func TestGardenerRunCycleTool_CallAppliesLearnedSelectorOrdering(t *testing.T) {
	treeDir := t.TempDir()
	tree := selectorOrderingTree()
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(treeDir, "tree-selector_tree.json"), data, 0644); err != nil {
		t.Fatalf("write tree file: %v", err)
	}

	refDir := t.TempDir()
	metricsDir := t.TempDir()
	snapDir := t.TempDir()
	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig: %v", err)
	}
	cfg.Registry = gardener.NewRegistry(treeDir)
	cfg.MaxMutations = 0 // isolate the reorder — no structural mutations this cycle
	cfg.EvolveWithoutReflections = true

	cfg, v2Cfg := wireSelectorOrdering(cfg, metricsDir)
	seedSelectorStats(t, cfg.SelectorStatsPath)

	g := gardener.NewGardener(cfg)
	tool := newGardenerRunCycleTool(g, v2Cfg)

	if _, err := tool.Call(context.Background(), ""); err != nil {
		t.Fatalf("tool.Call: %v", err)
	}

	var got *evolution.SerializableNode
	for _, e := range cfg.Registry.List() {
		if e.Name == "selector_tree" {
			got = e.Tree
		}
	}
	if got == nil {
		t.Fatal("selector_tree not found in registry after cycle")
	}

	names := routerChildNames(t, got)
	want := []string{"Reliable", "Cheap", "Fallback"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("gardener_run_cycle tool did not apply learned Selector ordering: Router children = %v, want %v", names, want)
	}
}

// TestGardenerRunCycleTool_CallAppliesLearnedSelectorOrdering above pins the
// mechanism by seeding stats directly at cfg.SelectorStatsPath — the exact
// single global file wireSelectorOrdering points at
// (metricsDir/selector-stats.json). In production, no writer in the repo ever
// produces that file: the real telemetry writer, RunDeps.flushSelectorTelemetry
// (internal/agent/selector_flush.go), only ever writes PER-TREE files via
// agent.SelectorStatsFile(treeID) under agent.HomeDir()/selector-stats/ — a
// different directory tree entirely (metricsDir is ~/.go-bt-gardener,
// agent.HomeDir() is ~/.go-bt-evolve). So applyLearnedSelectorOrdering's single
// shared g.cfg.SelectorStatsPath can never resolve to a file the real writer
// populates, and the pass is a permanent no-op outside tests that hand-seed
// the global path. This test seeds telemetry only at the real per-tree
// location (mirroring the actual production writer) and leaves
// cfg.SelectorStatsPath unseeded, pinning the requirement that the gardener
// read real per-tree telemetry instead.
func TestGardenerRunCycleTool_CallAppliesLearnedSelectorOrdering_PerTreeTelemetryFile(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	treeDir := t.TempDir()
	tree := selectorOrderingTree()
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(treeDir, "tree-selector_tree.json"), data, 0644); err != nil {
		t.Fatalf("write tree file: %v", err)
	}

	refDir := t.TempDir()
	metricsDir := t.TempDir()
	snapDir := t.TempDir()
	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig: %v", err)
	}
	cfg.Registry = gardener.NewRegistry(treeDir)
	cfg.MaxMutations = 0 // isolate the reorder — no structural mutations this cycle
	cfg.EvolveWithoutReflections = true

	cfg, v2Cfg := wireSelectorOrdering(cfg, metricsDir)

	// Seed telemetry only where the real production writer
	// (agent.RunDeps.flushSelectorTelemetry) actually puts it — a per-tree file
	// under agent.HomeDir()/selector-stats/, resolved by agent.SelectorStatsFile.
	// cfg.SelectorStatsPath (the global metricsDir file) is deliberately left
	// unseeded, since no writer in the repo ever produces it.
	perTreePath := agent.SelectorStatsFile("selector_tree")
	if err := os.MkdirAll(filepath.Dir(perTreePath), 0o755); err != nil {
		t.Fatalf("mkdir per-tree stats dir: %v", err)
	}
	seedSelectorStats(t, perTreePath)

	g := gardener.NewGardener(cfg)
	tool := newGardenerRunCycleTool(g, v2Cfg)

	if _, err := tool.Call(context.Background(), ""); err != nil {
		t.Fatalf("tool.Call: %v", err)
	}

	var got *evolution.SerializableNode
	for _, e := range cfg.Registry.List() {
		if e.Name == "selector_tree" {
			got = e.Tree
		}
	}
	if got == nil {
		t.Fatal("selector_tree not found in registry after cycle")
	}

	names := routerChildNames(t, got)
	want := []string{"Reliable", "Cheap", "Fallback"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("gardener_run_cycle tool did not apply learned Selector ordering from the real per-tree telemetry file %q: Router children = %v, want %v", perTreePath, names, want)
	}
}

// TestMainSupportsVersionFlagForDriftSmokeTest pins the --version fast path
// the deploy-drift restart handoff smoke-tests rebuilt binaries with
// (internal/agent/deploy_drift.go runs `<binary> --version` and rolls the
// swap back on failure): without it, arming BT_AUTO_RESTART_ON_DRIFT on this
// unit would roll back every rebuild and the daemon could never self-adopt a
// new revision.
func TestMainSupportsVersionFlagForDriftSmokeTest(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(src), "if versionRequested()") {
		t.Error("main.go must short-circuit on --version (versionRequested) so the deploy-drift smoke test can validate a rebuilt binary")
	}
}
