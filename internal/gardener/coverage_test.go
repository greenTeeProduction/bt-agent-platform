package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ============================================================================
// helper function tests
// ============================================================================

func TestMaxInt(t *testing.T) {
	if got := maxInt(5, 3); got != 5 {
		t.Errorf("maxInt(5,3)=%d, want 5", got)
	}
	if got := maxInt(2, 7); got != 7 {
		t.Errorf("maxInt(2,7)=%d, want 7", got)
	}
	if got := maxInt(0, 0); got != 0 {
		t.Errorf("maxInt(0,0)=%d, want 0", got)
	}
	if got := maxInt(-1, 1); got != 1 {
		t.Errorf("maxInt(-1,1)=%d, want 1", got)
	}
}

func TestBaseNodeCount(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"domain_code_review", 30},
		{"domain_devops_ci", 30},
		{"domain_agent_monitor", 30},
		// finance trees
		{"finance_pitch_agent", 39},
		{"finance_earnings_reviewer", 27},
		{"finance_market_researcher", 27},
		{"finance_model_builder", 27},
		{"finance_meeting_prep", 27},
		{"finance_valuation_reviewer", 27},
		{"finance_gl_reconciler", 27},
		{"finance_month_end_closer", 27},
		{"finance_statement_auditor", 27},
		{"finance_kyc_screener", 27},
		// research trees
		{"research_deep_research", 54},
		{"research_quick_research", 18},
		// special trees
		{"godev", 30},
		{"default", 22},
		// unknown
		{"custom_tree", 25},
		{"unknown_domain", 25},
	}
	for _, tt := range tests {
		if got := baseNodeCount(tt.name); got != tt.expected {
			t.Errorf("baseNodeCount(%q)=%d, want %d", tt.name, got, tt.expected)
		}
	}
}

func TestHasNodeNamed(t *testing.T) {
	tree := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Name: "Child1"},
			{Name: "Child2", Children: []evolution.SerializableNode{
				{Name: "Grandchild"},
			}},
		},
	}
	if !hasNodeNamed(tree, "Root") {
		t.Error("hasNodeNamed should find Root")
	}
	if !hasNodeNamed(tree, "Child1") {
		t.Error("hasNodeNamed should find Child1")
	}
	if !hasNodeNamed(tree, "Grandchild") {
		t.Error("hasNodeNamed should find Grandchild")
	}
	if hasNodeNamed(tree, "Nonexistent") {
		t.Error("hasNodeNamed should not find Nonexistent")
	}
}

func TestIsNodeWrapped(t *testing.T) {
	// Node NOT wrapped — Retry wraps a different node
	notWrapped := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Retry",
				Name: "RetryWrap",
				Children: []evolution.SerializableNode{
					{Name: "SomeOtherNode"},
				},
			},
		},
	}
	if isNodeWrapped(notWrapped, "TargetNode") {
		t.Error("TargetNode should not be detected as wrapped")
	}

	// Node IS wrapped
	wrapped := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Retry",
				Name: "RetryWrap",
				Children: []evolution.SerializableNode{
					{Name: "TargetNode"},
				},
			},
		},
	}
	if !isNodeWrapped(wrapped, "TargetNode") {
		t.Error("TargetNode should be detected as wrapped")
	}

	// Nested wrapped
	nested := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Name: "Middle",
				Children: []evolution.SerializableNode{
					{
						Type: "Retry",
						Name: "RetryWrap",
						Children: []evolution.SerializableNode{
							{Name: "DeepTarget"},
						},
					},
				},
			},
		},
	}
	if !isNodeWrapped(nested, "DeepTarget") {
		t.Error("DeepTarget should be detected as wrapped (nested)")
	}
}

