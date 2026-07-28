package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

func TestRegistry_Count(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRegistry(tempDir)
	if r.Count() <= 0 {
		t.Errorf("expected Count() > 0, got %d", r.Count())
	}
}

func TestRegistry_List_AllDomains(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRegistry(tempDir)
	entries := r.List()

	nameMap := make(map[string]bool)
	for _, e := range entries {
		nameMap[e.Name] = true
	}

	expected := []string{"default", "godev", "domain_code_review", "finance_pitch_agent", "research_deep_research"}
	for _, name := range expected {
		if !nameMap[name] {
			t.Errorf("expected entry %q in registry, but not found", name)
		}
	}
}

func TestRegistry_SaveAndReload(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRegistry(tempDir)
	entries := r.List()
	if len(entries) == 0 {
		t.Fatal("registry has no entries")
	}

	entry := entries[0]
	err := r.SaveTree(entry)
	if err != nil {
		t.Fatalf("SaveTree failed: %v", err)
	}

	if _, err := os.Stat(entry.FilePath); os.IsNotExist(err) {
		t.Errorf("expected file to exist at %s after SaveTree", entry.FilePath)
	}
}

// TestRegistry_SaveTree_WriteFailureLeavesOriginalUntouched forces the
// os.WriteFile call in SaveTree to fail while a stale tmp file is already on
// disk (e.g. left over from a prior crashed write). A naive implementation
// that ignores the WriteFile error still calls os.Rename, which succeeds
// unconditionally (rename permission is governed by the *directory*, not the
// file's own mode) and silently clobbers entry.FilePath with the stale tmp
// content. SaveTree must check the WriteFile error first and refuse to
// rename, leaving entry.FilePath untouched and reporting a non-nil error.
func TestRegistry_SaveTree_WriteFailureLeavesOriginalUntouched(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file write permission cannot be revoked to force this failure")
	}

	tempDir := t.TempDir()
	r := NewRegistry(tempDir)

	filePath := filepath.Join(tempDir, "tree.json")
	original := []byte(`{"original":"content"}`)
	if err := os.WriteFile(filePath, original, 0644); err != nil {
		t.Fatalf("seed WriteFile: %v", err)
	}

	// Pre-create the tmp file SaveTree will target, then revoke its write
	// permission so os.WriteFile(tmp, ...) inside SaveTree fails while the
	// stale tmp file remains on disk.
	tmpPath := filePath + ".tmp"
	stale := []byte("stale-leftover-data")
	if err := os.WriteFile(tmpPath, stale, 0644); err != nil {
		t.Fatalf("seed tmp WriteFile: %v", err)
	}
	if err := os.Chmod(tmpPath, 0444); err != nil {
		t.Fatalf("Chmod tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpPath, 0644) })

	entry := TreeEntry{
		Name:     "write-failure-test",
		Tree:     &evolution.SerializableNode{Type: "selector", Name: "root"},
		FilePath: filePath,
	}

	err := r.SaveTree(entry)
	if err == nil {
		t.Fatal("expected SaveTree to return a non-nil error when the write fails, got nil")
	}

	data, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatalf("original file unreadable after failed SaveTree: %v", readErr)
	}
	if string(data) != string(original) {
		t.Errorf("original file content changed after failed SaveTree: got %q, want %q", data, original)
	}

	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Errorf("expected stale tmp file to be removed after failed write, stat err = %v", statErr)
	}
}

func TestMetricsTracker_RecordAndSummary(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1})

	summary := mt.Summary()
	totalCycles, ok := summary["total_cycles"].(int)
	if !ok {
		t.Fatalf("total_cycles not found or wrong type in summary: %v", summary)
	}
	if totalCycles != 2 {
		t.Errorf("expected total_cycles == 2, got %d", totalCycles)
	}
}

func TestMetricsTracker_CyclesForTree(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1})
	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 2})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1})

	if got := mt.CyclesForTree("tree_a"); got != 2 {
		t.Errorf("CyclesForTree(tree_a) = %d, want 2", got)
	}
	if got := mt.CyclesForTree("tree_b"); got != 1 {
		t.Errorf("CyclesForTree(tree_b) = %d, want 1", got)
	}
}

