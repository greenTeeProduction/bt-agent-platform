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
	if _, ok := reflect.TypeFor[GardenerMetrics]().FieldByName("CrisisInterventions"); !ok {
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

// TestCollect_SurfacesTopEvolvedWinnersFromTrees pins milestone 3/4 of the
// "Surface knowledge-graph fitness and evolution lineage" program: the
// Evolution tab's "Best Fitness" stat and static "Algorithms Active" panel
// are sourced only from the disconnected gardener-metrics.json aggregate
// (loadGardenerMetrics/GardenerMetrics.BestFitness), never from live
// KnowledgeGraph tree data. Collect must accept a per-tree snapshot slice —
// the same structural_fitness/evolved_count/lineage fields milestone 2
// already added to the /api/trees response — and rank them into
// Metrics.TopWinners (descending by StructuralFitness) so the dashboard can
// render a live top-evolved-winners view instead of the static panel.
func TestCollect_SurfacesTopEvolvedWinnersFromTrees(t *testing.T) {
	trees := []TreeSnapshot{
		{ID: "base:tree", StructuralFitness: 40.0, EvolvedCount: 1},
		{ID: "base:tree-evolved-1", StructuralFitness: 72.5, EvolvedCount: 0, BaseID: "base:tree"},
		{ID: "other:tree", StructuralFitness: 55.0, EvolvedCount: 3},
	}

	m := Collect(len(trees), map[string]int{"finance": len(trees)}, trees)

	if len(m.TopWinners) != len(trees) {
		t.Fatalf("TopWinners len = %d, want %d (one ranked entry per tree)", len(m.TopWinners), len(trees))
	}

	// Ranked descending by StructuralFitness (live KnowledgeGraph data), not
	// the gardener-file best_fitness scalar.
	wantOrder := []string{"base:tree-evolved-1", "other:tree", "base:tree"}
	for i, id := range wantOrder {
		if m.TopWinners[i].ID != id {
			t.Errorf("TopWinners[%d].ID = %q, want %q (ranked by structural fitness desc)", i, m.TopWinners[i].ID, id)
		}
	}

	top := m.TopWinners[0]
	if top.StructuralFitness != 72.5 {
		t.Errorf("TopWinners[0].StructuralFitness = %v, want 72.5", top.StructuralFitness)
	}
	if top.BaseID != "base:tree" {
		t.Errorf("TopWinners[0].BaseID = %q, want %q (lineage base id, not dropped)", top.BaseID, "base:tree")
	}
	if got, want := m.TopWinners[1].EvolvedCount, 3; got != want {
		t.Errorf("TopWinners[1].EvolvedCount = %d, want %d", got, want)
	}

	// The dashboard JS reads /metrics/live JSON directly, so the ranked
	// winners must round-trip under a snake_case top_winners key.
	var wire map[string]any
	if err := json.Unmarshal(m.ToJSON(), &wire); err != nil {
		t.Fatalf("round-tripping Metrics JSON: %v", err)
	}
	winners, ok := wire["top_winners"].([]any)
	if !ok || len(winners) != len(trees) {
		t.Fatalf("dashboard JSON top_winners = %v, want %d entries", wire["top_winners"], len(trees))
	}
	first, ok := winners[0].(map[string]any)
	if !ok || first["structural_fitness"] != 72.5 {
		t.Errorf("wire top_winners[0].structural_fitness = %v, want 72.5", first["structural_fitness"])
	}
	if first["id"] != "base:tree-evolved-1" {
		t.Errorf("wire top_winners[0].id = %v, want %q", first["id"], "base:tree-evolved-1")
	}
}

// TestCollect_IncludesDLQCategoriesFromHook pins that Collect surfaces a
// lightweight DLQ health rollup, mirroring the existing DiscoverTreeFn /
// KGAnalyticsRefreshFn package-var injection-hook pattern (see executor.go
// and metrics_utils.go) instead of widening Collect's positional signature
// and breaking every call site. main.go is expected to wire
// DLQCategoriesFn = dlq.CategoryCounts at startup so the dashboard can
// render a DLQ category breakdown without a separate round-trip to
// /api/dlq. Today DLQCategoriesFn does not exist and Metrics carries no DLQ
// field, so this fails to compile until both land.
func TestCollect_IncludesDLQCategoriesFromHook(t *testing.T) {
	origFn := DLQCategoriesFn
	t.Cleanup(func() { DLQCategoriesFn = origFn })

	DLQCategoriesFn = func() map[string]int {
		return map[string]int{"network": 2, "timeout": 1}
	}

	// Collect memoizes its snapshot for 2s; bypass the cache so this test
	// isn't order-dependent on other Collect() calls in this package's suite.
	mu.Lock()
	lastSnap = nil
	mu.Unlock()

	m := Collect(0, map[string]int{}, nil)

	if m.DLQCategories == nil {
		t.Fatal("Metrics.DLQCategories is nil; DLQCategoriesFn hook result was dropped")
	}
	if got, want := m.DLQCategories["network"], 2; got != want {
		t.Errorf("DLQCategories[\"network\"] = %d, want %d", got, want)
	}
	if got, want := m.DLQCategories["timeout"], 1; got != want {
		t.Errorf("DLQCategories[\"timeout\"] = %d, want %d", got, want)
	}

	var wire map[string]any
	if err := json.Unmarshal(m.ToJSON(), &wire); err != nil {
		t.Fatalf("round-tripping Metrics JSON: %v", err)
	}
	rawCats, ok := wire["dlq_categories"]
	if !ok {
		t.Fatalf("dashboard JSON has no dlq_categories field; wire=%v", wire)
	}
	cats, ok := rawCats.(map[string]any)
	if !ok || cats["network"] != float64(2) {
		t.Errorf("wire dlq_categories = %v, want network=2", rawCats)
	}
}

// TestLoadGardenerMetricsParsesRollbacks verifies milestone 3/3 of the
// "Q2 Evolvability — Make gardener mutation rollback automatic,
// multi-revision, and observable" program: loadGardenerMetrics must surface
// total_rollbacks from gardener-metrics.json into a Rollbacks field on
// GardenerMetrics, so a recorded rollback cycle becomes visible in dashboard
// metrics instead of being silently dropped.
func TestLoadGardenerMetricsParsesRollbacks(t *testing.T) {
	writeGardenerFixture(t, `{
		"last_run": 1751980000,
		"total_cycles": 5,
		"total_rollbacks": 3,
		"active_trees": 2,
		"best_fitness": 42.0,
		"history": [
			{"tree_name": "code-analysis", "cycle": 1, "timestamp": 1751979000, "base_fitness": 40, "new_fitness": 42, "delta": 2, "improved": true, "rollbacks": 3}
		]
	}`)

	gm := loadGardenerMetrics()
	if gm == nil {
		t.Fatal("loadGardenerMetrics() = nil for a valid aggregate document, dashboard drops all gardener data")
	}

	if _, ok := reflect.TypeFor[GardenerMetrics]().FieldByName("Rollbacks"); !ok {
		t.Error("GardenerMetrics has no Rollbacks field; total_rollbacks is dropped")
	}

	serialized, err := json.Marshal(gm)
	if err != nil {
		t.Fatalf("marshaling GardenerMetrics: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(serialized, &wire); err != nil {
		t.Fatalf("round-tripping GardenerMetrics JSON: %v", err)
	}
	if got, ok := wire["rollbacks"]; !ok || got != float64(3) {
		t.Errorf("dashboard JSON rollbacks = %v (present=%v), want 3 (from total_rollbacks)", got, ok)
	}
}
