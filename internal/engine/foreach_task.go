package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildForEachTask runs its single child template once per Superpowers plan
// task, in order, skipping tasks already marked done. The loop index persists
// in ChainState ("foreach/<name>/index") so interrupted runs resume at the
// first incomplete task. Fail-fast: a child FAILURE stops the loop with the
// cursor pointing at the failing task. Child RUNNING propagates as RUNNING.
func BuildForEachTask(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) != 1 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			ctx.Blackboard.Outcome = "ForEachTask requires exactly one child template"
			return -1
		})
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	key := "foreach/" + node.Name + "/index"
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		run, ok := getSuperpowersRun(ctx.Blackboard)
		if !ok || len(run.Tasks) == 0 {
			ctx.Blackboard.Outcome = "ForEachTask: no superpowers run/tasks on blackboard"
			return -1
		}
		i, _ := chainStateInt(ctx.Blackboard, key)
		for ; i < len(run.Tasks); i++ {
			if run.Tasks[i].Status == "done" {
				continue
			}
			ctx.Blackboard.ChainState[key] = i
			ctx.Blackboard.ChainState["superpowers_task_index"] = i
			switch code := child.Run(ctx); {
			case code == 0:
				return 0
			case code < 0:
				return -1 // cursor stays on failing task for resume
			}
			// child SUCCESS: re-read run (child may have mutated it), loop continues
			run, _ = getSuperpowersRun(ctx.Blackboard)
		}
		delete(ctx.Blackboard.ChainState, key)
		delete(ctx.Blackboard.ChainState, "superpowers_task_index")
		return 1
	})
}
