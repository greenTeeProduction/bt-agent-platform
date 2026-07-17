package dashboard

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

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
// cycleBreakerSuccess semantics (a rate-limit carryover is a healthy,
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
		t.Fatalf("after a single RateLimitCarryoverOutcome run with threshold 1, breaker state = %v, want %v (closed) — recordCircuitBreakerOutcome must call CBStore.RecordSuccess for a rate-limit carryover outcome, matching scheduler.go's cycleBreakerSuccess semantics", cb.State(), agent.CircuitClosed)
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
