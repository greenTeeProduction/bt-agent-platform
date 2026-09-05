package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildPersistentMemSequence is MemSequence with its cursor in ChainState
// ("memseq/<name>") instead of the library's pointer-keyed map, so a run
// resumed after process restart (HITL waits, crashes — ADR-003 persistence)
// continues at the first incomplete child instead of re-running everything.
func BuildPersistentMemSequence(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}
	key := "memseq/" + node.Name
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		start, _ := chainStateInt(ctx.Blackboard, key)
		for i := start; i < len(children); i++ {
			switch code := children[i].Run(ctx); {
			case code == 0:
				ctx.Blackboard.ChainState[key] = i
				return 0
			case code < 0:
				delete(ctx.Blackboard.ChainState, key) // failure restarts the phase sequence next run
				return -1
			default:
				ctx.Blackboard.ChainState[key] = i + 1
			}
		}
		delete(ctx.Blackboard.ChainState, key)
		return 1
	})
}
