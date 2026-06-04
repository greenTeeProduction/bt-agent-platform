package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reflection"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// ─── BuildAndValidate ───

func TestBuildAndValidate_ValidTree(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "Sequence", Name: "test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	tree, err := BuildAndValidate(node, bb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
}

func TestBuildAndValidate_InvalidTree(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	// An unknown node type fails validation
	node := &evolution.SerializableNode{Type: "UnknownType", Name: "bad"}
	_, err := BuildAndValidate(node, bb)
	if err == nil {
		t.Fatal("expected validation error for unknown node type, got nil")
	}
}

// ─── evaluateGuardCondition ───

func TestEvaluateGuardCondition_NilChainState(t *testing.T) {
	bb := &Blackboard{ChainState: nil}
	// With nil ChainState, non-empty condition returns true (simple existence check)
	if !evaluateGuardCondition("hasAccess", bb) {
		t.Error("expected true when ChainState is nil and condition is non-empty")
	}
}

func TestEvaluateGuardCondition_BoolCondition(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"hasAccess": true}}
	if !evaluateGuardCondition("hasAccess", bb) {
		t.Error("expected true for truthy bool condition")
	}

	bb2 := &Blackboard{ChainState: map[string]any{"hasAccess": false}}
	if evaluateGuardCondition("hasAccess", bb2) {
		t.Error("expected false for falsy bool condition")
	}
}

func TestEvaluateGuardCondition_KeyExists(t *testing.T) {
	// Key exists but not bool = condition met
	bb := &Blackboard{ChainState: map[string]any{"budget_remaining": 100.0}}
	if !evaluateGuardCondition("budget_remaining", bb) {
		t.Error("expected true when key exists (non-bool)")
	}
}

func TestEvaluateGuardCondition_KeyMissing(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"someKey": true}}
	// Missing conditions default to true (assumed met, don't block)
	if !evaluateGuardCondition("nonExistentCondition", bb) {
		t.Error("expected true for unknown condition (assumed met)")
	}
}

func TestEvaluateGuardCondition_EmptyString(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{"hasAccess": true}}
	if !evaluateGuardCondition("", bb) {
		t.Error("expected true for empty condition string")
	}
}

// ─── intSliceFromInterface ───

func TestIntSliceFromInterface_Nil(t *testing.T) {
	result := intSliceFromInterface(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestIntSliceFromInterface_Float64Slice(t *testing.T) {
	result := intSliceFromInterface([]float64{1.0, 2.0, 3.0})
	if len(result) != 3 || result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", result)
	}
}

func TestIntSliceFromInterface_InterfaceSliceFloat64(t *testing.T) {
	result := intSliceFromInterface([]interface{}{1.5, 2.7, 3.0})
	if len(result) != 3 || result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", result)
	}
}

func TestIntSliceFromInterface_InterfaceSliceInt(t *testing.T) {
	result := intSliceFromInterface([]interface{}{1, 2, 3})
	if len(result) != 3 || result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", result)
	}
}

func TestIntSliceFromInterface_MixedTypes(t *testing.T) {
	result := intSliceFromInterface([]interface{}{1, "foo", 3.5, true})
	if len(result) != 2 || result[0] != 1 || result[1] != 3 {
		t.Errorf("expected [1 3], got %v", result)
	}
}

func TestIntSliceFromInterface_Empty(t *testing.T) {
	result := intSliceFromInterface([]interface{}{})
	if result == nil || len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}
}

// ─── runReactiveParallel ───

