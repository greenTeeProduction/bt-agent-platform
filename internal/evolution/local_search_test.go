package evolution

import "testing"

// ─── Local-search refinement wiring (milestone 1/5) ──────────────────────
//
// The tests below pin the seam evolveTreeV2 uses to run LocalSearch as a
// post-structural-mutation refinement pass: parameter extraction that sees
// the fields real trees actually carry, a hill climb that can move an
// integer-backed parameter across a step-shaped fitness landscape, and a
// gated entry point that only keeps a tuned tree when it beats the base
// fitness through the existing QualityGate thresholds.

// TestExtractMutableParams_CoversStructFields pins that parameter extraction
// sees SerializableNode's TimeoutMs/MaxRetries struct fields, not just the
// Metadata["timeout_ms"]/["threshold"] keys. Real trees (internal/domains,
// gardener registry) set the struct fields and leave Metadata nil, so a
// Metadata-only extractor makes the whole refinement pass inert in
// production. Zero-valued fields stay untunable — perturbing 0 multiplicatively
// can never leave 0 — so this tree must yield exactly two params.
func TestExtractMutableParams_CoversStructFields(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence", Name: "Root", TimeoutMs: 5000,
		Children: []SerializableNode{
			{
				Type: "Retry", Name: "RetryStep", MaxRetries: 8,
				Children: []SerializableNode{{Type: "Action", Name: "Step"}},
			},
		},
	}

	params := extractMutableParams(tree)
	if len(params) != 2 {
		values := make([]float64, len(params))
		for i := range params {
			values[i] = params[i].getValue()
		}
		t.Fatalf("extractMutableParams found %d params %v, want 2 (root TimeoutMs=5000, RetryStep MaxRetries=8)", len(params), values)
	}

	timeout, retries := -1, -1
	for i := range params {
		switch params[i].getValue() {
		case 5000:
			timeout = i
		case 8:
			retries = i
		}
	}
	if timeout < 0 {
		t.Fatalf("no param reads the root's TimeoutMs field (want 5000)")
	}
	if retries < 0 {
		t.Fatalf("no param reads RetryStep's MaxRetries field (want 8)")
	}

	// Setting must write back through to the struct field, so a tuned clone
	// is a real tree the gardener can persist.
	params[timeout].setValue(7000)
	if tree.TimeoutMs != 7000 {
		t.Errorf("setValue(7000) on the TimeoutMs param left tree.TimeoutMs = %d, want 7000", tree.TimeoutMs)
	}
	// Integer-backed fields round rather than truncate toward a no-op.
	params[retries].setValue(3.4)
	if got := tree.Children[0].MaxRetries; got != 3 {
		t.Errorf("setValue(3.4) on the MaxRetries param left MaxRetries = %d, want 3", got)
	}
}

// TestLocalSearcher_HillClimb_CrossesStepPlateau pins that hill climbing can
// actually reach a better parameter value across a plateau. The evaluator's
// structural-quality term is step-shaped (a Retry node scores only when
// MaxRetries is in [1,5]), so a climber whose only move is a ±5% multiplicative
// nudge is permanently stuck at 8: every intermediate value scores identically,
// is not an improvement, and gets reverted. A refinement pass that cannot cross
// that plateau is wired-but-dead on exactly the trees this milestone targets.
func TestLocalSearcher_HillClimb_CrossesStepPlateau(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []SerializableNode{
			{
				Type: "Retry", Name: "RetryStep", MaxRetries: 8,
				Children: []SerializableNode{{Type: "Action", Name: "Step"}},
			},
		},
	}
	fitness := func(n *SerializableNode) float64 {
		if r := n.Children[0].MaxRetries; r >= 1 && r <= 5 {
			return 1.0
		}
		return 0.0
	}

	tuned, delta := NewLocalSearcher(HillClimbSearch).Search(tree, fitness)

	if tuned == nil {
		t.Fatal("Search returned a nil tree")
	}
	if got := tuned.Children[0].MaxRetries; got < 1 || got > 5 {
		t.Errorf("hill climb left MaxRetries = %d, want a value in [1,5] that scores on the step landscape", got)
	}
	if delta <= 0 {
		t.Errorf("hill climb reported delta %.4f, want a positive improvement", delta)
	}
	if tree.Children[0].MaxRetries != 8 {
		t.Errorf("Search mutated the input tree (MaxRetries = %d, want the original 8)", tree.Children[0].MaxRetries)
	}
}

