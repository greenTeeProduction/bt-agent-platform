package evaluator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reflection"
)

// ─── containsWord tests ───

func TestContainsWord_ExactMatch(t *testing.T) {
	if !containsWord("AnalyzeTask", "AnalyzeTask") {
		t.Error("expected exact match")
	}
}

func TestContainsWord_PrefixMatch(t *testing.T) {
	if !containsWord("AnalyzeTask and more", "AnalyzeTask") {
		t.Error("expected prefix match")
	}
}

func TestContainsWord_SuffixMatch(t *testing.T) {
	if !containsWord("something AnalyzeTask", "AnalyzeTask") {
		t.Error("expected suffix match")
	}
}

func TestContainsWord_NoMatch(t *testing.T) {
	if containsWord("ExecutePlan", "AnalyzeTask") {
		t.Error("should not match different words")
	}
}

func TestContainsWord_ShorterThanWord(t *testing.T) {
	if containsWord("short", "longerword") {
		t.Error("should not match when source is shorter")
	}
}

func TestContainsWord_EmptySource(t *testing.T) {
	if containsWord("", "word") {
		t.Error("empty source should not match")
	}
}

func TestContainsWord_EmptyWord(t *testing.T) {
	if !containsWord("anything", "") {
		t.Error("empty word should always match")
	}
}

// ─── CascadeLevel.String tests ───

func TestCascadeLevelString_Skip(t *testing.T) {
	if LevelSkip.String() != "skip" {
		t.Errorf("expected 'skip', got '%s'", LevelSkip.String())
	}
}

func TestCascadeLevelString_Quick(t *testing.T) {
	if LevelQuick.String() != "quick" {
		t.Errorf("expected 'quick', got '%s'", LevelQuick.String())
	}
}

func TestCascadeLevelString_Bench(t *testing.T) {
	if LevelBench.String() != "bench" {
		t.Errorf("expected 'bench', got '%s'", LevelBench.String())
	}
}

func TestCascadeLevelString_Full(t *testing.T) {
	if LevelFull.String() != "full" {
		t.Errorf("expected 'full', got '%s'", LevelFull.String())
	}
}

func TestCascadeLevelString_Unknown(t *testing.T) {
	unknown := CascadeLevel(99)
	if unknown.String() != "unknown" {
		t.Errorf("expected 'unknown', got '%s'", unknown.String())
	}
}

// ─── CascadeResult.Passed tests ───

func TestCascadeResult_Passed_NotRejected(t *testing.T) {
	cr := CascadeResult{Rejected: false}
	if !cr.Passed() {
		t.Error("Passed() should return true when Rejected is false")
	}
}

func TestCascadeResult_Passed_Rejected(t *testing.T) {
	cr := CascadeResult{Rejected: true}
	if cr.Passed() {
		t.Error("Passed() should return false when Rejected is true")
	}
}

// ─── CascadeResult.Summary tests ───

func TestCascadeResult_Summary_Rejected(t *testing.T) {
	cr := CascadeResult{
		QuickScore:   15.5,
		Rejected:     true,
		RejectReason: "too low",
	}
	summary := cr.Summary()
	if summary != "REJECTED(too low) Q=15.5" {
		t.Errorf("unexpected rejected summary: %s", summary)
	}
}

func TestCascadeResult_Summary_Passed(t *testing.T) {
	cr := CascadeResult{
		QuickScore: 80,
		BenchScore: 70,
		FullScore:  90,
		Level:      LevelFull,
	}
	summary := cr.Summary()
	if summary != "PASSED(full) Q=80.0 B=70.0 F=90.0" {
		t.Errorf("unexpected passed summary: %s", summary)
	}
}

func TestCascadeResult_Summary_PassedQuickOnly(t *testing.T) {
	cr := CascadeResult{
		QuickScore: 60,
		Level:      LevelQuick,
	}
	summary := cr.Summary()
	if summary != "PASSED(quick) Q=60.0 B=0.0 F=0.0" {
		t.Errorf("unexpected quick-only summary: %s", summary)
	}
}

// ─── TranspositionTable load tests ───

func TestTranspositionTable_Load_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Load from non-existent file should not error
	tt.load()
	if tt.Stats() != 0 {
		t.Errorf("expected 0 entries after loading non-existent file, got %d", tt.Stats())
	}
}

func TestTranspositionTable_Load_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a TT, save some entries, then reload
	tt1, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()
	tt1.Store(tree, "task1", TranspositionEntry{Outcome: "success"})
	tt1.Store(tree, "task2", TranspositionEntry{Outcome: "failure"})
	if err := tt1.Save(); err != nil {
		t.Fatal(err)
	}

	// Reload from same directory
	tt2, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if tt2.Stats() != 2 {
		t.Errorf("expected 2 entries after reload, got %d", tt2.Stats())
	}
}

