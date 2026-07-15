package knowledge

import "testing"

// =============================================================================
// Domain-specific fitness injection (RegisterDomainFitness)
//
// RecordRun's generic runtime-success EMA is a coarse success/failure signal.
// Some domains (e.g. NotebookLM) have a richer, domain-aware fitness function
// that scores anti-fabrication and output quality from recent run history, but
// today nothing feeds a tree's recorded runs to it — the knowledge graph has no
// hook a domain package can register itself against. RegisterDomainFitness adds
// that hook: once a fitness function is registered for a tree ID, RecordRun must
// track a bounded window of that tree's recent runs and use the registered
// function's output — instead of the generic EMA — to update Fitness. Trees
// without a registered function keep the existing EMA behavior unchanged.
// =============================================================================

// A tree with a registered domain fitness function must have its Fitness driven
// by that function's output (scaled to the 0-100 Fitness range) from the
// window of recent runs, not by the generic outcome EMA.
func TestRecordRun_DomainFitness_OverridesGenericEMA(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:domainfit",
		Name:     "Domain Fit",
		Category: "test",
		Fitness:  50.0,
	})

	// A trivial domain fitness function: fraction of runs with Outcome=="success".
	kg.RegisterDomainFitness("tree:domainfit", func(runs []RunSummary) float64 {
		if len(runs) == 0 {
			return 0
		}
		successes := 0.0
		for _, r := range runs {
			if r.Outcome == "success" {
				successes++
			}
		}
		return successes / float64(len(runs))
	})

	kg.RecordRun(RunRecord{TreeID: "tree:domainfit", Outcome: "success", Quality: 1.0})
	kg.RecordRun(RunRecord{TreeID: "tree:domainfit", Outcome: "failure", Quality: 0.0})

	tree := kg.Trees["tree:domainfit"]
	if tree == nil {
		t.Fatal("tree should exist")
	}
	// 1 success out of 2 runs -> domain fn returns 0.5 -> Fitness == 50.0.
	// The generic EMA (0.9*50 + 0.1*(0.3*100) = 48.0 after the failure alone)
	// would land somewhere else entirely, so this pins that the domain fn — not
	// the EMA — drives Fitness once one is registered.
	if tree.Fitness != 50.0 {
		t.Errorf("expected Fitness=50.0 (domain fn output), got %.2f — RecordRun did not use the registered domain fitness function", tree.Fitness)
	}
}

// RecordRun must maintain a bounded window of a tree's recent runs so a
// registered domain fitness function has real history to score, without
// growing unbounded over a tree's lifetime.
func TestRecordRun_TracksBoundedRecentRunsWindow(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:history", Name: "History", Category: "test"})

	const totalRuns = maxRunHistory + 5
	for i := 0; i < totalRuns; i++ {
		kg.RecordRun(RunRecord{TreeID: "tree:history", Outcome: "success", Quality: 1.0})
	}

	tree := kg.Trees["tree:history"]
	if tree == nil {
		t.Fatal("tree should exist")
	}
	if len(tree.RecentRuns) != maxRunHistory {
		t.Errorf("expected RecentRuns bounded to %d, got %d", maxRunHistory, len(tree.RecentRuns))
	}
}

// A tree with NO registered domain fitness function must keep using the
// existing generic EMA — RegisterDomainFitness must not change behavior for
// trees nobody opted in for.
func TestRecordRun_NoDomainFitness_KeepsGenericEMA(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:plainema",
		Name:     "Plain EMA",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{TreeID: "tree:plainema", Outcome: "success"})

	tree := kg.Trees["tree:plainema"]
	// Same EMA as TestRecordRun_ExistingTree: 0.9*50 + 0.1*100 = 55.0.
	if tree.Fitness != 55.0 {
		t.Errorf("expected Fitness=55.0 (unregistered tree keeps generic EMA), got %.2f", tree.Fitness)
	}
}

// =============================================================================
// Structural fitness split (evolved structural fitness must not overwrite the
// runtime-success EMA)
//
// RecordRun's "evolved" branch writes a winning QD/island elite's *structural*
// fitness back into the tree. Today it writes it straight into TreeMeta.Fitness,
// which is ALSO the runtime-success EMA that genuine executions maintain. That
// conflation lets a structurally-elite but runtime-failing tree keep a near-100
// Fitness forever — a later genuine failure only nudges 0.9*95 + 0.1*30 = 88.5 —
// so discovery and breeding keep surfacing it despite its runtime failures.
//
// The fix splits evolved structural fitness into its own TreeMeta.StructuralFitness
// field, leaving Fitness as a pure runtime-success EMA, and has discovery/breeding
// BLEND structural fitness gated by RunCount (it fills in when a tree is unproven
// at runtime and yields to the runtime EMA once the tree is well-run). These tests
// pin both halves and fail against the current single-field implementation.
// =============================================================================

