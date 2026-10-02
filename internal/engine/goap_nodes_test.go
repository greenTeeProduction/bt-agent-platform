package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		ChainState: map[string]any{
			"goap_actions": []any{
				map[string]any{
					"name": "analyze_requirements",
					"cost": 1.0,
					"preconditions": map[string]any{
						"has_result": false,
					},
					"effects": map[string]any{
						"has_analysis": true,
					},
				},
				map[string]any{
					"name": "execute_build",
					"cost": 2.0,
					"preconditions": map[string]any{
						"has_analysis": true,
					},
					"effects": map[string]any{
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
		ChainState: map[string]any{
			"goap_actions": []any{
				map[string]any{
					"name": "analyze_requirements",
					"cost": 1.0,
					"preconditions": map[string]any{
						"has_result": false,
					},
					"effects": map[string]any{
						"has_analysis": true,
					},
				},
				"not_a_map", // invalid entry — should be skipped
				map[string]any{
					"name": "execute_build",
					"cost": 2.0,
					"preconditions": map[string]any{
						"has_analysis": true,
					},
					"effects": map[string]any{
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
	if result != -1 || bb.ChainState["goap_plan_found"] != false {
		t.Fatal("malformed capability list was partially accepted")
	}
}

func TestAction_PlanGoapActions_JSONActionsEmptyAfterFilter(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "build something",
		ChainState: map[string]any{
			"goap_actions": []any{
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
	if !stringContains(bb.Result, "invalid GOAP actions") {
		t.Error("result should identify invalid action input")
	}
}

func TestAction_PlanGoapActions_CustomGoal(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "build a deployment pipeline",
		ChainState: map[string]any{
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
		ChainState: map[string]any{
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
		ChainState: map[string]any{
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
		ChainState: map[string]any{
			"goap_actions": "not_an_action_slice",
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected -1 for wrong actions type, got %d", result)
	}
	if !stringContains(bb.Result, "invalid GOAP actions") {
		t.Error("result should identify invalid action input")
	}
}

func TestAction_PlanGoapActions_WithGoapConfig(t *testing.T) {
	fn := GetAction("PlanGoapActions")
	if fn == nil {
		t.Fatal("PlanGoapActions action not registered")
	}
	bb := &Blackboard{
		Task: "build a deployment pipeline",
		ChainState: map[string]any{
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

func TestAction_ExecuteGoapStepRejectsUnobservedText(t *testing.T) {
	plan := &goap.Plan{Goal: goap.NewGoal("done", 1, goap.WorldState{"deployed": true}), Steps: []goap.Action{{Name: "deploy", Effects: goap.WorldState{"deployed": true}}}}
	bb := &Blackboard{Task: "deploy", LLM: &MockLLM{GenerateResp: "deployment succeeded"}, ChainState: map[string]any{"goap_plan": plan, "goap_step_index": 0, "goap_world_state": goap.WorldState{}}}
	if status := GetAction("ExecuteGoapStep")(&btcore.BTContext[Blackboard]{Blackboard: bb}); status != -1 || bb.ChainState["goap_step_index"] != 0 || goapWorldStateFrom(bb)["deployed"] != nil {
		t.Fatal("model text advanced planned effects")
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
		ChainState: map[string]any{
			"goap_step_index": 0,
			"goap_steps":      []string{"analyze_requirements"},
			// No goap_plan — safe fallback
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	result := fn(ctx)
	if result != -1 {
		t.Errorf("expected rejection without full plan, got %d", result)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome 'failure', got %q", bb.Outcome)
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
		ChainState: map[string]any{
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
	bb := &Blackboard{Task: "build a pipeline", ChainState: map[string]any{
		"goap_step_index":  0,
		"goap_steps":       []string{"build"},
		"goap_world_state": goap.WorldState{"built": false},
		"goap_plan":        &goap.Plan{Steps: []goap.Action{{Name: "build", Effects: goap.WorldState{"built": true}}}},
	}}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if result := fn(ctx); result != -1 || bb.Outcome != "failure" {
		t.Fatalf("missing executor accepted: status=%d outcome=%q", result, bb.Outcome)
	}
	if bb.ChainState["goap_step_index"] != 0 || goapWorldStateFrom(bb)["built"] != false || len(getStepResults(bb.ChainState)) != 0 {
		t.Fatalf("missing executor claimed work: %+v", bb.ChainState)
	}
	if !bb.applyExecutionStop() {
		t.Fatal("failure could be masked by a fallback")
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

// ─── buildGoapStepPrompt — prior result injection ──────────────────────────

func TestBuildGoapStepPrompt_IncludesPriorResults(t *testing.T) {
	cs := map[string]any{
		"goap_step_results": []GoapStepResult{
			{Step: 1, Result: "analyzed requirements successfully"},
			{Step: 2, Result: "built deployment pipeline config"},
		},
	}
	prompt := buildGoapStepPrompt("build a pipeline", "execute_final", cs)
	if !stringContains(prompt, "Prior step results:") {
		t.Error("prompt should include prior step results header")
	}
	if !stringContains(prompt, "Step 1: analyzed requirements successfully") {
		t.Error("prompt should include step 1 result")
	}
	if !stringContains(prompt, "Step 2: built deployment pipeline config") {
		t.Error("prompt should include step 2 result")
	}
}

func TestBuildGoapStepPrompt_CapsLongPriorResult(t *testing.T) {
	// Build a result > goapPriorResultCap (600 chars)
	longResult := strings.Repeat("x", goapPriorResultCap+200)
	cs := map[string]any{
		"goap_step_results": []GoapStepResult{
			{Step: 1, Result: longResult},
		},
	}
	prompt := buildGoapStepPrompt("task", "step2", cs)
	// The capped version should appear, truncated with "..."
	expectedCapped := longResult[:goapPriorResultCap] + "..."
	if !stringContains(prompt, expectedCapped) {
		t.Errorf("prompt should contain capped result with '...', got:\n%s", prompt)
	}
	if stringContains(prompt, longResult) {
		t.Error("full long result should not appear in prompt")
	}
}

func TestBuildGoapStepPrompt_NoPriorResults(t *testing.T) {
	cs := map[string]any{}
	prompt := buildGoapStepPrompt("task", "step1", cs)
	if stringContains(prompt, "Prior step results:") {
		t.Error("prompt without prior results should not include header")
	}
}

// ─── ExecuteGoapStep — accumulation and synthesis ──────────────────────────

// This protocol test uses a real filesystem capability, not model inference.
func TestAction_ExecuteGoapStepAccumulatesObservedCapabilities(t *testing.T) {
	root := t.TempDir()
	calls := map[string]int{}
	names := []string{"ObservedFirstFile", "ObservedSecondFile"}
	steps := make([]goap.Action, 0, 2)
	for i, name := range names {
		key := []string{"first_written", "second_written"}[i]
		file := filepath.Join(root, name)
		previous := actionRegistry[name]
		actionRegistry[name] = func(ctx *btcore.BTContext[Blackboard]) int {
			calls[name]++
			if err := os.WriteFile(file, []byte(name), 0600); err != nil {
				t.Error(err)
				return -1
			}
			actual, err := os.ReadFile(file)
			if err != nil {
				t.Error(err)
				return -1
			}
			ctx.Blackboard.Result = name
			if err := ctx.Blackboard.ObserveGoapFacts("filesystem readback", map[string]any{key: string(actual) == name}); err != nil {
				t.Error(err)
				return -1
			}
			return 1
		}
		t.Cleanup(func() {
			if previous == nil {
				delete(actionRegistry, name)
			} else {
				actionRegistry[name] = previous
			}
		})
		steps = append(steps, goap.Action{Name: name, Effects: goap.WorldState{key: true}})
	}
	plan := &goap.Plan{Goal: goap.NewGoal("files written", 1, goap.WorldState{"first_written": true, "second_written": true}), Steps: steps}
	bb := &Blackboard{Task: "write both files", ChainState: map[string]any{"goap_plan": plan, "goap_step_index": 0, "goap_world_state": goap.WorldState{}}}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	fn := GetAction("ExecuteGoapStep")
	if status := fn(ctx); status != 0 || bb.ChainState["goap_step_index"] != 1 {
		t.Fatalf("first step did not retain progress: %d %+v", status, bb)
	}
	if status := fn(ctx); status != 1 || bb.Outcome != "success" {
		t.Fatalf("goal not observed: %d %+v", status, bb)
	}
	if len(getStepResults(bb.ChainState)) != 2 || !goapWorldStateFrom(bb).Satisfies(plan.Goal.Conditions) {
		t.Fatal("missing observations")
	}
	if status := fn(ctx); status != 1 || calls[names[0]] != 1 || calls[names[1]] != 1 {
		t.Fatal("completed work replayed")
	}
	if GetAction("ReflectGoapOutcome")(ctx) != 1 || bb.Result != names[1] {
		t.Fatal("reflection rewrote the final checked result")
	}
}

func TestAction_ReflectGoapOutcome_NoResultsFallsBack(t *testing.T) {
	bb := &Blackboard{
		Outcome: "success",
		ChainState: map[string]any{
			"goap_plan_found": true,
		},
	}
	reflectFn := GetAction("ReflectGoapOutcome")
	if reflectFn == nil {
		t.Fatal("ReflectGoapOutcome action not registered")
	}
	res := reflectFn(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if res != 1 {
		t.Errorf("expected 1, got %d", res)
	}
	if bb.Result != "" {
		t.Errorf("reflection fabricated output: %q", bb.Result)
	}
}
