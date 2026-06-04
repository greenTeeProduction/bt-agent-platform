package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTree_Structure(t *testing.T) {
	tree := DefaultTree()

	if tree.Type != "Sequence" {
		t.Errorf("root type: expected Sequence, got %s", tree.Type)
	}
	if tree.Name != "MainSequence" {
		t.Errorf("root name: expected MainSequence, got %s", tree.Name)
	}

	children := tree.Children
	if len(children) < 5 {
		t.Fatalf("expected at least 5 children, got %d", len(children))
	}

	// Verify PreGate
	if children[0].Name != "PreGate" || children[0].Type != "Sequence" {
		t.Errorf("child 0: expected PreGate Sequence, got %s %s", children[0].Type, children[0].Name)
	}

	// Verify StrategyRouter
	if children[1].Name != "StrategyRouter" || children[1].Type != "Selector" {
		t.Errorf("child 1: expected StrategyRouter Selector, got %s %s", children[1].Type, children[1].Name)
	}

	// Verify OutcomeSelector
	foundOutcome := false
	for _, c := range children {
		if c.Name == "OutcomeSelector" && c.Type == "Selector" {
			foundOutcome = true
			if len(c.Children) < 2 {
				t.Errorf("OutcomeSelector: expected at least 2 children, got %d", len(c.Children))
			}
		}
	}
	if !foundOutcome {
		t.Error("OutcomeSelector not found in tree children")
	}
}

func TestCountNodes(t *testing.T) {
	tree := DefaultTree()
	count := CountNodes(tree)
	if count < 20 {
		t.Errorf("default tree should have >= 20 nodes, got %d", count)
	}

	// Single node
	single := &SerializableNode{Type: "Action", Name: "Single"}
	if CountNodes(single) != 1 {
		t.Errorf("single node count: expected 1, got %d", CountNodes(single))
	}

	// Nil
	if CountNodes(nil) != 0 {
		t.Errorf("nil count: expected 0, got %d", CountNodes(nil))
	}
}

func TestTreeStore_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewTreeStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Load non-existent → nil
	tree, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if tree != nil {
		t.Error("expected nil for non-existent tree")
	}

	// Save and reload
	original := DefaultTree()
	if err := store.Save(original); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil loaded tree")
	}

	if CountNodes(loaded) != CountNodes(original) {
		t.Errorf("node count mismatch: %d vs %d", CountNodes(loaded), CountNodes(original))
	}
}

func TestTreeStore_SaveTo(t *testing.T) {
	tmpDir := t.TempDir()
	store, _ := NewTreeStore(tmpDir)

	tree := DefaultTree()
	customPath := filepath.Join(tmpDir, "agent-custom.json")

	if err := store.SaveTo(tree, customPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(customPath); os.IsNotExist(err) {
		t.Error("SaveTo: file not created")
	}
}

func TestMutation_AddBefore(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "Target", Description: "original"},
		},
	}

	newNode := SerializableNode{Type: "Condition", Name: "NewCheck", Description: "inserted"}
	ok := applyAddBefore(tree, "Target", newNode)
	if !ok {
		t.Fatal("applyAddBefore returned false")
	}
	if tree.Children[0].Name != "NewCheck" {
		t.Errorf("expected NewCheck first, got %s", tree.Children[0].Name)
	}
	if tree.Children[1].Name != "Target" {
		t.Errorf("expected Target second, got %s", tree.Children[1].Name)
	}
}

func TestMutation_AddAfter(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "Target", Description: "original"},
		},
	}

	newNode := SerializableNode{Type: "Action", Name: "FollowUp", Description: "after"}
	ok := applyAddAfter(tree, "Target", newNode)
	if !ok {
		t.Fatal("applyAddAfter returned false")
	}
	if tree.Children[0].Name != "Target" {
		t.Errorf("expected Target first, got %s", tree.Children[0].Name)
	}
	if tree.Children[1].Name != "FollowUp" {
		t.Errorf("expected FollowUp second, got %s", tree.Children[1].Name)
	}
}

func TestMutation_WrapRetry(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "FlakyAction", Description: "might fail"},
		},
	}

	ok := applyWrapRetry(tree, "FlakyAction")
	if !ok {
		t.Fatal("applyWrapRetry returned false")
	}

	wrapped := tree.Children[0]
	if wrapped.Type != "Retry" {
		t.Errorf("expected Retry type, got %s", wrapped.Type)
	}
	if wrapped.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", wrapped.MaxRetries)
	}
	if len(wrapped.Children) != 1 || wrapped.Children[0].Name != "FlakyAction" {
		t.Error("wrapped child mismatch")
	}
}

func TestMutation_AddFallback(t *testing.T) {
	// addFallback targets a Selector node. Wrap it so target is inside, not root.
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{
				Type: "Selector",
				Name: "TargetSelector",
				Children: []SerializableNode{
					{Type: "Action", Name: "Primary", Description: "try first"},
				},
			},
		},
	}

	newNode := SerializableNode{Type: "Action", Name: "FallbackAction", Description: "try second"}
	ok := applyAddFallback(tree, "TargetSelector", newNode)
	if !ok {
		t.Fatal("applyAddFallback returned false")
	}
	sel := tree.Children[0]
	if len(sel.Children) != 2 {
		t.Errorf("expected 2 children in selector, got %d", len(sel.Children))
	}
	if sel.Children[1].Name != "FallbackAction" {
		t.Errorf("expected FallbackAction, got %s", sel.Children[1].Name)
	}
}

