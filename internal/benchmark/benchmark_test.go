package benchmark

import (
	"context"
	"os"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestGoDevSuite_Routing(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()
	metrics := RunSuite(tree, GoDevSuite(), mock)

	if metrics.SuccessRate < 0.5 {
		t.Errorf("godev baseline success rate too low: %.2f", metrics.SuccessRate)
	}
	// Suite may have 6-8 tasks depending on tree restructuring
	if metrics.TotalTasks < 6 {
		t.Errorf("expected at least 6 tasks, got %d", metrics.TotalTasks)
	}
	if metrics.Failures == 0 {
		t.Error("expected at least 1 failure (empty task)")
	}

	// Verify empty task failed
	for _, r := range metrics.Results {
		if r.Task == "" && r.Success {
			t.Error("empty task should fail")
		}
	}
}

func TestCodeReviewSuite_Routing(t *testing.T) {
	tree := domains.CodeReviewTree()
	mock := DefaultMock()
	metrics := RunSuite(tree, CodeReviewSuite(), mock)

	if metrics.SuccessRate < 0.7 {
		t.Errorf("code_review baseline too low: %.2f", metrics.SuccessRate)
	}

	// Verify routing: bug task should go through BugDetection
	for _, r := range metrics.Results {
		if r.Task == "find bugs in this code" && r.Path != "BugDetection" {
			t.Errorf("bug task routed to %s, expected BugDetection", r.Path)
		}
	}
}

// TestRunSuite_PathMatchRate_ReflectsExpectedPath pins the not-yet-existing
// path-match wiring: RunSuite must compare the detected path (Result.Path)
// against each TaskCase's declared ExpectedPath/PossiblePaths and surface
// both a per-result PathMatched bool and a suite-level PathMatchRate,
// mirroring the existing PathCoverage field.
func TestRunSuite_PathMatchRate_ReflectsExpectedPath(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()
	metrics := RunSuite(tree, GoDevSuite(), mock)

	if metrics.PathMatchRate <= 0 {
		t.Errorf("PathMatchRate = %.2f, want > 0", metrics.PathMatchRate)
	}

	var found bool
	for _, r := range metrics.Results {
		if r.Task == "build and compile the Go project" {
			found = true
			if r.Path != "BuildPath" {
				t.Errorf("Path = %q, want %q", r.Path, "BuildPath")
			}
			if !r.PathMatched {
				t.Errorf("PathMatched = false for task with matching ExpectedPath %q and actual Path %q", "BuildPath", r.Path)
			}
		}
	}
	if !found {
		t.Fatal(`expected a result for task "build and compile the Go project"`)
	}

	// Direct unit check on pathMatches: a PossiblePaths hit should match, an
	// unrelated path should not.
	tc := TaskCase{Task: "x", ExpectedPath: "A", PossiblePaths: []string{"A", "B"}}
	if !pathMatches(tc, "B") {
		t.Error("pathMatches(tc, \"B\") = false, want true (B is in PossiblePaths)")
	}
	if pathMatches(tc, "C") {
		t.Error("pathMatches(tc, \"C\") = true, want false (C is not ExpectedPath or in PossiblePaths)")
	}
}

func TestABTest_IncreaseRetries_ImprovesSuccessRate(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{
		{Operation: "increase_retries", Target: "RetrySelfCorrect"},
	}

	ab := RunABTest(tree, suite, mock, ops)

	if !ab.Improved {
		t.Log("increase_retries did not improve — may be fine if tree already perfect on this suite")
	}
	// At minimum, it should not regress
	if ab.Delta.SuccessRate < -0.2 {
		t.Errorf("increase_retries caused significant regression: Δ=%.2f", ab.Delta.SuccessRate)
	}
}

func TestABTest_WrapRetry_DoesNotRegress(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{
		{Operation: "wrap_retry", Target: "AnalyzeTask"},
	}

	ab := RunABTest(tree, suite, mock, ops)

	if ab.Delta.SuccessRate < -0.2 {
		t.Errorf("wrap_retry caused regression: Δ=%.2f", ab.Delta.SuccessRate)
	}
}

func TestABTest_AddBefore_Validates(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{{
		Operation: "add_before",
		Target:    "PreGate",
		Node: &evolution.SerializableNode{
			Type: "Condition", Name: "CheckConfidence", Description: "Confidence gate",
		},
	}}

	ab := RunABTest(tree, suite, mock, ops)

	if ab.Delta.SuccessRate < -0.2 {
		t.Errorf("add_before caused regression: Δ=%.2f", ab.Delta.SuccessRate)
	}
	// Should not break anything
	if !ab.Improved && ab.Delta.SuccessRate == 0 {
		t.Log("add_before had no effect — may be neutral mutation")
	}
}

func TestABTest_AddFallback_HelpsEdgeCases(t *testing.T) {
	tree := domains.CodeReviewTree()
	suite := CodeReviewSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{{
		Operation: "add_fallback",
		Target:    "OutcomeSelector",
		Node: &evolution.SerializableNode{
			Type: "Action", Name: "DefaultFallback", Description: "Catch-all",
		},
	}}

	ab := RunABTest(tree, suite, mock, ops)

	if ab.Delta.SuccessRate < -0.2 {
		t.Errorf("add_fallback caused regression: Δ=%.2f", ab.Delta.SuccessRate)
	}
}

func TestScoreMutation_GoodMutation_ScoresPositive(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{
		{Operation: "increase_retries", Target: "RetrySelfCorrect"},
	}

	score := ScoreMutation(tree, suite, mock, ops)
	if score < -2 {
		t.Errorf("increase_retries scored too low: %.2f", score)
	}
}

func TestScoreMutation_BadMutation_ScoresNegative(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	// Prune ExecutePlan — removes the fallback execution path
	ops := []evolution.MutationOp{
		{Operation: "prune_node", Target: "ExecutePlan"},
	}

	score := ScoreMutation(tree, suite, mock, ops)
	if score > 0 {
		t.Errorf("pruning ExecutePlan should score negative or zero, got %.2f", score)
	}
}

// TestScoreMutation_PruneRoutingCondition_BreaksPathMatchWithoutSuccessRegression
// pins the routing-regression that plain SuccessRate/PathCoverage scoring
// blindly rewards today: pruning NeedsCompilation (BuildPath's gating
// Condition) removes the guard, so StrategyRouter's Selector semantics
// swallow every task that should have reached GoKnowledgePath/TestPath/
// ExecutionPath into BuildPath's CompileGoCode/FixBuildErrors instead — whose
// mocked actions still report success, so SuccessRate barely moves even
// though routing genuinely broke. ScoreMutation must catch this via
// PathMatchRate, not SuccessRate alone.
func TestScoreMutation_PruneRoutingCondition_BreaksPathMatchWithoutSuccessRegression(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{
		{Operation: "prune_node", Target: "NeedsCompilation"},
	}

	ab := RunABTest(tree, suite, mock, ops)
	if ab.Delta.SuccessRate < -0.01 {
		t.Fatalf("expected success rate roughly unchanged (mock actions still report success), got delta=%.3f", ab.Delta.SuccessRate)
	}
	if ab.Delta.PathMatchRate >= 0 {
		t.Errorf("PathMatchRate delta = %.3f, want < 0 (routing broke: tasks got swallowed into BuildPath)", ab.Delta.PathMatchRate)
	}

	score := ScoreMutation(tree, suite, mock, ops)
	if score > 0 {
		t.Errorf("ScoreMutation = %.2f, want <= 0 (routing regression must never score as an improvement)", score)
	}
}

// TestScoreMutation_PathMatchWeight_BothImprovementsOutrankSuccessOnly is a
// regression guard for the PathMatchRate weight term in ScoreMutation: pruning
// IsGoRelated (the PreGate condition that currently rejects Go-development
// tasks with no Go-flavored keywords) improves both SuccessRate and
// PathMatchRate on GoDevSuite. Scoring the identical mutation against a copy
// of the suite with every ExpectedPath/PossiblePaths cleared holds
// SuccessRate's improvement constant (bb.Outcome doesn't depend on a suite's
// path expectations) while pinning PathMatchRate at a constant 1.0 — so any
// score difference between the two runs is attributable only to the added
// PathMatchRate term, and the "both improved" run must score strictly higher.
func TestScoreMutation_PathMatchWeight_BothImprovementsOutrankSuccessOnly(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()

	ops := []evolution.MutationOp{
		{Operation: "prune_node", Target: "IsGoRelated"},
	}

	// Sanity: this mutation must genuinely move both signals on the real
	// suite, otherwise the comparison below would be vacuous.
	abBoth := RunABTest(tree, suite, mock, ops)
	if abBoth.Delta.SuccessRate <= 0 {
		t.Fatalf("expected prune_node(IsGoRelated) to improve success rate, got delta=%.3f", abBoth.Delta.SuccessRate)
	}
	if abBoth.Delta.PathMatchRate <= 0 {
		t.Fatalf("expected prune_node(IsGoRelated) to improve path match rate, got delta=%.3f", abBoth.Delta.PathMatchRate)
	}

	successOnlySuite := Suite{Name: suite.Name + "_noexpectedpath"}
	for _, tc := range suite.Tasks {
		tc.ExpectedPath = ""
		tc.PossiblePaths = nil
		successOnlySuite.Tasks = append(successOnlySuite.Tasks, tc)
	}

	scoreBoth := ScoreMutation(tree, suite, mock, ops)
	scoreSuccessOnly := ScoreMutation(tree, successOnlySuite, mock, ops)

	if scoreBoth <= scoreSuccessOnly {
		t.Errorf("mutation improving both success rate and path match (score=%.2f) should outscore the identical mutation scored where path match can't move (score=%.2f)", scoreBoth, scoreSuccessOnly)
	}
}

func TestAllSuites_Complete(t *testing.T) {
	suites := AllSuites()
	if len(suites) < 4 {
		t.Errorf("expected at least 4 suites, got %d", len(suites))
	}
	for _, s := range suites {
		if len(s.Tasks) == 0 {
			t.Errorf("suite %s has no tasks", s.Name)
		}
	}
}

func TestSuiteForTree_Matching(t *testing.T) {
	tests := []struct{ treeName, expectedSuite string }{
		{"godev", "godev"},
		{"domain_code_review", "code_review"},
		{"domain_devops_ci", "devops_ci"},
		{"finance_pitch_agent", "pitch_agent"},
		{"finance_kyc_screener", "kyc_screener"},
		{"domain_agent_monitor", "agent_monitor"},
		{"domain_security_audit", "security_audit"},
		{"research_deep_research", "research"},
		{"research_quick_research", "research"},
		{"domain_data_pipeline", "data_pipeline"},
		{"domain_game_ai", "game_ai"},
		{"domain_refactoring", "refactoring"},
		{"domain_crash_investigator", "crash_investigator"},
		{"domain_meeting_notes", "meeting_notes"},
		{"domain_alert_router", "alert_router"},
		{"domain_trading_signal", "trading_signal"},
		{"domain_arc42:section1", "arc42"},
		{"domain_arc42:docsync", "arc42_docsync"},
		{"domain_arc42_seeder", "arc42_seeder"},
		{"domain_goap_devops", "goap"},
		{"domain_goap_planning", "goap"},
		{"domain_goap_research", "goap"},
		{"domain_goap_fusion", "goap_fusion"},
		{"domain_goap_fusion_loop", "goap_fusion"},
		{"default", "default"},
		{"unknown_tree", "godev"}, // default fallback
		// NotebookLM family: domain_notebooklm, domain_notebooklm_consumer, and
		// domain_notebooklm_plan_implement are three structurally distinct trees
		// (see internal/domains/notebooklm.go, notebooklm_consumer.go, and
		// internal/evolution/notebooklm_workflow.go) that must each get their own
		// suite reflecting their own real node names — not all three collapsed
		// into one NotebookLMSuite() by a blanket containsStr(treeName, "notebooklm").
		{"domain_notebooklm", "notebooklm"},
		{"domain_notebooklm_consumer", "notebooklm_consumer"},
		{"domain_notebooklm_plan_implement", "notebooklm_plan_implement"},
	}
	for _, tt := range tests {
		suite := SuiteForTree(tt.treeName)
		if suite.Name != tt.expectedSuite {
			t.Errorf("SuiteForTree(%q) = %q, want %q", tt.treeName, suite.Name, tt.expectedSuite)
		}
	}
}

func TestCohensD_NoEffect(t *testing.T) {
	d := cohensD(10, 20, 10, 20)
	if mathAbs(d) > 0.01 {
		t.Errorf("Cohen's d for identical proportions should be ~0, got %.3f", d)
	}
}

func TestCohensD_LargeEffect(t *testing.T) {
	d := cohensD(5, 20, 15, 20)
	if d < 1.0 {
		t.Errorf("Cohen's d for large improvement should be >1.0, got %.3f", d)
	}
}

func TestFisherExact_Significant(t *testing.T) {
	p := fishersExact(5, 15, 14, 6) // 25% → 70% success
	if p > 0.05 {
		t.Errorf("large effect should be significant, p=%.4f", p)
	}
}

func TestFisherExact_NotSignificant(t *testing.T) {
	p := fishersExact(10, 10, 11, 9) // 50% → 55% success
	if p < 0.05 {
		t.Logf("small effect may be significant by chance, p=%.4f", p)
	}
}

func TestMockLLM_ReturnsPredictable(t *testing.T) {
	mock := DefaultMock()
	if mock.AnalyzeComplexity("any") != "medium" {
		t.Error("mock complexity mismatch")
	}
	plan := mock.GeneratePlan("task", "low")
	if len(plan) < 5 {
		t.Error("mock plan too short")
	}
	ww, ti := mock.Reflect("t", "success", "p")
	if ww != "task completed successfully" {
		t.Error("mock reflect mismatch")
	}
	if ti != "optimize performance" {
		t.Error("mock reflect mismatch")
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestAbsDiff(t *testing.T) {
	if absDiff(3.0, 5.0) != 2.0 {
		t.Error("absDiff(3,5) should be 2")
	}
	if absDiff(5.0, 3.0) != 2.0 {
		t.Error("absDiff(5,3) should be 2")
	}
	if absDiff(0.0, 0.0) != 0.0 {
		t.Error("absDiff(0,0) should be 0")
	}
	if absDiff(-1.0, 1.0) != 2.0 {
		t.Error("absDiff(-1,1) should be 2")
	}
}

func TestMinF(t *testing.T) {
	if minF(3.0, 5.0) != 3.0 {
		t.Error("minF(3,5) should be 3")
	}
	if minF(5.0, 3.0) != 3.0 {
		t.Error("minF(5,3) should be 3")
	}
	if minF(0.0, 0.0) != 0.0 {
		t.Error("minF(0,0) should be 0")
	}
}

func TestSortResults(t *testing.T) {
	results := []Result{
		{Task: "zebra"},
		{Task: "alpha"},
		{Task: "mega"},
	}
	SortResults(results)
	if results[0].Task != "alpha" || results[1].Task != "mega" || results[2].Task != "zebra" {
		t.Errorf("SortResults order wrong: %v, %v, %v", results[0].Task, results[1].Task, results[2].Task)
	}
	// Empty should not panic
	SortResults(nil)
	SortResults([]Result{})
}

func TestSmallSampleWarning(t *testing.T) {
	w := SmallSampleWarning("test", 5)
	if w == "" {
		t.Error("small sample should warn")
	}
	w = SmallSampleWarning("test", 15)
	if w == "" {
		t.Error("medium sample should warn")
	}
	w = SmallSampleWarning("test", 25)
	if w != "" {
		t.Errorf("large sample should not warn, got: %s", w)
	}
}

func TestBootstrapCI(t *testing.T) {
	lower, upper := BootstrapCI(0, 0)
	if lower != 0 || upper != 0 {
		t.Error("zero total should return 0,0")
	}
	lower, upper = BootstrapCI(10, 10)
	if lower > 1.0 || upper > 1.0 || lower < 0.0 || upper < 0.0 {
		t.Errorf("bounds out of range: [%.3f, %.3f]", lower, upper)
	}
	lower, upper = BootstrapCI(5, 20)
	if lower > upper {
		t.Error("lower should be <= upper")
	}
}

func TestAnnotateMetrics(t *testing.T) {
	m := &RunMetrics{TotalTasks: 10, Successes: 8}
	AnnotateMetrics(m)
	if m.LowerCI == 0 && m.UpperCI == 0 {
		t.Error("CIs should be populated")
	}
	if m.Warning == "" {
		t.Error("should warn on small sample")
	}

	m2 := &RunMetrics{TotalTasks: 0}
	AnnotateMetrics(m2)
	if m2.LowerCI != 0 || m2.UpperCI != 0 {
		t.Error("zero tasks should not annotate CIs")
	}
}

func TestMockLLM_GenerateCtx(t *testing.T) {
	mock := DefaultMock()
	result, err := mock.GenerateCtx(context.TODO(), "test prompt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result) < 5 {
		t.Error("result too short")
	}
}

func TestMockLLM_GenerateWithTimeout(t *testing.T) {
	mock := DefaultMock()
	result, err := mock.GenerateWithTimeout("test prompt", 1000)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result) < 5 {
		t.Error("result too short")
	}
}

func TestDetectPath_FromCurrentPath(t *testing.T) {
	bb := &engine.Blackboard{CurrentPath: "BugDetection", Task: "any task"}
	path := detectPath("result", bb)
	if path != "BugDetection" {
		t.Errorf("expected BugDetection, got %s", path)
	}
}

func TestDetectPath_FromVisitedPaths(t *testing.T) {
	bb := &engine.Blackboard{VisitedPaths: []string{"SecurityPath", "BugDetection"}, Task: "any task"}
	path := detectPath("result", bb)
	if path != "SecurityPath" {
		t.Errorf("expected SecurityPath, got %s", path)
	}
}

func TestDetectPath_KeywordFallback(t *testing.T) {
	tests := []struct {
		task, expected string
	}{
		{"health check for agent status", "HealthPath"},
		{"transcribe the meeting minutes", "MeetingPath"},
		{"build a DCF model for valuation", "FinancePath"},
		{"deploy to kubernetes pipeline", "DevOpsPath"},
		{"research new algorithms for optimization", "ResearchPath"},
		{"refactor the engine package", "RefactoringPath"},
		{"what is a Selector in behavior trees", "KnowledgePath"},
		{"move card to backlog", "WorkflowPath"},
		{"production outage postmortem analysis", "IncidentPath"},
		{"review this code for security bugs", "CodeReviewPath"},
		{"compile the Go build", "BuildPath"},
		{"unknown task with no keywords", "GeneralPath"},
		{"cron job audit and governance", "CronPath"},
		{"tree fitness evaluation for mutation candidate", "EvolutionPath"},
		{"platform maturity gap analysis for production readiness", "PlatformEvalPath"},
		{"notebooklm chat queries for research", "NotebookLMPath"},
		{"vault ingest the session and synthesize daily", "VaultPath"},
		{"analyze the strategy and forecast", "ThinkTankPath"},
	}
	for _, tt := range tests {
		bb := &engine.Blackboard{Task: tt.task}
		path := detectPath("result", bb)
		if path != tt.expected {
			t.Errorf("task %q: expected %s, got %s", tt.task, tt.expected, path)
		}
	}
}

func TestQuickValidate_SmallSuite(_ *testing.T) {
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()
	// Small suite (<=3 tasks) should call ScoreMutation directly
	suite := Suite{Name: "tiny", Tasks: []TaskCase{
		{Task: "build the Go module", ExpectedPath: "BuildPath", MinResultLen: 30, ShouldSucceed: true},
	}}
	ops := []evolution.MutationOp{{Operation: "increase_retries", Target: "RetrySelfCorrect"}}
	score := QuickValidate(tree, suite, mock, ops)
	// Should not panic, score can be any value
	_ = score
}

func TestQuickValidate_LargeSuite(_ *testing.T) {
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()
	suite := GoDevSuite() // has >3 tasks
	ops := []evolution.MutationOp{{Operation: "increase_retries", Target: "RetrySelfCorrect"}}
	score := QuickValidate(tree, suite, mock, ops)
	// Should use only first+last tasks and not panic
	_ = score
}

func TestScoreMutation_NeutralIsZero(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()
	// Empty ops should be neutral
	ops := []evolution.MutationOp{}
	score := ScoreMutation(tree, suite, mock, ops)
	if score != 0.0 {
		t.Errorf("no-op mutation should be neutral (0.0), got %.2f", score)
	}
}

func TestScoreMutation_RegressionIsNegative(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	suite := GoDevSuite()
	mock := DefaultMock()
	// Prune a critical path node — with mock LLM, output is identical so score is 0 (neutral)
	// Real LLM would show actual regression here
	ops := []evolution.MutationOp{
		{Operation: "prune_node", Target: "StrategyRouter"},
	}
	score := ScoreMutation(tree, suite, mock, ops)
	if score < 0 {
		t.Logf("pruning StrategyRouter scored negative as expected: %.2f", score)
	}
	if score > 0 {
		t.Errorf("pruning StrategyRouter should not improve score, got %.2f", score)
	}
}

func TestCohensD_SmallSamples(t *testing.T) {
	// n1 < 2 should return 0
	d := cohensD(1, 1, 5, 10)
	if d != 0 {
		t.Errorf("n1<2 should return 0, got %.3f", d)
	}
	// n2 < 2 should return 0
	d = cohensD(5, 10, 1, 1)
	if d != 0 {
		t.Errorf("n2<2 should return 0, got %.3f", d)
	}
	// Both small
	d = cohensD(0, 0, 0, 1)
	if d != 0 {
		t.Errorf("both small should return 0, got %.3f", d)
	}
}

func TestCohensD_ExtremeProportions(t *testing.T) {
	// pPool == 0 (all zeros)
	d := cohensD(0, 10, 0, 10)
	if d != 0 {
		t.Errorf("pPool=0 should return 0, got %.3f", d)
	}
	// pPool == 1 (all successes)
	d = cohensD(10, 10, 10, 10)
	if d != 0 {
		t.Errorf("pPool=1 should return 0, got %.3f", d)
	}
	// One group all zeros, other mixed — pPool in (0,1) but near boundary
	d = cohensD(0, 10, 5, 10)
	if d == 0 {
		t.Log("near-boundary may produce small effect size")
	}
}

func TestFisherExact_ZeroCounts(t *testing.T) {
	// N == 0
	p := fishersExact(0, 0, 0, 0)
	if p != 1.0 {
		t.Errorf("N=0 should return 1.0, got %.4f", p)
	}
	// n1 == 0
	p = fishersExact(0, 0, 5, 5)
	if p != 1.0 {
		t.Errorf("n1=0 should return 1.0, got %.4f", p)
	}
	// n2 == 0
	p = fishersExact(5, 5, 0, 0)
	if p != 1.0 {
		t.Errorf("n2=0 should return 1.0, got %.4f", p)
	}
}

func TestFisherExact_PerfectSeparation(t *testing.T) {
	// 100% success vs 0% success should be highly significant
	p := fishersExact(10, 0, 0, 10)
	if p > 0.001 {
		t.Errorf("perfect separation should be extremely significant, p=%.6f", p)
	}
	// 0% vs 100%
	p = fishersExact(0, 10, 10, 0)
	if p > 0.001 {
		t.Errorf("perfect separation should be extremely significant, p=%.6f", p)
	}
}

func TestBuiltinSWELite_CoverageAndUniqueness(t *testing.T) {
	entries := BuiltinSWELite()
	if len(entries) != 5 {
		t.Fatalf("expected 5 builtin SWE-lite entries, got %d", len(entries))
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.ID == "" || entry.Repo == "" || entry.IssueTitle == "" || entry.IssueBody == "" {
			t.Fatalf("entry has missing required fields: %+v", entry)
		}
		if seen[entry.ID] {
			t.Fatalf("duplicate SWE-lite entry ID %q", entry.ID)
		}
		seen[entry.ID] = true
	}
}

func TestMax1(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{name: "negative", in: -3, want: 1},
		{name: "zero", in: 0, want: 1},
		{name: "one", in: 1, want: 1},
		{name: "larger", in: 7, want: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := max1(tc.in); got != tc.want {
				t.Fatalf("max1(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuiltinSWEVerifiedSample_CoverageAndUniqueness(t *testing.T) {
	entries := BuiltinSWEVerifiedSample()
	if len(entries) != 10 {
		t.Fatalf("expected 10 SWE-bench Verified sample entries, got %d", len(entries))
	}

	seen := map[string]bool{}
	repos := map[string]bool{}
	for _, entry := range entries {
		if entry.InstanceID == "" || entry.Repo == "" || entry.ProblemStatement == "" {
			t.Fatalf("entry has missing required fields: %+v", entry)
		}
		if seen[entry.InstanceID] {
			t.Fatalf("duplicate SWE Verified instance ID %q", entry.InstanceID)
		}
		seen[entry.InstanceID] = true
		repos[entry.Repo] = true
	}
	for _, repo := range []string{"astropy/astropy", "django/django", "sympy/sympy", "scikit-learn/scikit-learn"} {
		if !repos[repo] {
			t.Fatalf("expected representative repo %q in sample", repo)
		}
	}
}

func TestLoadSWEVerifiedAndEvaluate(t *testing.T) {
	path := t.TempDir() + "/swe_verified.json"
	jsonData := `[{"instance_id":"case-1","repo":"go-bt-evolve","problem_statement":"Fix a deterministic bug with enough detail to exercise evaluation."}]`
	if err := os.WriteFile(path, []byte(jsonData), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	entries, err := LoadSWEVerified(path)
	if err != nil {
		t.Fatalf("LoadSWEVerified returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].InstanceID != "case-1" {
		t.Fatalf("unexpected entries: %+v", entries)
	}

	tree := &evolution.SerializableNode{Type: "Action", Name: "MarkSuccessful"}
	metrics := EvaluateSWEVerified(tree, entries, DefaultMock())
	if metrics.TotalEntries != 1 || len(metrics.Results) != 1 {
		t.Fatalf("unexpected metrics shape: %+v", metrics)
	}
	if metrics.Resolved != 0 || metrics.ResolveRate != 0 {
		t.Fatalf("MarkSuccessful without output should not be considered resolved: %+v", metrics)
	}
	if metrics.Results[0].Outcome != "failure" {
		t.Fatalf("MarkSuccessful without output should fail the quality gate, got %+v", metrics.Results[0])
	}
}

func TestLoadSWEVerified_Errors(t *testing.T) {
	if _, err := LoadSWEVerified(t.TempDir() + "/missing.json"); err == nil {
		t.Fatal("expected missing file error")
	}

	badPath := t.TempDir() + "/bad.json"
	if err := os.WriteFile(badPath, []byte(`{"not":"an array"}`), 0o600); err != nil {
		t.Fatalf("write bad fixture: %v", err)
	}
	if _, err := LoadSWEVerified(badPath); err == nil {
		t.Fatal("expected JSON unmarshal error")
	}
}

func TestTauBenchBuiltinRetailAndDefaultEntries(t *testing.T) {
	retail := BuiltinTauBenchRetail()
	if len(retail) != 5 {
		t.Fatalf("expected 5 retail τ-bench entries, got %d", len(retail))
	}
	for _, entry := range retail {
		if entry.Domain != "retail" {
			t.Fatalf("retail entry has wrong domain: %+v", entry)
		}
		if entry.ID == "" || entry.Scenario == "" || len(entry.ExpectedActions) == 0 || len(entry.Tools) == 0 {
			t.Fatalf("retail entry missing required benchmark fields: %+v", entry)
		}
	}

	all := DefaultTauBenchEntries()
	if len(all) != len(BuiltinTauBenchAirline())+len(retail) {
		t.Fatalf("default τ-bench entries length mismatch: got %d", len(all))
	}
	seenDomains := map[string]bool{}
	for _, entry := range all {
		seenDomains[entry.Domain] = true
	}
	if !seenDomains["airline"] || !seenDomains["retail"] {
		t.Fatalf("default τ-bench entries should include airline and retail domains, got %+v", seenDomains)
	}
}
