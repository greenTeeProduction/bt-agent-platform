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