// TestLocalSearcher_RefineGated_AcceptsImprovement pins the gated entry point
// evolveTreeV2 calls: run the configured strategy over the tree's mutable
// params using the caller's fitness function, and return the tuned tree only
// when it beats baseFitness and the QualityGate accepts the pre→post pair.
// The input tree is never mutated in place — the caller commits the result.
func TestLocalSearcher_RefineGated_AcceptsImprovement(t *testing.T) {
	tree := &SerializableNode{Type: "Sequence", Name: "Root", TimeoutMs: 5000}
	// Continuous and increasing in TimeoutMs, so any upward nudge improves.
	fitness := func(n *SerializableNode) float64 {
		return float64(n.TimeoutMs) / 1000.0
	}
	base := fitness(tree)
	gate := NewQualityGate(t.TempDir())

	res := NewLocalSearcher(HillClimbSearch).RefineGated(tree, base, fitness, gate, "refine_tree")

	if !res.Accepted {
		t.Fatalf("RefineGated rejected a strict improvement: %+v", res)
	}
	if res.Tree == nil {
		t.Fatal("accepted refinement returned a nil tree")
	}
	if res.Tree.TimeoutMs <= 5000 {
		t.Errorf("refined tree TimeoutMs = %d, want a tuned value above the original 5000", res.Tree.TimeoutMs)
	}
	if res.Delta <= 0 {
		t.Errorf("Delta = %.4f, want > 0", res.Delta)
	}
	if res.Fitness <= base {
		t.Errorf("Fitness = %.4f, want > baseFitness %.4f", res.Fitness, base)
	}
	if tree.TimeoutMs != 5000 {
		t.Errorf("RefineGated mutated the input tree in place (TimeoutMs = %d, want 5000)", tree.TimeoutMs)
	}
}

// TestLocalSearcher_RefineGated_RejectsNonImprovement pins the acceptance gate's
// other half: a refinement that cannot beat baseFitness is discarded, the
// caller gets its original tree back untouched, and — because a speculative
// tuning attempt is not a regression of the live tree — the gate's per-tree
// failure streak is left at zero. Burning the streak here would eventually
// disable evolution for a perfectly healthy tree.
func TestLocalSearcher_RefineGated_RejectsNonImprovement(t *testing.T) {
	tree := &SerializableNode{Type: "Sequence", Name: "Root", TimeoutMs: 5000}
	flat := func(*SerializableNode) float64 { return 42.0 }
	gate := NewQualityGate(t.TempDir())

	ls := NewLocalSearcher(HillClimbSearch)
	for i := 0; i < gate.ConsecutiveFails*2; i++ {
		res := ls.RefineGated(tree, 42.0, flat, gate, "refine_tree")
		if res.Accepted {
			t.Fatalf("attempt %d: RefineGated accepted a refinement that did not beat baseFitness: %+v", i, res)
		}
		if res.Tree != tree {
			t.Fatalf("attempt %d: rejected refinement must return the original tree, got %+v", i, res.Tree)
		}
		if res.Delta != 0 {
			t.Errorf("attempt %d: rejected refinement reported Delta = %.4f, want 0", i, res.Delta)
		}
	}

	if got := gate.FailCountFor("refine_tree"); got != 0 {
		t.Errorf("rejected refinements burned the quality gate's failure streak: FailCountFor = %d, want 0", got)
	}
	if gate.IsDisabledFor("refine_tree") {
		t.Error("rejected refinements disabled the quality gate for the tree; speculative tuning must never fail-close a healthy tree")
	}
}

// TestQualityGate_Probe_DoesNotRecordFailures pins the non-recording probe
// RefineGated consults: identical verdicts to Validate, zero bookkeeping.
func TestQualityGate_Probe_DoesNotRecordFailures(t *testing.T) {
	q := NewQualityGate(t.TempDir())

	if got := q.Probe(50, 60); got != GateAccepted {
		t.Errorf("Probe(50, 60) = %v, want %v", got, GateAccepted)
	}
	if got := q.Probe(20, 10); got != GateRejected {
		t.Errorf("Probe(20, 10) = %v, want %v (below the composite floor and declining)", got, GateRejected)
	}
	if got := q.Probe(50, 10); got != GateRollback {
		t.Errorf("Probe(50, 10) = %v, want %v (regression beyond MaxRegressionRate)", got, GateRollback)
	}

	for i := 0; i < q.ConsecutiveFails*2; i++ {
		q.Probe(50, 10)
	}
	if got := q.FailCount(); got != 0 {
		t.Errorf("Probe recorded %d consecutive failures, want 0 — probing must not mutate gate state", got)
	}
	if q.IsDisabled() {
		t.Error("Probe disabled the gate; a probe must be side-effect free")
	}
}

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
