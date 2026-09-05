package blocks

import (
	"testing"

	_ "github.com/nico/go-bt-evolve/internal/agent" // registers PrepareA2AHandoff
	"github.com/nico/go-bt-evolve/internal/engine"
)

// Characterization tests for a2a.go's A2AHandoffBlock(). These pin the exact
// node structure (PrepareA2AHandoff -> HasA2ATarget -> HumanApprovalGate ->
// DelegateToA2A) and confirm the tree builds against the live action/condition
// registries. They exist to lock in observable behavior before future
// refactors; they make no production changes.

func TestA2AHandoffBlock_Structure(t *testing.T) {
	n := A2AHandoffBlock()

	if n.Type != "Sequence" {
		t.Errorf("root type = %q, want %q", n.Type, "Sequence")
	}
	if n.Name != "A2AHandoff" {
		t.Errorf("root name = %q, want %q", n.Name, "A2AHandoff")
	}
	if len(n.Children) != 3 {
		t.Fatalf("root children = %d, want 3", len(n.Children))
	}

	prepare := n.Children[0]
	if prepare.Type != "Action" || prepare.Name != "PrepareA2AHandoff" {
		t.Errorf("child[0] = %s/%s, want Action/PrepareA2AHandoff", prepare.Type, prepare.Name)
	}

	hasTarget := n.Children[1]
	if hasTarget.Type != "Condition" || hasTarget.Name != "HasA2ATarget" {
		t.Errorf("child[1] = %s/%s, want Condition/HasA2ATarget", hasTarget.Type, hasTarget.Name)
	}

	gate := n.Children[2]
	if gate.Type != "HumanApprovalGate" || gate.Name != "A2AApproval" {
		t.Errorf("child[2] = %s/%s, want HumanApprovalGate/A2AApproval", gate.Type, gate.Name)
	}
	if prompt, _ := gate.Metadata["prompt"].(string); prompt == "" {
		t.Error("gate prompt metadata is empty")
	}
	if sec, _ := gate.Metadata["side_effect_class"].(string); sec != "external" {
		t.Errorf("gate side_effect_class = %q, want %q", sec, "external")
	}
	if len(gate.Children) != 1 {
		t.Fatalf("gate children = %d, want 1", len(gate.Children))
	}
	delegate := gate.Children[0]
	if delegate.Type != "Action" || delegate.Name != "DelegateToA2A" {
		t.Errorf("gate child = %s/%s, want Action/DelegateToA2A", delegate.Type, delegate.Name)
	}
}

// TestA2AHandoffBlock_BuildAndValidate pins that the tree builds cleanly
// against the live registries: HasA2ATarget/DelegateToA2A come from
// internal/engine's own init(), PrepareA2AHandoff from internal/agent's.
func TestA2AHandoffBlock_BuildAndValidate(t *testing.T) {
	n := A2AHandoffBlock()
	bb := &engine.Blackboard{
		Task: "hand off to external agent",
		ChainState: map[string]any{
			"a2a_target_url": "http://example.invalid/a2a",
		},
	}
	if _, err := engine.BuildAndValidate(&n, bb); err != nil {
		t.Fatalf("BuildAndValidate() error = %v", err)
	}
}