func TestRunReactiveParallel_ParallelAll_AllSuccess(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelAll, nil, nil, true, ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelAll_OneFails(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelAll, nil, nil, true, ctx)
	if result != -1 {
		t.Errorf("expected -1 (failure), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelAny_FirstWins(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelAny, nil, nil, true, ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelAny_AllFail(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelAny, nil, nil, true, ctx)
	if result != -1 {
		t.Errorf("expected -1 (failure), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelRace_TerminalReturned(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 0 }), // running
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }), // success
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelRace, nil, nil, true, ctx)
	if result != 1 {
		t.Errorf("expected 1 (terminal success), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelRace_AllRunning(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 0 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 0 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelRace, nil, nil, true, ctx)
	if result != 0 {
		t.Errorf("expected 0 (running), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelMonitor_MonitorFails(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }), // monitor fails
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),  // action
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelMonitor, []int{0}, []int{1}, true, ctx)
	if result != -1 {
		t.Errorf("expected -1 (monitor failure -> cancel), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelMonitor_AllSucceed(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelMonitor, []int{0}, []int{1}, true, ctx)
	if result != 1 {
		t.Errorf("expected 1 (all succeed), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelMonitor_ActionFailsCancelOnMonitor(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),  // monitor ok
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }), // action fails
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelMonitor, []int{0}, []int{1}, true, ctx)
	if result != -1 {
		t.Errorf("expected -1 (action failure with cancel), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelMonitor_ActionFailsNoCancel(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),  // monitor ok
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }), // action fails
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, ParallelMonitor, []int{0}, []int{1}, false, ctx)
	if result != -1 {
		t.Errorf("expected -1 (action failure no cancel -> all failed), got %d", result)
	}
}

func TestRunReactiveParallel_DefaultMode_Sequential(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, "unknown_mode", nil, nil, true, ctx)
	if result != 1 {
		t.Errorf("expected 1 (fallback sequential), got %d", result)
	}
}

func TestRunReactiveParallel_DefaultMode_Fails(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 }),
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	result := runReactiveParallel(children, "unknown_mode", nil, nil, true, ctx)
	if result != -1 {
		t.Errorf("expected -1 (fallback sequential failure), got %d", result)
	}
}

func TestRunReactiveParallel_ParallelMonitor_OutOfBoundsIndex(t *testing.T) {
	children := []btcore.Command[Blackboard]{
		btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 }),
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: &Blackboard{}}
	// Index 99 is out of bounds — should be ignored without panic
	result := runReactiveParallel(children, ParallelMonitor, []int{0}, []int{99}, true, ctx)
	if result != 1 {
		t.Errorf("expected 1 (out-of-bounds action index ignored), got %d", result)
	}
}

// ─── ScoreChildren ───

func TestScoreChildren_TieBreakByCost(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}, ChainState: map[string]any{"task_priority": 0.5}}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "highCost", Metadata: map[string]any{"cost_estimate": 0.9, "confidence": 0.5}},
			{Type: "Action", Name: "lowCost", Metadata: map[string]any{"cost_estimate": 0.1, "confidence": 0.5}},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	// With equal weighted scores, lower cost should win (sorted first)
	if scores[0].CostEstimate != 0.1 {
		t.Errorf("expected lowest cost first (0.1), got %f", scores[0].CostEstimate)
	}
	if scores[1].CostEstimate != 0.9 {
		t.Errorf("expected highest cost last (0.9), got %f", scores[1].CostEstimate)
	}
}

func TestScoreChildren_InvalidThenValidSorting(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"hasAccess": false},
	}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{
				Type: "Action", Name: "blocked",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeGuard, Condition: "hasAccess"},
				},
			},
			{Type: "Action", Name: "validChild"},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	// Invalid should sort last, valid first
	if !scores[0].Valid {
		t.Error("expected first score to be valid")
	}
	if scores[1].Valid {
		t.Error("expected second score to be invalid")
	}
}

func TestScoreChildren_EmptyNode(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{Type: "Selector", Name: "empty"}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 0 {
		t.Errorf("expected 0 scores for empty node, got %d", len(scores))
	}
}

func TestScoreChildren_SingleChild(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}
	if !scores[0].Valid {
		t.Error("expected valid score")
	}
	if scores[0].WeightedScore <= 0 {
		t.Errorf("expected positive weighted score, got %f", scores[0].WeightedScore)
	}
}

