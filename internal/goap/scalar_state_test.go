package goap

import (
	"encoding/json"
	"testing"
)

func TestObservedScalarFactsPreserveNumericIdentity(t *testing.T) {
	if !ValuesEqual(42, json.Number("42.0")) || ValuesEqual(42, "42") || ValuesEqual(true, "true") {
		t.Fatal("scalar type distinction changed")
	}
	if ValuesEqual(json.Number("9007199254740993"), json.Number("9007199254740992")) {
		t.Fatal("large numeric facts rounded together")
	}
	if ValuesEqual(map[string]any{"done": true}, map[string]any{"done": true}) {
		t.Fatal("structured facts were accepted as scalar observations")
	}
	if ValuesEqual(json.Number("1e99999999"), json.Number("1e99999999")) {
		t.Fatal("unbounded numeric exponent accepted")
	}
}

func TestGoapDefinitionJSONPreservesExactFactsAndRejectsTrailingInput(t *testing.T) {
	data := []byte(`{"goals":[{"name":"exact","conditions":{"number":9007199254740993}}],"actions":[{"name":"produce","effects":{"number":9007199254740993}}]}`)
	definition, err := FromJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Goals[0].Conditions["number"] != json.Number("9007199254740993") || !ValuesEqual(definition.Actions[0].Effects["number"], json.Number("9007199254740993")) {
		t.Fatal("persisted plan rounded its fact")
	}
	if _, err = FromJSON(append(data, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing input accepted")
	}
}
