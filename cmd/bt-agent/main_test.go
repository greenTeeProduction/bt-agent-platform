package main

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
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

// TestDLQReplayOutcomeError_HealthyNonSuccessNotFailing pins the NotebookLM
// research finding that the DLQ replay executor's outcome classification
// (main.go dlq.SetReplayExecutor, ~line 592-609) must treat the rate-limit
// carryover and other healthy non-success outcomes (no_change, degraded) as
// non-failing replays — mirroring recordSchedulerAttempt/IsBreakerSuccess
// above, which already give those same outcomes the same terminal-and-healthy
// treatment on the scheduler path. Before this fix the replay executor's
// inline check (res.Outcome != "success") flagged EVERY non-"success"
// outcome as a failure, so a replayed dead letter that gracefully paused on a
// Claude rate limit (or landed on an analysis-only/deterministic-fallback
// outcome) was kept in the DLQ and endlessly re-replayed instead of being
// dropped as a healthy terminal result — drop-safe Replay only removes an
// entry on a nil error (reliability.go:249-251).
func TestDLQReplayOutcomeError_HealthyNonSuccessNotFailing(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
	}{
		{"success", "success"},
		{"rate-limit carryover", agent.RateLimitCarryoverOutcome},
		{"no_change", "no_change"},
		{"degraded", "degraded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := &agent.RunResult{AgentName: "a", Task: "t", Outcome: c.outcome, Output: "ok"}
			if err := dlqReplayOutcomeError(res); err != nil {
				t.Errorf("dlqReplayOutcomeError(outcome=%q) = %v, want nil — a healthy non-success replay must not be treated as a failing one", c.outcome, err)
			}
		})
	}

	// Positive control: a genuine failure must still classify as an error so
	// the fix doesn't degrade into a blanket no-op — a truly failed replay
	// must keep its DLQ entry.
	bad := &agent.RunResult{AgentName: "a", Task: "t", Outcome: "failure", Output: "boom"}
	if err := dlqReplayOutcomeError(bad); err == nil {
		t.Fatal(`dlqReplayOutcomeError(outcome="failure") = nil, want non-nil error — a genuine failure must still keep its DLQ entry`)
	}

	// No result at all (nil) must not be treated as a failure either.
	if err := dlqReplayOutcomeError(nil); err != nil {
		t.Errorf("dlqReplayOutcomeError(nil) = %v, want nil", err)
	}
}

