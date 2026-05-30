package evaluator

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// testTree builds a simple tree for testing.
func testTree(name string, conditions, actions int) *evolution.SerializableNode {
	root := &evolution.SerializableNode{Type: "Selector", Name: name}
	for i := 0; i < conditions; i++ {
		root.Children = append(root.Children, evolution.SerializableNode{
			Type: "Condition",
			Name: name + "_cond_" + itoaTest(i),
		})
	}
	for i := 0; i < actions; i++ {
		root.Children = append(root.Children, evolution.SerializableNode{
			Type: "Action",
			Name: name + "_action_" + itoaTest(i),
		})
	}
	return root
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestStructuralQuickEval(t *testing.T) {
	tests := []struct {
		name       string
		tree       *evolution.SerializableNode
		wantMin    float64
		wantMax    float64
	}{
		{"nil tree", nil, 0, 0},
		{"optimal tree", buildOptimalTree(), 60, 100},
		{"too small", testTree("small", 1, 1), 10, 60},
		{"too large", buildDeepTree(10, 50), 5, 60},
		{"conditions only", testTree("conds", 8, 0), 30, 80},
		{"actions only", testTree("acts", 0, 8), 30, 80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StructuralQuickEval(tt.tree)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("StructuralQuickEval() = %.1f, want in [%.1f, %.1f]", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

// buildOptimalTree creates a tree with ~25 nodes, depth 4, good condition/action ratio.
func buildOptimalTree() *evolution.SerializableNode {
	root := &evolution.SerializableNode{Type: "Selector", Name: "optimal"}
	// Add a Sequence with conditions + actions
	seq := &evolution.SerializableNode{Type: "Sequence", Name: "main_path"}
	for i := 0; i < 5; i++ {
		seq.Children = append(seq.Children, evolution.SerializableNode{Type: "Condition", Name: "cond_" + itoaTest(i)})
	}
	for i := 0; i < 8; i++ {
		seq.Children = append(seq.Children, evolution.SerializableNode{Type: "Action", Name: "action_" + itoaTest(i)})
	}
	root.Children = append(root.Children, *seq)
	// Add a fallback sequence for depth
	fallback := &evolution.SerializableNode{Type: "Sequence", Name: "fallback_path"}
	fallback.Children = append(fallback.Children, evolution.SerializableNode{Type: "Action", Name: "fb_action_1"})
	inner := &evolution.SerializableNode{Type: "Sequence", Name: "inner"}
	inner.Children = append(inner.Children, evolution.SerializableNode{Type: "Action", Name: "inner_action"})
	fallback.Children = append(fallback.Children, *inner)
	root.Children = append(root.Children, *fallback)
	return root
}

func buildDeepTree(depth, width int) *evolution.SerializableNode {
	root := &evolution.SerializableNode{Type: "Selector", Name: "deep_root"}
	current := root
	for d := 1; d < depth; d++ {
		child := &evolution.SerializableNode{Type: "Sequence", Name: "deep_" + itoaTest(d)}
		for w := 0; w < width; w++ {
			child.Children = append(child.Children, evolution.SerializableNode{
				Type: "Action", Name: "leaf_" + itoaTest(d) + "_" + itoaTest(w),
			})
		}
		current.Children = append(current.Children, *child)
		if len(current.Children) > 0 {
			current = &current.Children[len(current.Children)-1]
		}
	}
	return root
}

func TestCascadeEvaluator_BasicFlow(t *testing.T) {
	cfg := DefaultCascadeConfig()
	quickFn := func(tree *evolution.SerializableNode) float64 {
		return StructuralQuickEval(tree)
	}
	benchFn := func(tree *evolution.SerializableNode) float64 {
		// Mock: return score based on tree name
		if tree.Name == "bad_tree" {
			return 30
		}
		return 70
	}
	fullFn := func(tree *evolution.SerializableNode) float64 {
		return 85
	}

	ce := NewCascadeEvaluator(cfg, quickFn, benchFn, fullFn)

	individuals := []evolution.Individual{
		{Tree: testTree("good_tree", 5, 8)},
		{Tree: testTree("bad_tree", 1, 1)},
		{Tree: testTree("medium_tree", 3, 4)},
	}

	results := ce.EvaluatePopulation(individuals)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// good_tree should pass all tiers
	if results[0].Rejected {
		t.Errorf("good_tree should not be rejected, got: %s", results[0].RejectReason)
	}
	if results[0].Level != LevelFull {
		t.Errorf("good_tree should reach Full, got %s", results[0].Level)
	}

	// bad_tree should be rejected at Bench (score 30 < threshold 50)
	badFound := false
	for _, r := range results {
		if r.Tree.Name == "bad_tree" {
			badFound = true
			if !r.Rejected {
				t.Error("bad_tree should be rejected (Bench score 30 < 50)")
			}
		}
	}
	if !badFound {
		t.Error("bad_tree not found in results")
	}
}

func TestCascadeEvaluator_QuickFilter(t *testing.T) {
	cfg := DefaultCascadeConfig()
	quickFn := func(tree *evolution.SerializableNode) float64 {
		return StructuralQuickEval(tree)
	}
	benchFn := func(tree *evolution.SerializableNode) float64 {
		return 0 // should never be called for rejected
	}
	fullFn := func(tree *evolution.SerializableNode) float64 {
		return 0
	}

	ce := NewCascadeEvaluator(cfg, quickFn, benchFn, fullFn)

	// Nil tree should score 0 → rejected at Quick
	individuals := []evolution.Individual{
		{Tree: nil},
	}

	results := ce.EvaluatePopulation(individuals)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Rejected {
		t.Error("nil tree should be rejected at Quick")
	}
	if results[0].Level != LevelQuick {
		t.Errorf("expected LevelQuick, got %s", results[0].Level)
	}
}

func TestCascadeEvaluator_CapacityLimits(t *testing.T) {
	// Force low capacities to test overflow rejection
	cfg := CascadeConfig{
		QuickThreshold:     10,
		BenchThreshold:     10,
		MaxBenchCandidates: 2,
		MaxFullCandidates:  1,
	}

	quickFn := func(tree *evolution.SerializableNode) float64 {
		return 50 // everyone passes Quick
	}
	benchFn := func(tree *evolution.SerializableNode) float64 {
		return 60 // everyone passes Bench
	}
	fullFn := func(tree *evolution.SerializableNode) float64 {
		return 90
	}

	ce := NewCascadeEvaluator(cfg, quickFn, benchFn, fullFn)

	individuals := []evolution.Individual{
		{Tree: testTree("a", 4, 6)},
		{Tree: testTree("b", 4, 6)},
		{Tree: testTree("c", 4, 6)},
		{Tree: testTree("d", 4, 6)},
		{Tree: testTree("e", 4, 6)},
	}

	results := ce.EvaluatePopulation(individuals)

	passed := 0
	rejected := 0
	for _, r := range results {
		if r.Rejected {
			rejected++
		} else {
			passed++
		}
	}

	if passed != 1 {
		t.Errorf("expected 1 to pass (MaxFullCandidates=1), got %d", passed)
	}
	if rejected != 4 {
		t.Errorf("expected 4 rejected, got %d", rejected)
	}
}

func TestCascadeResult_BestScore(t *testing.T) {
	tests := []struct {
		name string
		cr   CascadeResult
		want float64
	}{
		{"full available", CascadeResult{FullScore: 85, BenchScore: 70, QuickScore: 50, Level: LevelFull}, 85},
		{"bench only", CascadeResult{BenchScore: 70, QuickScore: 50, Level: LevelBench}, 70},
		{"quick only", CascadeResult{QuickScore: 50, Level: LevelQuick}, 50},
		{"rejected uses quick", CascadeResult{QuickScore: 20, Level: LevelQuick, Rejected: true}, 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cr.BestScore()
			if got != tt.want {
				t.Errorf("BestScore() = %.1f, want %.1f", got, tt.want)
			}
		})
	}
}

func TestCascadeStats(t *testing.T) {
	cs := CascadeStats{
		Total:       100,
		PassedQuick: 80,
		PassedBench: 30,
		PassedFull:  5,
	}

	if cs.FilterRate() != 75 {
		t.Errorf("FilterRate() = %d%%, want 75%%", int(cs.FilterRate()))
	}

	summary := cs.Summary()
	if summary == "" {
		t.Error("Summary() should not be empty")
	}
}

func TestCountConditionsActions(t *testing.T) {
	tree := testTree("test", 5, 8)
	conds, acts := countConditionsActions(tree)
	if conds != 5 {
		t.Errorf("conditions = %d, want 5", conds)
	}
	if acts != 8 {
		t.Errorf("actions = %d, want 8", acts)
	}
}

func TestMaxTreeDepthEval(t *testing.T) {
	tests := []struct {
		name  string
		tree  *evolution.SerializableNode
		want  int
	}{
		{"nil", nil, 0},
		{"single node", &evolution.SerializableNode{Type: "Selector"}, 0},
		{"depth 2", &evolution.SerializableNode{
			Type: "Selector",
			Children: []evolution.SerializableNode{
				{Type: "Action"},
			},
		}, 1},
		{"depth 3", buildDeepTree(3, 1), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maxTreeDepthEval(tt.tree, 0)
			if got != tt.want {
				t.Errorf("maxTreeDepthEval() = %d, want %d", got, tt.want)
			}
		})
	}
}