func TestGetRetryCount(t *testing.T) {
	noRetry := &evolution.SerializableNode{Name: "Root"}
	if got := getRetryCount(noRetry, "Any"); got != 0 {
		t.Errorf("getRetryCount on tree with no retry = %d, want 0", got)
	}

	withRetry := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type:       "Retry",
				Name:       "MyRetry",
				MaxRetries: 5,
			},
		},
	}
	if got := getRetryCount(withRetry, "MyRetry"); got != 5 {
		t.Errorf("getRetryCount = %d, want 5", got)
	}
	if got := getRetryCount(withRetry, "Other"); got != 0 {
		t.Errorf("getRetryCount for non-existent = %d, want 0", got)
	}

	nested := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Name: "Middle",
				Children: []evolution.SerializableNode{
					{
						Type:       "Retry",
						Name:       "NestedRetry",
						MaxRetries: 10,
					},
				},
			},
		},
	}
	if got := getRetryCount(nested, "NestedRetry"); got != 10 {
		t.Errorf("getRetryCount nested = %d, want 10", got)
	}
}

// ============================================================================
// Config and NewGardener tests
// ============================================================================

func TestNewGardener(t *testing.T) {
	cfg := Config{
		Interval:     60,
		MaxMutations: 3,
		UseRealLLM:   false,
	}
	g := NewGardener(cfg)
	if g == nil {
		t.Fatal("NewGardener returned nil")
	}
	if g.cfg.Interval != 60 {
		t.Errorf("Interval = %v, want 60", g.cfg.Interval)
	}
	if g.cfg.MaxMutations != 3 {
		t.Errorf("MaxMutations = %d, want 3", g.cfg.MaxMutations)
	}
	if g.cfg.UseRealLLM != false {
		t.Errorf("UseRealLLM = %v, want false", g.cfg.UseRealLLM)
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	if cfg.Interval != 0 {
		t.Error("Interval should default to 0")
	}
	if cfg.MaxMutations != 0 {
		t.Error("MaxMutations should default to 0")
	}
	if cfg.UseRealLLM != false {
		t.Error("UseRealLLM should default to false")
	}
}

// ============================================================================
// Registry tests
// ============================================================================

func TestRegistry_List(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRegistry(tempDir)
	entries := r.List()
	if len(entries) == 0 {
		t.Fatal("Registry.List returned 0 entries")
	}
	// Check every entry has required fields
	for _, e := range entries {
		if e.Name == "" {
			t.Error("entry has empty name")
		}
		if e.Tree == nil {
			t.Errorf("entry %q has nil tree", e.Name)
		}
		if e.FilePath == "" {
			t.Errorf("entry %q has empty file path", e.Name)
		}
		if !e.Active {
			t.Errorf("entry %q is not active", e.Name)
		}
	}
	// Verify we have expected entries
	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name] = true
	}
	expected := []string{"default", "godev", "domain_code_review", "finance_pitch_agent", "research_deep_research"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected %q in registry", name)
		}
	}
}

func TestRegistry_CountMatchesList(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRegistry(tempDir)
	count := r.Count()
	if count == 0 {
		t.Fatal("expected Count() > 0")
	}
	entries := r.List()
	if len(entries) != count {
		t.Errorf("Count()=%d but List() len=%d", count, len(entries))
	}
}

func TestRegistry_ListReturnsCopy(t *testing.T) {
	tempDir := t.TempDir()
	r := NewRegistry(tempDir)
	entries1 := r.List()
	entries2 := r.List()
	if len(entries1) != len(entries2) {
		t.Fatal("List returned different lengths on subsequent calls")
	}
	// Mutating the first should not affect the second
	entries1[0].Name = "mutated"
	if entries2[0].Name == "mutated" {
		t.Error("mutating returned slice affected registry or subsequent List calls")
	}
}

func TestRegistry_SaveTree(t *testing.T) {
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
	// Verify file exists
	if _, err := os.Stat(entry.FilePath); os.IsNotExist(err) {
		t.Errorf("expected file %s to exist after SaveTree", entry.FilePath)
	}
	// Verify file is valid JSON with tree content
	data, err := os.ReadFile(entry.FilePath)
	if err != nil {
		t.Fatalf("cannot read saved file: %v", err)
	}
	var tree evolution.SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Errorf("saved file is not valid JSON: %v", err)
	}
}

