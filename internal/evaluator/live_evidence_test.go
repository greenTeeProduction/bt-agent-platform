package evaluator

import "testing"

func TestPlatformScorecardCannotQualifyMissingInference(t *testing.T) {
	scorecard := buildScorecard([]SuiteEvalResult{{Name: "godev", SuccessRate: 100, Warning: "no model calls executed"}})
	if scorecard.UseCases["godev"].Status != "unqualified" {
		t.Fatal("missing inference became readiness evidence")
	}
}

func TestPlatformEvalRejectsInvalidModelConfiguration(t *testing.T) {
	t.Setenv("BT_BENCHMARK_BACKEND", "mock")
	result := RunPlatformEval()
	if result.Error == "" || result.Passed != 0 {
		t.Fatal("invalid provider produced platform evaluation evidence")
	}
}
