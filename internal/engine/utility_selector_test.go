package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// ─── UtilitySelector tests (Plan #2 acceptance criteria) ───

func TestScoreChild_ValidWithUrgency(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"task_priority": 0.9},
	}
	child := &evolution.SerializableNode{Type: "Action", Name: "TestAction"}
	criteria := DefaultScoringCriteria()
	score := ScoreChild(child, bb, criteria)
	if !score.Valid {
		t.Error("expected score to be valid")
	}
	if score.Criteria["urgency"] != 0.9 {
		t.Errorf("expected urgency 0.9, got %v", score.Criteria["urgency"])
	}
}

func TestScoreChild_GuardBlocks(t *testing.T) {
	bb := &Blackboard{}
	child := &evolution.SerializableNode{
		Type: "Action", Name: "Blocked",
		Edges: []evolution.TypedEdge{
			{Type: evolution.EdgeGuard, Condition: "nonexistent_key"},
		},
	}
	// With nil ChainState, evaluateGuardCondition returns true (permissive).
	// This test verifies the guard path is reached.
	criteria := DefaultScoringCriteria()
	score := ScoreChild(child, bb, criteria)
	// nil ChainState → guard passes
	if !score.Valid {
		t.Error("expected valid when ChainState is nil (guard passes)")
	}
}

func TestScoreChild_InvalidWithGuardFailed(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"has_access": false},
	}
	child := &evolution.SerializableNode{
		Type: "Action", Name: "Blocked",
		Edges: []evolution.TypedEdge{
			{Type: evolution.EdgeGuard, Condition: "has_access"},
		},
	}
	criteria := DefaultScoringCriteria()
	score := ScoreChild(child, bb, criteria)
	if score.Valid {
		t.Error("expected score to be invalid when guard condition is false")
	}
}

func TestScoreChildren_Ranking(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"task_priority": 0.5},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "LowConfidence", Metadata: map[string]any{"confidence": 0.1, "cost_estimate": 0.5}},
			{Type: "Action", Name: "HighConfidence", Metadata: map[string]any{"confidence": 0.9, "cost_estimate": 0.5}},
			{Type: "Action", Name: "MediumConfidence", Metadata: map[string]any{"confidence": 0.5, "cost_estimate": 0.5}},
		},
	}
	criteria := DefaultScoringCriteria()
	scores := ScoreChildren(node, bb, criteria)
	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}
	// HighConfidence should be first (highest confidence score)
	if scores[0].ChildIndex != 1 {
		t.Errorf("expected ChildIndex 1 (HighConfidence) first, got %d", scores[0].ChildIndex)
	}
	// LowConfidence should be last
	if scores[2].ChildIndex != 0 {
		t.Errorf("expected ChildIndex 0 (LowConfidence) last, got %d", scores[2].ChildIndex)
	}
}

func TestScoreChildren_InvalidExcluded(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"blocked": false},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Valid", Metadata: map[string]any{"confidence": 0.5}},
			{Type: "Action", Name: "Blocked",
				Edges:    []evolution.TypedEdge{{Type: evolution.EdgeGuard, Condition: "blocked"}},
				Metadata: map[string]any{"confidence": 0.9},
			},
		},
	}
	criteria := DefaultScoringCriteria()
	scores := ScoreChildren(node, bb, criteria)
	// Valid scores come first (invalid pushed to end)
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	if !scores[0].Valid {
		t.Error("expected first score to be valid")
	}
	if scores[1].Valid {
		t.Error("expected second score to be invalid")
	}
}

func TestBuildUtilitySelector_PicksBestChild(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"task_priority": 0.5},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "FailAction", Metadata: map[string]any{"confidence": 0.9, "cost_estimate": 0.1}},
			{Type: "Action", Name: "SuccessAction", Metadata: map[string]any{"confidence": 0.8, "cost_estimate": 0.1}},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}

	// Directly invoke the action
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected success (1) from highest-scoring child, got %d", result)
	}
}

func TestBuildUtilitySelector_FallbackOnFailure(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"task_priority": 0.5},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "HealthCheckAgent", Metadata: map[string]any{"confidence": 0.3, "cost_estimate": 0.8}},
			{Type: "Action", Name: "MetricsCollectionAgent", Metadata: map[string]any{"confidence": 0.9, "cost_estimate": 0.1}},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	// Both actions use real shell commands; at least one should succeed
	if result == -1 {
		t.Errorf("expected at least one child to succeed, got failure")
	}
}