func TestRegistry_PersistedTreeLoading(t *testing.T) {
	tempDir := t.TempDir()

	// Create a custom persisted tree file
	customTree := evolution.SerializableNode{
		Type: "Sequence",
		Name: "CustomTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "DoSomething"},
		},
	}
	data, _ := json.MarshalIndent(customTree, "", "  ")
	treePath := filepath.Join(tempDir, "tree-custom_tree.json")
	_ = os.WriteFile(treePath, data, 0644)

	// Now create the registry — it should load our custom tree
	r := NewRegistry(tempDir)
	entries := r.List()

	found := false
	for _, e := range entries {
		if e.Name == "tree-custom_tree" {
			found = true
			if e.Description != "Persisted tree" {
				t.Errorf("persisted tree description = %q, want 'Persisted tree'", e.Description)
			}
			break
		}
	}
	if !found {
		t.Error("persisted tree 'tree-custom_tree' not found in registry")
	}
}

func TestRegistry_InvalidJSONFileSkipped(t *testing.T) {
	tempDir := t.TempDir()

	// Write an invalid JSON file matching tree-*.json pattern
	_ = os.WriteFile(filepath.Join(tempDir, "tree-invalid.json"), []byte("{not json}"), 0644)

	// Should not panic
	r := NewRegistry(tempDir)
	entries := r.List()
	for _, e := range entries {
		if e.Name == "tree-invalid" {
			t.Error("invalid JSON file should have been skipped")
		}
	}
}

func TestRegistry_SkipsNonTreeJSON(t *testing.T) {
	tempDir := t.TempDir()

	// Write files that should be skipped
	_ = os.WriteFile(filepath.Join(tempDir, "not-tree.json"), []byte(`{"type":"Test"}`), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "tree-valid.txt"), []byte("not json"), 0644)

	r := NewRegistry(tempDir)
	entries := r.List()
	for _, e := range entries {
		if e.Name == "not-tree" {
			t.Error("non tree-*.json file should not be loaded")
		}
	}
}

// ============================================================================
// MetricsTracker tests
// ============================================================================

func TestMetricsTracker_RecordAndCyclesForTree(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "a", Cycle: 1})
	mt.Record(CycleMetrics{TreeName: "a", Cycle: 2})
	mt.Record(CycleMetrics{TreeName: "b", Cycle: 1})

	if got := mt.CyclesForTree("a"); got != 2 {
		t.Errorf("CyclesForTree(a)=%d, want 2", got)
	}
	if got := mt.CyclesForTree("b"); got != 1 {
		t.Errorf("CyclesForTree(b)=%d, want 1", got)
	}
	if got := mt.CyclesForTree("nonexistent"); got != 0 {
		t.Errorf("CyclesForTree(nonexistent)=%d, want 0", got)
	}
}

func TestMetricsTracker_Summary_Empty(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	summary := mt.Summary()
	if cycles, ok := summary["cycles"].(int); !ok || cycles != 0 {
		t.Errorf("empty summary should have cycles=0, got %v", summary)
	}
}

func TestMetricsTracker_Summary_Basic(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1})

	summary := mt.Summary()
	if totalCycles, ok := summary["total_cycles"].(int); !ok || totalCycles != 2 {
		t.Errorf("total_cycles: %v (type %T), want 2", summary["total_cycles"], summary["total_cycles"])
	}
	if uniqueTrees, ok := summary["unique_trees"].(int); !ok || uniqueTrees != 2 {
		t.Errorf("unique_trees: %v, want 2", summary["unique_trees"])
	}
}