// The "evolved" write-back must land in StructuralFitness and leave the
// runtime-success EMA (Fitness) untouched, so an evolution pass can never
// overwrite what genuine executions measured.
func TestRecordRun_Evolved_WritesStructuralFitnessNotRuntimeEMA(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:split",
		Name:     "Split",
		Category: "test",
		Fitness:  50.0,
	})

	// One genuine success moves the runtime EMA to 0.9*50 + 0.1*100 = 55.0.
	kg.RecordRun(RunRecord{TreeID: "tree:split", Task: "run", Outcome: "success"})

	// A strong evolution pass (structural fitness 95, well above the 55 EMA).
	kg.RecordRun(RunRecord{TreeID: "tree:split", Task: "evolve", Outcome: "evolved", Quality: 95.0})

	tree := kg.Trees["tree:split"]
	// Runtime EMA is preserved — the evolved pass must NOT overwrite it.
	if tree.Fitness != 55.0 {
		t.Errorf("evolved run overwrote the runtime-success EMA: expected Fitness=55.0 (untouched), got %.2f", tree.Fitness)
	}
	// Structural fitness is captured separately, clamped into [0,100].
	if tree.StructuralFitness != 95.0 {
		t.Errorf("expected StructuralFitness=95.0 (evolved write-back), got %.2f", tree.StructuralFitness)
	}
	// Evolution passes count separately and never inflate RunCount.
	if tree.RunCount != 1 {
		t.Errorf("expected RunCount=1 (genuine executions only), got %d", tree.RunCount)
	}
	if tree.EvolvedCount != 1 {
		t.Errorf("expected EvolvedCount=1, got %d", tree.EvolvedCount)
	}

	// Structural write-back stays monotone on its own field: a weaker elite
	// never regresses it, and it still leaves the runtime EMA alone.
	kg.RecordRun(RunRecord{TreeID: "tree:split", Task: "evolve", Outcome: "evolved", Quality: 70.0})
	if tree.StructuralFitness != 95.0 {
		t.Errorf("expected StructuralFitness to stay 95.0 (monotone), got %.2f", tree.StructuralFitness)
	}
	if tree.Fitness != 55.0 {
		t.Errorf("second evolved run disturbed the runtime EMA: expected Fitness=55.0, got %.2f", tree.Fitness)
	}
}

// Discovery must BLEND StructuralFitness, gated by RunCount. With runtime Fitness
// and RunCount held identical, the tree with the higher StructuralFitness must win
// the keyword tie-break — even though it sorts LAST by ID. At RunCount=0 (unproven
// at runtime) structural fitness is the signal that surfaces an archive-improved
// tree; a pure-Fitness tie-break would fall through to the sorted-ID fallback and
// pick the lower-ID tree instead.
func TestStringMatch_BlendsStructuralFitness(t *testing.T) {
	kg := NewKnowledgeGraph()
	// Sorts FIRST by ID, no structural fitness — wins a pure-ID tie-break.
	kg.Register(&TreeMeta{
		ID:                "aaa:plain",
		Name:              "Plain",
		Category:          "test",
		Keywords:          []string{"alpha"},
		Fitness:           50.0,
		RunCount:          0,
		StructuralFitness: 0.0,
	})
	// Sorts LAST by ID, but structurally elite — must win once structural
	// fitness is blended into the tie-break for this unproven tree.
	kg.Register(&TreeMeta{
		ID:                "zzz:elite",
		Name:              "Elite",
		Category:          "test",
		Keywords:          []string{"gamma"},
		Fitness:           50.0,
		RunCount:          0,
		StructuralFitness: 90.0,
	})

	// "alpha gamma workflow" matches both 5-char keywords equally, so the winner
	// is decided entirely by the (runtime + structural) blended tie-break.
	ids, _ := collectDiscoverIDs(kg, "alpha gamma workflow")

	if len(ids) != 1 {
		t.Fatalf("structural-blended keyword tie is non-deterministic: saw %d distinct trees over %d runs: %v",
			len(ids), discoverRuns, ids)
	}
	if !ids["zzz:elite"] {
		t.Errorf("expected the structurally-elite tree zzz:elite (StructuralFitness 90) to out-select aaa:plain (StructuralFitness 0) at equal runtime Fitness/RunCount, got %v", ids)
	}
}

