package evolution

import (
	"math"
	"testing"
)

func TestNewNSGAIIPopulation(t *testing.T) {
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}
	nsga2 := NewNSGAIIPopulation(10, baseTree, []FitnessDimension{DimSuccessRate, DimStability})
	if nsga2 == nil {
		t.Fatal("expected non-nil NSGAIIPopulation")
	}
	if len(nsga2.Individuals) != 10 {
		t.Errorf("expected 10 individuals, got %d", len(nsga2.Individuals))
	}
	if len(nsga2.Dimensions) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(nsga2.Dimensions))
	}
}

func TestNSGAII_FastNonDominatedSort(t *testing.T) {
	// Three individuals:
	// A (90, 80) — dominates B (A >= B in all dims)
	// B (50, 40) — dominated by A
	// C (85, 70) — incomparable with A (A better SR, C better Stab)
	vecs := []MultiFitness{
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 90, DimStability: 80}}, // A
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 50, DimStability: 40}}, // B
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 85, DimStability: 95}}, // C: better stability than A
	}

	sorter := NewNSGAIISorter([]FitnessDimension{DimSuccessRate, DimStability})
	fronts := sorter.fastNonDominatedSort(vecs)

	if len(fronts) == 0 {
		t.Fatal("expected at least 1 front")
	}

	t.Logf("Fronts: %+v", fronts)

	// Front 0 should contain A and C (neither dominates the other: A has better SR, C has better Stab)
	if len(fronts[0].Indices) != 2 {
		t.Errorf("expected 2 in front 0, got %d: %v", len(fronts[0].Indices), fronts[0].Indices)
	}

	// Front 1 should contain B (dominated by A)
	if len(fronts) < 2 {
		t.Fatal("expected at least 2 fronts")
	}
	if len(fronts[1].Indices) != 1 {
		t.Errorf("expected 1 in front 1, got %d: %v", len(fronts[1].Indices), fronts[1].Indices)
	}
}

func TestNSGAII_CrowdingDistance(t *testing.T) {
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}
	nsga2 := NewNSGAIIPopulation(5, baseTree, []FitnessDimension{DimSuccessRate, DimStability})

	// Set up fitness vectors with known spread
	nsga2.FitnessVecs = []MultiFitness{
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 100, DimStability: 0}},
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 75, DimStability: 25}},
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 50, DimStability: 50}},
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 25, DimStability: 75}},
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 0, DimStability: 100}},
	}

	indices := []int{0, 1, 2, 3, 4}
	nsga2.assignCrowdingDistance(indices)

	// Check boundary points have infinite distance
	if !math.IsInf(nsga2.CrowdingDist[0], 1) {
		t.Errorf("expected index 0 to have infinite crowding distance")
	}
	if !math.IsInf(nsga2.CrowdingDist[4], 1) {
		t.Errorf("expected index 4 to have infinite crowding distance")
	}

	// Check interior points have finite positive distance
	for _, idx := range []int{1, 2, 3} {
		if nsga2.CrowdingDist[idx] <= 0 && !math.IsInf(nsga2.CrowdingDist[idx], 1) {
			t.Errorf("expected index %d to have positive crowding distance, got %f", idx, nsga2.CrowdingDist[idx])
		}
	}

	t.Logf("Crowding distances: %v", nsga2.CrowdingDist)
}

func TestNSGAII_Dominates(t *testing.T) {
	// A dominates B if A is at least as good in ALL dimensions
	// and strictly better in at least one
	a := MultiFitness{Scores: map[FitnessDimension]float64{DimSuccessRate: 90, DimStability: 80}}
	b := MultiFitness{Scores: map[FitnessDimension]float64{DimSuccessRate: 50, DimStability: 40}}
	c := MultiFitness{Scores: map[FitnessDimension]float64{DimSuccessRate: 90, DimStability: 85}}
	d := MultiFitness{Scores: map[FitnessDimension]float64{DimSuccessRate: 100, DimStability: 80}}

	// A dominates B (90>50, 80>40)
	if !a.Dominates(b) {
		t.Error("expected A to dominate B")
	}
	// A does NOT dominate C (A.SR == C.SR, but A.Stab < C.Stab)
	if a.Dominates(c) {
		t.Error("expected A NOT to dominate C (C has higher stability)")
	}
	// C dominates A (C.SR >= A.SR, C.Stab > A.Stab)
	if !c.Dominates(a) {
		t.Error("expected C to dominate A (C has same SR, higher stability)")
	}
	// D dominates A (D.SR > A.SR, D.Stab >= A.Stab)
	if !d.Dominates(a) {
		t.Error("expected D to dominate A (D has higher SR, same stability)")
	}
	// A does NOT dominate D (A.SR < D.SR)
	if a.Dominates(d) {
		t.Error("expected A NOT to dominate D")
	}
}

