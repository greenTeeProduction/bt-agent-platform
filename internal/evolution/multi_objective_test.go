package evolution

import (
	"math"
	"path/filepath"
	"slices"
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

// TestNewNSGAIIPopulation_SeedsSpecialists pins milestone 1/2 of the
// self-healing-wiring program ("Close the NSGA-II self-healing wiring gap,
// the last production Evolve variant with zero specialist/crisis
// observability"): NewNSGAIIPopulation must seed Population.Specialists the
// same way IslandModel.EvolveAll's nil-guard does (island.go), so the
// self-healing envelope has a registry to consult from the very first
// generation instead of only after some other caller happens to set one.
func TestNewNSGAIIPopulation_SeedsSpecialists(t *testing.T) {
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}
	nsga2 := NewNSGAIIPopulation(5, baseTree, []FitnessDimension{DimSuccessRate})
	if nsga2.Specialists == nil {
		t.Fatal("NewNSGAIIPopulation did not seed Population.Specialists — the self-healing envelope has no registry to consult")
	}
}

// TestNSGAIIPopulation_Evolve_ResurrectsExtinctSpecialist pins milestone 2/2
// of the same program: NSGAIIPopulation.Evolve must wrap its offspring/
// replacement step in the same Population.selfHealGeneration envelope
// Evolve, EvolveWithExperience (learning.go), and EvolvePareto (pareto.go)
// already use, so a seeded Specialists registry is actually consulted on the
// NSGA-II path too. Today Evolve only calls nsga2.Evaluate() directly and
// never touches p.Specialists or p.Crisis, so this test fails.
//
// Setup mirrors TestParetoPopulation_EvolvePareto_ResurrectsExtinctSpecialist:
// a SpecialistRegistry pre-loaded with a validated, high-fitness goap
// archetype last seen at generation 0, and a live population of 10
// identical, non-specialist individuals — Diversity() collapses to 0.1
// (below the 0.2 crisis threshold), and the goap niche is entirely absent,
// so it qualifies as extinct. After one Evolve generation, the population
// must contain a resurrected:true individual, CrisisReasons must record
// diversity_collapse, and Resurrections must be positive.
func TestNSGAIIPopulation_Evolve_ResurrectsExtinctSpecialist(t *testing.T) {
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
	for i := range size {
		// Identical, non-specialist genomes → Diversity() == 1/size == 0.1
		// (< 0.2 threshold) trips diversity_collapse, and the goap niche is
		// absent → the archetype qualifies as extinct.
		pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-genome"}
	}

	if d := pop.Diversity(); d <= 0 || d >= 0.2 {
		t.Fatalf("test setup: want collapsed diversity in (0, 0.2), got %.3f", d)
	}

	nsga2 := &NSGAIIPopulation{
		Population:   pop,
		Dimensions:   []FitnessDimension{DimSuccessRate},
		FitnessVecs:  make([]MultiFitness, size),
		CrowdingDist: make([]float64, size),
	}

	fitnessFn := func(*SerializableNode) MultiFitness {
		mf := NewMultiFitness()
		mf.Set(DimSuccessRate, 100)
		return mf
	}

	nsga2.Evolve(1, fitnessFn)

	if !containsReason(nsga2.CrisisReasons, "diversity_collapse") {
		t.Errorf("CrisisReasons = %v, want to contain diversity_collapse", nsga2.CrisisReasons)
	}

	var resurrected bool
	for _, ind := range nsga2.Individuals {
		if ind.Meta != nil && ind.Meta.IsResurrected() {
			resurrected = true
			break
		}
	}
	if !resurrected {
		t.Fatal("expected Evolve to resurrect the extinct goap specialist and inject a resurrected:true-tagged individual into the population")
	}
	if nsga2.Resurrections <= 0 {
		t.Errorf("Resurrections = %d, want > 0", nsga2.Resurrections)
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

	fitnessFn := StructuralMultiFitness

	result := nsga2.Evolve(3, fitnessFn)
	if result == nil {
		t.Fatal("expected non-nil result from Evolve")
	}
	if result.Name == "" {
		t.Error("expected result tree to have a name")
	}
	t.Logf("NSGA-II evolve result: %s (fitness=%.2f)", result.Name, nsga2.BestFitness)
}

// TestNSGAIIPopulation_Load_MissingFileColdStart pins the cold-start contract
// milestone 3/5 of the Q2 Evolvability program requires: loading a path that
// does not exist yet (parent directory included) is a silent no-op, not an
// error — mirrors TestParetoFront_Load_MissingFileColdStart (milestone 1/5,
// pareto.go), which NSGAIIPopulation's final front (Fronts[0]) must match so
// all five evolution algorithms share the same durable cross-run archive
// contract. NSGAIIPopulation has no Save/Load/Archive today, so this fails to
// compile.
func TestNSGAIIPopulation_Load_MissingFileColdStart(t *testing.T) {
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}
	nsga2 := NewNSGAIIPopulation(3, baseTree, []FitnessDimension{DimSuccessRate, DimPathCoverage})
	path := filepath.Join(t.TempDir(), "absent", "nsga2.json")
	if err := nsga2.Load(path); err != nil {
		t.Fatalf("cold-start Load: %v", err)
	}
	if nsga2.Archive == nil || nsga2.Archive.Size() != 0 {
		t.Fatalf("cold-start archive = %v, want empty non-nil archive", nsga2.Archive)
	}
}

