package evolution

import (
	"encoding/json"
	"testing"

	"github.com/nico/go-bt-evolve/internal/goap"
)

func TestGoapStepModelResultsCannotAssertExternalCompletion(t *testing.T) {
	for _, fact := range []string{"deployed", "task_status", "report_saved", "result."} {
		node := &SerializableNode{Type: "GoapStep", Name: "untrusted_claim", Metadata: map[string]any{"effect_source": "result", "effects": map[string]any{fact: true}}, Children: []SerializableNode{{Type: "AlwaysSucceed"}}}
		if _, err := ParseGoapStep(node); err == nil {
			t.Fatalf("model result was allowed to assert %q", fact)
		}
	}
	node := &SerializableNode{Type: "GoapStep", Name: "calculation", Metadata: map[string]any{"effect_source": "result", "effects": map[string]any{"result.total": 42}}, Children: []SerializableNode{{Type: "AlwaysSucceed"}}}
	spec, err := ParseGoapStep(node)
	if err != nil || spec.Bindings["result.total"] != "total" {
		t.Fatalf("scoped value oracle rejected: %+v %v", spec, err)
	}
}

func TestGoapConversionPreservesLimitsEdgesAndExactNumbers(t *testing.T) {
	edge := json.RawMessage(`{"type":"guard","label":"value","condition":"value must be verified","child_index":-1}`)
	node := &goap.SerializableNode{Type: "Retry", Name: "budgeted", MaxRetries: 2, TimeoutMs: 15000, Description: "bounded executor", Edges: []json.RawMessage{edge}, Metadata: map[string]any{"effects": map[string]any{"large": json.Number("9007199254740993")}}, Children: []goap.SerializableNode{{Type: "AlwaysSucceed", Name: "leaf"}}}
	converted := FromGoapNode(node)
	if converted.MaxRetries != 2 || converted.TimeoutMs != 15000 || converted.Description != "bounded executor" || len(converted.Edges) != 1 || converted.Metadata["effects"].(map[string]any)["large"] != json.Number("9007199254740993") {
		t.Fatalf("execution definition changed: %+v", converted)
	}
}

func TestGoapStepPersistenceAndGovernancePreserveExactOracle(t *testing.T) {
	node := &SerializableNode{Type: "GoapStep", Name: "exact", Metadata: map[string]any{"effect_source": "result", "effects": map[string]any{"result.number": json.Number("9007199254740993")}}, Children: []SerializableNode{{Type: "ChainAction", Name: "llm_call:Compute the requested integer", Metadata: map[string]any{"max_tokens": 96}}}}
	data, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	var loaded SerializableNode
	if err = json.Unmarshal(data, &loaded); err != nil {
		t.Fatal(err)
	}
	spec, err := ParseGoapStep(&loaded)
	if err != nil {
		t.Fatal(err)
	}
	check := GoapCheck{Expected: spec.Effects, Observed: map[string]json.RawMessage{"result.number": json.RawMessage(`9007199254740992`)}}
	if check.Verify() == nil {
		t.Fatal("rounded number passed the persisted oracle")
	}
	before, _ := TreeVersion(node)
	after, _ := TreeVersion(&loaded)
	if before != after || !PreservesGovernance(node, &loaded) {
		t.Fatal("persistence changed the execution contract")
	}
	if PreservesGovernance(node, &node.Children[0]) {
		t.Fatal("evolution stripped the observation gate")
	}
	loaded.Metadata["effects"] = map[string]any{"result.number": json.Number("9007199254740992")}
	if PreservesGovernance(node, &loaded) {
		t.Fatal("evolution changed the required value")
	}
}
