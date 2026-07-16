package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestConsiderTreeCompileAction pins the in-tree autopilot seam (ADR-133
// Phase 4): the ConsiderTreeCompile action is registered, invokes the
// injection hook with the run's persona user on good outcomes, and is a
// successful no-op for anonymous runs, failed runs, and unwired hooks —
// automation proposals must never fail the surrounding tree.
func TestConsiderTreeCompileAction(t *testing.T) {
	action := GetAction("ConsiderTreeCompile")
	if action == nil {
		t.Fatal("ConsiderTreeCompile must be registered as an engine action")
	}

	prev := ConsiderAutomationFn
	defer func() { ConsiderAutomationFn = prev }()

	run := func(bb *Blackboard) int {
		return action(&btcore.BTContext[Blackboard]{Blackboard: bb})
	}

	// Good user-attributed run → hook fires with the user.
	var gotUser string
	ConsiderAutomationFn = func(user string) { gotUser = user }
	bb := &Blackboard{ChainState: map[string]interface{}{"persona_user": "nico"}, Outcome: "success"}
	if status := run(bb); status != 1 {
		t.Fatalf("ConsiderTreeCompile must succeed, got status %d", status)
	}
	if gotUser != "nico" {
		t.Errorf("hook must receive the persona user, got %q", gotUser)
	}

	// Failed run → no proposal, still success.
	gotUser = ""
	bb.Outcome = "failure"
	if status := run(bb); status != 1 {
		t.Fatalf("failed-run path must still return success, got %d", status)
	}
	if gotUser != "" {
		t.Errorf("hook must not fire for failed runs, got user %q", gotUser)
	}

	// Anonymous run → no proposal.
	bb = &Blackboard{ChainState: map[string]interface{}{}, Outcome: "success"}
	if status := run(bb); status != 1 {
		t.Fatalf("anonymous path must return success, got %d", status)
	}
	if gotUser != "" {
		t.Errorf("hook must not fire without a persona user, got %q", gotUser)
	}

	// Unwired hook → safe no-op.
	ConsiderAutomationFn = nil
	bb = &Blackboard{ChainState: map[string]interface{}{"persona_user": "nico"}, Outcome: "success"}
	if status := run(bb); status != 1 {
		t.Fatalf("nil-hook path must return success, got %d", status)
	}
}
