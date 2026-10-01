package benchmark

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestLiveRunEvidenceRecordsVerifiedModelResult(t *testing.T) {
	model := RealLLM(t)
	store, err := evolution.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "verified_sum", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "ValidateInput"},
		{Type: "QualityGate", Name: "VerifyResult", Metadata: map[string]any{"result_contract": map[string]any{"json_fields": map[string]any{"total": 42, "verified": true}}}, Children: []evolution.SerializableNode{{Type: "ChainAction", Name: "llm_call:{{.Task}}", Metadata: map[string]any{"max_tokens": 96, "system_msg": "Return compact JSON only."}}, {Type: "ChainAction", Name: "llm_call:Return exactly a JSON object with the field total containing the sum of 17 and 25, and the field verified containing boolean true. Use these exact field names.", Metadata: map[string]any{"max_tokens": 96}}}},
	}}
	bb := &engine.Blackboard{Task: "Compute 17 + 25. Return JSON with total equal to the result and verified true only if total is 42.", LLM: model, Reflections: store, TreeID: "factory:sum", User: "benchmark-owner"}
	engine.RunTask(bb, engine.BuildTree(tree, bb))
	records, err := store.LoadAll()
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	record := records[0]
	contract := TaskCase{ShouldSucceed: true, ExpectedJSON: map[string]any{"total": 42, "verified": true}}
	if !taskContractPassed(contract, string(record.Outcome), record.Result, record.QualityScore) {
		t.Fatalf("saved result failed independent verification: %+v", record)
	}
	version, _ := evolution.TreeVersion(tree)
	qualified := evolution.FilterByTreeVersion(records, "factory:sum", "benchmark-owner", version)
	if len(qualified) != 1 || record.DurationMs <= 0 {
		t.Fatalf("missing version/owner/time evidence: %+v", record)
	}
	evidence := model.(*LiveModel).Evidence()
	if evidence.Calls == 0 || evidence.Errors != 0 {
		t.Fatalf("missing actual inference: %+v", evidence)
	}
	if path := os.Getenv("BT_EVIDENCE_REPORT"); path != "" {
		data, err := json.MarshalIndent(map[string]any{"record": record, "model": evidence, "tree": tree, "independent_contract_passed": true}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("verified persisted result with %s:%s, calls=%d, tree=%s version=%s", evidence.Backend, evidence.Model, evidence.Calls, record.TreeName, record.TreeVersion)
}
