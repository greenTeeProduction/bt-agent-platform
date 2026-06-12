package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func TestObserveNode_RecordsMetrics(t *testing.T) {
	var ticks int
	prev := RecordNodeTickFn
	RecordNodeTickFn = func(_, _, _, _, _ string, _ int64) { ticks++ }
	t.Cleanup(func() { RecordNodeTickFn = prev })

	node := &evolution.SerializableNode{Type: "Action", Name: "MarkSuccessful"}
	child := btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { return 1 })
	cmd := observeNode(node, "Parent", child)

	bb := &Blackboard{ChainState: make(map[string]any)}
	ctx := btcore.NewBTContext(t.Context(), bb)
	if cmd.Run(ctx) != 1 {
		t.Fatal("expected success")
	}
	if ticks == 0 {
		t.Fatal("expected node metrics to be recorded")
	}
}

func TestBuildTree_ObservesNodes(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "MarkSuccessful"},
		},
	}
	bb := &Blackboard{ChainState: make(map[string]any)}
	cmd, err := BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatal(err)
	}
	ctx := btcore.NewBTContext(t.Context(), bb)
	_ = cmd.Run(ctx)
}
