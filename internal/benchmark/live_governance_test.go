package benchmark

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// A controlled bad-output experiment using actual inference, not canned LLM
// responses. It qualifies the gate/recovery mechanism, not fleet-wide impact.
func TestLiveGovernanceGateRecoversVerifiedResult(t *testing.T) {
	model := RealLLM(t)
	bad := evolution.SerializableNode{Type: "ChainAction", Name: "llm_call", Metadata: map[string]any{
		"prompt": "Reply with only the letter x.", "max_tokens": float64(16),
	}}
	recovery := evolution.SerializableNode{Type: "ChainAction", Name: "llm_call:{{.Task}}", Metadata: map[string]any{
		"system_msg": "Follow the task exactly. Return a compact JSON object only.", "max_tokens": float64(96),
	}}
	base := &evolution.SerializableNode{Type: "Sequence", Name: "ControlledBaseline", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "ValidateInput"}, bad,
	}}
	candidate := &evolution.SerializableNode{Type: "Sequence", Name: "GovernedCandidate", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "ValidateInput"},
		{Type: "QualityGate", Name: "VerifyAndRecover", Children: []evolution.SerializableNode{bad, recovery}},
	}}
	suite := Suite{Name: "live_gate_recovery", Tasks: []TaskCase{
		{Task: "Compute 17 + 25. Return JSON with total set to the answer and approved set to true only if total is 42.", ShouldSucceed: true, MinResultLen: 25, MinQualityScore: 0.1, ExpectedJSON: map[string]any{"total": 42, "approved": true}},
		{Task: "", ShouldReject: true},
	}}
	before, after := RunSuite(base, suite, model), RunSuite(candidate, suite, model)
	if before.ModelEvidence.Calls < 1 || after.ModelEvidence.Calls < 2 {
		t.Fatalf("missing real model execution: before=%+v after=%+v", before, after)
	}
	if before.Results[0].ContractPassed || !after.Results[0].ContractPassed || !after.Results[1].ContractPassed {
		t.Fatalf("gate did not improve the declared contracts: before=%+v after=%+v", before, after)
	}
	output := strings.TrimSpace(after.Results[0].Output)
	output = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(output, "```json"), "```"))
	var verified struct {
		Total    int  `json:"total"`
		Approved bool `json:"approved"`
	}
	if err := json.Unmarshal([]byte(output), &verified); err != nil || verified.Total != 42 || !verified.Approved {
		t.Fatalf("real model result failed independent verification: %q (%v)", output, err)
	}
	if path := os.Getenv("BT_BENCHMARK_REPORT"); path != "" {
		data, err := json.MarshalIndent(map[string]any{"experiment": "controlled_live_gate_recovery", "baseline_tree": base, "candidate_tree": candidate, "before": before, "after": after}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("live %s:%s: contract pass rate %.2f -> %.2f; calls %d + %d; recovered total=%d", after.ModelEvidence.Backend, after.ModelEvidence.Model, before.ContractPassRate, after.ContractPassRate, before.ModelEvidence.Calls, after.ModelEvidence.Calls, verified.Total)
}

func TestLiveBenchmarkFallsBackToSol(t *testing.T) {
	for _, test := range []struct{ name, endpoint, timeout string }{
		{"unavailable", "http://127.0.0.1:1", "15s"},
		{"deadline", "http://127.0.0.1:11434", "1ns"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("BT_BENCHMARK_BACKEND", "ollama")
			t.Setenv("BT_BENCHMARK_OLLAMA_URL", test.endpoint)
			t.Setenv("BT_BENCHMARK_TIMEOUT", test.timeout)
			model := RealLLM(t)
			output, err := model.Generate("Reply with the single word READY.")
			if err != nil || strings.TrimSpace(output) != "READY" {
				t.Fatalf("live Sol fallback output=%q err=%v", output, err)
			}
			evidence := model.(*LiveModel).Evidence()
			if evidence.Backend != "sol" || evidence.Model != "gpt-6.1-sol" || evidence.Fallbacks != 1 || evidence.Errors != 0 {
				t.Fatalf("wrong fallback provenance: %+v", evidence)
			}
			t.Logf("live %s fallback verified: %+v", test.name, evidence)
		})
	}
}
