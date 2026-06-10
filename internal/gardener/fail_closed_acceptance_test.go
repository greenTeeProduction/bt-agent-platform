package gardener

import (
	"os"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// runFailClosedArm runs evolveTreeV2 on a single seeded tree with the given
// ValidationGate config and reports whether a tree file was persisted.
func runFailClosedArm(t *testing.T, vgCfg ValidationGateConfig) (CycleMetrics, bool) {
	t.Helper()
	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

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

	const treeName = "fail_closed_acceptance"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	filePath := refDir + "/tree-" + treeName + ".json"
	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "fail-closed acceptance", Tree: tree, FilePath: filePath, Active: true},
	}
	registry.mu.Unlock()

	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		TT:             tt,
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: vgCfg,
		MaxMutations:   1,
	}
	g := NewGardener(cfg)

	v2cfg := EvolveV2Config{UseRealLLM: false}
	metrics := g.evolveTreeV2(registry.List()[0], v2cfg)

	_, statErr := os.Stat(filePath)
	return metrics, statErr == nil
}

// TestAcceptance_FailClosed_NoMutationsPersisted is the A2 acceptance test:
// with zero SLO evidence (the gardener process never executes trees, so the
// in-process SLO map is empty), the fail-closed ValidationGate must reject
// deployment and no tree file may be persisted — even when the mutation
// pipeline produces candidates the QualityGate accepts.
//
// The control arm proves non-vacuity: the identical setup with the
// ValidationGate disabled persists a tree file, so the fail-closed gate is
// the only thing standing between an unverified mutation and disk.
func TestAcceptance_FailClosed_NoMutationsPersisted(t *testing.T) {
	// Control arm: ValidationGate off — mutations must reach disk.
	ctrlMetrics, ctrlPersisted := runFailClosedArm(t, ValidationGateConfig{Enabled: false})
	if !ctrlPersisted {
		t.Fatalf("control arm did not persist a tree file (metrics=%+v) — "+
			"mutation pipeline produced no applied candidates, so the fail-closed "+
			"assertion below would be vacuous; fix the seeding", ctrlMetrics)
	}

	// Gate arm: default config (enabled), no SLO metrics — nothing may persist.
	gateMetrics, gatePersisted := runFailClosedArm(t, DefaultValidationGateConfig())
	if gatePersisted {
		t.Errorf("tree file persisted despite fail-closed ValidationGate with no SLO evidence (metrics=%+v)", gateMetrics)
	}
	if gateMetrics.Mutations != 0 {
		t.Errorf("expected 0 applied mutations under fail-closed gate, got %d", gateMetrics.Mutations)
	}
}
