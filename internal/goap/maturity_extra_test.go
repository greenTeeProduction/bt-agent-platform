package goap

import (
	"fmt"
	"strings"
	"testing"
)

type fakeBlackboard struct {
	task       string
	plan       string
	result     string
	outcome    string
	chainState map[string]interface{}
}

func (f *fakeBlackboard) GetTask() string                       { return f.task }
func (f *fakeBlackboard) GetPlan() string                       { return f.plan }
func (f *fakeBlackboard) GetResult() string                     { return f.result }
func (f *fakeBlackboard) GetOutcome() string                    { return f.outcome }
func (f *fakeBlackboard) SetOutcome(v string)                   { f.outcome = v }
func (f *fakeBlackboard) SetPlan(v string)                      { f.plan = v }
func (f *fakeBlackboard) SetResult(v string)                    { f.result = v }
func (f *fakeBlackboard) GetChainState() map[string]interface{} { return f.chainState }

func TestDocPlannerWorldStateRoundTripAndCounts(t *testing.T) {
	original := DocPlannerWorldState{
		GraphFresh:    true,
		Section1Done:  true,
		Section3Done:  true,
		Section5Done:  true,
		Section7Done:  true,
		Section9Done:  true,
		Section11Done: true,
		DocAssembled:  true,
	}

	state := original.ToWorldState()
	var restored DocPlannerWorldState
	restored.FromWorldState(state)

	if !restored.GraphFresh || !restored.Section1Done || !restored.Section11Done || !restored.DocAssembled {
		t.Fatalf("expected selected true fields to survive round trip: %+v", restored)
	}
	if restored.Section2Done || restored.Section4Done || restored.Section12Done {
		t.Fatalf("expected false fields to remain false after round trip: %+v", restored)
	}
	if got := restored.SectionCount(); got != 6 {
		t.Fatalf("SectionCount() = %d, want 6", got)
	}
	if restored.AllDone() {
		t.Fatal("partial state must not report AllDone")
	}

	all := DocPlannerWorldState{Section1Done: true, Section2Done: true, Section3Done: true, Section4Done: true, Section5Done: true, Section6Done: true, Section7Done: true, Section8Done: true, Section9Done: true, Section10Done: true, Section11Done: true, Section12Done: true, DocAssembled: true}
	if !all.AllDone() || all.SectionCount() != 12 {
		t.Fatalf("complete state should be all done with 12 sections, got all=%v count=%d", all.AllDone(), all.SectionCount())
	}
}

func TestDocPlannerWorldStateIgnoresWrongTypesAndMissingKeys(t *testing.T) {
	ws := DocPlannerWorldState{Section1Done: true, Section2Done: true}
	ws.FromWorldState(WorldState{
		"section1_done": "true", // wrong type should reset through failed assertion
		"section3_done": true,
		"doc_assembled": 7, // wrong type should remain false
	})

	if ws.Section1Done {
		t.Fatal("wrongly typed section1_done should not be accepted")
	}
	if !ws.Section2Done {
		t.Fatal("missing keys should not overwrite existing fields")
	}
	if !ws.Section3Done {
		t.Fatal("boolean section3_done should be accepted")
	}
	if ws.DocAssembled {
		t.Fatal("wrongly typed doc_assembled should not be accepted")
	}
}

func TestNewDocPlannerProducesValidArc42Plan(t *testing.T) {
	dp := NewDocPlanner()
	if len(dp.Actions) != len(SectionMappings)+2 {
		t.Fatalf("actions = %d, want %d", len(dp.Actions), len(SectionMappings)+2)
	}
	if dp.Goal == nil || len(dp.Goal.Conditions) != len(SectionMappings)+1 {
		t.Fatalf("unexpected goal: %+v", dp.Goal)
	}

	plan := dp.Plan(DocPlannerWorldState{})
	if plan == nil {
		t.Fatal("expected a plan from an empty doc state")
	}
	if err := ValidatePlan(plan, DocPlannerWorldState{}.ToWorldState()); err != nil {
		t.Fatalf("arc42 plan should validate: %v", err)
	}
	if plan.Steps[0].Name != "AnalyzeCodebase" {
		t.Fatalf("first step = %q, want AnalyzeCodebase", plan.Steps[0].Name)
	}
	if plan.Steps[len(plan.Steps)-1].Name != "AssembleDocument" {
		t.Fatalf("last step = %q, want AssembleDocument", plan.Steps[len(plan.Steps)-1].Name)
	}

	positions := map[string]int{}
	for i, step := range plan.Steps {
		positions[step.Name] = i
	}
	for _, sm := range SectionMappings {
		for _, dep := range sm.DependsOn {
			depName := SectionMappings[dep-1].ActionName
			if positions[depName] > positions[sm.ActionName] {
				t.Fatalf("dependency order violated: %s after %s in %v", depName, sm.ActionName, positions)
			}
		}
	}
}

