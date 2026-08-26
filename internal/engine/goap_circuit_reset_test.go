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
	for range 3 {
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

func TestRepeatedHashHaltBypassedInNormalOperation(t *testing.T) {
	// Readable program store (active program OR idle-about-to-seed) → the
	// crude repeated-hash halt is bypassed; a window of identical hashes
	// CONTINUES so the cycle can do real work / reach seeding.
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Prog", "test", []string{"m1 in internal/a2a/a.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	bb := newLoopBB(t)
	saveGoapFusionStateHashes(bb, []string{"d3b7", "d3b7", "d3b7"})
	for _, action := range []string{"EvaluateScheduledGoapFusionCircuitBreaker", "RunScheduledGoapFusionLoop"} {
		if got := GetAction(action)(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
			t.Fatalf("%s: repeated hash must be bypassed with a readable store (CONTINUE=1), got %d: %s", action, got, bb.Result)
		}
	}

	// Idle (no active program) but readable store → still bypassed (CONTINUE),
	// so the cycle reaches Phase 0.5 seeding instead of HALTing on stale hashes.
	ps.MarkDone(ps.Programs[0].ID, 0, "r")
	_ = ps.Save()
	if got := GetAction("EvaluateScheduledGoapFusionCircuitBreaker")(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("idle+readable store must bypass so the loop can seed, got %d", got)
	}
}

// The no-op-patch streak — the GENUINE Activity-Progress-Confusion signal — must
// still HALT even when the repeated-hash check is bypassed.
func TestNoopPatchStreakStillHaltsUnderBypass(t *testing.T) {
	path := withGoapPrograms(t)
	ps, _ := research.OpenPrograms(path)
	ps.Add("Prog", "test", []string{"m1 in internal/a2a/a.go"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	halt, _, _, noop := goapFusionCircuitPolicyVerdictWithBypass([]string{"a", "b", "c"}, goapFusionMaxNoopPatchStreak, true)
	if !halt || !noop {
		t.Fatal("no-op-patch streak must HALT even under repeated-hash bypass")
	}
}
