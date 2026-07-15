package evolution

import (
	"math/rand"
	"testing"
)

// rigDeathSpiralIsland builds a Population that reproduces the same
// diversity-collapse recipe TestSelfHealGeneration_ExtractsEvolveSelfHealingStep
// (learning_test.go) uses to force Population.selfHealGeneration into emergency
// mode: identical genomes across the island (Diversity() == 1/size, well below
// the 0.2 collapse threshold) and a Generation far past specialistExtinctAfter
// so the seeded archetype reads as long extinct. The Specialists registry is
// pre-seeded with a validated, high-fitness archetype for specialistType that
// is absent from the live population, so a genuine self-healing pass has a
// concrete extinct niche to resurrect.
func rigDeathSpiralIsland(name, specialistType string, size int) *Population {
	registry := NewSpecialistRegistry()
	archetype := &SerializableNode{
		Type:     "Sequence",
		Name:     specialistType + "Archetype",
		Children: []SerializableNode{{Type: "Action", Name: "Do" + specialistType}},
	}
	registry.Observe(&EvolutionMetadata{
		TreeID:  name + "-" + specialistType + "-archetype",
		Tags:    []string{"specialist:" + specialistType},
		Fitness: FitnessRecord{Score: 0.95, Validated: true},
	}, archetype, 0)

	pop := &Population{
		Individuals: make([]Individual, size),
		// Long past specialistExtinctAfter (5) generations since the archetype
		// was last seen at generation 0, so ExtinctSpecialists reports it missing.
		Generation:  500,
		Specialists: registry,
	}
	base := islandTestTree(name)
	for i := 0; i < size; i++ {
		// Identical genomes (no specialist provenance) across every individual
		// trips diversity_collapse the moment DetectPopulation runs, and leaves
		// the "specialistType" niche absent from the population.
		pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-" + name, Fitness: 1.0}
	}
	pop.BestFitness = 1.0
	pop.PrevBestFitness = 1.0
	return pop
}

// TestIslandModel_EvolveAllResurrectsExtinctSpecialistDuringDeathSpiral pins
// milestone 5/5 of the self-healing-wiring program ("Close the self-healing
// wiring drift across every production Evolve variant"): EvolveAll's per-island
// step must run the shared Population.selfHealGeneration envelope instead of a
// bare Evaluate, so a collapsed island resurrects an extinct specialist
// archetype from its own Specialists registry before migration runs — exactly
// like a direct Population.Evolve call already does. Today EvolveAll only calls
// pop.Evaluate(fitnessFn), so Crisis is never initialized, the seeded registry
// is never consulted, and this test fails.
func TestIslandModel_EvolveAllResurrectsExtinctSpecialistDuringDeathSpiral(t *testing.T) {
	im := NewIslandModel(1000, 0.5) // migration interval far beyond this test's single call
	pop := rigDeathSpiralIsland("go", "goap", 10)
	im.AddIsland("go", pop)

	im.EvolveAll(func(*SerializableNode) float64 { return 1.0 })

	island := im.GetIsland("go")
	if island == nil {
		t.Fatal("island 'go' vanished after EvolveAll")
	}
	if island.Crisis == nil {
		t.Fatal("EvolveAll did not run the self-healing envelope: Population.Crisis was never initialized")
	}
	if len(island.CrisisReasons) == 0 {
		t.Error("EvolveAll did not surface any CrisisReasons for a diversity-collapsed island")
	}
	if island.Resurrections <= 0 {
		t.Fatalf("island Resurrections = %d, want > 0 (the extinct goap specialist should have been resurrected during the death spiral)", island.Resurrections)
	}
	resurrected := false
	for _, ind := range island.Individuals {
		if ind.Meta != nil && ind.Meta.IsResurrected() {
			resurrected = true
			break
		}
	}
	if !resurrected {
		t.Error("no individual in the island carries a resurrected specialist after EvolveAll")
	}
	if got := im.Stats().Resurrections; got <= 0 {
		t.Errorf("Stats().Resurrections = %d, want > 0 (the model-level aggregate must reflect the island's resurrection)", got)
	}
}

// TestIslandModel_EvolveAllFleetParitySelfHealsEveryIsland is the fleet-parity
// regression guard: every island in the model must go through the same
// self-healing envelope, not just some of them. Islands are seeded the way
// production code adds them via AddIsland — without any Specialists/Crisis
// pre-populated — so EvolveAll itself must seed each island's Specialists and
// Crisis before resurrection is even possible. The test then asserts every
// single island was actually run through the self-healing step; a future
// regression that only wires up one island (an early return, a special case
// for a single domain, a loop that silently drops islands) must fail here
// regardless of Go's randomized map iteration order.
func TestIslandModel_EvolveAllFleetParitySelfHealsEveryIsland(t *testing.T) {
	im := NewIslandModel(1000, 0.5)
	domains := []string{"go", "ops", "fin"}
	for _, d := range domains {
		im.AddIsland(d, islandTestPopulation(d+"-a", d+"-b", d+"-c"))
	}

	im.EvolveAll(func(tree *SerializableNode) float64 { return float64(len(tree.Name)) })

	for _, d := range domains {
		island := im.GetIsland(d)
		if island == nil {
			t.Fatalf("island %q vanished after EvolveAll", d)
		}
		if island.Specialists == nil {
			t.Errorf("island %q: EvolveAll did not seed Population.Specialists — this island's evolve pass skips the shared self-healing helper", d)
		}
		if island.Crisis == nil {
			t.Errorf("island %q: EvolveAll did not seed Population.Crisis — this island's evolve pass skips the shared self-healing helper", d)
		}
	}
}

