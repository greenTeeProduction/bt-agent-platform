package evolution

import (
	"testing"
)

func TestNewPopulation_Evolution(t *testing.T) {
	base := DefaultTree()
	pop := NewPopulation(10, base)
	if pop == nil {
		t.Fatal("population nil")
	}
	pop.Evaluate(func(tree *SerializableNode) float64 {
		return float64(len(tree.Children)) * 10.0
	})
	selected := pop.Select()
	if len(selected) == 0 {
		t.Error("select returned empty")
	}
}

func TestPopulation_Evolve(t *testing.T) {
	base := DefaultTree()
	pop := NewPopulation(10, base)
	result := pop.Evolve(2, func(tree *SerializableNode) float64 {
		return float64(countTreeNodes(tree))
	})
	if result == nil {
		t.Error("evolve returned nil")
	}
}

func TestPopulation_Diversity(t *testing.T) {
	base := DefaultTree()
	pop := NewPopulation(20, base)
	d := pop.Diversity()
	if d < 0 || d > 1 {
		t.Errorf("diversity out of range: %.2f", d)
	}
}

func TestCrossover_Single(t *testing.T) {
	a := DefaultTree()
	b := GoDeveloperTree()
	child := Crossover(a, b)
	if child == nil {
		t.Error("crossover returned nil")
	}
}

func TestNewQTable(t *testing.T) {
	qt := NewQTable()
	if qt == nil {
		t.Error("qtable nil")
	}
}

func TestNewReinforcementLearner(t *testing.T) {
	rl := NewReinforcementLearner()
	if rl == nil {
		t.Error("reinforcement learner nil")
	}
}

func TestEntropy(t *testing.T) {
	e := Entropy(0.5, 0.5)
	if e <= 0 {
		t.Error("entropy of 0.5/0.5 should be > 0")
	}
	e2 := Entropy(0.99, 0.01)
	if e2 >= e {
		t.Error("entropy of 0.99/0.01 should be lower than 0.5/0.5")
	}
}

func TestSelectorOptimizer_New(t *testing.T) {
	so := NewSelectorOptimizer(OrderByGini)
	if so == nil {
		t.Error("selector optimizer nil")
	}
}

func TestLocalSearcher_New(t *testing.T) {
	ls := NewLocalSearcher(HillClimbSearch)
	if ls == nil {
		t.Error("local searcher nil")
	}
}

func TestDTAnalyzer_New(t *testing.T) {
	da := NewDTAnalyzer()
	if da == nil {
		t.Error("dt analyzer nil")
	}
}

func TestBTOptimizer_New(t *testing.T) {
	bo := NewBTOptimizer()
	if bo == nil {
		t.Error("bt optimizer nil")
	}
}

func TestExpertKnowledge_New(t *testing.T) {
	ek := NewExpertKnowledge()
	if ek == nil {
		t.Error("expert knowledge nil")
	}
}

func TestExpertKnowledge_RecommendMutations(t *testing.T) {
	ek := NewExpertKnowledge()
	tree := DefaultTree()
	recs := ek.RecommendMutations(tree)
	if recs == nil {
		t.Error("recommendations nil")
	}
}

func TestExpertKnowledge_DetectAntiPatterns(t *testing.T) {
	ek := NewExpertKnowledge()
	tree := DefaultTree()
	ap := ek.DetectAntiPatterns(tree)
	if ap == nil {
		t.Error("anti-patterns nil")
	}
}

func TestExpertKnowledge_ValidateArchetype(t *testing.T) {
	ek := NewExpertKnowledge()
	tree := DefaultTree()
	fits, issues := ek.ValidateArchetype(tree, "core")
	_ = fits
	_ = issues
	// May or may not fit — just verify no panic
}

func countTreeNodes(node *SerializableNode) int {
	n := 1
	for i := range node.Children {
		n += countTreeNodes(&node.Children[i])
	}
	return n
}
