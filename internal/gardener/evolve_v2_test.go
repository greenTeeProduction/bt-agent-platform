package gardener

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/util"
)

// ============================================================================
// Helper function tests (evolve_v2.go)
// ============================================================================

func TestExportSLOMetrics_CreatesMissingParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "slo-metrics.json")
	sloData := map[string]float64{"agentA": 0.97}

	if err := exportSLOMetrics(path, sloData); err != nil {
		t.Fatalf("exportSLOMetrics returned error: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist at %q: %v", path, err)
	}
}

func TestExportSLOMetrics_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "slo-metrics.json")
	sloData := map[string]float64{"agentA": 0.97}

	if err := exportSLOMetrics(path, sloData); err != nil {
		t.Fatalf("exportSLOMetrics returned error: %v", err)
	}

	var got map[string]float64
	if err := util.LoadJSON(path, &got); err != nil {
		t.Fatalf("util.LoadJSON returned error: %v", err)
	}

	if !reflect.DeepEqual(got, sloData) {
		t.Errorf("round-tripped data = %v, want %v", got, sloData)
	}
}

func TestCloneTreeForGardener_Nil(t *testing.T) {
	got := cloneTreeForGardener(nil)
	if got != nil {
		t.Error("cloneTreeForGardener(nil) should return nil")
	}
}

func TestCloneTreeForGardener_Basic(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		MaxRetries: 3, TimeoutMs: 5000,
		Metadata: map[string]any{"key": "value"},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step1"},
			{Type: "Condition", Name: "IsReady"},
		},
	}
	clone := cloneTreeForGardener(tree)
	if clone == nil {
		t.Fatal("clone should not be nil")
	}
	if clone.Type != "Sequence" || clone.Name != "Root" {
		t.Errorf("type/name mismatch: %s/%s", clone.Type, clone.Name)
	}
	if clone.MaxRetries != 3 || clone.TimeoutMs != 5000 {
		t.Errorf("metadata fields mismatch: retries=%d, timeout=%d", clone.MaxRetries, clone.TimeoutMs)
	}
	if clone.Metadata["key"] != "value" {
		t.Errorf("Metadata not copied: %v", clone.Metadata)
	}
	if len(clone.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(clone.Children))
	}
	if clone.Children[0].Name != "Step1" || clone.Children[1].Name != "IsReady" {
		t.Errorf("child names mismatch: %s/%s", clone.Children[0].Name, clone.Children[1].Name)
	}
	// Verify it's a deep copy (modifying clone doesn't affect original)
	clone.Children[0].Name = "Modified"
	if tree.Children[0].Name != "Step1" {
		t.Error("clone is not a deep copy — modifying clone affected original")
	}
	clone.Metadata["key"] = "modified"
	if tree.Metadata["key"] != "value" {
		t.Error("Metadata wasn't deep-copied")
	}
}

func TestCloneTreeForGardener_DoubleNested(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "Router",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "DeepAction"},
				},
			},
		},
	}
	clone := cloneTreeForGardener(tree)
	if len(clone.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(clone.Children))
	}
	if clone.Children[0].Type != "Selector" || clone.Children[0].Name != "Router" {
		t.Errorf("nested child mismatch: %s/%s", clone.Children[0].Type, clone.Children[0].Name)
	}
	if len(clone.Children[0].Children) != 1 {
		t.Fatalf("expected 1 grandchild, got %d", len(clone.Children[0].Children))
	}
	if clone.Children[0].Children[0].Name != "DeepAction" {
		t.Errorf("grandchild name mismatch: %s", clone.Children[0].Children[0].Name)
	}
}

func TestCloneTreeForGardener_NilMetadata(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Metadata: nil,
	}
	clone := cloneTreeForGardener(tree)
	if clone == nil {
		t.Fatal("clone should not be nil")
	}
	if clone.Metadata != nil {
		t.Error("nil Metadata should stay nil in clone")
	}
}

// ============================================================================
// DefaultEvolveV2Config tests
// ============================================================================

func TestDefaultEvolveV2Config(t *testing.T) {
	cfg := DefaultEvolveV2Config()
	if !cfg.BlocksEnabled {
		t.Error("BlocksEnabled should default to true")
	}
	if cfg.UseRealLLM {
		t.Error("UseRealLLM should default to false")
	}
	if cfg.CascadeCfg.QuickThreshold != evaluator.DefaultCascadeConfig().QuickThreshold {
		t.Errorf("CascadeCfg.QuickThreshold mismatch")
	}
}

// ============================================================================
// RunCycleV2 integration test (mock LLM, no Ollama)
// ============================================================================

func TestRunCycleV2_Basic(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycleV2(DefaultEvolveV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.TreeName != "default" {
		t.Errorf("TreeName = %q, want 'default'", r.TreeName)
	}
	if r.NodesBefore <= 0 {
		t.Errorf("NodesBefore should be > 0, got %d", r.NodesBefore)
	}
	if r.NodesAfter <= 0 {
		t.Errorf("NodesAfter should be > 0, got %d", r.NodesAfter)
	}
	// Metrics should have been saved
	if mt.CyclesForTree("default") != 1 {
		t.Errorf("CyclesForTree(default) = %d, want 1", mt.CyclesForTree("default"))
	}
}

func TestRunCycleV2_MultipleTrees(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
		{Name: "godev", Description: "go dev", Tree: simpleTree, FilePath: dir + "/tree-godev.json", Active: true},
		{Name: "custom", Description: "custom", Tree: simpleTree, FilePath: dir + "/tree-custom.json", Active: false},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycleV2(DefaultEvolveV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}
	// Should process 2 active trees (default, godev), skip custom
	if len(results) != 2 {
		t.Fatalf("expected 2 results (2 active trees), got %d", len(results))
	}
	// Results should be sorted alphabetically: "default" then "godev"
	if results[0].TreeName != "default" {
		t.Errorf("first result should be 'default', got %q", results[0].TreeName)
	}
	if results[1].TreeName != "godev" {
		t.Errorf("second result should be 'godev', got %q", results[1].TreeName)
	}
}

func TestRunCycleV2_EmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{} // empty
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycleV2(DefaultEvolveV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2 with empty registry: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty registry, got %d", len(results))
	}
}

func TestRunCycleV2_NilTree(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "nil_tree", Description: "nil tree", Tree: nil, FilePath: dir + "/nil.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	results, err := g.RunCycleV2(DefaultEvolveV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2 with nil tree: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	r := results[0]
	if r.TreeName != "nil_tree" {
		t.Errorf("TreeName = %q, want 'nil_tree'", r.TreeName)
	}
	if r.Improved {
		t.Error("nil tree should not be 'improved'")
	}
}

func TestEvolveTreeV2_MetricsSaved(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	_, err := g.RunCycleV2(DefaultEvolveV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}

	// Verify metrics file was saved
	metricsPath := filepath.Join(dir, "gardener-metrics.json")
	if _, err := os.Stat(metricsPath); os.IsNotExist(err) {
		t.Error("gardener-metrics.json was not saved after RunCycleV2")
	}
}

// TestRunCycleV2_MetricsSaveFailurePropagates pins Q3 Reliability milestone 3:
// RunCycleV2 currently discards every MetricsTracker.Save() error behind
// `_ = g.cfg.MetricsTracker.Save()` (evolve_v2.go lines 633 and 654), so a
// corrupted-write metrics snapshot is silently treated as successfully
// persisted. Pointing MetricsTracker at a path inside a directory that does
// not exist makes every Save() call fail at write time; RunCycleV2 must
// surface that failure through its existing error return instead of
// swallowing it.
func TestRunCycleV2_MetricsSaveFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
	}
	reg.mu.Unlock()

	// mt.path points inside a directory that is never created, so every
	// os.WriteFile inside MetricsTracker.Save() fails.
	mt := &MetricsTracker{path: filepath.Join(dir, "missing-subdir", "gardener-metrics.json")}

	cfg := Config{
		Registry:                 reg,
		MetricsTracker:           mt,
		RefStore:                 refStore,
		MaxMutations:             1,
		EvolveWithoutReflections: true,
		UseRealLLM:               false,
	}
	g := NewGardener(cfg)

	if _, err := g.RunCycleV2(DefaultEvolveV2Config()); err == nil {
		t.Fatal("RunCycleV2 returned a nil error despite every MetricsTracker.Save() call failing — the write failure is being silently discarded instead of propagated")
	}
}

// TestRunCycleV2_SLOMetricsExportFailurePropagates pins the fix for the
// "SLO metrics collection" block (evolve_v2.go, near the end of RunCycleV2):
// every error from json.MarshalIndent, os.WriteFile, and os.Rename there was
// silently discarded (`if data, err := ...; err == nil { ... }`, `_ =
// os.Rename(...)`), so a failed slo-metrics.json export was indistinguishable
// from a successful one. Pre-creating slo-metrics.json as an empty directory
// forces the block's os.Rename(tmp, sloPath) to fail — renaming a file onto
// an existing directory is rejected by the OS — while MetricsTracker.Save()
// itself succeeds cleanly. RunCycleV2 must surface that failure through its
// existing error return instead of swallowing it.
func TestRunCycleV2_SLOMetricsExportFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
	}
	reg.mu.Unlock()

	// CollectAgentSLOs only attempts an export when the evidence file is
	// non-empty, so seed it with one snapshot.
	evidencePath := writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "default", TreeName: "default", TotalCalls: 10, SuccessfulCalls: 9, FailedCalls: 1, RecoveredCalls: 1},
	})

	// slo-metrics.json sits next to MetricsTracker's file (same dir). Pre-create
	// it as an empty directory so the export's final os.Rename fails.
	sloPath := filepath.Join(dir, "slo-metrics.json")
	if err := os.Mkdir(sloPath, 0o755); err != nil {
		t.Fatalf("Mkdir slo-metrics.json: %v", err)
	}

	cfg := Config{
		Registry:                 reg,
		MetricsTracker:           mt,
		RefStore:                 refStore,
		MaxMutations:             1,
		EvolveWithoutReflections: true,
		UseRealLLM:               false,
		ValidationGate:           ValidationGateConfig{EvidencePath: evidencePath},
	}
	g := NewGardener(cfg)

	if _, err := g.RunCycleV2(DefaultEvolveV2Config()); err == nil {
		t.Fatal("RunCycleV2 returned a nil error despite the SLO-metrics export rename failing — the export failure is being silently discarded instead of propagated")
	}
}

func TestEvolveTreeV2_BloatGuard(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	// Build a massively bloated tree (> 600 nodes for godev)
	bloatedTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "BigTree",
	}
	bloatedTree.Children = make([]evolution.SerializableNode, 0)
	for range 700 {
		bloatedTree.Children = append(bloatedTree.Children, evolution.SerializableNode{
			Type: "Action", Name: "Dummy",
		})
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "godev", Description: "go dev", Tree: bloatedTree, FilePath: dir + "/tree-godev.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   2,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	// Use a config with RichContext/Ensemble disabled to avoid ensemble bugs
	v2cfg := EvolveV2Config{
		BlocksEnabled: false,
		UseRealLLM:    false,
	}
	// evolveTreeV2 should not panic with a huge tree
	_ = g.evolveTreeV2(TreeEntry{Name: "godev", Tree: bloatedTree, Active: true}, v2cfg)
}

func TestEvolveTreeV2_NoRegressionGate(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate"},
			{Type: "ChainAction", Name: "ResearchAgent", Metadata: map[string]any{"max_iterations": float64(3)}},
		},
	}
	for i, outcome := range []evolution.Outcome{evolution.Failure, evolution.Failure, evolution.Success} {
		if err := refStore.Save(&evolution.Record{
			TaskID:        "quality-gate-test-" + string(rune('a'+i)),
			TreeName:      "quality_tree",
			Task:          "research notebooklm production readiness",
			Plan:          "plan",
			Outcome:       outcome,
			DurationMs:    1000,
			WhatToImprove: []string{"ResearchAgent needs verified outputs"},
		}); err != nil {
			t.Fatalf("save reflection: %v", err)
		}
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "quality_tree", Description: "quality", Tree: tree, FilePath: dir + "/tree-quality.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{Registry: customReg, MetricsTracker: mt, RefStore: refStore, MaxMutations: 2, UseRealLLM: false}
	g := NewGardener(cfg)
	v2cfg := EvolveV2Config{
		BlocksEnabled: false,
		UseRealLLM:    false,
	}

	m := g.evolveTreeV2(TreeEntry{Name: "quality_tree", Tree: tree, FilePath: dir + "/tree-quality.json", Active: true}, v2cfg)
	if m.NewFitness+0.0001 < m.BaseFitness {
		t.Fatalf("no-regression gate failed: base %.4f new %.4f delta %.4f mutations %d rollbacks %d", m.BaseFitness, m.NewFitness, m.Delta, m.Mutations, m.Rollbacks)
	}
	if m.Delta < -0.0001 {
		t.Fatalf("expected non-negative recorded delta, got %.4f", m.Delta)
	}
}

// ============================================================================
// RunCycleV2 with config variants
// ============================================================================

func TestRunCycleV2_ConfigDisabledFeatures(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	mt, _ := NewMetricsTracker(dir)

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{
		{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
	}
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	// Run with all features disabled
	v2cfg := EvolveV2Config{
		BlocksEnabled: false,
		UseRealLLM:    false,
	}

	results, err := g.RunCycleV2(v2cfg)
	if err != nil {
		t.Fatalf("RunCycleV2 with all features disabled: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.TreeName != "default" {
		t.Errorf("TreeName = %q, want 'default'", r.TreeName)
	}
}

// ============================================================================
// ExperienceBank recording tests (evolve_v2.go)
// ============================================================================

// experienceRecordingGardener builds a gardener whose evolveTreeV2 run
// deterministically accepts exactly one fitness-improving mutation
// (gateDisabledTestTree + failure records produce the 0.92-score add_before
// candidate; ValidationGate disabled so the mutation lands).
func experienceRecordingGardener(t *testing.T, bank *evolution.ExperienceBank) (*Gardener, TreeEntry) {
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
	const treeName = "experience_recording"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "experience recording", Tree: tree, FilePath: refDir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: ValidationGateConfig{Enabled: false},
		MaxMutations:   1,
		ExperienceBank: bank,
	}
	return NewGardener(cfg), registry.List()[0]
}

// TestEvolveTreeV2_RecordsAcceptedMutationExperience pins milestone 1 of the
// experience-grounded gardener: every mutation accepted by evolveTreeV2 must be
// recorded in the configured evolution.ExperienceBank with the mutation op,
// target node, tree type, and the measured composite-fitness delta.
func TestEvolveTreeV2_RecordsAcceptedMutationExperience(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	g, entry := experienceRecordingGardener(t, bank)

	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	// Non-vacuity: this setup must accept an improving mutation, otherwise the
	// recording assertions below prove nothing.
	if m.Mutations < 1 {
		t.Fatalf("setup produced no accepted mutations (metrics=%+v) — fix the seeding", m)
	}
	if m.Delta <= 0 {
		t.Fatalf("setup produced no fitness improvement (delta=%.6f) — recording assertions would be vacuous", m.Delta)
	}

	if bank.Count() != m.Mutations {
		t.Fatalf("ExperienceBank has %d entries, want %d (one per accepted mutation)", bank.Count(), m.Mutations)
	}
	e := bank.Entries[0]
	if e.MutationOp == "" {
		t.Error("recorded entry is missing MutationOp")
	}
	if e.TargetNode == "" {
		t.Error("recorded entry is missing TargetNode")
	}
	if e.TreeType == "" {
		t.Error("recorded entry is missing TreeType")
	}
	if e.FitnessDelta <= 0 {
		t.Errorf("recorded FitnessDelta = %.6f, want > 0", e.FitnessDelta)
	}
	// With MaxMutations=1 the per-candidate delta (candidateFitness.Composite -
	// currentFitness.Composite) equals the cycle delta.
	if diff := e.FitnessDelta - m.Delta; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("recorded FitnessDelta = %.6f, want measured cycle delta %.6f", e.FitnessDelta, m.Delta)
	}
}

