package gardener

import (
	"os"
	"testing"
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
