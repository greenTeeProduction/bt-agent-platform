package benchmark

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestQuickValidateCandidateRejectsRegressedExecution(t *testing.T) {
	baseline, suite := liveRoutingFixture()
	engine.RegisterCondition("BenchmarkCandidateFalse", func(*engine.Blackboard) bool { return false })
	candidate := &evolution.SerializableNode{Type: "Condition", Name: "BenchmarkCandidateFalse"}
	if QuickValidateCandidate(baseline, candidate, suite, RealLLM(t)) {
		t.Fatal("regressed whole-tree candidate accepted")
	}
	if !QuickValidateCandidate(baseline, baseline, suite, RealLLM(t)) {
		t.Fatal("unchanged candidate rejected")
	}
	if QuickValidateCandidate(baseline, baseline, Suite{}, RealLLM(t)) {
		t.Fatal("missing evidence accepted")
	}
}

func TestQuickValidateCandidateRejectsInvalidTreeAgainstFailingBaseline(t *testing.T) {
	engine.RegisterCondition("BenchmarkFailingBaseline", func(*engine.Blackboard) bool { return false })
	baseline := &evolution.SerializableNode{Type: "Condition", Name: "BenchmarkFailingBaseline"}
	suite := Suite{Name: "failing", Tasks: []TaskCase{{Task: "test", ShouldSucceed: true}}}
	for _, candidate := range []*evolution.SerializableNode{{Type: "MissingNode"}, {Type: "SubTreeRef", Name: "missing-block-for-benchmark"}, {Type: "Action", Name: "MissingBenchmarkAction"}, {Type: "AlwaysSucceed", Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}}}} {
		if QuickValidateCandidate(baseline, candidate, suite, RealLLM(t)) {
			t.Fatalf("invalid candidate accepted: %+v", candidate)
		}
	}
}
