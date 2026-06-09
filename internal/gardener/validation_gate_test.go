package gardener

import (
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
)

func TestValidationGate_NoMetrics_Allow(t *testing.T) {
	config := DefaultValidationGateConfig()
	err := ValidationGate("new_agent", "test_tree", config)
	if err != nil {
		t.Errorf("expected allow for agent with no metrics, got: %v", err)
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