// TestDLQReplayExecutorUsesOutcomeClassifier pins — source-level, the same
// audit style as TestSchedulerAndDLQReplayDispatchThroughAgentRouter below —
// that the DLQ replay executor's closure in main.go actually delegates to
// dlqReplayOutcomeError instead of re-inlining the "!= \"success\"" check
// TestDLQReplayOutcomeError_HealthyNonSuccessNotFailing pins above. Without
// this the extracted classifier could be added and tested while the closure
// itself keeps using the old blanket check.
func TestDLQReplayExecutorUsesOutcomeClassifier(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	replayIdx := strings.Index(s, "dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {")
	if replayIdx < 0 {
		t.Fatal("main.go lost the DLQ replay executor")
	}
	tickerIdx := strings.Index(s, `reliability.SafeGo("dlq-replay-scan-ticker"`)
	if tickerIdx < 0 {
		t.Fatal("main.go lost the dlq-replay-scan-ticker wiring")
	}
	body := s[replayIdx:tickerIdx]
	if !strings.Contains(body, "dlqReplayOutcomeError(") {
		t.Error("the DLQ replay executor must classify outcomes via dlqReplayOutcomeError(res) instead of an inline `res.Outcome != \"success\"` check, so rate-limit carryover and other healthy non-success outcomes (mirroring recordSchedulerAttempt/IsBreakerSuccess) are not treated as failing replays")
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
// queue. Four sites are pinned:
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
//  4. the bt_dlq_list tool (tools.go) must call engine.TaskDLQ.Reload()
//     before engine.TaskDLQ.List() — today only bt_dlq_replay reloads, so
//     this MCP-facing listing renders a stale view of the shared file.
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

	// Site 4: bt_dlq_list tool in tools.go.
	listRegIdx := strings.Index(tool, `"bt_dlq_list"`)
	if listRegIdx < 0 {
		t.Fatal("tools.go must register the bt_dlq_list tool")
	}
	listReplayIdx := strings.Index(tool, `"bt_dlq_replay"`)
	if listReplayIdx < 0 || listReplayIdx < listRegIdx {
		t.Fatal("tools.go must register bt_dlq_list before bt_dlq_replay")
	}
	listHandler := tool[listRegIdx:listReplayIdx]
	listCallIdx := strings.Index(listHandler, "engine.TaskDLQ.List()")
	if listCallIdx < 0 {
		t.Fatal("bt_dlq_list must list entries via engine.TaskDLQ.List()")
	}
	if reloadIdx := strings.Index(listHandler, "engine.TaskDLQ.Reload()"); reloadIdx < 0 || reloadIdx > listCallIdx {
		t.Error("bt_dlq_list must call engine.TaskDLQ.Reload() before engine.TaskDLQ.List() — today only bt_dlq_replay reloads, so this MCP-facing listing renders a stale view of the shared file")
	}
}

// TestDLQReplayScanSurvivesPanicAcrossTicks pins milestone 5/5 of the Q3
// Reliability program: the DLQ replay-scan ticker loop in main.go must
// survive a panicking dlq.Replay call so a single poison-pill entry can't
// silently end the scan goroutine and stop every future tick — mirroring the
// per-tick reliability.Recover pattern internal/llm/health.go's ticker loop
// already uses (health.go:202-217), plus a reliability.SafeGo wrapper on the
// goroutine itself for the same reason recordSchedulerAttempt above was
// pulled out of its closure: main() starts the full daemon and can't run in
// a unit test, so the per-tick scan body must be its own testable function.
// runDLQReplayScanOnce is that function — called by the wrapped ticker loop
// on every tick, and driven directly here to simulate two ticks.
func TestDLQReplayScanSurvivesPanicAcrossTicks(t *testing.T) {
	dlq := reliability.NewDeadLetterQueue("")
	dlq.Push(reliability.DeadLetterEntry{ID: "flaky", Task: "t", Agent: "a"})
	dlq.Push(reliability.DeadLetterEntry{ID: "healthy", Task: "t", Agent: "a"})
	if _, ok := dlq.Requeue("flaky"); !ok {
		t.Fatal("setup: requeue must succeed")
	}
	if _, ok := dlq.Requeue("healthy"); !ok {
		t.Fatal("setup: requeue must succeed")
	}

	dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {
		if e.ID == "flaky" {
			panic("simulated replay panic")
		}
		return nil
	})

	runTick := func(tick int) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("tick %d: runDLQReplayScanOnce must recover its own panic instead of letting "+
					"it escape (an escaped panic would kill the ticker goroutine and stop every future "+
					"scan tick), got: %v", tick, r)
			}
		}()
		runDLQReplayScanOnce(dlq)
	}

	// Tick 1: "flaky" panics mid-scan. The tick must absorb that panic rather
	// than letting it propagate.
	runTick(1)
	// Tick 2 only runs at all if the scan loop survived tick 1's panic —
	// exactly the property this test pins. "healthy" was never reached
	// during tick 1 (it panicked on "flaky" first) so it is still requeued;
	// tick 2 must now replay it successfully.
	runTick(2)

	for _, e := range dlq.List() {
		if e.ID == "healthy" {
			t.Fatal("tick 2 must have replayed and removed \"healthy\" — the scan loop did not survive tick 1's panic")
		}
	}
}

// TestDaemonDriftWatcherWiresBackoffAndInFlightGuard pins — source-level, the
// same audit style as requireBuildIdentityWiring above — that the daemon's
// deploy-drift watcher actually consults the two guardrails ADR-045 shipped
// but left unwired (arc42 §Deploy Drift, 2026-07-12): agent.RebuildBackoff
// (throttles retry-storming a broken HEAD) and Scheduler.AnyInFlight()
// (never swap the live binary out from under a mid-execution job). Without
// this wiring both guardrails are unit-tested but dead from the running
// daemon's perspective.
func TestDaemonDriftWatcherWiresBackoffAndInFlightGuard(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	for _, needle := range []string{
		"Backoff:",
		"InFlightFn:",
		"globalSched.AnyInFlight",
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("main.go's deploy-drift watcher must wire %q; not found", needle)
		}
	}
}

