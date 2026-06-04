package engine

import (
	"testing"
)

func TestSuite_Smoke(t *testing.T) {
	runner := &Runner{
		RunFunc: func(agentName, treeID, task string) (string, string, error) {
			return "success", "Hello! I am a test agent. How can I help you today?", nil
		},
	}

	suite := NewDefaultSuite("test-agent", "1.0.0")
	result := runner.RunSuite(suite)

	if result.Passed < 1 {
		t.Errorf("expected at least 1 passing test, got %d passed", result.Passed)
	}
	if result.Score < 50 {
		t.Errorf("expected score >= 50, got %.1f", result.Score)
	}
}

func TestSuite_Failure(t *testing.T) {
	runner := &Runner{
		RunFunc: func(agentName, treeID, task string) (string, string, error) {
			return "failure", "error: something went wrong", nil
		},
	}

	suite := NewDefaultSuite("failing-agent", "1.0.0")
	result := runner.RunSuite(suite)

	if result.Passed >= result.Failed {
		t.Logf("Passed=%d, Failed=%d, Score=%.1f", result.Passed, result.Failed, result.Score)
	}
}

func TestSuite_Panic(t *testing.T) {
	runner := &Runner{
		RunFunc: func(agentName, treeID, task string) (string, string, error) {
			return "chain_panic", "PANIC: runtime error", nil
		},
	}

	suite := NewDefaultSuite("panic-agent", "1.0.0")
	result := runner.RunSuite(suite)

	if result.Passed > 0 {
		t.Errorf("panic agent should not pass tests, got %d passed", result.Passed)
	}
}

func TestScoreAgent(t *testing.T) {
	results := []TestResult{
		{TestCase: TestCase{Kind: TestOutput}, Passed: true},
		{TestCase: TestCase{Kind: TestOutput}, Passed: true},
		{TestCase: TestCase{Kind: TestOutput}, Passed: false},
		{TestCase: TestCase{Kind: TestRouting}, Passed: true},
		{TestCase: TestCase{Kind: TestRouting}, Passed: true},
		{TestCase: TestCase{Kind: TestEdge}, Passed: true},
	}

	score := ScoreAgent(results, 0.8, 5000)
	if score.OutputQuality < 0.5 {
		t.Errorf("expected output quality >= 0.5, got %.2f", score.OutputQuality)
	}
	if score.Composite < 0.5 || score.Composite > 1.0 {
		t.Errorf("expected composite 0.5-1.0, got %.2f", score.Composite)
	}
	t.Logf("Score: SR=%.2f OQ=%.2f Speed=%.2f Robust=%.2f Composite=%.2f",
		score.SuccessRate, score.OutputQuality, score.Speed, score.Robustness, score.Composite)
}
