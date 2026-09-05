package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestBuildAndValidate_SubTreeRefRequiresExpander(t *testing.T) {
	old := expandTreeFn
	t.Cleanup(func() { expandTreeFn = old })
	expandTreeFn = nil

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{
				Type: "SubTreeRef",
				Name: "ref:core:pre_gate",
				Metadata: map[string]any{
					"block_id": "core:pre_gate",
				},
			},
		},
	}
	_, err := BuildAndValidate(tree, &Blackboard{Task: "x"})
	if err == nil {
		t.Fatal("expected error when SubTreeRef present without expander")
	}
}

func TestPrepareTreeForBuild_NoRefs(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Action", Name: "MarkSuccessful"}
	got, err := prepareTreeForBuild(tree)
	if err != nil {
		t.Fatal(err)
	}
	if got != tree {
		t.Fatal("expected same pointer when no SubTreeRef")
	}
}