// TestGardenerDriftWatcherWiresRebuildBackoff pins the same RebuildBackoff
// wiring for cmd/bt-gardener (audited from here, cross-package, the same way
// TestGardenerLogsBuildIdentityAtStartup does — the gardener package has no
// wiring test file of its own).
func TestGardenerDriftWatcherWiresRebuildBackoff(t *testing.T) {
	src, err := os.ReadFile("../bt-gardener/main.go")
	if err != nil {
		t.Fatalf("read ../bt-gardener/main.go: %v", err)
	}
	if !strings.Contains(string(src), "Backoff:") {
		t.Error("../bt-gardener/main.go's deploy-drift watcher must wire a RebuildBackoff (Backoff:); not found")
	}
}

// TestDaemonWrapsSchedulerAndA2AStartWithSafeGo pins milestone 3/4 of the Q3
// Reliability program: the scheduler-start goroutine (globalSched.Start) and
// the A2A server-start goroutine (a2aSrv.Start) must run under
// reliability.SafeGo, matching the pattern already used in this same file for
// the KG index build ("kg-build-index") and the DLQ replay-scan ticker
// ("dlq-replay-scan-ticker"). Today both are bare `go` statements: an
// unrecovered panic in the scheduler callback or the A2A serve loop would
// escape the goroutine and crash the whole daemon, taking every other
// in-process subsystem (MCP server, drift watcher, DLQ consumer) down with
// it.
func TestDaemonWrapsSchedulerAndA2AStartWithSafeGo(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)

	startIdx := strings.Index(s, "globalSched.Start(")
	if startIdx < 0 {
		t.Fatal("main.go lost the globalSched.Start( callback registration")
	}
	if strings.Contains(s[:startIdx], "\n\tgo globalSched.Start(") {
		t.Error("scheduler-start goroutine must be wrapped by reliability.SafeGo, not a bare `go` statement — an unrecovered panic in the scheduler callback would crash the whole daemon")
	}
	if safeGoIdx := strings.LastIndex(s[:startIdx], `reliability.SafeGo("scheduler-start"`); safeGoIdx < 0 {
		t.Error(`main.go must invoke globalSched.Start via reliability.SafeGo("scheduler-start", func() { ... }, nil), matching the kg-build-index / dlq-replay-scan-ticker pattern in this file`)
	}

	a2aIdx := strings.Index(s, "a2aSrv.Start()")
	if a2aIdx < 0 {
		t.Fatal("main.go lost the a2aSrv.Start() call")
	}
	if strings.Contains(s[:a2aIdx], "go func() {\n\t\t\tif err := a2aSrv.Start()") {
		t.Error("a2a server-start goroutine must be wrapped by reliability.SafeGo, not a bare `go func(){...}()` — an unrecovered panic there would crash the daemon")
	}
	if safeGoIdx := strings.LastIndex(s[:a2aIdx], `reliability.SafeGo("a2a-server-start"`); safeGoIdx < 0 {
		t.Error(`main.go must invoke a2aSrv.Start via reliability.SafeGo("a2a-server-start", func() { ... }, nil), matching the kg-build-index / dlq-replay-scan-ticker pattern in this file`)
	}
}

// TestSchedulerAndDLQReplayDispatchThroughAgentRouter pins the NotebookLM
// research finding that the AgentRouter built from the live A2A card
// registry ("Horizontal-scaling substrate", main.go) is a dead seam: it is
// constructed and logged ("agent router constructed from A2A card
// registry"), then dropped. Neither the scheduler's AgentRunner closure
// (globalSched.Start) nor the DLQ replay executor (dlq.SetReplayExecutor)
// ever reference it — both call agentRunner.RunOnce directly — so scheduled
// and DLQ-replay-driven agent runs can never reach a remote peer even once
// one joins the registry. Both dispatch paths must route through
// agentRouter.Execute so horizontal scaling is actually reachable in
// production, not just from a single-node registry that always resolves to
// the local executor.
func TestSchedulerAndDLQReplayDispatchThroughAgentRouter(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)

	schedStartIdx := strings.Index(s, "globalSched.Start(func(ctx agent.RunContext)")
	if schedStartIdx < 0 {
		t.Fatal("main.go lost the globalSched.Start scheduler callback")
	}
	replayIdx := strings.Index(s, "dlq.SetReplayExecutor(func(e reliability.DeadLetterEntry) error {")
	if replayIdx < 0 {
		t.Fatal("main.go lost the DLQ replay executor")
	}
	tickerIdx := strings.Index(s, `reliability.SafeGo("dlq-replay-scan-ticker"`)
	if tickerIdx < 0 {
		t.Fatal("main.go lost the dlq-replay-scan-ticker wiring")
	}
	if !strings.Contains(s, "reliability.NewRouterFromEndpoints(") {
		t.Fatal("main.go lost the agentRouter construction from the A2A card registry")
	}

	schedBody := s[schedStartIdx:replayIdx]
	if !strings.Contains(schedBody, "agentRouter.Execute(") {
		t.Error("the scheduler's AgentRunner closure (globalSched.Start) must dispatch scheduled runs via agentRouter.Execute(...) instead of always calling agentRunner.RunOnce directly, so scheduled runs can reach remote peers")
	}

	replayBody := s[replayIdx:tickerIdx]
	if !strings.Contains(replayBody, "agentRouter.Execute(") {
		t.Error("the DLQ replay executor (dlq.SetReplayExecutor) must dispatch replays via agentRouter.Execute(...) instead of always calling agentRunner.RunOnce directly, so replayed dead letters can reach remote peers")
	}
}

