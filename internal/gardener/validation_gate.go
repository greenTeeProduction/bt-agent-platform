package gardener

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// ValidationGateConfig holds tunable thresholds for the validation gate.
type ValidationGateConfig struct {
	MinSuccessRate  float64 // minimum tool-call success rate (default 0.80)
	MinRecoveryRate float64 // minimum recovery rate (default 0.30)
	Enabled         bool    // whether the gate is active (default true)
	EvidencePath    string  // SLO evidence file written by the agent process (B1); empty disables file fallback
	// AllowUnverified permits trees with NO evidence anywhere to pass — the
	// gardener's persisted output is not consumed by live agents at runtime,
	// so blocking never-executed trees forever only freezes evolution.
	// Trees WITH evidence always get full threshold enforcement.
	// Default false (fail-closed, A2 semantics).
	AllowUnverified bool
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
			slog.Warn("validation-gate: no usable file evidence", "agent", agentName, "tree", treeName, "error", err)
		} else {
			evidence = fileEvidence
		}
	}

	if evidence.TotalCalls == 0 {
		// No evidence anywhere: the tree has never been executed by any agent.
		if config.AllowUnverified {
			slog.Info("validation-gate: no SLO evidence, passing unverified (AllowUnverified)",
				"agent", agentName, "tree", treeName)
			return nil
		}
		// Fail closed: no SLO evidence means the tree cannot be verified safe to
		// deploy, so missing metrics block deployment instead of waving it
		// through unverified.
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

	slog.Info("validation-gate: passed",
		"agent", agentName, "tree", treeName, "success_rate", successRate, "recovery_rate", recoveryRate, "calls", evidence.TotalCalls)
	return nil
}

// evidenceTreeNames returns all evidence-file tree names that count for a
// gardener registry name. The registry names domain trees "domain_<x>" while
// the agent process records SLO evidence under the engine name "domain:<x>" —
// both spellings must match or no domain tree can ever find its evidence.
func evidenceTreeNames(treeName string) []string {
	names := []string{treeName}
	if rest, ok := strings.CutPrefix(treeName, "domain_"); ok {
		names = append(names, "domain:"+rest)
	}
	return names
}

// loadTreeEvidence aggregates file-based SLO snapshots for treeName across all
// agents that executed it. The gate gates trees, not agent/tree pairs, so any
// agent's execution history counts as evidence.
func loadTreeEvidence(path, treeName string) (engine.SLOSnapshot, error) {
	snapshots, err := engine.LoadSLOEvidence(path)
	if err != nil {
		return engine.SLOSnapshot{}, err
	}
	accepted := evidenceTreeNames(treeName)
	agg := engine.SLOSnapshot{TreeName: treeName}
	for _, s := range snapshots {
		if !slices.Contains(accepted, s.TreeName) {
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
