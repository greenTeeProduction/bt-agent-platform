package validate

import (
	"errors"
	"strings"
	"testing"
)

func TestRunSuite_SmokeErrorPath(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "", "", errors.New("connection refused")
		},
	}
	suite := Suite{
		AgentName: "failing-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestSmoke, Name: "error-path", Input: "hello"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	tr := result.Results[0]
	if tr.Passed {
		t.Error("expected test to fail on error")
	}
	if tr.Error != "connection refused" {
		t.Errorf("expected error 'connection refused', got %q", tr.Error)
	}
	if result.Failed != 1 {
		t.Errorf("expected 1 failure, got %d", result.Failed)
	}
}

func TestRunSuite_SmokeShortOutput(t *testing.T) {
	// Smoke test output shorter than 30 chars should fail
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "Hi", nil // too short
		},
	}
	suite := Suite{
		AgentName: "shorty",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestSmoke, Name: "short", Input: "hello"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Passed {
		t.Error("expected short output smoke test to fail")
	}
}

func TestRunSuite_Routing(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "Task completed. All paths exercised.", nil
		},
	}
	suite := Suite{
		AgentName: "routing-test",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestRouting, Name: "code-review", Input: "Review this Go code"},
			{Kind: TestRouting, Name: "test-run", Input: "Run tests"},
		},
	}
	result := runner.RunSuite(suite)
	if result.Passed < 2 {
		t.Errorf("expected 2 passed routing tests, got %d passed", result.Passed)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failures, got %d", result.Failed)
	}
	if result.Score != 100 {
		t.Errorf("expected score 100, got %.1f", result.Score)
	}
}

func TestRunSuite_RoutingFailure(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "failure", "routing error: path not found", nil
		},
	}
	suite := Suite{
		AgentName: "bad-router",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestRouting, Name: "bad-path", Input: "do something"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	tr := result.Results[0]
	if tr.Passed {
		t.Error("expected routing test to fail on failure outcome")
	}
	if !strings.Contains(tr.Error, "routing failed") {
		t.Errorf("expected routing failure message, got error=%q", tr.Error)
	}
}

func TestRunSuite_RoutingError(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "", "", errors.New("agent not found")
		},
	}
	suite := Suite{
		AgentName: "missing-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestRouting, Name: "missing", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Passed {
		t.Error("expected routing test to fail on error")
	}
	if result.Results[0].Error != "agent not found" {
		t.Errorf("expected error 'agent not found', got %q", result.Results[0].Error)
	}
}

func TestRunSuite_Output(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "Here is a detailed analysis of the code quality, security issues, and suggested improvements.", nil
		},
	}
	suite := Suite{
		AgentName: "output-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestOutput, Name: "quality-check", Input: "Analyze this code"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if !result.Results[0].Passed {
		t.Errorf("output test should pass with long clean output, got error=%q", result.Results[0].Error)
	}
}

func TestRunSuite_OutputFailureShort(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "Too short", nil
		},
	}
	suite := Suite{
		AgentName: "short-output",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestOutput, Name: "short", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if result.Results[0].Passed {
		t.Error("output test should fail with short output")
	}
}

func TestRunSuite_OutputFailureContainsError(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "Here is the error: unable to process the request", nil
		},
	}
	suite := Suite{
		AgentName: "err-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestOutput, Name: "error-pattern", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if result.Results[0].Passed {
		t.Error("output test should fail when output contains error pattern")
	}
}

func TestRunSuite_OutputFailureOutcomeNotSuccess(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "failure", "A very long output that would pass length check but outcome is failure", nil
		},
	}
	suite := Suite{
		AgentName: "fail-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestOutput, Name: "bad-outcome", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if result.Results[0].Passed {
		t.Error("output test should fail when outcome is not success")
	}
}

func TestRunSuite_OutputError(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "", "", errors.New("LLM unavailable")
		},
	}
	suite := Suite{
		AgentName: "no-llm",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestOutput, Name: "llm-down", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if result.Results[0].Passed {
		t.Error("output test should fail with LLM error")
	}
	if result.Results[0].Error != "LLM unavailable" {
		t.Errorf("expected error 'LLM unavailable', got %q", result.Results[0].Error)
	}
}

func TestRunSuite_EdgeEmptyInput(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "failure", "empty input received", nil
		},
	}
	suite := Suite{
		AgentName: "edge-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestEdge, Name: "empty-input", Input: ""},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	// Edge case: should pass even with failure outcome, as long as output doesn't say "panic"
	if !result.Results[0].Passed {
		t.Errorf("edge test should pass with failure outcome (no panic), got error=%q", result.Results[0].Error)
	}
}

func TestRunSuite_EdgePanicOutput(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "chain_panic", "PANIC: runtime error: invalid memory address", nil
		},
	}
	suite := Suite{
		AgentName: "panic-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestEdge, Name: "panic", Input: "cause panic"},
		},
	}
	result := runner.RunSuite(suite)
	if result.Results[0].Passed {
		t.Error("edge test should fail when output contains 'panic'")
	}
}

func TestRunSuite_EdgeLongInput(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "Processed long input successfully with detailed analysis and recommendations.", nil
		},
	}
	suite := Suite{
		AgentName: "long-input-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestEdge, Name: "long-input", Input: longInput},
		},
	}
	result := runner.RunSuite(suite)
	if !result.Results[0].Passed {
		t.Errorf("edge test for long input should pass, got error=%q", result.Results[0].Error)
	}
}

func TestRunSuite_EdgeError(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "", "", errors.New("service unavailable")
		},
	}
	suite := Suite{
		AgentName: "down-agent",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestEdge, Name: "down", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	// Edge with an error but no outcome and output should still set tr.Error
	if result.Results[0].Error != "service unavailable" {
		t.Errorf("expected error 'service unavailable', got %q", result.Results[0].Error)
	}
}