// A2A ":8686 bind: address already in use" is EXPECTED sibling contention —
// every MCP/CLI-spawned bt-agent instance next to the daemon triggers it
// (CLAUDE.md documents it as warned-and-ignored), yet it was logged at ERROR
// ~38×/day. The serve-error path must classify the expected case as WARN and
// keep everything else at ERROR.
// TestValidateDomainRegistry_FlagsInvalidTree is milestone 3/3 of "Wire the
// real GOAP A* planner into production domain trees instead of the orphaned
// keyword router": validateDomainRegistry must run engine.ValidateTree over
// every tree in a domain registry and surface any validation message,
// prefixed with the offending domain name so a startup failure names the
// broken tree instead of just "something is wrong".
func TestValidateDomainRegistry_FlagsInvalidTree(t *testing.T) {
	broken := map[string]*evolution.SerializableNode{
		"bogus_domain": {
			Type: "Action",
			Name: "TotallyUnregisteredAction",
		},
	}
	msgs := validateDomainRegistry(broken)
	if len(msgs) != 1 {
		t.Fatalf("validateDomainRegistry(broken) = %v, want 1 message", msgs)
	}
	if !strings.Contains(msgs[0], "bogus_domain") || !strings.Contains(msgs[0], "TotallyUnregisteredAction") {
		t.Errorf("expected message to name both the domain and the bad node, got %q", msgs[0])
	}
}

// TestValidateDomainRegistry_RealTreesClean pins that the production domain
// registry domains.AllDomainTrees() — including the newly-composed
// goap_planning/goap_research/goap_devops trees that nest the real GOAP A*
// planner subtree beside the pre-existing keyword-routed paths — passes
// validateDomainRegistry with zero messages, so the startup gate this
// milestone adds to main() never trips on a tree that is actually fine.
func TestValidateDomainRegistry_RealTreesClean(t *testing.T) {
	if msgs := validateDomainRegistry(domains.AllDomainTrees()); len(msgs) != 0 {
		t.Errorf("validateDomainRegistry(real registry) = %v, want no messages", msgs)
	}
}

// TestDaemonValidatesDomainRegistryAtStartup pins — source-level, the same
// audit style as TestDaemonWiresDLQReplayConsumer above — that main()'s
// startup registry loop (which already ranges over domains.AllDomainTrees()
// to build kg.ExpectedDomains) also runs validateDomainRegistry over the same
// registry and exits fatally if it reports any messages. Without this wiring
// a future authoring mistake in a domain tree (duplicate node name, unguarded
// HITL condition under a CachedCondition, unknown chain_type, ...) would be
// silently served instead of failing the daemon's startup loudly.
func TestDaemonValidatesDomainRegistryAtStartup(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)
	loopIdx := strings.Index(s, "domains.AllDomainTrees()")
	if loopIdx < 0 {
		t.Fatal("main.go lost the domains.AllDomainTrees() startup registry loop")
	}
	kgAssignIdx := strings.Index(s, "kg.ExpectedDomains = expectedDomains")
	if kgAssignIdx < 0 || kgAssignIdx < loopIdx {
		t.Fatal("main.go lost the kg.ExpectedDomains assignment after the registry loop")
	}
	section := s[loopIdx:kgAssignIdx]
	if !strings.Contains(section, "validateDomainRegistry(") {
		t.Error("main.go's startup registry loop must call validateDomainRegistry(...) over domains.AllDomainTrees() before assigning kg.ExpectedDomains, so an invalid tree fails startup instead of being silently served")
	}
	if !strings.Contains(section, "os.Exit(1)") {
		t.Error("main.go must exit fatally (os.Exit(1)) when validateDomainRegistry reports messages, matching the fatal-startup-error pattern used elsewhere in this file")
	}
}

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

