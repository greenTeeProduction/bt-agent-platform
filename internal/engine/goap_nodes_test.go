package engine

import (
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/goap"
	btcore "github.com/rvitorper/go-bt/core"
)

// TestAction_ExecuteGoapStep_LLMFailure uses MockLLM with GenerateErr set.

// ─── PlanGoapActions — edge cases ──────────────────────────────────────────

func TestAction_PlanGoapActions_JSONActions(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	// Simulate JSON-deserialized actions ([]interface{} with map[string]interface{})
	// The actions must form a valid planning chain: preconditions → effects → goal
	bb := &Blackboard{
		Task: "build a deployment pipeline",
		ChainState: map[string]interface{}{
			"goap_actions": []interface{}{
				map[string]interface{}{
					"name": "analyze_requirements",
					"cost": 1.0,
					"preconditions": map[string]interface{}{
						"has_result": false,
					},
					"effects": map[string]interface{}{
						"has_analysis": true,
					},
				},
				map[string]interface{}{
					"name": "execute_build",
					"cost": 2.0,
					"preconditions": map[string]interface{}{
						"has_analysis": true,
					},
					"effects": map[string]interface{}{
						"has_result":  true,
						"task_status": "completed",
					},
				},
			},
			"goap_world_state": goap.WorldState{
				"has_result":  false,
				"task_status": "pending",
				"task":        "build a deployment pipeline",
			},
			"goap_current_goal": goap.NewGoal("task_completed", 1.0,
				goap.WorldState{"task_status": "completed"}),
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 for valid JSON actions, got %d: %s", result, bb.Result)
	}
	if _, ok := bb.ChainState["goap_plan"]; !ok {
		t.Error("goap_plan should be set after successful planning")
	}
	if _, ok := bb.ChainState["goap_steps"]; !ok {
		t.Error("goap_steps should be set after successful planning")
	}
	if bb.Outcome != "success" {
		t.Errorf("expected outcome 'success', got %q", bb.Outcome)
	}
}

func TestAction_PlanGoapActions_JSONActionsInvalidEntry(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	// One entry is not a map (should be skipped), but the remaining valid
	// actions must still form a viable plan chain.
	bb := &Blackboard{
		Task: "build a deployment pipeline",
		ChainState: map[string]interface{}{
			"goap_actions": []interface{}{
				map[string]interface{}{
					"name": "analyze_requirements",
					"cost": 1.0,
					"preconditions": map[string]interface{}{
						"has_result": false,
					},
					"effects": map[string]interface{}{
						"has_analysis": true,
					},
				},
				"not_a_map", // invalid entry — should be skipped
				map[string]interface{}{
					"name": "execute_build",
					"cost": 2.0,
					"preconditions": map[string]interface{}{
						"has_analysis": true,
					},
					"effects": map[string]interface{}{
						"has_result":  true,
						"task_status": "completed",
					},
				},
			},
			"goap_world_state": goap.WorldState{
				"has_result":  false,
				"task_status": "pending",
				"task":        "build a deployment pipeline",
			},
			"goap_current_goal": goap.NewGoal("task_completed", 1.0,
				goap.WorldState{"task_status": "completed"}),
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 despite invalid entry, got %d: %s", result, bb.Result)
	}
}

func TestAction_PlanGoapActions_JSONActionsEmptyAfterFilter(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "build something",
		ChainState: map[string]interface{}{
			"goap_actions": []interface{}{
				"not_a_map",
				"also_not_a_map",
			},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for all-invalid JSON actions, got %d", result)
	}
	if !stringContains(bb.Result, "no valid actions") {
		t.Error("result should mention no valid actions")
	}
}

func TestAction_PlanGoapActions_CustomGoal(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "build a deployment pipeline",
		ChainState: map[string]interface{}{
			"goap_actions": []goap.Action{
				{
					Name:          "analyze_requirements",
					Cost:          1.0,
					Preconditions: goap.WorldState{"has_result": false},
					Effects:       goap.WorldState{"has_analysis": true},
				},
				{
					Name:          "execute_build",
					Cost:          2.0,
					Preconditions: goap.WorldState{"has_analysis": true},
					Effects:       goap.WorldState{"has_result": true, "task_status": "completed"},
				},
			},
			"goap_current_goal": goap.NewGoal("task_completed", 1.0,
				goap.WorldState{"task_status": "completed"}),
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 with custom goal, got %d: %s", result, bb.Result)
	}
	if _, ok := bb.ChainState["goap_plan"]; !ok {
		t.Error("goap_plan should be set")
	}
}

func TestAction_PlanGoapActions_WorldStateFromTask(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	// No world state in ChainState — should initialize from task.
	// The auto-init sets has_result=false, task_status=pending, task=<task>.
	// Use actions whose preconditions are satisfied by this default state.
	bb := &Blackboard{
		Task: "analyze the quarterly results",
		ChainState: map[string]interface{}{
			"goap_actions": []goap.Action{
				{
					Name:          "analyze_requirements",
					Cost:          1.0,
					Preconditions: goap.WorldState{"has_result": false},
					Effects:       goap.WorldState{"has_analysis": true},
				},
				{
					Name: "execute_general",
					Cost: 1.0,
					// Remove task_type precondition — auto-init doesn't set it
					Preconditions: goap.WorldState{"has_analysis": true},
					Effects:       goap.WorldState{"has_result": true, "task_status": "completed"},
				},
			},
			"goap_current_goal": goap.NewGoal("task_completed", 1.0,
				goap.WorldState{"task_status": "completed"}),
			// No goap_world_state — should auto-init
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 with auto-initialized world state, got %d: %s", result, bb.Result)
	}
	if _, ok := bb.ChainState["goap_plan"]; !ok {
		t.Error("goap_plan should be set")
	}
}

func TestAction_PlanGoapActions_NoPlanFound(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	// Impossible goal with no matching actions
	bb := &Blackboard{
		Task: "impossible task",
		ChainState: map[string]interface{}{
			"goap_actions": []goap.Action{
				{
					Name:          "simple_action",
					Cost:          1.0,
					Preconditions: goap.WorldState{},
					Effects:       goap.WorldState{"result": "done"},
				},
			},
			"goap_current_goal": goap.NewGoal("impossible", 1.0,
				goap.WorldState{"impossible_flag": true}),
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for impossible goal, got %d", result)
	}
	if !stringContains(bb.Result, "could not find a plan") {
		t.Error("result should mention plan not found")
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected failure outcome, got %q", bb.Outcome)
	}
}

func TestAction_PlanGoapActions_WrongActionsType(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	// goap_actions is a string — not []goap.Action or []interface{}
	bb := &Blackboard{
		Task: "build something",
		ChainState: map[string]interface{}{
			"goap_actions": "not_an_action_slice",
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for wrong actions type, got %d", result)
	}
	if !stringContains(bb.Result, "no valid actions") {
		t.Error("result should mention no valid actions")
	}
}

func TestAction_PlanGoapActions_WithGoapConfig(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "build a deployment pipeline",
		ChainState: map[string]interface{}{
			"goap_actions": []goap.Action{
				{
					Name:          "analyze_requirements",
					Cost:          1.0,
					Preconditions: goap.WorldState{"has_result": false},
					Effects:       goap.WorldState{"has_analysis": true},
				},
				{
					Name:          "execute_build",
					Cost:          2.0,
					Preconditions: goap.WorldState{"has_analysis": true},
					Effects:       goap.WorldState{"has_result": true, "task_status": "completed"},
				},
			},
			"goap_world_state": goap.WorldState{
				"has_result":  false,
				"task_status": "pending",
				"task":        "build a deployment pipeline",
			},
			"goap_current_goal": goap.NewGoal("task_completed", 1.0,
				goap.WorldState{"task_status": "completed"}),
			"goap_config": goap.GOAPTreeConfig{
				MaxPlannerDepth: 100,
				MaxPlannerNodes: 10000,
			},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 with custom config, got %d: %s", result, bb.Result)
	}
	if _, ok := bb.ChainState["goap_plan"]; !ok {
		t.Error("goap_plan should be set")
	}
}

// ─── ExecuteGoapStep — LLM and world-state paths ──────────────────────────

func TestAction_ExecuteGoapStep_WithLLM(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	stepPlan := &goap.Plan{
		Goal: goap.NewGoal("task_completed", 1.0,
			goap.WorldState{"task_status": "completed"}),
		Steps: []goap.Action{
			{
				Name:    "analyze_requirements",
				Effects: goap.WorldState{"has_analysis": true},
			},
			{
				Name:    "execute_build",
				Effects: goap.WorldState{"has_result": true, "task_status": "completed"},
			},
		},
	}
	bb := &Blackboard{
		Task: "build a pipeline",
		LLM:  &MockLLM{},
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
			"goap_steps":      []string{"analyze_requirements", "execute_build"},
			"goap_plan":       stepPlan,
			"goap_world_state": goap.WorldState{
				"task":        "build a pipeline",
				"has_result":  false,
				"task_status": "pending",
			},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 for LLM-based step execution, got %d", result)
	}
	if bb.Outcome != "running" {
		t.Errorf("expected outcome 'running', got %q", bb.Outcome)
	}
	// Should have incremented step index
	idx, ok := bb.ChainState["goap_step_index"].(int)
	if !ok {
		t.Fatal("goap_step_index should be an int")
	}
	if idx != 1 {
		t.Errorf("expected step index 1, got %d", idx)
	}
	// Should have updated world state with step effects
	ws, ok := bb.ChainState["goap_world_state"].(goap.WorldState)
	if !ok {
		t.Fatal("goap_world_state should exist")
	}
	if v, ok := ws["has_analysis"]; !ok || v != true {
		t.Error("world state should reflect step effects (has_analysis=true)")
	}
	// Should track executed steps
	executed, ok := bb.ChainState["goap_executed_steps"].([]string)
	if !ok || len(executed) != 1 || executed[0] != "analyze_requirements" {
		t.Errorf("expected executed_steps [analyze_requirements], got %v", executed)
	}
}

func TestAction_ExecuteGoapStep_WithNoPlan(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	bb := &Blackboard{
		Task: "build a pipeline",
		LLM:  &MockLLM{},
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
			"goap_steps":      []string{"analyze_requirements"},
			// No goap_plan — safe fallback
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 even without plan, got %d", result)
	}
	if bb.Outcome != "running" {
		t.Errorf("expected outcome 'running', got %q", bb.Outcome)
	}
}

func TestAction_ExecuteGoapStep_LLMFailure(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	llm := &MockLLM{GenerateErr: errors.New("LLM unavailable")}
	bb := &Blackboard{
		Task: "build a pipeline",
		LLM:  llm,
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
			"goap_steps":      []string{"analyze_requirements"},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for LLM failure, got %d", result)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected failure outcome, got %q", bb.Outcome)
	}
}

func TestAction_ExecuteGoapStep_NoLLM(t *testing.T) {
	fn := GetAction("ExecuteGoapStep")
	if fn == nil {
		t.Fatal("ExecuteGoapStep action not registered")
	}
	bb := &Blackboard{
		Task: "build a pipeline",
		// No LLM — should fall through to no-LLM path
		ChainState: map[string]interface{}{
			"goap_step_index": 0,
			"goap_steps":      []string{"analyze_requirements"},
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1 without LLM, got %d", result)
	}
	if bb.Outcome != "running" {
		t.Errorf("expected outcome 'running', got %q", bb.Outcome)
	}
	lastResult, ok := bb.ChainState["goap_last_step_result"].(string)
	if !ok || !stringContains(lastResult, "marked complete") {
		t.Errorf("expected no-LLM fallback message, got %q", lastResult)
	}
}

// ─── SetupGoapTools — nil ChainState ───────────────────────────────────────

func TestAction_SetupGoapTools_NilChainState(t *testing.T) {
	fn := GetAction("SetupGoapTools")
	if fn == nil {
		t.Fatal("SetupGoapTools action not registered")
	}
	bb := &Blackboard{Task: "build a pipeline"}
	bb.ChainState = nil
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
	if bb.ChainState == nil {
		t.Fatal("ChainState should be initialized")
	}
	if _, ok := bb.ChainState["goap_actions"]; !ok {
		t.Error("goap_actions should be set")
	}
}
