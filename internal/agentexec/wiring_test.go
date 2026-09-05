package agentexec

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// TestResolveGeneratedTreeForUser_TwoUserCollision pins ADR-133 personalization
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

// TestResolveGeneratedTreeForUser_AutomationStatusGate pins Q4
// Personalization & Self-Growth milestone 1: a personal tree tied to a
// not-yet-approved or rejected automation proposal must never be handed back
// as runnable, even though the tree file itself exists on disk — only an
// "approved" persona.AutomationRecord.Status may execute. The matching
// record is found by AutomationRecord.TreeID.
func TestResolveGeneratedTreeForUser_AutomationStatusGate(t *testing.T) {
	usersRoot := t.TempDir()
	origDir, origUsers := generatedTreeDir, usersTreeRoot
	generatedTreeDir, usersTreeRoot = t.TempDir(), usersRoot
	defer func() { generatedTreeDir, usersTreeRoot = origDir, origUsers }()

	const owner = "carol"
	personaStore, err := persona.NewStore(usersRoot)
	if err != nil {
		t.Fatalf("persona.NewStore: %v", err)
	}
	autoStore, err := persona.NewAutomationStore(personaStore.Workspace(owner))
	if err != nil {
		t.Fatalf("persona.NewAutomationStore: %v", err)
	}

	for _, tc := range []struct {
		name     string
		status   string
		runnable bool
	}{
		{"pending", persona.AutomationPending, false},
		{"rejected", persona.AutomationRejected, false},
		{"approved", persona.AutomationApproved, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "goal:automate_" + tc.name
			tree := &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "Carol_" + tc.name}
			if _, err := evolution.SaveNamedTree(usersRoot+"/"+owner+"/trees", id, tree); err != nil {
				t.Fatalf("SaveNamedTree: %v", err)
			}
			if err := autoStore.Upsert(persona.AutomationRecord{
				Signature: "sig-" + tc.name,
				Status:    tc.status,
				TreeID:    id,
			}); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			got := ResolveGeneratedTreeForUser(owner, id)
			if tc.runnable && got == nil {
				t.Fatalf("%s automation: expected runnable tree, got nil", tc.status)
			}
			if !tc.runnable && got != nil {
				t.Fatalf("%s automation: expected execution to be gated (nil), got tree %+v", tc.status, got)
			}
		})
	}
}

// TestResolveGeneratedTree_AutomationStatusGate pins the unscoped resolver's
// half of the same gate (resolveUserTree's cross-user scan): a pending
// automation's tree must not be surfaced even when it is the only candidate
// found across all user workspaces.
func TestResolveGeneratedTree_AutomationStatusGate(t *testing.T) {
	usersRoot := t.TempDir()
	origDir, origUsers := generatedTreeDir, usersTreeRoot
	generatedTreeDir, usersTreeRoot = t.TempDir(), usersRoot
	defer func() { generatedTreeDir, usersTreeRoot = origDir, origUsers }()

	const owner = "dave"
	const id = "goal:automate_invoices"

	tree := &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "Dave_Invoices"}
	if _, err := evolution.SaveNamedTree(usersRoot+"/"+owner+"/trees", id, tree); err != nil {
		t.Fatalf("SaveNamedTree: %v", err)
	}

	personaStore, err := persona.NewStore(usersRoot)
	if err != nil {
		t.Fatalf("persona.NewStore: %v", err)
	}
	autoStore, err := persona.NewAutomationStore(personaStore.Workspace(owner))
	if err != nil {
		t.Fatalf("persona.NewAutomationStore: %v", err)
	}
	if err := autoStore.Upsert(persona.AutomationRecord{
		Signature: "sig-dave-invoices",
		Status:    persona.AutomationPending,
		TreeID:    id,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if got := ResolveGeneratedTree(id); got != nil {
		t.Fatalf("expected pending automation's tree to be gated even via the unscoped resolver, got %+v", got)
	}
}

// TestRunOnce_RefusesExecutionForNonApprovedAutomation pins the RunOnce side
// of milestone 1: when the resolved tree is gated by a pending or rejected
// AutomationRecord, RunOnce's fallback resolution path must refuse to
// execute — never fall through to running the tree anyway — while an
// approved automation still runs normally end to end.
func TestRunOnce_RefusesExecutionForNonApprovedAutomation(t *testing.T) {
	usersRoot := t.TempDir()
	origDir, origUsers := generatedTreeDir, usersTreeRoot
	generatedTreeDir, usersTreeRoot = t.TempDir(), usersRoot
	defer func() { generatedTreeDir, usersTreeRoot = origDir, origUsers }()

	const owner = "bob"
	personaStore, err := persona.NewStore(usersRoot)
	if err != nil {
		t.Fatalf("persona.NewStore: %v", err)
	}
	autoStore, err := persona.NewAutomationStore(personaStore.Workspace(owner))
	if err != nil {
		t.Fatalf("persona.NewAutomationStore: %v", err)
	}

	for _, tc := range []struct {
		name        string
		status      string
		wantErr     bool
		wantOutcome string
	}{
		{"pending", persona.AutomationPending, true, "failure"},
		{"rejected", persona.AutomationRejected, true, "failure"},
		{"approved", persona.AutomationApproved, false, "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "goal:automate_" + tc.name

			tree := &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "Noop"}
			if _, err := evolution.SaveNamedTree(usersRoot+"/"+owner+"/trees", id, tree); err != nil {
				t.Fatalf("SaveNamedTree: %v", err)
			}
			if err := autoStore.Upsert(persona.AutomationRecord{
				Signature: "sig-" + tc.name,
				Status:    tc.status,
				TreeID:    id,
			}); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			reg, err := agent.NewRegistry(t.TempDir())
			if err != nil {
				t.Fatalf("NewRegistry: %v", err)
			}
			agentName := owner + "-" + tc.name
			if _, err := reg.Create(agent.Definition{
				Name:     agentName,
				Tree:     id,
				Metadata: map[string]string{"user": owner},
			}); err != nil {
				t.Fatalf("Registry.Create: %v", err)
			}

			d := &agent.RunDeps{
				Registry:           reg,
				ResolveTree:        ResolveGeneratedTree,
				ResolveTreeForUser: ResolveGeneratedTreeForUser,
			}

			res, err := d.RunOnce(context.Background(), agentName, "run the report", agent.RunOptions{})
			if tc.wantErr && err == nil {
				t.Fatalf("%s automation: expected RunOnce to refuse execution, got success (result=%+v)", tc.status, res)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%s automation: expected RunOnce to succeed, got error: %v", tc.status, err)
			}
			if res == nil || res.Outcome != tc.wantOutcome {
				t.Fatalf("%s automation: expected outcome %q, got %+v", tc.status, tc.wantOutcome, res)
			}
		})
	}
}