// TestNSGAIIPopulation_Save_EvictsLowestFitnessFirst pins the write-path
// eviction order for milestone 3/5: front-0 members are mutually
// non-dominated by construction (same as ParetoFront), so Cap eviction must
// fall back to Individual.Fitness (the composite score) to decide which
// individuals to drop, keeping only the Cap strongest — mirrors
// TestParetoFront_Save_EvictsLowestFitnessFirst.
func TestNSGAIIPopulation_Save_EvictsLowestFitnessFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nsga2.json")
	dims := []FitnessDimension{DimSuccessRate, DimPathCoverage}
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}

	nsga2 := NewNSGAIIPopulation(3, baseTree, dims)
	nsga2.Cap = 2

	setTradeoff := func(i int, name string, fitness, sr, pc float64) {
		tree := makeTestTree(name, 1, 2)
		nsga2.Individuals[i] = Individual{Tree: tree, Fitness: fitness, Genome: hashTree(tree)}
		fv := NewMultiFitness()
		fv.Set(DimSuccessRate, sr)
		fv.Set(DimPathCoverage, pc)
		nsga2.FitnessVecs[i] = fv
	}
	// Trade-off along success_rate vs path_coverage keeps all three mutually
	// non-dominated, so front 0 holds all 3 before any capping happens.
	setTradeoff(0, "low", 10, 20, 90)
	setTradeoff(1, "mid", 20, 50, 50)
	setTradeoff(2, "high", 30, 90, 20)
	nsga2.Fronts = nsga2.fastNonDominatedSort(nsga2.FitnessVecs)
	if len(nsga2.Fronts) == 0 || len(nsga2.Fronts[0].Indices) != 3 {
		t.Fatalf("test setup: want all 3 individuals in front 0, got fronts=%+v", nsga2.Fronts)
	}

	if err := nsga2.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reload := NewNSGAIIPopulation(1, baseTree, dims)
	reload.Cap = 2
	if err := reload.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reload.Archive == nil || reload.Archive.Size() != 2 {
		t.Fatalf("persisted archive = %v, want cap 2", reload.Archive)
	}
	for _, ind := range reload.Archive.Individuals {
		if ind.Fitness == 10 {
			t.Fatalf("lowest-fitness individual (10) should have been evicted from the persisted archive")
		}
	}
}