// TestEvolveTreeV2_RecordsFailingTaskContext pins Q2 Evolvability milestone
// 2/3: an accepted mutation's ExperienceBank entry must carry the tree's most
// recent failing reflection's task text as failing_task= context, so ADR-109's
// read-side retrieval-by-failure-semantics has signal to match against.
// experienceRecordingGardener already seeds failing reflection records
// (seedFailureRecords) for this tree before the cycle runs.
func TestEvolveTreeV2_RecordsFailingTaskContext(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	g, entry := experienceRecordingGardener(t, bank)

	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	if m.Mutations < 1 {
		t.Fatalf("setup produced no accepted mutations (metrics=%+v) — fix the seeding", m)
	}
	if bank.Count() == 0 {
		t.Fatalf("ExperienceBank has no entries to inspect")
	}
	e := bank.Entries[0]
	if !strings.Contains(e.Context, "failing_task=") {
		t.Errorf("recorded entry Context = %q, want it to carry failing_task= from the tree's most recent failing reflection", e.Context)
	}
}

// ============================================================================
// Pre-mutation snapshot durability (Q2 Evolvability milestone 1)
// ============================================================================

// TestEvolveTreeV2_SnapshotsTreeBeforeMutation pins Q2 Evolvability milestone
// 1/3: evolveTreeV2 must call evolution.SnapshotTree for the tree's pre-cycle
// state before the mutation loop begins, so a process crash mid-cycle still
// has a durable on-disk snapshot to recover from — not just the in-memory
// originalTree clone. g.cfg.SnapshotDir is already wired into production
// (cmd/bt-gardener/config.go) but had zero readers before this change.
func TestEvolveTreeV2_SnapshotsTreeBeforeMutation(t *testing.T) {
	g, entry := experienceRecordingGardener(t, nil)

	g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	snapshotted, err := evolution.RestoreTree(entry.Name, g.cfg.SnapshotDir)
	if err != nil {
		t.Fatalf("expected pre-mutation snapshot for %s in %s, got error: %v", entry.Name, g.cfg.SnapshotDir, err)
	}
	if snapshotted.Name != entry.Tree.Name {
		t.Errorf("snapshot root Name = %q, want %q (must capture the pre-cycle tree)", snapshotted.Name, entry.Tree.Name)
	}
}

// TestEvolveTreeV2_NilExperienceBankIsNoOp pins the degradation contract: a nil
// bank must leave evolveTreeV2 behaving exactly as today — mutations still
// accepted, no panic.
func TestEvolveTreeV2_NilExperienceBankIsNoOp(t *testing.T) {
	g, entry := experienceRecordingGardener(t, nil)

	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	if m.Mutations < 1 {
		t.Fatalf("nil bank changed behavior: expected accepted mutations, got metrics=%+v", m)
	}
	if m.Delta <= 0 {
		t.Fatalf("nil bank changed behavior: expected fitness improvement, got delta=%.6f", m.Delta)
	}
}

// ============================================================================
// Experience-biased candidate ordering tests (evolve_v2.go) — milestone 2
// ============================================================================

// seedExperience records one high-quality experience entry (delta 0.2 → quality
// score 1.0 without an LLM) for the given op/target and returns its ID.
func seedExperience(t *testing.T, bank *evolution.ExperienceBank, tree *evolution.SerializableNode, op, target string) string {
	t.Helper()
	if err := bank.AddFromMutation(tree, evolution.MutationOp{Operation: op, Target: target}, 0.0, 0.2, nil); err != nil {
		t.Fatalf("AddFromMutation(%s/%s): %v", op, target, err)
	}
	return bank.Entries[len(bank.Entries)-1].ID
}

func experienceReuseCount(t *testing.T, bank *evolution.ExperienceBank, id string) int {
	t.Helper()
	for _, e := range bank.Entries {
		if e.ID == id {
			return e.TimesReused
		}
	}
	t.Fatalf("seeded experience entry %s disappeared from the bank", id)
	return 0
}

// TestBiasCandidatesWithExperience_SeededBankReordersCandidates pins milestone 2
// of the experience-grounded gardener: candidate ordering from
// evaluator.OrderMutations must be biased by ExperienceBank retrieval — a
// candidate whose op/target matches a high-quality past entry for the same tree
// type is boosted ahead of heuristically higher-scored non-matching candidates,
// and the matched entry's TimesReused is bumped.
func TestBiasCandidatesWithExperience_SeededBankReordersCandidates(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	tree := gateDisabledTestTree()

	matchedID := seedExperience(t, bank, tree, "add_fallback", "Router")
	unmatchedID := seedExperience(t, bank, tree, "prune_node", "UnrelatedNode")

	candidates := []evaluator.MutationCandidate{
		{Op: evolution.MutationOp{Operation: "increase_retries", Target: "ResearchAgent"}, Score: 0.55, Reason: "heuristic top"},
		{Op: evolution.MutationOp{Operation: "add_fallback", Target: "Router"}, Score: 0.50, Reason: "matches seeded experience"},
		{Op: evolution.MutationOp{Operation: "reorder_children", Target: "Root"}, Score: 0.48, Reason: "heuristic tail"},
	}

	got := biasCandidatesWithExperience(bank, tree, candidates, "")

	if len(got) != len(candidates) {
		t.Fatalf("biasing changed candidate count: got %d, want %d", len(got), len(candidates))
	}
	if got[0].Op.Operation != "add_fallback" || got[0].Op.Target != "Router" {
		t.Fatalf("seeded bank did not boost matching candidate to the front: got %s/%s first",
			got[0].Op.Operation, got[0].Op.Target)
	}
	if got[0].Score <= 0.50 {
		t.Errorf("matching candidate score not boosted: got %.4f, want > 0.50", got[0].Score)
	}

	// Non-matching candidates keep their relative heuristic order.
	idxRetries, idxReorder := -1, -1
	for i, c := range got {
		switch c.Op.Operation {
		case "increase_retries":
			idxRetries = i
		case "reorder_children":
			idxReorder = i
		}
	}
	if idxRetries == -1 || idxReorder == -1 {
		t.Fatalf("biasing dropped non-matching candidates: %+v", got)
	}
	if idxRetries > idxReorder {
		t.Errorf("relative order of non-matching candidates not preserved: increase_retries at %d, reorder_children at %d", idxRetries, idxReorder)
	}

	if n := experienceReuseCount(t, bank, matchedID); n < 1 {
		t.Errorf("matched experience entry TimesReused = %d, want >= 1", n)
	}
	if n := experienceReuseCount(t, bank, unmatchedID); n != 0 {
		t.Errorf("unmatched experience entry TimesReused = %d, want 0", n)
	}
}

// TestBiasCandidatesWithExperience_NilOrEmptyBankKeepsOrder pins the degradation
// contract: without usable experience the heuristic ordering is untouched.
func TestBiasCandidatesWithExperience_NilOrEmptyBankKeepsOrder(t *testing.T) {
	tree := gateDisabledTestTree()
	candidates := []evaluator.MutationCandidate{
		{Op: evolution.MutationOp{Operation: "increase_retries", Target: "ResearchAgent"}, Score: 0.55},
		{Op: evolution.MutationOp{Operation: "add_fallback", Target: "Router"}, Score: 0.50},
	}

	emptyBank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	for name, bank := range map[string]*evolution.ExperienceBank{"nil": nil, "empty": emptyBank} {
		got := biasCandidatesWithExperience(bank, tree, candidates, "")
		if len(got) != len(candidates) {
			t.Fatalf("%s bank changed candidate count: got %d, want %d", name, len(got), len(candidates))
		}
		for i := range got {
			if got[i].Op.Operation != candidates[i].Op.Operation || got[i].Score != candidates[i].Score {
				t.Errorf("%s bank altered candidate %d: got %s/%.4f, want %s/%.4f",
					name, i, got[i].Op.Operation, got[i].Score, candidates[i].Op.Operation, candidates[i].Score)
			}
		}
	}
}

// TestEvolveTreeV2_MarksMatchingExperienceReused pins the wiring: evolveTreeV2
// itself must run its OrderMutations candidates through the experience bias, so
// a seeded entry matching the deterministic top candidate (add_before on
// PreGate for the failure-heavy test tree) gets its TimesReused bumped during a
// real evolution cycle.
func TestEvolveTreeV2_MarksMatchingExperienceReused(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	seededID := seedExperience(t, bank, gateDisabledTestTree(), "add_before", "PreGate")

	g, entry := experienceRecordingGardener(t, bank)
	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	// Non-vacuity: the run must actually generate and accept candidates.
	if m.Mutations < 1 {
		t.Fatalf("setup produced no accepted mutations (metrics=%+v) — fix the seeding", m)
	}
	if n := experienceReuseCount(t, bank, seededID); n < 1 {
		t.Fatalf("evolveTreeV2 did not mark the matching experience entry reused: TimesReused = %d, want >= 1", n)
	}
}

// seedExperienceWithFailureContext records one high-quality experience entry
// like seedExperience, but also appends failureContext as
// AddFromMutation's optional failing-task text, so the resulting entry's
// Context carries "; failing_task=<failureContext>" — the ADR-109 write-side
// signal milestone 3 conditions retrieval on.
func seedExperienceWithFailureContext(t *testing.T, bank *evolution.ExperienceBank, tree *evolution.SerializableNode, op, target, failureContext string) string {
	t.Helper()
	if err := bank.AddFromMutation(tree, evolution.MutationOp{Operation: op, Target: target}, 0.0, 0.2, nil, failureContext); err != nil {
		t.Fatalf("AddFromMutation(%s/%s): %v", op, target, err)
	}
	return bank.Entries[len(bank.Entries)-1].ID
}

// TestBiasCandidatesWithExperience_QueryPathSurfacesOffTreeTypeEntry pins Q2
// Evolvability milestone 3/3: biasCandidatesWithExperience must condition
// retrieval on the milestone-2 lastFailureTask signal — a non-empty query
// calls bank.Retrieve(query, experienceBiasTopK), which is not filtered by
// tree type, instead of the tree-type-only evolution.RetrieveExperienceHints
// path. An off-tree-type entry whose Context carries the matching
// failing_task= text must only be surfaced (boosted + marked reused) when the
// query is supplied; the empty-query fallback must behave exactly like
// today's tree-type-filtered retrieval and leave it untouched.
func TestBiasCandidatesWithExperience_QueryPathSurfacesOffTreeTypeEntry(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}

	tree := gateDisabledTestTree()                                                      // extractTreeType(tree) == "Root"
	offTypeTree := &evolution.SerializableNode{Type: "Sequence", Name: "godev_variant"} // extractTreeType == "GoDev"

	const failingTask = "research production readiness"
	offTypeID := seedExperienceWithFailureContext(t, bank, offTypeTree, "add_fallback", "Router", failingTask)

	candidates := []evaluator.MutationCandidate{
		{Op: evolution.MutationOp{Operation: "increase_retries", Target: "ResearchAgent"}, Score: 0.55, Reason: "heuristic top"},
		{Op: evolution.MutationOp{Operation: "add_fallback", Target: "Router"}, Score: 0.50, Reason: "matches off-tree-type experience"},
	}

	// Empty query (no failing-task signal): falls back to today's
	// tree-type-only RetrieveExperienceHints, which excludes the off-tree-type
	// entry — ordering and reuse count stay untouched.
	unbiased := biasCandidatesWithExperience(bank, tree, candidates, "")
	if unbiased[0].Op.Operation != "increase_retries" || unbiased[1].Op.Operation != "add_fallback" {
		t.Fatalf("empty query changed candidate ordering: got %+v", unbiased)
	}
	if n := experienceReuseCount(t, bank, offTypeID); n != 0 {
		t.Fatalf("empty-query fallback marked off-tree-type entry reused: TimesReused = %d, want 0", n)
	}

	// Non-empty query built from the milestone-2 lastFailureTask signal must
	// route through bank.Retrieve(query, experienceBiasTopK), surfacing the
	// off-tree-type entry by Context match regardless of tree type.
	biased := biasCandidatesWithExperience(bank, tree, candidates, failingTask)
	if biased[0].Op.Operation != "add_fallback" || biased[0].Op.Target != "Router" {
		t.Fatalf("query path did not boost the off-tree-type matching candidate to the front: got %s/%s first",
			biased[0].Op.Operation, biased[0].Op.Target)
	}
	if biased[0].Score <= 0.50 {
		t.Errorf("query-matched candidate score not boosted: got %.4f, want > 0.50", biased[0].Score)
	}
	if n := experienceReuseCount(t, bank, offTypeID); n < 1 {
		t.Errorf("query path did not mark the off-tree-type experience entry reused: TimesReused = %d, want >= 1", n)
	}
}

// ============================================================================
// Selector-ordering production entry point (evolve_v2.go) — milestone 4
// ============================================================================

// selectorOrderingTree builds a tree with a single Selector ("Router") whose
// children start in the order [Cheap, Reliable, Fallback]. In the seeded
// telemetry Reliable has a higher success rate than Cheap, so a telemetry-driven
// reorder must promote Reliable ahead of Cheap. The AlwaysSucceed "Fallback"
// child has a perfect success rate but must still stay last, preserving Selector
// short-circuit semantics (the fallback/default-path guard).
func selectorOrderingTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "Router",
				Children: []evolution.SerializableNode{
					{Type: "Sequence", Name: "Cheap"},
					{Type: "Sequence", Name: "Reliable"},
					{Type: "AlwaysSucceed", Name: "Fallback"},
				},
			},
		},
	}
}

// seedSelectorStats writes durable Selector telemetry (the on-disk format
// produced by knowledge.RecordSelectorOutcomes via SelectorOptimizer) to path so
// that under "Router" the Reliable child (0.90) beats the Cheap child (0.20),
// and the Fallback child has a perfect (1.00) success rate that the reorder must
// NOT promote ahead of the real paths.
func seedSelectorStats(t *testing.T, path string) {
	t.Helper()
	so := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	rec := func(child, outcome string, n int) {
		for range n {
			so.Record("Router", evolution.NodeExecutionRecord{NodeName: child, Outcome: outcome})
		}
	}
	rec("Cheap", "success", 2)
	rec("Cheap", "failure", 8) // 0.20 success rate
	rec("Reliable", "success", 9)
	rec("Reliable", "failure", 1)  // 0.90 success rate
	rec("Fallback", "success", 10) // 1.00 success rate — guard keeps it last anyway
	if err := so.SaveSelectorStats(path); err != nil {
		t.Fatalf("SaveSelectorStats: %v", err)
	}
}

// routerChildNames returns the ordered child names of the "Router" Selector.
func routerChildNames(t *testing.T, tree *evolution.SerializableNode) []string {
	t.Helper()
	for i := range tree.Children {
		if tree.Children[i].Name == "Router" {
			names := make([]string, len(tree.Children[i].Children))
			for j, c := range tree.Children[i].Children {
				names[j] = c.Name
			}
			return names
		}
	}
	t.Fatalf("Router selector not found in tree")
	return nil
}