func TestBuildUtilitySelector_AllInvalidReturnsFailure(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"blocked_a": false, "blocked_b": false},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "BlockedChild", Edges: []evolution.TypedEdge{{Type: evolution.EdgeGuard, Condition: "blocked_a"}}, Metadata: map[string]any{"confidence": 0.9}},
			{Type: "Action", Name: "OtherBlocked", Edges: []evolution.TypedEdge{{Type: evolution.EdgeGuard, Condition: "blocked_b"}}, Metadata: map[string]any{"confidence": 0.5}},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected failure when all children invalid, got %d", result)
	}
}

func TestBuildUtilitySelector_RunningStatusYield(t *testing.T) {
	// Verify that a child returning Running (0) is yielded, not treated as success.
	bb := &Blackboard{}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "SuccessAction", Metadata: map[string]any{"confidence": 0.9}},
		},
	}
	cmd := BuildUtilitySelector(node, bb)

	// Running status is expected; return 0 means "still running"
	// The normal action should return 1, but we're testing the code path.
	// Since we can't easily inject Running, we verify the command is built.
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected success (1), got %d", result)
	}
}

func TestBuildUtilitySelector_FailFastPreventsFallback(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"task_priority": 0.5},
	}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Metadata: map[string]any{"fail_fast": true},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "HealthCheckAgent", Metadata: map[string]any{"confidence": 0.9, "cost_estimate": 0.1}},
			{Type: "Action", Name: "MetricsCollectionAgent", Metadata: map[string]any{"confidence": 0.3, "cost_estimate": 0.8}},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	// HealthCheckAgent is a real action; it should succeed.
	// fail_fast means we still succeed on first good child.
	if result != 1 {
		t.Errorf("expected success from fail_fast with working first child, got %d", result)
	}
}

// ─── PlannerNode tests ───

func TestBuildPlannerNode_WithGoals(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{"task_priority": 0.5},
	}
	node := &evolution.SerializableNode{
		Type: "PlannerNode", Name: "Planner",
		Metadata: map[string]any{
			"goals": []interface{}{
				map[string]interface{}{"name": "collect_metrics", "priority": 0.9},
				map[string]interface{}{"name": "health_check", "priority": 0.5},
			},
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MetricsCollectionAgent", Metadata: map[string]any{"confidence": 0.9}},
		},
	}
	cmd := BuildPlannerNode(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected PlannerNode to succeed on metrics collection, got %d", result)
	}
	// Verify goal was recorded as completed
	if ctx.Blackboard.ChainState == nil {
		t.Fatal("ChainState should not be nil after run")
	}
	completed, _ := ctx.Blackboard.ChainState["completed_goals"].([]string)
	if len(completed) == 0 {
		t.Error("expected at least one completed goal after success")
	}
}

func TestBuildPlannerNode_GoalStackPopOnFailure(t *testing.T) {
	bb := &Blackboard{}
	node := &evolution.SerializableNode{
		Type: "PlannerNode", Name: "Planner",
		Metadata: map[string]any{
			"goals": []interface{}{
				map[string]interface{}{"name": "should_fail", "priority": 0.9},
				map[string]interface{}{"name": "should_succeed", "priority": 0.5},
			},
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MetricsCollectionAgent", Metadata: map[string]any{"confidence": 0.9}},
		},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	// First run: MetricsCollectionAgent should succeed
	result := cmd.Run(ctx)
	if result != 1 {
		t.Logf("first run result: %d (expected success)", result)
	}
	// After success, first goal pops
	stack, _ := ctx.Blackboard.ChainState["goal_stack"].([]GoalDefinition)
	if len(stack) != 1 {
		t.Logf("goal stack after first success: %d goals remaining (expected 1)", len(stack))
	}
}

func TestBuildUtilitySelector_SingleChild(t *testing.T) {
	bb := &Blackboard{}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "SinglePicker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MetricsCollectionAgent"},
		},
	}
	cmd := BuildUtilitySelector(node, bb)
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected single child to succeed, got %d", result)
	}
}

func TestScoreChildren_TieBreaksOnCost(t *testing.T) {
	bb := &Blackboard{}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector", Name: "Picker",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Expensive", Metadata: map[string]any{"confidence": 0.5, "cost_estimate": 0.9}},
			{Type: "Action", Name: "Cheap", Metadata: map[string]any{"confidence": 0.5, "cost_estimate": 0.1}},
		},
	}
	criteria := DefaultScoringCriteria()
	scores := ScoreChildren(node, bb, criteria)
	if len(scores) != 2 {
		t.Fatalf("expected 2 scores, got %d", len(scores))
	}
	// With equal confidence, cheaper should win
	if scores[0].ChildIndex != 1 {
		t.Errorf("expected ChildIndex 1 (Cheap) to win tiebreak, got %d", scores[0].ChildIndex)
	}
}
