package agent

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestRunOnce_RequiresDeps(t *testing.T) {
	var d *RunDeps
	_, err := d.RunOnce(context.Background(), "x", "task", RunOptions{})
	if err == nil {
		t.Fatal("expected error for nil deps")
	}
}

func TestRunOnce_EmptyAgentName(t *testing.T) {
	d := &RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode { return nil },
	}
	_, err := d.RunOnce(context.Background(), "", "task", RunOptions{})
	if err == nil {
		t.Fatal("expected error for empty agent name")
	}
}

func TestRunOnce_NoTreeFound(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := &RunDeps{
		Registry: reg,
		ResolveTree: func(_ string) *evolution.SerializableNode {
			return nil
		},
	}
	res, err := d.RunOnce(context.Background(), "missing-agent", "do something", RunOptions{})
	if err == nil {
		t.Fatal("expected error when tree not found")
	}
	if res == nil || res.Outcome != "failure" {
		t.Fatalf("expected failure outcome, got %+v", res)
	}
}

// TestRunOnce_UsesUserScopedResolverWhenAgentHasOwner pins ADR-067's
// follow-up milestone: scheduled personal automations register under a
// deterministic slug tree ID (goal:automate_<slug>) that carries no user
// identity of its own, so an unscoped resolver can hand one user's compiled
// automation tree to another user's identically-slugged agent. The owning
// user IS available in scope at RunOnce time via the registered
// Definition's Metadata["user"] (set by cmd/bt-agent's activateAutomation),
// so RunOnce must consult a user-scoped resolver — never the bare, unscoped
// one — whenever the resolved agent has a known owner.
func TestRunOnce_UsesUserScopedResolverWhenAgentHasOwner(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	const treeID = "goal:automate_reports"
	if _, err := reg.Create(Definition{
		Name:     "bob-reports",
		Tree:     treeID,
		Metadata: map[string]string{"user": "bob"},
	}); err != nil {
		t.Fatal(err)
	}

	tree := &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "Noop"}

	var gotUser, gotID string
	scopedCalled := false
	d := &RunDeps{
		Registry: reg,
		ResolveTree: func(id string) *evolution.SerializableNode {
			t.Fatalf("unscoped ResolveTree must not be consulted for agent %q — it has a known owner and a user-scoped resolver is available", id)
			return nil
		},
		ResolveTreeForUser: func(user, id string) *evolution.SerializableNode {
			scopedCalled = true
			gotUser, gotID = user, id
			return tree
		},
	}

	if _, err := d.RunOnce(context.Background(), "bob-reports", "run the report", RunOptions{}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !scopedCalled {
		t.Fatal("expected the user-scoped resolver to be consulted")
	}
	if gotUser != "bob" {
		t.Fatalf("expected requesting user %q, got %q", "bob", gotUser)
	}
	if gotID != treeID {
		t.Fatalf("expected tree id %q, got %q", treeID, gotID)
	}
}

func TestHistoryQualityScore_UsesSpecWhenHigher(t *testing.T) {
	inst := &Instance{
		Definition: Definition{
			Quality: &QualitySpec{MinLength: 100},
		},
	}
	longOut := string(make([]byte, 150))
	for i := range longOut {
		longOut = longOut[:i] + "x" + longOut[i+1:]
	}
	// simpler: just repeat
	longOut = repeatChar('x', 150)
	score := historyQualityScore(inst, "success", longOut)
	if score < 0.5 {
		t.Fatalf("expected quality score >= 0.5, got %f", score)
	}
}

// TestIsRateLimitCarryover pins the single exported exemption check that
// consolidates the previously-duplicated `outcome == RateLimitCarryoverOutcome`
// comparison scattered across scheduler.go, cmd/bt-agent/main.go, and
// dashboard/executor.go — every call site must classify the sentinel (and
// only the sentinel) as a rate-limit carryover, so a future call site can
// consult this helper instead of re-typing the raw comparison and
// reintroducing the classification bug the 2026-07-17 scheduler fix chased.
func TestIsRateLimitCarryover(t *testing.T) {
	if !IsRateLimitCarryover(RateLimitCarryoverOutcome) {
		t.Fatalf("%q must be classified as a rate-limit carryover", RateLimitCarryoverOutcome)
	}
	for _, o := range []string{"success", "no_change", "degraded", "failure", "timeout", "partial", ""} {
		if IsRateLimitCarryover(o) {
			t.Fatalf("%q must not be classified as a rate-limit carryover", o)
		}
	}
}

func repeatChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
