package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// writeGardenerFixture points $HOME at a temp dir and writes the given
// gardener-metrics.json document where loadGardenerMetrics looks for it.
func writeGardenerFixture(t *testing.T, doc string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".go-bt-gardener")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating gardener dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gardener-metrics.json"), []byte(doc), 0o644); err != nil {
		t.Fatalf("writing gardener-metrics.json fixture: %v", err)
	}
}

// TestLoadGardenerMetricsParsesAggregateDocument pins milestone 3/5 of the
// evolution self-healing observability program: the dashboard must stop
// dropping gardener data. loadGardenerMetrics has to parse the aggregate
// gardener-metrics.json document — total_improvements into Improvements
// (today hardcoded 0), the real last_run unix timestamp into LastRun (today
// the "recent" literal), and total_crisis_interventions into a new
// CrisisInterventions field on GardenerMetrics — instead of discarding those
// keys, so crisis interventions and improvement counts survive to the
// dashboard JSON.
func TestLoadGardenerMetricsParsesAggregateDocument(t *testing.T) {
	const lastRunUnix = int64(1751980000)

	writeGardenerFixture(t, `{
		"last_run": 1751980000,
		"total_cycles": 12,
		"total_improvements": 4,
		"total_crisis_interventions": 2,
		"active_trees": 3,
		"best_fitness": 87.5,
		"history": [
			{"tree_name": "code-analysis", "cycle": 1, "timestamp": 1751979000, "base_fitness": 80, "new_fitness": 87.5, "delta": 7.5, "improved": true, "crisis_intervention": true},
			{"tree_name": "code-analysis", "cycle": 2, "timestamp": 1751980000, "base_fitness": 87.5, "new_fitness": 87.5, "delta": 0, "improved": false, "crisis_intervention": true}
		]
	}`)

	gm := loadGardenerMetrics()
	if gm == nil {
		t.Fatal("loadGardenerMetrics() = nil for a valid aggregate document, dashboard drops all gardener data")
	}

	// Existing parsing must keep working.
	if gm.Cycles != 12 {
		t.Errorf("Cycles = %d, want 12 (from total_cycles)", gm.Cycles)
	}
	if gm.Trees != 3 {
		t.Errorf("Trees = %d, want 3 (from active_trees)", gm.Trees)
	}
	if gm.BestFitness != 87.5 {
		t.Errorf("BestFitness = %v, want 87.5 (from best_fitness)", gm.BestFitness)
	}

	// Milestone behavior 1: total_improvements must reach Improvements
	// instead of the hardcoded 0.
	if gm.Improvements != 4 {
		t.Errorf("Improvements = %d, want 4 (parsed from total_improvements, not hardcoded 0)", gm.Improvements)
	}

	// Milestone behavior 2: the real last_run unix timestamp must be
	// surfaced (RFC3339 UTC) instead of the "recent" literal.
	wantLastRun := time.Unix(lastRunUnix, 0).UTC().Format(time.RFC3339)
	if gm.LastRun != wantLastRun {
		t.Errorf("LastRun = %q, want %q (parsed from last_run, not the \"recent\" literal)", gm.LastRun, wantLastRun)
	}

	// Milestone behavior 3: GardenerMetrics must carry a CrisisInterventions
	// field fed from total_crisis_interventions. Checked structurally so the
	// package still compiles pre-implementation and the run reports every
	// missing behavior at once.
	if _, ok := reflect.TypeOf(GardenerMetrics{}).FieldByName("CrisisInterventions"); !ok {
		t.Error("GardenerMetrics has no CrisisInterventions field; total_crisis_interventions is dropped")
	}
	serialized, err := json.Marshal(gm)
	if err != nil {
		t.Fatalf("marshaling GardenerMetrics: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(serialized, &wire); err != nil {
		t.Fatalf("round-tripping GardenerMetrics JSON: %v", err)
	}
	if got, ok := wire["crisis_interventions"]; !ok || got != float64(2) {
		t.Errorf("dashboard JSON crisis_interventions = %v (present=%v), want 2 (from total_crisis_interventions)", got, ok)
	}
}
