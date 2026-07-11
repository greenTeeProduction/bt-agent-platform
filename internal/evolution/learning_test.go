package evolution

import (
	"fmt"
	"testing"
)

// TestPopulationEvolve_RecordsCrisisReasons verifies that Population.Evolve
// wires proactive crisis intervention into the GA loop: it lazily initializes a
// CrisisDetector, calls DetectPopulation each generation (reusing the same
// PopulationState built for supervisor.Guide), and records the returned
// population-level crisis reasons on Population.CrisisReasons.
//
// The population is deliberately unhealthy: 10 individuals share one genome so
// Population.Diversity() collapses to 0.1 (below the 0.2 crisis threshold), and
// a pre-seeded high regression rate trips the regression streak. Evolve should
// therefore record both "diversity_collapse" and "regression_spiral".
func TestPopulationEvolve_RecordsCrisisReasons(t *testing.T) {
	base := DefaultTree()

	const size = 10
	pop := &Population{
		Individuals: make([]Individual, size),
		// Pre-seed a saturated regression rate. RegressionRate() is a percentage
		// (100 here), well above the detector's 0.5 threshold, so three
		// consecutive generations trip regression_spiral.
		Regressions:    100,
		TotalMutations: 100,
	}
	for i := 0; i < size; i++ {
		// Identical genome across the population → Diversity() == 1/size == 0.1.
		pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-genome"}
	}

	if d := pop.Diversity(); d <= 0 || d >= 0.2 {
		t.Fatalf("test setup: want collapsed diversity in (0, 0.2), got %.3f", d)
	}

	// Constant fitness keeps every individual "working" (WorkingRatio == 1) so
	// quality_crash stays quiet and the two target reasons are isolated.
	pop.Evolve(4, func(*SerializableNode) float64 { return 1.0 })

	if pop.Crisis == nil {
		t.Fatal("Evolve did not lazily initialize Population.Crisis")
	}
	if !containsReason(pop.CrisisReasons, "diversity_collapse") {
		t.Errorf("expected diversity_collapse recorded, got %v", pop.CrisisReasons)
	}
	if !containsReason(pop.CrisisReasons, "regression_spiral") {
		t.Errorf("expected regression_spiral recorded, got %v", pop.CrisisReasons)
	}
}

// TestPopulationEvolve_CrisisIntervention verifies milestone 2/5 of the
// proactive crisis-intervention wiring: it is no longer enough to merely
// *record* the crisis reasons — Evolve must ACT on the signal. When
// DetectPopulation fires (or the supervisor flags an intervention phase),
// Evolve overrides that generation's mutation rate with the CrisisDetector's
// emergency rate and calls ResetPopulation once the emergency generation
// completes, clearing the streak counters. A healthy generation keeps the
// supervisor's recommended rate and leaves no crisis footprint.
func TestPopulationEvolve_CrisisIntervention(t *testing.T) {
	base := DefaultTree()
	const size = 10

	t.Run("crisis generation forces the emergency rate and clears streaks", func(t *testing.T) {
		pop := &Population{
			Individuals: make([]Individual, size),
			// Saturated regression rate: RegressionRate() == 100% (well above
			// the detector's 0.5 threshold) so the regression streak advances.
			Regressions:    100,
			TotalMutations: 100,
		}
		for i := 0; i < size; i++ {
			// Identical genome → Diversity() == 0.1 < 0.2 → diversity_collapse.
			pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical"}
		}
		// Pre-seed the regression streak to 2 so this single generation's
		// DetectPopulation lifts it to 3 (a spiral). Only ResetPopulation can
		// bring it back to 0: the reactive DetectPopulation logic leaves it at
		// 3 because the regression rate stays high, so a non-zero value proves
		// ResetPopulation was never called.
		pop.Crisis = NewCrisisDetector()
		pop.Crisis.regressionStreak = 2
		pop.Crisis.qualityCrash = 3

		pop.Evolve(1, func(*SerializableNode) float64 { return 1.0 })

		emergency := pop.Crisis.GetEmergencyMutationRate()
		if pop.LastMutationRate < emergency {
			t.Errorf("crisis generation mutation rate = %.3f, want >= EmergencyRate %.3f",
				pop.LastMutationRate, emergency)
		}
		if pop.Crisis.regressionStreak != 0 {
			t.Errorf("regressionStreak = %d after emergency generation, want 0 (ResetPopulation not called)",
				pop.Crisis.regressionStreak)
		}
		if pop.Crisis.qualityCrash != 0 {
			t.Errorf("qualityCrash = %d after emergency generation, want 0", pop.Crisis.qualityCrash)
		}
	})

	t.Run("healthy generation is unaffected", func(t *testing.T) {
		pop := &Population{Individuals: make([]Individual, size)}
		for i := 0; i < size; i++ {
			// Distinct genomes → Diversity() == 1.0: no collapse, no crisis.
			pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: fmt.Sprintf("genome-%d", i)}
		}

		// Healthy fitness keeps the supervisor out of any intervention phase, so
		// the recommended (non-emergency) rate must survive untouched.
		pop.Evolve(1, func(*SerializableNode) float64 { return 0.85 })

		if pop.Crisis == nil {
			t.Fatal("Evolve did not lazily initialize Population.Crisis")
		}
		emergency := pop.Crisis.GetEmergencyMutationRate()
		if pop.LastMutationRate >= emergency {
			t.Errorf("healthy generation mutation rate = %.3f, want < EmergencyRate %.3f (should keep the supervisor's recommended rate)",
				pop.LastMutationRate, emergency)
		}
		if len(pop.CrisisReasons) != 0 {
			t.Errorf("healthy generation recorded crisis reasons %v, want none", pop.CrisisReasons)
		}
	})
}

