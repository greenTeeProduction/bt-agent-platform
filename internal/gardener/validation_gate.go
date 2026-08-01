package gardener

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
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
	// MaxObjectiveRegression bounds how far below its own threshold a single
	// objective may fall while the tree is still eligible for multi-objective
	// acceptance (default 0.10). Pareto non-domination alone would let a tree
	// buy an unbounded success-rate collapse with a high recovery rate, so the
	// trade-off band is capped: give up at most this much on any one axis.
	// Zero means no trade-off is tolerated, which reproduces the old
	// per-threshold scalar gate — the safe reading for hand-built configs.
	MaxObjectiveRegression float64
}

// DefaultValidationGateConfig returns sensible defaults.
func DefaultValidationGateConfig() ValidationGateConfig {
	return ValidationGateConfig{
		MinSuccessRate:         0.80,
		MinRecoveryRate:        0.30,
		Enabled:                true,
		MaxObjectiveRegression: 0.10,
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

	if err := paretoAcceptance(agentName, treeName, evidence, config); err != nil {
		return err
	}

	slog.Info("validation-gate: passed",
		"agent", agentName, "tree", treeName,
		"success_rate", evidence.SuccessRate(), "recovery_rate", evidence.RecoveryRate(), "calls", evidence.TotalCalls)
	return nil
}

// gateObjective is one axis of the gate's multi-objective space: what the
// evidence measured (got) against what the config asks for (want).
type gateObjective struct {
	dim  evolution.FitnessDimension
	got  float64
	want float64
}

// gateObjectives projects an SLO snapshot and the configured thresholds onto
// the same objective axes. Recovery rate only becomes an objective once
// something has actually failed — with no failures RecoveryRate is 0 by
// definition (engine.SLOSnapshot.RecoveryRate), so scoring it would penalize a
// flawless tree for never having needed to recover.
func gateObjectives(evidence engine.SLOSnapshot, config ValidationGateConfig) []gateObjective {
	objectives := []gateObjective{
		{dim: evolution.DimSuccessRate, got: evidence.SuccessRate(), want: config.MinSuccessRate},
	}
	if evidence.FailedCalls > 0 {
		objectives = append(objectives, gateObjective{
			dim: evolution.DimRecoveryRate, got: evidence.RecoveryRate(), want: config.MinRecoveryRate,
		})
	}
	return objectives
}

// paretoAcceptance is the gate's NSGA-II/Pareto acceptance rule, replacing the
// scalar per-threshold checks the gate used to run. The evidence and the
// configured thresholds become two points in the same multi-objective space:
//
//  1. If the threshold reference point Pareto-dominates the evidence — worse
//     or equal on every objective and strictly worse on at least one — the
//     tree is rejected outright. Nothing is being traded, the tree is simply
//     worse.
//  2. Otherwise the evidence is non-dominated, so it is on the front and a
//     genuine trade-off is on the table (e.g. below the success-rate floor but
//     far above the recovery-rate floor). It is accepted, but only within
//     MaxObjectiveRegression: non-domination on its own would let an arbitrary
//     collapse on one axis be bought with a win on another, which is not a
//     trade-off a deployment gate should sign off on.
func paretoAcceptance(agentName, treeName string, evidence engine.SLOSnapshot, config ValidationGateConfig) error {
	objectives := gateObjectives(evidence, config)

	candidate := evolution.NewMultiFitness()
	thresholds := evolution.NewMultiFitness()
	for _, obj := range objectives {
		candidate.Set(obj.dim, obj.got)
		thresholds.Set(obj.dim, obj.want)
	}

	if !evolution.ParetoAccepts(candidate, []evolution.MultiFitness{thresholds}) {
		return fmt.Errorf("validation gate REJECTED %s/%s: thresholds %s dominate evidence %s — no objective improved",
			agentName, treeName, thresholds, candidate)
	}

	for _, obj := range objectives {
		if shortfall := obj.want - obj.got; shortfall > config.MaxObjectiveRegression {
			return fmt.Errorf("validation gate REJECTED %s/%s: %s %.2f is %.2f below threshold %.2f, beyond the %.2f trade-off allowance",
				agentName, treeName, obj.dim, obj.got, shortfall, obj.want, config.MaxObjectiveRegression)
		}
	}
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