// =============================================================================
// Cold-start confidence (selection-pressure program m3/5)
//
// Milestone 2/5 taught both the deterministic discovery fallback (stringMatch)
// and tree breeding (selectParents) to prefer the fitter tree. But raw fitness
// alone lets a single lucky run dominate: a tree that scored 100 on its ONLY
// recorded run should not out-select a tree that has held 70 across 50 runs.
//
// Milestone 3/5 introduces a shared RunCount-based cold-start confidence helper
// that discounts the fitness contribution when a tree has few recorded runs, and
// wires it into BOTH callers. These tests pin that invariant from each side.
// They fail against the m2/5 implementation, which weights by raw Fitness with
// no RunCount discount, so the lucky fitness=100/RunCount=1 tree wins.
// =============================================================================

// Discover (stringMatch keyword tie-break): a fitness=100/RunCount=1 tree must
// NOT out-select a fitness=70/RunCount=50 tree. The lucky tree also sorts FIRST
// by ID, so if cold-start were ignored it would win on both raw fitness and the
// sorted-ID fallback — only a genuine RunCount discount can flip the result.
func TestStringMatch_ColdStartDiscountsLuckyTree(t *testing.T) {
	kg := NewKnowledgeGraph()
	// "aaa:lucky": one lucky run scored 100. Sorts first by ID.
	kg.Register(&TreeMeta{
		ID:       "aaa:lucky",
		Name:     "Lucky",
		Category: "test",
		Keywords: []string{"alpha"},
		Fitness:  100,
		RunCount: 1,
	})
	// "zzz:proven": lower peak fitness but proven across many runs. Sorts last.
	kg.Register(&TreeMeta{
		ID:       "zzz:proven",
		Name:     "Proven",
		Category: "test",
		Keywords: []string{"gamma"},
		Fitness:  70,
		RunCount: 50,
	})

	// "alpha gamma workflow" matches both 5-char keywords equally, so the winner
	// is decided entirely by the cold-start-adjusted fitness tie-break.
	ids, _ := collectDiscoverIDs(kg, "alpha gamma workflow")

	if len(ids) != 1 {
		t.Fatalf("cold-start-weighted keyword tie is non-deterministic: saw %d distinct trees over %d runs: %v",
			len(ids), discoverRuns, ids)
	}
	if !ids["zzz:proven"] {
		t.Errorf("expected the proven tree zzz:proven (Fitness 70, RunCount 50) to out-select the lucky tree aaa:lucky (Fitness 100, RunCount 1) after cold-start discounting, got %v", ids)
	}
}

// selectParents (fitness-weighted breeding): a fitness=100/RunCount=1 template
// must NOT be drawn as a parent more often than a fitness=70/RunCount=50
// template. Under raw-fitness weighting the lucky template (weight 100) is drawn
// more than the proven one (weight 70); the cold-start discount must reverse that.
func TestSelectParents_ColdStartDiscountsLuckyTemplate(t *testing.T) {
	const (
		luckyID  = "cat:lucky"
		provenID = "cat:proven"
		iters    = 4000
	)

	templates := map[string]*TreeTemplate{
		luckyID:  {SourceID: luckyID, Category: "cat", Metadata: map[string]any{"fitness": 100.0, "run_count": 1}},
		provenID: {SourceID: provenID, Category: "cat", Metadata: map[string]any{"fitness": 70.0, "run_count": 50}},
	}
	// Fill the pool with proven-like competitors (same fitness/run profile as the
	// proven template) so the 2-3 parent slots are genuinely contested. Under raw
	// fitness the lucky template's weight of 100 makes it the single most-drawn
	// parent; only a RunCount cold-start discount can push it below the well-run
	// crowd it competes against.
	for _, id := range []string{"cat:c1", "cat:c2", "cat:c3", "cat:c4", "cat:c5", "cat:c6"} {
		templates[id] = &TreeTemplate{SourceID: id, Category: "cat", Metadata: map[string]any{"fitness": 70.0, "run_count": 50}}
	}

	f := &Factory{Templates: templates}
	f.SetSeed(42)

	luckyHits, provenHits := 0, 0
	for i := 0; i < iters; i++ {
		for _, id := range f.selectParents("cat", "") {
			switch id {
			case luckyID:
				luckyHits++
			case provenID:
				provenHits++
			}
		}
	}

	luckyRate := float64(luckyHits) / float64(iters)
	provenRate := float64(provenHits) / float64(iters)

	// After cold-start discounting, the proven template (RunCount 50) must be
	// drawn strictly more often than the lucky template (RunCount 1), even though
	// the lucky template has the higher raw fitness.
	if provenRate <= luckyRate {
		t.Errorf("cold-start discount not applied: lucky template (Fitness 100, RunCount 1) drawn at %.3f, proven template (Fitness 70, RunCount 50) at %.3f; proven must out-select lucky",
			luckyRate, provenRate)
	}
}