// TestEvolveTreeV2_AppliesLearnedSelectorOrderingBeforePersist pins milestone 4
// of the Selector-ordering optimizer: evolveTreeV2 must apply learned Selector
// child ordering from the durable telemetry before an evolved tree is persisted.
// The pass is flag-gated (EvolveV2Config.SelectorOrdering) and reads the durable
// stats from Config.SelectorStatsPath. MaxMutations is 0 so the ONLY change to
// the tree is the reorder itself — isolating the behavior under test.
func TestEvolveTreeV2_AppliesLearnedSelectorOrderingBeforePersist(t *testing.T) {
	statsPath := filepath.Join(t.TempDir(), "selector_stats.json")
	seedSelectorStats(t, statsPath)

	newGardener := func(t *testing.T) (*Gardener, *evolution.SerializableNode, TreeEntry) {
		t.Helper()
		refStore, err := evolution.NewStore(filepath.Join(t.TempDir(), "reflections"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		mt, err := NewMetricsTracker(t.TempDir())
		if err != nil {
			t.Fatalf("NewMetricsTracker: %v", err)
		}
		treeDir := t.TempDir()
		tree := selectorOrderingTree()
		reg := &Registry{dir: treeDir}
		reg.mu.Lock()
		reg.entries = []TreeEntry{
			{Name: "selector_tree", Description: "selector ordering", Tree: tree, FilePath: treeDir + "/tree-selector_tree.json", Active: true},
		}
		reg.mu.Unlock()
		cfg := Config{
			Registry:                 reg,
			MetricsTracker:           mt,
			RefStore:                 refStore,
			MaxMutations:             0, // isolate the reorder — no structural mutations this cycle
			EvolveWithoutReflections: true,
			SelectorStatsPath:        statsPath,
		}
		return NewGardener(cfg), tree, reg.List()[0]
	}

	v2 := func(enabled bool) EvolveV2Config {
		return EvolveV2Config{
			CascadeCfg:       evaluator.CascadeConfig{QuickThreshold: 0},
			BlocksEnabled:    false,
			UseRealLLM:       false,
			SelectorOrdering: enabled,
		}
	}

	t.Run("enabled promotes higher-success child and keeps fallback last", func(t *testing.T) {
		g, tree, entry := newGardener(t)

		if got, want := routerChildNames(t, tree), []string{"Cheap", "Reliable", "Fallback"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("precondition: Router children = %v, want %v", got, want)
		}

		g.evolveTreeV2(entry, v2(true))

		got := routerChildNames(t, tree)
		want := []string{"Reliable", "Cheap", "Fallback"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("learned Selector ordering not applied before persist: Router children = %v, want %v", got, want)
		}
	})

	t.Run("disabled is a no-op", func(t *testing.T) {
		g, tree, entry := newGardener(t)

		g.evolveTreeV2(entry, v2(false))

		got := routerChildNames(t, tree)
		want := []string{"Cheap", "Reliable", "Fallback"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Selector ordering ran while disabled: Router children = %v, want %v", got, want)
		}
	})
}

// dtOrderingTree returns a tree with a "Router" Selector whose three real
// paths (A, B, C) are each guarded by their own Condition child, plus an
// AlwaysSucceed "Fallback" default path. seedDTStats below records B as hit
// far more often than A or C, so B's condition carries the highest
// information gain and a DTAnalyzer/BTOptimizer reorder must promote B ahead
// of A and C — which stay in their original relative order (tied gain) —
// while Fallback stays last (the default-path guard).
func dtOrderingTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "Router",
				Children: []evolution.SerializableNode{
					{Type: "Sequence", Name: "A", Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "CondA"},
					}},
					{Type: "Sequence", Name: "B", Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "CondB"},
					}},
					{Type: "Sequence", Name: "C", Children: []evolution.SerializableNode{
						{Type: "Condition", Name: "CondC"},
					}},
					{Type: "AlwaysSucceed", Name: "Fallback"},
				},
			},
		},
	}
}

// seedDTStats writes durable DTAnalyzer telemetry (the evolution.DTAnalyzer.Save
// format) to path so that under "Router" the B path (hit 8x) has far higher
// information gain than the A and C paths (hit 1x each, tied with one another).
func seedDTStats(t *testing.T, path string) {
	t.Helper()
	da := evolution.NewDTAnalyzer()
	da.RecordHit("Router", "A", "CondA", true)
	for range 8 {
		da.RecordHit("Router", "B", "CondB", true)
	}
	da.RecordHit("Router", "C", "CondC", true)
	if err := da.Save(path); err != nil {
		t.Fatalf("Save DT stats: %v", err)
	}
}

// TestEvolveTreeV2_AppliesDTOptimizerOrderingBeforePersist pins milestone 3/4
// of the "wire the entropy/Gini-based BTOptimizer/DTAnalyzer decision-tree
// engine into the same production telemetry and mutation paths its sibling
// SelectorOptimizer already uses" program: evolveTreeV2 must apply
// information-gain-based Selector child reordering (evolution.BTOptimizer,
// seeded from Config.DTStatsPath) before an evolved tree is persisted,
// alongside the existing learned-Selector-ordering pass. The pass is
// flag-gated (EvolveV2Config.DTOrdering). MaxMutations is 0 so the ONLY
// change to the tree is the reorder itself — isolating the behavior under
// test.
func TestEvolveTreeV2_AppliesDTOptimizerOrderingBeforePersist(t *testing.T) {
	statsPath := filepath.Join(t.TempDir(), "dt_stats.json")
	seedDTStats(t, statsPath)

	newGardener := func(t *testing.T) (*Gardener, *evolution.SerializableNode, TreeEntry) {
		t.Helper()
		refStore, err := evolution.NewStore(filepath.Join(t.TempDir(), "reflections"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		mt, err := NewMetricsTracker(t.TempDir())
		if err != nil {
			t.Fatalf("NewMetricsTracker: %v", err)
		}
		treeDir := t.TempDir()
		tree := dtOrderingTree()
		reg := &Registry{dir: treeDir}
		reg.mu.Lock()
		reg.entries = []TreeEntry{
			{Name: "dt_tree", Description: "dt ordering", Tree: tree, FilePath: treeDir + "/tree-dt_tree.json", Active: true},
		}
		reg.mu.Unlock()
		cfg := Config{
			Registry:                 reg,
			MetricsTracker:           mt,
			RefStore:                 refStore,
			MaxMutations:             0, // isolate the reorder — no structural mutations this cycle
			EvolveWithoutReflections: true,
			DTStatsPath:              statsPath,
		}
		return NewGardener(cfg), tree, reg.List()[0]
	}

	v2 := func(enabled bool) EvolveV2Config {
		return EvolveV2Config{
			CascadeCfg:    evaluator.CascadeConfig{QuickThreshold: 0},
			BlocksEnabled: false,
			UseRealLLM:    false,
			DTOrdering:    enabled,
		}
	}

	t.Run("enabled promotes highest-information-gain child and keeps fallback last", func(t *testing.T) {
		g, tree, entry := newGardener(t)

		if got, want := routerChildNames(t, tree), []string{"A", "B", "C", "Fallback"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("precondition: Router children = %v, want %v", got, want)
		}

		g.evolveTreeV2(entry, v2(true))

		got := routerChildNames(t, tree)
		want := []string{"B", "A", "C", "Fallback"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("DT-optimizer ordering not applied before persist: Router children = %v, want %v", got, want)
		}
	})

	t.Run("disabled is a no-op", func(t *testing.T) {
		g, tree, entry := newGardener(t)

		g.evolveTreeV2(entry, v2(false))

		got := routerChildNames(t, tree)
		want := []string{"A", "B", "C", "Fallback"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("DT-optimizer ordering ran while disabled: Router children = %v, want %v", got, want)
		}
	})
}

// seedDTStatsPromotingC writes durable DTAnalyzer telemetry to path so that
// under "Router" the C path (hit 8x) has far higher information gain than the
// A and B paths (hit 1x each, tied with one another) — the mirror image of
// seedDTStats, used to prove which of two candidate stats files was actually
// read.
func seedDTStatsPromotingC(t *testing.T, path string) {
	t.Helper()
	da := evolution.NewDTAnalyzer()
	da.RecordHit("Router", "A", "CondA", true)
	da.RecordHit("Router", "B", "CondB", true)
	for range 8 {
		da.RecordHit("Router", "C", "CondC", true)
	}
	if err := da.Save(path); err != nil {
		t.Fatalf("Save DT stats: %v", err)
	}
}

// TestEvolveTreeV2_DTOptimizerOrderingPrefersPerTreeStatsOverConfigPath pins
// milestone 1/2 of the "DT-ordering pass reads the per-tree DT stats the
// daemon actually writes; ADR-191's activation is currently inert" program:
// applyDTOptimizerOrdering must prefer the real per-tree DT stats file the
// daemon writes — agent.DecisionTreeStatsFile(treeID), under
// BT_AGENT_HOME/selector-stats/<tree>-dt.json, the sidecar
// RunDeps.flushSelectorTelemetry produces alongside SelectorStatsFile — over
// the configured Config.DTStatsPath, mirroring how selectorStatsPathFor
// prefers agent.SelectorStatsFile over Config.SelectorStatsPath. The per-tree
// file is seeded to promote B; Config.DTStatsPath (fallback) is seeded to
// promote C instead, so only a correct per-tree preference reorders to B.
func TestEvolveTreeV2_DTOptimizerOrderingPrefersPerTreeStatsOverConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BT_AGENT_HOME", home)

	perTreePath := agent.DecisionTreeStatsFile("dt_tree")
	if err := os.MkdirAll(filepath.Dir(perTreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	seedDTStats(t, perTreePath)

	fallbackPath := filepath.Join(t.TempDir(), "dt_stats.json")
	seedDTStatsPromotingC(t, fallbackPath)

	refStore, err := evolution.NewStore(filepath.Join(t.TempDir(), "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	treeDir := t.TempDir()
	tree := dtOrderingTree()
	reg := &Registry{dir: treeDir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: "dt_tree", Description: "dt ordering", Tree: tree, FilePath: treeDir + "/tree-dt_tree.json", Active: true},
	}
	reg.mu.Unlock()
	cfg := Config{
		Registry:                 reg,
		MetricsTracker:           mt,
		RefStore:                 refStore,
		MaxMutations:             0, // isolate the reorder — no structural mutations this cycle
		EvolveWithoutReflections: true,
		DTStatsPath:              fallbackPath,
	}
	g := NewGardener(cfg)
	entry := reg.List()[0]

	if got, want := routerChildNames(t, tree), []string{"A", "B", "C", "Fallback"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("precondition: Router children = %v, want %v", got, want)
	}

	g.evolveTreeV2(entry, EvolveV2Config{
		CascadeCfg:    evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled: false,
		UseRealLLM:    false,
		DTOrdering:    true,
	})

	got := routerChildNames(t, tree)
	want := []string{"B", "A", "C", "Fallback"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DT-optimizer ordering did not prefer per-tree stats over Config.DTStatsPath: Router children = %v, want %v", got, want)
	}
}

// ============================================================================
// CrisisIntervened / MutationBudget metrics (evolve_v2.go) — Q3 Reliability
// milestone 1: crisis intervention and the boosted mutation budget must be
// surfaced on the CycleMetrics returned by evolveTreeV2, not just logged.
// ============================================================================

// crisisMetricsGardener builds a gardener + single-tree entry with a fresh
// CrisisDetector and no reflection records (EvolveWithoutReflections bypasses
// the evidence gate so evolveTreeV2 reaches crisis detection). With zero
// records, evaluator.EvaluateTree always returns Composite 0, so fitness is
// perfectly flat across repeated cycles — deterministically driving the
// detector's stagnation counter without relying on mutation outcomes.
func crisisMetricsGardener(t *testing.T, treeName string, maxMutations int) (*Gardener, TreeEntry, Config) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: treeName, Description: "crisis metrics", Tree: tree, FilePath: dir + "/tree-" + treeName + ".json", Active: true},
	}
	reg.mu.Unlock()

	cfg := Config{
		Registry:                 reg,
		MetricsTracker:           mt,
		RefStore:                 refStore,
		MaxMutations:             maxMutations,
		UseRealLLM:               false,
		CrisisDetector:           evolution.NewCrisisDetector(),
		EvolveWithoutReflections: true,
	}
	return NewGardener(cfg), reg.List()[0], cfg
}

// crisisV2Config mirrors the low-threshold cascade config used elsewhere in
// this file so the structural quick-check never blocks the crisis path.
func crisisV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:    evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled: false,
		UseRealLLM:    false,
	}
}

// TestEvolveTreeV2_CrisisIntervention_SetsMetrics pins Q3 Reliability
// milestone 1: once CrisisDetector.Detect reports stagnation, the
// CycleMetrics returned by evolveTreeV2 for that cycle must report
// CrisisIntervened == true and a MutationBudget boosted above the configured
// MaxMutations (0 boosted to the floor of 1, per evolveTreeV2's `<1` guard).
//
// Stagnation now means strict DECLINE (flat fitness is a plateau, 2026-07-23
// review gap 6), and the zero-record harness fitness is a constant 0 — so the
// decline evidence is pre-seeded on the detector directly, ending above 0 so
// the cycle's flat-0 observation lands as the final decline that crosses the
// default StagnationLimit of 5.
func TestEvolveTreeV2_CrisisIntervention_SetsMetrics(t *testing.T) {
	g, entry, cfg := crisisMetricsGardener(t, "crisis_tree", 0)
	v2cfg := crisisV2Config()

	for _, fit := range []float64{0.6, 0.5, 0.4, 0.3, 0.2, 0.1} {
		g.cfg.CrisisDetector.Detect(evolution.CrisisState{TreeName: "crisis_tree", CurrentFitness: fit})
	}

	last := g.evolveTreeV2(entry, v2cfg)

	if !last.CrisisIntervened {
		t.Fatalf("expected CrisisIntervened == true after sustained decline, got false (metrics=%+v)", last)
	}
	if last.MutationBudget <= cfg.MaxMutations {
		t.Errorf("expected MutationBudget boosted above configured MaxMutations (%d) during crisis, got %d", cfg.MaxMutations, last.MutationBudget)
	}

	// A crisis is a transition, not a state: the intervention consumed the
	// stagnation evidence, so the immediately following (flat) cycle must not
	// re-fire — the permanent latch was gap 6 of the 2026-07-23 review.
	next := g.evolveTreeV2(entry, v2cfg)
	if next.CrisisIntervened {
		t.Fatalf("cycle after an intervention re-fired (metrics=%+v); interventions must consume the evidence", next)
	}
	if next.MutationBudget != cfg.MaxMutations {
		t.Errorf("post-intervention budget = %d, want configured %d", next.MutationBudget, cfg.MaxMutations)
	}
}

// TestEvolveTreeV2_CalmCycle_NoCrisisMetrics pins the counterpart: a fresh
// (first) cycle with no stagnation or diversity collapse must report
// CrisisIntervened == false and MutationBudget equal to the configured
// MaxMutations, unmodified.
func TestEvolveTreeV2_CalmCycle_NoCrisisMetrics(t *testing.T) {
	g, entry, cfg := crisisMetricsGardener(t, "calm_tree", 1)
	v2cfg := crisisV2Config()

	m := g.evolveTreeV2(entry, v2cfg)

	if m.CrisisIntervened {
		t.Fatalf("expected CrisisIntervened == false on a calm cycle, got true (metrics=%+v)", m)
	}
	if m.MutationBudget != cfg.MaxMutations {
		t.Errorf("expected MutationBudget == configured MaxMutations (%d) absent crisis, got %d", cfg.MaxMutations, m.MutationBudget)
	}
}

// TestEvolveTreeV2_CrisisIntervention_UsesCalibratedEmergencyRate pins the
// NotebookLM-research goal: crisis intervention must scale the mutation
// budget from CrisisDetector.EmergencyRate (the detector's calibrated
// μ_emergency), not a hardcoded ×2. evolveTreeV2 currently ignores
// action.EmergencyRate entirely (evolve_v2.go's `maxMutations =
// g.cfg.MaxMutations * 2`), so a calibrated rate away from the 0.50 default
// has zero effect on the boosted budget today.
//
// The calibrated formula this pins is ceil(MaxMutations / (1 -
// EmergencyRate)) — the natural generalization that reduces to the
// pre-existing ×2 behavior exactly at the 0.50 default, but diverges for any
// other calibration. With MaxMutations=2 and EmergencyRate=0.75, that is
// ceil(2 / 0.25) = 8, not the hardcoded 2*2=4.
func TestEvolveTreeV2_CrisisIntervention_UsesCalibratedEmergencyRate(t *testing.T) {
	g, entry, cfg := crisisMetricsGardener(t, "calibrated_crisis_tree", 2)
	g.cfg.CrisisDetector.EmergencyRate = 0.75
	v2cfg := crisisV2Config()

	// Pre-seed decline evidence (flat fitness no longer counts; see
	// TestEvolveTreeV2_CrisisIntervention_SetsMetrics).
	for _, fit := range []float64{0.6, 0.5, 0.4, 0.3, 0.2, 0.1} {
		g.cfg.CrisisDetector.Detect(evolution.CrisisState{TreeName: "calibrated_crisis_tree", CurrentFitness: fit})
	}

	last := g.evolveTreeV2(entry, v2cfg)

	if !last.CrisisIntervened {
		t.Fatalf("expected CrisisIntervened == true after sustained decline, got false (metrics=%+v)", last)
	}

	wantBudget := int(math.Ceil(float64(cfg.MaxMutations) / (1 - 0.75)))
	if wantBudget != 8 {
		t.Fatalf("test arithmetic sanity check failed: want 8, computed %d", wantBudget)
	}
	if last.MutationBudget != wantBudget {
		t.Errorf("expected MutationBudget derived from calibrated EmergencyRate 0.75 (ceil(%d/0.25)=%d), got %d — crisis intervention must apply the detector's calibrated rate instead of a hardcoded doubling",
			cfg.MaxMutations, wantBudget, last.MutationBudget)
	}
}

