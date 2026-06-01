package evolution

import (
	"math/rand"
	"strings"
	"testing"
)

func TestQTableStateBucketsAndDepth(t *testing.T) {
	qt := NewQTable()

	low := &SerializableNode{Type: "Sequence", Name: "Root", Children: []SerializableNode{
		{Type: "Action", Name: "A"},
	}}
	if got := qt.GetState(low, "core"); got != "core:low:1" {
		t.Fatalf("low bucket state = %q", got)
	}

	med := &SerializableNode{Type: "Sequence", Name: "Root"}
	for i := 0; i < 21; i++ {
		med.Children = append(med.Children, SerializableNode{Type: "Action", Name: "A"})
	}
	if got := qt.GetState(med, "core"); !strings.HasPrefix(got, "core:med:") {
		t.Fatalf("medium bucket state = %q", got)
	}

	high := &SerializableNode{Type: "Sequence", Name: "Root"}
	for i := 0; i < 36; i++ {
		high.Children = append(high.Children, SerializableNode{Type: "Action", Name: "A"})
	}
	if got := qt.GetState(high, "core"); !strings.HasPrefix(got, "core:high:") {
		t.Fatalf("high bucket state = %q", got)
	}
}

func TestQTableSelectUpdateBestAction(t *testing.T) {
	qt := NewQTable()
	state := "core:low:1"

	qt.Update(state, "add_before", 10, 0.5)
	qt.Update(state, "replace_node", 4, 0.5)
	qt.Update(state, "add_before", 2, 0.5)

	if got := qt.BestAction(state); got != "add_before" {
		t.Fatalf("best action = %q", got)
	}
	if got := qt.SelectAction(state, 0); got != "add_before" {
		t.Fatalf("greedy selected action = %q", got)
	}

	allowed := map[string]bool{"add_before": true, "add_after": true, "add_fallback": true, "replace_node": true, "remove_node": true}
	rand.Seed(1)
	if got := qt.SelectAction("unknown", 1); !allowed[got] {
		t.Fatalf("exploration returned unexpected mutation %q", got)
	}
	if got := qt.BestAction("missing"); got != "" {
		t.Fatalf("missing state best action = %q", got)
	}
}

func TestReinforcementLearnerLearnAndSuggest(t *testing.T) {
	rl := NewReinforcementLearner()
	rl.Epsilon = 0 // force exploitation for deterministic suggestion
	tree := &SerializableNode{Type: "Sequence", Name: "Root", Children: []SerializableNode{
		{Type: "Action", Name: "A"},
	}}

	rl.Learn(tree, "godev", "wrap_retry", 0.2, 0.9)
	state := rl.QTable.GetState(tree, "godev")
	if got := rl.QTable.Values[state]["wrap_retry"]; got <= 0 {
		t.Fatalf("expected positive Q value after improvement, got %.3f", got)
	}
	if got := rl.Suggest(tree, "godev"); got != "wrap_retry" {
		t.Fatalf("suggested action = %q", got)
	}
}

func TestPopulationDerivedMetrics(t *testing.T) {
	pop := &Population{
		Generation:     4,
		BestFitness:    12,
		TotalMutations: 8,
		Regressions:    2,
		Individuals: []Individual{
			{Genome: "aaaaaaaa11111111"},
			{Genome: "bbbbbbbb22222222"},
			{Genome: "bbbbbbbb33333333"},
		},
	}

	if got := pop.ConvergenceRate(); got != 3 {
		t.Fatalf("convergence rate = %.3f", got)
	}
	if got := pop.RegressionRate(); got != 25 {
		t.Fatalf("regression rate = %.3f", got)
	}
	if got := pop.NicheDiversity(); got <= 0 || got > 1 {
		t.Fatalf("niche diversity out of range: %.3f", got)
	}

	empty := &Population{}
	if got := empty.ConvergenceRate(); got != 0 {
		t.Fatalf("empty convergence rate = %.3f", got)
	}
	if got := empty.RegressionRate(); got != 0 {
		t.Fatalf("empty regression rate = %.3f", got)
	}
}

func TestLocalSearchMutableParamsAndTabu(t *testing.T) {
	tree := &SerializableNode{Type: "Sequence", Name: "Root", Metadata: map[string]any{
		"timeout_ms": int64(1000),
	}, Children: []SerializableNode{{Type: "Condition", Name: "Check", Metadata: map[string]any{
		"threshold": float32(0.8),
	}}}}

	params := extractMutableParams(tree)
	if len(params) != 2 {
		t.Fatalf("mutable params = %d", len(params))
	}
	params[0].setValue(2000)
	if got := getFloatMeta(params[0].node, "timeout_ms"); got != 2000 {
		t.Fatalf("timeout after setter = %.1f", got)
	}

	if _, ok := toFloat64("not-number"); ok {
		t.Fatal("string unexpectedly converted to float64")
	}
	ls := NewLocalSearcher(TabuSearch)
	if !ls.isTabu("abc", []tabuEntry{{genome: "abc", ttl: 2}}) {
		t.Fatal("expected genome to be tabu")
	}
	if ls.isTabu("def", []tabuEntry{{genome: "abc", ttl: 2}}) {
		t.Fatal("unexpected tabu match")
	}
}