// TestMetricsTracker_SaveAggregatesCrisisAndLastRun verifies milestone 2/5 of
// the "evolution self-healing observable end-to-end" program: Save must write
// gardener-metrics.json as an aggregate document that stamps a real last_run
// unix timestamp and totals the crisis interventions recorded in the
// CycleMetrics history, while the full history stays persisted and reloadable.
func TestMetricsTracker_SaveAggregatesCrisisAndLastRun(t *testing.T) {
	dir := t.TempDir()
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1, CrisisIntervention: true})
	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 2})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1, CrisisIntervention: true})

	if err := mt.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gardener-metrics.json"))
	if err != nil {
		t.Fatalf("reading gardener-metrics.json: %v", err)
	}

	var doc struct {
		LastRun                  int64          `json:"last_run"`
		TotalCrisisInterventions int            `json:"total_crisis_interventions"`
		History                  []CycleMetrics `json:"history"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("gardener-metrics.json must be an aggregate object, not a bare history array: %v", err)
	}
	if doc.LastRun == 0 {
		t.Error("last_run must be stamped with a non-zero unix timestamp")
	}
	if doc.TotalCrisisInterventions != 2 {
		t.Errorf("total_crisis_interventions = %d, want 2 (one per recorded crisis cycle)", doc.TotalCrisisInterventions)
	}
	if len(doc.History) != 3 {
		t.Errorf("history in gardener-metrics.json has %d cycles, want all 3 recorded", len(doc.History))
	}

	// The aggregate format must stay loadable: a fresh tracker on the same
	// directory has to rehydrate the recorded history.
	reloaded, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker (reload) failed: %v", err)
	}
	if got := reloaded.CyclesForTree("tree_a"); got != 2 {
		t.Errorf("reloaded CyclesForTree(tree_a) = %d, want 2", got)
	}
}

// TestMetricsTracker_SaveIncludesDashboardAggregateFields verifies milestone
// 5/5 of the "evolution self-healing observable end-to-end" program:
// gardener-metrics.json must carry the same total_cycles/active_trees/
// best_fitness/total_improvements aggregates that Summary() already computes
// in-memory, because dashboard.loadGardenerMetrics parses exactly those keys
// and unconditionally returns nil (blank panel) when total_cycles is absent
// or zero.
func TestMetricsTracker_SaveIncludesDashboardAggregateFields(t *testing.T) {
	dir := t.TempDir()
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1, NewFitness: 0.5, Improved: true, Delta: 0.5})
	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 2, NewFitness: 0.8, Improved: true, Delta: 0.3})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1, NewFitness: 0.2})

	if err := mt.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gardener-metrics.json"))
	if err != nil {
		t.Fatalf("reading gardener-metrics.json: %v", err)
	}

	var doc struct {
		TotalCycles       int     `json:"total_cycles"`
		ActiveTrees       int     `json:"active_trees"`
		BestFitness       float64 `json:"best_fitness"`
		TotalImprovements int     `json:"total_improvements"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("gardener-metrics.json must decode: %v", err)
	}

	if doc.TotalCycles != 3 {
		t.Errorf("total_cycles = %d, want 3 (dashboard.loadGardenerMetrics treats 0 as absent and returns nil)", doc.TotalCycles)
	}
	if doc.ActiveTrees != 2 {
		t.Errorf("active_trees = %d, want 2 (tree_a, tree_b)", doc.ActiveTrees)
	}
	if doc.BestFitness != 0.8 {
		t.Errorf("best_fitness = %v, want 0.8 (max NewFitness across all recorded cycles)", doc.BestFitness)
	}
	if doc.TotalImprovements != 2 {
		t.Errorf("total_improvements = %d, want 2 (cycles with Improved=true)", doc.TotalImprovements)
	}
}

