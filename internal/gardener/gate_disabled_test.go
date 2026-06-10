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
	// Record the fail count before evolveTree — Validate must NOT be called while disabled.
	// An accepted Validate resets failCount; a rejected one increments it.
	// Unchanged count proves no Validate call occurred.
	failCountBefore := gate.FailCount()

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
		t.Fatal("expected builtin trees in registry")
	}
	metrics := g.evolveTree(entries[0])
	// Assertion 1: it returns without panicking and has the tree name set.
	if metrics.TreeName == "" {
		t.Errorf("expected TreeName to be set in returned metrics")
	}
	// Assertion 2: failCount unchanged proves Validate was NOT called while disabled.
	if got := gate.FailCount(); got != failCountBefore {
		t.Errorf("gate.FailCount() changed during disabled run: before=%d after=%d — Validate must not be called while disabled",
			failCountBefore, got)
	}
}

// TestEvolveTreeV2_GateIsDisabled_Respected ensures that when a QualityGate is
// disabled, evolveTreeV2 skips the per-candidate gate check and does not call
// Validate — mirroring the V1 guarantee above.
func TestEvolveTreeV2_GateIsDisabled_Respected(t *testing.T) {
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
	gate.ConsecutiveFails = 1
	gate.Validate(50, 0.01) // force disable

	if !gate.IsDisabled() {
		t.Fatal("precondition: gate should be disabled after threshold exceeded")
	}
	failCountBefore := gate.FailCount()

	entries := registry.List()
	if len(entries) == 0 {
		t.Fatal("expected builtin trees in registry")
	}

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

	v2cfg := EvolveV2Config{
		MAPElitesEnabled:   false,
		ParetoEnabled:      false,
		IslandEnabled:      false,
		EnsembleEnabled:    false,
		RichContextEnabled: false,
		BlocksEnabled:      false,
		MetaPromptEnabled:  false,
		UseRealLLM:         false,
	}

	metrics := g.evolveTreeV2(entries[0], v2cfg)
	// Assertion 1: returns without panic with tree name set.
	if metrics.TreeName == "" {
		t.Errorf("expected TreeName to be set in returned metrics")
	}
	// Assertion 2: failCount unchanged proves Validate was NOT called while disabled.
	if got := gate.FailCount(); got != failCountBefore {
		t.Errorf("gate.FailCount() changed during disabled V2 run: before=%d after=%d — Validate must not be called while disabled",
			failCountBefore, got)
	}
}
