package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
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
