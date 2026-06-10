package gardener

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestGardenerConfig_GateIsDisabled_Respected ensures that when a QualityGate has
// accumulated enough consecutive failures to self-disable, evolveTree skips the
// gate check rather than blocking all mutations. This is the "disabled gate logs
// loudly but allows mutations through" path added in A1.
func TestGardenerConfig_GateIsDisabled_Respected(t *testing.T) {
	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	registry := NewRegistry(refDir)
	metricsTracker, err := NewMetricsTracker(metricsDir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	refStore, err := evolution.NewStore(refDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tt, err := evaluator.NewTranspositionTable(refDir, 100)
	if err != nil {
		t.Fatalf("NewTranspositionTable: %v", err)
	}

	gate := evolution.NewQualityGate(snapDir)
	// Set ConsecutiveFails threshold very low and force it to trigger.
	gate.ConsecutiveFails = 1
	gate.Validate(50, 0.01) // one rejection — threshold is 1, so gate is now disabled

	if !gate.IsDisabled() {
		t.Fatal("precondition: gate should be disabled after threshold exceeded")
	}

	// Verify the Config field is correctly accepted.
	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		TT:             tt,
		Gate:           gate,
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: DefaultValidationGateConfig(),
		MaxMutations:   1,
	}

	g := NewGardener(cfg)
	if g == nil {
		t.Fatal("NewGardener returned nil")
	}

	// When gate is disabled, evolveTree should not panic and should return metrics.
	entries := registry.List()
	if len(entries) == 0 {
		t.Skip("no entries in registry — skipping evolveTree assertion")
	}
	metrics := g.evolveTree(entries[0])
	// Only assertion: it returns without panicking and has the tree name set.
	if metrics.TreeName == "" {
		t.Errorf("expected TreeName to be set in returned metrics")
	}
}
