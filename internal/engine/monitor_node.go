package engine

import (
	"sync/atomic"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// MonitorTerminalCount counts Monitor node child terminations (tests/metrics).
var MonitorTerminalCount atomic.Uint64

// BuildMonitor runs the child and records terminal status to metrics/logs.
func BuildMonitor(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	label := node.Name
	if label == "" {
		label = "monitor"
	}
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		code := child.Run(ctx)
		if code != 0 {
			MonitorTerminalCount.Add(1)
			if ctx.Blackboard != nil && ctx.Blackboard.ChainState != nil {
				ctx.Blackboard.ChainState["monitor_"+label+"_last_code"] = code
			}
		}
		return code
	})
}
