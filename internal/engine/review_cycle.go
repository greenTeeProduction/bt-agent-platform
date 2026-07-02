// internal/engine/review_cycle.go
package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildReviewCycle implements the requesting/receiving-code-review loop:
// run child → run reviewer action → verdict "approved" ⇒ SUCCESS;
// "needs_work" ⇒ re-run child with feedback, up to max_iterations, then FAIL.
// Missing/unparseable verdict counts as needs_work (safe default).
//
// The iteration count persists in ChainState ("reviewcycle/<name>/iter", read
// via chainStateInt so it tolerates the float64 shape from JSON round-trips)
// rather than living in a closure-local loop variable, mirroring ForEachTask's
// cursor persistence. This matters because the child (e.g. a SemaphoreGuard or
// RateLimit node) may return RUNNING mid-iteration: without persistence, the
// next BT tick would restart at iter=0 and grant a fresh max_iterations budget
// every re-tick instead of resuming where it left off. The key is deleted on
// every terminal path (approved SUCCESS, child FAILURE, reviewer FAILURE,
// bound-exhausted FAILURE); while RUNNING, it holds the current iteration.
// ChainState["review_verdict"] and ["review_feedback"] are likewise cleared on
// every terminal path so a stale needs_work verdict never leaks past this node.
func BuildReviewCycle(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	reviewerName, _ := node.Metadata["reviewer_action"].(string)
	reviewer := GetAction(reviewerName)
	if reviewer == nil || len(node.Children) != 1 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "ReviewCycle requires metadata.reviewer_action (registered) and one child"
			return -1
		})
	}
	maxIter := 3
	switch v := node.Metadata["max_iterations"].(type) {
	case int:
		maxIter = v
	case float64:
		maxIter = int(v)
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	key := "reviewcycle/" + node.Name + "/iter"
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		iter, _ := chainStateInt(ctx.Blackboard, key)
		for ; iter < maxIter; iter++ {
			ctx.Blackboard.ChainState[key] = iter
			switch code := child.Run(ctx); {
			case code == 0:
				return 0
			case code < 0:
				delete(ctx.Blackboard.ChainState, key)
				delete(ctx.Blackboard.ChainState, "review_verdict")
				delete(ctx.Blackboard.ChainState, "review_feedback")
				return -1
			}
			delete(ctx.Blackboard.ChainState, "review_verdict")
			if reviewer(ctx) < 0 {
				delete(ctx.Blackboard.ChainState, key)
				delete(ctx.Blackboard.ChainState, "review_verdict")
				delete(ctx.Blackboard.ChainState, "review_feedback")
				return -1
			}
			verdict, _ := ctx.Blackboard.ChainState["review_verdict"].(string)
			if verdict == "approved" {
				delete(ctx.Blackboard.ChainState, key)
				delete(ctx.Blackboard.ChainState, "review_feedback")
				return 1
			}
			// needs_work (or unparseable): loop; feedback stays for the child's next pass
		}
		delete(ctx.Blackboard.ChainState, key)
		delete(ctx.Blackboard.ChainState, "review_verdict")
		delete(ctx.Blackboard.ChainState, "review_feedback")
		ctx.Blackboard.Outcome = "review_cycle_exhausted: " + node.Name
		return -1
	})
}
