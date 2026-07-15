package evolution

import "testing"

// TestMemeticEvolve_ResurrectsExtinctSpecialist verifies milestone 1/3 of the
// "Close the self-healing envelope gap across the remaining GA variants"
// program: MemeticEvolve must wrap its crossover/mutation replacement step in
// the shared selfHealGeneration envelope (crisis detection, emergency
// mutation-rate override, specialist archiving, extinct-specialist
// resurrection) instead of running a bare hardcoded rand.Float64() < 0.3
// mutation gate with no Crisis/Specialists logic.
//
// Setup mirrors TestPopulationEvolve_ResurrectsExtinctSpecialist: a
// SpecialistRegistry pre-loaded with a validated, high-fitness goap archetype
// last seen at generation 0. The live population is 10 identical,
// non-specialist individuals — Diversity() collapses to 0.1 (below the 0.2
// threshold, tripping diversity_collapse), the goap niche is entirely absent
// (so the archetype reads as extinct), and pop.Generation is far past the
// archetype's last-seen generation so any reasonable extinctAfter window has
// elapsed. After one MemeticEvolve generation the population must contain an
// individual whose provenance is tagged resurrected:true — exactly like
// Evolve, EvolveWithExperience, EvolveQLearning, NSGAIIPopulation.Evolve, and
// EvolvePareto already do via selfHealGeneration.
func TestMemeticEvolve_ResurrectsExtinctSpecialist(t *testing.T) {
	base := DefaultTree()

	// Archive a high-fitness specialist that is missing from the live population.
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

	searcher := NewLocalSearcher(HillClimbSearch)
	pop.MemeticEvolve(1, func(*SerializableNode) float64 { return 1.0 }, searcher, 1)

	var resurrected bool
	for _, ind := range pop.Individuals {
		if ind.Meta != nil && ind.Meta.IsResurrected() {
			resurrected = true
			break
		}
	}
	if !resurrected {
		t.Fatal("expected MemeticEvolve to resurrect the extinct goap specialist and inject a resurrected:true-tagged individual into the population")
	}
}