// TestMetricsTracker_SaveAggregatesDeepSearchCoverage verifies milestone 3/3
// of the "Q2 Evolvability — Harden and activate the gardener's Stockfish-
// style deep-search apply path in production" program: gardener-metrics.json
// must expose deep-search activity (total_deep_search_cycles,
// avg_tt_hit_rate) aggregated from the per-cycle DeepSearchUsed/TTHitRate
// fields, so a dashboard consumer can read coverage without replaying the
// full history array.
func TestMetricsTracker_SaveAggregatesDeepSearchCoverage(t *testing.T) {
	dir := t.TempDir()
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1, DeepSearchUsed: true, DeepSearchDepth: 4, TTHitRate: 0.8})
	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 2, DeepSearchUsed: true, DeepSearchDepth: 3, TTHitRate: 0.4})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1})

	if err := mt.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gardener-metrics.json"))
	if err != nil {
		t.Fatalf("reading gardener-metrics.json: %v", err)
	}

	var doc struct {
		TotalDeepSearchCycles int     `json:"total_deep_search_cycles"`
		AvgTTHitRate          float64 `json:"avg_tt_hit_rate"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("gardener-metrics.json must decode: %v", err)
	}

	if doc.TotalDeepSearchCycles != 2 {
		t.Errorf("total_deep_search_cycles = %d, want 2 (cycles with DeepSearchUsed=true)", doc.TotalDeepSearchCycles)
	}
	if doc.AvgTTHitRate != 0.6 {
		t.Errorf("avg_tt_hit_rate = %v, want 0.6 (mean TTHitRate over deep-search cycles only)", doc.AvgTTHitRate)
	}
}

// TestMetricsTracker_SaveAggregatesRollbacks verifies milestone 3/3 of the
// "Q2 Evolvability — Make gardener mutation rollback automatic,
// multi-revision, and observable" program: gardener-metrics.json must expose
// total_rollbacks aggregated from the per-cycle CycleMetrics.Rollbacks field,
// which Save silently drops today, so a dashboard consumer can see rollback
// activity without replaying the full history array.
func TestMetricsTracker_SaveAggregatesRollbacks(t *testing.T) {
	dir := t.TempDir()
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1, Rollbacks: 2})
	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 2, Rollbacks: 1})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1})

	if err := mt.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "gardener-metrics.json"))
	if err != nil {
		t.Fatalf("reading gardener-metrics.json: %v", err)
	}

	var doc struct {
		TotalRollbacks int `json:"total_rollbacks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("gardener-metrics.json must decode: %v", err)
	}

	if doc.TotalRollbacks != 3 {
		t.Errorf("total_rollbacks = %d, want 3 (sum of CycleMetrics.Rollbacks across all recorded cycles)", doc.TotalRollbacks)
	}
}

// TestMetricsTracker_SaveWriteFailureLeavesOriginalUntouched verifies
// milestone 2/4 of the "Q3 Reliability — Stop silent write-failure and
// breaker-bypass gaps in gardener persistence, dashboard circuit-breaker
// gating, and A2A history recording" program: like SaveTree, Save must not
// discard the os.WriteFile error via `_ =`. This test forces the WriteFile
// call to fail while a stale tmp file is already on disk (e.g. left over
// from a prior crashed write). A naive implementation that ignores the
// WriteFile error still calls os.Rename, which succeeds unconditionally
// (rename permission is governed by the *directory*, not the file's own
// mode) and silently clobbers gardener-metrics.json with the stale tmp
// content. Save must check the WriteFile error first and refuse to rename,
// leaving the prior metrics file untouched and reporting a non-nil error.
func TestMetricsTracker_SaveWriteFailureLeavesOriginalUntouched(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file write permission cannot be revoked to force this failure")
	}

	dir := t.TempDir()
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker failed: %v", err)
	}
	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1})

	filePath := filepath.Join(dir, "gardener-metrics.json")
	original := []byte(`{"original":"content"}`)
	if err := os.WriteFile(filePath, original, 0644); err != nil {
		t.Fatalf("seed WriteFile: %v", err)
	}

	// Pre-create the tmp file Save will target, then revoke its write
	// permission so os.WriteFile(tmp, ...) inside Save fails while the
	// stale tmp file remains on disk.
	tmpPath := filePath + ".tmp"
	stale := []byte("stale-leftover-data")
	if err := os.WriteFile(tmpPath, stale, 0644); err != nil {
		t.Fatalf("seed tmp WriteFile: %v", err)
	}
	if err := os.Chmod(tmpPath, 0444); err != nil {
		t.Fatalf("Chmod tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpPath, 0644) })

	err = mt.Save()
	if err == nil {
		t.Fatal("expected Save to return a non-nil error when the write fails, got nil")
	}

	data, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatalf("original metrics file unreadable after failed Save: %v", readErr)
	}
	if string(data) != string(original) {
		t.Errorf("original metrics file content changed after failed Save: got %q, want %q", data, original)
	}
}

