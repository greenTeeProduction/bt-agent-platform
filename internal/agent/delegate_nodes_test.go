package agent

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestDelegateToTree(t *testing.T) {
	engine.DelegateToTreeFn = func(treeID string, bb *engine.Blackboard) (string, error) {
		if treeID != "test:echo" {
			t.Fatalf("treeID=%q", treeID)
		}
		bb.Result = "delegated ok"
		bb.Outcome = "success"
		return bb.Result, nil
	}
	t.Cleanup(func() { engine.DelegateToTreeFn = nil })

	bb := &engine.Blackboard{
		Task: "hello",
		ChainState: map[string]any{
			"delegate_tree_id": "test:echo",
		},
	}
	fn := engine.GetAction("DelegateToTree")
	if fn == nil {
		t.Fatal("DelegateToTree not registered")
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d result=%q", got, bb.Result)
	}
}

func TestMergeResults(t *testing.T) {
	bb := &engine.Blackboard{
		Results: []string{"part-a", "part-b"},
	}
	fn := engine.GetAction("MergeResults")
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d", got)
	}
	if bb.Result == "" || len(bb.Results) < 2 {
		t.Fatalf("result=%q results=%v", bb.Result, bb.Results)
	}
}

func TestHasDelegateTarget(t *testing.T) {
	fn := engine.GetCondition("HasDelegateTarget")
	if fn == nil {
		t.Fatal("condition not registered")
	}
	if fn(&engine.Blackboard{ChainState: map[string]any{"delegate_tree_id": "x"}}) != true {
		t.Fatal("expected true")
	}
	if fn(&engine.Blackboard{ChainState: map[string]any{}}) {
		t.Fatal("expected false")
	}
}

// Characterization tests for PrepareA2AHandoff (registered in
// delegate_nodes.go:124). They pin the current mapping/validation behavior:
// a2a_url (when non-empty) is copied into a2a_target_url, and the action
// fails unless a2a_target_url ends up set to a non-empty string.

func TestPrepareA2AHandoff_MapsA2AURLToTargetURL(t *testing.T) {
	bb := &engine.Blackboard{
		ChainState: map[string]any{"a2a_url": "http://agent.example/a2a"},
	}
	fn := engine.GetAction("PrepareA2AHandoff")
	if fn == nil {
		t.Fatal("PrepareA2AHandoff not registered")
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d result=%q", got, bb.Result)
	}
	if bb.ChainState["a2a_target_url"] != "http://agent.example/a2a" {
		t.Fatalf("a2a_target_url = %v, want mapped a2a_url", bb.ChainState["a2a_target_url"])
	}
}

func TestPrepareA2AHandoff_UsesExistingTargetURL(t *testing.T) {
	bb := &engine.Blackboard{
		ChainState: map[string]any{"a2a_target_url": "http://agent.example/existing"},
	}
	fn := engine.GetAction("PrepareA2AHandoff")
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d result=%q", got, bb.Result)
	}
	if bb.ChainState["a2a_target_url"] != "http://agent.example/existing" {
		t.Fatalf("a2a_target_url = %v, want unchanged", bb.ChainState["a2a_target_url"])
	}
}

func TestPrepareA2AHandoff_EmptyA2AURLDoesNotOverwriteTarget(t *testing.T) {
	bb := &engine.Blackboard{
		ChainState: map[string]any{
			"a2a_url":        "",
			"a2a_target_url": "http://agent.example/keep",
		},
	}
	fn := engine.GetAction("PrepareA2AHandoff")
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != 1 {
		t.Fatalf("code=%d result=%q", got, bb.Result)
	}
	if bb.ChainState["a2a_target_url"] != "http://agent.example/keep" {
		t.Fatalf("a2a_target_url = %v, want unchanged", bb.ChainState["a2a_target_url"])
	}
}

func TestPrepareA2AHandoff_MissingBothURLsFails(t *testing.T) {
	bb := &engine.Blackboard{ChainState: map[string]any{}}
	fn := engine.GetAction("PrepareA2AHandoff")
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != -1 {
		t.Fatalf("code=%d, want -1", got)
	}
	if bb.Outcome != "failure" {
		t.Errorf("outcome = %q, want failure", bb.Outcome)
	}
	if bb.Result == "" {
		t.Error("expected explanatory Result on failure")
	}
}

func TestPrepareA2AHandoff_NilChainStateFails(t *testing.T) {
	bb := &engine.Blackboard{}
	fn := engine.GetAction("PrepareA2AHandoff")
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := fn(ctx); got != -1 {
		t.Fatalf("code=%d, want -1", got)
	}
	if bb.ChainState == nil {
		t.Fatal("expected ChainState to be initialized even on failure")
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
	_, err := engine.BuildAndValidate(tree, &engine.Blackboard{ChainState: map[string]any{"delegate_tree_id": "x"}})
	if err != nil {
		t.Fatal(err)
	}
}
