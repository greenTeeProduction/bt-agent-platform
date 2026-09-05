package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildParallel runs all children concurrently (sequential tick model).
// ReactiveParallel adds monitor/race modes; Parallel uses success_policy metadata:
//   - all (default): all children must succeed
//   - one: first success wins
func BuildParallel(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	mode := ParallelAll
	if node.Metadata != nil {
		switch node.Metadata["success_policy"] {
		case "one", "any":
			mode = ParallelAny
		}
	}
	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		return runReactiveParallel(children, mode, nil, nil, true, ctx)
	})
}