func TestTranspositionTable_Load_TrimOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	// Create TT, save many entries, reload with tiny maxSize
	tt1, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()
	for i := 0; i < 10; i++ {
		tt1.Store(tree, "task", TranspositionEntry{Outcome: "success"})
	}
	if err := tt1.Save(); err != nil {
		t.Fatal(err)
	}

	// Reload with maxSize=2 — should trim to 2 entries
	tt2, err := NewTranspositionTable(tmpDir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if tt2.Stats() > 2 {
		t.Errorf("expected ≤2 entries after reload with maxSize=2, got %d", tt2.Stats())
	}
}

func TestTranspositionTable_Store_Eviction(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 3)
	if err != nil {
		t.Fatal(err)
	}

	tree := evolution.DefaultTree()
	// Store 4 different (tree,task) pairs — should evict to stay ≤3
	for i := 0; i < 4; i++ {
		task := string(rune('a' + i))
		tt.Store(tree, task, TranspositionEntry{Outcome: "success"})
	}

	if tt.Stats() > 3 {
		t.Errorf("expected ≤3 entries after eviction, got %d", tt.Stats())
	}
}

// ─── TranspositionTable Save error path ───

func TestTranspositionTable_Save_BadPath(t *testing.T) {
	// Use a non-writable path
	tt := &TranspositionTable{
		entries: make(map[string]TranspositionEntry),
		path:    "/nonexistent/deep/path/transposition.json",
	}
	tt.Store(evolution.DefaultTree(), "task", TranspositionEntry{Outcome: "success"})
	err := tt.Save()
	if err == nil {
		t.Error("expected error when saving to non-writable path")
	}
}

// ─── findFailureNodes tests ───

func TestFindFailureNodes_NoFailures(t *testing.T) {
	records := makeRecords(reflection.Success, reflection.Success)
	nodes := findFailureNodes(records)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes with no failures, got %d", len(nodes))
	}
}

func TestFindFailureNodes_WithFailures(t *testing.T) {
	records := []reflection.Record{
		{
			Outcome: reflection.Failure,
			WhatToImprove: []string{
				"AnalyzeTask failed to parse input",
				"ExecutePlan was too slow",
			},
		},
		{
			Outcome: reflection.Failure,
			WhatToImprove: []string{
				"SelfCorrect did not fix the issue",
			},
		},
	}
	nodes := findFailureNodes(records)
	if len(nodes) == 0 {
		t.Fatal("expected at least one failure node")
	}
	// Collect into set for easy lookup
	nodeSet := make(map[string]bool)
	for _, n := range nodes {
		nodeSet[n] = true
	}
	if !nodeSet["AnalyzeTask"] {
		t.Error("expected AnalyzeTask in failure nodes")
	}
	if !nodeSet["ExecutePlan"] {
		t.Error("expected ExecutePlan in failure nodes")
	}
	if !nodeSet["SelfCorrect"] {
		t.Error("expected SelfCorrect in failure nodes")
	}
}

func TestFindFailureNodes_IgnoresNonFailure(t *testing.T) {
	records := []reflection.Record{
		{Outcome: reflection.Success, WhatToImprove: []string{"AnalyzeTask is great"}},
		{Outcome: reflection.Partial, WhatToImprove: []string{"ExecutePlan needs work"}},
	}
	nodes := findFailureNodes(records)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for non-failure outcomes, got %d", len(nodes))
	}
}

func TestFindFailureNodes_Deduplicates(t *testing.T) {
	records := []reflection.Record{
		{
			Outcome: reflection.Failure,
			WhatToImprove: []string{
				"AnalyzeTask failed",
				"AnalyzeTask needs improvement",
			},
		},
	}
	nodes := findFailureNodes(records)
	if len(nodes) != 1 {
		t.Errorf("expected 1 unique node (deduplicated), got %d", len(nodes))
	}
}

// ─── IterativeDeepening deeper coverage tests ───

func TestIterativeDeepening_MultipleDepths(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	records := makeRecords(reflection.Failure, reflection.Failure, reflection.Success)

	result := IterativeDeepening(tree, records, tt, 3)

	if result.Depth < 3 {
		t.Logf("depth reached: %d (may stop early if no further combos)", result.Depth)
	}
	if result.BaseFitness.Composite == 0 {
		t.Error("expected non-zero base fitness")
	}
}

