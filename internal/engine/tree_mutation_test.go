// internal/engine/tree_mutation_test.go
package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func mkTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "ApplyFix"},
			{Type: "Selector", Name: "sel", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "ApplyKnowledge"},
				{Type: "Action", Name: "ChooseAction"},
			}},
		},
	}
}

func TestCloneNode(t *testing.T) {
	src := mkTree()
	dst := cloneNode(src)
	if dst == src || &dst.Children[1] == &src.Children[1] {
		t.Fatal("clone must not alias source")
	}
	dst.Children[1].Children[0].Name = "changed"
	if src.Children[1].Children[0].Name != "ApplyKnowledge" {
		t.Fatal("mutating clone must not touch source")
	}
	// Metadata map must be copied, not aliased.
	src2 := &evolution.SerializableNode{Type: "Action", Name: "m",
		Metadata: map[string]any{"k": "v"}}
	dst2 := cloneNode(src2)
	dst2.Metadata["k"] = "w"
	if src2.Metadata["k"] != "v" {
		t.Fatal("metadata map aliased between source and clone")
	}
	// Edges Blackboard map must be deep-copied, not aliased.
	src3 := &evolution.SerializableNode{Type: "Sequence", Name: "seq",
		Edges: []evolution.TypedEdge{{
			Type:       evolution.EdgeChild,
			ChildIndex: 0,
			Label:      "test_edge",
			Blackboard: map[string]string{"k": "v"},
		}}}
	dst3 := cloneNode(src3)
	dst3.Edges[0].Blackboard["k"] = "w"
	if src3.Edges[0].Blackboard["k"] != "v" {
		t.Fatal("edge blackboard map aliased between source and clone")
	}
}

func TestMapCorrespondenceIdentityWalk(t *testing.T) {
	old := mkTree()
	nw := cloneNode(old)
	corr := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(old, nw, nil, "", 0, corr)
	if corr[old] != nw || corr[&old.Children[1]] != &nw.Children[1] ||
		corr[&old.Children[1].Children[0]] != &nw.Children[1].Children[0] {
		t.Fatal("identity walk must pair every node 1:1")
	}
}

