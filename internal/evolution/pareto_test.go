package evolution

import (
	"math/rand"
	"path/filepath"
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

// growthMultiFitness wraps growthFitness (experience_bank_test.go) — monotone
// in node count and bounded in (0,1) — as a single-dimension MultiFitness so
// any mutation that adds nodes strictly improves the composite score. Used to
// exercise the ExpertKnowledge.Observe wiring in EvolvePareto and NSGA-II's
// Evolve, mirroring how growthFitness drives
// TestExpertKnowledge_ObservesLearnedPatternFromQLearning (learning_test.go).
func growthMultiFitness(tr *SerializableNode) MultiFitness {
	mf := NewMultiFitness()
	mf.Set(DimNodeEfficiency, growthFitness(tr)*100)
	return mf
}

// TestParetoPopulation_EvolvePareto_ObservesLearnedPatternViaExpertKnowledge
// pins milestone 2/3 of the Q2 Evolvability "feed ExpertKnowledge's
// learned-pattern archive back into mutation recommendations and every
// production evolution algorithm" program: EvolvePareto's mutation-application
// step must call a caller-owned *ExpertKnowledge.Observe(action, category,
// gain) whenever a mutation genuinely improves fitness, mirroring the wiring
// EvolveQLearning/qLearnMutate already have (learning.go:845-921). The
// ExpertKnowledge field on ParetoPopulation does not exist yet, so this test
// fails to compile until milestone 2 lands.
func TestParetoPopulation_EvolvePareto_ObservesLearnedPatternViaExpertKnowledge(t *testing.T) {
	ek := NewExpertKnowledge()
	before := len(ek.LearnedPatterns)

	// A handful of fixed seeds keeps the run deterministic while tolerating
	// exactly which mutation op the random draw picks first, mirroring
	// TestExpertKnowledge_ObservesLearnedPatternFromQLearning.
	for _, seed := range []int64{42, 43, 44} {
		rand.Seed(seed) //nolint:staticcheck // deterministic evolution run for reproducibility
		pp := NewParetoPopulation(8, DefaultTree(), []FitnessDimension{DimNodeEfficiency})
		pp.ExpertKnowledge = ek
		best := pp.EvolvePareto(4, growthMultiFitness)
		if best == nil {
			t.Fatal("EvolvePareto returned nil best tree")
		}
		if len(ek.LearnedPatterns) > before {
			break
		}
	}

	if len(ek.LearnedPatterns) <= before {
		t.Fatal("expected EvolvePareto to grow ExpertKnowledge.LearnedPatterns via Observe across three seeded runs; archive is unchanged")
	}
	for _, lp := range ek.LearnedPatterns {
		if lp.Gain <= 0 {
			t.Errorf("learned pattern %+v recorded a non-positive gain; Observe must only retain genuine improvements", lp)
		}
	}
}

// TestNSGAIIPopulation_Evolve_ObservesLearnedPatternViaExpertKnowledge pins
// the NSGA-II half of milestone 2/3: Evolve's mutation-application step must
// call a caller-owned *ExpertKnowledge.Observe the same way EvolvePareto
// does, mirroring learning.go:845-921. The ExpertKnowledge field on
// NSGAIIPopulation does not exist yet, so this test fails to compile until
// milestone 2 lands.
func TestNSGAIIPopulation_Evolve_ObservesLearnedPatternViaExpertKnowledge(t *testing.T) {
	ek := NewExpertKnowledge()
	before := len(ek.LearnedPatterns)

	for _, seed := range []int64{42, 43, 44} {
		rand.Seed(seed) //nolint:staticcheck // deterministic evolution run for reproducibility
		nsga2 := NewNSGAIIPopulation(8, DefaultTree(), []FitnessDimension{DimNodeEfficiency})
		nsga2.ExpertKnowledge = ek
		best := nsga2.Evolve(4, growthMultiFitness)
		if best == nil {
			t.Fatal("Evolve returned nil best tree")
		}
		if len(ek.LearnedPatterns) > before {
			break
		}
	}

	if len(ek.LearnedPatterns) <= before {
		t.Fatal("expected NSGA-II Evolve to grow ExpertKnowledge.LearnedPatterns via Observe across three seeded runs; archive is unchanged")
	}
	for _, lp := range ek.LearnedPatterns {
		if lp.Gain <= 0 {
			t.Errorf("learned pattern %+v recorded a non-positive gain; Observe must only retain genuine improvements", lp)
		}
	}
}

// TestParetoPopulation_EvolvePareto_ResurrectsExtinctSpecialist verifies
// milestone 4/5 of the self-healing-wiring program: EvolvePareto must wrap its
// multi-objective breeding in the same selfHealGeneration envelope Evolve and
// EvolveWithExperience already use (learning.go:179, learning.go:480), so the
// seeded pop.Specialists registry (newProductionPopulation,
// cmd/bt-agent/tools.go:244) is actually consulted on the Pareto path too.
// Today EvolvePareto never reads p.Specialists or p.Crisis at all.
//
// Setup mirrors TestPopulationEvolve_ResurrectsExtinctSpecialist: a
// SpecialistRegistry pre-loaded with a validated, high-fitness goap archetype
// last seen at generation 0, and a live population of 10 identical,
// non-specialist individuals — Diversity() collapses to 0.1 (below the 0.2
// crisis threshold), and the goap niche is entirely absent, so it qualifies as
// extinct. After one EvolvePareto generation, the population must contain a
// resurrected:true individual, CrisisReasons must record diversity_collapse,
// and Resurrections must be positive.
func TestParetoPopulation_EvolvePareto_ResurrectsExtinctSpecialist(t *testing.T) {
	base := DefaultTree()

	registry := NewSpecialistRegistry()
	archetype := &SerializableNode{
		Type:     "Sequence",
		Name:     "GoapSpecialist",
		Children: []SerializableNode{{Type: "Action", Name: "PlanGoap"}},
	}
	registry.Observe(&EvolutionMetadata{
		TreeID:  "goap-archetype",
		Tags:    []string{"specialist:goap"},
		Fitness: FitnessRecord{Score: 0.95, Validated: true},
	}, archetype, 0)

	const size = 10
	pop := &Population{
		Individuals: make([]Individual, size),
		// Old generation so the archetype (last seen at gen 0) reads as long
		// extinct regardless of the detector's chosen extinctAfter window.
		Generation:  500,
		Specialists: registry,
	}
	for i := 0; i < size; i++ {
		// Identical, non-specialist genomes → Diversity() == 1/size == 0.1
		// (< 0.2 threshold) trips diversity_collapse, and the goap niche is
		// absent → the archetype qualifies as extinct.
		pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-genome"}
	}

	if d := pop.Diversity(); d <= 0 || d >= 0.2 {
		t.Fatalf("test setup: want collapsed diversity in (0, 0.2), got %.3f", d)
	}

	pp := &ParetoPopulation{
		Population: pop,
		Front:      NewParetoFront([]FitnessDimension{DimSuccessRate}),
	}

	fitnessFn := func(*SerializableNode) MultiFitness {
		mf := NewMultiFitness()
		mf.Set(DimSuccessRate, 100)
		return mf
	}

	pp.EvolvePareto(1, fitnessFn)

	if !containsReason(pp.CrisisReasons, "diversity_collapse") {
		t.Errorf("CrisisReasons = %v, want to contain diversity_collapse", pp.CrisisReasons)
	}

	var resurrected bool
	for _, ind := range pp.Individuals {
		if ind.Meta != nil && ind.Meta.IsResurrected() {
			resurrected = true
			break
		}
	}
	if !resurrected {
		t.Fatal("expected EvolvePareto to resurrect the extinct goap specialist and inject a resurrected:true-tagged individual into the population")
	}
	if pp.Resurrections <= 0 {
		t.Errorf("Resurrections = %d, want > 0", pp.Resurrections)
	}
}

// TestParetoFront_Load_MissingFileColdStart pins the cold-start contract the
// cross-run merge flow relies on: loading a path that does not exist yet
// (parent directory included) is a silent no-op, not an error, and the
// in-memory front is left untouched — mirrors
// TestMAPElitesGridLoad_MissingFileColdStart in map_elites_persist_test.go.
func TestParetoFront_Load_MissingFileColdStart(t *testing.T) {
	pf := NewParetoFront([]FitnessDimension{DimSuccessRate, DimPathCoverage})
	path := filepath.Join(t.TempDir(), "absent", "pareto.json")
	if err := pf.Load(path); err != nil {
		t.Fatalf("cold-start Load: %v", err)
	}
	if pf.Size() != 0 {
		t.Fatalf("cold-start front size = %d, want 0", pf.Size())
	}
}

// TestParetoFront_Save_EvictsLowestFitnessFirst pins the eviction order on
// the write path: front members are mutually non-dominated by construction,
// so Cap eviction must fall back to Individual.Fitness (the composite score)
// to decide which individuals to drop, keeping only the Cap strongest.
func TestParetoFront_Save_EvictsLowestFitnessFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pareto.json")

	dims := []FitnessDimension{DimSuccessRate, DimPathCoverage}
	pf := NewParetoFront(dims)
	pf.Cap = 2

	addTradeoff := func(name string, fitness, sr, pc float64) {
		tree := makeTestTree(name, 1, 2)
		ind := &Individual{Tree: tree, Fitness: fitness, Genome: hashTree(tree)}
		fv := NewMultiFitness()
		fv.Set(DimSuccessRate, sr)
		fv.Set(DimPathCoverage, pc)
		if !pf.Add(&MultiIndividual{Individual: ind, FitnessVec: fv}) {
			t.Fatalf("%s should be added (non-dominated trade-off)", name)
		}
	}
	// Trade-off along success_rate vs path_coverage keeps all three mutually
	// non-dominated, so Size() == 3 before any capping happens.
	addTradeoff("low", 10, 20, 90)
	addTradeoff("mid", 20, 50, 50)
	addTradeoff("high", 30, 90, 20)
	if pf.Size() != 3 {
		t.Fatalf("front size = %d, want 3 before save", pf.Size())
	}

	if err := pf.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reload := NewParetoFront(dims)
	if err := reload.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reload.Size(); got != 2 {
		t.Fatalf("persisted front size = %d, want cap 2", got)
	}
	for _, ind := range reload.Individuals {
		if ind.Fitness == 10 {
			t.Fatalf("lowest-fitness individual (10) should have been evicted from the persisted archive")
		}
	}
}

