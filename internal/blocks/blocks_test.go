package blocks

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestRegistryBuiltinBlocks(t *testing.T) {
	reg := NewRegistry("")
	ids := reg.IDs()
	if len(ids) < 10 {
		t.Fatalf("expected at least 4 builtin blocks, got %d", len(ids))
	}
	for _, id := range []string{"core:pre_gate", "core:tool_execution", "core:error_handling"} {
		if reg.Get(id) == nil {
			t.Fatalf("missing block %s", id)
		}
	}
}

func TestComposeAndExpand(t *testing.T) {
	reg := NewRegistry("")
	tree, err := Compose(reg, ComposeSpec{
		Name:   "TestComposed",
		Blocks: []string{"core:pre_gate", "core:tool_execution", "core:error_handling"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !HasSubTreeRefs(tree) {
		t.Fatal("expected SubTreeRef nodes before expand")
	}
	expanded, err := Expand(reg, tree)
	if err != nil {
		t.Fatal(err)
	}
	if HasSubTreeRefs(expanded) {
		t.Fatal("expected no SubTreeRef after expand")
	}
	if expanded.Name != "TestComposed" {
		t.Errorf("root name = %q", expanded.Name)
	}
	if len(expanded.Children) < 3 {
		t.Errorf("expected expanded children >= 3, got %d", len(expanded.Children))
	}
}

func TestComposeTaskTreeWithStrategy(t *testing.T) {
	reg := NewRegistry("")
	strategy := &evolution.SerializableNode{
		Type: "Selector",
		Name: "StrategyRouter",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	tree, err := ComposeTaskTree(reg, "Task_Main", strategy)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != 4 {
		t.Fatalf("expected 4 children (pre, strategy, tool, error), got %d", len(tree.Children))
	}
}

func TestBlockMutationsInsert(t *testing.T) {
	reg := NewRegistry("")
	tree := evolution.DefaultTree()
	op := evolution.MutationOp{
		Operation: "insert_block_after",
		Target:    "PreGate",
		Node:      ptr(SubTreeRefNode("core:reflect_only")),
	}
	if !ApplyBlockMutations(reg, tree, op) {
		t.Fatal("insert_block_after not applied")
	}
}

func TestPromoteSubtree(t *testing.T) {
	reg := NewRegistry("")
	tree := evolution.DefaultTree()
	b, err := PromoteSubtree(reg, tree, "PreGate", "custom:promoted_pregate")
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != "custom:promoted_pregate" {
		t.Errorf("id = %s", b.ID)
	}
	if reg.Get("custom:promoted_pregate") == nil {
		t.Fatal("promoted block not in registry")
	}
}

func ptr(n evolution.SerializableNode) *evolution.SerializableNode { return &n }

func TestWrapReliable_HasTimeoutAndFallbacks(t *testing.T) {
	child := evolution.SerializableNode{Type: "Action", Name: "MarkSuccessful"}
	wrapped := WrapReliable("ToolExecution", child, SpecToolExecution)
	if wrapped.Type != "Selector" {
		t.Fatalf("expected Selector root, got %s", wrapped.Type)
	}
	foundTimeout := false
	var walk func(n evolution.SerializableNode)
	walk = func(n evolution.SerializableNode) {
		if n.Type == "Timeout" {
			foundTimeout = true
		}
		for i := range n.Children {
			walk(n.Children[i])
		}
	}
	walk(wrapped)
	if !foundTimeout {
		t.Fatal("expected Timeout decorator in reliable wrapper")
	}
}

func TestBuiltinBlocks_HaveReliabilityWrappers(t *testing.T) {
	reg := NewRegistry("")
	b := reg.Get("core:tool_execution")
	if b == nil || b.Tree == nil {
		t.Fatal("tool_execution block missing")
	}
	if b.Tree.Name != "ToolExecution_Reliable" {
		t.Errorf("expected reliable wrapper root, got %q", b.Tree.Name)
	}
}
