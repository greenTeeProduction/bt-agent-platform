package engine

import (
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
	btdec "github.com/rvitorper/go-bt/decorators"
)

// BuildTimeout wraps the child with a tick-based timeout (TimeoutMs on node).
func BuildTimeout(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	d := time.Duration(node.TimeoutMs) * time.Millisecond
	if d <= 0 && node.Metadata != nil {
		if v, ok := node.Metadata["timeout_ms"].(float64); ok {
			d = time.Duration(v) * time.Millisecond
		}
	}
	if d <= 0 {
		d = 30 * time.Second
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	return btdec.NewTimeout(child, d)
}

// BuildInverter inverts child success/failure.
func BuildInverter(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	return btdec.NewInverter(buildNode(&node.Children[0], bb, node.Name))
}

// BuildSucceeder always succeeds (go-bt Optional).
func BuildSucceeder(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	}
	return btdec.NewOptional(buildNode(&node.Children[0], bb, node.Name))
}

// BuildRepeater repeats child up to max_retries.
func BuildRepeater(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	times := node.MaxRetries
	if times <= 0 {
		times = 1
	}
	return btdec.NewRepeat(buildNode(&node.Children[0], bb, node.Name), times)
}

// BuildRunner executes the child once (explicit leaf decorator).
func BuildRunner(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	return buildNode(&node.Children[0], bb, node.Name)
}

// BuildCircuitBreaker opens after consecutive failures (metadata: threshold, cooldown_ms).
func BuildCircuitBreaker(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	threshold := 3
	cooldown := 5 * time.Minute
	if node.Metadata != nil {
		if v, ok := node.Metadata["threshold"].(float64); ok && v > 0 {
			threshold = int(v)
		}
		if v, ok := node.Metadata["cooldown_ms"].(float64); ok && v > 0 {
			cooldown = time.Duration(v) * time.Millisecond
		}
	}
	key := "cb:" + node.Name
	child := buildNode(&node.Children[0], bb, node.Name)
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = make(map[string]any)
		}
		if openUntil, ok := bb.ChainState[key+"_open"].(time.Time); ok && time.Now().Before(openUntil) {
			bb.Outcome = "circuit_open"
			return 0
		}
		code := child.Run(ctx)
		failKey := key + "_fails"
		if code < 0 {
			fails := 1
			if v, ok := bb.ChainState[failKey].(int); ok {
				fails = v + 1
			}
			bb.ChainState[failKey] = fails
			if fails >= threshold {
				bb.ChainState[key+"_open"] = time.Now().Add(cooldown)
				delete(bb.ChainState, failKey)
			}
			return code
		}
		delete(bb.ChainState, failKey)
		delete(bb.ChainState, key+"_open")
		return code
	})
}
