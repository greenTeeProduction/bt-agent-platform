package knowledge

import "testing"

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