// perTreeStats extracts the per_tree map from the summary using JSON round-trip
// because treeStats is a local type inside Summary() and can't be type-asserted directly.
func perTreeStats(summary map[string]interface{}) map[string]testTreeStats {
	data, _ := json.Marshal(summary["per_tree"])
	var result map[string]testTreeStats
	_ = json.Unmarshal(data, &result)
	return result
}

func TestMetricsTracker_Summary_WithImprovements(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	// Record some improved and some non-improved cycles
	mt.Record(CycleMetrics{
		TreeName: "tree_a", Cycle: 1,
		BaseFitness: 1.0, NewFitness: 1.5, Delta: 0.5,
		Improved: true, Mutations: 2,
	})
	mt.Record(CycleMetrics{
		TreeName: "tree_a", Cycle: 2,
		BaseFitness: 1.5, NewFitness: 2.0, Delta: 0.5,
		Improved: true, Mutations: 1,
	})
	mt.Record(CycleMetrics{
		TreeName: "tree_b", Cycle: 1,
		BaseFitness: 0.8, NewFitness: 0.8, Delta: 0.0,
		Improved: false, Mutations: 0,
	})
	mt.Record(CycleMetrics{
		TreeName: "tree_b", Cycle: 2,
		BaseFitness: 0.8, NewFitness: 0.7, Delta: -0.1,
		Improved: false, Mutations: 0,
	})

	summary := mt.Summary()
	if totalCycles, ok := summary["total_cycles"].(int); !ok || totalCycles != 4 {
		t.Errorf("total_cycles = %v, want 4", summary["total_cycles"])
	}
	if totalImpr, ok := summary["total_improvements"].(int); !ok || totalImpr != 2 {
		t.Errorf("total_improvements = %v, want 2", summary["total_improvements"])
	}

	// Check improvement_rate
	if rate, ok := summary["improvement_rate"].(string); !ok || rate != "50.0%" {
		t.Errorf("improvement_rate = %v, want '50.0%%'", summary["improvement_rate"])
	}

	// Check per_tree stats using JSON round-trip
	ptMap := perTreeStats(summary)
	ts, ok := ptMap["tree_a"]
	if !ok {
		t.Fatal("tree_a missing from per_tree")
	}
	if ts.Cycles != 2 {
		t.Errorf("tree_a cycles = %d, want 2", ts.Cycles)
	}
	if ts.Improvements != 2 {
		t.Errorf("tree_a improvements = %d, want 2", ts.Improvements)
	}
	if ts.BestFitness != 2.0 {
		t.Errorf("tree_a best_fitness = %f, want 2.0", ts.BestFitness)
	}
	if ts.LastFitness != 2.0 {
		t.Errorf("tree_a last_fitness = %f, want 2.0", ts.LastFitness)
	}
	if ts.TotalDelta != 1.0 {
		t.Errorf("tree_a total_delta = %f, want 1.0", ts.TotalDelta)
	}
}

func TestMetricsTracker_BestFitnessTracking(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	// Fitness goes up then down — best should remain max
	mt.Record(CycleMetrics{TreeName: "x", Cycle: 1, BaseFitness: 1.0, NewFitness: 2.0, Improved: true, Delta: 1.0})
	mt.Record(CycleMetrics{TreeName: "x", Cycle: 2, BaseFitness: 2.0, NewFitness: 1.5, Improved: false, Delta: -0.5})

	summary := mt.Summary()
	ptMap := perTreeStats(summary)
	ts := ptMap["x"]
	if ts.BestFitness != 2.0 {
		t.Errorf("best_fitness = %f, want 2.0", ts.BestFitness)
	}
}

func TestMetricsTracker_LastFitnessTracking(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "x", Cycle: 1, BaseFitness: 1.0, NewFitness: 2.0, Improved: true, Delta: 1.0})
	mt.Record(CycleMetrics{TreeName: "x", Cycle: 2, BaseFitness: 2.0, NewFitness: 1.5, Improved: false, Delta: -0.5})

	summary := mt.Summary()
	ptMap := perTreeStats(summary)
	ts := ptMap["x"]
	if ts.LastFitness != 1.5 {
		t.Errorf("last_fitness = %f, want 1.5", ts.LastFitness)
	}
}

