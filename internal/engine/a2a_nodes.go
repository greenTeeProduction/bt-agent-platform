package engine

import (
	"fmt"

	btcore "github.com/rvitorper/go-bt/core"
)

// DelegateToA2AFn is injected by the a2a package at startup.
// When set, the DelegateToA2A action node uses this to send tasks to external A2A agents.
var DelegateToA2AFn func(targetURL, task string) (string, error)

// registerA2ANodes registers A2A delegation as behavior tree actions and conditions.
func registerA2ANodes() {
	// DelegateToA2A — sends the current task to an external A2A agent.
	// Requires a2a_target_url in chain state.
	actionRegistry["DelegateToA2A"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cs := b.ChainState

		targetURL, _ := cs["a2a_target_url"].(string)
		if targetURL == "" {
			b.Result = "a2a_target_url not set in chain state"
			b.Outcome = "failure"
			return -1
		}

		task := b.Task
		if task == "" {
			if t, ok := cs["a2a_task"].(string); ok {
				task = t
			}
		}
		if task == "" {
			b.Result = "no task provided for A2A delegation"
			b.Outcome = "failure"
			return -1
		}

		if DelegateToA2AFn == nil {
			b.Result = "A2A client not configured (set engine.DelegateToA2AFn)"
			b.Outcome = "failure"
			return -1
		}

		result, err := DelegateToA2AFn(targetURL, task)
		if err != nil {
			b.Result = fmt.Sprintf("A2A delegation failed: %v", err)
			b.Outcome = "failure"
			return -1
		}

		b.Result = result
		b.Outcome = "success"
		return 1
	}

	// HasA2ATarget — condition: true if an A2A target URL is configured.
	conditionRegistry["HasA2ATarget"] = func(b *Blackboard) bool {
		cs := b.ChainState
		if target, ok := cs["a2a_target_url"].(string); ok && target != "" {
			return true
		}
		return false
	}

	// SetA2ATarget — action: sets the A2A target URL from chain state for GOAP.
	actionRegistry["SetA2ATarget"] = func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		cs := b.ChainState
		target, ok := cs["a2a_target_url"].(string)
		if !ok || target == "" {
			b.Result = "no A2A target URL in chain state"
			b.Outcome = "failure"
			return -1
		}
		if ws, ok := cs["goap_world_state"].(map[string]interface{}); ok {
			ws["has_a2a_target"] = true
		}
		return 1
	}
}
