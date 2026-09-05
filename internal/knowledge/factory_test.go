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
	for range iters {
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
	for range iters {
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

// TestFactory_SelectParentsExcludesCategoryAliasKeys asserts that a template
// registered under BOTH its canonical SourceID key and NewFactory's synthesized
// category-alias key cannot be drawn as both parents of a crossover.
//
// extractTemplates stores every template twice in f.Templates — once under its
// SourceID and once under meta.Category — with both keys pointing at the SAME
// *TreeTemplate. selectParents ranges over every map key, so the alias entry puts
// the same underlying template into the candidate pool a second time, and
// weightedSampleParents dedups only by string key. Without excluding alias keys,
// two returned parents can resolve to a single SourceID: a self-crossover of a
// template with itself.
//
// Two same-category trees (fitness 50 vs 10) yield map keys {idA, idB, category},
// with the alias resolving to the higher-fitness idA. Because selectParents may
// draw n=3 from that 3-key pool, the alias key and idA are returned together and
// both resolve to idA. Every draw must instead map to distinct SourceIDs.
func TestFactory_SelectParentsExcludesCategoryAliasKeys(t *testing.T) {
	const category = "dup"
	idA := "dup:a"
	idB := "dup:b"

	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: idA, Category: category, Fitness: 50.0})
	kg.Register(&TreeMeta{ID: idB, Category: category, Fitness: 10.0})

	f := NewFactory(kg)
	f.SetSeed(1)

	for i := range 2000 {
		parents := f.selectParents(category, "")
		seen := make(map[string]int, len(parents))
		for _, key := range parents {
			tmpl := f.Templates[key]
			if tmpl == nil {
				t.Fatalf("iter %d: selectParents returned key %q with no backing template", i, key)
			}
			seen[tmpl.SourceID]++
			if seen[tmpl.SourceID] > 1 {
				t.Fatalf("iter %d: template %q drawn as more than one parent (parents=%v resolve to a duplicate SourceID); category-alias keys must be excluded from the candidate pool so a template cannot be both parents of a crossover", i, tmpl.SourceID, parents)
			}
		}
	}
}

// The any-category fallback (fewer than 2 same-category candidates) must not
// re-append templates already in the pool: a duplicated id survives the
// by-index weighted draw and can be returned as BOTH parents of a crossover.
func TestFactory_SelectParentsFallbackHasNoDuplicates(t *testing.T) {
	f := &Factory{Templates: map[string]*TreeTemplate{
		"cat:a": {SourceID: "cat:a", Category: "cat", Metadata: map[string]any{"fitness": 100.0}},
		"oth:b": {SourceID: "oth:b", Category: "oth", Metadata: map[string]any{"fitness": 1.0}},
		"oth:c": {SourceID: "oth:c", Category: "oth", Metadata: map[string]any{"fitness": 1.0}},
	}}
	f.SetSeed(7)

	for i := range 2000 {
		// Only one same-category candidate → the fallback pool is used.
		parents := f.selectParents("cat", "")
		seen := make(map[string]bool, len(parents))
		for _, id := range parents {
			if seen[id] {
				t.Fatalf("iter %d: duplicate parent %q from the fallback pool (parents=%v)", i, id, parents)
			}
			seen[id] = true
		}
	}
}