func TestRunSuite_RegressionSkip(t *testing.T) {
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "some output", nil
		},
	}
	suite := Suite{
		AgentName: "regression-test",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestRegression, Name: "regression-check", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	// Regression tests are skipped when no baseline
	if !result.Results[0].Passed {
		t.Error("regression test without baseline should pass (skip)")
	}
}

func TestRunSuite_TotalsMixedCounts(t *testing.T) {
	// Verify counting: passed=yes, failed=yes, but skipped never hits because
	// no code path sets Passed=false with Error="" on the same result.
	runner := &Runner{
		RunFunc: func(_, _, _ string) (string, string, error) {
			return "success", "A sufficiently long output that passes all quality checks without errors.", nil
		},
	}
	suite := Suite{
		AgentName: "full-coverage",
		Version:   "1.0.0",
		Tests: []TestCase{
			{Kind: TestSmoke, Name: "s1", Input: "hello"},
			{Kind: TestRouting, Name: "r1", Input: "review code"},
			{Kind: TestOutput, Name: "o1", Input: "analyze code"},
			{Kind: TestEdge, Name: "e1", Input: ""},
			{Kind: TestRegression, Name: "reg1", Input: "test"},
		},
	}
	result := runner.RunSuite(suite)
	if len(result.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(result.Results))
	}
	// smoke (pass), routing (pass), output (pass), edge (pass), regression (pass/skip)
	if result.Passed != 5 {
		t.Errorf("expected 5 passed, got %d", result.Passed)
	}
	if result.Failed != 0 {
		t.Errorf("expected 0 failures, got %d", result.Failed)
	}
}

func TestScoreAgent_ZeroResults(t *testing.T) {
	score := ScoreAgent(nil, 1.0, 0)
	if score.SuccessRate != 1.0 {
		t.Errorf("expected SuccessRate=1.0, got %.2f", score.SuccessRate)
	}
	if score.Speed != 1.0 {
		t.Errorf("expected Speed=1.0 (zero duration), got %.2f", score.Speed)
	}
	// Composite: 0.4*1.0 + 0.3*0 + 0.2*1.0 + 0.1*0 = 0.4 + 0.2 = 0.6
	if score.Composite < 0.59 || score.Composite > 0.61 {
		t.Errorf("expected Composite≈0.6 (0.4*sr + 0.2*speed), got %.2f", score.Composite)
	}
}

func TestScoreAgent_OutputQualityOnly(t *testing.T) {
	results := []TestResult{
		{TestCase: TestCase{Kind: TestOutput}, Passed: true},
	}
	score := ScoreAgent(results, 0.0, 5000)
	if score.OutputQuality != 1.0 {
		t.Errorf("expected OutputQuality=1.0, got %.2f", score.OutputQuality)
	}
}

func TestScoreAgent_EmptyResultsLists(t *testing.T) {
	// When no output/routing/edge results exist, those scores stay at 0
	score := ScoreAgent(nil, 0.5, 10000)
	if score.OutputQuality != 0 {
		t.Errorf("expected OutputQuality=0 with no output results, got %.2f", score.OutputQuality)
	}
	if score.RoutingScore != 0 {
		t.Errorf("expected RoutingScore=0 with no routing results, got %.2f", score.RoutingScore)
	}
	if score.Robustness != 0 {
		t.Errorf("expected Robustness=0 with no edge results, got %.2f", score.Robustness)
	}
}

func TestScoreAgent_SpeedNormalization(t *testing.T) {
	// Very fast: near 1.0
	fast := ScoreAgent(nil, 1.0, 1)
	if fast.Speed < 0.99 {
		t.Errorf("expected Speed near 1.0 for 1ms, got %.4f", fast.Speed)
	}
	// Moderate: around 0.5
	moderate := ScoreAgent(nil, 1.0, 10000)
	if moderate.Speed < 0.49 || moderate.Speed > 0.51 {
		t.Errorf("expected Speed around 0.5 for 10s, got %.4f", moderate.Speed)
	}
	// Slow: near 0
	slow := ScoreAgent(nil, 1.0, 1000000)
	if slow.Speed > 0.1 {
		t.Errorf("expected Speed near 0 for very slow, got %.4f", slow.Speed)
	}
}

func TestContainsErrorPattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "panic", input: "PANIC: nil pointer", want: true},
		{name: "error colon", input: "Error: something went wrong", want: true},
		{name: "failed", input: "Task failed", want: true},
		{name: "cannot", input: "Cannot process request", want: true},
		{name: "unable", input: "Unable to connect", want: true},
		{name: "clean", input: "All tests passed successfully", want: false},
		{name: "empty", input: "", want: false},
		{name: "error suffix with colon", input: "This is an error:", want: true}, // "error:" pattern matched
		{name: "errant", input: "errant behavior", want: false},                   // "error:" not matched
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := containsErrorPattern(tc.input)
			if got != tc.want {
				t.Errorf("containsErrorPattern(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestSuiteResult_Errors(t *testing.T) {
	// Ensure result error extraction works
	r := &SuiteResult{
		Results: []TestResult{
			{Passed: true},
			{Passed: false, Error: "some error"},
		},
	}
	errs := collectErrors(r)
	if len(errs) != 1 || errs[0] != "some error" {
		t.Errorf("expected [some error], got %v", errs)
	}
}

// collects non-empty errors from suite results
func collectErrors(sr *SuiteResult) []string {
	var errs []string
	for _, r := range sr.Results {
		if r.Error != "" {
			errs = append(errs, r.Error)
		}
	}
	return errs
}

// contains checks whether a string slice contains the given string.
