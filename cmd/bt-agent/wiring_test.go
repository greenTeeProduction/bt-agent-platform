package main

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestDaemonResolvesWiredGoapFusionLoopTree pins that THE DAEMON BINARY —
// whatever its import graph looks like in the future — resolves the
// scheduled goap_fusion_loop tree with production wiring applied. Today the
// wiring arrives via internal/agentexec's init (linked through tools.go);
// if that import is ever dropped, this test fails instead of the scheduled
// loop silently running unwired again (no preflight, no circuit gate, empty
// CIRCUITPOLICY state-hash history → breaker always answers CONTINUE).
func TestDaemonResolvesWiredGoapFusionLoopTree(t *testing.T) {
	tree := resolveTree("domain:goap_fusion_loop")
	if tree == nil {
		t.Fatal("domain:goap_fusion_loop did not resolve")
	}
	if len(tree.Children) == 0 || tree.Children[0].Name != "GoapFusionPreflight" {
		t.Fatalf("daemon must resolve the WIRED goap_fusion_loop tree (preflight first); first child = %q", tree.Children[0].Name)
	}
}

// TestDaemonConfiguresAuctionDelegateHook pins that THE DAEMON BINARY installs
// the auctioneer production wiring: engine.AuctionDelegateFn must be non-nil by
// the time the binary's packages are linked. The hook arrives via the same
// init-side-effect seam as the goap_fusion_loop wiring above
// (internal/agentexec, linked through tools.go), so this test fails if the
// auction wiring is ever dropped or never installed — instead of the
// AuctionDelegate action silently reporting "auction delegate not configured
// (set engine.AuctionDelegateFn)" at runtime.
func TestDaemonConfiguresAuctionDelegateHook(t *testing.T) {
	if engine.AuctionDelegateFn == nil {
		t.Fatal("daemon must configure engine.AuctionDelegateFn at startup (auctioneer production wiring); hook is nil")
	}
}

// TestDaemonConfiguresGoalPlanBrainstorm pins that the daemon binary installs
// the LLM plan-expansion (brainstorming) seam via internal/agentexec, so
// substantial goals get decomposed into deeper multi-task plans instead of
// one bounded task per goal.
func TestDaemonConfiguresGoalPlanBrainstorm(t *testing.T) {
	if !engine.GoalPlanBrainstormWired() {
		t.Fatal("daemon must wire engine.WireGoalPlanBrainstorm() at startup; plan-expansion seam is nil")
	}
}
