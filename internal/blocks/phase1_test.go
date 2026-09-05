package blocks

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestRegistry_Phase1Blocks(t *testing.T) {
	reg := NewRegistry("")
	for _, id := range []string{
		"core:plan",
		"core:rag_gate",
		"core:clarify_gate",
		"core:quality_gate",
		"core:strategy_router",
	} {
		if reg.Get(id) == nil {
			t.Fatalf("missing block %s", id)
		}
	}
}

func TestComposeTaskTreeAgentic(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposeTaskTreeAgentic(reg, "Agentic_Main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != len(DefaultTaskBlocksAgentic) {
		t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocksAgentic), len(tree.Children))
	}
	expanded, err := Expand(reg, tree)
	if err != nil {
		t.Fatal(err)
	}
	if HasSubTreeRefs(expanded) {
		t.Fatal("expected expanded tree without SubTreeRef")
	}
}

func TestComposeTaskTreeFull(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposeTaskTreeFull(reg, "Full_Main", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Children) != len(DefaultTaskBlocksFull) {
		t.Fatalf("expected %d refs, got %d", len(DefaultTaskBlocksFull), len(tree.Children))
	}
}

func TestComposePreset_Agentic(t *testing.T) {
	reg := NewRegistry("")
	tree, err := ComposePreset(reg, "agentic", "PresetTest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Name != "PresetTest" {
		t.Errorf("name=%q", tree.Name)
	}
}

func TestComposeTaskTreeAgentic_WithStrategyMiddle(t *testing.T) {
	reg := NewRegistry("")
	strategy := &evolution.SerializableNode{
		Type: "Selector",
		Name: "StrategyRouter",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	tree, err := ComposeTaskTreeAgentic(reg, "Agentic_Strategy", strategy)
	if err != nil {
		t.Fatal(err)
	}
	// pre, plan, tools, strategy, tool, quality, error = 7 refs
	if len(tree.Children) != 7 {
		t.Fatalf("expected 7 children, got %d", len(tree.Children))
	}
}

func TestPlanBlock_HasPlanCondition(t *testing.T) {
	reg := NewRegistry("")
	b := reg.Get("core:plan")
	if b == nil || b.Tree == nil {
		t.Fatal("plan block missing")
	}
	found := false
	var walk func(*evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n == nil {
			return
		}
		if n.Type == "Condition" && n.Name == "HasPlan" {
			found = true
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(b.Tree)
	if !found {
		t.Fatal("expected HasPlan condition in plan block")
	}
}
