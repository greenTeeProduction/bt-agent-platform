package engine

import (
	"fmt"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// ─── AuctionDelegate Tests ──────────────────────────────────────────────────
//
// AuctionDelegate wires auction-based A2A task allocation into the engine as a
// behavior tree action. Because internal/a2a imports internal/engine, the
// engine cannot import a2a; the auction is reached through the injected
// AuctionDelegateFn hook (mirroring DelegateToA2AFn / DelegateToTreeFn). When
// the auction produces no eligible bidders, the action falls back to running
// the task through a delegate tree via DelegateToTreeFn.

func TestAuctionDelegate_Registered(t *testing.T) {
	if _, ok := actionRegistry["AuctionDelegate"]; !ok {
		t.Fatal("AuctionDelegate action not registered")
	}
}

func TestAuctionDelegate_AuctionWinsReturnsWinnerResult(t *testing.T) {
	action, ok := actionRegistry["AuctionDelegate"]
	if !ok {
		t.Fatal("AuctionDelegate action not registered")
	}

	var gotTask string
	origAuction := AuctionDelegateFn
	AuctionDelegateFn = func(task string, _ map[string]any) (string, bool, error) {
		gotTask = task
		return "winner completed the task", true, nil
	}
	defer func() { AuctionDelegateFn = origAuction }()

	// If the auction awards a winner, the fallback tree must never be invoked.
	origTree := DelegateToTreeFn
	DelegateToTreeFn = func(_ string, _ *Blackboard) (string, error) {
		t.Fatal("fallback DelegateToTreeFn must not run when the auction awards a winner")
		return "", nil
	}
	defer func() { DelegateToTreeFn = origTree }()

	bb := &Blackboard{Task: "allocate this unit of work"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != 1 {
		t.Fatalf("expected success (1), got %d", status)
	}
	if bb.Outcome != "success" {
		t.Errorf("expected outcome=success, got %s", bb.Outcome)
	}
	if bb.Result != "winner completed the task" {
		t.Errorf("expected winner result, got %q", bb.Result)
	}
	if gotTask != "allocate this unit of work" {
		t.Errorf("auction hook received wrong task: %q", gotTask)
	}
}

func TestAuctionDelegate_NoBiddersFallsBackToDelegateToTree(t *testing.T) {
	action, ok := actionRegistry["AuctionDelegate"]
	if !ok {
		t.Fatal("AuctionDelegate action not registered")
	}

	origAuction := AuctionDelegateFn
	// awarded=false signals no candidate submitted an eligible bid.
	AuctionDelegateFn = func(_ string, _ map[string]any) (string, bool, error) {
		return "", false, nil
	}
	defer func() { AuctionDelegateFn = origAuction }()

	var gotTreeID string
	origTree := DelegateToTreeFn
	DelegateToTreeFn = func(treeID string, _ *Blackboard) (string, error) {
		gotTreeID = treeID
		return "tree handled the task", nil
	}
	defer func() { DelegateToTreeFn = origTree }()

	bb := &Blackboard{
		Task: "allocate this unit of work",
		ChainState: map[string]any{
			"delegate_tree_id": "worker-tree",
		},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != 1 {
		t.Fatalf("expected fallback success (1), got %d", status)
	}
	if bb.Outcome != "success" {
		t.Errorf("expected outcome=success, got %s", bb.Outcome)
	}
	if bb.Result != "tree handled the task" {
		t.Errorf("expected fallback tree result, got %q", bb.Result)
	}
	if gotTreeID != "worker-tree" {
		t.Errorf("fallback used wrong tree id: %q", gotTreeID)
	}
}

func TestAuctionDelegate_NoBiddersNoFallbackTargetFails(t *testing.T) {
	action, ok := actionRegistry["AuctionDelegate"]
	if !ok {
		t.Fatal("AuctionDelegate action not registered")
	}

	origAuction := AuctionDelegateFn
	AuctionDelegateFn = func(_ string, _ map[string]any) (string, bool, error) {
		return "", false, nil
	}
	defer func() { AuctionDelegateFn = origAuction }()

	// No delegate_tree_id in chain state — the fallback has no target.
	bb := &Blackboard{Task: "allocate this unit of work"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != -1 {
		t.Fatalf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
}

func TestAuctionDelegate_AuctionErrorFails(t *testing.T) {
	action, ok := actionRegistry["AuctionDelegate"]
	if !ok {
		t.Fatal("AuctionDelegate action not registered")
	}

	origAuction := AuctionDelegateFn
	AuctionDelegateFn = func(_ string, _ map[string]any) (string, bool, error) {
		return "", false, fmt.Errorf("transport exploded")
	}
	defer func() { AuctionDelegateFn = origAuction }()

	// A genuine auction error must not silently fall through to the tree.
	origTree := DelegateToTreeFn
	DelegateToTreeFn = func(_ string, _ *Blackboard) (string, error) {
		t.Fatal("fallback must not run on a hard auction error")
		return "", nil
	}
	defer func() { DelegateToTreeFn = origTree }()

	bb := &Blackboard{
		Task:       "allocate this unit of work",
		ChainState: map[string]any{"delegate_tree_id": "worker-tree"},
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != -1 {
		t.Fatalf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
}

func TestAuctionDelegate_HookNotConfiguredFails(t *testing.T) {
	action, ok := actionRegistry["AuctionDelegate"]
	if !ok {
		t.Fatal("AuctionDelegate action not registered")
	}

	origAuction := AuctionDelegateFn
	AuctionDelegateFn = nil
	defer func() { AuctionDelegateFn = origAuction }()

	bb := &Blackboard{Task: "allocate this unit of work"}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != -1 {
		t.Fatalf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
}

func TestAuctionDelegate_MissingTaskFails(t *testing.T) {
	action, ok := actionRegistry["AuctionDelegate"]
	if !ok {
		t.Fatal("AuctionDelegate action not registered")
	}

	origAuction := AuctionDelegateFn
	AuctionDelegateFn = func(_ string, _ map[string]any) (string, bool, error) {
		t.Fatal("auction hook must not run without a task")
		return "", false, nil
	}
	defer func() { AuctionDelegateFn = origAuction }()

	bb := &Blackboard{Task: ""}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if status := action(ctx); status != -1 {
		t.Fatalf("expected failure (-1), got %d", status)
	}
	if bb.Outcome != "failure" {
		t.Errorf("expected outcome=failure, got %s", bb.Outcome)
	}
}
