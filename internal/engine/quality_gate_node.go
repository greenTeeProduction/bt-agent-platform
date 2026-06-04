package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildQualityGate runs the primary child, validates output quality, then runs recovery on failure.
func BuildQualityGate(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return -1 })
	}
	primary := buildNode(&node.Children[0], bb, node.Name)
	var recovery btcore.Command[Blackboard]
	if len(node.Children) > 1 {
		recovery = buildNode(&node.Children[1], bb, node.Name)
	} else if idx := recoveryChildIndex(node.Edges); idx >= 0 && idx < len(node.Children) {
		recovery = buildNode(&node.Children[idx], bb, node.Name)
	}
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		code := primary.Run(ctx)
		if code == 0 {
			return 0
		}
		if validateOutputQuality(ctx.Blackboard) {
			return code
		}
		ctx.Blackboard.Outcome = "quality_gate_failed"
		if recovery != nil {
			return recovery.Run(ctx)
		}
		return -1
	})
}

func recoveryChildIndex(edges []evolution.TypedEdge) int {
	for _, e := range edges {
		if e.Type == evolution.EdgeRecovery && e.ChildIndex >= 0 {
			return e.ChildIndex
		}
	}
	return -1
}
