// internal/engine/review_cycle_test.go
package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewCycle_ReRunsChildUntilApprovedWithinBound(t *testing.T) {
	childRuns, reviews := 0, 0
	registerReviewAction(t, "TestRCChild", func(_ *btcore.BTContext[Blackboard]) int {
		childRuns++
		return 1
	})
	registerReviewAction(t, "TestRCReviewer", func(ctx *btcore.BTContext[Blackboard]) int {
		reviews++
		if reviews < 2 {
			ctx.Blackboard.ChainState["review_verdict"] = "needs_work"
			ctx.Blackboard.ChainState["review_feedback"] = "tighten error handling"
		} else {
			ctx.Blackboard.ChainState["review_verdict"] = "approved"
		}
		return 1
	})
	cmd := buildNode(&evolution.SerializableNode{
		Type: "ReviewCycle", Name: "RCUnderTest",
		Metadata: map[string]any{"reviewer_action": "TestRCReviewer", "max_iterations": 3},
		Children: []evolution.SerializableNode{{Type: "Action", Name: "TestRCChild"}},
	}, newTestBlackboard(), "")
	ctx := newTestBTContext(newTestBlackboard())
	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("want SUCCESS after approval, got %d", got)
	}
	if childRuns != 2 || reviews != 2 {
		t.Fatalf("want 2 child runs + 2 reviews, got %d/%d", childRuns, reviews)
	}
}

// TestReviewCycle_BoundSurvivesRunningChild proves the max_iterations budget
// persists across separate BT ticks (via ChainState), rather than resetting
// whenever the child returns RUNNING. The child returns RUNNING on its first
// call within an iteration and SUCCESS on its second, forcing the parent
// Action itself to return RUNNING/re-tick between reviewer invocations. With
// the old closure-local loop counter, every re-tick restarted at iter=0,
// letting the reviewer run more than max_iterations times before exhaustion.
func TestReviewCycle_BoundSurvivesRunningChild(t *testing.T) {
	childCallsThisIter := 0
	registerReviewAction(t, "TestRCPersistChild", func(_ *btcore.BTContext[Blackboard]) int {
		childCallsThisIter++
		if childCallsThisIter%2 == 1 {
			return 0 // RUNNING on the first call of each iteration
		}
		return 1 // SUCCESS on the second call
	})
	reviews := 0
	registerReviewAction(t, "TestRCPersistReviewer", func(ctx *btcore.BTContext[Blackboard]) int {
		reviews++
		ctx.Blackboard.ChainState["review_verdict"] = "needs_work"
		ctx.Blackboard.ChainState["review_feedback"] = "still not good enough"
		return 1
	})
	bb := newTestBlackboard()
	cmd := buildNode(&evolution.SerializableNode{
		Type: "ReviewCycle", Name: "RCPersist",
		Metadata: map[string]any{"reviewer_action": "TestRCPersistReviewer", "max_iterations": 2},
		Children: []evolution.SerializableNode{{Type: "Action", Name: "TestRCPersistChild"}},
	}, bb, "")
	ctx := newTestBTContext(bb)

	var last int
	for range 10 {
		last = cmd.Run(ctx)
		if last != 0 {
			break
		}
	}

	if last != -1 {
		t.Fatalf("want FAILURE (bound exhausted) after repeated ticks, got %d", last)
	}
	if reviews != 2 {
		t.Fatalf("want exactly 2 reviewer invocations (max_iterations=2), got %d", reviews)
	}
	if _, present := ctx.Blackboard.ChainState["reviewcycle/RCPersist/iter"]; present {
		t.Fatalf("want reviewcycle/RCPersist/iter cleared after terminal tick, got %v",
			ctx.Blackboard.ChainState["reviewcycle/RCPersist/iter"])
	}
	if _, present := ctx.Blackboard.ChainState["review_verdict"]; present {
		t.Fatalf("want review_verdict cleared after terminal tick, got %v",
			ctx.Blackboard.ChainState["review_verdict"])
	}
	if _, present := ctx.Blackboard.ChainState["review_feedback"]; present {
		t.Fatalf("want review_feedback cleared after terminal tick, got %v",
			ctx.Blackboard.ChainState["review_feedback"])
	}
}

func TestReviewCycle_FailsAfterMaxIterations(t *testing.T) {
	registerReviewAction(t, "TestRCChildB", func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	registerReviewAction(t, "TestRCReviewerB", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.ChainState["review_verdict"] = "needs_work"
		return 1
	})
	cmd := buildNode(&evolution.SerializableNode{
		Type: "ReviewCycle", Name: "RCBound",
		Metadata: map[string]any{"reviewer_action": "TestRCReviewerB", "max_iterations": 2},
		Children: []evolution.SerializableNode{{Type: "Action", Name: "TestRCChildB"}},
	}, newTestBlackboard(), "")
	if got := cmd.Run(newTestBTContext(newTestBlackboard())); got != -1 {
		t.Fatalf("want FAILURE after bound exhausted, got %d", got)
	}
}
