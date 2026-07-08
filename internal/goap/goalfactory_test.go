package goap

import (
	"fmt"
	"strings"
	"testing"
)

// fakeLLM returns queued replies in order; repeats the last one when drained.
type fakeLLM struct {
	replies []string
	calls   int
	prompts []string
}

func (f *fakeLLM) Generate(prompt string) (string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	if len(f.replies) == 0 {
		return "", fmt.Errorf("no replies configured")
	}
	idx := f.calls - 1
	if idx >= len(f.replies) {
		idx = len(f.replies) - 1
	}
	return f.replies[idx], nil
}

func TestVocabulary_CanonicalMapping(t *testing.T) {
	v := StandardVocabulary()

	got, ok := v.Canonical("Task-Automated")
	if !ok || got != "task_automated" {
		t.Fatalf("Canonical(Task-Automated) = %q, %v", got, ok)
	}
	if _, ok := v.Canonical("weather_in_paris"); ok {
		t.Fatal("expected unknown key to be rejected")
	}
	// "has" matches many keys → ambiguous, must be rejected.
	if _, ok := v.Canonical("has"); ok {
		t.Fatal("expected ambiguous key to be rejected")
	}
}

func TestGoalFactory_FromIntentLLM(t *testing.T) {
	llm := &fakeLLM{replies: []string{
		`Here is the goal: {"name": "automate report", "priority": 0.8, "deadline": 12, "conditions": {"task_automated": true}}`,
	}}
	f := NewGoalFactory(llm)

	goal, report, err := f.FromIntent("automate my weekly report")
	if err != nil {
		t.Fatalf("FromIntent: %v", err)
	}
	if goal.Name != "automate report" || goal.Priority != 0.8 || goal.Deadline != 12 {
		t.Fatalf("unexpected goal: %+v", goal)
	}
	if goal.Conditions["task_automated"] != true {
		t.Fatalf("conditions = %v", goal.Conditions)
	}
	if report.Source != "llm" {
		t.Fatalf("source = %q", report.Source)
	}
	if len(report.PlanPreview) == 0 {
		t.Fatal("expected a validation plan preview")
	}
	if !strings.Contains(llm.prompts[0], "task_automated") {
		t.Fatal("prompt should list vocabulary keys")
	}
}

func TestGoalFactory_RepairsUngroundedConditions(t *testing.T) {
	llm := &fakeLLM{replies: []string{
		`{"name": "bad", "priority": 0.5, "conditions": {"weather_is_sunny": true}}`,
		`{"name": "good", "priority": 0.5, "conditions": {"has_verification": true}}`,
	}}
	f := NewGoalFactory(llm)

	goal, report, err := f.FromIntent("verify my work")
	if err != nil {
		t.Fatalf("FromIntent: %v", err)
	}
	if goal.Name != "good" {
		t.Fatalf("expected repaired goal, got %+v", goal)
	}
	if report.Source != "llm_repaired" {
		t.Fatalf("source = %q", report.Source)
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", llm.calls)
	}
	if !strings.Contains(llm.prompts[1], "weather_is_sunny") {
		t.Fatal("repair prompt should name the rejected keys")
	}
}

func TestGoalFactory_GroundsKeyVariantsAndValues(t *testing.T) {
	llm := &fakeLLM{replies: []string{
		`{"name": "g", "priority": 2.5, "conditions": {"Task Automated": "true", "made_up_key": 1}}`,
	}}
	f := NewGoalFactory(llm)

	goal, report, err := f.FromIntent("automate this")
	if err != nil {
		t.Fatalf("FromIntent: %v", err)
	}
	if goal.Conditions["task_automated"] != true {
		t.Fatalf("expected mapped bool condition, got %v", goal.Conditions)
	}
	if goal.Priority != 1 {
		t.Fatalf("priority should clamp to 1, got %v", goal.Priority)
	}
	if report.MappedKeys["Task Automated"] != "task_automated" {
		t.Fatalf("mapped keys = %v", report.MappedKeys)
	}
	if len(report.DroppedKeys) != 1 || report.DroppedKeys[0] != "made_up_key" {
		t.Fatalf("dropped keys = %v", report.DroppedKeys)
	}
}

