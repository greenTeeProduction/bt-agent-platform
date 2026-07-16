package engine

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── validateNode ChainAction handling ───

// TestValidateTree_ChainAction_UnknownChainType verifies that a ChainAction
// node whose chain_type (parsed by parseChainConfig from "type:prompt" node
// names) is not one of the declared ChainKind constants fails validation at
// authoring time, before the tree ever reaches LLM-call runtime.
func TestValidateTree_ChainAction_UnknownChainType(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "totally_bogus_chain_type:do the thing",
	}
	msgs := ValidateTree(tree)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 validation message for unknown chain_type, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "totally_bogus_chain_type") {
		t.Errorf("expected message to mention the unknown chain_type, got %q", msgs[0])
	}
}

// TestValidateTree_ChainAction_KnownChainType verifies that a ChainAction
// node using a declared ChainKind (e.g. "llm_call") produces no validation
// message.
func TestValidateTree_ChainAction_KnownChainType(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "ChainAction",
		Name: "llm_call:Respond to: {{.Task}}",
	}
	msgs := ValidateTree(tree)
	if len(msgs) != 0 {
		t.Errorf("expected 0 validation messages for known chain_type, got %d: %v", len(msgs), msgs)
	}
}

// TestValidateTree_ChainAction_MixedTree verifies ChainAction validation
// composes correctly alongside other node types in a larger tree — a
// mistyped chain_type nested under a Sequence must still surface.
func TestValidateTree_ChainAction_MixedTree(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "ChainAction", Name: "llm_call:fine"},
			{Type: "ChainAction", Name: "mistyped_chain_typ:oops"},
		},
	}
	msgs := ValidateTree(tree)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 validation message for the mistyped chain_type, got %d: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "mistyped_chain_typ") {
		t.Errorf("expected message to mention the mistyped chain_type, got %q", msgs[0])
	}
}
