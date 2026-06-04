package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestDelegateToTree(t *testing.T) {
	DelegateToTreeFn = func(treeID string, bb *Blackboard) (string, error) {
		if treeID != "test:echo" {
			t.Fatalf("treeID=%q", treeID)
		}
		bb.Result = "delegated ok"
		bb.Outcome = "success"
		return bb.Result, nil
	}
	t.Cleanup(func() { DelegateToTreeFn = nil })

	bb := &Blackboard{
		Task: "hello",
		ChainState: map[string]any{
			"delegate_tree_id": "test:echo",
		},
	}
	fn := GetAction("DelegateToTree")
	if fn == nil {
		t.Fatal("DelegateToTree not registered")
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d result=%q", got, bb.Result)
	}
}

func TestMergeResults(t *testing.T) {
	bb := &Blackboard{
		Results: []string{"part-a", "part-b"},
	}
	fn := GetAction("MergeResults")
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d", got)
	}
	if bb.Result == "" || len(bb.Results) < 2 {
		t.Fatalf("result=%q results=%v", bb.Result, bb.Results)
	}
}

func TestHasDelegateTarget(t *testing.T) {
	fn := GetCondition("HasDelegateTarget")
	if fn == nil {
		t.Fatal("condition not registered")
	}
	if fn(&Blackboard{ChainState: map[string]any{"delegate_tree_id": "x"}}) != true {
		t.Fatal("expected true")
	}
	if fn(&Blackboard{ChainState: map[string]any{}}) {
		t.Fatal("expected false")
	}
}

func TestBuildDelegateBlockTree(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "HasDelegateTarget"},
			{Type: "Action", Name: "DelegateToTree"},
		},
	}
	_, err := BuildAndValidate(tree, &Blackboard{ChainState: map[string]any{"delegate_tree_id": "x"}})
	if err != nil {
		t.Fatal(err)
	}
}
