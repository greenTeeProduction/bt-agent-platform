package evolution

import (
	"path/filepath"
	"sync"
	"testing"
)

// TestSelectorOptimizer_InformationGain verifies IG-based ordering.
func TestSelectorOptimizer_InformationGain(t *testing.T) {
	so := NewSelectorOptimizer(OrderByIG)
	so.MinSamples = 1 // allow immediate reordering

	// Child A: 8 successes, 2 failures → IG should be higher
	// Child B: 3 successes, 7 failures → IG should be lower
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "QuickPath", Outcome: "failure"})

	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "success"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})

	order := so.OrderChildren("Router")
	if len(order) != 2 {
		t.Fatalf("expected 2 children, got %d", len(order))
	}
	// QuickPath (80% success) should come before SlowPath (30% success)
	if order[0] != "QuickPath" {
		t.Errorf("expected QuickPath first, got %v", order)
	}
}

// TestSelectorOptimizer_GiniImpurity verifies Gini-based ordering.
func TestSelectorOptimizer_GiniImpurity(t *testing.T) {
	so := NewSelectorOptimizer(OrderByGini)
	so.MinSamples = 1

	// Child A: all successes → Gini = 0 (perfectly pure)
	for range 10 {
		so.Record("Router", NodeExecutionRecord{NodeName: "PureChoice", Outcome: "success"})
	}
	// Child B: mixed → Gini > 0
	for range 5 {
		so.Record("Router", NodeExecutionRecord{NodeName: "MixedChoice", Outcome: "success"})
	}
	for range 5 {
		so.Record("Router", NodeExecutionRecord{NodeName: "MixedChoice", Outcome: "failure"})
	}

	order := so.OrderChildren("Router")
	if len(order) != 2 {
		t.Fatalf("expected 2 children, got %d", len(order))
	}
	if order[0] != "PureChoice" {
		t.Errorf("expected PureChoice first (lower Gini), got %v", order)
	}
}

// TestSelectorOptimizer_KillerHeuristic verifies killer heuristic ordering.
func TestSelectorOptimizer_KillerHeuristic(t *testing.T) {
	so := NewSelectorOptimizer(OrderByKiller)
	so.MinSamples = 1

	// A succeeds first, then B succeeds later
	so.Record("Router", NodeExecutionRecord{NodeName: "Alpha", Outcome: "success"}) // tick 0
	so.Record("Router", NodeExecutionRecord{NodeName: "Beta", Outcome: "failure"})  // tick 1
	so.Record("Router", NodeExecutionRecord{NodeName: "Alpha", Outcome: "failure"}) // tick 2
	so.Record("Router", NodeExecutionRecord{NodeName: "Beta", Outcome: "success"})  // tick 3 ← last success

	order := so.OrderChildren("Router")
	if len(order) != 2 {
		t.Fatalf("expected 2 children, got %d", len(order))
	}
	// Beta has the most recent success (tick 3 vs tick 0)
	if order[0] != "Beta" {
		t.Errorf("expected Beta first (killer heuristic), got %v", order)
	}
}

// TestGiniImpurity verifies the math.
func TestGiniImpurity(t *testing.T) {
	// All successes → Gini = 0
	cs := &ChildStats{Name: "test", Successes: 10}
	if g := GiniImpurity(cs); g != 0 {
		t.Errorf("expected Gini 0 for pure node, got %f", g)
	}

	// 50/50 → Gini = 0.5
	cs2 := &ChildStats{Name: "test", Successes: 5, Failures: 5}
	g := GiniImpurity(cs2)
	if g < 0.49 || g > 0.51 {
		t.Errorf("expected Gini ~0.5 for 50/50, got %f", g)
	}
}

// TestInformationGain verifies IG computation.
func TestInformationGain(t *testing.T) {
	stats := &SelectorStats{
		ParentName: "Router",
		Children:   make(map[string]*ChildStats),
	}
	// High success child
	stats.Children["Good"] = &ChildStats{Name: "Good", Successes: 8, Failures: 2}
	// Low success child
	stats.Children["Bad"] = &ChildStats{Name: "Bad", Successes: 3, Failures: 7}

	igGood := InformationGain(stats.Children["Good"], stats)
	igBad := InformationGain(stats.Children["Bad"], stats)

	if igGood <= igBad {
		t.Errorf("expected IG(Good) > IG(Bad), got %f vs %f", igGood, igBad)
	}
}

