package engine

import (
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	// ── Telegram Clarify — quality gate conditions ──
	RegisterCondition("IsTelegram", func(b *Blackboard) bool {
		if platform, ok := b.ChainState["platform"]; ok {
			if p, ok := platform.(string); ok && p == "telegram" {
				return true
			}
		}
		return false
	})

	RegisterCondition("HasQuestion", func(b *Blackboard) bool {
		response := b.Result
		if response == "" {
			response = b.Task
		}
		markers := []string{"?", "should I", "should we",
			"which ", "what ", "how ", "why ", "when ", "where ",
			"do you want", "would you like", "choose ", "pick ", "select "}
		lower := strings.ToLower(response)
		for _, m := range markers {
			if strings.Contains(lower, m) {
				return true
			}
		}
		return false
	})

	RegisterCondition("IsClarifyUsed", func(b *Blackboard) bool {
		if used, ok := b.ChainState["clarify_used"]; ok {
			if v, ok := used.(bool); ok && v {
				return true
			}
		}
		return strings.Contains(strings.ToLower(b.Result), "clarify") ||
			strings.Contains(strings.ToLower(b.Result), "multiple choice")
	})

	// ── Telegram Clarify — quality gate actions ──
	RegisterAction("MarkClarifyOK", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.Outcome = "success"
		b.ChainState["telegram_clarify_ok"] = true
		return 1
	})

	RegisterAction("ReportClarifyViolation", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		b.ChainState["telegram_clarify_violation"] = true
		b.ChainState["telegram_clarify_fix"] = "Use clarify(question=..., choices=[...]) instead of plain text"
		b.Outcome = "violation"
		return 1
	})

	RegisterAction("SuggestFix", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if s, ok := b.ChainState["telegram_clarify_suggestion"]; ok {
			if str, ok := s.(string); ok {
				b.Result = str
				b.Outcome = "success"
				return 1
			}
		}
		b.Result = "Use clarify(question=\"...\", choices=[\"Option A\", \"Option B\"])"
		b.Outcome = "success"
		return 1
	})
}
