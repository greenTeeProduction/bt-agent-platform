package blocks

import (
	"testing"

	_ "github.com/nico/go-bt-evolve/internal/agent" // registers HasDelegateTarget/DelegateToTree
	"github.com/nico/go-bt-evolve/internal/engine"
)

// Characterization tests for delegate.go's DelegateBlock(). These pin the
// exact node structure (HasDelegateTarget -> HumanApprovalGate -> DelegateToTree)
// and confirm the tree builds against the live action/condition registries.
// They exist to lock in observable behavior before future refactors; they
// make no production changes unless a real bug is found.

func TestDelegateBlock_Structure(t *testing.T) {
	n := DelegateBlock()

	if n.Type != "Sequence" {
		t.Errorf("root type = %q, want %q", n.Type, "Sequence")
	}
	if n.Name != "Delegate" {
		t.Errorf("root name = %q, want %q", n.Name, "Delegate")
	}
	if len(n.Children) != 2 {
		t.Fatalf("root children = %d, want 2", len(n.Children))
	}

	hasTarget := n.Children[0]
	if hasTarget.Type != "Condition" || hasTarget.Name != "HasDelegateTarget" {
		t.Errorf("child[0] = %s/%s, want Condition/HasDelegateTarget", hasTarget.Type, hasTarget.Name)
	}

	gate := n.Children[1]
	if gate.Type != "HumanApprovalGate" || gate.Name != "DelegateApproval" {
		t.Errorf("child[1] = %s/%s, want HumanApprovalGate/DelegateApproval", gate.Type, gate.Name)
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
	if delegate.Type != "Action" || delegate.Name != "DelegateToTree" {
		t.Errorf("gate child = %s/%s, want Action/DelegateToTree", delegate.Type, delegate.Name)
	}
}

// TestDelegateBlock_BuildAndValidate pins that the tree builds cleanly against
// the live registries: HasDelegateTarget/DelegateToTree come from
// internal/agent's own init(). The gate's side_effect_class="external"
// metadata sits on the HumanApprovalGate node itself, mirroring
// A2AHandoffBlock's gate-scoped placement, so ValidateTreeFull accepts it.
func TestDelegateBlock_BuildAndValidate(t *testing.T) {
	n := DelegateBlock()
	bb := &engine.Blackboard{
		Task: "delegate to another tree",
		ChainState: map[string]any{
			"delegate_tree_id": "worker-tree",
		},
	}
	if _, err := engine.BuildAndValidate(&n, bb); err != nil {
		t.Fatalf("BuildAndValidate() error = %v", err)
	}
}
