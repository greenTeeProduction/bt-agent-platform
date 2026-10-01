package benchmark

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// A successful model call cannot hide a later unavailable task capability.
func TestRunSuiteReportsUnsupportedCapabilityAfterLiveInference(t *testing.T) {
	model := RealLLM(t)
	executed := false
	engine.RegisterAction("BenchmarkSandboxProbe", func(_ *btcore.BTContext[engine.Blackboard]) int {
		executed = true
		return 1
	})

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "ChainAction", Name: "llm_call:Explain in one sentence why evidence matters.", Metadata: map[string]any{"max_tokens": float64(32)}},
			{Type: "Action", Name: "BenchmarkSandboxProbe"},
		},
	}
	suite := Suite{
		Name:  "sandbox_probe",
		Tasks: []TaskCase{{Task: "run the probe", ShouldSucceed: true}},
	}

	metrics := RunSuite(tree, suite, model)

	if executed {
		t.Fatal("unavailable capability executed")
	}
	if metrics.ModelEvidence.Calls != 1 || metrics.Warning == "" || metrics.Results[0].Outcome != "benchmark_unsupported" || metrics.Results[0].ContractPassed {
		t.Fatalf("missing capability was hidden by inference evidence: %+v", metrics)
	}
}