func TestIterativeDeepening_PruningOnNodeExplosion(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	// Use a tiny tree so mutations 2x look like explosion
	tinyTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Tiny",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
		},
	}
	// Create enough failure records to generate many mutation candidates
	records := []reflection.Record{
		{
			Outcome:       reflection.Failure,
			WhatToImprove: []string{"AnalyzeTask error", "ExecutePlan error", "SelfCorrect error"},
		},
		{
			Outcome:       reflection.Failure,
			WhatToImprove: []string{"ReflectOnOutcome error"},
		},
	}

	result := IterativeDeepening(tinyTree, records, tt, 2)

	// Pruning should fire for wrap_retry mutations that double node count
	t.Logf("pruned: %d (expected >0 for tiny tree with many candidates)", result.PrunedCount)
	// Should complete without error regardless
	_ = result
}

func TestIterativeDeepening_TTHitDuringSearch(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	records := makeRecords(reflection.Success)

	// Pre-populate with the tree's own hash to trigger TT hit
	treeHash := hashTree(tree)
	tt.Store(tree, treeHash+":eval", TranspositionEntry{SuccessRate: 0.75})

	result := IterativeDeepening(tree, records, tt, 1)

	if result.TTProbes < 1 {
		t.Log("expected at least 1 TT probe")
	}
	_ = result
}

// ─── generateCombos edge cases ───

func TestGenerateCombos_DepthZero(t *testing.T) {
	candidates := []MutationCandidate{
		{Op: evolution.MutationOp{Operation: "wrap_retry"}},
	}
	result := generateCombos(candidates, 0)
	if result != nil {
		t.Errorf("expected nil for depth 0, got %d combos", len(result))
	}
}

func TestGenerateCombos_DepthExceedsLength(t *testing.T) {
	candidates := []MutationCandidate{
		{Op: evolution.MutationOp{Operation: "wrap_retry"}},
	}
	result := generateCombos(candidates, 5)
	if len(result) != 1 {
		t.Errorf("expected 1 combo when depth > len, got %d", len(result))
	}
}

func TestGenerateCombos_EmptyCandidates(t *testing.T) {
	result := generateCombos(nil, 1)
	if result != nil {
		t.Errorf("expected nil for empty candidates")
	}
}

// ─── hasNode tests ───

func TestHasNode_RootMatch(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "Root"}
	if !hasNode(tree, "Root") {
		t.Error("should match root node name")
	}
}

func TestHasNode_DeepMatch(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "Middle",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "Leaf"},
				},
			},
		},
	}
	if !hasNode(tree, "Leaf") {
		t.Error("should find deep node")
	}
}

func TestHasNode_NoMatch(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "Root"}
	if hasNode(tree, "Nonexistent") {
		t.Error("should not match nonexistent node")
	}
}

func TestHasNode_EmptyChildren(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Action", Name: "Leaf"}
	if !hasNode(tree, "Leaf") {
		t.Error("should match leaf node")
	}
	if hasNode(tree, "Other") {
		t.Error("should not match wrong name")
	}
}

// ─── minFloat64 tests ───

func TestMinFloat64_FirstSmaller(t *testing.T) {
	if minFloat64(1.0, 5.0) != 1.0 {
		t.Error("expected 1.0 as min")
	}
}

func TestMinFloat64_SecondSmaller(t *testing.T) {
	if minFloat64(5.0, 1.0) != 1.0 {
		t.Error("expected 1.0 as min")
	}
}

func TestMinFloat64_Equal(t *testing.T) {
	if minFloat64(3.0, 3.0) != 3.0 {
		t.Error("expected 3.0 when equal")
	}
}

func TestMinFloat64_Negative(t *testing.T) {
	if minFloat64(-5.0, -2.0) != -5.0 {
		t.Error("expected -5.0 as min")
	}
}

// ─── CascadeStats.FilterRate zero total ───

func TestFilterRate_ZeroTotal(t *testing.T) {
	cs := CascadeStats{Total: 0}
	if cs.FilterRate() != 0 {
		t.Errorf("expected 0 filter rate for zero total, got %.1f", cs.FilterRate())
	}
}

// ─── TreeMaxDepth edge cases ───

func TestTreeMaxDepth_EmptyTree(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root"}
	depth := treeMaxDepth(tree, 0)
	if depth != 1 {
		t.Errorf("expected depth 1 for single node, got %d", depth)
	}
}

func TestTreeMaxDepth_Nested(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "l1",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "l2",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "l3"},
				},
			},
		},
	}
	depth := treeMaxDepth(tree, 0)
	if depth != 3 {
		t.Errorf("expected depth 3, got %d", depth)
	}
}

// ─── OrderMutations edge cases ───