func TestMutation_IncreaseRetries(t *testing.T) {
	// increaseRetries targets a Retry node. Wrap so target is inside, not root.
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{
				Type:       "Retry",
				Name:       "RetryNode",
				MaxRetries: 3,
				Children: []SerializableNode{
					{Type: "Action", Name: "Inner", Description: "action"},
				},
			},
		},
	}

	ok := applyIncreaseRetries(tree, "RetryNode")
	if !ok {
		t.Fatal("applyIncreaseRetries returned false")
	}
	if tree.Children[0].MaxRetries != 5 {
		t.Errorf("expected 5 retries, got %d", tree.Children[0].MaxRetries)
	}
}

func TestMutation_PruneNode(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "Keep", Description: "stay"},
			{Type: "Action", Name: "Remove", Description: "go away"},
			{Type: "Action", Name: "AlsoKeep", Description: "stay too"},
		},
	}

	ok := applyPruneNode(tree, "Remove")
	if !ok {
		t.Fatal("applyPruneNode returned false")
	}
	if len(tree.Children) != 2 {
		t.Errorf("expected 2 children after prune, got %d", len(tree.Children))
	}
	for _, c := range tree.Children {
		if c.Name == "Remove" {
			t.Error("Remove node should be gone")
		}
	}
}

func TestMutation_TargetNotFound(t *testing.T) {
	tree := DefaultTree()

	if applyAddBefore(tree, "NonExistentNode", SerializableNode{Type: "Action", Name: "X"}) {
		t.Error("expected false for non-existent target")
	}
	if applyWrapRetry(tree, "Nowhere") {
		t.Error("expected false for non-existent wrap target")
	}
}

func TestApplyMutations_Batch(t *testing.T) {
	tree := DefaultTree()
	initial := CountNodes(tree)

	ops := []MutationOp{
		{Operation: "add_before", Target: "PreGate", Node: &SerializableNode{
			Type: "Condition", Name: "ExtraCheck", Description: "added",
		}},
		{Operation: "add_fallback", Target: "OutcomeSelector", Node: &SerializableNode{
			Type: "Action", Name: "FallbackAction", Description: "fallback",
		}},
	}

	applied := ApplyMutations(tree, ops)
	if applied < 2 {
		t.Errorf("expected at least 2 mutations applied, got %d", applied)
	}

	after := CountNodes(tree)
	if after <= initial {
		t.Errorf("expected node count increase after mutations: %d → %d", initial, after)
	}
}

func TestApplyMutations_NoOpDoesNotCountAsApplied(t *testing.T) {
	tree := DefaultTree()
	initial := CountNodes(tree)

	applied := ApplyMutations(tree, []MutationOp{{Operation: "wrap_retry", Target: "MissingNode"}})
	if applied != 0 {
		t.Fatalf("expected no-op mutation to apply 0 changes, got %d", applied)
	}
	if got := CountNodes(tree); got != initial {
		t.Fatalf("no-op mutation changed tree size: got %d, want %d", got, initial)
	}
}

func TestApplyMutations_DuplicateFallbackRejected(t *testing.T) {
	tree := DefaultTree()
	op := MutationOp{Operation: "add_fallback", Target: "OutcomeSelector", Node: &SerializableNode{
		Type: "Action", Name: "DefaultFallback", Description: "fallback",
	}}

	if applied := ApplyMutations(tree, []MutationOp{op}); applied != 1 {
		t.Fatalf("expected first fallback mutation to apply once, got %d", applied)
	}
	if applied := ApplyMutations(tree, []MutationOp{op}); applied != 0 {
		t.Fatalf("expected duplicate fallback mutation to be rejected, got %d", applied)
	}
}

func TestApplyMutations_PromptToolIterationMutationsAreBounded(t *testing.T) {
	tree := &SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []SerializableNode{{
			Type: "ChainAction", Name: "Agent", Metadata: map[string]any{
				"max_iterations": float64(5),
			},
		}},
	}

	ops := []MutationOp{
		{Operation: "add_tool", Target: "Agent", Metadata: map[string]any{"recommended_tool": "file_read"}},
		{Operation: "improve_prompt", Target: "Agent", Metadata: map[string]any{"system_msg": "Verify every claim with real tool output. Never fabricate."}},
		{Operation: "increase_iterations", Target: "Agent"},
	}
	if applied := ApplyMutations(tree, ops); applied != 3 {
		t.Fatalf("expected three first-time content mutations, got %d", applied)
	}
	if applied := ApplyMutations(tree, ops); applied != 1 {
		t.Fatalf("expected only bounded iteration bump to remain applicable on second pass, got %d", applied)
	}
}
