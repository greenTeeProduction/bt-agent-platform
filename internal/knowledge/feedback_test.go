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

	for range 5 {
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
// RecordRun must mark feedback dirty (NotebookLM research finding: gardener's
// evolved-tree feedback never got flagged dirty, so FlushFeedback never
// persisted it — RecordRun is the one call site every feedback-producing path
// (scheduler, gardener) already funnels through, so marking dirty there closes
// the gap for all callers at once instead of requiring every call site to
// remember MarkFeedbackDirty individually).
// =============================================================================

// TestRecordRun_MarksFeedbackDirty pins the genuine-execution path: once
// persistence is configured, a real RecordRun call must flip the debounced
// writer's dirty flag so a later FlushFeedback actually persists it.
func TestRecordRun_MarksFeedbackDirty(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:dirty", Name: "Dirty Test", Category: "test"})
	kg.ConfigureFeedbackPersistence(t.TempDir()+"/feedback.json", time.Minute)

	if kg.feedbackPersist.dirty {
		t.Fatal("dirty flag should start false before any RecordRun")
	}

	kg.RecordRun(RunRecord{TreeID: "tree:dirty", Task: "run", Outcome: "success"})

	if !kg.feedbackPersist.dirty {
		t.Error("RecordRun did not mark feedback dirty — FlushFeedback will silently skip this write")
	}
}

// TestRecordRun_Evolved_MarksFeedbackDirty pins the evolved-tree path
// specifically: this is the path the gardener's recordEvolvedRun (evolve_v2.go)
// exercises, and the one NotebookLM research found was falling through —
// evolved-tree feedback was computed in memory but never flagged dirty, so it
// was silently dropped on the next restart.
func TestRecordRun_Evolved_MarksFeedbackDirty(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{ID: "tree:evolved-dirty", Name: "Evolved Dirty Test", Category: "test"})
	kg.ConfigureFeedbackPersistence(t.TempDir()+"/feedback.json", time.Minute)

	if kg.feedbackPersist.dirty {
		t.Fatal("dirty flag should start false before any RecordRun")
	}

	kg.RecordRun(RunRecord{TreeID: "tree:evolved-dirty", Task: "evolve", Outcome: "evolved", Quality: 75.0})

	if !kg.feedbackPersist.dirty {
		t.Error("RecordRun(\"evolved\") did not mark feedback dirty — evolved-tree feedback will not persist")
	}
}

// =============================================================================
// Evolved fitness write-back (QD/island elites → KnowledgeGraph)
// =============================================================================

// An "evolved" outcome writes a winning QD/island elite's structural fitness
// into the tree's dedicated StructuralFitness field — bypassing the EMA so
// fitness-aware discovery can surface archive-improved trees on the very next
// run — while leaving the runtime-success EMA (Fitness) untouched.
func TestRecordRun_Evolved_BumpsStructuralFitness(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:evolved",
		Name:     "Evolved Elite",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:evolved",
		Task:    "illuminate behavior space",
		Outcome: "evolved",
		Quality: 80.0,
	})

	tree := kg.Trees["tree:evolved"]
	// An evolution pass is NOT a genuine execution: it must not inflate
	// RunCount (which drives cold-start confidence weighting). It is counted
	// separately in EvolvedCount instead.
	if tree.RunCount != 0 {
		t.Errorf("expected RunCount=0 (evolved is not a genuine execution), got %d", tree.RunCount)
	}
	if tree.EvolvedCount != 1 {
		t.Errorf("expected EvolvedCount=1, got %d", tree.EvolvedCount)
	}
	if tree.LastOutcome != "evolved" {
		t.Errorf("expected LastOutcome='evolved', got %q", tree.LastOutcome)
	}
	// StructuralFitness captures the elite's structural fitness (Quality).
	if tree.StructuralFitness != 80.0 {
		t.Errorf("expected StructuralFitness=80.0 (elite write-back), got %.2f", tree.StructuralFitness)
	}
	// The runtime-success EMA is left exactly as registered — an evolution pass
	// must never overwrite what genuine executions measured.
	if tree.Fitness != 50.0 {
		t.Errorf("expected Fitness=50.0 (runtime EMA untouched by evolved pass), got %.2f", tree.Fitness)
	}
}

