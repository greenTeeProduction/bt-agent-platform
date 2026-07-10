package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// requireBuildIdentityWiring asserts — at the source level, the same way
// TestDaemonPlumbsExperienceBankIntoMCPDeps audits main.go — that a
// long-running binary's main package installs and logs its build identity at
// startup: it must call dashboard.InstallBuildIdentity() (reads
// runtime/debug.ReadBuildInfo, publishes the bt_build_info{revision,dirty}
// gauge, returns the identity) and log all three identity fields
// (vcs_revision, vcs_time, vcs_dirty). Without this wiring the recurring
// stale-daemon-binary drift (three incidents to date) stays detectable only
// via DLQ-message heuristics instead of by comparing the running revision
// against repo HEAD.
func requireBuildIdentityWiring(t *testing.T, mainPath string) {
	t.Helper()
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read %s: %v", mainPath, err)
	}
	s := string(src)
	if !strings.Contains(s, "dashboard.InstallBuildIdentity()") {
		t.Errorf("%s must call dashboard.InstallBuildIdentity() at startup (read + publish build identity); no reference found", mainPath)
	}
	for _, key := range []string{`"vcs_revision"`, `"vcs_time"`, `"vcs_dirty"`} {
		if !strings.Contains(s, key) {
			t.Errorf("%s must log the build identity at startup with the %s field; no reference found", mainPath, key)
		}
	}
}

// TestDaemonLogsBuildIdentityAtStartup pins that THE DAEMON BINARY
// (cmd/bt-agent) embeds its build identity: startup wiring reads
// runtime/debug build info, logs revision/commit-time/dirty, and publishes
// the bt_build_info gauge so a running daemon's revision is comparable
// against repo HEAD.
func TestDaemonLogsBuildIdentityAtStartup(t *testing.T) {
	requireBuildIdentityWiring(t, "main.go")
}

// TestGardenerLogsBuildIdentityAtStartup pins the same build-identity wiring
// for the other long-running binary, cmd/bt-gardener (audited from here
// because the gardener package has no wiring test file of its own; the check
// is source-level, so cross-package distance does not matter).
func TestGardenerLogsBuildIdentityAtStartup(t *testing.T) {
	requireBuildIdentityWiring(t, "../bt-gardener/main.go")
}

// TestSchedulerAttempt_RateLimitCarryoverRecordedAsDeferred pins the honest
// SLO/history accounting for a graceful Claude rate-limit carryover.
//
// When a scheduled agent run comes back with the goap_fusion_rate_limited
// sentinel, RunOnce surfaces the sentinel outcome together with a non-nil error
// (any non-"success" outcome is wrapped as an error). The scheduler must treat
// that carryover as *terminal* — an expected, healthy pause, not a retryable
// failure — so it neither retries nor dead-letters it. But it must ALSO not
// fold the pause into RecordSuccess: doing so inflates the success-count and
// success-latency stats that the gardener's validation gate reads, making a
// rate-limit backoff masquerade as real, low-latency throughput.
//
// The correct disposition is a dedicated deferred outcome: record it via
// slo.RecordDeferred and leave the success/failure counters and success-latency
// totals untouched.
func TestSchedulerAttempt_RateLimitCarryoverRecordedAsDeferred(t *testing.T) {
	slo := &engine.SLOMetrics{AgentName: "goap-fusion-loop-runner", TreeName: "goap_fusion_loop"}

	// RunOnce shape for a rate-limit carryover: sentinel outcome + wrapped error.
	runErr := errors.New("agent outcome: goap_fusion_rate_limited: paused on Claude rate limit")
	retry := recordSchedulerAttempt(slo, "goap_fusion_rate_limited", runErr, "paused on Claude rate limit", 1, 1500*time.Millisecond)

	if retry != nil {
		t.Fatalf("rate-limit carryover must be terminal (no retry / no DLQ); got retry error %v", retry)
	}
	if slo.SuccessfulCalls != 0 {
		t.Errorf("carryover must NOT be recorded as success; SuccessfulCalls=%d, want 0", slo.SuccessfulCalls)
	}
	if slo.FailedCalls != 0 {
		t.Errorf("carryover is a graceful pause, not a failure; FailedCalls=%d, want 0", slo.FailedCalls)
	}
	if slo.DeferredCalls != 1 {
		t.Errorf("carryover must be recorded via RecordDeferred; DeferredCalls=%d, want 1", slo.DeferredCalls)
	}
	// The deferral must not touch the success-latency accounting the gardener
	// gate reads: TotalCalls/TotalLatencyMs stay 0 so avg latency and success
	// rate are unaffected by the pause.
	if slo.TotalCalls != 0 {
		t.Errorf("deferral must not increment TotalCalls (would skew success rate); TotalCalls=%d, want 0", slo.TotalCalls)
	}
	if slo.TotalLatencyMs != 0 || slo.AvgLatencyMs() != 0 {
		t.Errorf("deferral must not inflate success-latency; TotalLatencyMs=%d avg=%.0f, want 0/0", slo.TotalLatencyMs, slo.AvgLatencyMs())
	}
	if slo.SuccessRate() != 1.0 {
		t.Errorf("a healthy pause must not degrade success rate; SuccessRate=%.3f, want 1.000", slo.SuccessRate())
	}
}

