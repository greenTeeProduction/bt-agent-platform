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
	contract, err := parseResultContract(node)
	if err != nil {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { ctx.Blackboard.Result = err.Error(); return -1 })
	}
	if contract != nil {
		return qualityGateCommandWithVerifier(primary, recovery, resultContractVerifier(node, contract))
	}
	return qualityGateCommand(primary, recovery)
}

// Recovery is a continuation of the same attempt, and must satisfy the same
// output contract. Never restart primary work while recovery is still running.
func qualityGateCommand(primary, recovery btcore.Command[Blackboard]) btcore.Command[Blackboard] {
	return qualityGateCommandWithVerifier(primary, recovery, validateOutputQuality)
}

func qualityGateCommandWithVerifier(primary, recovery btcore.Command[Blackboard], verify func(*Blackboard) bool) btcore.Command[Blackboard] {
	recovering := false
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		if ctx.Blackboard.applyExecutionStop() {
			recovering = false
			return -1
		}
		if !recovering {
			code := primary.Run(ctx)
			if ctx.Blackboard.applyExecutionStop() {
				return -1
			}
			if code == 0 {
				return 0
			}
			if code == 1 && verify(ctx.Blackboard) {
				return 1
			}
			ctx.Blackboard.Outcome = "quality_gate_failed"
			if recovery == nil {
				return -1
			}
			recovering = true
		}
		code := recovery.Run(ctx)
		if ctx.Blackboard.applyExecutionStop() {
			recovering = false
			return -1
		}
		if code == 0 {
			return 0
		}
		recovering = false
		if code == 1 && verify(ctx.Blackboard) {
			if ctx.Blackboard.Outcome == "quality_gate_failed" {
				ctx.Blackboard.Outcome = "success"
			}
			return 1
		}
		ctx.Blackboard.Outcome = "quality_gate_failed"
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
