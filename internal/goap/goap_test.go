package goap

import (
	"testing"
)

// --- WorldState Tests ---

func TestWorldStateSatisfies(t *testing.T) {
	ws := WorldState{"hungry": true, "hasFood": false, "energy": 50}

	t.Run("empty goal always satisfied", func(t *testing.T) {
		if !ws.Satisfies(WorldState{}) {
			t.Error("empty goal should always be satisfied")
		}
	})

	t.Run("single matching condition", func(t *testing.T) {
		if !ws.Satisfies(WorldState{"hungry": true}) {
			t.Error("should satisfy single matching condition")
		}
	})

	t.Run("single non-matching condition", func(t *testing.T) {
		if ws.Satisfies(WorldState{"hungry": false}) {
			t.Error("should NOT satisfy non-matching condition")
		}
	})

	t.Run("missing key", func(t *testing.T) {
		if ws.Satisfies(WorldState{"nonexistent": true}) {
			t.Error("should NOT satisfy when key is missing")
		}
	})

	t.Run("multiple matching conditions", func(t *testing.T) {
		if !ws.Satisfies(WorldState{"hungry": true, "hasFood": false}) {
			t.Error("should satisfy multiple matching conditions")
		}
	})

	t.Run("partial match fails", func(t *testing.T) {
		if ws.Satisfies(WorldState{"hungry": true, "hasFood": true}) {
			t.Error("should fail when one condition doesn't match")
		}
	})
}

func TestWorldStateApply(t *testing.T) {
	t.Run("apply single effect", func(t *testing.T) {
		ws := WorldState{"hungry": true}
		newState := ws.Apply(WorldState{"hungry": false})
		if newState["hungry"] != false {
			t.Error("effect should change value")
		}
		if ws["hungry"] != true {
			t.Error("original should be unchanged")
		}
	})

	t.Run("apply multiple effects", func(t *testing.T) {
		ws := WorldState{"x": 1}
		newState := ws.Apply(WorldState{"x": 2, "y": 3})
		if newState["x"] != 2 || newState["y"] != 3 {
			t.Errorf("expected {x:2, y:3}, got %v", newState)
		}
	})
}

func TestWorldStateClone(t *testing.T) {
	ws := WorldState{"a": 1, "b": "hello"}
	clone := ws.Clone()
	clone["c"] = 3
	if _, ok := ws["c"]; ok {
		t.Error("clone should be independent")
	}
}

func TestWorldStateEquals(t *testing.T) {
	a := WorldState{"x": 1, "y": 2}
	b := WorldState{"x": 1, "y": 2}
	c := WorldState{"x": 1, "y": 3}
	d := WorldState{"x": 1}

	if !a.Equals(b) {
		t.Error("identical states should be equal")
	}
	if a.Equals(c) {
		t.Error("different values should not be equal")
	}
	if a.Equals(d) {
		t.Error("different lengths should not be equal")
	}
}

// --- Planner Tests ---

func TestPlannerTrivialGoal(t *testing.T) {
	planner := DefaultPlanner(StandardActions())
	state := WorldState{"has_result": true, "task_status": "completed"}
	goal := NewGoal("test", 1.0, WorldState{"task_status": "completed"})

	plan := planner.Plan(state, goal)
	if plan == nil {
		t.Fatal("plan should be found for already-satisfied goal")
	}
	if len(plan.Steps) != 0 {
		t.Errorf("expected 0 steps for satisfied goal, got %d", len(plan.Steps))
	}
	if plan.Cost != 0 {
		t.Errorf("expected cost 0, got %f", plan.Cost)
	}
}

func TestPlannerSimplePlan(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	state := WorldState{"hungry": true}
	goal := NewGoal("not hungry", 1.0, WorldState{"hungry": false})

	plan := planner.Plan(state, goal)
	if plan == nil {
		t.Fatal("plan should exist")
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].Name != "eat" {
		t.Errorf("expected 'eat', got %q", plan.Steps[0].Name)
	}
	if plan.Cost != 1.0 {
		t.Errorf("expected cost 1.0, got %f", plan.Cost)
	}
}

