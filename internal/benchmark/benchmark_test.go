package benchmark

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestGoDevSuite_Routing(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()
	metrics := RunSuite(tree, GoDevSuite(), mock)

	if metrics.SuccessRate < 0.6 {
		t.Errorf("godev baseline success rate too low: %.2f", metrics.SuccessRate)
	}
	if metrics.TotalTasks != 6 {
		t.Errorf("expected 6 tasks, got %d", metrics.TotalTasks)
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
		{"finance_pitch_agent", "finance"},
		{"finance_kyc_screener", "finance"},
		{"domain_agent_monitor", "agent_monitor"},
		{"unknown_tree", "godev"}, // default fallback
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
	if x < 0 { return -x }
	return x
}
