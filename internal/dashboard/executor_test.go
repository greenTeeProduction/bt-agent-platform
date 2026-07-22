package dashboard

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// TestRecordCircuitBreakerOutcome_HealthyOutcomesKeepBreakerClosed verifies
// that healthy-but-not-"success" outcomes — no_change (analysis-only) and
// degraded (deterministic fallback) — do NOT trip the breaker. The scheduler's
// authoritative classifier treats these as healthy; the dashboard must agree,
// or an analysis-heavy agent run repeatedly through the dashboard opens the
// shared breaker and gets blocked everywhere despite nothing being broken.
func TestRecordCircuitBreakerOutcome_HealthyOutcomesKeepBreakerClosed(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	cbStore := agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{
		Threshold: 2,
		Cooldown:  time.Minute,
	})
	exec := &AgentExecutor{CBStore: cbStore}

	for _, outcome := range []string{"no_change", "degraded", "completed", "no_change", "degraded", "completed"} {
		exec.recordCircuitBreakerOutcome("analysis-agent", &agent.RunResult{Outcome: outcome}, nil)
	}
	if cb := cbStore.Get("analysis-agent"); cb.State() == agent.CircuitOpen {
		t.Fatalf("healthy no_change/degraded runs opened the breaker (state=%v); they must count as successes", cb.State())
	}
}

// TestRecordCircuitBreakerOutcome_NilResultTripsBreaker verifies that a hard
// failure — agent.RunAgent returning (nil, err) on runner-not-configured,
// context timeout, or LLM-unavailable — is recorded as a breaker failure. The
// pre-fix code returned early on res == nil, so a persistently broken agent
// never opened the breaker and the dashboard kept burning worker slots on it.
func TestRecordCircuitBreakerOutcome_NilResultTripsBreaker(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	cbStore := agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{
		Threshold: 2,
		Cooldown:  time.Minute,
	})
	exec := &AgentExecutor{CBStore: cbStore}

	for i := 0; i < 2; i++ {
		exec.recordCircuitBreakerOutcome("broken-agent", nil, errAgentBroken)
	}
	if cb := cbStore.Get("broken-agent"); cb.State() != agent.CircuitOpen {
		t.Fatalf("after 2 nil-result hard failures with threshold 2, breaker state = %v, want open", cb.State())
	}
}

var errAgentBroken = errors.New("runner not configured")

// TestHermesOutcome pins the Hermes-fallback outcome vocabulary to the
// scheduler's canonical one: the CLI path must emit "failure" (not the
// one-off "failed" spelling only literal matchers downstream understood) and
// "completed" for an error-free run whose output matches no keyword.
func TestHermesOutcome(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		execErr error
		want    string
	}{
		{"exec error is canonical failure", "boom", errors.New("exit 1"), "failure"},
		{"success keyword", "Task succeeded: success", nil, "success"},
		{"failed keyword is canonical failure", "the run failed", nil, "failure"},
		{"error keyword is canonical failure", "error: nope", nil, "failure"},
		{"indeterminate is completed", "42 widgets processed", nil, "completed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hermesOutcome(tc.output, tc.execErr); got != tc.want {
				t.Fatalf("hermesOutcome(%q, %v) = %q, want %q", tc.output, tc.execErr, got, tc.want)
			}
		})
	}
}

// TestRecordTaskMetric_NilResultRecordsFailure verifies the task metrics stay
// consistent with the circuit breaker for hard failures: a nil RunResult with
// a run error records a zero-duration failed task instead of being silently
// skipped, so an agent the breaker counts as failing is also visible as
// failing in GetAgentMetrics.
func TestRecordTaskMetric_NilResultRecordsFailure(t *testing.T) {
	exec := &AgentExecutor{}
	const agentName = "nil-result-metric-agent"

	exec.recordTaskMetric(agentName, nil, errAgentBroken)

	var stats *AgentStats
	for _, s := range GetAgentMetrics() {
		s := s
		if s.Name == agentName {
			stats = &s
			break
		}
	}
	if stats == nil {
		t.Fatalf("GetAgentMetrics() has no entry for %q — a nil-result hard failure must record a failed task", agentName)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1 (nil result + run error must count as a task failure)", stats.ErrorCount)
	}
}

