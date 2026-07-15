package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestSchedulerAttempt_HealthyNoCodeOutcomesAreDeferredTerminal reproduces the
// 2026-07-15 finding: no_change/degraded are healthy terminal states (RunOnce
// returns them with a nil error by design), but recordSchedulerAttempt only
// special-cased "success" and the rate-limit sentinel — healthy no-code
// outcomes fell into "anything else", were retried x3 (each retry a full
// Claude cycle: the 07:00 loop run burned 1h49m on two doomed retries) and
// dead-lettered on exhaustion. They must be terminal and SLO-deferred.
func TestSchedulerAttempt_HealthyNoCodeOutcomesAreDeferredTerminal(t *testing.T) {
	for _, outcome := range []string{"no_change", "degraded"} {
		t.Run(outcome, func(t *testing.T) {
			slo := &engine.SLOMetrics{AgentName: "goap-fusion-runner", TreeName: "goap_fusion"}
			retry := recordSchedulerAttempt(slo, outcome, nil, "## GOAP Fusion Cycle Complete\n\nanalysis-only cycle", 1, time.Second)
			if retry != nil {
				t.Fatalf("%s is a healthy terminal outcome and must not be retried/dead-lettered; got retry error %v", outcome, retry)
			}
			if slo.DeferredCalls != 1 {
				t.Errorf("%s must be SLO-deferred (neither success nor failure); DeferredCalls=%d, want 1", outcome, slo.DeferredCalls)
			}
			if slo.FailedCalls != 0 || slo.SuccessfulCalls != 0 || slo.TotalCalls != 0 {
				t.Errorf("%s must leave success/failure/latency counters untouched; failed=%d success=%d total=%d", outcome, slo.FailedCalls, slo.SuccessfulCalls, slo.TotalCalls)
			}
		})
	}
}

// TestSchedulerJobStoreIsReadOnlyForSiblings pins the job-table wiper fix at
// the config level: the daemon (persistJobs=true) gets the durable
// FileJobStore; MCP/CLI sibling instances (persistJobs=false) get a read-only
// wrapper so their scheduler state can never clobber the daemon's table —
// sibling saves were the attributed 2026-07-15 wiper (fresh job IDs, reset
// run counters, and a live [] overwrite).
func TestSchedulerJobStoreIsReadOnlyForSiblings(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
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

	daemon := buildSchedulerConfig(cfg, reg, hist, "rev", true)
	if _, ok := daemon.JobStore.(*agent.FileJobStore); !ok {
		t.Fatalf("daemon SchedulerConfig.JobStore = %T, want *agent.FileJobStore (durable writes)", daemon.JobStore)
	}

	sibling := buildSchedulerConfig(cfg, reg, hist, "rev", false)
	if _, ok := sibling.JobStore.(*agent.ReadOnlyJobStore); !ok {
		t.Fatalf("sibling SchedulerConfig.JobStore = %T, want *agent.ReadOnlyJobStore (job-table wiper fix)", sibling.JobStore)
	}
}

// TestMainGatesSchedulerOnDaemonMode pins — source-level, like the DLQ replay
// and experience-bank wiring tests — that main() (a) passes noMCPMode() as the
// persistJobs flag, (b) only STARTS the scheduler cron loop in daemon mode,
// and (c) only auto-schedules agents in daemon mode. Sibling instances ran a
// full scheduler for months: firing phantom duplicate cycles, marking the
// daemon's in-flight jobs crashed, and wiping the shared job table.
func TestMainGatesSchedulerOnDaemonMode(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)

	if !strings.Contains(s, "buildSchedulerConfig(cfg, agentReg, agentHist, buildID.Revision, noMCPMode())") {
		t.Error("main.go must pass noMCPMode() as buildSchedulerConfig's persistJobs flag")
	}

	requireGated := func(marker string) {
		t.Helper()
		idx := strings.Index(s, marker)
		if idx < 0 {
			t.Errorf("main.go no longer contains %q — gating pin needs updating", marker)
			return
		}
		windowStart := idx - 900
		if windowStart < 0 {
			windowStart = 0
		}
		if !strings.Contains(s[windowStart:idx], "noMCPMode()") {
			t.Errorf("%q must sit inside a noMCPMode() gate (no noMCPMode() found in the preceding window) — sibling instances must not run the scheduler", marker)
		}
	}
	requireGated("globalSched.Start(")
	requireGated(`engine.Info("auto-scheduled agent"`)
}

// TestMainWiresSynchronousIdleAdoption pins the deploy-drift starvation fix
// wiring: the scheduler's OnCycleIdle hook must call agent.AdoptDriftOnIdle
// SYNCHRONOUSLY (in the scheduler loop, queue blocked) — the async Kick
// variant lost a milliseconds race with the tick loop starting the next
// queued job (observed live 2026-07-15 13:54/14:01: skip 4ms after the kick;
// zero adoptions all day despite the armed flags).
func TestMainWiresSynchronousIdleAdoption(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "OnCycleIdle") {
		t.Error("main.go must set SchedulerConfig.OnCycleIdle (idle-window drift adoption)")
	}
	if !strings.Contains(s, "AdoptDriftOnIdle") {
		t.Error("main.go must wire OnCycleIdle to agent.AdoptDriftOnIdle (synchronous, un-raceable adoption)")
	}
}
