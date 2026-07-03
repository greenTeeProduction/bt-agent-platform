package engine

import (
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// A graph report that merely mentions "test" (as almost every report does)
// must NOT fabricate a bogus "engine tests executable / import cycle" blocker.
// Regression context: AnalyzeImprovementGaps used a brittle
// `Contains(report,"test") && !Contains(report,"engine test")` heuristic that
// emitted a CHECK gap no real blocker justified; it then flowed into
// PrioritizeGoapGoals as a P0 "Unblock engine tests" goal that is
// un-implementable (no import cycle exists) and dead-letters the loop.
// See memory: goap-fusion-engine-test-blocker-false-goal.
func TestAnalyzeImprovementGaps_NoFabricatedEngineTestBlocker(t *testing.T) {
	analyze := GetAction("AnalyzeImprovementGaps")
	if analyze == nil {
		t.Fatal("action \"AnalyzeImprovementGaps\" not registered")
	}

	// A typical graph report: contains "test" but no genuine import-cycle or
	// test-compilation blocker.
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_graphify_report": "GRAPH_REPORT: 412 nodes, smoke test coverage across domain trees, condition tests present.",
	}}

	if got := analyze(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("AnalyzeImprovementGaps status = %d, want 1; result: %s", got, bb.Result)
	}

	gaps, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
	if strings.Contains(gaps, "Engine tests executable") ||
		strings.Contains(gaps, "import cycles block test compilation") {
		t.Fatalf("gap analysis fabricated a bogus engine-test blocker:\n%s", gaps)
	}

	// And it must not survive prioritization into a P0 "Unblock engine tests"
	// goal fed to the un-implementable failure path.
	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}
	goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if strings.Contains(goals, "Unblock engine tests") {
		t.Fatalf("prioritization produced an un-implementable engine-test P0 goal:\n%s", goals)
	}
}
