package gardener

import (
	"bytes"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestEvolveTreeV2_ValidationGateRejection_RestoresTree is the A3 failing-first
// test: when the ValidationGate rejects (fail-closed, no SLO evidence), the
// in-memory tree must be identical to the pre-cycle snapshot, and CycleMetrics
// must report no improvement (NewFitness == BaseFitness, Mutations == 0).
func TestEvolveTreeV2_ValidationGateRejection_RestoresTree(t *testing.T) {
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

	const treeName = "gate_rejection_rollback"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	// Capture original serialized form before any evolution.
	treeBefore := marshalTree(t, tree)

	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "gate rejection rollback test", Tree: tree, FilePath: refDir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	// Default ValidationGate: enabled, no SLO evidence → always rejects (fail-closed).
	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		TT:             tt,
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: DefaultValidationGateConfig(), // enabled, fail-closed
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

	entries := registry.List()
	if len(entries) == 0 {
		t.Fatal("expected tree in registry")
	}

	metrics := g.evolveTreeV2(entries[0], v2cfg)

	// Requirement 1a: tree must be byte-identical to its pre-cycle state.
	treeAfter := marshalTree(t, entries[0].Tree)
	if !bytes.Equal(treeBefore, treeAfter) {
		t.Errorf("tree was mutated in-memory after ValidationGate rejection — baseline leak\nbefore: %s\nafter:  %s",
			treeBefore, treeAfter)
	}

	// Requirement 1b: CycleMetrics must NOT report improvement.
	if metrics.Mutations != 0 {
		t.Errorf("expected Mutations==0 after ValidationGate rejection, got %d", metrics.Mutations)
	}
	if metrics.Improved {
		t.Errorf("expected Improved==false after ValidationGate rejection, got true")
	}
	if metrics.NewFitness > metrics.BaseFitness+0.0001 {
		t.Errorf("expected NewFitness==BaseFitness after rejection, got base=%.4f new=%.4f",
			metrics.BaseFitness, metrics.NewFitness)
	}
}

// TestEvolveTreeV2_QualityGateRejection_TreeUnchanged pins that candidates
// rejected by the per-candidate QualityGate do NOT leave partial mutations on
// the shared tree. The clone-and-prescore pattern in the candidate loop operates
// on candidateTree (not tree), so the live tree is never touched for rejected
// candidates — this test asserts that invariant.
func TestEvolveTreeV2_QualityGateRejection_TreeUnchanged(t *testing.T) {
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

	const treeName = "quality_gate_per_candidate"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	treeBefore := marshalTree(t, tree)

	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "quality gate per-candidate test", Tree: tree, FilePath: refDir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	// Use a QualityGate that is pre-disabled so every candidate the loop sees
	// gets rejected. This exercises the reject-and-continue path without allowing
	// any mutation to reach the live tree. ValidationGate is disabled so we
	// isolate the QualityGate-rejection path.
	gate := evolution.NewQualityGate(snapDir)
	gate.ConsecutiveFails = 1
	gate.Validate(50, 0.01) // force-disable after 1 rejection
	if !gate.IsDisabled() {
		t.Fatal("precondition: gate should be disabled")
	}

	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		TT:             tt,
		Gate:           gate,
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: ValidationGateConfig{Enabled: false}, // disable so we don't compound with ValidationGate
		MaxMutations:   3,
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

	entries := registry.List()
	metrics := g.evolveTreeV2(entries[0], v2cfg)

	// Pin: when the QualityGate is disabled (fail-closed), the candidate loop is
	// entirely skipped — the live tree must remain byte-identical.
	treeAfter := marshalTree(t, entries[0].Tree)
	if !bytes.Equal(treeBefore, treeAfter) {
		t.Errorf("tree was mutated while QualityGate was disabled — per-candidate rejection must not leave partial mutations\nbefore: %s\nafter:  %s",
			treeBefore, treeAfter)
	}
	if metrics.Mutations != 0 {
		t.Errorf("expected 0 mutations applied when QualityGate disabled, got %d", metrics.Mutations)
	}
}
