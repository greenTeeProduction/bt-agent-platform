package benchmark

import "testing"

func TestConfiguredInferenceDoesNotSubstituteMockEvidence(t *testing.T) {
	t.Setenv("BT_LLM_SOL_ONLY", "true")
	t.Setenv("BT_BENCHMARK_TIMEOUT", "-1s")
	if model, err := DefaultLLM(); model != nil || err == nil {
		t.Fatalf("invalid configuration returned %T, %v", model, err)
	}
	result := RunSuiteWithLLM(nil, Suite{Tasks: []TaskCase{{Task: "test"}}})
	if result.Successes != 0 || result.Failures != 1 || result.Warning == "" {
		t.Fatalf("invalid configuration produced benchmark evidence: %+v", result)
	}
}