func TestPlannerMultiStep(t *testing.T) {
	actions := []Action{
		NewAction("walk_to_kitchen", 2.0,
			WorldState{"at_kitchen": false, "hungry": true},
			WorldState{"at_kitchen": true}),
		NewAction("cook_food", 3.0,
			WorldState{"at_kitchen": true, "has_food": false},
			WorldState{"has_food": true}),
		NewAction("eat", 1.0,
			WorldState{"has_food": true, "hungry": true},
			WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	state := WorldState{"hungry": true, "at_kitchen": false, "has_food": false}
	goal := NewGoal("not hungry", 1.0, WorldState{"hungry": false})

	plan := planner.Plan(state, goal)
	if plan == nil {
		t.Fatal("plan should exist")
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(plan.Steps))
	}

	names := []string{plan.Steps[0].Name, plan.Steps[1].Name, plan.Steps[2].Name}
	expected := []string{"walk_to_kitchen", "cook_food", "eat"}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("step %d: expected %q, got %q", i, expected[i], n)
		}
	}
}

func TestPlannerNoPlan(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"has_food": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	state := WorldState{"hungry": true, "has_food": false}
	goal := NewGoal("not hungry", 1.0, WorldState{"hungry": false})

	plan := planner.Plan(state, goal)
	if plan != nil {
		t.Error("should be nil when no plan exists (preconditions never met)")
	}
}

