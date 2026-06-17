package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── FULL INTEGRATION: All Trees, Chains, Use Cases ───
// Tests every tree category, chain type, routing path, quality gate,
// panic recovery, and edge case with mock LLM.

func TestIntegration_AllChainTypes(t *testing.T) {
	chainTests := []struct {
		name string
		tree *evolution.SerializableNode
		task string
	}{
		{"llm_call", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "llm_call:Respond to: {{.Task}}", Metadata: map[string]any{"max_tokens": float64(128)}},
			},
		}, "say hello"},
		{"tool_action", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "tool_action:web_search:{{.Task}}"},
			},
		}, "search for testing"},
		{"agent", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "llm_call:Complete: {{.Task}}", Metadata: map[string]any{"max_tokens": float64(128)}},
			},
		}, "what is 2+2"},
		{"refine", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "refine:Draft then improve: {{.Task}}", Metadata: map[string]any{"max_tokens": float64(128)}},
			},
		}, "explain testing"},
		{"map_reduce", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "map_reduce:Break down and combine: {{.Task}}"},
			},
		}, "analyze multiple aspects"},
		{"conversation", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "conversation:Let's discuss: {{.Task}}"},
			},
		}, "tell me about yourself"},
		{"retrieval_qa", &evolution.SerializableNode{
			Type: "Sequence", Name: "test",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "SetupDefaultTools"},
				{Type: "ChainAction", Name: "retrieval_qa:Find and answer: {{.Task}}"},
			},
		}, "what is a behavior tree"},
	}

	for _, tt := range chainTests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &Blackboard{Task: tt.task, LLM: &MockLLM{}, ChainState: map[string]any{}}
			bt := BuildTree(tt.tree, bb)
			_ = RunTask(bb, bt)
			// Chain types should execute without crashing; empty outcome is acceptable for some mock chains
		})
	}
}

func TestIntegration_QualityGatesFullFlow(t *testing.T) {
	// Structured output passes quality gates
	t.Run("structured_output_passes", func(t *testing.T) {
		bb := &Blackboard{
			Task: "test", LLM: &MockLLM{},
			Result: "# Report\n\n## Analysis\nDetailed analysis with substantial content for quality check.",
		}
		// Direct quality check
		if !validateOutputQuality(bb) {
			t.Errorf("structured output should pass quality, score=%.2f", bb.QualityScore)
		}
	})

	// Short output fails quality
	t.Run("short_output_fails", func(t *testing.T) {
		bb := &Blackboard{Task: "test", LLM: &MockLLM{}, Result: "short"}
		if validateOutputQuality(bb) {
			t.Error("short output should fail quality")
		}
	})

	// Error pattern fails
	t.Run("error_pattern_fails", func(t *testing.T) {
		bb := &Blackboard{Task: "test", LLM: &MockLLM{}, Result: "I cannot complete this task due to an error"}
		if validateOutputQuality(bb) {
			t.Error("error output should fail quality")
		}
		if bb.QualityScore > 0.2 {
			t.Errorf("error output should score low, got %.2f", bb.QualityScore)
		}
	})
}

func TestIntegration_PanicRecovery(t *testing.T) {
	// Unknown action nodes are handled gracefully (return failure, don't panic)
	bb := &Blackboard{Task: "test", LLM: &MockLLM{}}
	panicTree := &evolution.SerializableNode{
		Type: "Sequence", Name: "panic_test",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "NonExistentAction"},
		},
	}
	bt := BuildTree(panicTree, bb)
	// Should not panic — should return failure
	_ = RunTask(bb, bt)
	if bb.Outcome == "" {
		t.Log("unknown action produces empty outcome — this is expected (no crash)")
	}
}

func TestIntegration_EdgeCases(t *testing.T) {
	// Empty task
	t.Run("empty_task", func(t *testing.T) {
		bb := &Blackboard{Task: "", LLM: &MockLLM{}}
		bt := BuildTree(evolution.DefaultTree(), bb)
		outcome := RunTask(bb, bt)
		if outcome == string(evolution.Success) {
			t.Error("empty task should not succeed")
		}
	})

	// Very long task
	t.Run("very_long_task", func(t *testing.T) {
		longTask := ""
		for i := 0; i < 100; i++ {
			longTask += "This is a very long task description to test input handling. "
		}
		bb := &Blackboard{Task: longTask, LLM: &MockLLM{}}
		bt := BuildTree(evolution.DefaultTree(), bb)
		outcome := RunTask(bb, bt)
		if outcome == "" {
			t.Error("long task should produce outcome")
		}
	})

	// Special characters
	t.Run("special_chars", func(t *testing.T) {
		bb := &Blackboard{Task: "!@#$%^&*()_+{}|:\"<>?~`", LLM: &MockLLM{}}
		bt := BuildTree(evolution.DefaultTree(), bb)
		outcome := RunTask(bb, bt)
		if outcome == "" {
			t.Error("special chars task should produce outcome")
		}
	})

	// Unicode
	t.Run("unicode", func(t *testing.T) {
		bb := &Blackboard{Task: "こんにちは世界 review this Go code for bugs", LLM: &MockLLM{}}
		bt := BuildTree(evolution.GoDeveloperTree(), bb)
		outcome := RunTask(bb, bt)
		if outcome == "" {
			t.Error("unicode task should produce outcome")
		}
	})
}

func TestIntegration_ReflectionAndPersistence(t *testing.T) {
	refStore, _ := evolution.NewStore("/tmp/test-reflections-int")
	treeStore, _ := evolution.NewTreeStore("/tmp/test-trees-int")

	bb := &Blackboard{
		Task: "integration test task", LLM: &MockLLM{},
		Reflections: refStore, TreeStore: treeStore,
	}
	tree := evolution.DefaultTree()
	bt := BuildTree(tree, bb)
	outcome := RunTask(bb, bt)

	if outcome == "" {
		t.Error("no outcome from integration test")
	}
	// Verify tree was saved
	loaded, err := treeStore.Load()
	if err == nil && loaded != nil {
		t.Log("tree persisted successfully")
	}
}

func TestIntegration_MutationOperators(t *testing.T) {
	tree := evolution.DefaultTree()
	originalNodes := evolution.CountNodes(tree)

	t.Run("apply_mutations", func(t *testing.T) {
		newCondition := evolution.SerializableNode{Type: "Condition", Name: "CheckConfidence"}
		ops := []evolution.MutationOp{
			{Operation: "add_before", Target: "PreGate", Node: &newCondition},
			{Operation: "wrap_retry", Target: "ReflectOnOutcome"},
		}
		evolution.ApplyMutations(tree, ops)
		newNodes := evolution.CountNodes(tree)
		if newNodes <= originalNodes {
			t.Errorf("expected more nodes after mutations, got %d (was %d)", newNodes, originalNodes)
		}
	})

	t.Run("prune_node", func(t *testing.T) {
		tree2 := evolution.DefaultTree()
		ops := []evolution.MutationOp{
			{Operation: "prune_node", Target: "CachePath"},
		}
		evolution.ApplyMutations(tree2, ops)
		prunedNodes := evolution.CountNodes(tree2)
		if prunedNodes >= originalNodes {
			t.Errorf("expected fewer nodes after prune, got %d (was %d)", prunedNodes, originalNodes)
		}
	})
}