// hasChildNamed and the other v1 idempotency-guard helpers were retired with
// the v1 pipeline (ADR-133 Phase 6); v2 relies on clone-and-prescore candidate
// isolation instead.

// ============================================================================
// Transposition-table persistence (Q2 Evolvability milestone 1/3)
// ============================================================================

// TestRunCycleV2_TranspositionTablePersistsAcrossGardenerInstances pins Q2
// Evolvability milestone 1/3: a Gardener configured with
// Config.TranspositionTablePath must construct and thread a
// *evaluator.TranspositionTable through the v2 evolution pipeline
// (EvolveV2Config/evolveTreeV2), storing a cached (tree,task) evaluation for
// every processed tree and persisting the table to disk after every tree in
// RunCycleV2 — alongside the existing MetricsTracker.Save() call — so cached
// evaluations survive a gardener restart instead of only the standalone
// bt-evaluator binary persisting them. A second Gardener instance pointed at
// the same directory must see (and continue to persist) the entries the
// first instance wrote.
func TestRunCycleV2_TranspositionTablePersistsAcrossGardenerInstances(t *testing.T) {
	dir := t.TempDir()
	ttDir := filepath.Join(dir, "tt")

	simpleTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	newGardener := func(t *testing.T) *Gardener {
		t.Helper()
		refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}
		mt, err := NewMetricsTracker(dir)
		if err != nil {
			t.Fatalf("NewMetricsTracker: %v", err)
		}
		reg := &Registry{dir: dir}
		reg.mu.Lock()
		reg.entries = []TreeEntry{
			{Name: "default", Description: "default", Tree: simpleTree, FilePath: dir + "/tree-default.json", Active: true},
		}
		reg.mu.Unlock()

		cfg := Config{
			Registry:               reg,
			MetricsTracker:         mt,
			RefStore:               refStore,
			MaxMutations:           1,
			UseRealLLM:             false,
			TranspositionTablePath: ttDir,
		}
		return NewGardener(cfg)
	}

	g1 := newGardener(t)
	if _, err := g1.RunCycleV2(DefaultEvolveV2Config()); err != nil {
		t.Fatalf("RunCycleV2 (gardener 1): %v", err)
	}

	ttPath := filepath.Join(ttDir, "transposition.json")
	if _, err := os.Stat(ttPath); err != nil {
		t.Fatalf("expected transposition table to be persisted at %s after RunCycleV2, got: %v", ttPath, err)
	}

	tt1, err := evaluator.NewTranspositionTable(ttDir, 1000)
	if err != nil {
		t.Fatalf("NewTranspositionTable: %v", err)
	}
	entriesAfterFirst := tt1.Stats()
	if entriesAfterFirst == 0 {
		t.Fatal("expected at least one cached (tree,task) evaluation persisted after RunCycleV2, got 0 entries")
	}

	// A second Gardener instance sharing the same TranspositionTablePath must
	// load and continue to persist the entries the first instance wrote —
	// this is the "survives gardener restarts" contract this milestone adds.
	g2 := newGardener(t)
	if _, err := g2.RunCycleV2(DefaultEvolveV2Config()); err != nil {
		t.Fatalf("RunCycleV2 (gardener 2): %v", err)
	}

	tt2, err := evaluator.NewTranspositionTable(ttDir, 1000)
	if err != nil {
		t.Fatalf("NewTranspositionTable (reload): %v", err)
	}
	if tt2.Stats() < entriesAfterFirst {
		t.Fatalf("second gardener instance lost entries persisted by the first: got %d entries, had %d", tt2.Stats(), entriesAfterFirst)
	}
}

// ============================================================================
// MetaValidator wiring tests (NotebookLM research goal)
// ============================================================================

// metaValidatorWiringGardener builds a Gardener around gateDisabledTestTree(),
// which reliably produces one high-scoring, fitness-improving candidate (the
// 0.92-score add_before/HasClearTask mutation) with the QualityGate and
// ValidationGate both configured to accept it — isolating whatever
// MetaValidator decides as the only variable.
func metaValidatorWiringGardener(t *testing.T, metaValidator *evolution.MetaValidator) (*Gardener, TreeEntry) {
	t.Helper()
	dir := t.TempDir()
	snapDir := t.TempDir()

	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	const treeName = "meta_validator_wiring"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	registry := &Registry{dir: dir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "meta validator wiring", Tree: tree, FilePath: dir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	cfg := Config{
		Registry:       registry,
		MetricsTracker: mt,
		RefStore:       refStore,
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		ValidationGate: ValidationGateConfig{Enabled: false},
		MaxMutations:   1,
		MetaValidator:  metaValidator,
	}
	return NewGardener(cfg), registry.List()[0]
}

// TestEvolveTreeV2_MetaValidatorRejectsStructurallyBrokenMutation pins the
// NotebookLM research goal: evolution.MetaValidator must be consulted inside
// the live per-candidate acceptance loop in evolveTreeV2, so a structurally
// broken candidate is rejected even when the fitness/SLO gates (QualityGate,
// ValidationGate) already accepted it.
func TestEvolveTreeV2_MetaValidatorRejectsStructurallyBrokenMutation(t *testing.T) {
	v2cfg := EvolveV2Config{BlocksEnabled: false, UseRealLLM: false}

	t.Run("AcceptsWithoutMetaValidator", func(t *testing.T) {
		g, entry := metaValidatorWiringGardener(t, nil)
		m := g.evolveTreeV2(entry, v2cfg)
		if m.Mutations != 1 {
			t.Fatalf("precondition failed: expected fitness/SLO gates alone to accept the candidate (Mutations=1), got %d — fixture no longer produces an acceptable candidate", m.Mutations)
		}
	})

	t.Run("RejectsWithMetaValidator", func(t *testing.T) {
		// MinScore: 1.0 means ANY structural issue or warning forces MetaReject —
		// standing in for "structurally broken" without depending on exactly
		// which check a given candidate trips.
		strict := evolution.NewMetaValidator(evolution.MetaValidatorConfig{MinScore: 1.0})
		g, entry := metaValidatorWiringGardener(t, strict)
		m := g.evolveTreeV2(entry, v2cfg)
		if m.Mutations != 0 {
			t.Errorf("expected MetaValidator to reject the candidate despite fitness/SLO gates accepting it, got %d mutations applied", m.Mutations)
		}
	})
}

// ============================================================================
// Deep-search feedback (NotebookLM research goal, Q2 Evolvability milestone 3)
// ============================================================================

// TestEvolveTreeV2_DeepSearchResultAppliedWhenGreedyLoopFindsNothing pins the
// NotebookLM research goal: evaluator.IterativeDeepening's BestMutation/
// BestFitness must feed back into the tree the gardener evolves instead of
// being discarded after computing it every cycle (evolve_v2.go currently only
// reads deep.Depth/TTProbes/TTProbeHits into metrics — see the "Metrics-only
// this milestone" comment above the deep-search call).
//
// gateDisabledTestTree() + seedFailureRecords() reliably makes
// evaluator.IterativeDeepening discover a genuine fitness-improving
// add_before/PreGate mutation (verified: base composite ~41.5, deep search's
// BestFitness composite ~45.1). Setting Config.MaxMutations to 0 disables the
// greedy per-candidate loop entirely — applied stays 0 and the tree is
// untouched by it — isolating the deep-search feedback path as the only
// possible source of improvement.
func TestEvolveTreeV2_DeepSearchResultAppliedWhenGreedyLoopFindsNothing(t *testing.T) {
	dir := t.TempDir()
	ttDir := filepath.Join(dir, "tt")

	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	const treeName = "deep_search_feedback"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	registry := &Registry{dir: dir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "deep search feedback", Tree: tree, FilePath: dir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	cfg := Config{
		Registry:               registry,
		MetricsTracker:         mt,
		RefStore:               refStore,
		TranspositionTablePath: ttDir,
		MaxMutations:           0, // greedy loop budget is zero: it cannot apply anything itself
	}
	g := NewGardener(cfg)

	entry := registry.List()[0]
	treeBefore := marshalTree(t, entry.Tree)

	// A zero-value CascadeCfg (not DefaultEvolveV2Config's QuickThreshold: 30)
	// matches the other tests built on gateDisabledTestTree() — its
	// StructuralQuickEval score of 10 would otherwise trip the cascade's
	// early-return gate before the pipeline ever reaches deep search.
	v2cfg := EvolveV2Config{BlocksEnabled: false, UseRealLLM: false}
	m := g.evolveTreeV2(entry, v2cfg)

	if !m.DeepSearchUsed {
		t.Fatalf("precondition failed: expected DeepSearchUsed=true with TranspositionTablePath configured, got false (metrics=%+v)", m)
	}

	if m.NewFitness <= m.BaseFitness+0.0001 {
		t.Errorf("expected the deep search's BestMutation/BestFitness to be applied and raise NewFitness above BaseFitness (base=%.4f, new=%.4f) even though the greedy mutation budget was zero — IterativeDeepening's result must feed back into the chosen mutation instead of being discarded as metrics-only",
			m.BaseFitness, m.NewFitness)
	}
	if m.Mutations == 0 {
		t.Errorf("expected Mutations > 0 from the applied deep-search mutation despite greedy MaxMutations=0, got 0 — BestMutation must be applied to the tree, not just recorded in metrics")
	}

	treeAfter := marshalTree(t, entry.Tree)
	if bytes.Equal(treeBefore, treeAfter) {
		t.Errorf("expected the in-memory tree to be mutated by the deep search's BestMutation, but it is unchanged")
	}
}

// TestEvolveTreeV2_DeepSearchMutationRejectedByValidationGateNotPersisted pins
// Q2 Evolvability milestone 4: applying deep.BestMutation (milestone 3, see
// TestEvolveTreeV2_DeepSearchResultAppliedWhenGreedyLoopFindsNothing above)
// must be re-validated against ValidationGate before evolve_v2.go's second
// Registry.SaveTree call — exactly like the greedy loop's own gate at
// evolve_v2.go:307-321 — so a rejection reverts the tree to its
// pre-deep-search state and never persists the rejected mutation.
//
// Same fixture as the milestone-3 test (gateDisabledTestTree() +
// seedFailureRecords() + MaxMutations: 0 so only deep search can apply
// anything), but with ValidationGate enabled and no SLO evidence recorded for
// this tree name — ValidationGate fails closed in that case (see
// validation_gate.go:55-66), so the deep-search mutation must be rejected.
func TestEvolveTreeV2_DeepSearchMutationRejectedByValidationGateNotPersisted(t *testing.T) {
	dir := t.TempDir()
	ttDir := filepath.Join(dir, "tt")

	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	const treeName = "deep_search_gate_reject"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	filePath := filepath.Join(dir, "tree-"+treeName+".json")
	registry := &Registry{dir: dir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "deep search gate reject", Tree: tree, FilePath: filePath, Active: true},
	}
	registry.mu.Unlock()

	cfg := Config{
		Registry:               registry,
		MetricsTracker:         mt,
		RefStore:               refStore,
		TranspositionTablePath: ttDir,
		MaxMutations:           0, // greedy loop budget is zero: only deep search can apply anything
		ValidationGate:         DefaultValidationGateConfig(),
	}
	g := NewGardener(cfg)

	entry := registry.List()[0]
	treeBefore := marshalTree(t, entry.Tree)

	v2cfg := EvolveV2Config{BlocksEnabled: false, UseRealLLM: false}
	m := g.evolveTreeV2(entry, v2cfg)

	if !m.DeepSearchUsed {
		t.Fatalf("precondition failed: expected DeepSearchUsed=true with TranspositionTablePath configured, got false (metrics=%+v)", m)
	}

	if m.Mutations != 0 {
		t.Errorf("expected the deep-search mutation to be reverted when ValidationGate rejects it (no SLO evidence, fail-closed), got Mutations=%d", m.Mutations)
	}
	if m.NewFitness > m.BaseFitness+0.0001 {
		t.Errorf("expected NewFitness to be reverted to BaseFitness after ValidationGate rejection, got base=%.4f new=%.4f", m.BaseFitness, m.NewFitness)
	}

	treeAfter := marshalTree(t, entry.Tree)
	if !bytes.Equal(treeBefore, treeAfter) {
		t.Errorf("expected the in-memory tree to be reverted to its pre-deep-search state after ValidationGate rejection, but it changed")
	}

	if _, statErr := os.Stat(filePath); !os.IsNotExist(statErr) {
		t.Errorf("expected Registry.SaveTree NOT to persist a deep-search mutation rejected by ValidationGate, but %s exists", filePath)
	}
}

// TestEvolveTreeV2_DeepSearchMutationRejectedByMetaValidatorNotPersisted pins
// Q2 Evolvability ("harden and activate the deep-search apply path")
// milestone 1/3: the deep.BestMutation branch must consult
// Config.MetaValidator.ValidateMutation before committing, exactly like the
// greedy per-candidate loop already does at evolve_v2.go:274-280, and revert
// to the pre-deep-search tree on evolution.MetaReject the same way a
// ValidationGate rejection already reverts.
//
// Same fixture as the milestone-3/4 deep-search tests (gateDisabledTestTree()
// + seedFailureRecords() + MaxMutations: 0 so only deep search can apply
// anything), but with ValidationGate disabled (so it cannot be the source of
// rejection) and a strict MetaValidator (MinScore: 1.0, per
// TestEvolveTreeV2_MetaValidatorRejectsStructurallyBrokenMutation's
// "RejectsWithMetaValidator" subtest) as the only configured gate — isolating
// MetaValidator as the sole variable.
func TestEvolveTreeV2_DeepSearchMutationRejectedByMetaValidatorNotPersisted(t *testing.T) {
	dir := t.TempDir()
	ttDir := filepath.Join(dir, "tt")

	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	const treeName = "deep_search_meta_reject"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	filePath := filepath.Join(dir, "tree-"+treeName+".json")
	registry := &Registry{dir: dir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "deep search meta reject", Tree: tree, FilePath: filePath, Active: true},
	}
	registry.mu.Unlock()

	// MinScore: 1.0 means ANY structural issue or warning forces MetaReject —
	// standing in for "structurally broken" without depending on exactly
	// which check the deep-search mutation trips.
	strict := evolution.NewMetaValidator(evolution.MetaValidatorConfig{MinScore: 1.0})

	cfg := Config{
		Registry:               registry,
		MetricsTracker:         mt,
		RefStore:               refStore,
		TranspositionTablePath: ttDir,
		MaxMutations:           0, // greedy loop budget is zero: only deep search can apply anything
		ValidationGate:         ValidationGateConfig{Enabled: false},
		MetaValidator:          strict,
	}
	g := NewGardener(cfg)

	entry := registry.List()[0]
	treeBefore := marshalTree(t, entry.Tree)

	v2cfg := EvolveV2Config{BlocksEnabled: false, UseRealLLM: false}
	m := g.evolveTreeV2(entry, v2cfg)

	if !m.DeepSearchUsed {
		t.Fatalf("precondition failed: expected DeepSearchUsed=true with TranspositionTablePath configured, got false (metrics=%+v)", m)
	}

	if m.Mutations != 0 {
		t.Errorf("expected the deep-search mutation to be reverted when MetaValidator rejects it (MinScore: 1.0), got Mutations=%d", m.Mutations)
	}
	if m.NewFitness > m.BaseFitness+0.0001 {
		t.Errorf("expected NewFitness to be reverted to BaseFitness after MetaValidator rejection, got base=%.4f new=%.4f", m.BaseFitness, m.NewFitness)
	}

	treeAfter := marshalTree(t, entry.Tree)
	if !bytes.Equal(treeBefore, treeAfter) {
		t.Errorf("expected the in-memory tree to be reverted to its pre-deep-search state after MetaValidator rejection, but it changed")
	}

	if _, statErr := os.Stat(filePath); !os.IsNotExist(statErr) {
		t.Errorf("expected Registry.SaveTree NOT to persist a deep-search mutation rejected by MetaValidator, but %s exists", filePath)
	}
}