func TestDocPlannerAlreadyCompleteReturnsEmptyPlan(t *testing.T) {
	dp := NewDocPlanner()
	complete := DocPlannerWorldState{Section1Done: true, Section2Done: true, Section3Done: true, Section4Done: true, Section5Done: true, Section6Done: true, Section7Done: true, Section8Done: true, Section9Done: true, Section10Done: true, Section11Done: true, Section12Done: true, DocAssembled: true}
	plan := dp.Plan(complete)
	if plan == nil || len(plan.Steps) != 0 || plan.Cost != 0 {
		t.Fatalf("complete doc should produce zero-cost empty plan, got %#v", plan)
	}
}

func TestBuildGoalFromTaskMultipleKeywordsAndTruncation(t *testing.T) {
	goal := BuildGoalFromTask("build and test a pipeline with validation")
	if goal.Conditions["task_type"] != "test" {
		t.Fatalf("later matching task category should win, got %v", goal.Conditions["task_type"])
	}
	if goal.Conditions["has_result"] != true {
		t.Fatalf("matched task should require has_result=true, got %v", goal.Conditions)
	}

	longTask := strings.Repeat("x", 45)
	goal = BuildGoalFromTask(longTask)
	if len(goal.Name) != 43 || !strings.HasSuffix(goal.Name, "...") {
		t.Fatalf("long goal name should be truncated to 40 chars plus ellipsis, got %q", goal.Name)
	}
}

func TestFromJSONDefaultsAndInvalidJSON(t *testing.T) {
	def, err := FromJSON([]byte(`{"name":"minimal","goals":[{"name":"done","conditions":{"done":true}}]}`))
	if err != nil {
		t.Fatalf("FromJSON minimal config failed: %v", err)
	}
	if def.Config != DefaultGOAPConfig() {
		t.Fatalf("zero config should default, got %+v", def.Config)
	}

	if _, err := FromJSON([]byte(`{"name":`)); err == nil {
		t.Fatal("invalid JSON should return an error")
	}
}

func TestSetupStandardRegistryCapturesEachActionName(t *testing.T) {
	registry := SetupStandardRegistry(func(actionName string, state WorldState) (WorldState, error) {
		return state.Apply(WorldState{"executed": actionName}), nil
	})

	for _, action := range StandardActions() {
		fn, ok := registry[action.Name]
		if !ok {
			t.Fatalf("missing registry entry for %s", action.Name)
		}
		state, err := fn(WorldState{})
		if err != nil {
			t.Fatalf("registry function for %s returned error: %v", action.Name, err)
		}
		if state["executed"] != action.Name {
			t.Fatalf("closure captured wrong action name: got %v want %s", state["executed"], action.Name)
		}
	}
}

func TestBlackboardBridgeSyncAndPlanSuccess(t *testing.T) {
	actions := []Action{NewAction("finish", 1, WorldState{"ready": true}, WorldState{"done": true})}
	agent := NewAgent(DefaultPlanner(actions), ActionRegistry{
		"finish": func(state WorldState) (WorldState, error) { return state.Apply(WorldState{"done": true}), nil },
	})
	agent.SetState("ready", true)
	agent.SetGoals(NewGoal("done", 1, WorldState{"done": true}))
	bb := &fakeBlackboard{chainState: map[string]interface{}{"has_plan": true, "has_resources": true, "task_status": "ready", "ready": true}}
	bridge := NewBlackboardBridge(agent, bb)
	bridge.RegisterLLMAction("finish", "do {{.ActionName}}")
	if bridge.LLMActions["finish"] == "" {
		t.Fatal("LLM action registration did not persist")
	}

	bridge.SyncFromBB()
	if got, ok := agent.GetState("has_plan"); !ok || got != true {
		t.Fatalf("SyncFromBB did not copy mapped chain state, got %v ok=%v", got, ok)
	}
	run := bridge.PlanAndSync()
	if run.Status != AgentSucceeded {
		t.Fatalf("PlanAndSync status = %s: %s", run.Status, run.Error)
	}
	if bb.outcome != "success" || bb.result == "" || bb.plan == "" {
		t.Fatalf("success should set outcome/result/plan, bb=%+v", bb)
	}
	if bb.chainState["done"] != true || bb.chainState["goap_status"] != string(AgentSucceeded) {
		t.Fatalf("SyncToBB should write world state and run metadata, got %#v", bb.chainState)
	}
}