func TestPlannerCostOptimization(t *testing.T) {
	actions := []Action{
		NewAction("eat_snack", 1.0,
			WorldState{"hungry": true},
			WorldState{"hungry": false}),
		NewAction("cook_and_eat", 5.0,
			WorldState{"hungry": true, "has_food": true},
			WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	state := WorldState{"hungry": true}
	goal := NewGoal("not hungry", 1.0, WorldState{"hungry": false})

	plan := planner.Plan(state, goal)
	if plan == nil {
		t.Fatal("plan should exist")
	}
	if plan.Steps[0].Name != "eat_snack" {
		t.Errorf("should pick eat_snack (cost 1) over cook_and_eat (cost 5), got %q", plan.Steps[0].Name)
	}
}

func TestPlannerDepthLimit(t *testing.T) {
	// Create actions that form an infinite loop (A->B->A->B...)
	actions := []Action{
		NewAction("a_to_b", 1.0, WorldState{"at": "a"}, WorldState{"at": "b"}),
		NewAction("b_to_a", 1.0, WorldState{"at": "b"}, WorldState{"at": "a"}),
	}
	planner := NewPlanner(actions, 5, 100)
	state := WorldState{"at": "a"}
	goal := NewGoal("reach c", 1.0, WorldState{"at": "c"})

	plan := planner.Plan(state, goal)
	if plan != nil {
		t.Error("should be nil — goal unreachable, should hit depth limit")
	}
}

func TestPlannerNodeLimit(t *testing.T) {
	actions := StandardActions()
	planner := NewPlanner(actions, 50, 3) // only 3 nodes
	state := WorldState{"has_result": false}
	goal := NewGoal("complete", 1.0, WorldState{"task_status": "completed"})

	plan := planner.Plan(state, goal)
	if plan != nil {
		t.Error("should be nil when node limit hit")
	}
}

func TestPlanMultiple(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
		NewAction("sleep", 1.0, WorldState{"tired": true}, WorldState{"tired": false}),
	}
	state := WorldState{"hungry": true, "tired": true}
	goals := []*Goal{
		NewGoal("eat", 0.5, WorldState{"hungry": false}),
		NewGoal("sleep", 0.9, WorldState{"tired": false}),
	}

	plan := PlanMultiple(state, goals, actions)
	if plan == nil {
		t.Fatal("should find a plan")
	}
	// Should prefer "sleep" (priority 0.9) over "eat" (0.5)
	if plan.Goal.Name != "sleep" {
		t.Errorf("expected 'sleep' (higher priority), got %q", plan.Goal.Name)
	}
}

func TestValidatePlan(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	state := WorldState{"hungry": true}
	goal := NewGoal("not hungry", 1.0, WorldState{"hungry": false})

	plan := planner.Plan(state, goal)
	if err := ValidatePlan(plan, state); err != nil {
		t.Errorf("plan should validate: %v", err)
	}
}

func TestValidatePlanFailsWrongState(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	state := WorldState{"hungry": true}
	goal := NewGoal("not hungry", 1.0, WorldState{"hungry": false})

	plan := planner.Plan(state, goal)
	wrongState := WorldState{"hungry": false}
	if err := ValidatePlan(plan, wrongState); err == nil {
		t.Error("should fail validation from wrong state")
	}
}

// --- Agent Tests ---

func TestAgentSimpleExecution(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	registry := ActionRegistry{
		"eat": func(state WorldState) (WorldState, error) {
			return state.Apply(WorldState{"hungry": false}), nil
		},
	}
	agent := NewAgent(planner, registry)
	agent.SetState("hungry", true)
	agent.SetGoals(NewGoal("not hungry", 1.0, WorldState{"hungry": false}))

	run := agent.Run()
	if run.Status != AgentSucceeded {
		t.Errorf("expected succeeded, got %s: %s", run.Status, run.Error)
	}
	if len(run.StepsTaken) != 1 || run.StepsTaken[0] != "eat" {
		t.Errorf("unexpected steps: %v", run.StepsTaken)
	}
}

func TestAgentNoPlan(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"has_food": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	agent := NewAgent(planner, nil)
	agent.SetState("hungry", true)
	agent.SetGoals(NewGoal("not hungry", 1.0, WorldState{"hungry": false}))

	run := agent.Run()
	if run.Status != AgentFailed {
		t.Errorf("expected failed, got %s", run.Status)
	}
}

func TestAgentActionFails(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	registry := ActionRegistry{
		"eat": func(_ WorldState) (WorldState, error) {
			return WorldState{}, assertError("food poisoned")
		},
	}
	agent := NewAgent(planner, registry)
	agent.SetState("hungry", true)
	agent.SetGoals(NewGoal("not hungry", 1.0, WorldState{"hungry": false}))

	run := agent.Run()
	if run.Status != AgentFailed {
		t.Errorf("expected failed, got %s", run.Status)
	}
}

func TestAgentGetSetState(t *testing.T) {
	agent := NewAgent(nil, nil)
	agent.SetState("x", 42)
	v, ok := agent.GetState("x")
	if !ok || v != 42 {
		t.Errorf("expected 42, got %v", v)
	}
	_, ok = agent.GetState("missing")
	if ok {
		t.Error("missing key should return false")
	}
}

func TestAgentHistory(t *testing.T) {
	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	registry := ActionRegistry{
		"eat": func(state WorldState) (WorldState, error) {
			return state.Apply(WorldState{"hungry": false}), nil
		},
	}
	agent := NewAgent(planner, registry)
	agent.SetState("hungry", true)
	agent.SetGoals(NewGoal("not hungry", 1.0, WorldState{"hungry": false}))

	agent.Run()
	if len(agent.HistoryRuns()) != 1 {
		t.Errorf("expected 1 history entry, got %d", len(agent.HistoryRuns()))
	}
	agent.Run()
	if len(agent.HistoryRuns()) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(agent.HistoryRuns()))
	}
}

func TestAgentCallbacks(t *testing.T) {
	planFound := false
	stepStarted := false
	stepComplete := false
	completed := false

	actions := []Action{
		NewAction("eat", 1.0, WorldState{"hungry": true}, WorldState{"hungry": false}),
	}
	planner := DefaultPlanner(actions)
	registry := ActionRegistry{
		"eat": func(state WorldState) (WorldState, error) {
			return state.Apply(WorldState{"hungry": false}), nil
		},
	}
	agent := NewAgent(planner, registry)
	agent.Callbacks = AgentCallbacks{
		OnPlanFound:    func(_ *Plan) { planFound = true },
		OnStepStart:    func(_ int, _ *Action) { stepStarted = true },
		OnStepComplete: func(_ int, _ *Action, _ error) { stepComplete = true },
		OnComplete:     func(_ *AgentRun) { completed = true },
	}
	agent.SetState("hungry", true)
	agent.SetGoals(NewGoal("not hungry", 1.0, WorldState{"hungry": false}))

	agent.Run()
	if !planFound {
		t.Error("OnPlanFound should fire")
	}
	if !stepStarted {
		t.Error("OnStepStart should fire")
	}
	if !stepComplete {
		t.Error("OnStepComplete should fire")
	}
	if !completed {
		t.Error("OnComplete should fire")
	}
}