// TestEvolveTreeV2_DisabledGateTriggersAutomaticRollback pins milestone 2/3 of
// the "Q2 Evolvability — automatic, multi-revision, observable rollback"
// program: once QualityGate.IsDisabledFor trips (ConsecutiveFails exceeded)
// evolveTreeV2 must call Registry.RollbackTree to restore the tree's
// last-known-good pre-mutation snapshot, not merely skip mutations and leave
// the tree frozen in whatever (possibly regressed) state it was already in.
func TestEvolveTreeV2_DisabledGateTriggersAutomaticRollback(t *testing.T) {
	snapDir := t.TempDir()
	refDir := t.TempDir()
	const treeName = "rollback_target"

	// Seed a last-known-good snapshot revision before the tree ever regresses.
	goodTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "GoodRevision",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}
	if _, err := evolution.SnapshotTree(goodTree, treeName, snapDir); err != nil {
		t.Fatalf("SnapshotTree(good) failed: %v", err)
	}

	// The tree currently registered is a different, regressed revision — as if
	// prior cycles drifted it away from the last-known-good snapshot before the
	// gate tripped.
	regressedTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "RegressedRevision",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
			{Type: "Action", Name: "ExtraBadStep"},
		},
	}

	refStore, err := evolution.NewStore(refDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	metricsTracker, err := NewMetricsTracker(t.TempDir())
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	filePath := filepath.Join(refDir, "tree-"+treeName+".json")
	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "rollback test", Tree: regressedTree, FilePath: filePath, Active: true},
	}
	registry.mu.Unlock()

	gate := evolution.NewQualityGate(snapDir)
	gate.ConsecutiveFails = 1
	// Simulate a prior cycle's regression streak crossing the threshold, so
	// this cycle starts with the gate already disabled for treeName.
	gate.ValidateFor(treeName, 100, 0)
	if !gate.IsDisabledFor(treeName) {
		t.Fatalf("test setup: expected gate to be disabled for %q", treeName)
	}

	cfg := Config{
		Registry:                 registry,
		MetricsTracker:           metricsTracker,
		RefStore:                 refStore,
		Gate:                     gate,
		SnapshotDir:              snapDir,
		MaxMutations:             1,
		EvolveWithoutReflections: true, // bypass the evidence gate; irrelevant to this test
	}
	g := NewGardener(cfg)

	v2cfg := EvolveV2Config{BlocksEnabled: false, UseRealLLM: false}
	entry := registry.List()[0]
	g.evolveTreeV2(entry, v2cfg)

	restored := registry.List()[0]
	if restored.Tree == nil || restored.Tree.Name != goodTree.Name {
		gotName := "<nil>"
		if restored.Tree != nil {
			gotName = restored.Tree.Name
		}
		t.Errorf("expected the disabled-gate cycle to auto-rollback the in-memory tree to the last-known-good revision %q, got %q — a bare skip leaves it frozen in the regressed state",
			goodTree.Name, gotName)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading persisted tree after rollback: %v", err)
	}
	var onDisk evolution.SerializableNode
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("unmarshal persisted tree: %v", err)
	}
	if onDisk.Name != goodTree.Name {
		t.Errorf("expected the automatic rollback to durably persist the restored tree via Registry.SaveTree, got on-disk name %q, want %q",
			onDisk.Name, goodTree.Name)
	}
}

// ============================================================================
// Knowledge-graph "evolved" write-back (evolve_v2.go) — Q2 Evolvability
// program milestone 2/4: evolveTreeV2 must call KnowledgeGraph.RecordRun with
// Outcome="evolved" whenever a cycle accepts at least one mutation, mirroring
// recordEvolvedFitness in cmd/bt-agent/tools.go, guarded so a nil
// KnowledgeGraph or an unregistered tree ID stays a safe no-op.
// ============================================================================

// knowledgeRecordingGardener mirrors experienceRecordingGardener but wires a
// knowledge.KnowledgeGraph into Config instead of an ExperienceBank, reusing
// the same gateDisabledTestTree()+seedFailureRecords() fixture that
// deterministically produces exactly one accepted, fitness-improving mutation.
func knowledgeRecordingGardener(t *testing.T, kg *knowledge.KnowledgeGraph) (*Gardener, TreeEntry) {
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
	const treeName = "knowledge_recording"
	tree := gateDisabledTestTree()
	seedFailureRecords(t, refStore, treeName)

	registry := &Registry{dir: refDir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "knowledge recording", Tree: tree, FilePath: refDir + "/tree-" + treeName + ".json", Active: true},
	}
	registry.mu.Unlock()

	cfg := Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),
		ValidationGate: ValidationGateConfig{Enabled: false},
		MaxMutations:   1,
		KnowledgeGraph: kg,
	}
	return NewGardener(cfg), registry.List()[0]
}

// TestEvolveTreeV2_RecordsEvolvedRunInKnowledgeGraph pins the core milestone
// 2/4 behavior: an accepted mutation must write an "evolved" RunRecord back
// into the configured KnowledgeGraph, bumping the tree's StructuralFitness.
func TestEvolveTreeV2_RecordsEvolvedRunInKnowledgeGraph(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "knowledge_recording", Name: "Knowledge Recording", Category: "test"})

	g, entry := knowledgeRecordingGardener(t, kg)

	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	// Non-vacuity: this setup must accept an improving mutation, otherwise the
	// write-back assertions below prove nothing.
	if m.Mutations < 1 {
		t.Fatalf("setup produced no accepted mutations (metrics=%+v) — fix the seeding", m)
	}

	tree := kg.Trees[entry.Name]
	if tree == nil {
		t.Fatalf("KnowledgeGraph has no entry for %q after an accepted-mutation cycle", entry.Name)
	}
	if tree.LastOutcome != "evolved" {
		t.Errorf("LastOutcome = %q, want %q", tree.LastOutcome, "evolved")
	}
	if tree.EvolvedCount != 1 {
		t.Errorf("EvolvedCount = %d, want 1", tree.EvolvedCount)
	}
	if tree.StructuralFitness <= 0 {
		t.Errorf("StructuralFitness = %.4f, want > 0 after an accepted mutation", tree.StructuralFitness)
	}
}

// TestEvolveTreeV2_NoAcceptedMutation_DoesNotRecordEvolvedRun pins the other
// half of the "whenever a cycle accepts at least one mutation" condition: a
// cycle that accepts zero mutations must not write an "evolved" run at all.
func TestEvolveTreeV2_NoAcceptedMutation_DoesNotRecordEvolvedRun(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph()
	kg.Register(&knowledge.TreeMeta{ID: "knowledge_no_mutation", Name: "No Mutation", Category: "test"})

	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "Step"}},
	}
	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: "knowledge_no_mutation", Description: "no mutation", Tree: tree, FilePath: dir + "/tree-knowledge_no_mutation.json", Active: true},
	}
	reg.mu.Unlock()

	cfg := Config{
		Registry:       reg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		KnowledgeGraph: kg,
	}
	g := NewGardener(cfg)

	// No reflection records were seeded, so the evidence gate skips mutation
	// entirely — this fixture deterministically accepts zero mutations.
	m := g.evolveTreeV2(reg.List()[0], DefaultEvolveV2Config())
	if m.Mutations != 0 {
		t.Fatalf("setup accepted a mutation (metrics=%+v) — fix the setup so this test isolates the no-mutation path", m)
	}

	got := kg.Trees["knowledge_no_mutation"]
	if got == nil {
		t.Fatalf("KnowledgeGraph lost its registered entry for %q", "knowledge_no_mutation")
	}
	if got.LastOutcome == "evolved" {
		t.Errorf("LastOutcome = %q — evolveTreeV2 must not record an 'evolved' run when no mutation was accepted", got.LastOutcome)
	}
	if got.EvolvedCount != 0 {
		t.Errorf("EvolvedCount = %d, want 0 (no accepted mutation this cycle)", got.EvolvedCount)
	}
}

// TestEvolveTreeV2_NilKnowledgeGraphIsNoOp pins the degradation contract: a nil
// KnowledgeGraph must leave evolveTreeV2 behaving exactly as today — mutations
// still accepted, no panic.
func TestEvolveTreeV2_NilKnowledgeGraphIsNoOp(t *testing.T) {
	g, entry := knowledgeRecordingGardener(t, nil)

	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	if m.Mutations < 1 {
		t.Fatalf("nil KnowledgeGraph changed behavior: expected accepted mutations, got metrics=%+v", m)
	}
	if m.Delta <= 0 {
		t.Fatalf("nil KnowledgeGraph changed behavior: expected fitness improvement, got delta=%.6f", m.Delta)
	}
}

// TestEvolveTreeV2_UnregisteredTreeID_KnowledgeGraphWriteBackIsNoOp pins the
// unregistered-tree-ID guard: evolveTreeV2 must not panic or otherwise change
// its accepted-mutation behavior when the configured KnowledgeGraph has no
// entry for entry.Name — relying on KnowledgeGraph.RecordRun's own no-op for
// unknown tree IDs rather than skipping the call outright.
func TestEvolveTreeV2_UnregisteredTreeID_KnowledgeGraphWriteBackIsNoOp(t *testing.T) {
	kg := knowledge.NewKnowledgeGraph() // no Register call — entry.Name is unknown to the graph
	g, entry := knowledgeRecordingGardener(t, kg)

	m := g.evolveTreeV2(entry, EvolveV2Config{UseRealLLM: false})

	if m.Mutations < 1 {
		t.Fatalf("setup produced no accepted mutations (metrics=%+v) — fix the seeding", m)
	}
	if _, ok := kg.Trees[entry.Name]; ok {
		t.Errorf("KnowledgeGraph gained a phantom entry for unregistered tree %q", entry.Name)
	}
}

// ============================================================================
// DT-optimizer read-only diagnostic entry point (evolve_v2.go) — milestone
// 4/4 of the "wire the entropy/Gini-based BTOptimizer/DTAnalyzer decision-tree
// engine into the same production telemetry and mutation paths its sibling
// SelectorOptimizer already uses" program. Unlike applyDTOptimizerOrdering
// (which mutates the live tree in place before persistence), this entry
// point must run evolution.BTOptimizer.AnalyzeTree — which destructively
// reorders/prunes via OptimizeSelectors/PruneDeadPaths — on a clone, so HITL
// reviewers can see the report without any risk of the live production tree
// changing underneath them.
// ============================================================================

// TestAnalyzeTreeDiagnostics_DoesNotMutateLiveTree pins milestone 4/4: the
// diagnostic entry point must clone entry.Tree (via cloneTreeForGardener)
// before calling evolution.BTOptimizer.AnalyzeTree, so the resulting
// DTImprovementReport's destructive-analysis counts land on the clone and the
// live production tree stays byte-for-byte unchanged. Reusing
// dtOrderingTree/seedDTStats from the milestone-3 tests above deterministically
// makes B's condition the highest-information-gain path, so an unprotected
// AnalyzeTree call would reorder Router's live children — proving this test
// is non-vacuous (it would catch an implementation that analyzes entry.Tree
// directly instead of a clone).
func TestAnalyzeTreeDiagnostics_DoesNotMutateLiveTree(t *testing.T) {
	statsPath := filepath.Join(t.TempDir(), "dt_stats.json")
	seedDTStats(t, statsPath)

	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	tree := dtOrderingTree()
	entry := TreeEntry{Name: "dt_tree", Tree: tree, Active: true}

	cfg := Config{
		Registry:       &Registry{dir: dir},
		MetricsTracker: mt,
		RefStore:       refStore,
		DTStatsPath:    statsPath,
	}
	g := NewGardener(cfg)

	before, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal original tree: %v", err)
	}

	report := g.AnalyzeTreeDiagnostics(entry)

	if report == nil {
		t.Fatal("AnalyzeTreeDiagnostics returned a nil report")
	}
	if report.TreeName != entry.Name {
		t.Errorf("report.TreeName = %q, want %q", report.TreeName, entry.Name)
	}
	if report.NodeCount != evolution.CountNodes(tree) {
		t.Errorf("report.NodeCount = %d, want %d", report.NodeCount, evolution.CountNodes(tree))
	}
	if report.ReorderChanges == 0 {
		t.Fatal("setup produced zero ReorderChanges on the clone — analysis must find the B-ahead-of-A reorder, otherwise this test cannot prove clone-isolation")
	}

	after, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal tree after AnalyzeTreeDiagnostics: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("AnalyzeTreeDiagnostics mutated the live production tree:\nbefore: %s\nafter:  %s", before, after)
	}

	got := routerChildNames(t, tree)
	want := []string{"A", "B", "C", "Fallback"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("live tree's Router child order changed: got %v, want %v (original order)", got, want)
	}
}

// TestAnalyzeTreeDiagnostics_NilTree pins the degradation contract: a nil
// entry.Tree must not panic and must report no findings, mirroring the nil
// guards already used by cloneTreeForGardener and applyDTOptimizerOrdering.
func TestAnalyzeTreeDiagnostics_NilTree(t *testing.T) {
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	cfg := Config{Registry: &Registry{dir: dir}, MetricsTracker: mt, RefStore: refStore}
	g := NewGardener(cfg)

	report := g.AnalyzeTreeDiagnostics(TreeEntry{Name: "nil_tree", Tree: nil, Active: true})
	if report != nil {
		t.Errorf("expected nil report for nil tree, got %+v", report)
	}
}

// TestEvolveTreeV2_SuiteForTreeExpectedPathsMatchRealNodes guards the
// production call site at evolve_v2.go's "suite :=
// benchmark.SuiteForTree(entry.Name)" — milestone 5/5 of the SuiteForTree
// benchmark-gating fix. Registry.addBuiltin names every domain tree
// "domain_"+name (gardener.go), exactly what entry.Name holds during a real
// evolution cycle, so this test mirrors the production lookup precisely. A
// suite whose ExpectedPath values don't occur anywhere in the tree it's
// scoring can never register a path-matched success during
// RunSuite/ScoreMutation, silently starving that tree's mutation scoring of
// signal during evolution.
func TestEvolveTreeV2_SuiteForTreeExpectedPathsMatchRealNodes(t *testing.T) {
	for name, tree := range domains.AllDomainTrees() {
		entryName := "domain_" + name
		suite := benchmark.SuiteForTree(entryName)
		for _, tc := range suite.Tasks {
			if tc.ExpectedPath == "" {
				continue
			}
			if !hasNodeNamed(tree, tc.ExpectedPath) {
				t.Errorf("%s: task %q declares ExpectedPath %q, which is not a real node anywhere in its tree (suite=%s)",
					entryName, tc.Task, tc.ExpectedPath, suite.Name)
			}
		}
	}
}

// hasNodeNamed reports whether name occurs anywhere in tree (root or any
// descendant).
func hasNodeNamed(node *evolution.SerializableNode, name string) bool {
	if node == nil {
		return false
	}
	if node.Name == name {
		return true
	}
	for i := range node.Children {
		if hasNodeNamed(&node.Children[i], name) {
			return true
		}
	}
	return false
}

// ============================================================================
// Per-tree behavioral-diversity archive tests (evolve_v2.go) — milestone 1
// ============================================================================

