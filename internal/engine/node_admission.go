package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func withNodeAdmission(node *evolution.SerializableNode, child btcore.Command[Blackboard], bb *Blackboard) btcore.Command[Blackboard] {
	if bb == nil || bb.NodeAdmission == nil {
		return child
	}
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.applyExecutionStop() {
			return -1
		}
		if b.NodeAdmission != nil {
			if err := b.NodeAdmission(node.Type, node.Name); err != nil {
				if !reliability.IsExecutionTerminalError(err) {
					err = &reliability.ExecutionStoppedError{Outcome: "admission_rejected", Err: err}
				}
				b.stopExecution(err.Error(), err)
				b.applyExecutionStop()
				return -1
			}
		}
		return child.Run(ctx)
	})
}