// TestLocalSearch_HillClimb verifies hill climbing doesn't regress fitness.
func TestLocalSearch_HillClimb(t *testing.T) {
	// Simple fitness: prefers trees with more children (more thorough)
	fitnessFn := func(tree *SerializableNode) float64 {
		return float64(CountNodes(tree))
	}

	tree := &SerializableNode{
		Type: "Selector",
		Name: "Root",
		Metadata: map[string]any{
			"threshold":  0.5,
			"timeout_ms": 1000.0,
		},
		Children: []SerializableNode{
			{Type: "Action", Name: "Child1"},
		},
	}

	searcher := NewLocalSearcher(HillClimbSearch)
	searcher.MaxIterations = 10

	initialFitness := fitnessFn(tree)
	refined, delta := searcher.Search(tree, fitnessFn)

	if delta < 0 {
		t.Errorf("hill climb should not regress fitness, got delta=%f", delta)
	}
	_ = refined
	_ = initialFitness
}

// TestLocalSearch_SimulatedAnnealing verifies SA produces valid output.
func TestLocalSearch_SimulatedAnnealing(t *testing.T) {
	fitnessFn := func(tree *SerializableNode) float64 {
		return float64(CountNodes(tree))
	}

	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Metadata: map[string]any{
			"threshold": 0.3,
		},
	}

	searcher := NewLocalSearcher(SimulatedAnnealingSearch)
	searcher.MaxIterations = 15
	searcher.Temperature = 2.0
	searcher.CoolingRate = 0.9

	initialFitness := fitnessFn(tree)
	refined, delta := searcher.Search(tree, fitnessFn)

	// SA can accept worse moves, so delta can be negative
	// but the tree should remain valid
	if refined == nil {
		t.Fatal("SA returned nil tree")
	}
	_ = initialFitness
	_ = delta
}

// TestLocalSearch_TabuSearch verifies tabu search produces valid output.
func TestLocalSearch_TabuSearch(t *testing.T) {
	fitnessFn := func(tree *SerializableNode) float64 {
		return float64(CountNodes(tree))
	}

	tree := &SerializableNode{
		Type: "Selector",
		Name: "Root",
	}

	searcher := NewLocalSearcher(TabuSearch)
	searcher.MaxIterations = 10

	refined, _ := searcher.Search(tree, fitnessFn)
	if refined == nil {
		t.Fatal("tabu search returned nil tree")
	}
}

// TestSelectorOptimizer_ApplyOrdering verifies reorder applies to tree.
func TestSelectorOptimizer_ApplyOrdering(t *testing.T) {
	tree := &SerializableNode{
		Type: "Selector",
		Name: "Router",
		Children: []SerializableNode{
			{Type: "Action", Name: "SlowPath"},
			{Type: "Action", Name: "FastPath"},
		},
	}

	so := NewSelectorOptimizer(OrderBySuccessRate)
	so.MinSamples = 1

	// FastPath has more successes
	for range 10 {
		so.Record("Router", NodeExecutionRecord{NodeName: "FastPath", Outcome: "success"})
		so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "failure"})
	}

	changed := so.ApplyOrdering(tree, "Router")
	if !changed {
		t.Error("expected ordering to change")
	}
	if tree.Children[0].Name != "FastPath" {
		t.Errorf("expected FastPath first after reorder, got %s", tree.Children[0].Name)
	}
}

// ─── Durable selector telemetry (ADR-024 flock + atomic tmp+rename) ───────