// TestGardener_TreeDiversityGrid_PerTreeIsolationAndAccumulation pins
// milestone 1 of the behavioral-diversity crisis-detection wiring:
// treeDiversityGrid must lazily create and cache one evolution.MAPElitesGrid
// per tree name (same name -> same instance, distinct names -> distinct
// grids), and recordDiversityObservation must feed that tree's grid via
// Grid.Insert so a structurally novel tree raises DiversityScore() off its
// zero-value cold-start baseline while re-recording an already-seen shape
// leaves the score unchanged (same niche, not a new cell).
func TestGardener_TreeDiversityGrid_PerTreeIsolationAndAccumulation(t *testing.T) {
	g := &Gardener{}

	gridA1 := g.treeDiversityGrid("a")
	if gridA1 == nil {
		t.Fatal("treeDiversityGrid should never return nil")
	}
	if gridA1.DiversityScore() != 0 {
		t.Fatalf("a freshly created grid should start empty, got DiversityScore=%f", gridA1.DiversityScore())
	}

	gridA2 := g.treeDiversityGrid("a")
	if gridA1 != gridA2 {
		t.Fatal("treeDiversityGrid(\"a\") called twice should return the same grid instance")
	}

	gridB := g.treeDiversityGrid("b")
	if gridB == gridA1 {
		t.Fatal("treeDiversityGrid should return distinct grids for distinct tree names")
	}

	shallow := &evolution.SerializableNode{Type: "Action", Name: "Leaf"}

	g.recordDiversityObservation("a", shallow, 0.5)
	afterFirst := gridA1.DiversityScore()
	if afterFirst <= 0 {
		t.Fatalf("recording a tree should raise DiversityScore above its cold-start 0, got %f", afterFirst)
	}

	// Re-recording the same-shaped tree lands in the same MAP-Elites niche, so
	// the occupied-cell count — and therefore DiversityScore — must not change.
	g.recordDiversityObservation("a", shallow, 0.6)
	afterRepeat := gridA1.DiversityScore()
	if afterRepeat != afterFirst {
		t.Fatalf("re-recording a same-shaped tree changed DiversityScore: before=%f after=%f", afterFirst, afterRepeat)
	}

	// "b"'s grid must remain untouched by "a"'s observations (per-tree isolation).
	if gridB.DiversityScore() != 0 {
		t.Fatalf("recording observations for tree \"a\" leaked into tree \"b\"'s grid: DiversityScore=%f", gridB.DiversityScore())
	}
}

// TestEvolveTreeV2_DiversityCollapse_CanFireCrisis pins milestone 2 of the
// behavioral-diversity crisis-detection wiring: evolveTreeV2 must read a live
// score from g.treeDiversityGrid(name).DiversityScore() into
// CrisisState.BehavioralDiversity before calling CrisisDetector.Detect, so the
// diversity_collapse branch — structurally dead today because
// BehavioralDiversity is always the hardcoded zero value, and Detect's
// "meaningful data" guard requires BehavioralDiversity > 0 — can actually
// fire once the archive holds a sparse-but-nonzero spread of observations.
func TestEvolveTreeV2_DiversityCollapse_CanFireCrisis(t *testing.T) {
	g, entry, _ := crisisMetricsGardener(t, "diversity_tree", 1)
	v2cfg := crisisV2Config()

	// Populate the tree's diversity archive with 6 distinct, sparsely spread
	// shapes: each chain occupies its own (node-bucket, depth-bucket) niche
	// (default MAPElitesGrid bucket sizes 10 and 2), so the grid's
	// occupied/estimated-total ratio (DiversityScore) works out to 6/36 ≈
	// 0.167 — below the detector's default 0.2 threshold, but non-zero so
	// Detect's "no meaningful data" guard (BehavioralDiversity > 0) does not
	// suppress it.
	for _, depth := range []int{0, 10, 20, 30, 40, 50} {
		g.recordDiversityObservation("diversity_tree", chainTree(depth), 0.5)
	}

	score := g.treeDiversityGrid("diversity_tree").DiversityScore()
	if score <= 0 || score >= g.cfg.CrisisDetector.DiversityThreshold {
		t.Fatalf("test setup sanity check failed: DiversityScore = %v, want in (0, %v)", score, g.cfg.CrisisDetector.DiversityThreshold)
	}

	m := g.evolveTreeV2(entry, v2cfg)

	if !m.CrisisIntervened {
		t.Fatalf("expected CrisisIntervened == true from diversity collapse (archive DiversityScore=%v, threshold=%v), got false (metrics=%+v)", score, g.cfg.CrisisDetector.DiversityThreshold, m)
	}
}

// chainTree builds a linear Sequence chain depth nodes deep, terminated by a
// single Action leaf — giving it a predictable CountNodes (depth+1) and
// MaxDepth (depth) for MAP-Elites niche placement in tests.
func chainTree(depth int) *evolution.SerializableNode {
	node := evolution.SerializableNode{Type: "Action", Name: "Leaf"}
	for range depth {
		node = evolution.SerializableNode{Type: "Sequence", Name: "Seq", Children: []evolution.SerializableNode{node}}
	}
	return &node
}

// ============================================================================
// Local-search parameter refinement (milestone 1/5) — evolveTreeV2 must run
// the evolution package's LocalSearcher as a post-structural-mutation
// refinement pass over the settled tree's mutable parameters, scored by the
// same cascade fitness function that scores structural candidates, and commit
// the tuned tree only when it beats baseFitness through the acceptance gate.
// ============================================================================

// refinableTree carries a Retry node whose MaxRetries (8) is outside the
// evaluator's rewarded [1,5] band, so estimateStructuralQuality withholds its
// 0.20 retry credit. Tuning MaxRetries into the band is a pure-parameter
// change worth +1.6 composite points — no structural mutation can produce it,
// which is exactly what the local-search pass is for.
func refinableTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "HasClearTask"},
			{
				Type: "Retry", Name: "RetryStep", MaxRetries: 8,
				Children: []evolution.SerializableNode{{Type: "Action", Name: "Step"}},
			},
		},
	}
}

// refineGardener wires a single-tree gardener with reflection records (so the
// evidence gate passes and EvaluateTree produces a non-zero composite) and
// MaxMutations 0, isolating the refinement pass from structural mutation.
func refineGardener(t *testing.T, treeName string) (*Gardener, TreeEntry, *evolution.SerializableNode) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	for i, outcome := range []evolution.Outcome{evolution.Success, evolution.Failure, evolution.Success} {
		if err := refStore.Save(&evolution.Record{
			TaskID:     "refine-" + string(rune('a'+i)),
			TreeName:   treeName,
			Task:       "refine tunable parameters",
			Plan:       "plan",
			Outcome:    outcome,
			DurationMs: 1000,
		}); err != nil {
			t.Fatalf("save reflection: %v", err)
		}
	}

	tree := refinableTree()
	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: treeName, Description: "refinement", Tree: tree, FilePath: filepath.Join(dir, "tree-"+treeName+".json"), Active: true},
	}
	reg.mu.Unlock()

	cfg := Config{
		Registry:       reg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   0, // isolate the refinement pass from structural mutations
		UseRealLLM:     false,
	}
	return NewGardener(cfg), reg.List()[0], tree
}

// TestEvolveTreeV2_LocalSearchRefinesParams pins the milestone: after the
// structural-mutation loop, evolveTreeV2 refines the tree's mutable
// parameters, keeps the result only because it beats baseFitness, reports the
// gain on CycleMetrics, and persists the tuned tree (a refinement is a
// persistable change on its own, like the Selector/DT reordering passes).
func TestEvolveTreeV2_LocalSearchRefinesParams(t *testing.T) {
	g, entry, tree := refineGardener(t, "refine_tree")

	m := g.evolveTreeV2(entry, EvolveV2Config{
		CascadeCfg:    evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled: false,
		UseRealLLM:    false,
	})

	if got := tree.Children[1].MaxRetries; got < 1 || got > 5 {
		t.Fatalf("local search did not tune MaxRetries into the rewarded band: got %d, want a value in [1,5]", got)
	}
	if m.LocalSearchDelta <= 0 {
		t.Errorf("CycleMetrics.LocalSearchDelta = %.4f, want the positive refinement gain (metrics=%+v)", m.LocalSearchDelta, m)
	}
	if m.NewFitness <= m.BaseFitness {
		t.Errorf("NewFitness %.4f did not beat BaseFitness %.4f after refinement", m.NewFitness, m.BaseFitness)
	}
	if !m.Improved {
		t.Errorf("expected Improved == true after an accepted refinement (metrics=%+v)", m)
	}

	var saved evolution.SerializableNode
	if err := util.LoadJSON(entry.FilePath, &saved); err != nil {
		t.Fatalf("tuned tree was not persisted to %s: %v", entry.FilePath, err)
	}
	if got := saved.Children[1].MaxRetries; got != tree.Children[1].MaxRetries {
		t.Errorf("persisted MaxRetries = %d, want the in-memory tuned value %d", got, tree.Children[1].MaxRetries)
	}
}

// TestEvolveTreeV2_LocalSearchDisabled_LeavesParamsUntouched pins the opt-out:
// with DisableLocalSearch set, the tree's parameters are left exactly as they
// were and no refinement gain is reported.
func TestEvolveTreeV2_LocalSearchDisabled_LeavesParamsUntouched(t *testing.T) {
	g, entry, tree := refineGardener(t, "no_refine_tree")

	m := g.evolveTreeV2(entry, EvolveV2Config{
		CascadeCfg:         evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled:      false,
		UseRealLLM:         false,
		DisableLocalSearch: true,
	})

	if got := tree.Children[1].MaxRetries; got != 8 {
		t.Errorf("DisableLocalSearch still tuned MaxRetries: got %d, want the original 8", got)
	}
	if m.LocalSearchDelta != 0 {
		t.Errorf("CycleMetrics.LocalSearchDelta = %.4f with local search disabled, want 0", m.LocalSearchDelta)
	}
}

// localSearchGateTree combines both fixtures the gate/local-search interaction
// test needs: gateDisabledTestTree's PreGate-without-HasClearTask shape (which
// reliably yields a high-score structural candidate when paired with failure
// records) wrapped around a Retry node whose MaxRetries (8) sits outside the
// evaluator's rewarded [1,5] band, exactly like refinableTree. So one cycle can
// both apply a structural mutation and find a purely numeric gain the
// local-search pass would commit.
func localSearchGateTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate"},
			{
				Type: "Retry", Name: "RetryStep", MaxRetries: 8,
				Children: []evolution.SerializableNode{
					{Type: "ChainAction", Name: "ResearchAgent", Metadata: map[string]any{"max_iterations": float64(3)}},
				},
			},
		},
	}
}

// localSearchGateArm runs one evolveTreeV2 cycle over localSearchGateTree with
// the given ValidationGate config and reports the cycle metrics, the tree's
// serialized form before and after, and whether a tree file was persisted.
func localSearchGateArm(t *testing.T, treeName string, vgCfg ValidationGateConfig) (m CycleMetrics, before, after []byte, persisted bool) {
	t.Helper()
	dir := t.TempDir()

	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	tree := localSearchGateTree()
	seedFailureRecords(t, refStore, treeName)

	filePath := filepath.Join(dir, "tree-"+treeName+".json")
	registry := &Registry{dir: dir}
	registry.mu.Lock()
	registry.entries = []TreeEntry{
		{Name: treeName, Description: "local search vs validation gate", Tree: tree, FilePath: filePath, Active: true},
	}
	registry.mu.Unlock()

	g := NewGardener(Config{
		Registry:       registry,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   1,
		ValidationGate: vgCfg,
	})

	entry := registry.List()[0]
	before = marshalTree(t, entry.Tree)
	m = g.evolveTreeV2(entry, EvolveV2Config{
		CascadeCfg:    evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled: false,
		UseRealLLM:    false,
	})
	after = marshalTree(t, entry.Tree)

	if _, statErr := os.Stat(filePath); statErr == nil {
		persisted = true
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat %s: %v", filePath, statErr)
	}
	return m, before, after, persisted
}

// TestEvolveTreeV2_LocalSearchRefinementRespectsValidationGate pins that the
// post-structural-mutation local-search pass is covered by the same
// ValidationGate as the rest of the cycle.
//
// evolve_v2.go runs refineTreeParameters AFTER the ValidationGate block, and
// its delta independently forces the Registry.SaveTree at the bottom of the
// cycle. So a cycle the gate rejected — tree restored to its pre-cycle state,
// applied reset to 0 — could still have its tuned parameters written to disk,
// smuggling an unvalidated change past a gate that just said no. The refinement
// must either run above the gate (so the gate's rejection reverts it too) or be
// skipped entirely once the gate has rejected this cycle.
//
// Two arms over the same fixture: the control arm (gate disabled) proves the
// fixture really is refinable — without it, an all-zero result would look like
// a pass for the wrong reason.
func TestEvolveTreeV2_LocalSearchRefinementRespectsValidationGate(t *testing.T) {
	// Control arm: ValidationGate off — the refinement must land and persist.
	ctrl, ctrlBefore, ctrlAfter, ctrlPersisted := localSearchGateArm(t, "local_search_gate_control", ValidationGateConfig{Enabled: false})
	if ctrl.LocalSearchDelta <= 0 {
		t.Fatalf("precondition failed: expected the ungated arm to find a positive local-search gain, got LocalSearchDelta=%.4f (metrics=%+v)", ctrl.LocalSearchDelta, ctrl)
	}
	if bytes.Equal(ctrlBefore, ctrlAfter) {
		t.Fatalf("precondition failed: expected the ungated arm to change the tree in memory, but it is unchanged")
	}
	if !ctrlPersisted {
		t.Fatalf("precondition failed: expected the ungated arm to persist the refined tree")
	}

	// Gated arm: same fixture, but DefaultValidationGateConfig with no SLO
	// evidence recorded for this tree name fails closed, so the gate rejects.
	m, before, after, persisted := localSearchGateArm(t, "local_search_gate_reject", DefaultValidationGateConfig())

	if m.LocalSearchDelta != 0 {
		t.Errorf("expected LocalSearchDelta==0 when ValidationGate rejects the cycle, got %.4f (metrics=%+v)", m.LocalSearchDelta, m)
	}
	if m.NewFitness > m.BaseFitness+0.0001 {
		t.Errorf("expected NewFitness==BaseFitness when ValidationGate rejects the cycle, got base=%.4f new=%.4f", m.BaseFitness, m.NewFitness)
	}
	if m.Improved {
		t.Errorf("expected Improved==false when ValidationGate rejects the cycle (metrics=%+v)", m)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("local-search refinement mutated the in-memory tree past a rejecting ValidationGate\nbefore: %s\nafter:  %s", before, after)
	}
	if persisted {
		t.Errorf("local-search refinement persisted a tree the ValidationGate rejected")
	}
}

// ============================================================================
// MAP-Elites active elitism (milestone 2/5) — the per-tree MAP-Elites archive
// must stop being a write-only diversity signal. When CrisisDetector reports a
// diversity collapse, evolveTreeV2 must draw an alternate mutation seed from a
// DIFFERENT niche of that tree's grid instead of mutating current-best again,
// adopt it only when it is genuinely fitter under the tree's own records, and
// persist the reseeded lineage.
// ============================================================================

// eliteSeedTree is the fitter alternate seed the diversity-crisis reseed must
// adopt. Under identical records it scores strictly above the plain
// Sequence→Action tree eliteSeedGardener starts from, because its validation
// gate and in-band Retry earn full estimateStructuralQuality credit (1.0 vs
// 0.55). It also lands in a different MAP-Elites niche — 4 nodes / depth 2
// ("n0|d2|") versus the current best's 2 nodes / depth 1 ("n0|d0|") — so it is
// a real escape from the collapsed niche rather than a same-cell shuffle.
func eliteSeedTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "EliteRoot",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "HasClearTask"},
			{
				Type: "Retry", Name: "RetryStep", MaxRetries: 3,
				Children: []evolution.SerializableNode{{Type: "Action", Name: "Step"}},
			},
		},
	}
}

// eliteSeedGardener wires a single-tree gardener with a fresh CrisisDetector
// and three reflection records — so the evidence gate passes and
// evaluator.EvaluateTree yields a non-zero composite that actually
// discriminates between tree shapes — around a deliberately weak current-best
// tree (no validation gate, no retry bound). MaxMutations is 0 so the pipeline
// has no structural-mutation budget of its own to confuse the reseed signal
// with. Returns the gardener, its registry entry, and the live tree pointer.
func eliteSeedGardener(t *testing.T, treeName string) (*Gardener, TreeEntry, *evolution.SerializableNode) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}
	for i, outcome := range []evolution.Outcome{evolution.Success, evolution.Failure, evolution.Success} {
		if err := refStore.Save(&evolution.Record{
			TaskID:     "elite-" + string(rune('a'+i)),
			TreeName:   treeName,
			Task:       "escape the collapsed niche",
			Plan:       "plan",
			Outcome:    outcome,
			DurationMs: 1000,
		}); err != nil {
			t.Fatalf("save reflection: %v", err)
		}
	}

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}

	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = []TreeEntry{
		{Name: treeName, Description: "elite reseed", Tree: tree, FilePath: filepath.Join(dir, "tree-"+treeName+".json"), Active: true},
	}
	reg.mu.Unlock()

	cfg := Config{
		Registry:       reg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   0,
		UseRealLLM:     false,
		CrisisDetector: evolution.NewCrisisDetector(),
	}
	return NewGardener(cfg), reg.List()[0], tree
}

