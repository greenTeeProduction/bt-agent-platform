package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