// TestExpertKnowledge_SeedSpecialistsCarryProvenance verifies milestone 3/5 of the
// crisis-intervention program: population individuals must carry specialist
// provenance so the SpecialistRegistry has a type to key on.
//
// The expert seeder builds trees tagged with specialist metadata. Each seeded
// Individual must expose that metadata through the new Individual.Meta field
// (*EvolutionMetadata), carrying a "specialist:<type>" tag and validated fitness.
// SpecialistRegistry.Observe requires exactly those two properties, so a seeded
// individual fed to Observe must actually register an archetype — otherwise the
// registry seam stays reachable only from tests and never sees a real population.
func TestExpertKnowledge_SeedSpecialistsCarryProvenance(t *testing.T) {
	seeded := NewExpertKnowledge().SeedSpecialists()
	if len(seeded) == 0 {
		t.Fatal("expected expert seeder to produce at least one specialist individual")
	}

	registry := NewSpecialistRegistry()
	for i, ind := range seeded {
		if ind.Tree == nil {
			t.Fatalf("seeded individual %d has no tree", i)
		}
		if ind.Meta == nil {
			t.Fatalf("seeded individual %d carries no *EvolutionMetadata (Individual.Meta is nil)", i)
		}
		if firstSpecialistType(ind.Meta.Tags) == "" {
			t.Fatalf("seeded individual %d meta has no specialist: tag, got tags %v", i, ind.Meta.Tags)
		}
		if !ind.Meta.Fitness.Validated {
			t.Fatalf("seeded individual %d meta fitness is not validated", i)
		}
		// The point of the provenance: the registry can key on it.
		registry.Observe(ind.Meta, ind.Tree, 0)
	}

	if len(registry.Archetypes) == 0 {
		t.Fatal("expected SpecialistRegistry.Observe to record at least one archetype from the expert-seeded population")
	}
}

// TestPopulationEvolve_ResurrectsExtinctSpecialist verifies milestone 4/5 of the
// crisis-intervention program: Evolve must archive and resurrect specialists
// during recovery. It is not enough to detect a crisis (milestones 1-2) or to
// carry specialist provenance (milestone 3) — when a diversity-collapse
// generation coincides with a high-fitness specialist that has gone extinct in
// the live population, Evolve must pull that archetype out of the attached
// SpecialistRegistry via ExtinctSpecialists + Resurrect and inject it back into
// the population in place of the weakest non-elite individuals.
//
// Setup: a SpecialistRegistry pre-loaded with a validated, high-fitness goap
// archetype last seen at generation 0. The live population is 10 identical,
// non-specialist individuals — Diversity() collapses to 0.1 (below the 0.2
// threshold, tripping diversity_collapse), the goap niche is entirely absent
// (so the archetype reads as extinct), and pop.Generation is far past the
// archetype's last-seen generation so any reasonable extinctAfter window has
// elapsed. After one Evolve generation the population must contain an
// individual whose provenance is tagged resurrected:true.
func TestPopulationEvolve_ResurrectsExtinctSpecialist(t *testing.T) {
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

	pop.Evolve(1, func(*SerializableNode) float64 { return 1.0 })

	var resurrected bool
	for _, ind := range pop.Individuals {
		if ind.Meta != nil && ind.Meta.IsResurrected() {
			resurrected = true
			break
		}
	}
	if !resurrected {
		t.Fatal("expected Evolve to resurrect the extinct goap specialist and inject a resurrected:true-tagged individual into the population")
	}
}