func TestGoalFactory_ArchetypeFallbackWithoutLLM(t *testing.T) {
	f := NewGoalFactory(nil)

	cases := []struct {
		intent    string
		archetype GoalArchetype
		wantKey   string
	}{
		{"automate my daily standup notes", ArchetypeAutomateRecurring, "task_automated"},
		{"improve the quality of my summaries", ArchetypeImproveQuality, "quality_improved"},
		{"make my reviews faster", ArchetypeReduceTurnaround, "turnaround_optimized"},
		{"monitor the deploy pipeline and alert me", ArchetypeWatchAndAlert, "alerts_enabled"},
		{"finish the tax filing", ArchetypeCompleteTask, "has_verification"},
	}
	for _, tc := range cases {
		goal, report, err := f.FromIntent(tc.intent)
		if err != nil {
			t.Fatalf("FromIntent(%q): %v", tc.intent, err)
		}
		if report.Source != "archetype" || report.Archetype != tc.archetype {
			t.Fatalf("intent %q: report %+v", tc.intent, report)
		}
		if _, ok := goal.Conditions[tc.wantKey]; !ok {
			t.Fatalf("intent %q: conditions %v missing %s", tc.intent, goal.Conditions, tc.wantKey)
		}
		if len(report.PlanPreview) == 0 {
			t.Fatalf("intent %q: archetype goal should be plannable", tc.intent)
		}
	}
}

func TestGoalFactory_ArchetypeFallbackWhenLLMKeepsFailing(t *testing.T) {
	llm := &fakeLLM{replies: []string{`not json at all`}}
	f := NewGoalFactory(llm)

	goal, report, err := f.FromIntent("automate my weekly report")
	if err != nil {
		t.Fatalf("FromIntent: %v", err)
	}
	if report.Source != "archetype" {
		t.Fatalf("expected archetype fallback, got %q", report.Source)
	}
	if goal.Conditions["task_automated"] != true {
		t.Fatalf("conditions = %v", goal.Conditions)
	}
}

func TestGoalFactory_EmptyIntent(t *testing.T) {
	f := NewGoalFactory(nil)
	if _, _, err := f.FromIntent("   "); err == nil {
		t.Fatal("expected error for empty intent")
	}
}

func TestGoalFactory_FromPattern(t *testing.T) {
	f := NewGoalFactory(nil)

	goal := f.FromPattern("summarize weekly sales numbers", 3)
	if goal.Conditions["task_automated"] != true {
		t.Fatalf("conditions = %v", goal.Conditions)
	}
	if goal.Priority != 0.7 {
		t.Fatalf("priority = %v", goal.Priority)
	}
	if !strings.HasPrefix(goal.Name, "automate: ") {
		t.Fatalf("name = %q", goal.Name)
	}

	// Habit goals cap below explicit user goals.
	if p := f.FromPattern("x", 50).Priority; p != 0.9 {
		t.Fatalf("capped priority = %v", p)
	}

	// Pattern goals must be plannable with the factory's actions.
	report := &GroundingReport{}
	if err := f.validate(goal, report); err != nil {
		t.Fatalf("pattern goal not plannable: %v", err)
	}
}

func TestAutomationActions_ReachTaskAutomated(t *testing.T) {
	planner := NewPlanner(AutomationActions(), 20, 5000)
	goal := NewGoal("automate", 0.5, WorldState{"task_automated": true})
	plan := planner.Plan(WorldState{}, goal)
	if plan == nil {
		t.Fatal("expected a plan to task_automated=true")
	}
	want := []string{"detect_recurring_pattern", "compile_automation_tree", "propose_automation_hitl", "schedule_automation_agent"}
	if len(plan.Steps) != len(want) {
		t.Fatalf("plan = %v", plan)
	}
	for i, step := range plan.Steps {
		if step.Name != want[i] {
			t.Fatalf("step %d = %q, want %q", i, step.Name, want[i])
		}
	}
}

func TestGoalQueue_DeadlineBreaksPriorityTies(t *testing.T) {
	relaxed := &Goal{Name: "no-deadline", Priority: 0.5, Conditions: WorldState{"a": true}}
	urgent := &Goal{Name: "urgent", Priority: 0.5, Deadline: 5, Conditions: WorldState{"b": true}}
	later := &Goal{Name: "later", Priority: 0.5, Deadline: 20, Conditions: WorldState{"c": true}}
	gq := NewGoalQueueFrom(relaxed, later, urgent)

	if got := gq.SelectGoal(WorldState{}); got.Name != "urgent" {
		t.Fatalf("SelectGoal = %q, want urgent", got.Name)
	}

	// Higher priority still wins regardless of deadline.
	gq.Add(&Goal{Name: "important", Priority: 0.9, Conditions: WorldState{"d": true}})
	if got := gq.SelectGoal(WorldState{}); got.Name != "important" {
		t.Fatalf("SelectGoal = %q, want important", got.Name)
	}
}
