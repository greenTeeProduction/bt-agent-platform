package gardener

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evaluator"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ============================================================================
// Helper function tests (evolve_v2.go)
// ============================================================================

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

func TestHashTreeForGardener(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "TestTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step"},
		},
	}
	h := hashTreeForGardener(tree)
	if h == "" {
		t.Error("hash should not be empty")
	}
	// Same tree should produce same hash
	h2 := hashTreeForGardener(tree)
	if h != h2 {
		t.Error("same tree should produce same hash")
	}
	// Very different tree should produce different hash
	diffTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "A-Very-Different-Tree-Name",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{Type: "Action", Name: "B"},
			{Type: "Action", Name: "C"},
		},
	}
	h3 := hashTreeForGardener(diffTree)
	if h == h3 {
		t.Errorf("different trees should produce different hashes: %q vs %q", h, h3)
	}
}

func TestSerializeTreeForGardener_Nil(t *testing.T) {
	got := serializeTreeForGardener(nil)
	if got != "(nil)" {
		t.Errorf("nil tree should serialize to '(nil)', got %q", got)
	}
}

func TestSerializeTreeForGardener_NoChildren(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Action", Name: "DoWork"}
	got := serializeTreeForGardener(tree)
	expected := "Action(DoWork)[0 children]"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestSerializeTreeForGardener_WithChildren(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Step1"},
			{Type: "Action", Name: "Step2"},
		},
	}
	got := serializeTreeForGardener(tree)
	expected := "Sequence(Root)[2 children]"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"domain_code_review", "code_review"},
		{"domain_devops_ci", "devops_ci"},
		{"domain_agent_monitor", "agent_monitor"},
		{"finance_pitch_agent", "finance"},
		{"finance_earnings_reviewer", "finance"},
		{"research_deep_research", "research"},
		{"research_quick_research", "research"},
		{"godev", "godev"},
		{"default", "default"},
		{"custom_tree", "general"},
		{"unknown", "general"},
	}
	for _, tt := range tests {
		got := extractDomain(tt.name)
		if got != tt.expected {
			t.Errorf("extractDomain(%q)=%q, want %q", tt.name, got, tt.expected)
		}
	}
}

// ============================================================================
// DefaultEvolveV2Config tests
// ============================================================================

