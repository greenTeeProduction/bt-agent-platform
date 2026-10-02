package evolution

import (
	"encoding/json"
	"testing"
)

func TestResultContractChecksFieldsValuesAndMalformedDeclarations(t *testing.T) {
	contract, err := ParseResultContract(&SerializableNode{Metadata: map[string]any{"result_contract": map[string]any{"json_fields": map[string]any{"total": 42, "approved": true}, "required_keys": []string{"source"}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{`{"result":42,"approved":true,"source":"input"}`, `{"total":41,"approved":true,"source":"input"}`, `{"total":"42","approved":true,"source":"input"}`, `{"total":42,"approved":true}`, `null`, `not JSON`} {
		if contract.Verify(output) == nil {
			t.Errorf("invalid output accepted: %s", output)
		}
	}
	if err = contract.Verify("```json\n{\"total\":42,\"approved\":true,\"source\":\"input\"}\n```"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []any{nil, "json", map[string]any{}, map[string]any{"json_field": map[string]any{"total": 42}}, map[string]any{"required_keys": []string{""}}, map[string]any{"min_length": -1}} {
		if _, err := ParseResultContract(&SerializableNode{Metadata: map[string]any{"result_contract": raw}}); err == nil {
			t.Errorf("malformed contract accepted: %#v", raw)
		}
	}
}

func TestEvolutionPreservesDeclaredContractAndRewardsItsEnforcement(t *testing.T) {
	tree := &SerializableNode{Type: "QualityGate", Name: "Verify", Metadata: map[string]any{"result_contract": map[string]any{"json_fields": map[string]any{"total": 42}}}, Children: []SerializableNode{{Type: "ChainAction", Name: "llm_call", Metadata: map[string]any{"prompt": "Compute the task result.", "max_tokens": 128}}}}
	baseline := cloneTree(tree)
	baseline.Metadata = nil
	if AssessGovernance(tree).Score <= AssessGovernance(baseline).Score {
		t.Fatal("enforced task contract gets no additional reward")
	}
	if PreservesGovernance(tree, baseline) {
		t.Fatal("removing the task contract was accepted")
	}
	changed := cloneTree(tree)
	changed.Metadata["result_contract"] = map[string]any{"json_fields": map[string]any{"total": 17}}
	if PreservesGovernance(tree, changed) {
		t.Fatal("rewriting the expected answer was accepted")
	}
	if !PreservesGovernance(baseline, tree) {
		t.Fatal("adding a task contract should preserve prior controls")
	}
}

func TestResultContractDoesNotRoundDifferentNumbersIntoEquality(t *testing.T) {
	contract := &ResultContract{JSONFields: map[string]json.RawMessage{"total": json.RawMessage(`9007199254740993`)}}
	if contract.Verify(`{"total":9007199254740992}`) == nil {
		t.Fatal("different large integers rounded into equality")
	}
	if err := contract.Verify(`{"total":9007199254740993}`); err != nil {
		t.Fatal(err)
	}
	contract.JSONFields["total"] = json.RawMessage(`42`)
	if err := contract.Verify(`{"total":42.0}`); err != nil {
		t.Fatal(err)
	}
	if contract.Verify(`{"total":4.2e1000000000}`) == nil {
		t.Fatal("unbounded numeric exponent accepted")
	}
}