// TestRecordTaskMetric_HealthyOutcomeWithRunErrorIsFailure pins the runErr
// half of the shared classifier at the metrics layer: an outcome that reads
// healthy but arrived with a non-nil run error must count as a task failure,
// matching agent.IsBreakerSuccess.
func TestRecordTaskMetric_HealthyOutcomeWithRunErrorIsFailure(t *testing.T) {
	exec := &AgentExecutor{}
	const agentName = "errored-success-metric-agent"

	exec.recordTaskMetric(agentName, &agent.RunResult{
		AgentName: agentName,
		Outcome:   "success",
		Duration:  time.Millisecond,
	}, errAgentBroken)

	var stats *AgentStats
	for _, s := range GetAgentMetrics() {
		s := s
		if s.Name == agentName {
			stats = &s
			break
		}
	}
	if stats == nil {
		t.Fatalf("GetAgentMetrics() has no entry for %q", agentName)
	}
	if stats.ErrorCount != 1 || stats.SuccessCount != 0 {
		t.Errorf("got SuccessCount=%d ErrorCount=%d, want 0/1 (healthy outcome with a run error must count as failure)", stats.SuccessCount, stats.ErrorCount)
	}
}

// TestRunTaskResult_RecordsCircuitBreakerOutcome verifies that AgentExecutor.
// RunTaskResult, when given a CBStore, reports each run's outcome to the
// shared agent.AgentCircuitBreakerStore and persists it to
// agent.CircuitBreakersFile() — mirroring internal/agent/scheduler.go's
// runJob, which calls reportAgentOutcome then cbStore.Save(CircuitBreakersFile())
// on every cycle. Today RunTaskResult never touches a circuit breaker store at
// all, so a flaky agent invoked only through the dashboard never trips the
// breaker the scheduler and A2A auction paths already honor.
func TestRunTaskResult_RecordsCircuitBreakerOutcome(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	cbStore := agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{
		Threshold: 2,
		Cooldown:  time.Minute,
	})

	exec := &AgentExecutor{
		Timeout: 5 * time.Second,
		CBStore: cbStore,
		Runner: &agent.RunDeps{
			// Inverter(AlwaysSucceed) deterministically fails without needing
			// a real LLM or registry — the same technique cmd/bt-dashboard's
			// tests use for a deterministic AlwaysSucceed tree, inverted.
			ResolveTree: func(_ string) *evolution.SerializableNode {
				return &evolution.SerializableNode{
					Type:     "Inverter",
					Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}},
				}
			},
		},
	}

	const agentName = "flaky-dashboard-agent"
	for i := 0; i < 2; i++ {
		res, err := exec.RunTaskResult(agentName, "do the thing", "flaky-tree")
		if res == nil {
			t.Fatalf("run %d: RunTaskResult returned nil result (err=%v)", i, err)
		}
		if res.Outcome != "failure" {
			t.Fatalf("run %d: got outcome %q, want %q (test setup must drive a failing outcome)", i, res.Outcome, "failure")
		}
	}

	cb := cbStore.Get(agentName)
	if cb.State() != agent.CircuitOpen {
		t.Fatalf("after %d consecutive failing RunTaskResult calls with threshold 2, breaker state = %v, want %v (open) — RunTaskResult must call CBStore.RecordFailure on failing outcomes", 2, cb.State(), agent.CircuitOpen)
	}

	data, err := os.ReadFile(agent.CircuitBreakersFile())
	if err != nil {
		t.Fatalf("RunTaskResult must persist breaker state via CBStore.Save(agent.CircuitBreakersFile()) on every run, like scheduler.go's runJob does: %v", err)
	}
	if !strings.Contains(string(data), agentName) || !strings.Contains(string(data), "open") {
		t.Fatalf("persisted circuit breaker file missing open state for %q: %s", agentName, data)
	}
}

