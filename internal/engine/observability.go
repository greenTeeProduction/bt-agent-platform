package engine

import (
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/metrics"
	"github.com/nico/go-bt-evolve/internal/tracing"
	btcore "github.com/rvitorper/go-bt/core"
)

// observedCommand wraps a BT command with tracing and metrics per node tick.
type observedCommand struct {
	nodeType   string
	nodeName   string
	parentName string
	blockID    string // set when node is expanded from a SubTreeRef block
	child      btcore.Command[Blackboard]
}

func (o *observedCommand) Run(ctx *btcore.BTContext[Blackboard]) int {
	parentCtx := ctx.Context
	if ctx.Blackboard != nil && ctx.Blackboard.TraceContext != nil {
		parentCtx = ctx.Blackboard.TraceContext
	}

	spanName := fmt.Sprintf("bt.node/%s:%s", o.nodeType, o.nodeName)
	ctxSpan, span := tracing.StartSpan(parentCtx, spanName)
	defer span.End()

	span.SetAttribute("node.type", o.nodeType)
	span.SetAttribute("node.name", o.nodeName)
	if o.parentName != "" {
		span.SetAttribute("node.parent", o.parentName)
	}
	if o.blockID != "" {
		span.SetAttribute("block.id", o.blockID)
	}

	prevCtx := ctx.Context
	ctx.Context = ctxSpan
	defer func() { ctx.Context = prevCtx }()

	start := time.Now()
	code := o.child.Run(ctx)
	durMs := time.Since(start).Milliseconds()

	status := tickStatusLabel(code)
	metrics.RecordNodeTick(o.nodeType, o.nodeName, o.parentName, o.blockID, status, durMs)

	span.SetAttribute("bt.status", status)
	span.SetAttribute("bt.tick_code", fmt.Sprintf("%d", code))
	span.SetAttribute("duration_ms", fmt.Sprintf("%d", durMs))

	if code < 0 && ctx.Blackboard != nil && ctx.Blackboard.ChainState != nil {
		if cat, ok := ctx.Blackboard.ChainState["last_error_category"].(string); ok && cat != "" {
			span.SetAttribute("error.category", cat)
		}
	}

	return code
}

func tickStatusLabel(code int) string {
	switch {
	case code > 0:
		return "success"
	case code < 0:
		return "failure"
	default:
		return "running"
	}
}

// observeNode wraps a built command with per-node tracing and metrics.
func observeNode(node *evolution.SerializableNode, parentName string, child btcore.Command[Blackboard]) btcore.Command[Blackboard] {
	if node == nil || child == nil {
		return child
	}
	name := node.Name
	if name == "" {
		name = node.Type
	}
	blockID := ""
	if node.Metadata != nil {
		if id, ok := node.Metadata["block_id"].(string); ok {
			blockID = id
		}
	}
	return &observedCommand{
		nodeType:   node.Type,
		nodeName:   name,
		parentName: parentName,
		blockID:    blockID,
		child:      child,
	}
}
