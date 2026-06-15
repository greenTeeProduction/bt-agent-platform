package engine

import (
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

func shouldUseFusionCond(b *Blackboard) bool {
	if b == nil {
		return false
	}
	if forceFusionCond(b) {
		return true
	}
	if strings.EqualFold(b.Complexity, "high") {
		return true
	}
	task := strings.ToLower(strings.TrimSpace(b.Task))
	if len(task) > 350 {
		return true
	}
	if len(task) < 40 {
		return false
	}
	markers := []string{
		"research", "survey", "compare", "contrast", "strongest arguments",
		"where do experts disagree", "critique", "risk", "high-stakes", "accuracy",
		"multiple perspectives", "tradeoff", "trade-off", "pros and cons", "for and against",
	}
	for _, marker := range markers {
		if strings.Contains(task, marker) {
			return true
		}
	}
	return false
}

func forceFusionCond(b *Blackboard) bool {
	if b == nil || b.ChainState == nil {
		return false
	}
	v, ok := b.ChainState["fusion_force"]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true") || x == "1" || strings.EqualFold(x, "yes")
	default:
		return false
	}
}

func markFusionSkippedAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["fusion_skipped"] = true
	return 1
}