// --- Integration Tests ---

func TestBuildGoalFromTask(t *testing.T) {
	tests := []struct{ task, expectedType string }{
		{"build a new API endpoint", "build"},
		{"test the login flow", "test"},
		{"deploy to production", "deploy"},
		{"research quantum computing", "research"},
		{"fix the memory leak", "fix"},
		{"do something random", "general"},
	}
	for _, tc := range tests {
		goal := BuildGoalFromTask(tc.task)
		if goal.Conditions["task_type"] != tc.expectedType {
			t.Errorf("task %q: expected type %q, got %v", tc.task, tc.expectedType, goal.Conditions["task_type"])
		}
	}
}

func TestStandardActionsCoverage(t *testing.T) {
	actions := StandardActions()
	if len(actions) == 0 {
		t.Fatal("standard actions should not be empty")
	}
	names := make(map[string]bool)
	for _, a := range actions {
		if a.Name == "" {
			t.Error("action should have a name")
		}
		if names[a.Name] {
			t.Errorf("duplicate action name: %q", a.Name)
		}
		names[a.Name] = true
	}
}

func TestBuildSerializableTree(t *testing.T) {
	def := GOAPTreeDefinition{
		Name:        "test_tree",
		Description: "a test GOAP tree",
		Goals:       []*Goal{NewGoal("test", 1.0, WorldState{"done": true})},
		Actions:     StandardActions(),
		Config:      DefaultGOAPConfig(),
	}

	tree := BuildSerializableTree(def)
	if tree.Type != "Sequence" {
		t.Errorf("root should be Sequence, got %s", tree.Type)
	}
	if tree.Name != "GOAP_Root" {
		t.Errorf("root name should be GOAP_Root, got %q", tree.Name)
	}
	if len(tree.Children) != 4 {
		t.Errorf("expected 4 children (HasGoapGoal, PlanGoapActions, GoapStrategyRouter, ReflectGoapOutcome), got %d", len(tree.Children))
	}
}

func TestGOAPTreeDefToJSON(t *testing.T) {
	def := GOAPTreeDefinition{
		Name:        "test",
		Description: "test tree",
		Goals:       []*Goal{NewGoal("t", 1.0, WorldState{"x": true})},
		Actions:     []Action{NewAction("a", 1.0, WorldState{}, WorldState{"x": true})},
		Config:      DefaultGOAPConfig(),
	}
	data, err := def.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "test" {
		t.Errorf("expected 'test', got %q", parsed.Name)
	}
}

func TestPlanString(t *testing.T) {
	plan := &Plan{
		Goal:  NewGoal("eat", 1.0, WorldState{}),
		Steps: []Action{NewAction("cook", 2.0, WorldState{}, WorldState{}), NewAction("eat", 1.0, WorldState{}, WorldState{})},
		Cost:  3.0,
	}
	s := plan.String()
	if len(s) == 0 {
		t.Error("plan string should not be empty")
	}
}

func TestNewGoal(t *testing.T) {
	g := NewGoal("test", 0.7, WorldState{"x": 1})
	if g.Name != "test" || g.Priority != 0.7 || g.Conditions["x"] != 1 {
		t.Errorf("unexpected goal fields: %+v", g)
	}
}

func TestNewAction(t *testing.T) {
	a := NewAction("test", 2.5, WorldState{"pre": true}, WorldState{"eff": "done"})
	if a.Name != "test" || a.Cost != 2.5 || a.Preconditions["pre"] != true || a.Effects["eff"] != "done" {
		t.Errorf("unexpected action fields: %+v", a)
	}
}

func TestWorldStateString(t *testing.T) {
	ws := WorldState{"z": 1, "a": 2}
	s := ws.String()
	if s == "" {
		t.Error("string should not be empty")
	}
	// Sorted order should put "a" before "z"
	if s[:3] != "{a:" {
		t.Errorf("expected sorted keys, got %q", s)
	}
}

// Helper
type testError string

func (e testError) Error() string { return string(e) }

func assertError(msg string) error { return testError(msg) }
