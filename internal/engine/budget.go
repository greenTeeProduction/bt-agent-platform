package engine

import (
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// BuildBudget wraps a child and enforces max_ticks / max_tokens from metadata.
func BuildBudget(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	if len(node.Children) == 0 {
		return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int { return -1 })
	}
	maxTicks := 0
	maxTokens := 0
	if node.Metadata != nil {
		if v, ok := node.Metadata["max_ticks"].(float64); ok {
			maxTicks = int(v)
		}
		if v, ok := node.Metadata["max_ticks"].(int); ok {
			maxTicks = v
		}
		if v, ok := node.Metadata["max_tokens"].(float64); ok {
			maxTokens = int(v)
		}
		if v, ok := node.Metadata["max_tokens"].(int); ok {
			maxTokens = v
		}
	}
	if maxTicks <= 0 && node.MaxRetries > 0 {
		maxTicks = node.MaxRetries
	}
	child := buildNode(&node.Children[0], bb, node.Name)
	return &budgetCmd{child: child, maxTicks: maxTicks, maxTokens: maxTokens}
}

type budgetCmd struct {
	child      btcore.Command[Blackboard]
	maxTicks   int
	maxTokens  int
}

func (b *budgetCmd) Run(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb == nil {
		return -1
	}
	if b.maxTicks > 0 && bb.TreeTicks >= b.maxTicks {
		bb.Outcome = "budget_exhausted_ticks"
		return -1
	}
	if b.maxTokens > 0 && bb.TokensUsed >= b.maxTokens {
		bb.Outcome = "budget_exhausted_tokens"
		return -1
	}
	bb.TreeTicks++
	code := b.child.Run(ctx)
	if b.maxTicks > 0 && bb.TreeTicks > b.maxTicks {
		bb.Outcome = "budget_exhausted_ticks"
		return -1
	}
	if b.maxTokens > 0 && bb.TokensUsed > b.maxTokens {
		bb.Outcome = "budget_exhausted_tokens"
		return -1
	}
	return code
}
