package knowledge

import "testing"

// TestFactory_BreedRefreshesTemplateFitnessFromGraph asserts that breeding reads
// each candidate's CURRENT KnowledgeGraph fitness at breed time, not the snapshot
// captured when NewFactory extracted templates (milestone 4/5 of the
// selection-pressure program: refresh template fitness from the live graph).
//
// All four same-category trees are registered with equal low fitness, so
// NewFactory snapshots them as indistinguishable. AFTER construction one tree's
// KG fitness is raised sharply. If parent selection re-reads the live graph, that
// tree must now dominate the fitness-weighted draw; if it only ever consults the
// stale NewFactory snapshot, every candidate stays equally weighted and the
// raised tree is drawn at the uniform rate.
func TestFactory_BreedRefreshesTemplateFitnessFromGraph(t *testing.T) {
	const (
		category = "c"
		targetID = "c:target"
		iters    = 4000
	)
	otherIDs := []string{"c:other0", "c:other1", "c:other2"}

	kg := NewKnowledgeGraph()
	for _, id := range append([]string{targetID}, otherIDs...) {
		kg.Register(&TreeMeta{ID: id, Category: category, Fitness: 1.0})
	}

	f := NewFactory(kg)
	f.SetSeed(42)

	// Raise the target's fitness in the live graph AFTER the factory snapshotted
	// its templates. A stale snapshot cannot see this change.
	kg.Trees[targetID].Fitness = 100.0

	targetHits := 0
	otherHits := make(map[string]int, len(otherIDs))
	for i := 0; i < iters; i++ {
		for _, id := range f.selectParents(category, "") {
			if id == targetID {
				targetHits++
			}
			for _, oid := range otherIDs {
				if id == oid {
					otherHits[oid]++
				}
			}
		}
	}

	targetRate := float64(targetHits) / float64(iters)

	var otherTotal int
	for _, oid := range otherIDs {
		otherTotal += otherHits[oid]
	}
	avgOtherRate := float64(otherTotal) / float64(len(otherIDs)) / float64(iters)

	// With a live re-read the raised 100-vs-1 target must dominate the draw; a
	// stale snapshot leaves it at the ~uniform rate its low-fitness siblings share.
	if targetRate <= 0.85 {
		t.Fatalf("target drawn at rate %.3f after its KG fitness was raised post-construction; want > 0.85 (breeding must re-read live graph fitness, not the NewFactory snapshot)", targetRate)
	}
	if targetRate <= 1.5*avgOtherRate {
		t.Fatalf("post-raise target draw rate %.3f is not meaningfully above the avg sibling rate %.3f (breeding still using the stale fitness snapshot)", targetRate, avgOtherRate)
	}
}

// TestFactory_SelectParentsFavorsHighFitness asserts that Factory.selectParents
// draws high-fitness templates as parents at a rate meaningfully above the
// uniform baseline (milestone 1/5 of the selection-pressure program: replace the
// uniform rand.Shuffle parent draw with fitness-weighted sampling over
// templateFitness).
//
// The map is keyed only by tree IDs (no category-alias entries, unlike the ones
// NewFactory synthesizes) so every same-category candidate is distinct. Under
// the old uniform draw each of the four candidates is equally likely, so the
// dominant template is picked at the uniform rate (~0.625 when drawing 2-3 of 4)
// — the same rate as its low-fitness siblings. Fitness-weighted sampling must
// push the high-fitness template's draw rate well past that and well past the
// siblings'.
func TestFactory_SelectParentsFavorsHighFitness(t *testing.T) {
	const (
		highID = "cat:high"
		iters  = 4000
	)
	lowIDs := []string{"cat:low1", "cat:low2", "cat:low3"}

	f := &Factory{
		Templates: map[string]*TreeTemplate{
			highID:    {SourceID: highID, Category: "cat", Metadata: map[string]any{"fitness": 100.0}},
			lowIDs[0]: {SourceID: lowIDs[0], Category: "cat", Metadata: map[string]any{"fitness": 1.0}},
			lowIDs[1]: {SourceID: lowIDs[1], Category: "cat", Metadata: map[string]any{"fitness": 1.0}},
			lowIDs[2]: {SourceID: lowIDs[2], Category: "cat", Metadata: map[string]any{"fitness": 1.0}},
		},
	}

	highHits := 0
	lowHits := make(map[string]int, len(lowIDs))
	for i := 0; i < iters; i++ {
		for _, id := range f.selectParents("cat", "") {
			if id == highID {
				highHits++
			}
			for _, lid := range lowIDs {
				if id == lid {
					lowHits[lid]++
				}
			}
		}
	}

	highRate := float64(highHits) / float64(iters)

	var lowTotal int
	for _, lid := range lowIDs {
		lowTotal += lowHits[lid]
	}
	avgLowRate := float64(lowTotal) / float64(len(lowIDs)) / float64(iters)

	// Uniform selection draws each same-category template at ~0.625; a
	// fitness-driven draw of a 100-vs-1 dominant template must land far higher.
	if highRate <= 0.85 {
		t.Fatalf("high-fitness template drawn at rate %.3f; want > 0.85 (fitness-weighted, above the ~0.625 uniform rate)", highRate)
	}
	// And it must dominate its low-fitness siblings rather than tie them.
	if highRate <= 1.5*avgLowRate {
		t.Fatalf("high-fitness draw rate %.3f is not meaningfully above the avg low-fitness rate %.3f (uniform selection ties them)", highRate, avgLowRate)
	}
}
