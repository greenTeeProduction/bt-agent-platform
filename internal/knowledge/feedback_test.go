package knowledge

import (
	"testing"
	"time"
)

// =============================================================================
// Feedback — RecordRun, connectLocked, outcomeScore
// =============================================================================

func TestRecordRun_ExistingTree(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:feedback",
		Name:     "Feedback Test",
		Category: "test",
		Fitness:  50.0,
	})

	rec := RunRecord{
		TreeID:   "tree:feedback",
		Task:     "do the thing",
		Outcome:  "success",
		Duration: 2 * time.Second,
		Tools:    []string{"web_search", "calculator"},
		Quality:  85.0,
	}

	kg.RecordRun(rec)

	tree := kg.Trees["tree:feedback"]
	if tree == nil {
		t.Fatal("tree should exist")
	}
	if tree.RunCount != 1 {
		t.Errorf("expected RunCount=1, got %d", tree.RunCount)
	}
	if tree.LastOutcome != "success" {
		t.Errorf("expected LastOutcome='success', got %q", tree.LastOutcome)
	}
	if tree.LastDuration != 2*time.Second {
		t.Errorf("expected LastDuration=2s, got %v", tree.LastDuration)
	}
	// Fitness: 0.9*50 + 0.1*(1.0*100) = 45 + 10 = 55
	if tree.Fitness != 55.0 {
		t.Errorf("expected Fitness=55.0 (EMA), got %.2f", tree.Fitness)
	}
}

func TestRecordRun_Failure(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:fail",
		Name:     "Fail Test",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:fail",
		Task:    "hard task",
		Outcome: "failure",
	})

	tree := kg.Trees["tree:fail"]
	// Fitness: 0.9*50 + 0.1*(0.3*100) = 45 + 3 = 48
	if tree.Fitness != 48.0 {
		t.Errorf("expected Fitness=48.0 after failure, got %.2f", tree.Fitness)
	}
}

func TestRecordRun_ChainFailed(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:cf",
		Name:     "Chain Failed",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:cf",
		Task:    "chain task",
		Outcome: "chain_failed",
	})

	tree := kg.Trees["tree:cf"]
	// Fitness: 0.9*50 + 0.1*(0.1*100) = 45 + 1 = 46
	if tree.Fitness != 46.0 {
		t.Errorf("expected Fitness=46.0 after chain_failed, got %.2f", tree.Fitness)
	}
}

func TestRecordRun_ChainPanic(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:panic",
		Name:     "Panic",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:panic",
		Task:    "panic task",
		Outcome: "chain_panic",
	})

	tree := kg.Trees["tree:panic"]
	// Fitness: 0.9*50 + 0.1*(0.0*100) = 45 + 0 = 45
	if tree.Fitness != 45.0 {
		t.Errorf("expected Fitness=45.0 after chain_panic, got %.2f", tree.Fitness)
	}
}

func TestRecordRun_UnknownOutcome(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:unknown",
		Name:     "Unknown Outcome",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:unknown",
		Task:    "weird",
		Outcome: "something_else",
	})

	tree := kg.Trees["tree:unknown"]
	// Fitness: 0.9*50 + 0.1*(0.5*100) = 45 + 5 = 50
	if tree.Fitness != 50.0 {
		t.Errorf("expected Fitness=50.0 for unknown outcome, got %.2f", tree.Fitness)
	}
}

func TestRecordRun_NilTree(t *testing.T) {
	// RecordRun on nonexistent tree should not panic
	kg := NewKnowledgeGraph()
	kg.RecordRun(RunRecord{
		TreeID:  "tree:nonexistent",
		Task:    "nothing",
		Outcome: "success",
	})
	// Should not have added a tree
	if len(kg.Trees) != 0 {
		t.Errorf("should not create tree for nonexistent ID, got %d trees", len(kg.Trees))
	}
}

func TestRecordRun_UpdatesRunCount(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:multi",
		Name:     "Multi Run",
		Category: "test",
		Fitness:  50.0,
	})

	for i := 0; i < 5; i++ {
		kg.RecordRun(RunRecord{
			TreeID:  "tree:multi",
			Task:    "run ",
			Outcome: "success",
		})
	}

	tree := kg.Trees["tree:multi"]
	if tree.RunCount != 5 {
		t.Errorf("expected RunCount=5, got %d", tree.RunCount)
	}
}

func TestRecordRun_ToolEdges(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:tools", Name: "Tool User", Category: "test"})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:tools",
		Task:    "with tools",
		Outcome: "success",
		Tools:   []string{"search", "calc", "search"}, // duplicate should be dedup'd by connectLocked
	})

	// Should have 2 edges (search, calc) — duplicate search is dedup'd
	edgeCount := 0
	for _, e := range kg.Edges {
		if e.From == "tree:tools" && e.Type == "uses_tool" {
			edgeCount++
		}
	}
	if edgeCount != 2 {
		t.Errorf("expected 2 tool edges (search + calc), got %d", edgeCount)
	}
}

func TestRecordRun_ChainSuccess(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:cs",
		Name:     "Chain Success",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:cs",
		Task:    "chain success",
		Outcome: "chain_success",
	})

	tree := kg.Trees["tree:cs"]
	if tree.Fitness != 55.0 {
		t.Errorf("expected Fitness=55.0 after chain_success, got %.2f", tree.Fitness)
	}
}

// =============================================================================
// outcomeScore edge cases
// =============================================================================

func TestOutcomeScore_Success(t *testing.T) {
	if s := outcomeScore("success"); s != 1.0 {
		t.Errorf("success should score 1.0, got %.2f", s)
	}
}

func TestOutcomeScore_ChainSuccess(t *testing.T) {
	if s := outcomeScore("chain_success"); s != 1.0 {
		t.Errorf("chain_success should score 1.0, got %.2f", s)
	}
}

func TestOutcomeScore_Failure(t *testing.T) {
	if s := outcomeScore("failure"); s != 0.3 {
		t.Errorf("failure should score 0.3, got %.2f", s)
	}
}

func TestOutcomeScore_ChainFailed(t *testing.T) {
	if s := outcomeScore("chain_failed"); s != 0.1 {
		t.Errorf("chain_failed should score 0.1, got %.2f", s)
	}
}

func TestOutcomeScore_ChainPanic(t *testing.T) {
	if s := outcomeScore("chain_panic"); s != 0.0 {
		t.Errorf("chain_panic should score 0.0, got %.2f", s)
	}
}

func TestOutcomeScore_Default(t *testing.T) {
	if s := outcomeScore("anything_else"); s != 0.5 {
		t.Errorf("default should score 0.5, got %.2f", s)
	}
}