// TestRegistry_RollbackTree verifies milestone 2/3 of the "Q2 Evolvability"
// program: RollbackTree must restore a tree from its milestone-1 pre-mutation
// snapshot (evolution.RestoreTree) and durably persist the restored state via
// SaveTree, so a bad mutation can be recovered without rerunning a full
// evolution cycle.
func TestRegistry_RollbackTree(t *testing.T) {
	tempDir := t.TempDir()
	snapshotDir := t.TempDir()
	r := NewRegistry(tempDir)

	original := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "Original"}},
	}
	r.addBuiltin("rollback_target", "test tree", original)
	if _, err := evolution.SnapshotTree(original, "rollback_target", snapshotDir); err != nil {
		t.Fatalf("SnapshotTree failed: %v", err)
	}

	// Simulate a bad mutation: overwrite the in-memory entry and persist it,
	// exactly like evolveTreeV2 does when it applies and saves a mutation.
	mutated := &evolution.SerializableNode{Type: "Action", Name: "Mutated"}
	var mutatedEntry TreeEntry
	for i := range r.entries {
		if r.entries[i].Name == "rollback_target" {
			r.entries[i].Tree = mutated
			mutatedEntry = r.entries[i]
		}
	}
	if err := r.SaveTree(mutatedEntry); err != nil {
		t.Fatalf("SaveTree(mutated) failed: %v", err)
	}

	if err := r.RollbackTree("rollback_target", snapshotDir); err != nil {
		t.Fatalf("RollbackTree failed: %v", err)
	}

	// The registry's in-memory entry must reflect the restored tree.
	var restored TreeEntry
	for _, e := range r.List() {
		if e.Name == "rollback_target" {
			restored = e
		}
	}
	if restored.Tree == nil || restored.Tree.Name != "Root" || len(restored.Tree.Children) != 1 || restored.Tree.Children[0].Name != "Original" {
		t.Errorf("RollbackTree did not restore the in-memory tree, got %+v", restored.Tree)
	}

	// The on-disk file must durably reflect the restored tree too, not the
	// mutated one, so a process crash right after RollbackTree still recovers.
	data, err := os.ReadFile(mutatedEntry.FilePath)
	if err != nil {
		t.Fatalf("reading rolled-back tree file: %v", err)
	}
	var onDisk evolution.SerializableNode
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal rolled-back tree: %v", err)
	}
	if onDisk.Name != "Root" || len(onDisk.Children) != 1 || onDisk.Children[0].Name != "Original" {
		t.Errorf("RollbackTree did not persist the pre-mutation snapshot to disk, got %+v", onDisk)
	}
}

// TestRegistry_RollbackTree_UnknownTree verifies RollbackTree returns an
// error instead of silently succeeding when no entry matches name.
func TestRegistry_RollbackTree_UnknownTree(t *testing.T) {
	tempDir := t.TempDir()
	snapshotDir := t.TempDir()
	r := NewRegistry(tempDir)

	if err := r.RollbackTree("does_not_exist", snapshotDir); err == nil {
		t.Error("RollbackTree should return an error for an unregistered tree name")
	}
}

func TestEvolveTreeSkipsWhenNoReflectionEvidence(t *testing.T) {
	dir := t.TempDir()
	store, err := evolution.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(dir)
	reg.addBuiltin("lonely_tree", "no reflections", &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "DoThing"}},
	})
	mt, err := NewMetricsTracker(filepath.Join(dir, "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	g := NewGardener(Config{Registry: reg, RefStore: store, MetricsTracker: mt, MaxMutations: 3})

	var entry TreeEntry
	for _, e := range reg.List() {
		if e.Name == "lonely_tree" {
			entry = e
		}
	}
	metrics := g.evolveTreeV2(entry, DefaultEvolveV2Config())
	if !metrics.SkippedNoEvidence {
		t.Fatalf("a tree with no reflection records must be skipped by the evidence gate: %+v", metrics)
	}
	if metrics.Mutations != 0 || metrics.Improved {
		t.Fatalf("skipped tree must apply no mutations: %+v", metrics)
	}
}

// ============================================================================
// Deep-search (IterativeDeepening) metrics — Q2 Evolvability milestone 2/3
// ============================================================================

// deepSearchGardener builds a Gardener + single-tree entry for the deep-search
// metrics tests below. ttPath == "" leaves Config.TranspositionTablePath
// unset, the same knob milestone 1 introduced for TT persistence and that
// evaluator.IterativeDeepening requires a non-nil table to run at all.
func deepSearchGardener(t *testing.T, treeName, ttPath string) (*Gardener, TreeEntry) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}
	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: treeName, Description: "deep search metrics", Tree: tree, FilePath: dir + "/tree-" + treeName + ".json", Active: true},
	}
	reg.mu.Unlock()

	g := NewGardener(Config{
		Registry:                 reg,
		MetricsTracker:           mt,
		RefStore:                 refStore,
		MaxMutations:             1,
		EvolveWithoutReflections: true,
		TranspositionTablePath:   ttPath,
	})
	return g, reg.List()[0]
}