func TestMapCorrespondenceAfterAddSurvivesRealloc(t *testing.T) {
	// The regression this guards: correspondence captured during the clone
	// goes stale when applyMutationOp's insert reallocates the parent's
	// children backing array. Correspondence must therefore be computed
	// AFTER the op, via this dual walk with shift arithmetic.
	old := mkTree()
	nw := cloneNode(old)
	parent, at, err := applyMutationOp(nw, MutationOp{
		Kind: "add", ParentPath: "1", Index: 0,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "BuildCompsTable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	corr := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(old, nw, parent, "add", at, corr)
	// Shifted siblings ApplyKnowledge (old 1.0 → new 1.1) and ChooseAction (old 1.1 → new 1.2)
	// must pair with their POST-reallocation addresses.
	if corr[&old.Children[1].Children[0]] != &nw.Children[1].Children[1] {
		t.Fatal("sibling ApplyKnowledge must pair across the insert shift")
	}
	if corr[&old.Children[1].Children[1]] != &nw.Children[1].Children[2] {
		t.Fatal("sibling ChooseAction must pair across the insert shift")
	}
	// The inserted node has no old counterpart.
	for _, nwNode := range corr {
		if nwNode == &nw.Children[1].Children[0] {
			t.Fatal("inserted node must not be paired")
		}
	}
}

func TestMapCorrespondenceAfterRemove(t *testing.T) {
	old := mkTree()
	nw := cloneNode(old)
	parent, at, err := applyMutationOp(nw, MutationOp{Kind: "remove", Path: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	corr := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(old, nw, parent, "remove", at, corr)
	if corr[&old.Children[1].Children[1]] != &nw.Children[1].Children[0] {
		t.Fatal("sibling ChooseAction must pair across the removal shift")
	}
	if corr[&old.Children[1].Children[0]] != nil {
		t.Fatal("removed node must not be paired")
	}
}

func TestApplyMutationOpAdd(t *testing.T) {
	root := mkTree()
	parent, idx, err := applyMutationOp(root, MutationOp{
		Kind: "add", ParentPath: "1", Index: 1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "BuildCompsTable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Name != "sel" || idx != 1 {
		t.Fatalf("want parent sel idx 1, got %s idx %d", parent.Name, idx)
	}
	got := []string{root.Children[1].Children[0].Name, root.Children[1].Children[1].Name, root.Children[1].Children[2].Name}
	if got[0] != "ApplyKnowledge" || got[1] != "BuildCompsTable" || got[2] != "ChooseAction" {
		t.Fatalf("insertion order wrong: %v", got)
	}
	// Append with Index -1.
	if _, idx, err = applyMutationOp(root, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "AssemblePitchDeck"}}); err != nil || idx != 2 {
		t.Fatalf("append: idx=%d err=%v", idx, err)
	}
	if root.Children[2].Name != "AssemblePitchDeck" {
		t.Fatal("append did not land at end of root children")
	}
}

func TestApplyMutationOpRemove(t *testing.T) {
	root := mkTree()
	parent, idx, err := applyMutationOp(root, MutationOp{Kind: "remove", Path: "1.0", ExpectName: "ApplyKnowledge"})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Name != "sel" || idx != 0 || len(root.Children[1].Children) != 1 || root.Children[1].Children[0].Name != "ChooseAction" {
		t.Fatal("remove 1.0 must delete node ApplyKnowledge")
	}
}

func TestApplyMutationOpRejections(t *testing.T) {
	cases := []struct {
		name string
		op   MutationOp
		want string
	}{
		{"remove root", MutationOp{Kind: "remove", Path: ""}, "root"},
		{"bad path", MutationOp{Kind: "remove", Path: "9.9"}, "resolve"},
		{"expect name mismatch", MutationOp{Kind: "remove", Path: "0", ExpectName: "zzz"}, "expect"},
		{"add under leaf", MutationOp{Kind: "add", ParentPath: "0", Index: -1,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "ApplyKnowledge"}}, "children"},
		{"nil subtree", MutationOp{Kind: "add", ParentPath: "", Index: -1}, "subtree"},
		{"subtree with SubTreeRef", MutationOp{Kind: "add", ParentPath: "", Index: -1,
			Subtree: &evolution.SerializableNode{Type: "SubTreeRef", Name: "blk"}}, "SubTreeRef"},
		{"unknown kind", MutationOp{Kind: "replace", Path: "0"}, "kind"},
		{"index out of range", MutationOp{Kind: "add", ParentPath: "", Index: 7,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "ApplyKnowledge"}}, "index"},
	}
	for _, tc := range cases {
		root := mkTree()
		if _, _, err := applyMutationOp(root, tc.op); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
			t.Errorf("%s: want error containing %q, got %v", tc.name, tc.want, err)
		}
	}
}

func TestApplyMutationOpDecoratorCap(t *testing.T) {
	root := &evolution.SerializableNode{Type: "Retry", Name: "r", MaxRetries: 2,
		Children: []evolution.SerializableNode{{Type: "Action", Name: "ApplyFix"}}}
	if _, _, err := applyMutationOp(root, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "ApplyKnowledge"}}); err == nil {
		t.Fatal("adding a second child to a single-child decorator must be rejected")
	}

	// QualityGate accepts up to 2 children (primary + recovery).
	qg := &evolution.SerializableNode{Type: "QualityGate", Name: "qg",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "Primary"}}}
	if _, _, err := applyMutationOp(qg, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Recovery"}}); err != nil {
		t.Fatalf("QualityGate should accept 2nd child (recovery): %v", err)
	}
	if _, _, err := applyMutationOp(qg, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Third"}}); err == nil {
		t.Fatal("QualityGate should reject 3rd child")
	}

	// ForEachTask requires exactly 1 child.
	fe := &evolution.SerializableNode{Type: "ForEachTask", Name: "fe",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "Template"}}}
	if _, _, err := applyMutationOp(fe, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Second"}}); err == nil {
		t.Fatal("ForEachTask should reject 2nd child")
	}

	// ReviewCycle requires exactly 1 child.
	rc := &evolution.SerializableNode{Type: "ReviewCycle", Name: "rc",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "WorkItem"}}}
	if _, _, err := applyMutationOp(rc, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Second"}}); err == nil {
		t.Fatal("ReviewCycle should reject 2nd child")
	}

	// AbortOnEvent requires exactly 1 child.
	ae := &evolution.SerializableNode{Type: "AbortOnEvent", Name: "ae",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "WorkItem"}}}
	if _, _, err := applyMutationOp(ae, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Second"}}); err == nil {
		t.Fatal("AbortOnEvent should reject 2nd child")
	}

	// HumanApprovalGate accepts any number of children (gets wrapped in Sequence).
	ha := &evolution.SerializableNode{Type: "HumanApprovalGate", Name: "ha",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "Action1"}}}
	if _, _, err := applyMutationOp(ha, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Action2"}}); err != nil {
		t.Fatalf("HumanApprovalGate should accept 2nd child: %v", err)
	}
	if _, _, err := applyMutationOp(ha, MutationOp{Kind: "add", ParentPath: "", Index: -1,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "Action3"}}); err != nil {
		t.Fatalf("HumanApprovalGate should accept 3rd child: %v", err)
	}
}