// TestSchedulerAttempt_SuccessAndFailureUnchanged is the positive control: the
// extracted recording helper must preserve the pre-existing success/failure
// disposition so the deferred path is a genuine third case, not a blanket
// no-op that hides real outcomes.
func TestSchedulerAttempt_SuccessAndFailureUnchanged(t *testing.T) {
	// Real success: recorded as success, terminal (nil retry).
	ok := &engine.SLOMetrics{AgentName: "a", TreeName: "t"}
	if retry := recordSchedulerAttempt(ok, "success", nil, "done", 1, 200*time.Millisecond); retry != nil {
		t.Fatalf("success must be terminal; got retry error %v", retry)
	}
	if ok.SuccessfulCalls != 1 || ok.DeferredCalls != 0 {
		t.Errorf("success accounting drifted: SuccessfulCalls=%d DeferredCalls=%d", ok.SuccessfulCalls, ok.DeferredCalls)
	}

	// Real failure: recorded as failure, retryable (non-nil retry error).
	bad := &engine.SLOMetrics{AgentName: "a", TreeName: "t"}
	retry := recordSchedulerAttempt(bad, "failure", errors.New("boom"), "boom", 1, 200*time.Millisecond)
	if retry == nil {
		t.Fatalf("a genuine failure must stay retryable; got nil retry error")
	}
	if bad.FailedCalls != 1 || bad.DeferredCalls != 0 {
		t.Errorf("failure accounting drifted: FailedCalls=%d DeferredCalls=%d", bad.FailedCalls, bad.DeferredCalls)
	}
}

// TestDaemonWiresDLQReplayConsumer pins — source-level, the same way the
// build-identity wiring tests do — that the daemon installs the drop-safe
// replay executor and the background requeue scan (c8094002 ms2). Without
// them, dashboard and MCP requeues flag entries that nothing re-executes.
// The wiring must be daemon-gated (noMCPMode) so MCP-spawned sibling
// instances sharing the same DLQ file cannot double-replay entries.
func TestDaemonWiresDLQReplayConsumer(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	for _, needle := range []string{
		"dlq.SetReplayExecutor(",
		"dlq.RequeuedReady()",
		"dlqReplayScanInterval",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("main.go must wire the DLQ replay consumer: missing %q", needle)
		}
	}
}

