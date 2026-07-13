package agentexec

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestResolveGeneratedTreeForUser_TwoUserCollision pins ADR-010 personalization
// hardening (Q1 Correctness): deterministic slug IDs like
// "goal:automate_reports" are derived from the goal text, not the user, so two
// different users can independently compile a personal tree under the exact
// same ID. Resolution must be scoped to the requesting user — bob asking for
// his automation must never receive alice's tree just because "alice" sorts
// before "bob" in the legacy all-users scan (resolveUserTree).
func TestResolveGeneratedTreeForUser_TwoUserCollision(t *testing.T) {
	usersRoot := t.TempDir()
	origDir, origUsers := generatedTreeDir, usersTreeRoot
	generatedTreeDir, usersTreeRoot = t.TempDir(), usersRoot
	defer func() { generatedTreeDir, usersTreeRoot = origDir, origUsers }()

	const id = "goal:automate_reports"

	aliceTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Alice_AutomateReports",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
		},
	}
	bobTree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Bob_AutomateReports",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}

	if _, err := evolution.SaveNamedTree(usersRoot+"/alice/trees", id, aliceTree); err != nil {
		t.Fatalf("SaveNamedTree(alice): %v", err)
	}
	if _, err := evolution.SaveNamedTree(usersRoot+"/bob/trees", id, bobTree); err != nil {
		t.Fatalf("SaveNamedTree(bob): %v", err)
	}

	gotBob := ResolveGeneratedTreeForUser("bob", id)
	if gotBob == nil {
		t.Fatal("bob: expected personal tree, got nil")
	}
	if gotBob.Name != "Bob_AutomateReports" {
		t.Fatalf("bob resolved %q — got someone else's tree (want Bob_AutomateReports)", gotBob.Name)
	}

	gotAlice := ResolveGeneratedTreeForUser("alice", id)
	if gotAlice == nil {
		t.Fatal("alice: expected personal tree, got nil")
	}
	if gotAlice.Name != "Alice_AutomateReports" {
		t.Fatalf("alice resolved %q — got someone else's tree (want Alice_AutomateReports)", gotAlice.Name)
	}
}

// TestResolveTreeIDForUser_ScopesToRequestingUser pins the domains-level entry
// point: ResolveTreeIDForUser must consult the per-user dynamic resolver hook
// (DynamicResolveForUserFn) instead of the unscoped DynamicResolveFn when a
// non-empty user is supplied, so the production ID→tree lookup used by any
// agentexec-linked binary respects user isolation.
func TestResolveTreeIDForUser_ScopesToRequestingUser(t *testing.T) {
	usersRoot := t.TempDir()
	origDir, origUsers := generatedTreeDir, usersTreeRoot
	generatedTreeDir, usersTreeRoot = t.TempDir(), usersRoot
	defer func() { generatedTreeDir, usersTreeRoot = origDir, origUsers }()

	const id = "goal:automate_invoices"

	aliceTree := &evolution.SerializableNode{Type: "Sequence", Name: "Alice_Invoices"}
	bobTree := &evolution.SerializableNode{Type: "Sequence", Name: "Bob_Invoices"}

	if _, err := evolution.SaveNamedTree(usersRoot+"/alice/trees", id, aliceTree); err != nil {
		t.Fatalf("SaveNamedTree(alice): %v", err)
	}
	if _, err := evolution.SaveNamedTree(usersRoot+"/bob/trees", id, bobTree); err != nil {
		t.Fatalf("SaveNamedTree(bob): %v", err)
	}

	got := domains.ResolveTreeIDForUser("bob", id)
	if got == nil || got.Name != "Bob_Invoices" {
		t.Fatalf("ResolveTreeIDForUser(bob, %q) = %+v, want Bob_Invoices", id, got)
	}
}