// growthFitness (experience_bank_test.go) is monotone in node count and
// bounded in (0,1), so any mutation that adds nodes strictly improves
// fitness — reused here (as it already is by
// TestExpertKnowledge_ObservesLearnedPatternFromQLearning and the Pareto/
// NSGA-II equivalents in pareto_test.go) to guarantee a seeded run
// encounters improving mutations for ExpertKnowledge to record.

// TestIslandModel_EvolveAllObservesLearnedPatternViaExpertKnowledge pins
// milestone 3/3 of the Q2 Evolvability "feed ExpertKnowledge's
// learned-pattern archive back into mutation recommendations and every
// production evolution algorithm" program: EvolveAll's per-island
// mutation-application step must call a caller-owned
// *ExpertKnowledge.Observe(action, category, gain) whenever a mutation
// genuinely improves fitness, mirroring the wiring EvolvePareto and NSGA-II
// Evolve already have (pareto.go:416-419, multi_objective.go) and
// EvolveQLearning/qLearnMutate before them (learning.go:845-921). The
// ExpertKnowledge field on IslandModel does not exist yet, so this test
// fails to compile until milestone 3 lands.
func TestIslandModel_EvolveAllObservesLearnedPatternViaExpertKnowledge(t *testing.T) {
	ek := NewExpertKnowledge()
	before := len(ek.LearnedPatterns)

	// A handful of fixed seeds keeps the run deterministic while tolerating
	// exactly which mutation op the random draw picks first, mirroring
	// TestParetoPopulation_EvolvePareto_ObservesLearnedPatternViaExpertKnowledge.
	for _, seed := range []int64{42, 43, 44} {
		rand.Seed(seed)                 //nolint:staticcheck // deterministic evolution run for reproducibility
		im := NewIslandModel(1000, 0.5) // migration interval far beyond this test's generations
		im.ExpertKnowledge = ek
		im.AddIsland("go", NewPopulation(8, DefaultTree()))

		var bestTrees map[string]*SerializableNode
		for gen := 0; gen < 4; gen++ {
			bestTrees = im.EvolveAll(growthFitness)
		}
		if bestTrees["go"] == nil {
			t.Fatal("EvolveAll returned no best tree for island 'go'")
		}
		if len(ek.LearnedPatterns) > before {
			break
		}
	}

	if len(ek.LearnedPatterns) <= before {
		t.Fatal("expected EvolveAll to grow ExpertKnowledge.LearnedPatterns via Observe across three seeded runs; archive is unchanged")
	}
	for _, lp := range ek.LearnedPatterns {
		if lp.Gain <= 0 {
			t.Errorf("learned pattern %+v recorded a non-positive gain; Observe must only retain genuine improvements", lp)
		}
	}
}

// TestMAPElitesPopulation_EvolveMAPElites_ObservesLearnedPatternViaExpertKnowledge
// pins the MAP-Elites half of milestone 3/3: EvolveMAPElites's
// mutation-application step must call a caller-owned
// *ExpertKnowledge.Observe the same way EvolvePareto, NSGA-II Evolve, and now
// EvolveAll do. The ExpertKnowledge field on MAPElitesPopulation does not
// exist yet, so this test fails to compile until milestone 3 lands.
func TestMAPElitesPopulation_EvolveMAPElites_ObservesLearnedPatternViaExpertKnowledge(t *testing.T) {
	ek := NewExpertKnowledge()
	before := len(ek.LearnedPatterns)

	for _, seed := range []int64{42, 43, 44} {
		rand.Seed(seed) //nolint:staticcheck // deterministic evolution run for reproducibility
		mp := NewMAPElitesPopulation(8, DefaultTree(), "go")
		mp.ExpertKnowledge = ek

		best := mp.EvolveMAPElites(4, growthFitness)
		if best == nil {
			t.Fatal("EvolveMAPElites returned nil best tree")
		}
		if len(ek.LearnedPatterns) > before {
			break
		}
	}

	if len(ek.LearnedPatterns) <= before {
		t.Fatal("expected EvolveMAPElites to grow ExpertKnowledge.LearnedPatterns via Observe across three seeded runs; archive is unchanged")
	}
	for _, lp := range ek.LearnedPatterns {
		if lp.Gain <= 0 {
			t.Errorf("learned pattern %+v recorded a non-positive gain; Observe must only retain genuine improvements", lp)
		}
	}
}