// TestRunTaskResult_RateLimitCarryoverOutcome_DoesNotTripBreaker verifies
// that recordCircuitBreakerOutcome treats a agent.RateLimitCarryoverOutcome
// result as a circuit-breaker success, matching internal/agent/scheduler.go's
// IsBreakerSuccess semantics (a rate-limit carryover is a healthy,
// expected backoff pause, not a genuine failure). Today
// recordCircuitBreakerOutcome only special-cases the literal "success"
// string, so a rate-limit carryover run calls CBStore.RecordFailure exactly
// like a real failure would, tripping the breaker on a healthy pause.
func TestRunTaskResult_RateLimitCarryoverOutcome_DoesNotTripBreaker(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	engine.RegisterAction("DashboardRateLimitCarryoverAction", func(ctx *btcore.BTContext[engine.Blackboard]) int {
		ctx.Blackboard.Outcome = agent.RateLimitCarryoverOutcome
		return -1
	})

	cbStore := agent.NewAgentCircuitBreakerStore(agent.CircuitBreakerOptions{
		Threshold: 1,
		Cooldown:  time.Minute,
	})

	exec := &AgentExecutor{
		Timeout: 5 * time.Second,
		CBStore: cbStore,
		Runner: &agent.RunDeps{
			ResolveTree: func(_ string) *evolution.SerializableNode {
				return &evolution.SerializableNode{Type: "Action", Name: "DashboardRateLimitCarryoverAction"}
			},
		},
	}

	const agentName = "rate-limit-dashboard-agent"
	res, err := exec.RunTaskResult(agentName, "do the thing", "rate-limit-tree")
	if res == nil {
		t.Fatalf("RunTaskResult returned nil result (err=%v)", err)
	}
	if res.Outcome != agent.RateLimitCarryoverOutcome {
		t.Fatalf("got outcome %q, want %q (test setup must drive a rate-limit carryover outcome)", res.Outcome, agent.RateLimitCarryoverOutcome)
	}

	cb := cbStore.Get(agentName)
	if cb.State() != agent.CircuitClosed {
		t.Fatalf("after a single RateLimitCarryoverOutcome run with threshold 1, breaker state = %v, want %v (closed) — recordCircuitBreakerOutcome must call CBStore.RecordSuccess for a rate-limit carryover outcome, matching scheduler.go's IsBreakerSuccess semantics", cb.State(), agent.CircuitClosed)
	}
	if cb.FailureCount() != 0 {
		t.Errorf("cb.FailureCount() = %d, want 0 (a rate-limit carryover must not be counted as a failure)", cb.FailureCount())
	}
}

// TestRunTaskResult_RecordsTaskMetric verifies that AgentExecutor.
// RunTaskResult, run through the in-process Runner path, records every run's
// outcome and duration to the dashboard's global agent-task metrics (via
// dashboard.RecordTask) so GetAgentMetrics() — the data backing the
// dashboard's agent metrics panel and /metrics endpoint — reflects agent
// executions. Today RunTaskResult's only per-run side effect is
// recordCircuitBreakerOutcome; it never calls RecordTask, so every agent run
// through the dashboard leaves GetAgentMetrics() permanently empty for that
// agent even though internal/agent/scheduler.go's equivalent path records it.
func TestRunTaskResult_RecordsTaskMetric(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	exec := &AgentExecutor{
		Timeout: 5 * time.Second,
		Runner: &agent.RunDeps{
			ResolveTree: func(_ string) *evolution.SerializableNode {
				return &evolution.SerializableNode{Type: "AlwaysSucceed"}
			},
		},
	}

	const agentName = "metrics-dashboard-agent"
	res, err := exec.RunTaskResult(agentName, "do the thing", "metrics-tree")
	if res == nil {
		t.Fatalf("RunTaskResult returned nil result (err=%v)", err)
	}
	if res.Outcome != "success" {
		t.Fatalf("got outcome %q, want %q (test setup must drive a succeeding outcome)", res.Outcome, "success")
	}

	var stats *AgentStats
	for _, s := range GetAgentMetrics() {
		s := s
		if s.Name == agentName {
			stats = &s
			break
		}
	}
	if stats == nil {
		t.Fatalf("GetAgentMetrics() has no entry for %q — RunTaskResult must call dashboard.RecordTask on every run", agentName)
	}
	if stats.TotalCount != 1 {
		t.Errorf("GetAgentMetrics()[%q].TotalCount = %d, want 1", agentName, stats.TotalCount)
	}
	if stats.SuccessCount != 1 {
		t.Errorf("GetAgentMetrics()[%q].SuccessCount = %d, want 1", agentName, stats.SuccessCount)
	}
}

