package benchmark

import (
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// executeLiveTask is shared by external benchmark adapters. It refuses
// synthetic providers and requires this task to have actually used inference.
func executeLiveTask(tree *evolution.SerializableNode, bb *engine.Blackboard) (string, ModelEvidence, string) {
	live, ok := bb.LLM.(*LiveModel)
	if !ok || live == nil {
		bb.Outcome = "benchmark_unqualified"
		return "", ModelEvidence{}, "real benchmark model required"
	}
	before := live.Evidence()
	bb.NodeAdmission = benchmarkAdmission
	output := engine.RunTask(bb, engine.BuildTree(tree, bb))
	evidence := live.Evidence()
	evidence.Calls -= before.Calls
	evidence.Fallbacks -= before.Fallbacks
	evidence.Errors -= before.Errors
	if evidence.Errors == 0 {
		evidence.LastError = ""
	}
	warning := ""
	switch {
	case benchmarkOutcome(bb) == "benchmark_unsupported":
		warning = "task requires an isolated capability fixture"
	case evidence.Errors > 0:
		warning = "real model execution failed: " + evidence.LastError
	case evidence.Calls == 0:
		warning = "no model calls executed"
	}
	return output, evidence, warning
}

func mergeModelEvidence(total *ModelEvidence, next ModelEvidence) {
	if total.Calls == 0 {
		total.Backend, total.Model = next.Backend, next.Model
	} else if total.Backend != next.Backend || total.Model != next.Model {
		total.Backend, total.Model = "mixed", "mixed"
	}
	total.Calls += next.Calls
	total.Errors += next.Errors
	total.Fallbacks += next.Fallbacks
	if next.LastError != "" {
		total.LastError = next.LastError
	}
}
