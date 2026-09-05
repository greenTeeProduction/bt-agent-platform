package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildMemSelector is a Selector with memory: while a child is RUNNING (or
// earlier children have FAILED this pass), re-ticks resume at the remembered
// index instead of re-running failed children. Cursor lives in ChainState
// ("memsel/<name>") so it survives blackboard persistence. Reactivity is
// intentionally traded away — use plain Selector for guard-style fallbacks.
func BuildMemSelector(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	children := make([]btcore.Command[Blackboard], len(node.Children))
	for i := range node.Children {
		children[i] = buildNode(&node.Children[i], bb, node.Name)
	}
	key := "memsel/" + node.Name
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		start, _ := chainStateInt(ctx.Blackboard, key)
		if start < 0 {
			// ChainState is JSON-persisted and externally writable, so the
			// cursor is untrusted: a negative value would index children[-1]
			// and panic mid-tick. Restart the pass instead, matching the
			// range check the sibling bandit selector applies to its own
			// resume cursor. Cursors past the last child need no clamp — the
			// loop falls straight through to FAILURE.
			start = 0
		}
		for i := start; i < len(children); i++ {
			switch code := children[i].Run(ctx); {
			case code == 0:
				ctx.Blackboard.ChainState[key] = i
				return 0
			case code > 0:
				delete(ctx.Blackboard.ChainState, key)
				return 1
			default:
				ctx.Blackboard.ChainState[key] = i + 1 // don't re-tick failed child this pass
			}
		}
		delete(ctx.Blackboard.ChainState, key)
		return -1
	})
}