// deepSearchV2Config mirrors crisisV2Config: a zero QuickThreshold so the
// structural cascade never blocks the tiny test tree from reaching the
// deep-search path.
func deepSearchV2Config() EvolveV2Config {
	return EvolveV2Config{CascadeCfg: evaluator.CascadeConfig{QuickThreshold: 0}}
}

// TestEvolveTreeV2_DeepSearchMetrics_PopulatedWhenTTConfigured pins Q2
// Evolvability milestone 2/3 ("Wire the Stockfish transposition-table search
// into the gardener's live evolution loop"): whenever evolveTreeV2 actually
// exercises evaluator.IterativeDeepening — gated on a configured
// TranspositionTablePath, since IterativeDeepening requires a non-nil
// *evaluator.TranspositionTable — the returned CycleMetrics must report
// DeepSearchUsed=true with DeepSearchDepth and TTHitRate derived from the
// evaluator.DeepeningResult the search produced.
func TestEvolveTreeV2_DeepSearchMetrics_PopulatedWhenTTConfigured(t *testing.T) {
	dir := t.TempDir()
	g, entry := deepSearchGardener(t, "deep_search_tree", filepath.Join(dir, "tt"))

	metrics := g.evolveTreeV2(entry, deepSearchV2Config())

	if !metrics.DeepSearchUsed {
		t.Fatalf("expected DeepSearchUsed == true when TranspositionTablePath is configured, got %+v", metrics)
	}
	if metrics.DeepSearchDepth <= 0 {
		t.Errorf("expected DeepSearchDepth > 0, got %d", metrics.DeepSearchDepth)
	}
	if metrics.TTHitRate < 0 || metrics.TTHitRate > 1 {
		t.Errorf("expected TTHitRate in [0,1], got %v", metrics.TTHitRate)
	}
}

// TestEvolveTreeV2_DeepSearchMetrics_ZeroWithoutTranspositionTable pins the
// counterpart this milestone also requires: a cycle that never exercises
// IterativeDeepening (no TranspositionTablePath configured) must leave
// DeepSearchUsed/DeepSearchDepth/TTHitRate at their zero values, not just
// "unset but nonzero from a stale run".
func TestEvolveTreeV2_DeepSearchMetrics_ZeroWithoutTranspositionTable(t *testing.T) {
	g, entry := deepSearchGardener(t, "no_tt_tree", "")

	metrics := g.evolveTreeV2(entry, deepSearchV2Config())

	if metrics.DeepSearchUsed {
		t.Errorf("expected DeepSearchUsed == false without a configured TranspositionTablePath, got true (metrics=%+v)", metrics)
	}
	if metrics.DeepSearchDepth != 0 {
		t.Errorf("expected DeepSearchDepth == 0, got %d", metrics.DeepSearchDepth)
	}
	if metrics.TTHitRate != 0 {
		t.Errorf("expected TTHitRate == 0, got %v", metrics.TTHitRate)
	}
}

