package gardener

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func newWiringFixture(t *testing.T, treeName string) (Config, TreeEntry, *evolution.QualityGate) {
	t.Helper()
	snapDir := t.TempDir()
	refDir := t.TempDir()

	metricsTracker, err := NewMetricsTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	refStore, err := evolution.NewStore(refDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "wiring test", Tree: tree, FilePath: refDir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	gate := evolution.NewQualityGate(snapDir)
	gate.ConsecutiveFails = 1
	// Disable ONLY this tree via its per-tree streak — the global key stays clean.
	gate.ValidateFor(treeName, 50, 0.01)
	if !gate.IsDisabledFor(treeName) {
		t.Fatal("precondition: tree must be disabled via per-tree streak")
	}
	if gate.IsDisabled() {
		t.Fatal("precondition: global kill switch must NOT be tripped")
	}

	vg := DefaultValidationGateConfig()
	vg.AllowUnverified = true
	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		Gate:           gate,
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: vg,
		MaxMutations:   1,
	}
	return cfg, registry.List()[0], gate
}

// evolveTreeV2 must honor a per-tree disable: mutations skipped for THIS tree
// even though the global kill switch is untouched.
func TestEvolveTreeV2_PerTreeDisabledFailsClosed(t *testing.T) {
	cfg, entry, _ := newWiringFixture(t, "wiring_v2_tree")
	g := NewGardener(cfg)

	metrics := g.evolveTreeV2(entry, EvolveV2Config{})
	if metrics.Mutations != 0 {
		t.Errorf("expected 0 mutations for per-tree-disabled tree, got %d", metrics.Mutations)
	}
}