// TestNSGAIIPopulation_Load_MergeKeepsFitterCopy exercises the merge-on-load
// semantics milestone 3/5 requires: like ParetoFront, NSGAIIPopulation's
// archive has no niche key, so Load must merge disk individuals into memory
// via dominance — an in-memory individual that dominates a persisted one must
// survive, and the dominated disk copy must be dropped. Mirrors
// TestParetoFront_Load_MergeKeepsFitterCopy.
func TestNSGAIIPopulation_Load_MergeKeepsFitterCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nsga2.json")
	dims := []FitnessDimension{DimSuccessRate, DimPathCoverage}
	baseTree := &SerializableNode{Name: "root", Type: "Selector"}

	// Run 1 persists a weak individual.
	run1 := NewNSGAIIPopulation(1, baseTree, dims)
	weakTree := makeTestTree("shared", 1, 2)
	run1.Individuals[0] = Individual{Tree: weakTree, Fitness: 10, Genome: hashTree(weakTree)}
	weakFV := NewMultiFitness()
	weakFV.Set(DimSuccessRate, 30)
	weakFV.Set(DimPathCoverage, 30)
	run1.FitnessVecs[0] = weakFV
	run1.Fronts = run1.fastNonDominatedSort(run1.FitnessVecs)
	if err := run1.Save(path); err != nil {
		t.Fatalf("run1 Save: %v", err)
	}

	// Run 2 already holds an individual that strictly dominates the one on
	// disk (better on every dimension).
	run2 := NewNSGAIIPopulation(1, baseTree, dims)
	strongTree := makeTestTree("shared-strong", 1, 2)
	run2.Individuals[0] = Individual{Tree: strongTree, Fitness: 50, Genome: hashTree(strongTree)}
	strongFV := NewMultiFitness()
	strongFV.Set(DimSuccessRate, 80)
	strongFV.Set(DimPathCoverage, 80)
	run2.FitnessVecs[0] = strongFV
	run2.Fronts = run2.fastNonDominatedSort(run2.FitnessVecs)

	if err := run2.Load(path); err != nil {
		t.Fatalf("run2 Load: %v", err)
	}

	if got := run2.Archive.Size(); got != 1 {
		t.Fatalf("merged archive size = %d, want 1 (dominated disk copy dropped)", got)
	}
	if run2.Archive.Individuals[0].Fitness != 50 {
		t.Fatalf("merged archive kept fitness %.0f, want the fitter in-memory copy (50)", run2.Archive.Individuals[0].Fitness)
	}
}

// ─── Q5: one canonical NSGA-II implementation ───────────────────────────────

// tradeoffVecs is a fixed population whose members span both a Pareto front and
// a dominated tail, so a non-dominated sort over it produces more than one
// front and the crowding distances over front 0 are non-trivial.
func tradeoffVecs() []MultiFitness {
	return []MultiFitness{
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 90, DimStability: 10}}, // 0: front 0 boundary
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 60, DimStability: 55}}, // 1: front 0 interior
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 30, DimStability: 90}}, // 2: front 0 boundary
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 20, DimStability: 5}},  // 3: dominated by all
		{Scores: map[FitnessDimension]float64{DimSuccessRate: 50, DimStability: 50}}, // 4: dominated by 1
	}
}

// frontIndexSets renders fronts as comparable sorted index slices.
func frontIndexSets(fronts []NSGAIIFront) [][]int {
	out := make([][]int, len(fronts))
	for i, f := range fronts {
		idx := slices.Clone(f.Indices)
		slices.Sort(idx)
		out[i] = idx
	}
	return out
}

func sameFronts(a, b []NSGAIIFront) bool {
	as, bs := frontIndexSets(a), frontIndexSets(b)
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if len(as[i]) != len(bs[i]) {
			return false
		}
		for j := range as[i] {
			if as[i][j] != bs[i][j] {
				return false
			}
		}
	}
	return true
}