// eliteSeedV2Config keeps the cascade quick-check and blocks out of the way and
// disables the local-search pass, so the only thing that can change the tree
// (or force a save) in these tests is the diversity-crisis reseed itself.
func eliteSeedV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:         evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled:      false,
		UseRealLLM:         false,
		DisableLocalSearch: true,
	}
}

// entryRecords resolves the reflection records evolveTreeV2 itself will score
// entry against, so a test can predict baseFitness exactly.
func entryRecords(t *testing.T, g *Gardener, entry TreeEntry) []evolution.Record {
	t.Helper()
	all, err := g.cfg.RefStore.LoadAll()
	if err != nil {
		t.Fatalf("RefStore.LoadAll: %v", err)
	}
	return recordsForEntry(all, entry)
}

// seedCollapsedArchive fills name's diversity archive with six sparsely spread
// chain shapes, each in its own (node-bucket, depth-bucket) niche, giving a
// DiversityScore of 6/36 ≈ 0.167 — below the detector's default 0.2 threshold
// but non-zero, so Detect's "no meaningful data" guard does not suppress the
// diversity_collapse branch. Every shape is archived at fitness, letting the
// caller decide whether these niches are worth reseeding from.
func seedCollapsedArchive(g *Gardener, name string, fitness float64) {
	for _, depth := range []int{0, 10, 20, 30, 40, 50} {
		g.recordDiversityObservation(name, chainTree(depth), fitness)
	}
}

// TestEvolveTreeV2_DiversityCrisis_ReseedsFromMAPElitesElite pins the
// milestone: on a diversity-collapse crisis, evolveTreeV2 must sample an elite
// from the tree's MAP-Elites grid as the cycle's mutation seed instead of
// mutating current-best, adopt it in place (it is fitter under this tree's own
// records), report it on CycleMetrics, and persist it — a reseed is a
// persistable change on its own, like the Selector/DT reordering passes.
func TestEvolveTreeV2_DiversityCrisis_ReseedsFromMAPElitesElite(t *testing.T) {
	g, entry, tree := eliteSeedGardener(t, "elite_reseed_tree")
	records := entryRecords(t, g, entry)

	baseComposite := evaluator.EvaluateTree(tree, records).Composite
	eliteComposite := evaluator.EvaluateTree(eliteSeedTree(), records).Composite
	if eliteComposite <= baseComposite {
		t.Fatalf("test setup sanity check failed: elite composite %.4f must beat current-best %.4f", eliteComposite, baseComposite)
	}

	// The chain niches are archived BELOW the current tree's fitness, so the
	// only elite worth reseeding from is the genuinely fitter one.
	seedCollapsedArchive(g, entry.Name, baseComposite-1)
	g.recordDiversityObservation(entry.Name, eliteSeedTree(), eliteComposite)

	score := g.treeDiversityGrid(entry.Name).DiversityScore()
	if score <= 0 || score >= g.cfg.CrisisDetector.DiversityThreshold {
		t.Fatalf("test setup sanity check failed: DiversityScore = %v, want in (0, %v)", score, g.cfg.CrisisDetector.DiversityThreshold)
	}

	m := g.evolveTreeV2(entry, eliteSeedV2Config())

	if !m.CrisisIntervened {
		t.Fatalf("test setup sanity check failed: expected a diversity-collapse crisis (DiversityScore=%v, threshold=%v), got metrics=%+v",
			score, g.cfg.CrisisDetector.DiversityThreshold, m)
	}
	if !m.EliteReseed {
		t.Fatalf("expected CycleMetrics.EliteReseed == true — the grid held a fitter elite in a different niche (metrics=%+v)", m)
	}
	if !hasNodeNamed(tree, "HasClearTask") {
		t.Errorf("live tree was not reseeded from the grid elite (still %s/%q with %d nodes)", tree.Type, tree.Name, evolution.CountNodes(tree))
	}
	if m.NewFitness < eliteComposite-0.0001 {
		t.Errorf("NewFitness = %.4f, want at least the adopted elite's composite %.4f (metrics=%+v)", m.NewFitness, eliteComposite, m)
	}
	if !m.Improved {
		t.Errorf("expected Improved == true after adopting a fitter elite seed (metrics=%+v)", m)
	}

	var saved evolution.SerializableNode
	if err := util.LoadJSON(entry.FilePath, &saved); err != nil {
		t.Fatalf("reseeded tree was not persisted to %s: %v", entry.FilePath, err)
	}
	if !hasNodeNamed(&saved, "HasClearTask") {
		t.Errorf("persisted tree is not the reseeded lineage: %+v", saved)
	}
}

// TestEvolveTreeV2_DiversityCrisis_NoFitterElite_KeepsCurrentBest pins the
// safety half of the milestone: a diversity collapse alone must not hand the
// cycle over to the archive. Every archived niche here is weaker than the live
// tree, so the reseed must decline and the cycle must go on mutating
// current-best.
func TestEvolveTreeV2_DiversityCrisis_NoFitterElite_KeepsCurrentBest(t *testing.T) {
	g, entry, tree := eliteSeedGardener(t, "weak_archive_tree")
	records := entryRecords(t, g, entry)
	baseComposite := evaluator.EvaluateTree(tree, records).Composite

	seedCollapsedArchive(g, entry.Name, baseComposite-1)

	score := g.treeDiversityGrid(entry.Name).DiversityScore()
	if score <= 0 || score >= g.cfg.CrisisDetector.DiversityThreshold {
		t.Fatalf("test setup sanity check failed: DiversityScore = %v, want in (0, %v)", score, g.cfg.CrisisDetector.DiversityThreshold)
	}

	m := g.evolveTreeV2(entry, eliteSeedV2Config())

	if !m.CrisisIntervened {
		t.Fatalf("test setup sanity check failed: expected a diversity-collapse crisis, got metrics=%+v", m)
	}
	if m.EliteReseed {
		t.Errorf("expected EliteReseed == false — every archived niche is weaker than the live tree (metrics=%+v)", m)
	}
	if m.NewFitness < baseComposite-0.0001 {
		t.Errorf("declining the reseed still regressed the tree: NewFitness %.4f < base %.4f", m.NewFitness, baseComposite)
	}
}

// TestEvolveTreeV2_NoCrisis_DoesNotReseed pins the gate: elite reseeding is a
// crisis response, not a standing policy. With a healthy archive (no crisis)
// the fitter elite must be left alone and the live tree must keep its own
// lineage.
func TestEvolveTreeV2_NoCrisis_DoesNotReseed(t *testing.T) {
	g, entry, tree := eliteSeedGardener(t, "healthy_archive_tree")
	records := entryRecords(t, g, entry)
	eliteComposite := evaluator.EvaluateTree(eliteSeedTree(), records).Composite

	// A single occupied niche scores DiversityScore 1.0 — well above the
	// detector's threshold — so no crisis fires even though a fitter elite is
	// sitting in the archive.
	g.recordDiversityObservation(entry.Name, eliteSeedTree(), eliteComposite)

	m := g.evolveTreeV2(entry, eliteSeedV2Config())

	if m.CrisisIntervened {
		t.Fatalf("test setup sanity check failed: expected no crisis with a healthy archive, got metrics=%+v", m)
	}
	if m.EliteReseed {
		t.Errorf("expected EliteReseed == false outside a crisis (metrics=%+v)", m)
	}
	if hasNodeNamed(tree, "HasClearTask") {
		t.Errorf("live tree was reseeded from the archive without a crisis: %+v", tree)
	}
}

// TestGardener_RecordDiversityObservation_SnapshotsTreePerCell pins the
// archive's independence-from-the-live-tree invariant. Production never hands
// recordDiversityObservation a fresh tree the way the tests above do: it hands
// over entry.Tree (evolveTreeV2), and Registry.List copies the entry slice but
// not the *SerializableNode it points at, so every cycle archives the SAME
// pointer after mutating that one object in place. Storing the pointer verbatim
// therefore leaves every cell in a tree's grid aliasing one live object holding
// whatever shape the latest cycle happened to leave behind, paired with a stale
// per-cell Fitness. Each cell must instead hold an independent snapshot of the
// shape whose descriptor selected it.
func TestGardener_RecordDiversityObservation_SnapshotsTreePerCell(t *testing.T) {
	g := &Gardener{}
	const name = "aliasing_tree"

	// The one live tree pointer the registry hands out to every cycle.
	live := &evolution.SerializableNode{Type: "Action", Name: "Leaf"}

	depths := []int{0, 10, 20, 30, 40, 50}
	for i, depth := range depths {
		*live = *chainTree(depth) // this cycle mutated the live tree in place
		g.recordDiversityObservation(name, live, 0.5+0.01*float64(i))
	}

	// A later cycle leaves the live tree in yet another shape. The already
	// archived cells must not follow it.
	*live = *chainTree(2)

	grid := g.treeDiversityGrid(name)
	if grid.CellCount() != len(depths) {
		t.Fatalf("test setup sanity check failed: expected %d occupied niches, got %d", len(depths), grid.CellCount())
	}

	type shape struct{ nodes, depth int }
	seen := make(map[shape]string, grid.CellCount())
	for key, cell := range grid.Cells {
		if cell == nil || cell.Tree == nil {
			t.Fatalf("niche %q holds no tree", key)
		}
		if cell.Tree == live {
			t.Errorf("niche %q stores the live tree pointer itself instead of a snapshot", key)
		}
		if got := grid.Key(evolution.Descriptor(cell.Tree, "")); got != key {
			t.Errorf("niche %q holds a tree whose own descriptor keys to %q (%d nodes, depth %d) — the cell no longer matches the shape that claimed it",
				key, got, evolution.CountNodes(cell.Tree), evolution.MaxDepth(cell.Tree, 0))
		}
		s := shape{nodes: evolution.CountNodes(cell.Tree), depth: evolution.MaxDepth(cell.Tree, 0)}
		if other, dup := seen[s]; dup {
			t.Errorf("niches %q and %q both hold a %d-node/depth-%d tree — the cells are aliasing one object", other, key, s.nodes, s.depth)
		}
		seen[s] = key
	}
}

// TestEvolveTreeV2_DiversityCrisis_ReseedsFromLiveDrivenArchive is the
// end-to-end consequence of the aliasing pinned above, driven exactly the way
// production drives it: every archived observation is the SAME live entry.Tree
// pointer, mutated in place between cycles. When the archive stores that
// pointer verbatim, the elite EliteSeed hands back IS the live tree, so
// reseedFromDiversityArchive re-scores current-best against itself, its
// `<= floor+0.0001` check rejects, and EliteReseed can never be true in
// production — the sibling test above only passes because it injects freshly
// built trees the production path never produces.
func TestEvolveTreeV2_DiversityCrisis_ReseedsFromLiveDrivenArchive(t *testing.T) {
	g, entry, live := eliteSeedGardener(t, "live_archive_reseed_tree")
	records := entryRecords(t, g, entry)

	currentBest := cloneTreeForGardener(live)
	baseComposite := evaluator.EvaluateTree(currentBest, records).Composite
	eliteComposite := evaluator.EvaluateTree(eliteSeedTree(), records).Composite
	if eliteComposite <= baseComposite {
		t.Fatalf("test setup sanity check failed: elite composite %.4f must beat current-best %.4f", eliteComposite, baseComposite)
	}

	// Replay earlier cycles the way evolveTreeV2 records them — mutate the ONE
	// live entry.Tree in place, then archive that same pointer. The chain niches
	// are archived below the current tree's fitness, so the only elite worth
	// reseeding from is the genuinely fitter shape.
	for _, depth := range []int{0, 10, 20, 30, 40, 50} {
		*live = *chainTree(depth)
		g.recordDiversityObservation(entry.Name, live, baseComposite-1)
	}
	*live = *eliteSeedTree()
	g.recordDiversityObservation(entry.Name, live, eliteComposite)

	// …and the cycle under test opens on current-best again, as it would after a
	// later cycle's regression restore put the tree back on its own lineage.
	*live = *currentBest

	score := g.treeDiversityGrid(entry.Name).DiversityScore()
	if score <= 0 || score >= g.cfg.CrisisDetector.DiversityThreshold {
		t.Fatalf("test setup sanity check failed: DiversityScore = %v, want in (0, %v)", score, g.cfg.CrisisDetector.DiversityThreshold)
	}

	m := g.evolveTreeV2(entry, eliteSeedV2Config())

	if !m.CrisisIntervened {
		t.Fatalf("test setup sanity check failed: expected a diversity-collapse crisis (DiversityScore=%v, threshold=%v), got metrics=%+v",
			score, g.cfg.CrisisDetector.DiversityThreshold, m)
	}
	if !m.EliteReseed {
		t.Fatalf("expected CycleMetrics.EliteReseed == true — an earlier cycle archived a fitter shape (composite %.4f) in a different niche than current-best (%.4f) (metrics=%+v)",
			eliteComposite, baseComposite, m)
	}
	if !hasNodeNamed(live, "HasClearTask") {
		t.Errorf("live tree was not reseeded from the archived elite (still %s/%q with %d nodes)", live.Type, live.Name, evolution.CountNodes(live))
	}
	if m.NewFitness < eliteComposite-0.0001 {
		t.Errorf("NewFitness = %.4f, want at least the adopted elite's composite %.4f (metrics=%+v)", m.NewFitness, eliteComposite, m)
	}
}

// ============================================================================
// Island-model population exploration (milestone 3/5): RunCycleV2 must run
// evolution.IslandModel.EvolveAll as a periodic per-domain exploration pass —
// one island per active tree — and migrate a winning individual back into the
// tree's persisted TreeEntry state, so the island model stops being an
// MCP-tool-only algorithm the 24/7 daemon never exercises.
// ============================================================================

