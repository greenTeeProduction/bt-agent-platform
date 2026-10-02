package knowledge

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestFeedbackDistinctSameTreeRunsSurviveAndAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.json")
	seed := NewKnowledgeGraph()
	seed.Register(&TreeMeta{ID: "tree:shared", Fitness: 50, RunCount: 10})
	if err := seed.SaveFeedback(path); err != nil {
		t.Fatal(err)
	}
	writers := make([]*KnowledgeGraph, 0, 3)
	for _, outcome := range []string{"success", "failure", "chain_success"} {
		kg := NewKnowledgeGraph()
		if err := kg.LoadFeedback(path); err != nil {
			t.Fatal(err)
		}
		kg.RecordRun(RunRecord{TreeID: "tree:shared", Outcome: outcome})
		writers = append(writers, kg)
	}
	for _, kg := range writers {
		for range 2 {
			if err := kg.SaveFeedback(path); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := readFeedbackSnapshot(t, path).Trees["tree:shared"]
	wantFitness := 0.9*(0.9*(0.9*50+10)+3) + 10
	if got.RunCount != 13 || len(got.RecentRuns) != 3 || math.Abs(got.Fitness-wantFitness) > 1e-9 {
		t.Fatalf("lost or duplicated evidence: %+v, fitness want %v", got, wantFitness)
	}
}

func TestFeedbackEvolutionCountsAndBestMetadataSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feedback.json")
	seed := NewKnowledgeGraph()
	seed.RegisterEvolved("base", "winner", 10, 70)
	if err := seed.SaveFeedback(path); err != nil {
		t.Fatal(err)
	}
	a, b := NewKnowledgeGraph(), NewKnowledgeGraph()
	for _, kg := range []*KnowledgeGraph{a, b} {
		if err := kg.LoadFeedback(path); err != nil {
			t.Fatal(err)
		}
	}
	a.RegisterEvolved("base", "winner", 12, 90)
	b.RegisterEvolved("base", "winner", 11, 80)
	for _, kg := range []*KnowledgeGraph{a, b, a, b} {
		if err := kg.SaveFeedback(path); err != nil {
			t.Fatal(err)
		}
	}
	for _, kg := range []*KnowledgeGraph{a, b} {
		live := kg.Trees["winner"]
		if live.StructuralFitness != 90 || live.NodeCount != 12 || kg.EvolvedFitnessImproves("winner", 80) {
			t.Fatalf("save retained stale live winner: %+v", live)
		}
	}
	got := readFeedbackSnapshot(t, path).Trees["winner"]
	if got.EvolvedCount != 3 || got.StructuralFitness != 90 || got.NodeCount != 12 {
		t.Fatalf("lost evolution evidence or regressed winning metadata: %+v", got)
	}
}

func TestFeedbackFailedSaveRetainsBoundedPendingEvidence(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "shared", Fitness: 50})
	for range 100 {
		kg.RecordRun(RunRecord{TreeID: "shared", Outcome: "success"})
	}
	if err := kg.SaveFeedback(filepath.Join(blocker, "feedback.json")); err == nil {
		t.Fatal("expected persistence failure")
	}
	if n := len(kg.feedbackPersist.pending["shared"].recent); n > maxRunHistory {
		t.Fatalf("pending history is unbounded: %d", n)
	}
	path := filepath.Join(dir, "feedback.json")
	for range 2 {
		if err := kg.SaveFeedback(path); err != nil {
			t.Fatal(err)
		}
	}
	got := readFeedbackSnapshot(t, path).Trees["shared"]
	if got.RunCount != 100 || len(got.RecentRuns) != maxRunHistory || math.Abs(got.Fitness-(100-50*math.Pow(0.9, 100))) > 1e-9 {
		t.Fatalf("retry lost/duplicated observations: %+v", got)
	}
}