// TestRoutedRunResult_FabricatedOutcomeUsesCanonicalFailure pins the outcome
// vocabulary at the router boundary: when a peer's AgentResult omits Outcome,
// the fabricated fallback must use the scheduler's canonical "failure" token,
// not the one-off "failed" spelling.
func TestRoutedRunResult_FabricatedOutcomeUsesCanonicalFailure(t *testing.T) {
	res := routedRunResult("a", "t", &reliability.AgentResult{Success: false})
	if res.Outcome != "failure" {
		t.Fatalf("fabricated outcome = %q, want canonical %q", res.Outcome, "failure")
	}
	ok := routedRunResult("a", "t", &reliability.AgentResult{Success: true})
	if ok.Outcome != "success" {
		t.Fatalf("fabricated success outcome = %q, want %q", ok.Outcome, "success")
	}
	preserved := routedRunResult("a", "t", &reliability.AgentResult{Outcome: agent.RateLimitCarryoverOutcome})
	if preserved.Outcome != agent.RateLimitCarryoverOutcome {
		t.Fatalf("outcome = %q, want the peer's raw outcome preserved", preserved.Outcome)
	}
}

// TestDriftConfigs_FleetOwnerSetsRestartSiblings audits — at the source level,
// like the build-identity wiring tests above — that BOTH of bt-agent's
// DriftWatchConfig constructions (the periodic StartDriftWatcher and the
// synchronous OnCycleIdle idleDriftCfg, the path that reliably fires on busy
// fleets) opt into RestartSiblings. bt-agent is the fleet owner: if either
// path omits it, siblings get rebuilt on disk but keep running their old
// binaries — the 2026-07-16 23:46 live case the sibling-restart loop exists
// for — because after bt-agent's own restart the drift clears and the other
// path never fires.
func TestDriftConfigs_FleetOwnerSetsRestartSiblings(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if got := strings.Count(string(src), "RestartSiblings: true"); got < 2 {
		t.Fatalf("found %d 'RestartSiblings: true' in cmd/bt-agent/main.go, want >= 2 (both the periodic watcher config and idleDriftCfg must opt in as fleet owner)", got)
	}
}

// TestShutdownStopsA2AServer pins a NotebookLM research finding: neither of
// main.go's two shutdown paths — the "--no-mcp" early-return branch, or the
// daemon-mode fallback reached after the MCP server exits (e.g. stdin
// closed) — ever calls a2aSrv.Stop() after receiving SIGINT/SIGTERM. Both
// blocks already wait on <-sigCh and log "shutdown signal received", but the
// A2A HTTP listener (internal/a2a.Server, whose Stop() gracefully closes the
// http.Server — see maturity_test.go) is left to be killed by process exit
// instead of shutting down gracefully, unlike tracingShutdown/logShutdown
// which already run via defer. Audited at the source level, like the other
// shutdown/wiring tests in this file, because main() itself is not
// unit-testable (it blocks on a signal channel and calls os.Exit).
func TestShutdownStopsA2AServer(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	s := string(src)

	noMCPIdx := strings.Index(s, `engine.Info("MCP server disabled (--no-mcp), A2A + scheduler running")`)
	if noMCPIdx < 0 {
		t.Fatal("main.go lost the --no-mcp shutdown branch")
	}
	daemonSigIdx := strings.Index(s, `engine.Info("bt-agent running in daemon mode (--no-mcp), scheduler + A2A active")`)
	if daemonSigIdx < 0 {
		t.Fatal("main.go lost the daemon-mode fallback shutdown branch")
	}

	noMCPBody := s[noMCPIdx:daemonSigIdx]
	if !strings.Contains(noMCPBody, "a2aSrv.Stop()") {
		t.Error("the --no-mcp shutdown branch must call a2aSrv.Stop() after receiving SIGINT/SIGTERM so the A2A HTTP listener closes gracefully instead of being killed by process exit")
	}

	daemonBody := s[daemonSigIdx:]
	if !strings.Contains(daemonBody, "a2aSrv.Stop()") {
		t.Error("the daemon-mode fallback shutdown branch (after the MCP server exits) must call a2aSrv.Stop() after receiving SIGINT/SIGTERM so the A2A HTTP listener closes gracefully instead of being killed by process exit")
	}
}