func TestNSGAII_FrontRank(t *testing.T) {
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}
	nsga2 := NewNSGAIIPopulation(3, baseTree, []FitnessDimension{DimSuccessRate, DimStability})

	// A (90, 80) dominates B (50, 40)
	// C (85, 95) is incomparable with A (A better SR, C better Stab)
	// C dominates B
	nsga2.FitnessVecs = []MultiFitness{
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 90, DimStability: 80}}, // 0: A
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 50, DimStability: 40}}, // 1: B
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 85, DimStability: 95}}, // 2: C
	}

	// Run non-dominated sort
	nsga2.Fronts = nsga2.fastNonDominatedSort(nsga2.FitnessVecs)

	// A (0) and C (2) should be front 0 (neither dominates the other)
	// B (1) should be front 1 (dominated by both A and C)
	rank0 := nsga2.frontRank(0)
	rankC := nsga2.frontRank(2)
	rankB := nsga2.frontRank(1)

	if rank0 != 0 {
		t.Errorf("expected index 0 (A) rank 0, got %d", rank0)
	}
	if rankC != 0 {
		t.Errorf("expected index 2 (C) rank 0, got %d", rankC)
	}
	if rankB != 1 {
		t.Errorf("expected index 1 (B) rank 1, got %d", rankB)
	}
}

func TestNSGAII_CrowdedComparison(t *testing.T) {
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}
	nsga2 := NewNSGAIIPopulation(3, baseTree, []FitnessDimension{DimSuccessRate, DimStability})

	// Set up: A and C are front 0 (incomparable), B is front 1 (dominated)
	nsga2.FitnessVecs = []MultiFitness{
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 90, DimStability: 80}}, // 0: A
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 30, DimStability: 20}}, // 1: B
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 85, DimStability: 90}}, // 2: C
	}
	nsga2.Fronts = nsga2.fastNonDominatedSort(nsga2.FitnessVecs)
	nsga2.CrowdingDist = make([]float64, 3)

	// Assign crowding distances for front 0
	nsga2.assignCrowdingDistance(nsga2.Fronts[0].Indices)

	// Index 0 (front 0) should be "better" than index 1 (front 1)
	if !nsga2.crowdedComparison(0, 1) {
		t.Error("expected index 0 (front 0) to be better than index 1 (front 1)")
	}
	if nsga2.crowdedComparison(1, 0) {
		t.Error("expected index 1 (front 1) not to be better than index 0 (front 0)")
	}
}

// Test that NSGAII_Evolve doesn't crash and returns a tree
func TestNSGAII_Evolve_Basic(t *testing.T) {
	baseTree := &SerializableNode{
		Name: "root",
		Type: "Selector",
		Children: []SerializableNode{
			{Name: "child", Type: "Action"},
		},
	}

	nsga2 := NewNSGAIIPopulation(6, baseTree, []FitnessDimension{DimSuccessRate, DimStability})

	fitnessFn := func(node *SerializableNode) MultiFitness {
		return StructuralMultiFitness(node)
	}

	result := nsga2.Evolve(3, fitnessFn)
	if result == nil {
		t.Fatal("expected non-nil result from Evolve")
	}
	if result.Name == "" {
		t.Error("expected result tree to have a name")
	}
	t.Logf("NSGA-II evolve result: %s (fitness=%.2f)", result.Name, nsga2.BestFitness)
}