// TestPopulationHealthSnapshot_DiversityCollapseRun verifies milestone 4/5 of
// the observability program ("Make evolution self-healing observable
// end-to-end"): the GA's self-healing signals — crisis reasons, the mutation
// rate actually applied, the generation counter, and how many specialists were
// resurrected — must be exposed through a single Population.HealthSnapshot()
// accessor so metrics/dashboard consumers can read population health without
// reaching into Evolve internals.
//
// Setup mirrors TestPopulationEvolve_ResurrectsExtinctSpecialist: a
// SpecialistRegistry holds a validated high-fitness goap archetype last seen at
// generation 0, while the live population is 10 identical non-specialist
// individuals at generation 500. One Evolve generation therefore trips
// diversity_collapse, forces the emergency mutation rate, and resurrects the
// extinct specialist — and the snapshot must surface all of it:
// non-empty CrisisReasons, LastMutationRate at (or above) the emergency rate,
// the post-run Generation, and a positive Resurrections count tracked where
// p.Specialists.Resurrect succeeds.
func TestPopulationHealthSnapshot_DiversityCollapseRun(t *testing.T) {
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
		// extinct regardless of the extinctAfter window.
		Generation:  500,
		Specialists: registry,
	}
	for i := 0; i < size; i++ {
		// Identical, non-specialist genomes → Diversity() == 1/size == 0.1
		// (< 0.2 threshold) trips diversity_collapse; the goap niche is absent
		// so the archetype qualifies as extinct.
		pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-genome"}
	}

	if d := pop.Diversity(); d <= 0 || d >= 0.2 {
		t.Fatalf("test setup: want collapsed diversity in (0, 0.2), got %.3f", d)
	}

	pop.Evolve(1, func(*SerializableNode) float64 { return 1.0 })

	snap := pop.HealthSnapshot()

	if len(snap.CrisisReasons) == 0 {
		t.Error("HealthSnapshot().CrisisReasons is empty after a diversity-collapse run, want at least one reason")
	}
	if !containsReason(snap.CrisisReasons, "diversity_collapse") {
		t.Errorf("HealthSnapshot().CrisisReasons = %v, want to contain diversity_collapse", snap.CrisisReasons)
	}
	if pop.Crisis == nil {
		t.Fatal("Evolve did not lazily initialize Population.Crisis")
	}
	emergency := pop.Crisis.GetEmergencyMutationRate()
	if snap.LastMutationRate < emergency {
		t.Errorf("HealthSnapshot().LastMutationRate = %.3f, want >= EmergencyRate %.3f (crisis generation must run under emergency control)",
			snap.LastMutationRate, emergency)
	}
	if snap.LastMutationRate != pop.LastMutationRate {
		t.Errorf("HealthSnapshot().LastMutationRate = %.3f, want the applied rate %.3f", snap.LastMutationRate, pop.LastMutationRate)
	}
	if snap.Generation != pop.Generation {
		t.Errorf("HealthSnapshot().Generation = %d, want post-run generation %d", snap.Generation, pop.Generation)
	}
	if snap.Resurrections <= 0 {
		t.Errorf("HealthSnapshot().Resurrections = %d, want > 0 (the extinct goap specialist was resurrected this run)", snap.Resurrections)
	}
}

