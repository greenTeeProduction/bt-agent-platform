package benchmark

import (
	"reflect"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestBenchmarkClonePreservesExecutableDefinition(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "controlled", Edges: []evolution.TypedEdge{{Type: evolution.EdgeQualityGate, ChildIndex: 0}}, Children: []evolution.SerializableNode{{Type: "ChainAction", Name: "llm_call", Metadata: map[string]any{"prompt": "Return the verified task result.", "max_tokens": float64(64), "nested": map[string]any{"value": true}}}}}
	cloned := cloneTree(tree)
	if !reflect.DeepEqual(tree, cloned) {
		t.Fatalf("clone discarded executable configuration: %+v", cloned)
	}
	cloned.Children[0].Metadata["nested"].(map[string]any)["value"] = false
	cloned.Edges[0].ChildIndex = 1
	if tree.Children[0].Metadata["nested"].(map[string]any)["value"] != true || tree.Edges[0].ChildIndex != 0 {
		t.Fatal("clone shares mutable controls with baseline")
	}
}

func TestTaskContractChecksActualJSONValues(t *testing.T) {
	task := TaskCase{ShouldSucceed: true, ExpectedJSON: map[string]any{"total": 42, "approved": true}}
	for _, output := range []string{`{"total":17,"approved":true}`, `{"total":42,"approved":false}`, `{"total":42}`, `{"total":"42","approved":true}`, "not JSON"} {
		if taskContractPassed(task, "success", output, 1) {
			t.Errorf("wrong result passed: %s", output)
		}
	}
	if !taskContractPassed(task, "success", "```json\n{\"total\":42,\"approved\":true}\n```", 1) {
		t.Fatal("correct result rejected")
	}
}

func TestTaskContractsRejectFalseSuccessAndAcceptExpectedRejection(t *testing.T) {
	c := TaskCase{ShouldSucceed: true, MinResultLen: 40, MinQualityScore: 0.8}
	for _, test := range []struct {
		outcome, result string
		quality         float64
		want            bool
	}{
		{"success", "short", 1, false},
		{"success", "This is a sufficiently long but low quality task result.", 0.3, false},
		{"failure", "This is a sufficiently long but failed task execution.", 1, false},
		{"success", "This is a sufficiently long and verified task result.", 0.9, true},
	} {
		if got := taskContractPassed(c, test.outcome, test.result, test.quality); got != test.want {
			t.Errorf("case %+v: got %v", test, got)
		}
	}
	negative := TaskCase{ShouldReject: true}
	if !taskContractPassed(negative, "failure", "", 0) {
		t.Error("expected rejection failed the contract")
	}
	if taskContractPassed(negative, "success", "", 1) || taskContractPassed(negative, "panic", "", 0) {
		t.Error("success or a crash is not a valid rejection")
	}
}
