package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// Characterization tests for registerGoalTools and its unexported helpers
// (ADR-133 Phase 2 per-user goal factory tools): pin the currently observed
// behavior of bt_goal_add, bt_goal_list, bt_goal_remove, bt_goal_compile,
// bt_goal_from_pattern, and the pure helpers they share.

// stubLLM satisfies llm.LLM without making network calls, so tests can pin
// goalFactory's "client present" branch deterministically.
type stubLLM struct{}

func (stubLLM) Generate(prompt string) (string, error) { return "", nil }
func (stubLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return "", nil
}
func (stubLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return "", nil
}
func (stubLLM) AnalyzeComplexity(task string) string                { return "" }
func (stubLLM) GeneratePlan(task, complexity string) string         { return "" }
func (stubLLM) Reflect(task, outcome, plan string) (string, string) { return "", "" }

func TestGoalTreeSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "automate reports", "automate_reports"},
		{"uppercase folded", "Automate My Reports", "automate_my_reports"},
		{"truncated to 5 fields", "one two three four five six seven", "one_two_three_four_five"},
		{"strips punctuation", "watch & alert!", "watch_alert"},
		{"empty name falls back", "", "goal"},
		{"only punctuation falls back", "!!!", "goal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goalTreeSlug(tt.in); got != tt.want {
				t.Errorf("goalTreeSlug(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGoalError(t *testing.T) {
	res := goalError("boom")
	if res == nil || len(res.Content) != 1 {
		t.Fatalf("goalError returned malformed result: %+v", res)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("goalError content is not valid JSON: %v", err)
	}
	if out["error"] != "boom" {
		t.Errorf("error = %v, want %q", out["error"], "boom")
	}
}

func TestUserGoalStore(t *testing.T) {
	t.Run("no persona store configured", func(t *testing.T) {
		deps := &mcpDeps{}
		store, res := userGoalStore(deps, "nico")
		if store != nil || res == nil {
			t.Fatalf("expected error result with nil store, got store=%v res=%v", store, res)
		}
	})

	t.Run("empty user rejected", func(t *testing.T) {
		deps := newFeedbackDeps(t)
		store, res := userGoalStore(deps, "   ")
		if store != nil || res == nil {
			t.Fatalf("expected error result with nil store, got store=%v res=%v", store, res)
		}
	})

	t.Run("valid user returns a usable store", func(t *testing.T) {
		deps := newFeedbackDeps(t)
		store, res := userGoalStore(deps, "nico")
		if res != nil {
			t.Fatalf("unexpected error result: %+v", res)
		}
		if store == nil {
			t.Fatal("expected a non-nil goal store")
		}
		goals, err := store.Load()
		if err != nil || len(goals) != 0 {
			t.Errorf("fresh store should load empty, got %v err=%v", goals, err)
		}
	})
}

func TestPersonaStyleHints(t *testing.T) {
	t.Run("no persona store configured", func(t *testing.T) {
		deps := &mcpDeps{}
		if got := personaStyleHints(deps, "nico"); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("empty user", func(t *testing.T) {
		deps := newFeedbackDeps(t)
		if got := personaStyleHints(deps, ""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("default profile has no hints", func(t *testing.T) {
		deps := newFeedbackDeps(t)
		if got := personaStyleHints(deps, "nico"); got != "" {
			t.Errorf("got %q, want empty for a never-seen user", got)
		}
	})

	t.Run("preferred style and prompt hints are joined", func(t *testing.T) {
		deps := newFeedbackDeps(t)
		profile, err := deps.personaStore.Load("nico")
		if err != nil {
			t.Fatalf("load profile: %v", err)
		}
		profile.PreferredStyle = "minimal"
		profile.PromptHints = []string{"answer in German.", "prefer tables."}
		if err := deps.personaStore.Save(profile); err != nil {
			t.Fatalf("save profile: %v", err)
		}

		got := personaStyleHints(deps, "nico")
		want := "Preferred output style: minimal. answer in German. prefer tables."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestGoalFactory(t *testing.T) {
	t.Run("nil llm client degrades to archetype-only", func(t *testing.T) {
		deps := &mcpDeps{}
		f := goalFactory(deps)
		if f.LLM != nil {
			t.Error("expected LLM to be nil with no client configured")
		}
	})

	t.Run("client set, no health monitor uses the client", func(t *testing.T) {
		deps := &mcpDeps{llmClient: stubLLM{}}
		f := goalFactory(deps)
		if f.LLM == nil {
			t.Error("expected LLM to be set when a health monitor is not configured")
		}
	})

	t.Run("client set, unhealthy monitor degrades to archetype-only", func(t *testing.T) {
		deps := &mcpDeps{llmClient: stubLLM{}, llmHealth: llm.NewHealthMonitor("", 0)}
		f := goalFactory(deps)
		if f.LLM != nil {
			t.Error("expected LLM to be nil when the health monitor reports unhealthy")
		}
	})
}

func TestBTGoalToolsRegistered(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)
	for _, name := range []string{"bt_goal_add", "bt_goal_list", "bt_goal_remove", "bt_goal_compile", "bt_goal_from_pattern"} {
		if !server.HasTool(name) {
			t.Errorf("%s must be registered by registerGoalTools", name)
		}
	}
}

func invokeGoal(t *testing.T, server *engine.Server, tool string, args string) map[string]any {
	t.Helper()
	res, ok := server.Invoke(tool, json.RawMessage(args))
	if !ok || res == nil || len(res.Content) == 0 {
		t.Fatalf("%s returned no content", tool)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("%s result is not valid JSON: %v", tool, err)
	}
	return out
}

func TestBTGoalAdd_ExtractsAndPersistsGoal(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_add", `{"user":"nico","intent":"automate my daily report"}`)
	if out["added"] != true {
		t.Fatalf("expected added=true, got %v", out)
	}
	goal, ok := out["goal"].(map[string]any)
	if !ok || goal["name"] == "" || goal["name"] == nil {
		t.Fatalf("expected a named goal in the response, got %v", out["goal"])
	}
	if out["grounding"] == nil {
		t.Error("expected a grounding report in the response")
	}

	store, res := userGoalStore(deps, "nico")
	if res != nil {
		t.Fatalf("unexpected error opening store: %+v", res)
	}
	goals, err := store.Load()
	if err != nil || len(goals) != 1 {
		t.Fatalf("expected 1 persisted goal, got %v err=%v", goals, err)
	}
}

func TestBTGoalAdd_PriorityAndDeadlineOverrides(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_add", `{"user":"nico","intent":"automate my daily report","priority":0.9,"deadline":5}`)
	goal, ok := out["goal"].(map[string]any)
	if !ok {
		t.Fatalf("expected a goal object, got %v", out["goal"])
	}
	if p, _ := goal["priority"].(float64); p != 0.9 {
		t.Errorf("priority = %v, want 0.9 (explicit override)", goal["priority"])
	}
	if d, _ := goal["deadline"].(float64); d != 5 {
		t.Errorf("deadline = %v, want 5 (explicit override)", goal["deadline"])
	}
}

func TestBTGoalAdd_RejectsMissingUser(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_add", `{"user":"","intent":"automate my daily report"}`)
	if out["error"] == nil {
		t.Errorf("expected an error for missing user, got %v", out)
	}
}

func TestBTGoalAdd_RejectsEmptyIntent(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_add", `{"user":"nico","intent":""}`)
	if out["error"] == nil {
		t.Errorf("expected an error for empty intent, got %v", out)
	}
}

func TestBTGoalList_OrdersByPriorityAndReportsNext(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	store, res := userGoalStore(deps, "nico")
	if res != nil {
		t.Fatalf("unexpected error opening store: %+v", res)
	}
	low := &goap.Goal{Name: "low", Priority: 0.2, Conditions: goap.WorldState{"task_automated": true}}
	high := &goap.Goal{Name: "high", Priority: 0.8, Conditions: goap.WorldState{"task_automated": true}}
	if err := store.Add(low); err != nil {
		t.Fatalf("add low: %v", err)
	}
	if err := store.Add(high); err != nil {
		t.Fatalf("add high: %v", err)
	}

	out := invokeGoal(t, server, "bt_goal_list", `{"user":"nico"}`)
	if c, _ := out["count"].(float64); c != 2 {
		t.Fatalf("count = %v, want 2", out["count"])
	}
	if out["next"] != "high" {
		t.Errorf("next = %v, want %q (higher priority goal, unsatisfied by empty state)", out["next"], "high")
	}
	goals, ok := out["goals"].([]any)
	if !ok || len(goals) != 2 {
		t.Fatalf("expected 2 goals in the list, got %v", out["goals"])
	}
}

func TestBTGoalList_EmptyQueue(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_list", `{"user":"nico"}`)
	if c, _ := out["count"].(float64); c != 0 {
		t.Fatalf("count = %v, want 0", out["count"])
	}
	if out["next"] != "" && out["next"] != nil {
		t.Errorf("next = %v, want empty for an empty queue", out["next"])
	}
	goals, ok := out["goals"].([]any)
	if !ok || len(goals) != 0 {
		t.Fatalf("expected an empty (not null) goals array, got %v", out["goals"])
	}
}

func TestBTGoalRemove(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	store, res := userGoalStore(deps, "nico")
	if res != nil {
		t.Fatalf("unexpected error opening store: %+v", res)
	}
	if err := store.Add(&goap.Goal{Name: "target", Priority: 0.5, Conditions: goap.WorldState{"task_automated": true}}); err != nil {
		t.Fatalf("seed goal: %v", err)
	}

	out := invokeGoal(t, server, "bt_goal_remove", `{"user":"nico","name":"target"}`)
	if out["removed"] != true {
		t.Fatalf("expected removed=true, got %v", out)
	}
	if out["name"] != "target" {
		t.Errorf("name = %v, want %q", out["name"], "target")
	}

	goals, err := store.Load()
	if err != nil || len(goals) != 0 {
		t.Fatalf("expected goal removed from store, got %v err=%v", goals, err)
	}

	// Removing an already-absent goal reports removed=false, not an error.
	out = invokeGoal(t, server, "bt_goal_remove", `{"user":"nico","name":"target"}`)
	if out["removed"] != false {
		t.Errorf("expected removed=false for an unknown goal, got %v", out)
	}
	if out["error"] != nil {
		t.Errorf("removing an absent goal must not be an error, got %v", out["error"])
	}
}

func TestBTGoalCompile_HappyPath(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	add := invokeGoal(t, server, "bt_goal_add", `{"user":"nico","intent":"automate my daily report"}`)
	goal, _ := add["goal"].(map[string]any)
	name, _ := goal["name"].(string)
	if name == "" {
		t.Fatalf("expected a goal name from bt_goal_add, got %v", add)
	}

	args, _ := json.Marshal(map[string]string{"user": "nico", "name": name})
	out := invokeGoal(t, server, "bt_goal_compile", string(args))
	if out["error"] != nil {
		t.Fatalf("unexpected error compiling a freshly added goal: %v", out["error"])
	}
	if out["tree_id"] == "" || out["tree_id"] == nil {
		t.Errorf("expected a tree_id, got %v", out["tree_id"])
	}
	if out["goal"] != name {
		t.Errorf("goal = %v, want %q", out["goal"], name)
	}
	plan, ok := out["plan"].([]any)
	if !ok || len(plan) == 0 {
		t.Errorf("expected a non-empty plan, got %v", out["plan"])
	}
	if nc, _ := out["node_count"].(float64); nc <= 0 {
		t.Errorf("node_count = %v, want > 0", out["node_count"])
	}
	if out["persisted"] != true {
		t.Errorf("expected persisted=true for a user-attributed compile, got %v", out["persisted"])
	}
	if out["seeded_reflection"] != true {
		t.Errorf("expected seeded_reflection=true after a persisted compile, got %v", out["seeded_reflection"])
	}
}

func TestBTGoalCompile_UnknownGoalErrors(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_compile", `{"user":"nico","name":"does-not-exist"}`)
	if out["error"] == nil {
		t.Errorf("expected an error for an unknown goal name, got %v", out)
	}
}

func TestBTGoalFromPattern_NoPatternsErrors(t *testing.T) {
	deps := newFeedbackDeps(t)
	server := engine.NewServer("test")
	registerGoalTools(server, deps)

	out := invokeGoal(t, server, "bt_goal_from_pattern", `{"user":"nico"}`)
	if out["error"] == nil {
		t.Errorf("expected an error when the user has no recorded interactions, got %v", out)
	}
}
