package evolution

import (
	"encoding/json"
	"testing"
)

func TestPersistedResultContractRetainsExactNumbers(t *testing.T) {
	tree := &SerializableNode{Type: "QualityGate", Name: "ExactInteger", Metadata: map[string]any{"max_tokens": 96, "result_contract": json.RawMessage(`{"json_fields":{"number":9007199254740993}}`)}, Children: []SerializableNode{{Type: "ChainAction", Name: "llm_call:Compute the number"}}}
	store, err := NewTreeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveNamed("exact", tree); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadNamed("exact")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := ParseResultContract(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Verify(`{"number":9007199254740993}`) != nil || contract.Verify(`{"number":9007199254740992}`) == nil {
		t.Fatal("persistence rounded the declared numeric oracle")
	}
	before, _ := TreeVersion(tree)
	after, _ := TreeVersion(loaded)
	if before != after || loaded.Metadata["max_tokens"] != float64(96) {
		t.Fatalf("roundtrip changed definition or legacy metadata: %s %s %+v", before, after, loaded.Metadata)
	}
}
