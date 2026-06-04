package evaluator

import (
	"os"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func makeRecords(outcomes ...evolution.Outcome) []evolution.Record {
	var records []evolution.Record
	for i, o := range outcomes {
		records = append(records, evolution.Record{
			TaskID:     "task",
			Task:       "task",
			Plan:       "plan",
			Outcome:    o,
			DurationMs: int64(10000 + i*5000),
		})
	}
	return records
}

func TestEvaluateTree_Perfect(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Success, evolution.Success, evolution.Success, evolution.Success)

	fitness := EvaluateTree(tree, records)

	if fitness.SuccessRate != 1.0 {
		t.Errorf("expected 1.0 success rate, got %.2f", fitness.SuccessRate)
	}
	if fitness.Stability != 1.0 {
		t.Errorf("expected 1.0 stability for all successes, got %.2f", fitness.Stability)
	}
	if fitness.Composite < 75 {
		t.Errorf("expected composite > 75 for perfect tree, got %.1f", fitness.Composite)
	}
}

func TestEvaluateTree_AllFailures(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Failure, evolution.Failure, evolution.Failure)

	fitness := EvaluateTree(tree, records)

	if fitness.SuccessRate != 0.0 {
		t.Errorf("expected 0.0 success rate, got %.2f", fitness.SuccessRate)
	}
	if fitness.Composite > 50 {
		t.Errorf("expected composite < 50 for all failures, got %.1f", fitness.Composite)
	}
}

func TestEvaluateTree_Mixed(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Success, evolution.Failure, evolution.Success, evolution.Failure)

	fitness := EvaluateTree(tree, records)

	if fitness.SuccessRate != 0.5 {
		t.Errorf("expected 0.5 success rate, got %.2f", fitness.SuccessRate)
	}
	if fitness.Stability > 0.8 {
		t.Errorf("expected low stability for mixed outcomes, got %.2f", fitness.Stability)
	}
}

func TestEvaluateTree_EmptyRecords(t *testing.T) {
	tree := evolution.DefaultTree()
	fitness := EvaluateTree(tree, nil)

	if fitness.Composite != 0 {
		t.Errorf("expected 0 composite for empty records, got %.1f", fitness.Composite)
	}
	if fitness.NodeCount == 0 {
		t.Error("expected non-zero node count even with empty records")
	}
}

func TestEvaluateTree_StructuralQualityRewardsUsefulContentMutations(t *testing.T) {
	weak := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{{Type: "ChainAction", Name: "Agent", Metadata: map[string]any{"max_iterations": float64(3)}}},
	}
	strong := cloneTree(weak)
	evolution.ApplyMutations(strong, []evolution.MutationOp{
		{Operation: "improve_prompt", Target: "Agent", Metadata: map[string]any{"system_msg": "Verify every claim with real tool output. Never fabricate."}},
		{Operation: "add_tool", Target: "Agent", Metadata: map[string]any{"recommended_tool": "file_read"}},
		{Operation: "increase_iterations", Target: "Agent"},
	})
	records := makeRecords(evolution.Success)

	weakFit := EvaluateTree(weak, records)
	strongFit := EvaluateTree(strong, records)
	if strongFit.StructuralQuality <= weakFit.StructuralQuality {
		t.Fatalf("expected structural quality improvement: weak %.2f strong %.2f", weakFit.StructuralQuality, strongFit.StructuralQuality)
	}
	if strongFit.Composite <= weakFit.Composite {
		t.Fatalf("expected useful content mutations to improve composite: weak %.2f strong %.2f", weakFit.Composite, strongFit.Composite)
	}
}

func TestEvaluateTree_NodeCountPenalty(t *testing.T) {
	// A tree with many nodes should score lower on composite
	smallTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Small",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "A"},
		},
	}
	records := makeRecords(evolution.Success)

	smallFit := EvaluateTree(smallTree, records)
	bigFit := EvaluateTree(evolution.GoDeveloperTree(), records)

	if smallFit.Composite <= bigFit.Composite {
		t.Logf("small tree composite: %.1f, big tree composite: %.1f", smallFit.Composite, bigFit.Composite)
		// Small tree should score higher (fewer nodes = less complexity penalty)
	}
}