// TestParetoFront_Load_MergeKeepsFitterCopy exercises the merge-on-load
// semantics: unlike MAPElitesGrid (keyed by niche), ParetoFront has no niche
// key, so Load must merge disk individuals into memory via the existing
// dominance-based Add — an in-memory individual that dominates a persisted
// one must survive, and the dominated disk copy must be dropped.
func TestParetoFront_Load_MergeKeepsFitterCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pareto.json")
	dims := []FitnessDimension{DimSuccessRate, DimPathCoverage}

	// Run 1 persists a weak individual.
	run1 := NewParetoFront(dims)
	weakTree := makeTestTree("shared", 1, 2)
	weakInd := &Individual{Tree: weakTree, Fitness: 10, Genome: hashTree(weakTree)}
	weakFV := NewMultiFitness()
	weakFV.Set(DimSuccessRate, 30)
	weakFV.Set(DimPathCoverage, 30)
	run1.Add(&MultiIndividual{Individual: weakInd, FitnessVec: weakFV})
	if err := run1.Save(path); err != nil {
		t.Fatalf("run1 Save: %v", err)
	}

	// Run 2 already holds an individual that strictly dominates the one on
	// disk (better on every dimension).
	run2 := NewParetoFront(dims)
	strongTree := makeTestTree("shared-strong", 1, 2)
	strongInd := &Individual{Tree: strongTree, Fitness: 50, Genome: hashTree(strongTree)}
	strongFV := NewMultiFitness()
	strongFV.Set(DimSuccessRate, 80)
	strongFV.Set(DimPathCoverage, 80)
	run2.Add(&MultiIndividual{Individual: strongInd, FitnessVec: strongFV})

	if err := run2.Load(path); err != nil {
		t.Fatalf("run2 Load: %v", err)
	}

	if got := run2.Size(); got != 1 {
		t.Fatalf("merged front size = %d, want 1 (dominated disk copy dropped)", got)
	}
	if run2.Individuals[0].Fitness != 50 {
		t.Fatalf("merged front kept fitness %.0f, want the fitter in-memory copy (50)", run2.Individuals[0].Fitness)
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