func TestMetricsTracker_ImprovementRate(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	// 3 cycles, 2 improved = 66.7%
	mt.Record(CycleMetrics{TreeName: "a", Cycle: 1, Improved: true, Delta: 0.5, BaseFitness: 1.0, NewFitness: 1.5})
	mt.Record(CycleMetrics{TreeName: "a", Cycle: 2, Improved: true, Delta: 0.5, BaseFitness: 1.5, NewFitness: 2.0})
	mt.Record(CycleMetrics{TreeName: "a", Cycle: 3, Improved: false, Delta: 0.0, BaseFitness: 2.0, NewFitness: 2.0})

	summary := mt.Summary()
	if rate, ok := summary["improvement_rate"].(string); !ok || rate != "66.7%" {
		t.Errorf("improvement_rate = %v, want '66.7%%'", summary["improvement_rate"])
	}
}

func TestMetricsTracker_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	mt.Record(CycleMetrics{TreeName: "tree_a", Cycle: 1, Improved: true, Delta: 0.5, NodesBefore: 10, NodesAfter: 12})
	mt.Record(CycleMetrics{TreeName: "tree_b", Cycle: 1, Improved: false, Delta: 0.0, NodesBefore: 5, NodesAfter: 5})

	if err := mt.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Create a new tracker from same dir — should load saved data
	mt2, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker (reload): %v", err)
	}

	if got := mt2.CyclesForTree("tree_a"); got != 1 {
		t.Errorf("after reload: CyclesForTree(tree_a)=%d, want 1", got)
	}
	if got := mt2.CyclesForTree("tree_b"); got != 1 {
		t.Errorf("after reload: CyclesForTree(tree_b)=%d, want 1", got)
	}
}

func TestMetricsTracker_SaveNewDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new-subdir")
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	mt.Record(CycleMetrics{TreeName: "x", Cycle: 1})
	if err := mt.Save(); err != nil {
		t.Errorf("Save on new directory failed: %v", err)
	}
}

func TestMetricsTracker_TruncationAtMaxHistory(t *testing.T) {
	tempDir := t.TempDir()
	mt, err := NewMetricsTracker(tempDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	// Add more than 10000 records — should truncate to last 5000
	for i := 0; i < 10001; i++ {
		mt.Record(CycleMetrics{TreeName: "tree", Cycle: i + 1})
	}

	// After truncation, the first records should be gone
	// CyclesForTree counts all records for that tree
	// But the history should have been trimmed
	summary := mt.Summary()
	totalCycles, _ := summary["total_cycles"].(int)
	// Should be exactly 5000 (truncated to last 5000)
	if totalCycles != 5000 {
		t.Errorf("after truncation, total_cycles = %d, want 5000", totalCycles)
	}
}

// ============================================================================
// RunCycle / evolveTree tests (mock LLM, no Ollama)
// ============================================================================

func setupGardener(t *testing.T) (*Gardener, *Registry, *MetricsTracker, string) {
	t.Helper()
	dir := t.TempDir()

	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tt, err := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 100)
	if err != nil {
		t.Fatalf("NewTranspositionTable: %v", err)
	}

	registry := NewRegistry(dir)
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	cfg := Config{
		Registry:       registry,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
		Interval:       60,
		MaxMutations:   2,
		UseRealLLM:     false, // use mock LLM
	}
	return NewGardener(cfg), registry, mt, dir
}

func TestRunCycle_NoActiveTrees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping RunCycle benchmark validation path in short mode")
	}
	g, _, _, _ := setupGardener(t)

	// Deactivate all trees using the proper registry API
	g.cfg.Registry.DeactivateAll()

	results, err := g.RunCycle()
	if err != nil {
		t.Errorf("RunCycle returned error: %v", err)
	}
	// Should produce no results since all trees are inactive
	if len(results) != 0 {
		t.Errorf("expected 0 results with all trees deactivated, got %d", len(results))
	}
}

