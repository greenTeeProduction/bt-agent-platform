package gardener

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestValidationGate_NoMetrics_FailClosed proves the gate REJECTS when no SLO
// metrics exist (A2 fail-closed remediation). Previously the gate allowed
// deployment on empty metrics, which made it decorative: the gardener process
// never populates the in-process SLO map, so TotalCalls was always 0 and every
// deployment was waved through.
func TestValidationGate_NoMetrics_FailClosed(t *testing.T) {
	config := DefaultValidationGateConfig()
	err := ValidationGate("agent_with_no_metrics", "test_tree", config)
	if err == nil {
		t.Fatal("expected rejection when no SLO metrics exist (fail closed), got allow")
	}
	if !strings.Contains(err.Error(), "no SLO evidence") {
		t.Errorf("rejection reason should mention missing SLO evidence, got: %v", err)
	}
}

func TestValidationGate_HighSuccessRate_Pass(t *testing.T) {
	metrics := engine.GetSLOMetrics("good_agent", "test_tree")
	// Reset by getting a fresh instance — record 10 successes
	for i := 0; i < 10; i++ {
		metrics.RecordSuccess(50 * time.Millisecond)
	}

	config := DefaultValidationGateConfig()
	err := ValidationGate("good_agent", "test_tree", config)
	if err != nil {
		t.Errorf("expected pass for high success rate, got: %v", err)
	}
}

func TestValidationGate_LowSuccessRate_Reject(t *testing.T) {
	agentName := "bad_agent_test"
	treeName := "test_tree"

	metrics := engine.GetSLOMetrics(agentName, treeName)
	// Record 3 successes, 7 failures = 30% success rate
	for i := 0; i < 3; i++ {
		metrics.RecordSuccess(50 * time.Millisecond)
	}
	for i := 0; i < 7; i++ {
		metrics.RecordFailure(100 * time.Millisecond)
	}

	config := DefaultValidationGateConfig()
	err := ValidationGate(agentName, treeName, config)
	if err == nil {
		t.Errorf("expected rejection for 30%% success rate")
	}
}

func TestValidationGate_Disabled(t *testing.T) {
	metrics := engine.GetSLOMetrics("any_agent", "test_tree")
	for i := 0; i < 100; i++ {
		metrics.RecordFailure(100 * time.Millisecond)
	}

	config := DefaultValidationGateConfig()
	config.Enabled = false
	err := ValidationGate("any_agent", "test_tree", config)
	if err != nil {
		t.Errorf("expected allow when gate disabled, got: %v", err)
	}
}

func TestValidationGate_LowRecoveryRate_Reject(t *testing.T) {
	agentName := "low_recovery_agent"
	treeName := "test_tree"

	metrics := engine.GetSLOMetrics(agentName, treeName)
	// 8 successes, 2 failures, 0 recoveries = 0% recovery
	for i := 0; i < 8; i++ {
		metrics.RecordSuccess(50 * time.Millisecond)
	}
	for i := 0; i < 2; i++ {
		metrics.RecordFailure(100 * time.Millisecond)
	}

	config := DefaultValidationGateConfig()
	config.MinRecoveryRate = 0.50 // stricter than default
	err := ValidationGate(agentName, treeName, config)
	if err == nil {
		t.Errorf("expected rejection for 0%% recovery rate")
	}
}

// ─── NSGA-II/Pareto multi-objective acceptance ──────────────────────────────
//
// The gate used to accept on scalar fitness: each SLO dimension was compared
// against its own threshold in isolation and any single shortfall rejected the
// tree outright. That makes every objective a hard constraint and forbids
// trade-offs — an evolved tree that gives up a little success rate to gain a
// large amount of recovery capability was refused even though it is strictly
// better on balance. These tests pin the replacement: the evidence vector and
// the configured thresholds become points in the same multi-objective space,
// and acceptance is Pareto non-domination against the threshold reference
// point (evolution.ParetoAccepts).

// TestValidationGate_ParetoAcceptance_AllowsTradeoff proves the gate accepts a
// tree that sits BELOW MinSuccessRate but far above MinRecoveryRate: the
// threshold reference point (0.80 success, 0.30 recovery) does not dominate
// (0.75 success, 1.00 recovery), so the candidate is on the front. The old
// scalar check rejected this on the success-rate axis alone.
func TestValidationGate_ParetoAcceptance_AllowsTradeoff(t *testing.T) {
	agentName := "pareto_tradeoff_agent"
	treeName := "pareto_tradeoff_tree"

	metrics := engine.GetSLOMetrics(agentName, treeName)
	// 75 successes / 25 failures = 0.75 success rate (below the 0.80 floor),
	// with every failure recovered = 1.00 recovery rate (far above 0.30).
	for i := 0; i < 75; i++ {
		metrics.RecordSuccess(50 * time.Millisecond)
	}
	for i := 0; i < 25; i++ {
		metrics.RecordFailure(100 * time.Millisecond)
		metrics.RecordRecovery(20 * time.Millisecond)
	}

	config := DefaultValidationGateConfig()
	if err := ValidationGate(agentName, treeName, config); err != nil {
		t.Errorf("expected multi-objective acceptance for a success-rate/recovery-rate trade-off, got: %v", err)
	}
}

// TestValidationGate_ParetoAcceptance_RejectsDominatedEvidence is the guard
// that keeps the trade-off allowance above from becoming a free pass: evidence
// that is worse than the threshold reference point on EVERY objective is
// dominated by it, so it must still be rejected.
func TestValidationGate_ParetoAcceptance_RejectsDominatedEvidence(t *testing.T) {
	// Names deliberately avoid the word "dominate": the rejection message
	// interpolates them, so a name containing it would satisfy the reason
	// assertion below without the gate ever running a domination check.
	agentName := "pareto_floor_agent"
	treeName := "pareto_floor_tree"

	metrics := engine.GetSLOMetrics(agentName, treeName)
	// 0.75 success rate (below 0.80) AND 0.10 recovery rate (below 0.30):
	// worse on both axes, so the threshold point dominates this evidence.
	for i := 0; i < 75; i++ {
		metrics.RecordSuccess(50 * time.Millisecond)
	}
	for i := 0; i < 25; i++ {
		metrics.RecordFailure(100 * time.Millisecond)
	}
	for i := 0; i < 2; i++ {
		metrics.RecordRecovery(20 * time.Millisecond)
	}

	config := DefaultValidationGateConfig()
	err := ValidationGate(agentName, treeName, config)
	if err == nil {
		t.Fatal("expected rejection for evidence dominated by the threshold reference point")
	}
	if !strings.Contains(err.Error(), "dominate") {
		t.Errorf("rejection should name multi-objective domination as the reason, got: %v", err)
	}
}
