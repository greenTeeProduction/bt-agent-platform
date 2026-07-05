package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/research"
	btcore "github.com/rvitorper/go-bt/core"
)

func newLoopBB(t *testing.T) *Blackboard {
	t.Helper()
	mgr := blackboard.NewManager(nil)
	return &Blackboard{BB: blackboard.NewHandle(mgr, "run-x", "", "goap-fusion-loop-runner"), ChainState: map[string]any{}}
}

func TestClearGoapFusionStateHashes(t *testing.T) {
	bb := newLoopBB(t)
	saveGoapFusionStateHashes(bb, []string{"h1", "h1", "h1"})
	if len(goapFusionStateHashes(bb)) != 3 {
		t.Fatal("setup: expected 3 hashes")
	}
	ClearGoapFusionStateHashes(bb)
	if len(goapFusionStateHashes(bb)) != 0 {
		t.Fatalf("history must be empty after clear, got %v", goapFusionStateHashes(bb))
	}
}

func TestPublishStateHashResetsOnMilestoneTransition(t *testing.T) {
	bb := newLoopBB(t)
	pub := GetAction("PublishGoapFusionStateHash")

	// Milestone A: publish the same goal-queue hash 3 times (would trip the breaker).
	bb.ChainState["goap_fusion_program_milestone"] = "prog:0"
	bb.ChainState["goap_fusion_goal_queue"] = "[P0] milestone A goal"
	for i := 0; i < 3; i++ {
		pub(&btcore.BTContext[Blackboard]{Blackboard: bb})
	}
	if got := len(goapFusionStateHashes(bb)); got != 3 {
		t.Fatalf("milestone A: want 3 hashes, got %d", got)
	}

	// Transition to milestone B: the window must reset to a single fresh hash.
	bb.ChainState["goap_fusion_program_milestone"] = "prog:1"
	bb.ChainState["goap_fusion_goal_queue"] = "[P0] milestone B goal"
	pub(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if got := len(goapFusionStateHashes(bb)); got != 1 {
		t.Fatalf("milestone transition must reset window to 1, got %d", got)
	}
}

func TestCircuitBreakerWouldHaltStaleWindowWithoutReset(t *testing.T) {
	// Sanity: three identical hashes DO trip the shared verdict — confirming
	// the reset is what prevents the false HALT, not a weakened breaker.
	halt, _, _, _ := goapFusionCircuitPolicyVerdict([]string{"d3b7", "d3b7", "d3b7"}, 0)
	if !halt {
		t.Fatal("three identical hashes must still trip the breaker (reset, not weaken)")
	}
}

func TestCircuitBreakerBypassedForActiveProgram(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Prog", "test", []string{"m1 in internal/a2a/a.go", "m2 in internal/engine/b.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}

	// A window of three identical hashes that WOULD trip the breaker.
	bb := newLoopBB(t)
	saveGoapFusionStateHashes(bb, []string{"d3b7", "d3b7", "d3b7"})

	for _, action := range []string{"EvaluateScheduledGoapFusionCircuitBreaker", "RunScheduledGoapFusionLoop"} {
		fn := GetAction(action)
		if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
			t.Fatalf("%s: active program must bypass the stagnation halt (CONTINUE=1), got %d: %s", action, got, bb.Result)
		}
	}

	// With NO active program, the same window must still HALT.
	ps.MarkDone(ps.Programs[0].ID, 0, "r")
	ps.MarkDone(ps.Programs[0].ID, 1, "r")
	_ = ps.Save()
	fn := GetAction("EvaluateScheduledGoapFusionCircuitBreaker")
	if got := fn(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("no active program: repeated hash must still HALT, got %d", got)
	}
}
