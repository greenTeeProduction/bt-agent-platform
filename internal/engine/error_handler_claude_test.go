// internal/engine/error_handler_claude_test.go
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
)

func TestErrorHandlerSignature_StableAndDigitInsensitive(t *testing.T) {
	bb := &Blackboard{ChainState: map[string]any{
		"last_error_category": "rate_limit",
		"last_error_node":     "CallClaude",
		"last_error":          "429 after 17 attempts at 2026-07-15T17:32:37",
	}}
	sig1 := errorHandlerSignatureFromBB(bb, "h")
	bb.ChainState["last_error"] = "429 after 99 attempts at 2026-07-16T09:00:00"
	sig2 := errorHandlerSignatureFromBB(bb, "h")
	if sig1 != sig2 {
		t.Fatalf("digit-only differences must not change the signature: %s vs %s", sig1, sig2)
	}
	if len(sig1) != 12 {
		t.Fatalf("signature length = %d, want 12", len(sig1))
	}
	bb.ChainState["last_error_category"] = "timeout"
	if errorHandlerSignatureFromBB(bb, "h") == sig1 {
		t.Fatal("different category must change the signature")
	}
}

func TestErrorHandlerSignature_FallsBackToResult(t *testing.T) {
	bb := &Blackboard{Result: "boom failure text"}
	if errorHandlerSignatureFromBB(bb, "h") == "" {
		t.Fatal("must derive a signature from bb.Result when ChainState is empty")
	}
}

func TestParseErrorHandlerProposal(t *testing.T) {
	out := "Here is my analysis.\n```json\n{\"resolvable\": true, \"node\": {\"type\": \"Sequence\", \"name\": \"Handle_x\"}}\n```\n"
	p, err := parseErrorHandlerProposal(out)
	if err != nil || !p.Resolvable || p.Node == nil || p.Node.Name != "Handle_x" {
		t.Fatalf("p=%+v err=%v", p, err)
	}
	p, err = parseErrorHandlerProposal(`{"resolvable": false, "reason": "needs new Go code"}`)
	if err != nil || p.Resolvable || p.Reason == "" {
		t.Fatalf("unresolvable parse: p=%+v err=%v", p, err)
	}
	if _, err = parseErrorHandlerProposal("no json here"); err == nil {
		t.Fatal("garbage must error")
	}
	if _, err = parseErrorHandlerProposal(`{"resolvable": true}`); err == nil {
		t.Fatal("resolvable without node must error")
	}
}

func guardedSeq(guard, action string) *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "Handle_test", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: guard},
		{Type: "Action", Name: action},
	}}
}

func TestValidateErrorHandlerProposal(t *testing.T) {
	RegisterAction("eh_validate_known_action", func(*btcore.BTContext[Blackboard]) int { return 1 })
	valid := guardedSeq("LastErrorCategoryIs:testcat", "eh_validate_known_action")
	if err := validateErrorHandlerProposal(valid, map[string]bool{}); err != nil {
		t.Fatalf("valid proposal rejected: %v", err)
	}
	cases := []struct {
		desc string
		node *evolution.SerializableNode
	}{
		{"nil node", nil},
		{"unknown action", guardedSeq("LastErrorCategoryIs:x", "definitely_not_registered_xyz")},
		{"disallowed node type", &evolution.SerializableNode{Type: "HumanApprovalGate", Name: "n"}},
		{"unknown node type", &evolution.SerializableNode{Type: "Bogus", Name: "n"}},
		{"first leaf not a guard", &evolution.SerializableNode{Type: "Sequence", Name: "n", Children: []evolution.SerializableNode{
			{Type: "Action", Name: "eh_validate_known_action"},
		}}},
		{"empty name", func() *evolution.SerializableNode {
			n := guardedSeq("LastErrorCategoryIs:x", "eh_validate_known_action")
			n.Name = ""
			return n
		}()},
		{"taken name", guardedSeq("LastErrorCategoryIs:x", "eh_validate_known_action")},
	}
	for _, tc := range cases {
		names := map[string]bool{}
		if tc.desc == "taken name" {
			names["Handle_test"] = true
		}
		if err := validateErrorHandlerProposal(tc.node, names); err == nil {
			t.Errorf("%s: expected rejection", tc.desc)
		}
	}
	// Size cap: a chain of 11 nested sequences exceeds maxProposalNodes=10.
	deep := &evolution.SerializableNode{Type: "Condition", Name: "LastErrorCategoryIs:x"}
	node := *deep
	for i := 0; i < 10; i++ {
		node = evolution.SerializableNode{Type: "Sequence", Name: "s" + strings.Repeat("x", i+1), Children: []evolution.SerializableNode{node}}
	}
	if err := validateErrorHandlerProposal(&node, map[string]bool{}); err == nil {
		t.Error("oversized/deep proposal must be rejected")
	}
}

func TestErrorHandlerConfigDefaults(t *testing.T) {
	if !errorHandlerEnabled() {
		t.Fatal("enabled by default")
	}
	t.Setenv("BT_CLAUDE_ERROR_HANDLER", "off")
	if errorHandlerEnabled() {
		t.Fatal("BT_CLAUDE_ERROR_HANDLER=off must disable")
	}
	t.Setenv("BT_ERROR_HANDLER_COOLDOWN", "90m")
	if errorHandlerCooldown() != 90*time.Minute {
		t.Fatal("cooldown env override")
	}
	t.Setenv("BT_ERROR_HANDLER_COOLDOWN", "bogus")
	if errorHandlerCooldown() != 6*time.Hour {
		t.Fatal("cooldown default on parse failure")
	}
	t.Setenv("BT_ERROR_HANDLER_MAX_NODES", "2")
	if errorHandlerMaxNodes() != 2 {
		t.Fatal("max nodes env override")
	}
}
