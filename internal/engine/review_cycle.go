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
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		for iter := 0; iter < maxIter; iter++ {
			switch code := child.Run(ctx); {
			case code == 0:
				return 0
			case code < 0:
				return -1
			}
			delete(ctx.Blackboard.ChainState, "review_verdict")
			if reviewer(ctx) < 0 {
				return -1
			}
			verdict, _ := ctx.Blackboard.ChainState["review_verdict"].(string)
			if verdict == "approved" {
				delete(ctx.Blackboard.ChainState, "review_feedback")
				return 1
			}
			// needs_work (or unparseable): loop; feedback stays for the child's next pass
		}
		ctx.Blackboard.Outcome = "review_cycle_exhausted: " + node.Name
		return -1
	})
}
