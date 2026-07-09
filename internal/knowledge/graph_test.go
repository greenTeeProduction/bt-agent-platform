package knowledge

import "testing"

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
