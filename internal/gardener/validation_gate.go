package gardener

import (
	"fmt"
	"log"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// ValidationGateConfig holds tunable thresholds for the validation gate.
type ValidationGateConfig struct {
	MinSuccessRate  float64 // minimum tool-call success rate (default 0.80)
	MinRecoveryRate float64 // minimum recovery rate (default 0.30)
	Enabled         bool    // whether the gate is active (default true)
}

// DefaultValidationGateConfig returns sensible defaults.
func DefaultValidationGateConfig() ValidationGateConfig {
	return ValidationGateConfig{
		MinSuccessRate:  0.80,
		MinRecoveryRate: 0.30,
		Enabled:         true,
	}
}

// ValidationGate checks evolved trees against minimum quality thresholds
// before allowing them to be deployed to agents.
func ValidationGate(agentName, treeName string, config ValidationGateConfig) error {
	if !config.Enabled {
		return nil
	}

	metrics := engine.GetSLOMetrics(agentName, treeName)

	// Fail closed: no SLO evidence means the tree cannot be verified safe to
	// deploy. The gardener process never executes trees, so until SLO metrics
	// are persisted and shared across processes (remediation task B1), missing
	// metrics block deployment instead of waving it through unverified.
	if metrics.TotalCalls == 0 {
		return fmt.Errorf("validation gate REJECTED %s/%s: no SLO evidence; failing closed", agentName, treeName)
	}

	successRate := metrics.SuccessRate()
	if successRate < config.MinSuccessRate {
		return fmt.Errorf("validation gate REJECTED %s/%s: success rate %.2f below threshold %.2f",
			agentName, treeName, successRate, config.MinSuccessRate)
	}

	recoveryRate := metrics.RecoveryRate()
	// Only enforce recovery rate if there have been failures
	if metrics.FailedCalls > 0 && recoveryRate < config.MinRecoveryRate {
		return fmt.Errorf("validation gate REJECTED %s/%s: recovery rate %.2f below threshold %.2f",
			agentName, treeName, recoveryRate, config.MinRecoveryRate)
	}

	log.Printf("[validation-gate] %s/%s: PASSED (success=%.2f, recovery=%.2f, calls=%d)",
		agentName, treeName, successRate, recoveryRate, metrics.TotalCalls)
	return nil
}