// TestRecordTaskMetric_RateLimitCarryoverOutcome_CountsAsSuccess verifies
// that recordTaskMetric treats a agent.RateLimitCarryoverOutcome result as a
// dashboard success, matching the exemption recordCircuitBreakerOutcome
// already applies (see TestRunTaskResult_RateLimitCarryoverOutcome_DoesNotTripBreaker)
// and scheduler.go's IsBreakerSuccess: a rate-limit carryover is an
// expected backoff pause, not a genuine task failure. Today recordTaskMetric
// only treats the literal "success" string as success, so RecordTask logs a
// carryover run as an error, inflating GetAgentMetrics().ErrorCount for a
// healthy pause.
func TestRecordTaskMetric_RateLimitCarryoverOutcome_CountsAsSuccess(t *testing.T) {
	exec := &AgentExecutor{}

	const agentName = "rate-limit-task-metric-agent"
	exec.recordTaskMetric(agentName, &agent.RunResult{
		AgentName: agentName,
		Outcome:   agent.RateLimitCarryoverOutcome,
		Duration:  time.Millisecond,
	}, nil)

	var stats *AgentStats
	for _, s := range GetAgentMetrics() {
		s := s
		if s.Name == agentName {
			stats = &s
			break
		}
	}
	if stats == nil {
		t.Fatalf("GetAgentMetrics() has no entry for %q — recordTaskMetric must call dashboard.RecordTask", agentName)
	}
	if stats.SuccessCount != 1 {
		t.Errorf("GetAgentMetrics()[%q].SuccessCount = %d, want 1 (recordTaskMetric must treat RateLimitCarryoverOutcome as a dashboard success, not a failure)", agentName, stats.SuccessCount)
	}
	if stats.ErrorCount != 0 {
		t.Errorf("GetAgentMetrics()[%q].ErrorCount = %d, want 0 (a healthy rate-limit carryover must not count as a dashboard task failure)", agentName, stats.ErrorCount)
	}
}

