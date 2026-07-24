package agent

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
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

// TestRunOnce_AuctionAwardAttributesHistoryToWinner mirrors
// internal/a2a/server.go's Execute check
// (TestExecute_AuctionAwardAttributesHistoryToWinner in internal/a2a/server_test.go):
// AuctionDelegate writes the winning Award into bb.ChainState["auction_award"]
// when a production tree delegates a subtask through an auction — RunAuction
// dispatches only the real work to the winner, never the losing candidates.
// RunOnce's d.History.Record call today always attributes the run to the
// caller-supplied agentName, even when the tree it ran was really just a thin
// auction wrapper whose real work was done by a different, winning bidder.
//
// internal/agent cannot import internal/a2a (a2a already imports agent, and
// agent importing a2a back would cycle), so RunOnce can't type-assert the
// concrete a2a.Award the way server.go does. It must instead consult a
// cycle-safe extraction hook — AuctionWinnerNameFn, wired from internal/a2a at
// startup the same way engine.AuctionDelegateFn already is (see
// internal/agentexec/wiring.go) — and, when it returns a non-empty winner
// name, record History under that name instead of agentName.
func TestRunOnce_AuctionAwardAttributesHistoryToWinner(t *testing.T) {
	engine.RegisterAction("TestAgentWriteAuctionAward", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		bb.ChainState["auction_award"] = "winner-bot"
		return 1
	})

	prev := AuctionWinnerNameFn
	AuctionWinnerNameFn = func(chainState map[string]any) string {
		name, _ := chainState["auction_award"].(string)
		return name
	}
	t.Cleanup(func() { AuctionWinnerNameFn = prev })

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "TestAgentWriteAuctionAward"},
			{Type: "AlwaysSucceed"},
		},
	}

	hist, err := NewHistory(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}

	d := &RunDeps{
		History:     hist,
		ResolveTree: func(_ string) *evolution.SerializableNode { return tree },
	}

	if _, err := d.RunOnce(context.Background(), "executor", "please handle this task", RunOptions{RecordHistory: true}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	winnerRuns := hist.List("winner-bot", 0)
	if len(winnerRuns) != 1 {
		t.Fatalf("History has %d runs for auction winner %q, want 1: %+v", len(winnerRuns), "winner-bot", winnerRuns)
	}
	if winnerRuns[0].AgentName != "winner-bot" {
		t.Errorf("recorded AgentName = %q, want the auction winner %q", winnerRuns[0].AgentName, "winner-bot")
	}

	executorRuns := hist.List("executor", 0)
	if len(executorRuns) != 0 {
		t.Errorf("History has %d runs for the executing agent %q, want 0 — the auction-won run "+
			"must attribute to the winning bidder, not the executor: %+v", len(executorRuns), "executor", executorRuns)
	}
}

// TestRunOnce_RecordsSLOMetricsOnSuccess pins the NotebookLM-research finding
// that the gardener's validation gate (internal/gardener/validation_gate.go)
// only ever gets SLO evidence from the cron-scheduler closure in
// cmd/bt-agent/main.go (recordSchedulerAttempt) — RunOnce itself, the
// dominant interactive/chat/MCP-driven execution path (bt_agent_run), never
// touches engine.SLOMetrics. That leaves AllowUnverified=true a permanent
// no-op for every tree that is only ever run through chat/MCP instead of the
// scheduler. RunOnce must record success/failure evidence itself so the
// validation gate has real data regardless of which path executed the run.
func TestRunOnce_RecordsSLOMetricsOnSuccess(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "AlwaysSucceed", Name: "ok"}
	d := &RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode { return tree },
	}

	const agentName = "slo-evidence-success-agent"
	if _, err := d.RunOnce(context.Background(), agentName, "do the thing", RunOptions{}); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	snap := engine.GetSLOMetrics(agentName, agentName).Snapshot()
	if snap.TotalCalls != 1 || snap.SuccessfulCalls != 1 {
		t.Fatalf("expected RunOnce to record one successful SLO call for the interactive/MCP path, "+
			"got TotalCalls=%d SuccessfulCalls=%d — RunOnce never records to engine.SLOMetrics today, "+
			"so the gardener validation gate has no evidence for chat/MCP-driven trees",
			snap.TotalCalls, snap.SuccessfulCalls)
	}
}

// TestRunOnce_RecordsSLOMetricsOnFailure is the failure-path twin of
// TestRunOnce_RecordsSLOMetricsOnSuccess above.
func TestRunOnce_RecordsSLOMetricsOnFailure(t *testing.T) {
	engine.RegisterAction("TestRunOnceSLOFailureAction", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		return -1
	})
	tree := &evolution.SerializableNode{Type: "Action", Name: "TestRunOnceSLOFailureAction"}
	d := &RunDeps{
		ResolveTree: func(_ string) *evolution.SerializableNode { return tree },
	}

	const agentName = "slo-evidence-failure-agent"
	if _, err := d.RunOnce(context.Background(), agentName, "do the thing", RunOptions{}); err == nil {
		t.Fatal("expected RunOnce to return an error for a failing tree")
	}

	snap := engine.GetSLOMetrics(agentName, agentName).Snapshot()
	if snap.TotalCalls != 1 || snap.FailedCalls != 1 {
		t.Fatalf("expected RunOnce to record one failed SLO call for the interactive/MCP path, "+
			"got TotalCalls=%d FailedCalls=%d — RunOnce never records to engine.SLOMetrics today, "+
			"so the gardener validation gate has no evidence for chat/MCP-driven trees",
			snap.TotalCalls, snap.FailedCalls)
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