// TestSelectorOptimizer_SaveLoadRoundTrip verifies that recorded per-child
// outcomes survive a Save → fresh-optimizer Load round-trip so Selector
// telemetry persists across process restarts.
func TestSelectorOptimizer_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selector_stats.json")

	so := NewSelectorOptimizer(OrderBySuccessRate)
	for range 7 {
		so.Record("Router", NodeExecutionRecord{NodeName: "FastPath", Outcome: "success"})
	}
	for range 3 {
		so.Record("Router", NodeExecutionRecord{NodeName: "FastPath", Outcome: "failure"})
	}
	so.Record("Router", NodeExecutionRecord{NodeName: "SlowPath", Outcome: "running"})

	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("SaveSelectorStats: %v", err)
	}

	// A fresh optimizer (simulating a restart) must recover the same counts.
	loaded := NewSelectorOptimizer(OrderBySuccessRate)
	if err := loaded.LoadSelectorStats(path); err != nil {
		t.Fatalf("LoadSelectorStats: %v", err)
	}

	rs := loaded.Stats["Router"]
	if rs == nil {
		t.Fatalf("Router stats missing after load")
	}
	fast := rs.Children["FastPath"]
	if fast == nil {
		t.Fatalf("FastPath stats missing after load")
	}
	if fast.Successes != 7 || fast.Failures != 3 {
		t.Errorf("FastPath want 7 successes / 3 failures, got %d / %d",
			fast.Successes, fast.Failures)
	}
	slow := rs.Children["SlowPath"]
	if slow == nil || slow.Running != 1 {
		t.Errorf("SlowPath want 1 running, got %+v", slow)
	}
}

// TestSelectorOptimizer_ConcurrentMergeSumsCounts verifies that many
// independent optimizers persisting to the same file under the ADR-024
// flock have their per-child counts summed, not clobbered — the durable
// telemetry accumulates from every writer.
func TestSelectorOptimizer_ConcurrentMergeSumsCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selector_stats.json")

	const writers = 8
	const perWriter = 5

	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			// Each writer is a fresh optimizer contributing only its own
			// records; Save's flock+merge must sum them, not overwrite.
			so := NewSelectorOptimizer(OrderBySuccessRate)
			for range perWriter {
				so.Record("Router", NodeExecutionRecord{NodeName: "FastPath", Outcome: "success"})
			}
			if err := so.SaveSelectorStats(path); err != nil {
				t.Errorf("concurrent SaveSelectorStats: %v", err)
			}
		})
	}
	wg.Wait()

	final := NewSelectorOptimizer(OrderBySuccessRate)
	if err := final.LoadSelectorStats(path); err != nil {
		t.Fatalf("final LoadSelectorStats: %v", err)
	}
	rs := final.Stats["Router"]
	if rs == nil || rs.Children["FastPath"] == nil {
		t.Fatalf("FastPath stats missing after concurrent merge")
	}
	if got, want := rs.Children["FastPath"].Successes, writers*perWriter; got != want {
		t.Errorf("concurrent-merge successes: want %d, got %d", want, got)
	}
}

// A long-lived optimizer that Saves more than once must not double-count:
// Save merges only the UNSAVED delta onto disk, so repeated saves are
// idempotent. The merge-the-whole-in-memory-state behavior re-added what was
// already on disk on every save (5 → 10 → 20 …), silently corrupting the
// telemetry any periodic saver produced.
func TestSelectorOptimizer_RepeatedSaveIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selector_stats.json")

	so := NewSelectorOptimizer(OrderBySuccessRate)
	for range 5 {
		so.Record("Router", NodeExecutionRecord{NodeName: "FastPath", Outcome: "success"})
	}
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("second save (no new records): %v", err)
	}

	reloaded := NewSelectorOptimizer(OrderBySuccessRate)
	if err := reloaded.LoadSelectorStats(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := reloaded.Stats["Router"].Children["FastPath"].Successes; got != 5 {
		t.Fatalf("successes after double save = %d, want 5 (no double-count)", got)
	}

	// Records added after a save contribute exactly once more.
	so.Record("Router", NodeExecutionRecord{NodeName: "FastPath", Outcome: "success"})
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("third save: %v", err)
	}
	again := NewSelectorOptimizer(OrderBySuccessRate)
	if err := again.LoadSelectorStats(path); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := again.Stats["Router"].Children["FastPath"].Successes; got != 6 {
		t.Fatalf("successes after post-save record = %d, want 6", got)
	}
}