// TestSelfHealGeneration_ExtractsEvolveSelfHealingStep pins milestone 1/5 of the
// self-healing-wiring program ("Close the self-healing wiring drift across every
// production Evolve variant"): the per-generation self-healing step must be
// extracted out of Population.Evolve into a reusable Population.selfHealGeneration
// helper that wraps a variant-supplied reproduction callback. The helper owns the
// five self-healing responsibilities the program cares about — crisis detection,
// emergency mutation-rate override, specialist archiving (Observe),
// extinct-specialist resurrection on emergency, and streak reset after a spiral —
// while the reproduction closure it is handed performs the variant-specific
// breeding at the effective (possibly emergency-elevated) mutation rate. Keeping
// reproduction a callback is what makes the envelope reusable across Evolve,
// EvolveWithExperience, and EvolveQLearning in the later milestones.
//
// These are characterization tests: each subtest reproduces the observable
// self-healing behavior Evolve exhibits today (mirroring
// TestPopulationEvolve_CrisisIntervention and
// TestPopulationEvolve_ResurrectsExtinctSpecialist) so the extraction can be
// verified byte-for-byte identical. selfHealGeneration does not exist yet, so this
// test fails to compile until milestone 1 lands.
//
// Contract exercised: the caller owns p.Generation and computes eliteCount; the
// helper is handed (eliteCount, supervisor, reproduce). It builds one
// PopulationState, reuses it for both supervisor.Guide and
// CrisisDetector.DetectPopulation, sets p.LastMutationRate, invokes reproduce with
// that effective rate, resurrects extinct specialists when the generation ran
// under emergency control, and resets the streak counters after a spiral.
func TestSelfHealGeneration_ExtractsEvolveSelfHealingStep(t *testing.T) {
	base := DefaultTree()
	const size = 10
	// Same elite clamp Evolve computes once before its loop.
	eliteCount := min(max(2, size/10), size)

	t.Run("emergency generation forces the emergency rate, resurrects, and resets streaks", func(t *testing.T) {
		// Archive a high-fitness goap specialist that is missing from the live
		// population and last seen at generation 0.
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

		pop := &Population{
			Individuals: make([]Individual, size),
			// Saturated regression rate → RegressionRate() == 100% (> 0.5) so the
			// regression streak advances this generation.
			Regressions:    100,
			TotalMutations: 100,
			// Old generation so the archetype (last seen at gen 0) reads as long
			// extinct regardless of the detector's extinctAfter window.
			Generation:  500,
			Specialists: registry,
		}
		for i := 0; i < size; i++ {
			// Identical, non-specialist genomes → Diversity() == 0.1 (< 0.2) trips
			// diversity_collapse and leaves the goap niche absent (extinct).
			pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: "identical-genome"}
		}
		// Mirror Evolve's pre-loop Evaluate: constant working fitness so the two
		// target reasons stay isolated (quality_crash quiet).
		for i := range pop.Individuals {
			pop.Individuals[i].Fitness = 1.0
		}
		pop.BestFitness = 1.0
		pop.PrevBestFitness = 1.0
		// Pre-seed the regression streak to 2 so this single generation lifts it to
		// 3 (a spiral). Only ResetPopulation can bring it back to 0 — the reactive
		// DetectPopulation logic leaves it at 3 because the regression rate stays
		// high, so a zero value proves ResetPopulation was called.
		pop.Crisis = NewCrisisDetector()
		pop.Crisis.regressionStreak = 2
		pop.Crisis.qualityCrash = 3

		var reproduceRate float64
		reproduced := false
		pop.selfHealGeneration(eliteCount, NewLLMSupervisor(), func(mutationRate float64) {
			reproduced = true
			reproduceRate = mutationRate
		})

		if !reproduced {
			t.Fatal("selfHealGeneration never invoked the reproduction callback")
		}
		emergency := pop.Crisis.GetEmergencyMutationRate()
		if reproduceRate < emergency {
			t.Errorf("reproduction callback received rate %.3f, want >= EmergencyRate %.3f (emergency generation must reproduce under emergency control)",
				reproduceRate, emergency)
		}
		if pop.LastMutationRate != reproduceRate {
			t.Errorf("LastMutationRate = %.3f, want the rate handed to reproduce %.3f", pop.LastMutationRate, reproduceRate)
		}
		if !containsReason(pop.CrisisReasons, "diversity_collapse") {
			t.Errorf("CrisisReasons = %v, want to contain diversity_collapse", pop.CrisisReasons)
		}
		if !containsReason(pop.CrisisReasons, "regression_spiral") {
			t.Errorf("CrisisReasons = %v, want to contain regression_spiral", pop.CrisisReasons)
		}
		var resurrected bool
		for _, ind := range pop.Individuals {
			if ind.Meta != nil && ind.Meta.IsResurrected() {
				resurrected = true
				break
			}
		}
		if !resurrected {
			t.Error("expected the extinct goap specialist to be resurrected into the population")
		}
		if pop.Resurrections <= 0 {
			t.Errorf("Resurrections = %d, want > 0", pop.Resurrections)
		}
		if pop.Crisis.regressionStreak != 0 {
			t.Errorf("regressionStreak = %d after emergency generation, want 0 (ResetPopulation not called)",
				pop.Crisis.regressionStreak)
		}
	})

	t.Run("healthy generation keeps the supervisor's recommended rate", func(t *testing.T) {
		pop := &Population{Individuals: make([]Individual, size)}
		for i := 0; i < size; i++ {
			// Distinct genomes → Diversity() == 1.0: no collapse, no crisis.
			pop.Individuals[i] = Individual{Tree: cloneTree(base), Genome: fmt.Sprintf("genome-%d", i)}
		}
		// Mirror Evolve's pre-loop Evaluate at a healthy fitness so the supervisor
		// stays out of any intervention phase.
		for i := range pop.Individuals {
			pop.Individuals[i].Fitness = 0.85
		}
		pop.BestFitness = 0.85
		pop.PrevBestFitness = 0.85

		var reproduceRate float64
		reproduced := false
		pop.selfHealGeneration(eliteCount, NewLLMSupervisor(), func(mutationRate float64) {
			reproduced = true
			reproduceRate = mutationRate
		})

		if !reproduced {
			t.Fatal("selfHealGeneration never invoked the reproduction callback")
		}
		if pop.Crisis == nil {
			t.Fatal("selfHealGeneration did not lazily initialize Population.Crisis")
		}
		emergency := pop.Crisis.GetEmergencyMutationRate()
		if reproduceRate >= emergency {
			t.Errorf("healthy generation reproduce rate = %.3f, want < EmergencyRate %.3f (should keep the supervisor's recommended rate)",
				reproduceRate, emergency)
		}
		if pop.LastMutationRate != reproduceRate {
			t.Errorf("LastMutationRate = %.3f, want the rate handed to reproduce %.3f", pop.LastMutationRate, reproduceRate)
		}
		if len(pop.CrisisReasons) != 0 {
			t.Errorf("healthy generation recorded crisis reasons %v, want none", pop.CrisisReasons)
		}
		if pop.Resurrections != 0 {
			t.Errorf("healthy generation Resurrections = %d, want 0", pop.Resurrections)
		}
	})

	t.Run("archives validated specialist elites via the registry", func(t *testing.T) {
		registry := NewSpecialistRegistry()
		pop := &Population{
			Individuals: make([]Individual, size),
			Specialists: registry,
		}
		for i := 0; i < size; i++ {
			// Distinct genomes keep the generation healthy so Observe is isolated
			// from crisis/resurrection side effects.
			pop.Individuals[i] = Individual{Tree: cloneTree(base), Fitness: 0.5, Genome: fmt.Sprintf("genome-%d", i)}
		}
		// The two top-fitness individuals carry validated specialist provenance, so
		// after the helper's fitness sort they occupy the elite window Observe
		// archives every generation.
		for i := 0; i < eliteCount; i++ {
			pop.Individuals[i].Fitness = 1.0
			pop.Individuals[i].Meta = &EvolutionMetadata{
				TreeID:  fmt.Sprintf("planner-%d", i),
				Tags:    []string{"specialist:planner"},
				Fitness: FitnessRecord{Score: 1.0, Validated: true},
			}
		}
		pop.BestFitness = 1.0
		pop.PrevBestFitness = 1.0

		pop.selfHealGeneration(eliteCount, NewLLMSupervisor(), func(float64) {})

		if len(registry.Archetypes) == 0 {
			t.Fatal("selfHealGeneration did not Observe any specialist elite into the registry")
		}
		if _, ok := registry.Archetypes["planner"]; !ok {
			t.Errorf("registry archetypes = %v, want a planner archetype observed from the elites", registry.Archetypes)
		}
	})
}
