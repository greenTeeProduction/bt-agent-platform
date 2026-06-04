package evolution

import (
	"strconv"
	"testing"
)

func TestMultiFitness_Dominates(t *testing.T) {
	a := NewMultiFitness()
	a.Set(DimSuccessRate, 80)
	a.Set(DimPathCoverage, 70)

	b := NewMultiFitness()
	b.Set(DimSuccessRate, 70)
	b.Set(DimPathCoverage, 60)

	// a dominates b (better on both)
	if !a.Dominates(b) {
		t.Error("a should dominate b (better on all dims)")
	}
	// b does NOT dominate a
	if b.Dominates(a) {
		t.Error("b should NOT dominate a")
	}

	// Partial dominance: a is better on one dim, worse on another → neither dominates
	c := NewMultiFitness()
	c.Set(DimSuccessRate, 90)
	c.Set(DimPathCoverage, 50)

	if a.Dominates(c) {
		t.Error("a should NOT dominate c (a is worse on success_rate)")
	}
	if c.Dominates(a) {
		t.Error("c should NOT dominate a (c is worse on path_coverage)")
	}
}

func TestMultiFitness_CompositeScore(t *testing.T) {
	mf := NewMultiFitness()
	mf.Set(DimSuccessRate, 80)
	mf.Set(DimPathCoverage, 60)

	// Default weights (1.0 each): (80 + 60) / 2 = 70
	cs := mf.CompositeScore(nil)
	if cs != 70 {
		t.Errorf("composite = %.1f, want 70", cs)
	}

	// Custom weights: success_rate=2, path_coverage=1
	weights := map[FitnessDimension]float64{DimSuccessRate: 2, DimPathCoverage: 1}
	cs = mf.CompositeScore(weights)
	if cs < 73 || cs > 74 {
		t.Errorf("weighted composite = %.1f, want ~73.3", cs)
	}
}

func TestParetoFront_Add(t *testing.T) {
	pf := NewParetoFront([]FitnessDimension{DimSuccessRate, DimPathCoverage})

	tree1 := makeTestTree("t1", 2, 3)
	ind1 := &Individual{Tree: tree1, Fitness: 70, Genome: hashTree(tree1)}
	fv1 := NewMultiFitness()
	fv1.Set(DimSuccessRate, 80)
	fv1.Set(DimPathCoverage, 70)

	// First individual always added
	if !pf.Add(&MultiIndividual{Individual: ind1, FitnessVec: fv1}) {
		t.Error("first individual should be added")
	}
	if pf.Size() != 1 {
		t.Errorf("front size = %d, want 1", pf.Size())
	}

	// Second individual that's dominated → rejected
	tree2 := makeTestTree("t2", 2, 3)
	ind2 := &Individual{Tree: tree2, Fitness: 50, Genome: hashTree(tree2)}
	fv2 := NewMultiFitness()
	fv2.Set(DimSuccessRate, 60)
	fv2.Set(DimPathCoverage, 50)

	if pf.Add(&MultiIndividual{Individual: ind2, FitnessVec: fv2}) {
		t.Error("dominated individual should be rejected")
	}
	if pf.Size() != 1 {
		t.Errorf("front size = %d, want 1 (dominated rejected)", pf.Size())
	}

	// Third individual that's non-dominated → added
	tree3 := makeTestTree("t3", 4, 5)
	ind3 := &Individual{Tree: tree3, Fitness: 75, Genome: hashTree(tree3)}
	fv3 := NewMultiFitness()
	fv3.Set(DimSuccessRate, 70)
	fv3.Set(DimPathCoverage, 90) // better on path_coverage, worse on success_rate

	if !pf.Add(&MultiIndividual{Individual: ind3, FitnessVec: fv3}) {
		t.Error("non-dominated individual should be added")
	}
	if pf.Size() != 2 {
		t.Errorf("front size = %d, want 2", pf.Size())
	}
}

func TestParetoFront_Best(t *testing.T) {
	pf := NewParetoFront([]FitnessDimension{DimSuccessRate, DimPathCoverage})

	// Add non-dominated individuals (different trade-offs)
	for i := 0; i < 5; i++ {
		tree := makeTestTree("t"+strconv.Itoa(i), 2, 3)
		ind := &Individual{Tree: tree, Fitness: 50, Genome: hashTree(tree)}
		fv := NewMultiFitness()
		// Trade-off: higher success rate = lower path coverage
		fv.Set(DimSuccessRate, float64(20+i*15))
		fv.Set(DimPathCoverage, float64(90-i*15))
		pf.Add(&MultiIndividual{Individual: ind, FitnessVec: fv})
	}

	if pf.Size() != 5 {
		t.Errorf("front size = %d, want 5 (all non-dominated with trade-offs)", pf.Size())
	}

	best := pf.Best(2)
	if len(best) != 2 {
		t.Errorf("Best(2) = %d, want 2", len(best))
	}
	if best[0].Fitness < best[1].Fitness {
		t.Error("Best should be sorted by composite descending")
	}
}