// TestNSGAII_CanonicalNonDominatedSort_SharedByBothCallers pins Q5 of the
// "evolveTreeV2 actually uses the evolution package's non-heuristic algorithms"
// program: multi_objective.go carried TWO near-identical copies of NSGA-II's
// fast non-dominated sort — NSGAIIPopulation.fastNonDominatedSort (read by
// Evaluate) and NSGAIISorter's private copy (read by Evolve's offspring
// replacement step). Two copies means a correctness fix can land in one and
// silently miss the other, so both must delegate to ONE canonical package-level
// nonDominatedSort. That canonical function does not exist yet, so this fails
// to compile.
func TestNSGAII_CanonicalNonDominatedSort_SharedByBothCallers(t *testing.T) {
	dims := []FitnessDimension{DimSuccessRate, DimStability}
	vecs := tradeoffVecs()

	canonical := nonDominatedSort(vecs)
	if len(canonical) < 2 {
		t.Fatalf("canonical sort produced %d fronts, want at least 2 for %v", len(canonical), frontIndexSets(canonical))
	}
	if got := frontIndexSets(canonical)[0]; len(got) != 3 {
		t.Fatalf("canonical front 0 = %v, want the 3 mutually non-dominated members {0,1,2}", got)
	}

	sorter := NewNSGAIISorter(dims)
	if got := sorter.fastNonDominatedSort(vecs); !sameFronts(got, canonical) {
		t.Errorf("NSGAIISorter fronts %v diverge from canonical %v", frontIndexSets(got), frontIndexSets(canonical))
	}

	pop := NewNSGAIIPopulation(len(vecs), &SerializableNode{Name: "root", Type: "Selector"}, dims)
	if got := pop.fastNonDominatedSort(vecs); !sameFronts(got, canonical) {
		t.Errorf("NSGAIIPopulation fronts %v diverge from canonical %v", frontIndexSets(got), frontIndexSets(canonical))
	}
}

// TestNSGAII_CanonicalCrowdingDistance_SharedByBothCallers is the crowding-
// distance half of the same Q5 deduplication: NSGAIIPopulation.assignCrowding
// Distance writes a slice and NSGAIISorter.assignCrowdingDistance returns a
// map, but the arithmetic between them must come from ONE canonical
// crowdingDistances implementation rather than two hand-maintained copies.
func TestNSGAII_CanonicalCrowdingDistance_SharedByBothCallers(t *testing.T) {
	dims := []FitnessDimension{DimSuccessRate, DimStability}
	vecs := tradeoffVecs()
	front0 := []int{0, 1, 2}

	canonical := crowdingDistances(front0, vecs, dims)
	// Boundaries are infinitely crowded-apart; the interior member must carry a
	// finite, positive distance or the sort has nothing to rank diversity by.
	if !math.IsInf(canonical[0], 1) || !math.IsInf(canonical[2], 1) {
		t.Fatalf("canonical boundary distances = %v/%v, want +Inf", canonical[0], canonical[2])
	}
	if math.IsInf(canonical[1], 1) || canonical[1] <= 0 {
		t.Fatalf("canonical interior distance = %v, want finite positive", canonical[1])
	}

	sorter := NewNSGAIISorter(dims)
	for _, idx := range front0 {
		if got := sorter.assignCrowdingDistance(front0, vecs)[idx]; got != canonical[idx] {
			t.Errorf("NSGAIISorter distance[%d] = %v, canonical = %v", idx, got, canonical[idx])
		}
	}

	pop := NewNSGAIIPopulation(len(vecs), &SerializableNode{Name: "root", Type: "Selector"}, dims)
	pop.FitnessVecs = vecs
	pop.assignCrowdingDistance(front0)
	for _, idx := range front0 {
		if pop.CrowdingDist[idx] != canonical[idx] {
			t.Errorf("NSGAIIPopulation distance[%d] = %v, canonical = %v", idx, pop.CrowdingDist[idx], canonical[idx])
		}
	}
}