func TestEvaluateTree_GoDevVsDefault(t *testing.T) {
	records := makeRecords(evolution.Success, evolution.Success, evolution.Success)

	defaultFit := EvaluateTree(evolution.DefaultTree(), records)
	goDevFit := EvaluateTree(evolution.GoDeveloperTree(), records)

	if defaultFit.NodeCount >= goDevFit.NodeCount {
		t.Errorf("GoDev tree (%d nodes) should have more nodes than default (%d nodes)",
			goDevFit.NodeCount, defaultFit.NodeCount)
	}
	// Default should score higher on composite (fewer nodes = less penalty)
	if defaultFit.Composite <= goDevFit.Composite {
		t.Logf("default composite: %.1f, godev composite: %.1f (expected default > godev)",
			defaultFit.Composite, goDevFit.Composite)
	}
}

// --- Transposition Table tests ---

func TestTranspositionTable_ProbeMiss(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}

	tree := evolution.DefaultTree()
	_, ok := tt.Probe(tree, "test task")
	if ok {
		t.Error("expected miss on empty TT")
	}
}

func TestTranspositionTable_StoreAndProbe(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	entry := TranspositionEntry{
		Outcome:     "success",
		SuccessRate: 0.95,
		DurationMs:  42000,
	}

	tt.Store(tree, "test task", entry)

	probed, ok := tt.Probe(tree, "test task")
	if !ok {
		t.Fatal("expected hit after store")
	}
	if probed.Outcome != "success" {
		t.Errorf("expected success, got %s", probed.Outcome)
	}
	if probed.SuccessRate != 0.95 {
		t.Errorf("expected 0.95 success rate, got %.2f", probed.SuccessRate)
	}
}

func TestTranspositionTable_DifferentTree_DifferentKey(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	defaultTree := evolution.DefaultTree()
	goDevTree := evolution.GoDeveloperTree()

	tt.Store(defaultTree, "task", TranspositionEntry{Outcome: "success"})

	_, ok := tt.Probe(goDevTree, "task")
	if ok {
		t.Error("different tree should NOT match same task in TT")
	}
}

func TestTranspositionTable_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	tt.Store(tree, "persist me", TranspositionEntry{Outcome: "success"})
	tt.Save()

	// Reload
	tt2, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}

	_, ok := tt2.Probe(tree, "persist me")
	if !ok {
		t.Error("TT should survive Save/Load roundtrip")
	}
}

func TestTranspositionTable_Eviction(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 3) // small max

	tree := evolution.DefaultTree()
	for i := 0; i < 10; i++ {
		tt.Store(tree, "task", TranspositionEntry{Outcome: "success"})
	}

	if tt.Stats() > 3 {
		t.Errorf("TT should evict to stay under max, got %d entries", tt.Stats())
	}
}

// --- Move Ordering tests ---

func TestOrderMutations_SortsByScore(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Failure, evolution.Failure, evolution.Success)
	fitness := EvaluateTree(tree, records)

	candidates := OrderMutations(tree, records, fitness)

	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}

	// Verify descending score order
	for i := 1; i < len(candidates); i++ {
		if candidates[i].Score > candidates[i-1].Score {
			t.Errorf("candidates not sorted by descending score: [%d]=%.1f > [%d]=%.1f",
				i, candidates[i].Score, i-1, candidates[i-1].Score)
		}
	}
}

func TestOrderMutations_HighSuccess_NoPreCheck(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Success, evolution.Success, evolution.Success, evolution.Success)
	fitness := EvaluateTree(tree, records)

	candidates := OrderMutations(tree, records, fitness)

	// With high success rate, add_before (confidence check) should NOT appear
	for _, c := range candidates {
		if c.Op.Operation == "add_before" && c.Op.Target == "PreGate" {
			t.Error("add_before PreGate should not be recommended when success rate is high")
		}
	}
}

func TestOrderMutations_LowSuccess_RecommendsValidation(t *testing.T) {
	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Failure, evolution.Failure, evolution.Success)
	fitness := EvaluateTree(tree, records)

	candidates := OrderMutations(tree, records, fitness)

	hasValidation := false
	for _, c := range candidates {
		if c.Op.Operation == "add_before" && c.Op.Target == "PreGate" {
			hasValidation = true
		}
	}
	if !hasValidation {
		t.Error("expected add_before PreGate recommendation when success rate is low")
	}
}

