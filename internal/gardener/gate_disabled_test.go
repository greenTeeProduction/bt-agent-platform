package gardener

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// gateDisabledTestTree returns a tree that reliably produces high-score mutation
// candidates (PreGate without HasClearTask triggers the 0.92-score add_before
// candidate when paired with failure records), so these tests prove mutations
// WOULD have been applied if the disabled gate did not fail closed.
func gateDisabledTestTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate"},
			{Type: "ChainAction", Name: "ResearchAgent", Metadata: map[string]any{"max_iterations": float64(3)}},
		},
	}
}

// seedFailureRecords saves failure-heavy reflection records so OrderMutations
// generates candidates for the tree under test.
func seedFailureRecords(t *testing.T, refStore *evolution.Store, treeName string) {
	t.Helper()
	for i, outcome := range []evolution.Outcome{evolution.Failure, evolution.Failure, evolution.Failure, evolution.Success} {
		if err := refStore.Save(&evolution.Record{
			TaskID:        "gate-disabled-test-" + string(rune('a'+i)),
			TreeName:      treeName,
			Task:          "research production readiness",
			Plan:          "plan using ResearchAgent",
			Outcome:       outcome,
			DurationMs:    1000,
			WhatToImprove: []string{"ResearchAgent needs verified outputs"},
		}); err != nil {
			t.Fatalf("save reflection: %v", err)
		}
	}
}

func marshalTree(t *testing.T, tree *evolution.SerializableNode) []byte {
	t.Helper()
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	return data
}

// TestEvolveTreeV2_GateIsDisabled_Respected ensures that when a QualityGate is
// disabled, evolveTreeV2 fails closed: no Validate calls and zero mutations
// applied for the tree (A2 fail-closed semantics — evolution paused until
// process restart).
func TestEvolveTreeV2_GateIsDisabled_Respected(t *testing.T) {
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

	const treeName = "gate_disabled_v2"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "gate-disabled test", Tree: tree, FilePath: refDir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	gate := evolution.NewQualityGate(snapDir)
	gate.ConsecutiveFails = 1
	gate.Validate(50, 0.01) // force disable

	if !gate.IsDisabled() {
		t.Fatal("precondition: gate should be disabled after threshold exceeded")
	}
	failCountBefore := gate.FailCount()

	entries := registry.List()
	if len(entries) == 0 {
		t.Fatal("expected tree in registry")
	}
	treeBefore := marshalTree(t, entries[0].Tree)

	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		Gate:           gate,
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: DefaultValidationGateConfig(),
		MaxMutations:   1,
	}
	g := NewGardener(cfg)

	v2cfg := EvolveV2Config{
		BlocksEnabled: false,
		UseRealLLM:    false,
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
	// Assertion 3 (A2 fail-closed): zero mutations applied — the candidate loop
	// must be skipped entirely while the gate is disabled.
	treeAfter := marshalTree(t, entries[0].Tree)
	if !bytes.Equal(treeBefore, treeAfter) {
		t.Errorf("tree was mutated while quality gate disabled — disabled must mean fail-closed (skip mutations), not ungated\nbefore: %s\nafter:  %s",
			treeBefore, treeAfter)
	}
	if metrics.Mutations != 0 {
		t.Errorf("expected 0 applied mutations while gate disabled, got %d", metrics.Mutations)
	}
}