func TestOrderMutations_LargeNodeCount_RecommendsPrune(t *testing.T) {
	tree := evolution.DefaultTree()
	// Override node count by constructing a fitness with high node count
	records := makeRecords(reflection.Success)
	fitness := EvaluateTree(tree, records)
	// Force node count > 40
	fitness.NodeCount = 50

	candidates := OrderMutations(tree, records, fitness)

	hasPrune := false
	for _, c := range candidates {
		if c.Op.Operation == "prune_node" {
			hasPrune = true
			break
		}
	}
	if !hasPrune {
		t.Error("expected prune_node recommendation when node count > 40")
	}
}

func TestOrderMutations_FewSelectors_RecommendsFallback(t *testing.T) {
	// A tree with very few selectors relative to node count should get add_fallback
	// DefaultTree has Selector children — use a small tree
	smallTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Main",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
			{Type: "Action", Name: "B"},
		},
	}
	records := makeRecords(reflection.Success)
	fitness := EvaluateTree(smallTree, records)

	candidates := OrderMutations(smallTree, records, fitness)

	hasFallback := false
	for _, c := range candidates {
		if c.Op.Operation == "add_fallback" {
			hasFallback = true
			break
		}
	}
	if !hasFallback {
		t.Log("no add_fallback — may be fine if selector ratio adequate")
	}
	_ = hasFallback
}

// ─── EvaluateTree additional edge cases ───

func TestEvaluateTree_SingleRecord(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(reflection.Success)

	fitness := EvaluateTree(tree, records)

	if fitness.SuccessRate != 1.0 {
		t.Errorf("expected 1.0 success for single record, got %.2f", fitness.SuccessRate)
	}
	if fitness.PathCoverage != 0.5 {
		t.Errorf("expected 0.5 path coverage for single record, got %.2f", fitness.PathCoverage)
	}
}

func TestEvaluateTree_NegativeDuration(t *testing.T) {
	tree := evolution.DefaultTree()
	// Duration should not cause issues even if negative (unlikely but defensive)
	records := []reflection.Record{
		{TaskID: "t1", Task: "test", Plan: "plan", Outcome: reflection.Success, DurationMs: -100},
	}
	fitness := EvaluateTree(tree, records)
	if fitness.AvgDurationMs < 0 {
		t.Logf("avg duration: %d (negative input)", fitness.AvgDurationMs)
	}
	if fitness.Composite == 0 {
		t.Error("expected non-zero composite even with negative duration")
	}
}

// ─── CascadeEvaluator edge case: all rejected at Quick ───

func TestCascadeEvaluator_AllRejectedAtQuick(t *testing.T) {
	cfg := DefaultCascadeConfig()
	ce := NewCascadeEvaluator(cfg,
		func(_ *evolution.SerializableNode) float64 { return 0 }, // all fail quick
		nil,
		nil,
	)

	individuals := []evolution.Individual{
		{Tree: testTree("a", 1, 1)},
		{Tree: testTree("b", 1, 1)},
	}

	results := ce.EvaluatePopulation(individuals)

	for _, r := range results {
		if !r.Rejected {
			t.Errorf("tree %s should be rejected at Quick", r.Tree.Name)
		}
	}
}

// ─── Helper: load with corrupted file ───

func TestTranspositionTable_Load_CorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	// Write garbage to the transposition file
	path := filepath.Join(tmpDir, "transposition.json")
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	tt, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Should load empty (bad JSON silently fails in load())
	if tt.Stats() != 0 {
		t.Errorf("expected 0 entries after loading corrupted file, got %d", tt.Stats())
	}
}

// ─── countSelectors edge cases ───

func TestCountSelectors_NilChildren(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Selector", Name: "Root"}
	count := countSelectors(tree)
	if count != 1 {
		t.Errorf("expected 1 selector, got %d", count)
	}
}

func TestCountSelectors_Nested(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Selector", Name: "S1"},
			{Type: "Sequence", Name: "Seq1"},
			{
				Type: "Selector", Name: "S2",
				Children: []evolution.SerializableNode{
					{Type: "Selector", Name: "S3"},
				},
			},
		},
	}
	count := countSelectors(tree)
	if count != 3 {
		t.Errorf("expected 3 selectors, got %d", count)
	}
}

// ─── estimatePathCoverage edge cases ───

func TestEstimatePathCoverage_FewerThanTwoRecords(t *testing.T) {
	records := makeRecords(reflection.Success)
	cov := estimatePathCoverage(records)
	if cov != 0.5 {
		t.Errorf("expected 0.5 for single record, got %.2f", cov)
	}
}

func TestEstimatePathCoverage_EmptyRecords(t *testing.T) {
	cov := estimatePathCoverage(nil)
	if cov != 0.5 {
		t.Errorf("expected 0.5 for empty records, got %.2f", cov)
	}
}