func TestScoreChildren_GuardConditionBlocks(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"hasAccess": false},
	}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{
				Type: "Action", Name: "restricted",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeGuard, Condition: "hasAccess"},
				},
			},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}
	if scores[0].Valid {
		t.Error("expected invalid score (guard blocked)")
	}
}

func TestScoreChildren_GuardConditionPasses(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"hasAccess": true},
	}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{
				Type: "Action", Name: "allowed",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeGuard, Condition: "hasAccess"},
				},
			},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}
	if !scores[0].Valid {
		t.Error("expected valid score (guard passed)")
	}
}

func TestScoreChildren_MultipleChildrenRanked(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"task_priority": 0.9},
	}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "highCost", Metadata: map[string]any{"cost_estimate": 0.9, "confidence": 0.5}},
			{Type: "Action", Name: "lowCost", Metadata: map[string]any{"cost_estimate": 0.1, "confidence": 0.8}},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	// Both should be valid
	if !scores[0].Valid || !scores[1].Valid {
		t.Error("expected both scores valid")
	}
}

func TestScoreChildren_ChildWithGoalAlignment(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"goal_alignment": 0.85},
	}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "aligned"},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}
	if scores[0].Criteria["goal_alignment"] != 0.85 {
		t.Errorf("expected goal_alignment 0.85, got %f", scores[0].Criteria["goal_alignment"])
	}
}

func TestScoreChildren_ChildWithRiskScore(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "risky", Metadata: map[string]any{"risk_score": 0.9}},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}
	if scores[0].RiskScore != 0.9 {
		t.Errorf("expected risk_score 0.9, got %f", scores[0].RiskScore)
	}
}

func TestScoreChildren_EdgeWeightOverrides(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "Selector", Name: "test",
		Children: []evolution.SerializableNode{
			{
				Type: "Action", Name: "weighted",
				Edges: []evolution.TypedEdge{
					{Type: evolution.EdgeChild, Weight: 0.95},
				},
			},
		},
	}
	scores := ScoreChildren(node, bb, DefaultScoringCriteria())
	if len(scores) != 1 {
		t.Fatalf("expected 1 score, got %d", len(scores))
	}
	if scores[0].Confidence != 0.95 {
		t.Errorf("expected confidence 0.95 from edge weight, got %f", scores[0].Confidence)
	}
}

// ─── BuildUtilitySelector ───

func TestBuildUtilitySelector_NoChildren(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{Type: "UtilitySelector", Name: "empty"}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (failure, no children), got %d", result)
	}
}

func TestBuildUtilitySelector_WithMetadataCriteria(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"task_priority": 0.95},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "withMeta",
		Metadata: map[string]any{
			"urgency_weight":    0.5,
			"cost_weight":       0.3,
			"confidence_weight": 0.2,
			"risk_tolerance":    0.7,
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (MarkSuccessful succeeds), got %d", result)
	}
	if bb.Outcome != string(reflection.Success) {
		t.Errorf("expected outcome=success, got %s", bb.Outcome)
	}
}

func TestBuildUtilitySelector_FailFast_BestFails(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "failFast",
		Metadata: map[string]any{"fail_fast": true},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "hasCachedFitness"},
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	// fail_fast=true should NOT try MarkSuccessful when hasCachedFitness fails
	if result != -1 {
		t.Errorf("expected -1 (fail-fast, no retry), got %d", result)
	}
}

func TestBuildUtilitySelector_FailFastFalse_BestFailsThenNextSucceeds(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "tryNext",
		Metadata: map[string]any{"fail_fast": false},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "hasCachedFitness"}, // fails, ChainState nil
			{Type: "Action", Name: "MarkSuccessful"},   // succeeds
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	// Should retry next best child (MarkSuccessful) and succeed
	if result != 1 {
		t.Errorf("expected 1 (retry next child succeeds), got %d", result)
	}
}

func TestBuildUtilitySelector_FailFastFalse_AllChildrenFail(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "allFail",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "hasCachedFitness"}, // fails
			{Type: "Action", Name: "hasCachedFitness"}, // also fails
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	// All children fail → should return -1
	if result != -1 {
		t.Errorf("expected -1 (all children failed), got %d", result)
	}
}

