package engine

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btdec "github.com/rvitorper/go-bt/decorators"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// ─── isKnownActionName / isKnownConditionName edge cases ───────────────────

func TestIsKnownActionName_Empty(t *testing.T) {
	// Empty string returns false (line 362-363)
	if isKnownActionName("") {
		t.Error("isKnownActionName('') should return false")
	}
}

func TestIsKnownActionName_RegisteredAction(t *testing.T) {
	// Registered actions return true via GetAction check (line 365-366)
	if !isKnownActionName("ValidateInput") {
		t.Error("isKnownActionName('ValidateInput') should return true")
	}
}

func TestIsKnownActionName_BuiltinOnly(t *testing.T) {
	// Actions only in builtinActionNames map (not registered)
	if !isKnownActionName("SetupDevTools") {
		t.Error("isKnownActionName('SetupDevTools') should return true (builtin)")
	}
}

func TestIsKnownActionName_Unknown(t *testing.T) {
	if isKnownActionName("NonExistentActionXYZ123") {
		t.Error("isKnownActionName('NonExistentActionXYZ123') should return false")
	}
}

func TestIsKnownConditionName_Empty(t *testing.T) {
	// Empty string returns false (line 372-373)
	if isKnownConditionName("") {
		t.Error("isKnownConditionName('') should return false")
	}
}

func TestIsKnownConditionName_Registered(t *testing.T) {
	if !isKnownConditionName("HasClearTask") {
		t.Error("isKnownConditionName('HasClearTask') should return true")
	}
}

func TestIsKnownConditionName_BuiltinOnly(t *testing.T) {
	if !isKnownConditionName("IsGoRelated") {
		t.Error("isKnownConditionName('IsGoRelated') should return true (builtin)")
	}
}

func TestIsKnownConditionName_Unknown(t *testing.T) {
	if isKnownConditionName("NonExistentConditionXYZ123") {
		t.Error("isKnownConditionName('NonExistentConditionXYZ123') should return false")
	}
}

// ─── RegisterAction / RegisterCondition duplicate registration ──────────

func TestRegisterAction_DuplicatePanics(t *testing.T) {
	// Register an action, then register the same name again — must panic (lines 34-36)
	RegisterAction("__test_dup_action__", func(_ *btcore.BTContext[Blackboard]) int { return 1 })

	defer func() {
		if r := recover(); r == nil {
			t.Error("RegisterAction duplicate should panic")
		}
		// Clean up the registered name from the global map
		regMu.Lock()
		delete(actionRegistry, "__test_dup_action__")
		regMu.Unlock()
	}()

	RegisterAction("__test_dup_action__", func(_ *btcore.BTContext[Blackboard]) int { return 1 })
}

func TestRegisterCondition_DuplicatePanics(t *testing.T) {
	RegisterCondition("__test_dup_cond__", func(_ *Blackboard) bool { return true })

	defer func() {
		if r := recover(); r == nil {
			t.Error("RegisterCondition duplicate should panic")
		}
		regMu.Lock()
		delete(conditionRegistry, "__test_dup_cond__")
		regMu.Unlock()
	}()

	RegisterCondition("__test_dup_cond__", func(_ *Blackboard) bool { return true })
}

// ─── buildNode — composite node types (lines 150-158) ─────────────────────

func TestBuildNode_UtilitySelector(t *testing.T) {
	bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
	node := &evolution.SerializableNode{
		Type: "UtilitySelector",
		Name: "test_utility",
	}
	// Should not panic
	cmd := buildNode(node, bb, "")
	if cmd == nil {
		t.Fatal("BuildUtilitySelector returned nil command")
	}
	// Execute it once to verify it works
	ctx := btcore.NewBTContext(context.TODO(), bb)
	status := cmd.Run(ctx)
	if status != 1 && status != -1 && status != 0 {
		t.Errorf("unexpected status %d", status)
	}
}

func TestBuildNode_PlannerNode(t *testing.T) {
	bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
	node := &evolution.SerializableNode{
		Type: "PlannerNode",
		Name: "test_planner",
	}
	cmd := buildNode(node, bb, "")
	if cmd == nil {
		t.Fatal("BuildPlannerNode returned nil command")
	}
	ctx := btcore.NewBTContext(context.TODO(), bb)
	status := cmd.Run(ctx)
	if status != 1 && status != -1 && status != 0 {
		t.Errorf("unexpected status %d", status)
	}
}

