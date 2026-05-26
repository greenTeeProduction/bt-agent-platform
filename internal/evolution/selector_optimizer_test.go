package evolution

import (
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
	for i := 0; i < 10; i++ {
		so.Record("Router", NodeExecutionRecord{NodeName: "PureChoice", Outcome: "success"})
	}
	// Child B: mixed → Gini > 0
	for i := 0; i < 5; i++ {
		so.Record("Router", NodeExecutionRecord{NodeName: "MixedChoice", Outcome: "success"})
	}
	for i := 0; i < 5; i++ {
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
	so.Record("Router", NodeExecutionRecord{NodeName: "Alpha", Outcome: "success"})   // tick 0
	so.Record("Router", NodeExecutionRecord{NodeName: "Beta", Outcome: "failure"})     // tick 1
	so.Record("Router", NodeExecutionRecord{NodeName: "Alpha", Outcome: "failure"})    // tick 2
	so.Record("Router", NodeExecutionRecord{NodeName: "Beta", Outcome: "success"})     // tick 3 ← last success

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
			"threshold":   0.5,
			"timeout_ms":  1000.0,
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
	for i := 0; i < 10; i++ {
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
