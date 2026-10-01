package engine

import (
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// boundedRetry retries failed work, stops on success, and preserves a running
// attempt across ticks. MaxRetries bounds total attempts, as configured by BTs.
// A typed stop (approval, budget or admission) must never trigger another call.
func boundedRetry(child btcore.Command[Blackboard], limit int) btcore.Command[Blackboard] {
	attempts := 0
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		for attempts < limit {
			if ctx.Blackboard.applyExecutionStop() {
				attempts = 0
				return -1
			}
			status := child.Run(ctx)
			if ctx.Blackboard.applyExecutionStop() {
				attempts = 0
				return -1
			}
			if status == 0 {
				return 0
			}
			if status > 0 {
				attempts = 0
				return 1
			}
			attempts++
		}
		attempts = 0
		return -1
	})
}