func TestApplyMutationOpShiftsEdgeIndices(t *testing.T) {
	t.Run("add shifts edges at or after the insertion point up", func(t *testing.T) {
		root := &evolution.SerializableNode{
			Type: "Selector", Name: "root",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "A"},
				{Type: "Action", Name: "B"},
				{Type: "Action", Name: "C"},
			},
			Edges: []evolution.TypedEdge{
				{ChildIndex: 0, Label: "before"},
				{ChildIndex: 1, Label: "at"},
				{ChildIndex: 2, Label: "after"},
			},
		}
		if _, _, err := applyMutationOp(root, MutationOp{
			Kind: "add", ParentPath: "", Index: 1,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "New"},
		}); err != nil {
			t.Fatal(err)
		}
		if got := root.Edges[0].ChildIndex; got != 0 {
			t.Errorf("edge before insertion point: want ChildIndex 0, got %d", got)
		}
		if got := root.Edges[1].ChildIndex; got != 2 {
			t.Errorf("edge at insertion point: want ChildIndex 2, got %d", got)
		}
		if got := root.Edges[2].ChildIndex; got != 3 {
			t.Errorf("edge after insertion point: want ChildIndex 3, got %d", got)
		}
	})

	t.Run("remove shifts edges down and invalidates out-of-range indices", func(t *testing.T) {
		root := &evolution.SerializableNode{
			Type: "Selector", Name: "root",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "A"},
				{Type: "Action", Name: "B"},
				{Type: "Action", Name: "C"},
			},
			Edges: []evolution.TypedEdge{
				{ChildIndex: 0, Label: "before"},
				{ChildIndex: 1, Label: "at-removed"},
				{ChildIndex: 3, Label: "dangling"},
			},
		}
		if _, _, err := applyMutationOp(root, MutationOp{Kind: "remove", Path: "1"}); err != nil {
			t.Fatal(err)
		}
		if got := root.Edges[0].ChildIndex; got != 0 {
			t.Errorf("edge before removal point: want untouched ChildIndex 0, got %d", got)
		}
		if got := root.Edges[1].ChildIndex; got != 0 {
			t.Errorf("edge at removal point: want ChildIndex shifted to 0, got %d", got)
		}
		if got := root.Edges[2].ChildIndex; got != -1 {
			t.Errorf("edge shifted out of [0, len(Children)) range: want invalidation to -1, got %d", got)
		}
	})
}

func TestValidateMutatedTreeLLMAllowlist(t *testing.T) {
	root := mkTree()
	// Operator-origin: a plain action subtree is fine.
	if err := validateMutatedTree(root, MutationOp{Kind: "add", Origin: OriginOperator,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "ApplyKnowledge"}}); err != nil {
		t.Fatalf("operator origin should pass: %v", err)
	}
	// LLM-origin: same subtree fails the proposal policy (no Condition guard).
	if err := validateMutatedTree(root, MutationOp{Kind: "add", Origin: OriginLLM,
		Subtree: &evolution.SerializableNode{Type: "Action", Name: "ApplyKnowledge"}}); err == nil {
		t.Fatal("llm origin without guard-first structure must be rejected")
	}
}
