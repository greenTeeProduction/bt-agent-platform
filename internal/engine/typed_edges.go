package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcomp "github.com/rvitorper/go-bt/composite"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// buildCompositeChildren builds child commands with typed-edge guard and quality_gate semantics.
func buildCompositeChildren(node *evolution.SerializableNode, bb *Blackboard, parentName string) []btcore.Command[Blackboard] {
	if len(node.Edges) == 0 {
		children := make([]btcore.Command[Blackboard], len(node.Children))
		for i := range node.Children {
			children[i] = buildNode(&node.Children[i], bb, parentName)
		}
		return children
	}

	recoveryIdx := recoveryChildIndex(node.Edges)
	children := make([]btcore.Command[Blackboard], 0, len(node.Children))
	for i := range node.Children {
		if i == recoveryIdx {
			continue // recovery-only child; invoked from quality_gate wrapper
		}
		child := buildNode(&node.Children[i], bb, parentName)
		if cond := guardConditionForChild(node.Edges, i); cond != "" {
			if !evaluateGuardCondition(cond, bb) {
				child = btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 })
			}
		}
		if hasQualityGateForChild(node.Edges, i) {
			var recovery btcore.Command[Blackboard]
			if recoveryIdx >= 0 && recoveryIdx < len(node.Children) {
				recovery = buildNode(&node.Children[recoveryIdx], bb, parentName)
			}
			child = wrapChildQualityGate(child, recovery)
		}
		children = append(children, child)
	}
	return children
}

func guardConditionForChild(edges []evolution.TypedEdge, idx int) string {
	for _, e := range edges {
		if e.Type == evolution.EdgeGuard && e.ChildIndex == idx && e.Condition != "" {
			return e.Condition
		}
	}
	return ""
}

func hasQualityGateForChild(edges []evolution.TypedEdge, idx int) bool {
	for _, e := range edges {
		if e.Type == evolution.EdgeQualityGate && e.ChildIndex == idx {
			return true
		}
	}
	return false
}

func wrapChildQualityGate(primary, recovery btcore.Command[Blackboard]) btcore.Command[Blackboard] {
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

// buildSequenceWithEdges returns a Sequence command respecting typed edges on the node.
func buildSequenceWithEdges(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	return btcomp.NewSequence(buildCompositeChildren(node, bb, node.Name)...)
}

// buildSelectorWithEdges returns a Selector command respecting typed edges on the node.
func buildSelectorWithEdges(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	return btcomp.NewSelector(buildCompositeChildren(node, bb, node.Name)...)
}