func TestBlackboardBridgeHandlesNilChainStateAndFailure(t *testing.T) {
	agent := NewAgent(DefaultPlanner(nil), nil)
	agent.SetGoals(NewGoal("impossible", 1, WorldState{"done": true}))
	bb := &fakeBlackboard{}
	bridge := NewBlackboardBridge(agent, bb)

	bridge.SyncFromBB()
	bridge.SyncToBB()
	run := bridge.PlanAndSync()
	if run.Status != AgentFailed {
		t.Fatalf("run status = %s, want failed", run.Status)
	}
	if bb.outcome != "failure" || !strings.Contains(bb.result, "GOAP failed") {
		t.Fatalf("failure should set blackboard failure result, bb=%+v", bb)
	}
}

func TestAgentFailurePathsAndSummary(t *testing.T) {
	t.Run("missing registry action", func(t *testing.T) {
		agent := NewAgent(DefaultPlanner([]Action{NewAction("missing", 1, WorldState{"ready": true}, WorldState{"done": true})}), ActionRegistry{})
		agent.SetState("ready", true)
		agent.SetGoals(NewGoal("done", 1, WorldState{"done": true}))
		run := agent.Run()
		if run.Status != AgentFailed || !strings.Contains(run.Error, "not found") {
			t.Fatalf("expected missing action failure, got status=%s err=%q", run.Status, run.Error)
		}
		if got := agent.Summary(); !strings.Contains(got, "failed") || !strings.Contains(got, "history=1") {
			t.Fatalf("summary should reflect failed history, got %q", got)
		}
	})

	t.Run("action error calls callbacks", func(t *testing.T) {
		errorCalled := false
		stepCompleteErr := false
		agent := NewAgent(DefaultPlanner([]Action{NewAction("boom", 1, WorldState{"ready": true}, WorldState{"done": true})}), ActionRegistry{
			"boom": func(WorldState) (WorldState, error) { return nil, fmt.Errorf("boom") },
		})
		agent.MaxReplans = 0
		agent.Callbacks.OnStepComplete = func(_ int, _ *Action, err error) { stepCompleteErr = err != nil }
		agent.Callbacks.OnError = func(_ *AgentRun, _ error) { errorCalled = true }
		agent.SetState("ready", true)
		agent.SetGoals(NewGoal("done", 1, WorldState{"done": true}))
		run := agent.Run()
		if run.Status != AgentFailed || !stepCompleteErr {
			t.Fatalf("expected action error failure and callback, got status=%s callback=%v", run.Status, stepCompleteErr)
		}
		if errorCalled {
			t.Fatal("OnError is reserved for no-plan failures and should not fire for execution errors")
		}
	})
}

func TestValidatePlanNilEmptyAndFinalStateFailure(t *testing.T) {
	if err := ValidatePlan(nil, WorldState{}); err == nil {
		t.Fatal("nil plan should fail validation")
	}
	if err := ValidatePlan(&Plan{Goal: NewGoal("empty", 1, WorldState{"done": true})}, WorldState{}); err != nil {
		t.Fatalf("empty plan is considered trivially valid by ValidatePlan, got %v", err)
	}

	plan := &Plan{
		Goal:  NewGoal("done", 1, WorldState{"done": true}),
		Steps: []Action{NewAction("no_effect", 1, WorldState{"ready": true}, WorldState{"ready": true})},
	}
	if err := ValidatePlan(plan, WorldState{"ready": true}); err == nil || !strings.Contains(err.Error(), "does not satisfy") {
		t.Fatalf("expected final-state validation failure, got %v", err)
	}
}