func TestParetoFront_DiversityScore(t *testing.T) {
	pf := NewParetoFront([]FitnessDimension{DimSuccessRate, DimPathCoverage})

	// Empty front
	if pf.DiversityScore() != 0 {
		t.Error("empty front diversity should be 0")
	}

	// Add two diverse individuals
	for i := 0; i < 2; i++ {
		tree := makeTestTree("t"+strconv.Itoa(i), 2, 3)
		ind := &Individual{Tree: tree, Fitness: 50, Genome: hashTree(tree)}
		fv := NewMultiFitness()
		fv.Set(DimSuccessRate, float64(20+i*60))
		fv.Set(DimPathCoverage, float64(80-i*60))
		pf.Add(&MultiIndividual{Individual: ind, FitnessVec: fv})
	}

	div := pf.DiversityScore()
	if div <= 0 {
		t.Errorf("diverse front should have diversity > 0, got %.3f", div)
	}
}

func TestStructuralMultiFitness(t *testing.T) {
	tree := makeOptimalParetoTree()
	mf := StructuralMultiFitness(tree)

	if mf.Get(DimSuccessRate) <= 0 {
		t.Error("success rate should be > 0 for optimal tree")
	}
	if mf.Get(DimPathCoverage) <= 0 {
		t.Error("path coverage should be > 0")
	}
	if mf.Get(DimStability) <= 0 {
		t.Error("stability should be > 0")
	}
	if mf.Get(DimNodeEfficiency) <= 0 {
		t.Error("node efficiency should be > 0")
	}

	// Nil tree
	nilMf := StructuralMultiFitness(nil)
	if nilMf.Get(DimSuccessRate) != 0 {
		t.Error("nil tree should have 0 on all dimensions")
	}
}

func makeOptimalParetoTree() *SerializableNode {
	root := &SerializableNode{Type: "Selector", Name: "pareto_opt"}
	for i := 0; i < 5; i++ {
		root.Children = append(root.Children, SerializableNode{Type: "Condition", Name: "cond_" + strconv.Itoa(i)})
	}
	for i := 0; i < 8; i++ {
		root.Children = append(root.Children, SerializableNode{Type: "Action", Name: "act_" + strconv.Itoa(i)})
	}
	// Add depth
	seq := &SerializableNode{Type: "Sequence", Name: "deep"}
	seq.Children = append(seq.Children, SerializableNode{Type: "Action", Name: "deep_act"})
	root.Children = append(root.Children, *seq)
	return root
}

func TestParetoPopulation_BasicFlow(t *testing.T) {
	baseTree := makeOptimalParetoTree()
	pp := NewParetoPopulation(10, baseTree, []FitnessDimension{
		DimSuccessRate, DimPathCoverage, DimStability, DimNodeEfficiency, DimExecutionSpeed,
	})

	fitnessFn := StructuralMultiFitness

	pp.Evaluate(fitnessFn)

	if pp.Front.Size() == 0 {
		t.Error("Pareto front should have entries after evaluation")
	}

	parents := pp.SelectPareto()
	if len(parents) != 2 {
		t.Errorf("SelectPareto() = %d, want 2", len(parents))
	}

	stats := pp.Front.Stats()
	if stats.FrontSize == 0 {
		t.Error("stats should show front size > 0")
	}
}

func TestParetoPopulation_EvolvePareto(t *testing.T) {
	baseTree := makeOptimalParetoTree()
	pp := NewParetoPopulation(8, baseTree, []FitnessDimension{
		DimSuccessRate, DimPathCoverage, DimStability,
	})

	fitnessFn := StructuralMultiFitness

	result := pp.EvolvePareto(3, fitnessFn)
	if result == nil {
		t.Error("EvolvePareto returned nil tree")
	}
	if pp.Front.Size() == 0 {
		t.Error("Pareto front should have entries after evolution")
	}
}

func TestMultiFitness_String(t *testing.T) {
	mf := NewMultiFitness()
	mf.Set(DimSuccessRate, 75)
	mf.Set(DimPathCoverage, 60)

	s := mf.String()
	if s == "" {
		t.Error("String should not be empty")
	}
	// Should contain both dimensions
	if len(mf.Scores) != 2 {
		t.Errorf("scores = %d, want 2", len(mf.Scores))
	}
}