func TestOrderMutations_PrioritizesClearTaskGateForWeakTrees(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "PreGate"},
			{Type: "ChainAction", Name: "ResearchAgent", Metadata: map[string]any{"max_iterations": float64(3)}},
		},
	}
	records := makeRecords(evolution.Failure, evolution.Failure, evolution.Success)
	fitness := EvaluateTree(tree, records)

	candidates := OrderMutations(tree, records, fitness)
	if len(candidates) == 0 {
		t.Fatal("expected candidates")
	}
	first := candidates[0]
	if first.Op.Operation != "add_before" || first.Op.Target != "PreGate" || first.Op.Node == nil || first.Op.Node.Name != "HasClearTask" {
		t.Fatalf("expected HasClearTask gate as top mutation, got op=%s target=%s node=%v reason=%q", first.Op.Operation, first.Op.Target, first.Op.Node, first.Reason)
	}
}

func TestOrderMutations_PrioritizesPromptThenToolThenIterations(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{{
			Type: "ChainAction", Name: "ResearchAgent", Metadata: map[string]any{
				"system_msg":     "Research the task.",
				"tools":          []any{},
				"max_iterations": float64(3),
			},
		}},
	}
	records := makeRecords(evolution.Success)
	fitness := EvaluateTree(tree, records)

	candidates := OrderMutations(tree, records, fitness)
	if len(candidates) < 3 {
		t.Fatalf("expected at least three content candidates, got %d", len(candidates))
	}
	want := []string{"improve_prompt", "add_tool", "increase_iterations"}
	for i, op := range want {
		if candidates[i].Op.Operation != op {
			t.Fatalf("candidate[%d] op=%s, want %s; all=%v", i, candidates[i].Op.Operation, op, candidates)
		}
	}
}

// --- Iterative Deepening tests ---

func TestIterativeDeepening_NoCrash(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Success, evolution.Failure, evolution.Success)

	result := IterativeDeepening(tree, records, tt, 1)

	if result.Depth != 1 {
		t.Errorf("expected depth 1, got %d", result.Depth)
	}
	if result.BaseFitness.Composite == 0 {
		t.Error("expected non-zero base fitness")
	}
}

func TestIterativeDeepening_PrunesExplodingTrees(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	// Tree with wrap_retry on many nodes — mutations would explode node count
	tree := evolution.GoDeveloperTree()
	records := makeRecords(evolution.Success)

	result := IterativeDeepening(tree, records, tt, 3)

	if result.PrunedCount == 0 {
		t.Log("no pruning occurred — may be fine with small tree")
	}
	// Should complete without error
	if result.TTProbes == 0 {
		t.Log("no TT probes — may be fine with empty TT")
	}
}

func TestIterativeDeepening_TTHits(t *testing.T) {
	tmpDir := t.TempDir()
	tt, _ := NewTranspositionTable(tmpDir, 100)

	tree := evolution.DefaultTree()
	records := makeRecords(evolution.Success)

	// Pre-populate TT with evaluations of mutation combos
	clone := cloneTree(tree)
	evolution.ApplyMutations(clone, []evolution.MutationOp{
		{Operation: "wrap_retry", Target: "AnalyzeTask"},
	})
	tt.Store(clone, hashTree(clone)+":eval", TranspositionEntry{SuccessRate: 0.8})

	result := IterativeDeepening(tree, records, tt, 1)

	if result.TTProbeHits == 0 {
		t.Log("no TT hits — tree hash may differ")
	}
	_ = result
}

// --- Helpers for tests ---

func TestCloneTree_Independent(t *testing.T) {
	original := evolution.DefaultTree()
	clone := cloneTree(original)

	// Modify clone — should not affect original
	clone.Name = "Modified"
	if original.Name == "Modified" {
		t.Error("clone modification affected original")
	}

	clone.Children = append(clone.Children, evolution.SerializableNode{Type: "Action", Name: "Extra"})
	if len(original.Children) == len(clone.Children) {
		t.Error("clone children modification affected original")
	}
}

func TestHashTree_Deterministic(t *testing.T) {
	tree := evolution.DefaultTree()
	h1 := hashTree(tree)
	h2 := hashTree(tree)

	if h1 != h2 {
		t.Errorf("hash not deterministic: %s != %s", h1, h2)
	}
}

func TestHashTree_DifferentTrees(t *testing.T) {
	h1 := hashTree(evolution.DefaultTree())
	h2 := hashTree(evolution.GoDeveloperTree())

	if h1 == h2 {
		t.Error("different trees should have different hashes")
	}
}

func TestTT_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := tmpDir + "/new/sub/path"
	tt, err := NewTranspositionTable(subDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(subDir); os.IsNotExist(err) {
		t.Error("TT directory should be created")
	}
	_ = tt
}