// islandGardener wires a gardener over one deliberately weak tree per name in
// names, each backed by three reflection records so the evidence gate passes
// and evaluator.EvaluateTree yields a composite that discriminates between tree
// shapes. An IslandModel is configured with a per-cycle pass (IslandInterval
// 1) and MaxMutations is 0, so the structural-mutation loop has no budget of
// its own: anything that moves a tree here came from the island pass. Returns
// the gardener, its registry, and the island model.
func islandGardener(t *testing.T, names ...string) (*Gardener, *Registry, *evolution.IslandModel) {
	t.Helper()
	dir := t.TempDir()
	refStore, err := evolution.NewStore(filepath.Join(dir, "reflections"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mt, err := NewMetricsTracker(dir)
	if err != nil {
		t.Fatalf("NewMetricsTracker: %v", err)
	}

	entries := make([]TreeEntry, 0, len(names))
	for _, name := range names {
		for i, outcome := range []evolution.Outcome{evolution.Success, evolution.Failure, evolution.Success} {
			if err := refStore.Save(&evolution.Record{
				TaskID:     name + "-" + string(rune('a'+i)),
				TreeName:   name,
				Task:       "explore the domain population",
				Plan:       "plan",
				Outcome:    outcome,
				DurationMs: 1000,
			}); err != nil {
				t.Fatalf("save reflection: %v", err)
			}
		}
		entries = append(entries, TreeEntry{
			Name:        name,
			Description: "island domain",
			Tree: &evolution.SerializableNode{
				Type: "Sequence", Name: "Tree",
				Children: []evolution.SerializableNode{{Type: "Action", Name: "Step"}},
			},
			FilePath: filepath.Join(dir, "tree-"+name+".json"),
			Active:   true,
		})
	}

	reg := &Registry{dir: dir}
	reg.mu.Lock()
	reg.entries = entries
	reg.mu.Unlock()

	im := evolution.NewIslandModel(5, 0.25)
	return NewGardener(Config{
		Registry:       reg,
		MetricsTracker: mt,
		RefStore:       refStore,
		MaxMutations:   0,
		UseRealLLM:     false,
		IslandModel:    im,
		IslandInterval: 1,
	}), reg, im
}

// islandV2Config keeps the cascade quick-check and blocks out of the way and
// disables the local-search pass, so the island exploration pass is the only
// thing in the cycle that can change a tree or force a save.
func islandV2Config() EvolveV2Config {
	return EvolveV2Config{
		CascadeCfg:         evaluator.CascadeConfig{QuickThreshold: 0},
		BlocksEnabled:      false,
		UseRealLLM:         false,
		DisableLocalSearch: true,
	}
}

// metricsForTree returns the cycle metrics RunCycleV2 reported for name.
func metricsForTree(t *testing.T, results []CycleMetrics, name string) CycleMetrics {
	t.Helper()
	for _, m := range results {
		if m.TreeName == name {
			return m
		}
	}
	t.Fatalf("no CycleMetrics reported for tree %q (got %+v)", name, results)
	return CycleMetrics{}
}

// TestRunCycleV2_IslandPass_MigratesWinnerIntoPersistedTree pins the core of
// the milestone: when a domain's island holds an individual that is genuinely
// fitter than the live tree under that tree's own reflection records, the
// exploration pass must migrate it back into the registry's TreeEntry, persist
// it, and report the migration on CycleMetrics.
func TestRunCycleV2_IslandPass_MigratesWinnerIntoPersistedTree(t *testing.T) {
	g, reg, im := islandGardener(t, "island_tree")
	entry := reg.List()[0]
	records := entryRecords(t, g, entry)

	baseComposite := evaluator.EvaluateTree(entry.Tree, records).Composite
	winner := eliteSeedTree()
	winnerComposite := evaluator.EvaluateTree(winner, records).Composite
	if winnerComposite <= baseComposite {
		t.Fatalf("test setup sanity check failed: island winner composite %.4f must beat the live tree's %.4f", winnerComposite, baseComposite)
	}

	// The domain's island already carries an exploring subpopulation: one
	// genuinely fitter individual (an elite, so EvolveAll carries it through
	// the generation untouched) among weaker clones of the live tree.
	pop := &evolution.Population{
		Individuals: []evolution.Individual{{Tree: winner, Fitness: winnerComposite}},
	}
	for range 5 {
		pop.Individuals = append(pop.Individuals, evolution.Individual{
			Tree:    cloneTreeForGardener(entry.Tree),
			Fitness: baseComposite,
		})
	}
	im.AddIsland(entry.Name, pop)

	results, err := g.RunCycleV2(islandV2Config())
	if err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}

	m := metricsForTree(t, results, entry.Name)
	if !m.IslandAdopted {
		t.Errorf("expected CycleMetrics.IslandAdopted == true — the domain's island held a fitter individual (metrics=%+v)", m)
	}

	live := reg.List()[0].Tree
	if got := evaluator.EvaluateTree(live, records).Composite; got < winnerComposite-0.0001 {
		t.Errorf("live registry tree composite = %.4f, want at least the island winner's %.4f — the winning individual was not migrated back into TreeEntry state", got, winnerComposite)
	}

	var saved evolution.SerializableNode
	if err := util.LoadJSON(entry.FilePath, &saved); err != nil {
		t.Fatalf("island winner was not persisted to %s: %v", entry.FilePath, err)
	}
	if got := evaluator.EvaluateTree(&saved, records).Composite; got < winnerComposite-0.0001 {
		t.Errorf("persisted tree composite = %.4f, want at least the island winner's %.4f (persisted tree = %+v)", got, winnerComposite, saved)
	}
}

// TestRunCycleV2_IslandPass_SeedsAnIslandPerActiveDomain pins the "per-domain"
// half: every active tree gets its own island, warm-started from that tree, and
// a due cycle runs exactly one EvolveAll generation across all of them.
func TestRunCycleV2_IslandPass_SeedsAnIslandPerActiveDomain(t *testing.T) {
	g, reg, im := islandGardener(t, "island_a", "island_b")

	if _, err := g.RunCycleV2(islandV2Config()); err != nil {
		t.Fatalf("RunCycleV2: %v", err)
	}

	if im.Generation != 1 {
		t.Errorf("IslandModel.Generation = %d after one due cycle, want 1 — RunCycleV2 must run exactly one EvolveAll pass per due cycle", im.Generation)
	}
	for _, entry := range reg.List() {
		pop := im.GetIsland(entry.Name)
		if pop == nil {
			t.Errorf("no island for active domain %q — the exploration pass must seed one island per active tree", entry.Name)
			continue
		}
		if len(pop.Individuals) == 0 {
			t.Errorf("island for domain %q holds no individuals — it must be warm-started from the domain's current tree", entry.Name)
		}
	}
}

// TestRunCycleV2_IslandPass_RunsOnConfiguredInterval pins the "periodic" half:
// the pass is a population-exploration side channel, not per-cycle work, so
// with IslandInterval=3 only cycles 1 and 4 of four may advance the model.
func TestRunCycleV2_IslandPass_RunsOnConfiguredInterval(t *testing.T) {
	g, _, im := islandGardener(t, "island_interval_tree")
	g.cfg.IslandInterval = 3

	for cycle := 1; cycle <= 4; cycle++ {
		if _, err := g.RunCycleV2(islandV2Config()); err != nil {
			t.Fatalf("RunCycleV2 (cycle %d): %v", cycle, err)
		}
	}

	if im.Generation != 2 {
		t.Errorf("IslandModel.Generation = %d after 4 cycles with IslandInterval=3, want 2 (cycles 1 and 4 are due; 2 and 3 are not)", im.Generation)
	}
}

// islandAdoptionFixture builds exactly the setup
// TestRunCycleV2_IslandPass_MigratesWinnerIntoPersistedTree proves DOES adopt:
// a single-tree island gardener whose island already holds an individual
// strictly fitter than the live tree under that tree's own reflection records.
// The pre-pass tree is persisted first, so "the file on disk did not change"
// compares real bytes rather than the absence of a file. Returns the gardener,
// its registry entry, the island model, and the tree's records.
func islandAdoptionFixture(t *testing.T, treeName string) (*Gardener, TreeEntry, *evolution.IslandModel, []evolution.Record) {
	t.Helper()
	g, reg, im := islandGardener(t, treeName)
	entry := reg.List()[0]
	records := entryRecords(t, g, entry)

	baseComposite := evaluator.EvaluateTree(entry.Tree, records).Composite
	winner := eliteSeedTree()
	winnerComposite := evaluator.EvaluateTree(winner, records).Composite
	if winnerComposite <= baseComposite {
		t.Fatalf("test setup sanity check failed: island winner composite %.4f must beat the live tree's %.4f", winnerComposite, baseComposite)
	}

	pop := &evolution.Population{
		Individuals: []evolution.Individual{{Tree: winner, Fitness: winnerComposite}},
	}
	for range 5 {
		pop.Individuals = append(pop.Individuals, evolution.Individual{
			Tree:    cloneTreeForGardener(entry.Tree),
			Fitness: baseComposite,
		})
	}
	im.AddIsland(entry.Name, pop)

	if err := g.cfg.Registry.SaveTree(entry); err != nil {
		t.Fatalf("seeding the pre-pass tree file: %v", err)
	}
	return g, entry, im, records
}

// assertIslandAdoptionSkipped runs one exploration pass and pins that the
// safety gate under test stopped it: nothing adopted, the live tree unchanged,
// its on-disk file byte-identical. The closing check keeps the assertion
// honest — the island must STILL hold a strictly fitter champion afterwards, so
// "nothing adopted" means the gate refused a migration that was genuinely on
// the table, not that this generation's breeding left nothing worth adopting.
func assertIslandAdoptionSkipped(t *testing.T, g *Gardener, entry TreeEntry, im *evolution.IslandModel, records []evolution.Record) {
	t.Helper()
	score := func(tree *evolution.SerializableNode) float64 {
		return evaluator.EvaluateTree(tree, records).Composite
	}
	liveBefore := marshalTree(t, entry.Tree)
	// Scored before the pass on purpose: the pass migrates in place, so reading
	// the live tree afterwards would compare the champion against itself.
	liveScoreBefore := score(entry.Tree)
	diskBefore, err := os.ReadFile(entry.FilePath)
	if err != nil {
		t.Fatalf("reading the seeded tree file: %v", err)
	}

	adopted := g.runIslandExploration(g.cfg.Registry.List())

	if len(adopted) != 0 {
		t.Errorf("runIslandExploration adopted %v — a gated tree must adopt nothing", adopted)
	}
	if liveAfter := marshalTree(t, entry.Tree); !bytes.Equal(liveBefore, liveAfter) {
		t.Errorf("island winner was migrated into the live tree past the gate\nbefore: %s\nafter:  %s", liveBefore, liveAfter)
	}
	diskAfter, err := os.ReadFile(entry.FilePath)
	if err != nil {
		t.Fatalf("reading the tree file after the island pass: %v", err)
	}
	if !bytes.Equal(diskBefore, diskAfter) {
		t.Errorf("island winner was persisted past the gate\nbefore: %s\nafter:  %s", diskBefore, diskAfter)
	}

	champion, championFitness := im.BestIndividualFor(entry.Name, score)
	if champion == nil || championFitness <= liveScoreBefore+0.0001 {
		t.Fatalf("assertion is vacuous: after the pass the island's best individual scores %.4f against the pre-pass live tree's %.4f, so nothing would have been adopted regardless of the gate",
			championFitness, liveScoreBefore)
	}
}

// TestAdoptIslandWinner_SkipsWhenQualityGateDisabled pins the fail-closed half
// of the island pass. A tree whose quality gate is disabled has regressed
// ConsecutiveFails times in a row; evolveTreeV2 answers that state by pausing
// evolution and rolling the tree back to its last known-good revision. The
// island pass runs BEFORE that per-tree loop (RunCycleV2), so unless it fails
// closed on the same signal it writes a randomly bred individual over the very
// tree the rollback is about to restore — and persists it.
func TestAdoptIslandWinner_SkipsWhenQualityGateDisabled(t *testing.T) {
	g, entry, im, records := islandAdoptionFixture(t, "island_gate_disabled")

	gate := evolution.NewQualityGate(t.TempDir())
	gate.ConsecutiveFails = 1
	gate.ValidateFor(entry.Name, 50, 0.01) // force per-tree disable
	if !gate.IsDisabledFor(entry.Name) {
		t.Fatalf("precondition: quality gate must be disabled for %q", entry.Name)
	}
	g.cfg.Gate = gate

	assertIslandAdoptionSkipped(t, g, entry, im, records)
}

// TestAdoptIslandWinner_SkipsWhenValidationGateRejects pins the other gate the
// island pass bypasses: ValidationGate, the check every other persist path in
// evolve_v2.go clears before a tree reaches disk. With the fail-closed default
// config and no SLO evidence anywhere for this tree, the gate rejects, so the
// island winner must not reach the live tree or its file.
func TestAdoptIslandWinner_SkipsWhenValidationGateRejects(t *testing.T) {
	g, entry, im, records := islandAdoptionFixture(t, "island_validation_gate")

	g.cfg.ValidationGate = DefaultValidationGateConfig() // enabled, no evidence → fail-closed
	if err := ValidationGate(entry.Name, entry.Name, g.cfg.ValidationGate); err == nil {
		t.Fatalf("precondition: ValidationGate must reject %q with no SLO evidence", entry.Name)
	}

	assertIslandAdoptionSkipped(t, g, entry, im, records)
}

// ============================================================================
// MCTS as a second structural-mutation generator (milestone 4/5)
// ============================================================================

// TestAugmentWithMCTSCandidates_MergesSearchIntoOneCompetition pins the
// gardener half of the wiring: when the per-tree strategy says to augment,
// evolution.MCTSMutator's search proposals must enter the SAME scored
// candidate list evaluator.OrderMutations produced — one descending-score
// competition handed to the existing benchmark/pre-score/gate loop, not a
// separate side channel.
func TestAugmentWithMCTSCandidates_MergesSearchIntoOneCompetition(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	g, entry := experienceRecordingGardener(t, bank)

	allRecords, _ := g.cfg.RefStore.LoadAll()
	records := recordsForEntry(allRecords, entry)
	seedFitness := evaluator.EvaluateTree(entry.Tree, records)
	heuristic := evaluator.OrderMutations(entry.Tree, records, seedFitness)
	if len(heuristic) == 0 {
		t.Fatal("setup produced no heuristic candidates — the merge assertions below would be vacuous")
	}

	cfg := EvolveV2Config{MCTSStructuralSearch: true, MCTSIterations: 12}
	merged := g.augmentWithMCTSCandidates(entry.Tree, entry.Name, records, seedFitness, heuristic, cfg)

	if len(merged) <= len(heuristic) {
		t.Fatalf("merged competition has %d candidates, want more than the %d heuristic ones — the MCTS search contributed nothing",
			len(merged), len(heuristic))
	}
	mctsSourced := 0
	for _, c := range merged {
		if strings.Contains(strings.ToLower(c.Reason), "mcts") {
			mctsSourced++
		}
		if c.Op.Operation == "" || c.Op.Target == "" {
			t.Errorf("merged candidate %+v is not applicable — ApplyMutations needs an operation and a target", c.Op)
		}
	}
	if mctsSourced == 0 {
		t.Error("no merged candidate is attributed to the MCTS search")
	}
	for i := 1; i < len(merged); i++ {
		if merged[i-1].Score < merged[i].Score {
			t.Errorf("merged candidates not sorted by descending score: [%d]=%.4f < [%d]=%.4f",
				i-1, merged[i-1].Score, i, merged[i].Score)
		}
	}

	// Every merged candidate must apply on its own, since evolveTreeV2 runs
	// them one at a time against a clone of the live tree.
	for _, c := range merged {
		clone := cloneTreeForGardener(entry.Tree)
		if evolution.ApplyMutations(clone, []evolution.MutationOp{c.Op}) == 0 {
			t.Errorf("merged candidate %s/%s did not apply to the tree", c.Op.Operation, c.Op.Target)
		}
	}
}

// TestAugmentWithMCTSCandidates_RespectsStrategySelection pins the other half:
// the search is a per-tree decision, so a disabled flag and a tree the
// heuristics already cover both leave the heuristic ordering untouched.
func TestAugmentWithMCTSCandidates_RespectsStrategySelection(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatalf("NewExperienceBank: %v", err)
	}
	g, entry := experienceRecordingGardener(t, bank)

	allRecords, _ := g.cfg.RefStore.LoadAll()
	records := recordsForEntry(allRecords, entry)
	seedFitness := evaluator.EvaluateTree(entry.Tree, records)
	heuristic := evaluator.OrderMutations(entry.Tree, records, seedFitness)

	off := g.augmentWithMCTSCandidates(entry.Tree, entry.Name, records, seedFitness, heuristic,
		EvolveV2Config{MCTSStructuralSearch: false})
	if len(off) != len(heuristic) {
		t.Errorf("MCTSStructuralSearch=false changed the candidate list: %d vs %d", len(off), len(heuristic))
	}

	// A tree that BOTH heuristics already cover — its shape is a preserved
	// specialist archetype and every one of its Selectors is fully informed by
	// durable telemetry — keeps today's heuristic-only ordering. One signal
	// alone is not enough (see TestSelectStructuralStrategy), so this needs
	// both.
	covered := selectorOrderingTree()
	statsPath := filepath.Join(t.TempDir(), "selector-stats.json")
	seedSelectorStats(t, statsPath)
	g.cfg.SelectorStatsPath = statsPath

	reg := evolution.NewSpecialistRegistry()
	reg.Observe(&evolution.EvolutionMetadata{
		TreeID:   "covered",
		Fitness:  evolution.FitnessRecord{Score: 0.95, Validated: true},
		Tags:     []string{"specialist:gardener"},
		Genotype: "archetype",
	}, covered, 1)

	coveredFitness := evaluator.EvaluateTree(covered, records)
	coveredHeuristic := evaluator.OrderMutations(covered, records, coveredFitness)
	known := g.augmentWithMCTSCandidates(covered, "covered", records, coveredFitness, coveredHeuristic,
		EvolveV2Config{MCTSStructuralSearch: true, MCTSIterations: 12, Specialists: reg})
	if len(known) != len(coveredHeuristic) {
		t.Errorf("a preserved archetype with fully-informed Selectors still got the search: %d candidates vs %d heuristic",
			len(known), len(coveredHeuristic))
	}
}