// TestRecordBlockFitnessMetric_RateLimitCarryoverOutcome_UsesHealthyTier
// verifies that recordBlockFitnessMetric treats a zero-Quality
// agent.RateLimitCarryoverOutcome result as healthy — the same tier a
// "success" or "completed" outcome gets (score 75) — instead of the
// failure-tier score of 25. A zero Quality is exactly what the Hermes-CLI
// fallback path in RunTaskResult produces (it never sets RunResult.Quality),
// so this is the scenario the milestone's failure-tier regression actually
// hits. Today recordBlockFitnessMetric's score<=0 fallback only checks
// success/"completed", so a RateLimitCarryoverOutcome result falls through
// to the failure tier.
func TestRecordBlockFitnessMetric_RateLimitCarryoverOutcome_UsesHealthyTier(t *testing.T) {
	exec := &AgentExecutor{}

	const agentName = "rate-limit-block-fitness-agent"
	const treeID = "rate-limit-block-fitness-tree"
	exec.recordBlockFitnessMetric(agentName, treeID, &agent.RunResult{
		AgentName: agentName,
		Outcome:   agent.RateLimitCarryoverOutcome,
		Quality:   0,
	}, nil)

	snap := BlockFitnessSnapshot()
	var score int64
	var found bool
	for key, val := range snap {
		if strings.Contains(key, treeID) && strings.Contains(key, agentName) {
			score = val
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("BlockFitnessSnapshot() has no entry for tree %q / agent %q — recordBlockFitnessMetric must call RecordBlockFitness", treeID, agentName)
	}
	if score < 75 {
		t.Errorf("block fitness score for %q/%q = %d, want >= 75 (healthy tier) — recordBlockFitnessMetric must not drop a RateLimitCarryoverOutcome result to the failure tier (25)", treeID, agentName, score)
	}
}

// recordBlockFitnessMetric duplicates reliability.ScoreOutcome's formula (Q5
// Consistency & Reuse milestone 1 extracted the canonical version; milestones
// 2-3 already delegated internal/blocks.ScoreFromBlackboard and
// internal/engine.fitnessScoreFromBB). Scanning the source directly — rather
// than calling recordBlockFitnessMetric and comparing outputs — is necessary
// because the duplicated formula is byte-for-byte identical to
// reliability.ScoreOutcome, so every input/output pair matches whether or not
// the delegation exists. This test fails until recordBlockFitnessMetric's
// body is replaced with a call to reliability.ScoreOutcome and the
// duplicated inline formula is deleted.
func TestRecordBlockFitnessMetric_DelegatesToReliabilityScoreOutcome(t *testing.T) {
	src, err := os.ReadFile("executor.go")
	if err != nil {
		t.Fatalf("reading executor.go: %v", err)
	}
	body := string(src)

	if !strings.Contains(body, "reliability.ScoreOutcome(") {
		t.Error("recordBlockFitnessMetric must call reliability.ScoreOutcome instead of duplicating its formula")
	}
	if strings.Contains(body, "score := res.Quality * 100") {
		t.Error("executor.go still contains the duplicated inline scoring formula; delete it now that reliability.ScoreOutcome is canonical")
	}
}

// TestPickTreeForTask_RoutesAuctionShapedTasksToAuctionDemo verifies that
// tasks whose text signals auction/delegation intent (mirroring
// engine.AuctionTaskKeywords, the same keyword set that gates the
// auction_demo tree's IsAuctionTask condition) are routed to the
// "auction_demo" tree so the sprint-execution path actually exercises
// internal/a2a's announce->bid->award auction machinery, instead of falling
// through to a generic tree that never touches auction code.
func TestPickTreeForTask_RoutesAuctionShapedTasksToAuctionDemo(t *testing.T) {
	if len(engine.AuctionTaskKeywords) == 0 {
		t.Fatal("engine.AuctionTaskKeywords must be non-empty and exported so dashboard can mirror it")
	}

	for _, kw := range engine.AuctionTaskKeywords {
		task := Task{
			Title:       "Please " + kw + " this piece of work",
			Description: "routed via keyword: " + kw,
		}
		got := PickTreeForTask(task)
		if got != "auction_demo" {
			t.Errorf("PickTreeForTask(keyword %q) = %q, want %q", kw, got, "auction_demo")
		}
	}
}

func TestPickTreeForTask_NonAuctionTasksUnaffected(t *testing.T) {
	task := Task{Title: "Fix a bug in the login flow", Description: "unrelated to auctions"}
	got := PickTreeForTask(task)
	if got != "domain:code_review" {
		t.Errorf("PickTreeForTask(bug task) = %q, want %q (regression: auction routing must not steal unrelated tasks)", got, "domain:code_review")
	}
}

// TestPickTreeForTask_ConsultsKnowledgeGraphOverStaticSwitch verifies that
// once a knowledge-graph discoverer is wired in (via DiscoverTreeFn, set from
// cmd/bt-dashboard to knowledge.KnowledgeGraph.Discover), PickTreeForTask
// prefers the graph's confident answer over the static 7-branch keyword
// switch — the knowledge graph is meant to replace the switch, not merely
// fill gaps it misses.
func TestPickTreeForTask_ConsultsKnowledgeGraphOverStaticSwitch(t *testing.T) {
	orig := DiscoverTreeFn
	t.Cleanup(func() { DiscoverTreeFn = orig })

	DiscoverTreeFn = func(task string) (string, float64) {
		return "domain:security_audit", 0.9
	}

	// The static switch would route this to domain:code_review (the "bug"
	// branch), but the wired knowledge graph should win.
	task := Task{Title: "Fix a bug in the login flow", Description: "unrelated to auctions"}
	got := PickTreeForTask(task)
	if got != "domain:security_audit" {
		t.Errorf("PickTreeForTask() = %q, want %q (knowledge graph must take priority over the static switch)", got, "domain:security_audit")
	}
}

// TestPickTreeForTask_FallsBackToStaticSwitchWhenGraphUnconfident verifies
// that when the wired knowledge graph reports no confident match (mirroring
// knowledge.KnowledgeGraph.Discover's own ("", 0) contract for an
// unconfident task), PickTreeForTask still falls back to the static keyword
// switch instead of routing to an empty tree ID.
func TestPickTreeForTask_FallsBackToStaticSwitchWhenGraphUnconfident(t *testing.T) {
	orig := DiscoverTreeFn
	t.Cleanup(func() { DiscoverTreeFn = orig })

	DiscoverTreeFn = func(task string) (string, float64) {
		return "", 0
	}

	task := Task{Title: "Fix a bug in the login flow", Description: "unrelated to auctions"}
	got := PickTreeForTask(task)
	if got != "domain:code_review" {
		t.Errorf("PickTreeForTask() = %q, want %q (must fall back to the static switch when the graph has no confident match)", got, "domain:code_review")
	}
}

// TestPickTreeForTask_AuctionRoutingBeatsKnowledgeGraph verifies that
// auction/delegation-shaped task text (ADR-073) still routes to
// "auction_demo" ahead of the knowledge graph, even when the graph is wired
// and confidently suggests a different tree — the auction pre-check is not
// part of the "static 7-branch switch" this goal replaces.
func TestPickTreeForTask_AuctionRoutingBeatsKnowledgeGraph(t *testing.T) {
	if len(engine.AuctionTaskKeywords) == 0 {
		t.Fatal("engine.AuctionTaskKeywords must be non-empty and exported so dashboard can mirror it")
	}
	orig := DiscoverTreeFn
	t.Cleanup(func() { DiscoverTreeFn = orig })

	DiscoverTreeFn = func(task string) (string, float64) {
		return "domain:code_review", 0.95
	}

	kw := engine.AuctionTaskKeywords[0]
	task := Task{Title: "Please " + kw + " this piece of work", Description: "routed via keyword: " + kw}
	got := PickTreeForTask(task)
	if got != "auction_demo" {
		t.Errorf("PickTreeForTask(keyword %q) = %q, want %q (auction routing must beat the knowledge graph)", kw, got, "auction_demo")
	}
}
