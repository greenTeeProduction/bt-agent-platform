package evolution

import "testing"

func TestSpecialistRegistry_ObserveKeepsBestArchetype(t *testing.T) {
	registry := NewSpecialistRegistry()
	weak := &SerializableNode{Type: "Sequence", Name: "WeakGOAP"}
	strong := &SerializableNode{Type: "Sequence", Name: "StrongGOAP"}

	registry.Observe(&EvolutionMetadata{
		TreeID:  "weak-goap",
		Tags:    []string{"specialist:goap"},
		Fitness: FitnessRecord{Score: 0.72, Validated: true},
	}, weak, 3)
	registry.Observe(&EvolutionMetadata{
		TreeID:  "strong-goap",
		Tags:    []string{"specialist:goap"},
		Fitness: FitnessRecord{Score: 0.91, Validated: true},
	}, strong, 4)
	registry.Observe(&EvolutionMetadata{
		TreeID:  "weaker-late-goap",
		Tags:    []string{"specialist:goap"},
		Fitness: FitnessRecord{Score: 0.80, Validated: true},
	}, &SerializableNode{Type: "Sequence", Name: "WeakerLateGOAP"}, 5)

	arch, ok := registry.Archetypes["goap"]
	if !ok {
		t.Fatal("expected goap archetype to be stored")
	}
	if arch.ID != "strong-goap" {
		t.Fatalf("expected strongest archetype to be retained, got %q", arch.ID)
	}
	if arch.LastSeenGen != 5 {
		t.Fatalf("expected last seen generation to refresh to 5, got %d", arch.LastSeenGen)
	}
	if arch.Tree == nil || arch.Tree.Name != "StrongGOAP" {
		t.Fatalf("expected stored tree clone for strongest archetype, got %#v", arch.Tree)
	}
}

func TestSpecialistRegistry_ExtinctSpecialists(t *testing.T) {
	registry := NewSpecialistRegistry()
	registry.Observe(&EvolutionMetadata{
		TreeID:  "security-1",
		Tags:    []string{"specialist:security"},
		Fitness: FitnessRecord{Score: 0.85, Validated: true},
	}, &SerializableNode{Type: "Sequence", Name: "SecurityAudit"}, 2)

	missing := registry.ExtinctSpecialists(map[string]int{"goap": 2}, 9, 5, 0.7)
	if len(missing) != 1 {
		t.Fatalf("expected one extinct specialist, got %d: %#v", len(missing), missing)
	}
	if missing[0].Type != "security" {
		t.Fatalf("expected security specialist to be extinct, got %q", missing[0].Type)
	}
}

func TestSpecialistRegistry_ResurrectTagsClone(t *testing.T) {
	registry := NewSpecialistRegistry()
	original := &SerializableNode{Type: "Sequence", Name: "CodeReviewer", Children: []SerializableNode{{Type: "Action", Name: "ReviewGoCode"}}}
	registry.Observe(&EvolutionMetadata{
		TreeID:  "code-reviewer-1",
		Tags:    []string{"specialist:code_reviewer"},
		Fitness: FitnessRecord{Score: 0.88, Validated: true},
	}, original, 1)

	ind, meta, ok := registry.Resurrect("code_reviewer", 8)
	if !ok {
		t.Fatal("expected resurrection to succeed")
	}
	if ind.Tree == nil || ind.Tree.Name != "CodeReviewer" {
		t.Fatalf("unexpected resurrected tree: %#v", ind.Tree)
	}
	if ind.Tree == original {
		t.Fatal("expected resurrected tree to be a clone, not the original pointer")
	}
	if meta.TreeID != "resurrected:code-reviewer-1:g8" {
		t.Fatalf("unexpected resurrected TreeID: %q", meta.TreeID)
	}
	if !meta.IsResurrected() || !meta.IsSpecialist() {
		t.Fatalf("expected resurrected specialist tags, got %#v", meta.Tags)
	}
}

func TestEvolutionMetadata_RecordFitnessCountsRegressions(t *testing.T) {
	meta := &EvolutionMetadata{Fitness: FitnessRecord{Score: 0.8}}
	meta.RecordFitness(0.7, true)
	if meta.Fitness.Score != 0.7 {
		t.Fatalf("expected updated score 0.7, got %.2f", meta.Fitness.Score)
	}
	if meta.Fitness.Regressions != 1 {
		t.Fatalf("expected regression count 1, got %d", meta.Fitness.Regressions)
	}

	meta.RecordFitness(0.9, true)
	if meta.Fitness.Regressions != 1 {
		t.Fatalf("expected regression count to remain 1 after improvement, got %d", meta.Fitness.Regressions)
	}
}