func TestBuildUtilitySelector_FailFastFalse_FirstSucceeds(t *testing.T) {
	bb := &Blackboard{
		LLM:        &mockLLM{},
		ChainState: map[string]any{"cached_fitness": 0.85},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "firstWins",
		// Default fail_fast=false
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "hasCachedFitness"}, // succeeds (cached_fitness exists)
			{Type: "Action", Name: "MarkSuccessful"},   // also would succeed
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	// First child (hasCachedFitness) should succeed → don't try second
	if result != 1 {
		t.Errorf("expected 1 (first child succeeds), got %d", result)
	}
}

// ─── BuildReactiveParallel ───

func TestBuildReactiveParallel_AllModeSuccess(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "ReactiveParallel", Name: "parallel",
		Metadata: map[string]any{"mode": "all"},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	cmd := BuildReactiveParallel(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (all succeed), got %d", result)
	}
}

func TestBuildReactiveParallel_AbortOnEventDelegates(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "AbortOnEvent", Name: "abort",
		Metadata: map[string]any{
			"event_source": "monitor",
			"event_name":   "failure",
		},
	}
	cmd := BuildReactiveParallel(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	// Should not panic — BuildEventDrivenAbort handles it
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	_ = cmd.Run(ctx)
}

func TestBuildReactiveParallel_AnyMode(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "ReactiveParallel", Name: "parallel",
		Metadata: map[string]any{"mode": "any"},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	cmd := BuildReactiveParallel(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

func TestBuildReactiveParallel_MonitorMode(t *testing.T) {
	bb := &Blackboard{LLM: &mockLLM{}}
	node := &evolution.SerializableNode{
		Type: "ReactiveParallel", Name: "parallel",
		Metadata: map[string]any{
			"mode":                      "monitor",
			"monitor_indices":           []interface{}{0.0},
			"action_indices":            []interface{}{1.0},
			"cancel_on_monitor_failure": true,
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	cmd := BuildReactiveParallel(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (monitor + action both succeed), got %d", result)
	}
}

// ─── DefaultScoringCriteria ───

func TestDefaultScoringCriteria_Values(t *testing.T) {
	c := DefaultScoringCriteria()
	if c.UrgencyWeight != 0.25 {
		t.Errorf("expected UrgencyWeight 0.25, got %f", c.UrgencyWeight)
	}
	if c.CostWeight != 0.25 {
		t.Errorf("expected CostWeight 0.25, got %f", c.CostWeight)
	}
	if c.ConfidenceWeight != 0.25 {
		t.Errorf("expected ConfidenceWeight 0.25, got %f", c.ConfidenceWeight)
	}
	if c.HistoricalWeight != 0.25 {
		t.Errorf("expected HistoricalWeight 0.25, got %f", c.HistoricalWeight)
	}
	if c.GoalAlignmentWeight != 0.0 {
		t.Errorf("expected GoalAlignmentWeight 0.0, got %f", c.GoalAlignmentWeight)
	}
	if c.RiskTolerance != 0.5 {
		t.Errorf("expected RiskTolerance 0.5, got %f", c.RiskTolerance)
	}
}

// ─── UtilityScore structure ───

func TestUtilityScore_StructFields(t *testing.T) {
	s := UtilityScore{
		ChildIndex:    5,
		WeightedScore: 0.85,
		Criteria:      map[string]float64{"urgency": 0.9},
		Confidence:    0.7,
		CostEstimate:  0.2,
		RiskScore:     0.3,
		Valid:         true,
	}
	if s.ChildIndex != 5 {
		t.Errorf("expected ChildIndex 5, got %d", s.ChildIndex)
	}
	if s.WeightedScore != 0.85 {
		t.Errorf("expected WeightedScore 0.85, got %f", s.WeightedScore)
	}
	if !s.Valid {
		t.Error("expected Valid=true")
	}
}