// RunCount must mean "genuine executions" only. Interleaved evolution passes
// bump EvolvedCount and never RunCount, so cold-start confidence weighting is
// not inflated by synthetic archive write-backs.
func TestRecordRun_Evolved_DoesNotIncrementRunCount(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:counts",
		Name:     "Counts",
		Category: "test",
		Fitness:  50.0,
	})

	// Two genuine executions.
	kg.RecordRun(RunRecord{TreeID: "tree:counts", Task: "run", Outcome: "success"})
	kg.RecordRun(RunRecord{TreeID: "tree:counts", Task: "run", Outcome: "failure"})

	// Three evolution passes interleaved.
	kg.RecordRun(RunRecord{TreeID: "tree:counts", Task: "evolve", Outcome: "evolved", Quality: 60.0})
	kg.RecordRun(RunRecord{TreeID: "tree:counts", Task: "evolve", Outcome: "evolved", Quality: 70.0})
	kg.RecordRun(RunRecord{TreeID: "tree:counts", Task: "evolve", Outcome: "evolved", Quality: 80.0})

	// One more genuine execution.
	kg.RecordRun(RunRecord{TreeID: "tree:counts", Task: "run", Outcome: "success"})

	tree := kg.Trees["tree:counts"]
	if tree.RunCount != 3 {
		t.Errorf("expected RunCount=3 (genuine executions only), got %d", tree.RunCount)
	}
	if tree.EvolvedCount != 3 {
		t.Errorf("expected EvolvedCount=3 (evolution passes only), got %d", tree.EvolvedCount)
	}
}

// Structural write-back is monotone: after a strong elite lands, a weaker one
// never regresses StructuralFitness — and the runtime EMA stays untouched.
func TestRecordRun_Evolved_Monotone(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:evolved-mono",
		Name:     "Evolved Monotone",
		Category: "test",
		Fitness:  80.0,
	})

	// A strong elite illuminates the tree at structural fitness 80.
	kg.RecordRun(RunRecord{
		TreeID:  "tree:evolved-mono",
		Task:    "strong elite",
		Outcome: "evolved",
		Quality: 80.0,
	})
	// A later weaker elite must not regress StructuralFitness.
	kg.RecordRun(RunRecord{
		TreeID:  "tree:evolved-mono",
		Task:    "weaker elite",
		Outcome: "evolved",
		Quality: 60.0,
	})

	tree := kg.Trees["tree:evolved-mono"]
	if tree.StructuralFitness != 80.0 {
		t.Errorf("expected StructuralFitness to stay 80.0 (monotone write-back), got %.2f", tree.StructuralFitness)
	}
	if tree.Fitness != 80.0 {
		t.Errorf("expected Fitness to stay 80.0 (runtime EMA untouched), got %.2f", tree.Fitness)
	}
}

// Structural write-back is clamped into [0,100] regardless of the reported
// elite fitness, and never touches the runtime EMA.
func TestRecordRun_Evolved_Clamped(t *testing.T) {
	kg := NewKnowledgeGraph()
	kg.Register(&TreeMeta{
		ID:       "tree:evolved-clamp",
		Name:     "Evolved Clamped",
		Category: "test",
		Fitness:  50.0,
	})

	kg.RecordRun(RunRecord{
		TreeID:  "tree:evolved-clamp",
		Task:    "overflow elite",
		Outcome: "evolved",
		Quality: 150.0,
	})

	tree := kg.Trees["tree:evolved-clamp"]
	if tree.StructuralFitness != 100.0 {
		t.Errorf("expected StructuralFitness=100.0 (clamped to [0,100]), got %.2f", tree.StructuralFitness)
	}
	if tree.Fitness != 50.0 {
		t.Errorf("expected Fitness=50.0 (runtime EMA untouched), got %.2f", tree.Fitness)
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

// TestOutcomeScore_SharedVocabulary pins outcomeScore to the scheduler's
// outcome vocabulary: healthy non-"success" outcomes (no_change, degraded —
// see internal/agent's isHealthyOutcome) must score as healthy runs, the
// rate-limit carryover is a neutral pause (it is not evidence about tree
// quality either way), and the named bad outcomes must score at or below
// "failure" — the pre-fix default of 0.5 ranked a panicked or timed-out run
// ABOVE a plain failure and penalized healthy analysis-only runs to half
// credit.
func TestOutcomeScore_SharedVocabulary(t *testing.T) {
	fail := outcomeScore("failure")
	tests := []struct {
		outcome string
		min     float64
		max     float64
	}{
		{"no_change", 0.8, 1.0},                // healthy: analysis-only
		{"degraded", 0.6, 1.0},                 // healthy: deterministic fallback
		{"goap_fusion_rate_limited", 0.5, 0.5}, // neutral pause
		{"panic", 0.0, fail},                   // at or below failure
		{"timeout", 0.0, fail},                 // at or below failure
		{"failed", fail, fail},                 // alias of failure
	}
	for _, tc := range tests {
		if s := outcomeScore(tc.outcome); s < tc.min || s > tc.max {
			t.Errorf("outcomeScore(%q) = %.2f, want in [%.2f, %.2f]", tc.outcome, s, tc.min, tc.max)
		}
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
