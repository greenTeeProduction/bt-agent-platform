package knowledge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// RecordSelectorChildOutcomes is the writer half of the selector-telemetry
// loop: the runner delivers the run's Selector-attributed terminal child
// ticks, and successive runs must ACCUMULATE into the same durable per-tree
// stats file (SaveSelectorStats merges onto disk).
func TestRecordSelectorChildOutcomes_WritesAndAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selector-stats.json")

	first := []SelectorChildOutcome{
		{Selector: "Router", Child: "Cheap", Status: "failure"},
		{Selector: "Router", Child: "Reliable", Status: "success"},
	}
	if err := RecordSelectorChildOutcomes(path, first); err != nil {
		t.Fatalf("first record: %v", err)
	}
	second := []SelectorChildOutcome{
		{Selector: "Router", Child: "Reliable", Status: "success"},
	}
	if err := RecordSelectorChildOutcomes(path, second); err != nil {
		t.Fatalf("second record: %v", err)
	}

	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	if err := so.LoadSelectorStats(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	rs := so.Stats["Router"]
	if rs == nil {
		t.Fatal("Router stats missing from durable telemetry")
	}
	if got := rs.Children["Reliable"]; got == nil || got.Successes != 2 {
		t.Fatalf("Reliable successes = %+v, want 2 (accumulated across runs)", got)
	}
	if got := rs.Children["Cheap"]; got == nil || got.Failures != 1 {
		t.Fatalf("Cheap failures = %+v, want 1", got)
	}
}

// Malformed and empty batches must not litter the stats dir with empty files.
func TestRecordSelectorChildOutcomes_SkipsEmptyAndMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selector-stats.json")

	if err := RecordSelectorChildOutcomes(path, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := RecordSelectorChildOutcomes(path, []SelectorChildOutcome{
		{Selector: "", Child: "x", Status: "success"},
		{Selector: "S", Child: "", Status: "success"},
	}); err != nil {
		t.Fatalf("malformed batch: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("no stats file should exist for empty/malformed batches; stat err = %v", err)
	}
}

// RecordDecisionTreeChildOutcomes is the DTAnalyzer-side sibling of
// RecordSelectorChildOutcomes: the runner delivers the run's
// Selector-attributed terminal child ticks (each carrying the child's
// decision-tree condition), and successive runs must ACCUMULATE into the same
// durable per-tree DTAnalyzer stats file. DTAnalyzer.Save itself does not
// merge onto disk (unlike SaveSelectorStats), so the bridge must Load before
// Save to fold new hits onto whatever is already persisted.
func TestRecordDecisionTreeChildOutcomes_WritesAndAccumulates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dt-stats.json")

	first := []DecisionTreeChildOutcome{
		{Selector: "Router", Child: "Cheap", Condition: "IsCheap", Status: "failure"},
		{Selector: "Router", Child: "Reliable", Condition: "IsReliable", Status: "success"},
	}
	if err := RecordDecisionTreeChildOutcomes(path, first); err != nil {
		t.Fatalf("first record: %v", err)
	}
	second := []DecisionTreeChildOutcome{
		{Selector: "Router", Child: "Reliable", Condition: "IsReliable", Status: "success"},
	}
	if err := RecordDecisionTreeChildOutcomes(path, second); err != nil {
		t.Fatalf("second record: %v", err)
	}

	da := evolution.NewDTAnalyzer()
	if err := da.Load(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	ss := da.Stats["Router"]
	if ss == nil {
		t.Fatal("Router stats missing from durable decision-tree telemetry")
	}
	var reliable, cheap *evolution.PathStats
	for i := range ss.Paths {
		switch ss.Paths[i].PathName {
		case "Reliable":
			reliable = &ss.Paths[i]
		case "Cheap":
			cheap = &ss.Paths[i]
		}
	}
	if reliable == nil || reliable.HitCount != 2 || reliable.SuccessCount != 2 {
		t.Fatalf("Reliable path = %+v, want HitCount/SuccessCount 2/2 (accumulated across runs)", reliable)
	}
	if reliable.Condition != "IsReliable" {
		t.Fatalf("Reliable condition = %q, want IsReliable", reliable.Condition)
	}
	if cheap == nil || cheap.HitCount != 1 || cheap.SuccessCount != 0 {
		t.Fatalf("Cheap path = %+v, want HitCount/SuccessCount 1/0", cheap)
	}
}

// Malformed and empty batches must not litter the stats dir with empty files.
func TestRecordDecisionTreeChildOutcomes_SkipsEmptyAndMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dt-stats.json")

	if err := RecordDecisionTreeChildOutcomes(path, nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if err := RecordDecisionTreeChildOutcomes(path, []DecisionTreeChildOutcome{
		{Selector: "", Child: "x", Condition: "c", Status: "success"},
		{Selector: "S", Child: "", Condition: "c", Status: "success"},
	}); err != nil {
		t.Fatalf("malformed batch: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("no stats file should exist for empty/malformed batches; stat err = %v", err)
	}
}
