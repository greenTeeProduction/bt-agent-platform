package evolution

import (
	"encoding/json"
	"testing"
)

func TestCheckpointContractPreservesTypedGOAPFacts(t *testing.T) {
	tree := WrapWithCheckpointVerifier(&SerializableNode{Type: "AlwaysSucceed", Name: "Task"}, 2,
		"has_result=true,task_status=completed,count=9007199254740993,ready=false")
	dir := t.TempDir()
	if _, err := SaveNamedTree(dir, tree.Name, tree); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadNamedTree(dir, tree.Name)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := ParseCheckpointContract(loaded)
	if err != nil || contract.StateKey != "goap_world_state" {
		t.Fatalf("contract=%+v err=%v", contract, err)
	}
	state := map[string]any{"has_result": true, "task_status": "completed", "count": json.Number("9007199254740993"), "ready": false}
	if err := contract.Verify(state); err != nil {
		t.Fatal(err)
	}
	state["count"] = json.Number("9007199254740992")
	if contract.Verify(state) == nil {
		t.Fatal("rounded neighboring integer passed")
	}
	state["count"], state["task_status"] = json.Number("9007199254740993"), false
	if contract.Verify(state) == nil {
		t.Fatal("string postcondition was coerced to false")
	}
	state["task_status"] = "completed"
	delete(state, "ready")
	if contract.Verify(state) == nil {
		t.Fatal("missing false fact passed")
	}
}

func TestCheckpointContractRejectsMalformedOrWeakenedDeclarations(t *testing.T) {
	for _, spec := range []string{"", "done=true,invalid", "done=true,done=false", "=true", "done=null", "done=[]"} {
		t.Run(spec, func(t *testing.T) {
			tree := WrapWithCheckpointVerifier(&SerializableNode{Name: "task"}, 1, spec)
			if _, err := ParseCheckpointContract(tree); err == nil {
				t.Fatalf("invalid contract passed: %+v", tree.Metadata)
			}
			if hasPostconditionContract(tree) {
				t.Fatal("invalid checkpoint earned governance credit")
			}
		})
	}
}

func TestCheckpointSourceAndFactsCannotBeRemovedByEvolution(t *testing.T) {
	tree := WrapWithCheckpointVerifier(&SerializableNode{Type: "ChainAction", Name: "llm_call:Perform the task"}, 1, "task_status=completed")
	changed := *tree
	changed.Metadata = map[string]any{"state_key": "world_state", "postconditions": map[string]any{"task_status": "completed"}}
	if PreservesGovernance(tree, &changed) {
		t.Fatal("evolution replaced the authoritative state map")
	}
	changed.Metadata = map[string]any{"state_key": "goap_world_state", "postconditions": map[string]any{"task_status": false}}
	if PreservesGovernance(tree, &changed) {
		t.Fatal("evolution replaced a typed postcondition")
	}
}
