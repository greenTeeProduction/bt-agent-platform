package main

import (
	"fmt"
	"os"
	"time"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
)

// buildGardenerConfig constructs the production gardener.Config, wiring all
// safety components: Gate, SnapshotDir, CrisisDetector, and the SLO evidence
// file the validation gate reads (B1).
//
// Parameters are split out so the function is testable without touching the
// real home directory.
func buildGardenerConfig(refDir, metricsDir, snapDir, sloEvidencePath string) (gardener.Config, error) {
	if err := os.MkdirAll(snapDir, 0700); err != nil {
		return gardener.Config{}, fmt.Errorf("create snapshot dir %q: %w", snapDir, err)
	}

	refStore, err := evolution.NewStore(refDir)
	if err != nil {
		return gardener.Config{}, fmt.Errorf("open reflection store: %w", err)
	}

	registry := gardener.NewRegistry(refDir)

	metricsTracker, err := gardener.NewMetricsTracker(metricsDir)
	if err != nil {
		return gardener.Config{}, fmt.Errorf("open metrics tracker: %w", err)
	}

	tt, err := evaluator.NewTranspositionTable(refDir, 2000)
	if err != nil {
		return gardener.Config{}, fmt.Errorf("open transposition table: %w", err)
	}

	validationGate := gardener.DefaultValidationGateConfig()
	validationGate.EvidencePath = sloEvidencePath

	return gardener.Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		TT:             tt,
		Interval:       5 * time.Minute,
		MaxMutations:   2,
		UseRealLLM:     false,
		ValidationGate: validationGate,

		// Safety components — wired by A1 remediation
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
	}, nil
}
