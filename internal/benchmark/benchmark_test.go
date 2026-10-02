package benchmark

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// The fixture exercises real inference and real routing without needing an
// external repository or service. Domain trees lacking such capabilities must
// report a qualification gap; they cannot inherit this fixture's results.
func liveRoutingFixture() (*evolution.SerializableNode, Suite) {
	work := evolution.SerializableNode{Type: "ChainAction", Name: "llm_call", Metadata: map[string]any{
		"prompt": "Describe this task in one concise sentence using only the supplied information. Do not claim execution. Task: {{.Task}}", "max_tokens": float64(64),
	}}
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "LiveRouting", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "ValidateInput"},
		{Type: "Selector", Name: "StrategyRouter", Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "BuildPath", Children: []evolution.SerializableNode{{Type: "Condition", Name: "NeedsCompilation"}, work}},
			{Type: "Sequence", Name: "GoKnowledgePath", Children: []evolution.SerializableNode{{Type: "Condition", Name: "IsGoQuestion"}, work}},
			{Type: "Sequence", Name: "ExecutionPath", Children: []evolution.SerializableNode{work}},
		}},
	}}
	return tree, Suite{Name: "live_routing_fixture", Tasks: []TaskCase{
		{Task: "explain how to build and compile a Go project", ExpectedPath: "BuildPath", ShouldSucceed: true, MinResultLen: 20},
		{Task: "what is a Go goroutine?", ExpectedPath: "GoKnowledgePath", ShouldSucceed: true, MinResultLen: 20},
		{Task: "", ShouldReject: true},
	}}
}

func TestDomainSuitesReportMissingCapabilityFixtures(t *testing.T) {
	model, err := DefaultLLM()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		tree  *evolution.SerializableNode
		suite Suite
	}{
		{evolution.GoDeveloperTree(), GoDevSuite()}, {domains.CodeReviewTree(), CodeReviewSuite()},
	} {
		metrics := RunSuite(test.tree, test.suite, model)
		if metrics.Warning == "" {
			t.Fatalf("missing capabilities were not reported: %+v", metrics)
		}
		if QuickValidateCandidate(test.tree, test.tree, test.suite, model) {
			t.Fatal("unqualified domain tree accepted for promotion")
		}
	}
}

func TestRunSuite_PathMatchRate_ReflectsExpectedPath(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	metrics := RunSuite(tree, suite, model)
	if metrics.Warning != "" || metrics.ContractPassRate != 1 || metrics.PathMatchRate != 1 || metrics.ModelEvidence.Calls < 2 {
		t.Fatalf("live routing qualification failed: %+v", metrics)
	}
	tc := TaskCase{ExpectedPath: "A", PossiblePaths: []string{"A", "B"}}
	if !pathMatches(tc, "B") || pathMatches(tc, "C") {
		t.Fatal("possible-path matching is incorrect")
	}
}

func TestABTest_WrapRetry_DoesNotRegress(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	ab := RunABTest(tree, suite, model, []evolution.MutationOp{{Operation: "wrap_retry", Target: "llm_call"}})
	if !ab.Qualified || ab.Delta.ContractPassRate < 0 || ab.Delta.PathMatchRate < 0 {
		t.Fatalf("bounded retry regressed the real fixture: %+v", ab)
	}
}

func TestABTest_AddBefore_Validates(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	ab := RunABTest(tree, suite, model, []evolution.MutationOp{{Operation: "add_before", Target: "StrategyRouter", Node: &evolution.SerializableNode{Type: "Condition", Name: "TaskIsNotEmpty"}}})
	if !ab.Qualified || ab.Improved || ab.Delta.ContractPassRate != 0 {
		t.Fatalf("a duplicate guard should preserve results without earning impact: %+v", ab)
	}
}

func TestScoreMutation_PruneRoutingCondition_BreaksPathMatchWithoutSuccessRegression(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	ops := []evolution.MutationOp{{Operation: "prune_node", Target: "NeedsCompilation"}}
	ab := RunABTest(tree, suite, model, ops)
	if !ab.Qualified || ab.Improved || ab.Delta.PathMatchRate >= 0 || ab.Delta.ContractPassRate != 0 {
		t.Fatalf("lost routing was not detected in real execution: %+v", ab)
	}
	if score := ScoreMutation(tree, suite, model, ops); score >= 0 {
		t.Fatalf("routing regression score = %v", score)
	}
}

func TestScoreMutation_RequiresQualifiedEvidence(t *testing.T) {
	tree, suite := liveRoutingFixture()
	if score := ScoreMutation(tree, suite, nil, nil); score >= 0 {
		t.Fatalf("missing inference cannot qualify: %v", score)
	}
	if ab := RunABTest(tree, suite, nil, nil); ab.Qualified || ab.Improved {
		t.Fatal("missing inference qualified")
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

func TestLiveLLM_GenerateCtx(t *testing.T) {
	model := RealLLM(t)
	result, err := model.GenerateCtx(context.TODO(), "test prompt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result) < 5 {
		t.Error("result too short")
	}
}

func TestLiveLLM_GenerateWithTimeout(t *testing.T) {
	model := RealLLM(t)
	result, err := model.GenerateWithTimeout("Explain in one sentence why output verification matters.", 20*time.Second)
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

func TestQuickValidate_SmallSuite(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	suite.Tasks = suite.Tasks[:1]
	if score := QuickValidate(tree, suite, model, nil); score != 0 {
		t.Fatalf("unchanged live tree: score=%v", score)
	}
}

func TestQuickValidate_LargeSuite(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	suite.Tasks = append(suite.Tasks[:2], suite.Tasks...)
	if score := QuickValidate(tree, suite, model, nil); score != 0 {
		t.Fatalf("unchanged live tree: score=%v", score)
	}
}

func TestScoreMutation_NeutralIsZero(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	if score := ScoreMutation(tree, suite, model, nil); score != 0 {
		t.Fatalf("unchanged live tree: score=%v", score)
	}
}

func TestScoreMutation_RegressionIsNegative(t *testing.T) {
	model := RealLLM(t)
	tree, suite := liveRoutingFixture()
	ops := []evolution.MutationOp{{Operation: "prune_node", Target: "StrategyRouter"}}
	if score := ScoreMutation(tree, suite, model, ops); score >= 0 {
		t.Fatalf("deleted task work: score=%v", score)
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
	metrics := EvaluateSWEVerified(tree, entries, RealLLM(t))
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
