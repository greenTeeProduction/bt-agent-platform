package engine

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestBuildPlannerNode_NilMetadata(t *testing.T) {
	bb := &Blackboard{}
	node := &evolution.SerializableNode{
		Type:     "PlannerNode",
		Children: []evolution.SerializableNode{},
	}
	cmd := BuildPlannerNode(node, bb)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != -1 {
		t.Errorf("expected -1 (no valid children), got %d", result)
	}
}

func TestBuildPlannerNode_GoalInitOnEmptyStack(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Metadata: map[string]any{
			"goals": []any{
				map[string]any{
					"name":     "goal1",
					"priority": float64(1.0),
				},
				map[string]any{
					"name":        "goal2",
					"priority":    float64(0.5),
					"description": "test goal 2",
					"preconditions": []any{
						"condition_a",
					},
				},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	_ = BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	cmd := BuildPlannerNode(node, bb)

	// First tick — goals should be initialized
	result := cmd.Run(ctx)
	// With a single AlwaysSucceed child, should succeed and pop goal
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

func TestBuildPlannerNode_MaxGoalDepth(t *testing.T) {
	bb := &Blackboard{
		ChainState: make(map[string]any),
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Metadata: map[string]any{
			"max_goal_depth": float64(2),
			"goals": []any{
				map[string]any{"name": "g1", "priority": float64(1.0)},
				map[string]any{"name": "g2", "priority": float64(0.9)},
				map[string]any{"name": "g3", "priority": float64(0.8)},
				map[string]any{"name": "g4", "priority": float64(0.7)},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

func TestBuildPlannerNode_NilChainStateInit(t *testing.T) {
	bb := &Blackboard{
		ChainState: nil,
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Metadata: map[string]any{
			"goals": []any{
				map[string]any{"name": "g1", "priority": float64(1.0)},
			},
		},
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

func TestBuildPlannerNode_ReadGoalsFromChainState(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"goals": []GoalDefinition{
				{Name: "cs_goal", Priority: 0.8, Description: "from chainstate"},
			},
		},
	}
	childNode := evolution.SerializableNode{
		Type: "AlwaysSucceed",
	}
	node := &evolution.SerializableNode{
		Type:     "PlannerNode",
		Children: []evolution.SerializableNode{childNode},
	}
	cmd := BuildPlannerNode(node, bb)
	ctx := btcore.NewBTContext(context.Background(), bb)
	result := cmd.Run(ctx)
	if result != 1 {
		t.Errorf("expected 1 (success), got %d", result)
	}
}

// ----- readGoals tests -----

func TestReadGoals_NilMetadata(t *testing.T) {
	goals := readGoals(&evolution.SerializableNode{}, &Blackboard{})
	if goals != nil {
		t.Errorf("expected nil, got %v", goals)
	}
}

func TestReadGoals_FromMetadata(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"goals": []any{
				map[string]any{
					"name":     "meta_goal",
					"priority": float64(0.9),
				},
			},
		},
	}
	goals := readGoals(node, &Blackboard{})
	if len(goals) != 1 {
		t.Fatalf("expected 1 goal, got %d", len(goals))
	}
	if goals[0].Name != "meta_goal" {
		t.Errorf("expected Name='meta_goal', got %q", goals[0].Name)
	}
	if goals[0].Priority != 0.9 {
		t.Errorf("expected Priority=0.9, got %f", goals[0].Priority)
	}
}

func TestReadGoals_NonSliceMetadata(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"goals": "not_a_slice",
		},
	}
	goals := readGoals(node, &Blackboard{})
	if goals != nil {
		t.Errorf("expected nil for non-slice metadata, got %v", goals)
	}
}

// readGoals also reads from ChainState — need to set it before readGoals
func TestReadGoals_FromChainState(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: nil,
	}
	// Use interface{} intermediate to simulate JSON-decoded ChainState
	bb := &Blackboard{
		ChainState: map[string]any{
			"goals": []interface{}{
				map[string]interface{}{
					"name":     "cs_goal",
					"priority": float64(0.7),
				},
			},
		},
	}
	goals := readGoals(node, bb)
	// readGoals only reads []GoalDefinition from ChainState, not []interface{}
	// This path is only hit when the value was stored as []GoalDefinition directly
	if goals != nil {
		t.Fatalf("expected nil from ChainState read ([]interface{} not matchable as []GoalDefinition), got %d items", len(goals))
	}
}

func TestReadGoals_ChainStateNoGoals(t *testing.T) {
	bb := &Blackboard{
		ChainState: map[string]any{
			"other_key": "value",
		},
	}
	goals := readGoals(&evolution.SerializableNode{}, bb)
	if goals != nil {
		t.Errorf("expected nil when no goals key in ChainState, got %v", goals)
	}
}

// ----- stringFromMap tests -----

func TestStringFromMap_Exists(t *testing.T) {
	m := map[string]interface{}{"name": "test-name", "other": 42}
	result := stringFromMap(m, "name")
	if result != "test-name" {
		t.Errorf("expected 'test-name', got %q", result)
	}
}

func TestStringFromMap_Missing(t *testing.T) {
	m := map[string]interface{}{"other": 42}
	result := stringFromMap(m, "name")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestStringFromMap_WrongType(t *testing.T) {
	m := map[string]interface{}{"name": 123}
	result := stringFromMap(m, "name")
	if result != "" {
		t.Errorf("expected empty string for non-string value, got %q", result)
	}
}

// ----- floatFromMap tests -----

func TestFloatFromMap_Exists(t *testing.T) {
	m := map[string]interface{}{"priority": 0.85}
	result := floatFromMap(m, "priority")
	if result != 0.85 {
		t.Errorf("expected 0.85, got %f", result)
	}
}

func TestFloatFromMap_Missing(t *testing.T) {
	m := map[string]interface{}{}
	result := floatFromMap(m, "priority")
	if result != 0.5 {
		t.Errorf("expected default 0.5, got %f", result)
	}
}

func TestFloatFromMap_WrongType(t *testing.T) {
	m := map[string]interface{}{"priority": "high"}
	result := floatFromMap(m, "priority")
	if result != 0.5 {
		t.Errorf("expected default 0.5 for string value, got %f", result)
	}
}

// Test readGoals with empty goals list (non-nil but length 0)
func TestReadGoals_EmptyGoalsSlice(t *testing.T) {
	node := &evolution.SerializableNode{
		Metadata: map[string]any{
			"goals": []any{},
		},
	}
	goals := readGoals(node, &Blackboard{})
	if len(goals) != 0 {
		t.Errorf("expected empty goals slice, got %d items", len(goals))
	}
}