func TestDefaultEvolveV2Config(t *testing.T) {
	cfg := DefaultEvolveV2Config()
	if !cfg.MAPElitesEnabled {
		t.Error("MAPElitesEnabled should default to true")
	}
	if cfg.MAPElitesGridSize != 5 {
		t.Errorf("MAPElitesGridSize = %d, want 5", cfg.MAPElitesGridSize)
	}
	if !cfg.ParetoEnabled {
		t.Error("ParetoEnabled should default to true")
	}
	if !cfg.IslandEnabled {
		t.Error("IslandEnabled should default to true")
	}
	if cfg.MigrationInterval != 5 {
		t.Errorf("MigrationInterval = %d, want 5", cfg.MigrationInterval)
	}
	if cfg.MigrationRate != 0.1 {
		t.Errorf("MigrationRate = %f, want 0.1", cfg.MigrationRate)
	}
	if !cfg.EnsembleEnabled {
		t.Error("EnsembleEnabled should default to true")
	}
	if !cfg.RichContextEnabled {
		t.Error("RichContextEnabled should default to true")
	}
	if !cfg.BlocksEnabled {
		t.Error("BlocksEnabled should default to true")
	}
	if !cfg.MetaPromptEnabled {
		t.Error("MetaPromptEnabled should default to true")
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
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
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
		TT:             tt,
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
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
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
		TT:             tt,
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
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	customReg := &Registry{dir: dir}
	customReg.mu.Lock()
	customReg.entries = []TreeEntry{} // empty
	customReg.mu.Unlock()

	cfg := Config{
		Registry:       customReg,
		MetricsTracker: mt,
		RefStore:       refStore,
		TT:             tt,
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
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
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
		TT:             tt,
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
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
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
		TT:             tt,
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

func TestEvolveTreeV2_BloatGuard(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
	mt, _ := NewMetricsTracker(dir)

	// Build a massively bloated tree (> 600 nodes for godev)
	bloatedTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "BigTree",
	}
	bloatedTree.Children = make([]evolution.SerializableNode, 0)
	for i := 0; i < 700; i++ {
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
		TT:             tt,
		MaxMutations:   2,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	// Use a config with RichContext/Ensemble disabled to avoid ensemble bugs
	v2cfg := EvolveV2Config{
		MAPElitesEnabled:   true,
		ParetoEnabled:      true,
		IslandEnabled:      false,
		EnsembleEnabled:    false,
		RichContextEnabled: false,
		BlocksEnabled:      false,
		MetaPromptEnabled:  false,
		UseRealLLM:         false,
	}
	// evolveTreeV2 should not panic with a huge tree
	_ = g.evolveTreeV2(TreeEntry{Name: "godev", Tree: bloatedTree, Active: true}, v2cfg)
}

func TestEvolveTreeV2_NoRegressionGate(t *testing.T) {
	dir := t.TempDir()
	refStore, _ := evolution.NewStore(filepath.Join(dir, "reflections"))
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
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

	cfg := Config{Registry: customReg, MetricsTracker: mt, RefStore: refStore, TT: tt, MaxMutations: 2, UseRealLLM: false}
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
	tt, _ := evaluator.NewTranspositionTable(filepath.Join(dir, "tt"), 10)
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
		TT:             tt,
		MaxMutations:   1,
		UseRealLLM:     false,
	}
	g := NewGardener(cfg)

	// Run with all features disabled
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
// hasChildNamed tests (gardener.go)
// ============================================================================

func TestHasChildNamed_Found(t *testing.T) {
	tree := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Name: "Child1"},
			{Name: "Child2", Children: []evolution.SerializableNode{
				{Name: "Grandchild"},
			}},
		},
	}
	if !hasChildNamed(tree, "Root", "Child1") {
		t.Error("hasChildNamed should find Child1 under Root")
	}
	if !hasChildNamed(tree, "Root", "Child2") {
		t.Error("hasChildNamed should find Child2 under Root")
	}
	if !hasChildNamed(tree, "Child2", "Grandchild") {
		t.Error("hasChildNamed should find Grandchild under Child2")
	}
}

func TestHasChildNamed_NotFound(t *testing.T) {
	tree := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Name: "Child1"},
		},
	}
	if hasChildNamed(tree, "Root", "Nonexistent") {
		t.Error("hasChildNamed should not find Nonexistent")
	}
	if hasChildNamed(tree, "Root", "Child1") == false {
		t.Error("hasChildNamed should find Child1")
	}
	// Non-existent parent
	if hasChildNamed(tree, "MissingParent", "Anything") {
		t.Error("hasChildNamed should not find anything under a non-existent parent")
	}
}

func TestHasChildNamed_NoChildren(t *testing.T) {
	tree := &evolution.SerializableNode{Name: "Leaf"}
	if hasChildNamed(tree, "Leaf", "Anything") {
		t.Error("hasChildNamed on leaf node should return false")
	}
}

func TestHasChildNamed_DeepNested(t *testing.T) {
	tree := &evolution.SerializableNode{
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Name: "Middle",
				Children: []evolution.SerializableNode{
					{
						Name: "Deep",
						Children: []evolution.SerializableNode{
							{Name: "Target"},
						},
					},
				},
			},
		},
	}
	if !hasChildNamed(tree, "Deep", "Target") {
		t.Error("hasChildNamed should find Target under Deep (nested)")
	}
	if !hasChildNamed(tree, "Middle", "Deep") {
		t.Error("hasChildNamed should find Deep under Middle")
	}
	if hasChildNamed(tree, "Target", "Anything") {
		t.Error("hasChildNamed on Target leaf should return false")
	}
}
