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

// PrioritizeGoapGoals must not fabricate the un-implementable "Unblock engine
// tests" P0 goal just because a gap line mentions the phrase "import cycle".
// The removed AnalyzeImprovementGaps heuristic was only one source of that
// phrase; research review (grill/NotebookLM/Claude) routinely appends free-form
// gap text, and this codebase uses "import cycle" constantly in its own design
// notes ("avoid import cycle", "engine → domains import cycle"). The downstream
// Contains(gaps,"import cycle") matcher in PrioritizeGoapGoals turns any such
// mention into a P0 goal for a blocker that does not exist — the engine package
// builds cleanly — and that goal dead-letters the loop.
// Regression context: memory goap-fusion-engine-test-blocker-false-goal.
func TestPrioritizeGoapGoals_NoImportCycleFalseGoalFromResearchGap(t *testing.T) {
	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}

	// Gaps as they arrive from research review: free-form text that merely
	// mentions "import cycle" as a design note, with no genuine blocker behind
	// it and no other goal-trigger keyword present.
	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "GAP: research notes the engine and domains packages must avoid an " +
			"import cycle when guard builders move — no blocker exists today, just a boundary design note.",
	}}

	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if strings.Contains(goals, "Unblock engine tests") {
		t.Fatalf("prioritization fabricated an un-implementable engine-test P0 goal from a research gap "+
			"that merely mentions \"import cycle\":\n%s", goals)
	}
}

// The discriminator must not degenerate to "never fire": when a gap describes a
// genuine, affirmative build blocker (tests fail to compile because of an active
// import cycle), the P0 "Unblock engine tests" goal SHOULD still be produced.
// This locks the affirmative branch so a future refactor cannot silently drop
// the capability while still passing the negative regression tests above.
func TestPrioritizeGoapGoals_AffirmativeBlockerProducesEngineTestGoal(t *testing.T) {
	prioritize := GetAction("PrioritizeGoapGoals")
	if prioritize == nil {
		t.Fatal("action \"PrioritizeGoapGoals\" not registered")
	}

	bb := &Blackboard{ChainState: map[string]any{
		"goap_fusion_improvement_gaps": "GAP: an import cycle blocks test compilation in internal/engine — " +
			"tests fail to compile and the package cannot run tests until it is broken.",
	}}

	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
	if !strings.Contains(goals, "Unblock engine tests") {
		t.Fatalf("prioritization dropped the P0 engine-test goal for a genuine, affirmative "+
			"build blocker:\n%s", goals)
	}
}