// TestRunCycleV2_PrioritizesByKGAnalytics pins the gap identified in the
// 2026-07-17 structural review: RunCycleV2 round-robins its registry entries
// in flat alphabetical order and is completely blind to the knowledge graph's
// ComputeAnalytics() output (Bottlenecks/SelectionPressure). A tree the graph
// flags as a bottleneck (low success rate, enough runs to be trusted) should
// be evolved before healthy, well-run trees that merely sort earlier
// alphabetically — otherwise the daemon spends its limited per-cycle budget
// on trees that don't need attention while a known-broken tree waits behind
// them purely because of its name.
func TestRunCycleV2_PrioritizesByKGAnalytics(t *testing.T) {
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	mkTree := func(name string) *evolution.SerializableNode {
		return &evolution.SerializableNode{
			Type: "Sequence", Name: name,
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "Step"},
			},
		}
	}

	// "alpha_tree" and "beta_tree" sort well before "zzz_bottleneck" under a
	// flat alphabetical ordering, but the KG below marks zzz_bottleneck as a
	// bottleneck and the other two as healthy — it must run first.
	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: "alpha_tree", Description: "healthy", Tree: mkTree("alpha_tree"), FilePath: dir + "/tree-alpha_tree.json", Active: true},
		{Name: "beta_tree", Description: "healthy", Tree: mkTree("beta_tree"), FilePath: dir + "/tree-beta_tree.json", Active: true},
		{Name: "zzz_bottleneck", Description: "broken", Tree: mkTree("zzz_bottleneck"), FilePath: dir + "/tree-zzz_bottleneck.json", Active: true},
	}
	reg.mu.Unlock()

	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "alpha_tree", Name: "alpha_tree", Category: "domain", Fitness: 95, RunCount: 20})
	kg.Register(&knowledge.TreeMeta{ID: "beta_tree", Name: "beta_tree", Category: "domain", Fitness: 90, RunCount: 20})
	// Bottleneck criteria (see knowledge.ComputeAnalytics): RunCount >= 3 and
	// Fitness < 30.
	kg.Register(&knowledge.TreeMeta{ID: "zzz_bottleneck", Name: "zzz_bottleneck", Category: "domain", Fitness: 10, RunCount: 8})

	g := NewGardener(Config{
		Registry:       reg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
		KnowledgeGraph: kg,
	})

	results, err := g.RunCycleV2(DefaultEvolveV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if results[0].TreeName != "zzz_bottleneck" {
		names := make([]string, len(results))
		for i, r := range results {
			names[i] = r.TreeName
		}
		t.Errorf("expected KG-flagged bottleneck tree to be evolved first, got order %v", names)
	}
}

// TestRegistry_Rescan_PicksUpTreeAddedAfterConstruction verifies milestone
// 3/4 of the "Q4 Personalization & Self-Growth — Close the HITL-adoption and
// live-rescan gaps in the self-generated GOAP tree lifecycle" program:
// NewRegistryWithUsers only scans usersRoot once, at construction time. A
// personal tree the autopilot compiler writes into a user's workspace after
// HITL approval — while the gardener daemon is already running — is
// therefore invisible to evolution until the process restarts. Rescan() must
// re-invoke loadUserTreesLocked so a tree written post-construction becomes
// visible without a restart.
func TestRegistry_Rescan_PicksUpTreeAddedAfterConstruction(t *testing.T) {
	storageDir := t.TempDir()
	usersRoot := t.TempDir()

	r := NewRegistryWithUsers(storageDir, usersRoot)

	for _, e := range r.List() {
		if e.User == "nico" {
			t.Fatalf("tree unexpectedly present before it was ever written: %+v", e)
		}
	}

	// Simulate autopilot compiling + HITL-approving a new personal tree while
	// the daemon is already running.
	writeUserTree(t, usersRoot, "nico", "goal:automate_reports")

	for _, e := range r.List() {
		if e.User == "nico" && e.Name == "goal:automate_reports" {
			t.Fatal("tree written after construction must not be visible before Rescan() is called")
		}
	}

	r.Rescan()

	found := false
	for _, e := range r.List() {
		if e.User == "nico" && e.Name == "goal:automate_reports" {
			found = true
		}
	}
	if !found {
		t.Error("Rescan() did not pick up the personal tree added to usersRoot after construction")
	}
}

