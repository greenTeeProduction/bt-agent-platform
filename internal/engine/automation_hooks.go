// Automation autopilot hook (ADR-010 Phase 4). engine must not import the
// persona/goal layers, so the in-tree ConsiderTreeCompile action delegates
// through an injection-hook var wired from cmd/bt-agent — the same pattern
// as DelegateToTreeFn (delegate_hooks.go).
package engine

import (
	btcore "github.com/rvitorper/go-bt/core"
)

// ConsiderAutomationFn is invoked after a successful user-attributed run to
// decide whether a recurring habit should be compiled into an automation
// proposal. Wired at startup in cmd/bt-agent; nil means the autopilot is
// not available (library use, tests) and ConsiderTreeCompile is a no-op.
var ConsiderAutomationFn func(user string)

func init() {
	// ConsiderTreeCompile closes the in-run decision path: trees can embed it
	// after their execution body so a good run immediately feeds the
	// autopilot. It never fails the surrounding tree — automation proposals
	// are strictly best-effort side work.
	RegisterAction("ConsiderTreeCompile", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if ConsiderAutomationFn == nil || bb == nil || bb.ChainState == nil {
			return 1
		}
		user, _ := bb.ChainState["persona_user"].(string)
		if user == "" {
			return 1 // anonymous run: nothing to personalize
		}
		if bb.Outcome == "failure" {
			return 1 // only good runs count toward automation evidence
		}
		ConsiderAutomationFn(user)
		return 1
	})
}
