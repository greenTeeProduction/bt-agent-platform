package blocks

import (
	"slices"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Characterization tests for clarify.go. These pin the *current* exported
// behavior of ClarifyGateBlock — the raw (un-wrapped) tree it returns, before
// any reliability wrapping the registry layers on top — so a future refactor
// that reshapes the clarify gate is caught. They make no production changes.

// clarifyLeafNames returns the depth-first (pre-order) names of the leaf nodes
// (those without children) in the given tree.
func clarifyLeafNames(n evolution.SerializableNode) []string {
	if len(n.Children) == 0 {
		return []string{n.Name}
	}
	var names []string
	for i := range n.Children {
		names = append(names, clarifyLeafNames(n.Children[i])...)
	}
	return names
}

// clarifyNodeCount returns the total number of nodes (root plus all
// descendants) in the given tree.
func clarifyNodeCount(n evolution.SerializableNode) int {
	count := 1
	for i := range n.Children {
		count += clarifyNodeCount(n.Children[i])
	}
	return count
}

// TestClarifyGateBlock_Root pins the root selector's identity and arity.
func TestClarifyGateBlock_Root(t *testing.T) {
	root := ClarifyGateBlock()
	if root.Type != "Selector" {
		t.Errorf("root type = %q, want %q", root.Type, "Selector")
	}
	if root.Name != "ClarifyGate" {
		t.Errorf("root name = %q, want %q", root.Name, "ClarifyGate")
	}
	if root.Description != "Clarify ambiguous tasks before execution" {
		t.Errorf("root description = %q, want %q", root.Description, "Clarify ambiguous tasks before execution")
	}
	if len(root.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(root.Children))
	}
}

// TestClarifyGateBlock_Branches pins, table-driven, the two selector branches
// and every leaf they contain: type, name, and description in order.
func TestClarifyGateBlock_Branches(t *testing.T) {
	type leaf struct {
		typ, name, desc string
	}
	branches := []struct {
		typ, name string
		leaves    []leaf
	}{
		{
			typ:  "Sequence",
			name: "NeedClarify",
			leaves: []leaf{
				{"Condition", "IsAmbiguousQuery", "Task is underspecified"},
				{"Action", "AskClarifyingQuestions", "Emit clarifying questions"},
				{"Action", "MarkSuccessful", "Await user clarification"},
			},
		},
		{
			typ:  "Sequence",
			name: "ClearTask",
			leaves: []leaf{
				{"Action", "ProceedDirectly", "Task is clear enough to continue"},
			},
		},
	}

	root := ClarifyGateBlock()
	if len(root.Children) != len(branches) {
		t.Fatalf("root children = %d, want %d", len(root.Children), len(branches))
	}
	for i, want := range branches {
		got := root.Children[i]
		t.Run(want.name, func(t *testing.T) {
			if got.Type != want.typ {
				t.Errorf("branch type = %q, want %q", got.Type, want.typ)
			}
			if got.Name != want.name {
				t.Errorf("branch name = %q, want %q", got.Name, want.name)
			}
			if got.Description != "" {
				t.Errorf("branch description = %q, want empty", got.Description)
			}
			if len(got.Children) != len(want.leaves) {
				t.Fatalf("branch children = %d, want %d", len(got.Children), len(want.leaves))
			}
			for j, wl := range want.leaves {
				gl := got.Children[j]
				if gl.Type != wl.typ {
					t.Errorf("leaf[%d] type = %q, want %q", j, gl.Type, wl.typ)
				}
				if gl.Name != wl.name {
					t.Errorf("leaf[%d] name = %q, want %q", j, gl.Name, wl.name)
				}
				if gl.Description != wl.desc {
					t.Errorf("leaf[%d] description = %q, want %q", j, gl.Description, wl.desc)
				}
				if len(gl.Children) != 0 {
					t.Errorf("leaf[%d] %q has %d children, want 0", j, gl.Name, len(gl.Children))
				}
			}
		})
	}
}

// TestClarifyGateBlock_NodeCount pins the total node count of the tree.
func TestClarifyGateBlock_NodeCount(t *testing.T) {
	const wantClarifyNodeCount = 7
	if got := clarifyNodeCount(ClarifyGateBlock()); got != wantClarifyNodeCount {
		t.Fatalf("ClarifyGateBlock node count = %d, want %d", got, wantClarifyNodeCount)
	}
}

// TestClarifyGateBlock_LeafOrder pins the depth-first (pre-order) order of leaf
// node names: the NeedClarify branch's three leaves first, then the ClearTask
// branch's single leaf.
func TestClarifyGateBlock_LeafOrder(t *testing.T) {
	wantClarifyLeafOrder := []string{
		"IsAmbiguousQuery",
		"AskClarifyingQuestions",
		"MarkSuccessful",
		"ProceedDirectly",
	}
	got := clarifyLeafNames(ClarifyGateBlock())
	if !slices.Equal(got, wantClarifyLeafOrder) {
		t.Fatalf("ClarifyGateBlock leaf order = %v, want %v", got, wantClarifyLeafOrder)
	}
}