func TestEvolveTree_NilTree(t *testing.T) {
	// Create a gardener with a tree that has nil Tree in the entry.
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	// Create a custom registry with a nil-tree entry
	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "nil_tree", Description: "tree with nil", Tree: nil, FilePath: dir + "/nil.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
		MaxMutations:   2,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycle()
	if err != nil {
		t.Errorf("RunCycle with nil tree: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result for nil tree entry")
	}
	r := results[0]
	if r.TreeName != "nil_tree" {
		t.Errorf("TreeName = %q, want 'nil_tree'", r.TreeName)
	}
	if r.Improved {
		t.Error("nil tree should not be 'improved'")
	}
}

func TestEvolveTree_BloatGuard(t *testing.T) {
	// Create a tree with massive node count to trigger the bloat guard
	// The bloat guard fires when nodesBefore > baseNodes * 20
	// For "godev", baseNodes = 30, so we need > 600 nodes
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	// Build a bloated tree with 601+ nodes
	bloatedTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "BigTree",
	}
	bloatedTree.Children = make([]evolution.SerializableNode, 0)
	for i := 0; i < 700; i++ {
		bloatedTree.Children = append(bloatedTree.Children, evolution.SerializableNode{
			Type: "Action",
			Name: "Dummy",
		})
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "godev", Description: "go dev tree", Tree: bloatedTree, FilePath: dir + "/tree-godev.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
		MaxMutations:   2,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycle()
	if err != nil {
		t.Errorf("RunCycle with bloated tree: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	r := results[0]
	if r.Improved {
		t.Error("bloated tree should be stopped by bloat guard (not improved)")
	}
	// baseNodes for godev is 30, threshold is 30*20=600, we have 701 nodes
	// (1 root + 700 children)
	if r.NodesBefore <= 600 {
		t.Errorf("bloated tree should have > 600 nodes, got %d", r.NodesBefore)
	}
}

func TestEvolveTree_WithRealTree(t *testing.T) {
	// Test evolveTree with a real tree using mock LLM
	// This exercises the full evolution path: fitness eval, mutation ordering,
	// benchmark validation, etc.
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	// Minimal valid tree structure
	simpleTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "SimpleTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Analyze", Description: "analyze input"},
			{Type: "Action", Name: "Execute", Description: "run task"},
			{Type: "Action", Name: "Verify", Description: "verify output"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "godev", Description: "go dev tree", Tree: simpleTree, FilePath: dir + "/tree-godev.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
		MaxMutations:   2,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycle()
	if err != nil {
		t.Errorf("RunCycle with real tree: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	r := results[0]
	if r.TreeName != "godev" {
		t.Errorf("TreeName = %q, want 'godev'", r.TreeName)
	}
	// Just verify the result structure is valid — mutations may or may not apply
	if r.BaseFitness < 0 || r.NewFitness < 0 {
		t.Log("fitness may be 0 with no reflection records, this is expected")
	}
	if r.NodesBefore <= 0 {
		t.Errorf("NodesBefore should be > 0, got %d", r.NodesBefore)
	}
	// Metrics should have been recorded
	if mt.CyclesForTree("godev") != 1 {
		t.Errorf("CyclesForTree(godev) = %d, want 1", mt.CyclesForTree("godev"))
	}
}

func TestEvolveTree_MultipleTrees(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "godev", Description: "go dev", Tree: simpleTree, FilePath: dir + "/tree-godev.json", Active: true},
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: false}, // inactive
		{Name: "custom", Description: "custom", Tree: simpleTree, FilePath: dir + "/tree-custom.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycle()
	if err != nil {
		t.Errorf("RunCycle: %v", err)
	}
	// Should process 2 active trees (godev and custom), skip default
	if len(results) != 2 {
		t.Errorf("expected 2 results (2 active trees), got %d: %+v", len(results), results)
	}
	// Entries should be sorted alphabetically: "custom" then "godev"
	if results[0].TreeName != "custom" {
		t.Errorf("first result should be 'custom', got %q", results[0].TreeName)
	}
	if results[1].TreeName != "godev" {
		t.Errorf("second result should be 'godev', got %q", results[1].TreeName)
	}
}

func TestRunCycle_MetricsSave(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	_, err := g.RunCycle()
	if err != nil {
		t.Fatalf("RunCycle: %v", err)
	}

	// Metrics should have been saved to disk after RunCycle
	metricsPath := filepath.Join(dir, "gardener-metrics.json")
	if _, err := os.Stat(metricsPath); os.IsNotExist(err) {
		t.Error("gardener-metrics.json was not created after RunCycle")
	}
}

func TestNewMetricsTracker_InvalidDir(t *testing.T) {
	// Create a file where a directory is expected
	dir := t.TempDir()
	filePath := filepath.Join(dir, "metrics")
	_ = os.WriteFile(filePath, []byte("data"), 0644)

	// NewMetricsTracker inside a file path? Actually it creates dir if missing.
	// Test with a path where the parent exists as a file — should still work
	// since os.MkdirAll creates dirs.
	subdir := filepath.Join(filePath, "sub")
	mt, err := NewMetricsTracker(subdir)
	if err != nil {
		t.Errorf("NewMetricsTracker on file-based path returned error: %v", err)
	}
	if mt == nil {
		t.Fatal("expected non-nil MetricsTracker")
	}
}

// ============================================================================
// CycleMetrics struct test
// ============================================================================

func TestCycleMetricsStruct(t *testing.T) {
	m := CycleMetrics{
		TreeName:    "test",
		Cycle:       5,
		Timestamp:   1234567890,
		BaseFitness: 1.0,
		NewFitness:  1.5,
		Delta:       0.5,
		Mutations:   3,
		NodesBefore: 10,
		NodesAfter:  12,
		Improved:    true,
		DurationMs:  150,
	}
	if m.TreeName != "test" {
		t.Error("TreeName mismatch")
	}
	if m.Cycle != 5 {
		t.Error("Cycle mismatch")
	}
	if m.Improved != true {
		t.Error("Improved should be true")
	}
	if m.Delta != 0.5 {
		t.Error("Delta mismatch")
	}
}

// ============================================================================
// Benchmark integration (mock LLM)
// ============================================================================

func TestBenchmarkMockIntegration(t *testing.T) {
	// Verify that benchmark.DefaultMock() works as expected
	mock := benchmark.DefaultMock()
	if mock.AnalyzeComplexity("any task") != "medium" {
		t.Error("mock complexity mismatch")
	}
	plan, err := mock.Generate("test prompt")
	if err != nil {
		t.Errorf("mock Generate error: %v", err)
	}
	if plan == "" {
		t.Error("mock Generate returned empty plan")
	}
	ww, ti := mock.Reflect("task", "outcome", "plan")
	if ww == "" || ti == "" {
		t.Error("mock Reflect returned empty strings")
	}
}

func TestQuickValidate_WithMockLLM(_ *testing.T) {
	// Test the benchmark.QuickValidate function with mock LLM
	mock := benchmark.DefaultMock()
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "TestTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step1"},
		},
	}
	suite := benchmark.GoDevSuite()
	score := benchmark.QuickValidate(tree, suite, mock, nil)
	// We don't assert specific score values, just that it runs without panic
	_ = score
}

// ============================================================================
// testTreeStats mirrors the local treeStats type defined inside Summary()
type testTreeStats struct {
	Cycles       int     `json:"cycles"`
	BestFitness  float64 `json:"best_fitness"`
	LastFitness  float64 `json:"last_fitness"`
	Improvements int     `json:"improvements"`
	TotalDelta   float64 `json:"total_delta"`
}

// ============================================================================