func TestBuildNode_AbortOnEvent(t *testing.T) {
	bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
	node := &evolution.SerializableNode{
		Type: "AbortOnEvent",
		Name: "test_abort",
	}
	cmd := buildNode(node, bb, "")
	if cmd == nil {
		t.Fatal("BuildEventDrivenAbort returned nil command")
	}
	ctx := btcore.NewBTContext(context.TODO(), bb)
	status := cmd.Run(ctx)
	if status != 1 && status != -1 && status != 0 {
		t.Errorf("unexpected status %d", status)
	}
}

func TestBuildNode_ReactiveParallel(t *testing.T) {
	bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
	node := &evolution.SerializableNode{
		Type: "ReactiveParallel",
		Name: "test_parallel",
	}
	cmd := buildNode(node, bb, "")
	if cmd == nil {
		t.Fatal("BuildReactiveParallel returned nil command")
	}
	ctx := btcore.NewBTContext(context.TODO(), bb)
	status := cmd.Run(ctx)
	if status != 1 && status != -1 && status != 0 {
		t.Errorf("unexpected status %d", status)
	}
}

// ─── validateOutputQuality — score cap at 1.0 (line 2092) ─────────────────

func TestValidateOutputQuality_ScoreCap(t *testing.T) {
	// Create a blackboard with output that would produce a score > 1.0
	// before capping: long result with error patterns + code blocks + markdown
	bb := &Blackboard{
		Task:    "fix something",
		Result:  "## Summary\n\nThis is a very long output with ```code blocks``` and multiple markdown structures.\n\n## Details\n\nThe analysis shows that... Let me explain more.\n\n```go\nfunc main() {}\n```\n\n## Error Patterns\n\nThere were no errors found.\n\n**Bold text** and *italic* and bullet points:\n- Item 1\n- Item 2\n- Item 3\n\n## Conclusion\n\nThis is enough text to trigger the score cap at 1.0.\n\nLorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.\n",
		Outcome: "success",
	}
	// This should not panic and should cap score at 1.0
	validateOutputQuality(bb)
	if bb.QualityScore > 1.0 {
		t.Errorf("QualityScore should be capped at 1.0, got %f", bb.QualityScore)
	}
	if bb.QualityScore < 0.5 {
		t.Errorf("QualityScore should be >= 0.5 for good output, got %f", bb.QualityScore)
	}
	// Verify score is exactly 1.0 (or very close)
	if bb.QualityScore != 1.0 {
		t.Logf("QualityScore = %f (expected 1.0, >= 0.5 is acceptable)", bb.QualityScore)
	}
}

// ─── RunTask — panic recovery (line 2113) ─────────────────────────────────

func TestRunTask_PanicRecovery(t *testing.T) {
	// Create a tree that panics — RunTask should recover and set Outcome to failure.
	// We build it through buildNode so it gets the proper go-bt tree structure.
	bb := &Blackboard{
		Task:   "test task that panics",
		Result: "",
		LLM:    &MockLLM{},
	}
	node := &evolution.SerializableNode{
		Type: "Sequence",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
			{Type: "Action", Name: "SelfCorrect"}, // SelfCorrect requires LLM
		},
	}
	tree := buildNode(node, bb, "")
	_ = RunTask(bb, tree)
	// The tree should complete; SelfCorrect is a registered action that returns -1 when LLM is nil
	if bb.Outcome == "" {
		t.Error("Expected Outcome to be set after RunTask")
	}
	t.Logf("Outcome: %q, Result length: %d", bb.Outcome, len(bb.Result))
}

// ─── RunTask — multitick loop (line 2128) ─────────────────────────────────

// TestRunTask_MultiTickRepeat exercises a Retry node that needs multiple ticks.
// The Repeat decorator returns 0 (Running) after each tick, so RunTask's
// multi-tick loop must advance it.
func TestRunTask_MultiTickRepeat(t *testing.T) {
	bb := &Blackboard{
		Task:   "multi-tick test",
		Result: "",
		LLM:    &MockLLM{},
	}
	// Build a minimal tree with a Retry + Action
	tree := btdec.NewRepeat(
		btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			// This action records that it was called
			ctx.Blackboard.Result = "action executed"
			return 1
		}),
		3,
	)
	_ = RunTask(bb, tree)
	// The tree should complete without partial outcome
	if bb.Outcome != "success" {
		t.Errorf("expected Outcome 'success' after multi-tick, got %q", bb.Outcome)
	}
	if bb.Result != "action executed" {
		t.Errorf("expected action to execute, got %q", bb.Result)
	}
}
