package main

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
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

// TestDaemonWiresFeedbackPersistencePath pins that THE DAEMON BINARY resolves the
// same on-disk feedback-snapshot path the scheduler persists knowledge-graph
// feedback to. The daemon must expose feedbackSnapshotPath() and set it as
// SchedulerConfig.FeedbackPath so Fitness/RunCount/tool-edges rehydrate on
// startup instead of resetting every restart; if the helper diverges from
// agent.FeedbackFile() (the path the scheduler loads/persists), the learn→
// discover→evolve loop silently resets across restarts again.
func TestDaemonWiresFeedbackPersistencePath(t *testing.T) {
	got := feedbackSnapshotPath()
	if got == "" {
		t.Fatal("daemon must resolve a non-empty feedback-snapshot path (feedbackSnapshotPath())")
	}
	if want := agent.FeedbackFile(); got != want {
		t.Fatalf("daemon feedback-snapshot path must equal agent.FeedbackFile(); got %q, want %q", got, want)
	}
}

// TestDaemonSchedulerConfigWiresFeedbackPath pins the SchedulerConfig the daemon
// actually hands to agent.NewScheduler — not just the feedbackSnapshotPath()
// helper. The previous test (TestDaemonWiresFeedbackPersistencePath) only checks
// that the helper equals agent.FeedbackFile(); it stays green even if someone
// deletes the `FeedbackPath: feedbackSnapshotPath()` line from the scheduler
// config, silently disabling persistence. This test closes that gap by asserting
// the assembled config carries FeedbackPath (rehydration), the durable FileJobStore,
// the circuit-breaker store, and the passed-in Registry/History end-to-end.
func TestDaemonSchedulerConfigWiresFeedbackPath(t *testing.T) {
	cfg, _ := config.Load()
	if cfg == nil {
		t.Fatal("config.Load returned nil config")
	}
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	hist, err := agent.NewHistory(t.TempDir())
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}

	scfg := buildSchedulerConfig(cfg, reg, hist)

	if want := agent.FeedbackFile(); scfg.FeedbackPath != want {
		t.Fatalf("SchedulerConfig.FeedbackPath = %q, want %q (agent.FeedbackFile()); feedback persistence disabled", scfg.FeedbackPath, want)
	}
	if scfg.FeedbackPath != feedbackSnapshotPath() {
		t.Fatalf("SchedulerConfig.FeedbackPath = %q, want feedbackSnapshotPath() %q", scfg.FeedbackPath, feedbackSnapshotPath())
	}
	if scfg.Registry != reg {
		t.Fatal("SchedulerConfig.Registry not wired from argument")
	}
	if scfg.History != hist {
		t.Fatal("SchedulerConfig.History not wired from argument")
	}
	if scfg.JobStore == nil {
		t.Fatal("SchedulerConfig.JobStore must be set (durable FileJobStore)")
	}
	if scfg.CBStore == nil {
		t.Fatal("SchedulerConfig.CBStore must be set (per-agent circuit breakers)")
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
