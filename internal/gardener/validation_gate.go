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
	EvidencePath    string  // SLO evidence file written by the agent process (B1); empty disables file fallback
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

	evidence := engine.GetSLOMetrics(agentName, treeName).Snapshot()

	// The gardener process never executes trees, so its in-memory metrics are
	// empty — fall back to file evidence written by the agent process (B1).
	if evidence.TotalCalls == 0 && config.EvidencePath != "" {
		fileEvidence, err := loadTreeEvidence(config.EvidencePath, treeName)
		if err != nil {
			log.Printf("[validation-gate] %s/%s: no usable file evidence: %v", agentName, treeName, err)
		} else {
			evidence = fileEvidence
		}
	}

	// Fail closed: no SLO evidence means the tree cannot be verified safe to
	// deploy, so missing metrics block deployment instead of waving it through
	// unverified.
	if evidence.TotalCalls == 0 {
		return fmt.Errorf("validation gate REJECTED %s/%s: no SLO evidence; failing closed", agentName, treeName)
	}

	successRate := evidence.SuccessRate()
	if successRate < config.MinSuccessRate {
		return fmt.Errorf("validation gate REJECTED %s/%s: success rate %.2f below threshold %.2f",
			agentName, treeName, successRate, config.MinSuccessRate)
	}

	recoveryRate := evidence.RecoveryRate()
	// Only enforce recovery rate if there have been failures
	if evidence.FailedCalls > 0 && recoveryRate < config.MinRecoveryRate {
		return fmt.Errorf("validation gate REJECTED %s/%s: recovery rate %.2f below threshold %.2f",
			agentName, treeName, recoveryRate, config.MinRecoveryRate)
	}

	log.Printf("[validation-gate] %s/%s: PASSED (success=%.2f, recovery=%.2f, calls=%d)",
		agentName, treeName, successRate, recoveryRate, evidence.TotalCalls)
	return nil
}

// loadTreeEvidence aggregates file-based SLO snapshots for treeName across all
// agents that executed it. The gate gates trees, not agent/tree pairs, so any
// agent's execution history counts as evidence.
func loadTreeEvidence(path, treeName string) (engine.SLOSnapshot, error) {
	snapshots, err := engine.LoadSLOEvidence(path)
	if err != nil {
		return engine.SLOSnapshot{}, err
	}
	agg := engine.SLOSnapshot{TreeName: treeName}
	for _, s := range snapshots {
		if s.TreeName != treeName {
			continue
		}
		agg.TotalCalls += s.TotalCalls
		agg.SuccessfulCalls += s.SuccessfulCalls
		agg.FailedCalls += s.FailedCalls
		agg.RecoveredCalls += s.RecoveredCalls
	}
	if agg.TotalCalls == 0 {
		return agg, fmt.Errorf("no snapshots for tree %q in %s", treeName, path)
	}
	return agg, nil
}