// TestDLQCrossProcessConsumersReloadFirst pins — source-level, like
// TestDaemonWiresDLQReplayConsumer above — that every cross-process DLQ
// consume site reloads the queue from the shared file before reading or
// requeuing. Each process (daemon, dashboard, MCP siblings) holds its own
// in-memory DeadLetterQueue over one file, so a consume against a stale view
// misses every stamp a sibling wrote since this process last read the file:
// dashboard/MCP requeues are never replayed and stale listings misreport the
// queue. Three sites are pinned:
//
//  1. the daemon's replay-scan tick (main.go) must call dlq.Reload() inside
//     each tick, before dlq.RequeuedReady();
//  2. the bt_dlq_replay tool (tools.go) must call engine.TaskDLQ.Reload()
//     before engine.TaskDLQ.Requeue, or it requeues against — and merge-saves
//     from — a view that predates sibling writes;
//  3. the dashboard's handleDLQ list handler (../bt-dashboard/main.go, read
//     cross-package the same way requireBuildIdentityWiring audits
//     ../bt-gardener) must call dlq.Reload() before dlq.List(); today only
//     handleDLQReplay reloads, so the DLQ panel shows the dashboard's stale
//     boot-time view.
func TestDLQCrossProcessConsumersReloadFirst(t *testing.T) {
	// Site 1: daemon replay-scan tick in main.go.
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	scanIdx := strings.Index(s, "time.NewTicker(dlqReplayScanInterval)")
	if scanIdx < 0 {
		t.Fatal("main.go must create the DLQ replay-scan ticker (pinned by TestDaemonWiresDLQReplayConsumer)")
	}
	scan := s[scanIdx:]
	loopIdx := strings.Index(scan, "for range ticker.C")
	readyIdx := strings.Index(scan, "dlq.RequeuedReady()")
	if loopIdx < 0 || readyIdx < 0 {
		t.Fatal("main.go replay scan lost its tick loop or RequeuedReady call")
	}
	if reloadIdx := strings.Index(scan, "dlq.Reload()"); reloadIdx < loopIdx || reloadIdx > readyIdx {
		t.Error("main.go replay scan must call dlq.Reload() inside each tick before dlq.RequeuedReady() — a stale in-memory view never sees dashboard/MCP requeue stamps, so cross-process replay stays dead")
	}

	// Site 2: bt_dlq_replay tool in tools.go.
	toolSrc, err := os.ReadFile("tools.go")
	if err != nil {
		t.Fatalf("read tools.go: %v", err)
	}
	tool := string(toolSrc)
	regIdx := strings.Index(tool, `"bt_dlq_replay"`)
	if regIdx < 0 {
		t.Fatal("tools.go must register the bt_dlq_replay tool")
	}
	handler := tool[regIdx:]
	requeueIdx := strings.Index(handler, "engine.TaskDLQ.Requeue(")
	if requeueIdx < 0 {
		t.Fatal("bt_dlq_replay must requeue via engine.TaskDLQ.Requeue")
	}
	if reloadIdx := strings.Index(handler, "engine.TaskDLQ.Reload()"); reloadIdx < 0 || reloadIdx > requeueIdx {
		t.Error("bt_dlq_replay must call engine.TaskDLQ.Reload() before engine.TaskDLQ.Requeue — requeuing against a stale view misses sibling stamps and merge-saves stale state over them")
	}

	// Site 3: dashboard handleDLQ list handler.
	dashSrc, err := os.ReadFile("../bt-dashboard/main.go")
	if err != nil {
		t.Fatalf("read ../bt-dashboard/main.go: %v", err)
	}
	dash := string(dashSrc)
	start := strings.Index(dash, "func handleDLQ(")
	end := strings.Index(dash, "func handleDLQReplay(")
	if start < 0 || end < start {
		t.Fatal("dashboard main.go lost handleDLQ/handleDLQReplay")
	}
	list := dash[start:end]
	listIdx := strings.Index(list, "dlq.List()")
	if listIdx < 0 {
		t.Fatal("dashboard handleDLQ must list entries via dlq.List()")
	}
	if reloadIdx := strings.Index(list, "dlq.Reload()"); reloadIdx < 0 || reloadIdx > listIdx {
		t.Error("dashboard handleDLQ must call dlq.Reload() before dlq.List() — only handleDLQReplay reloads today, so the DLQ panel renders the dashboard's stale boot-time view of the shared file")
	}
}

// A2A ":8686 bind: address already in use" is EXPECTED sibling contention —
// every MCP/CLI-spawned bt-agent instance next to the daemon triggers it
// (CLAUDE.md documents it as warned-and-ignored), yet it was logged at ERROR
// ~38×/day. The serve-error path must classify the expected case as WARN and
// keep everything else at ERROR.
func TestA2AServeErrorDemotesPortContention(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	for _, needle := range []string{
		"logA2AServeError(",
		`"address already in use"`,
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("main.go must classify A2A serve errors (WARN for port contention): missing %q", needle)
		}
	}
}
