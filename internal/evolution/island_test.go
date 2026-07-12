package evolution

import "testing"

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