// CollectAgentSLOs must read persisted cross-process evidence
// (engine.LoadSLOEvidence), not the in-process-only engine.AllSLOMetrics()
// sync.Map — the gardener process never executes trees, so that registry is
// always empty there, exactly like ValidationGate's file-fallback (see
// validation_gate.go). Without this, gardener.CollectAgentSLOs()
// unconditionally returns nil in production and the dashboard's
// GardenerMetrics.SLOs field is permanently empty.
func TestCollectAgentSLOs_ReadsPersistedFileEvidence(t *testing.T) {
	path := writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "domain_goap_fusion", TreeName: "domain_goap_fusion", TotalCalls: 10, SuccessfulCalls: 8, FailedCalls: 2, RecoveredCalls: 1, TotalLatencyMs: 500},
	})

	got := CollectAgentSLOs(path)

	key := "domain_goap_fusion:domain_goap_fusion"
	if got == nil {
		t.Fatalf("CollectAgentSLOs(%q) = nil; want metrics read from file evidence (the gardener process never populates the in-process SLO registry)", path)
	}
	if sr, ok := got[key+"/success_rate"]; !ok || sr != 0.8 {
		t.Errorf("%s/success_rate = %v (ok=%v), want 0.8", key, sr, ok)
	}
	if rr, ok := got[key+"/recovery_rate"]; !ok || rr != 0.5 {
		t.Errorf("%s/recovery_rate = %v (ok=%v), want 0.5", key, rr, ok)
	}
	if lat, ok := got[key+"/avg_latency"]; !ok || lat != 50 {
		t.Errorf("%s/avg_latency = %v (ok=%v), want 50", key, lat, ok)
	}
}

func TestCollectAgentSLOs_NoMemoryNoFile_ReturnsNil(t *testing.T) {
	got := CollectAgentSLOs(filepath.Join(t.TempDir(), "missing-evidence.json"))
	if got != nil {
		t.Errorf("CollectAgentSLOs with no in-process metrics and no evidence file = %v, want nil", got)
	}
}

// RunCycleV2 must thread its ValidationGateConfig.EvidencePath — already
// wired from cmd/bt-gardener/main.go into g.cfg.ValidationGate.EvidencePath
// for ValidationGate's own file fallback — through to CollectAgentSLOs so
// the exported slo-metrics.json the dashboard reads is populated from
// cross-process evidence instead of the always-empty in-process registry.
func TestRunCycleV2_SLOExport_ReadsFileEvidence(t *testing.T) {
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	mkTree := func(name string) *evolution.SerializableNode {
		return &evolution.SerializableNode{
			Type: "Sequence", Name: name,
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "Step"},
			},
		}
	}

	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: "slo_export_tree", Description: "test", Tree: mkTree("slo_export_tree"), FilePath: dir + "/tree-slo_export_tree.json", Active: true},
	}
	reg.mu.Unlock()

	evidencePath := writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "agent-x", TreeName: "slo_export_tree", TotalCalls: 4, SuccessfulCalls: 4},
	})

	validationGate := DefaultValidationGateConfig()
	validationGate.EvidencePath = evidencePath
	validationGate.AllowUnverified = true

	g := NewGardener(Config{
		Registry:       reg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
		ValidationGate: validationGate,
	})

	if _, err := g.RunCycleV2(DefaultEvolveV2Config()); err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}

	sloPath := filepath.Join(dir, "slo-metrics.json")
	data, err := os.ReadFile(sloPath)
	if err != nil {
		t.Fatalf("expected RunCycleV2 to export %s from file evidence, but it wasn't written: %v", sloPath, err)
	}
	var exported map[string]float64
	if err := json.Unmarshal(data, &exported); err != nil {
		t.Fatalf("unmarshal exported slo-metrics.json: %v", err)
	}
	if v, ok := exported["agent-x:slo_export_tree/success_rate"]; !ok || v != 1.0 {
		t.Errorf("exported SLO map = %v; want agent-x:slo_export_tree/success_rate = 1.0 sourced from file evidence", exported)
	}
}

// TestGardener_AnyInFlight verifies the Gardener exposes a mid-cycle guard
// mirroring bt-agent's Scheduler.AnyInFlight (see
// TestScheduler_AnyInFlight in internal/agent/scheduler_test.go): the
// deploy-drift AutoRestart wiring in cmd/bt-gardener/main.go consults this
// before restarting the daemon's own binary, so a rebuild adoption can no
// longer SIGTERM the gardener mid-evolution-cycle.
func TestGardener_AnyInFlight(t *testing.T) {
	g := NewGardener(Config{})

	if g.AnyInFlight() {
		t.Fatal("AnyInFlight = true before any cycle started, want false")
	}

	g.cycleInFlight.Store(true)
	if !g.AnyInFlight() {
		t.Fatal("AnyInFlight = false with a cycle marked in-flight, want true")
	}

	g.cycleInFlight.Store(false)
	if g.AnyInFlight() {
		t.Fatal("AnyInFlight = true after cycle completed, want false")
	}
}
