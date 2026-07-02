// internal/engine/review_cycle_test.go
package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestReviewCycle_ReRunsChildUntilApprovedWithinBound(t *testing.T) {
	childRuns, reviews := 0, 0
	RegisterAction("TestRCChild", func(_ *btcore.BTContext[Blackboard]) int {
		childRuns++
		return 1
	})
	RegisterAction("TestRCReviewer", func(ctx *btcore.BTContext[Blackboard]) int {
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

func TestReviewCycle_FailsAfterMaxIterations(t *testing.T) {
	RegisterAction("TestRCChildB", func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	RegisterAction("TestRCReviewerB", func(ctx *btcore.BTContext[Blackboard]) int {
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
