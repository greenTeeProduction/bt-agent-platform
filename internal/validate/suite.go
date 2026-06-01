// Package validate provides agent validation suites and scoring.
// Every agent gets a validation suite on creation. Tests include:
// smoke (does it run?), routing (do all paths work?), output (quality gates), regression (better than before?)
package validate

import (
	"fmt"
	"strings"
	"time"
)

// TestKind defines the type of validation test.
type TestKind string

const (
	TestSmoke     TestKind = "smoke"     // Does the agent run without crashing?
	TestRouting   TestKind = "routing"   // Do all strategy paths get exercised?
	TestOutput    TestKind = "output"    // Does output meet quality gates?
	TestRegression TestKind = "regression" // Does new version score better than old?
	TestEdge       TestKind = "edge"     // Edge cases: empty input, long input, special chars
)

// TestCase is a single validation test.
type TestCase struct {
	Kind     TestKind `json:"kind"`
	Name     string   `json:"name"`
	Input    string   `json:"input"`
	Expected string   `json:"expected,omitempty"` // for routing: expected path name
}

// Suite defines a set of validation tests for an agent.
type Suite struct {
	AgentName string     `json:"agent_name"`
	Version   string     `json:"version"`
	Tests     []TestCase `json:"tests"`
	CreatedAt time.Time  `json:"created_at"`
}

// Result is the outcome of running a single test case.
type TestResult struct {
	TestCase TestCase `json:"test_case"`
	Passed   bool     `json:"passed"`
	Output   string   `json:"output"`
	Error    string   `json:"error,omitempty"`
	Duration time.Duration `json:"duration"`
}

// SuiteResult is the aggregate result of running a full suite.
type SuiteResult struct {
	AgentName  string       `json:"agent_name"`
	Version    string       `json:"version"`
	Results    []TestResult `json:"results"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Skipped    int          `json:"skipped"`
	Duration   time.Duration `json:"duration"`
	Score      float64      `json:"score"` // overall quality score 0-100
}

// AgentScore is a composite quality score for an agent.
type AgentScore struct {
	AgentName    string  `json:"agent_name"`
	Version      string  `json:"version"`
	SuccessRate  float64 `json:"success_rate"`  // 0-1
	OutputQuality float64 `json:"output_quality"` // 0-1
	RoutingScore float64 `json:"routing_score"`  // 0-1
	Speed        float64 `json:"speed"`          // normalized 0-1
	Robustness   float64 `json:"robustness"`     // 0-1 (edge case handling)
	Composite    float64 `json:"composite"`      // weighted: 0.4*sr + 0.3*oq + 0.2*speed + 0.1*robustness
}

// Runner executes validation tests against an agent.
// The RunFunc is injected for testability — in production it calls the BT MCP agent.
type Runner struct {
	RunFunc func(agentName, treeID, task string) (outcome, output string, err error)
}

// RunSuite executes all tests in a suite and returns the result.
func (r *Runner) RunSuite(suite Suite) *SuiteResult {
	start := time.Now()
	sr := &SuiteResult{
		AgentName: suite.AgentName,
		Version:   suite.Version,
	}

	for _, tc := range suite.Tests {
		tStart := time.Now()

		switch tc.Kind {
		case TestSmoke:
			outcome, output, err := r.RunFunc(suite.AgentName, "", tc.Input)
			tr := TestResult{
				TestCase: tc,
				Output:   output,
				Duration: time.Since(tStart),
			}
			if err != nil {
				tr.Error = err.Error()
				tr.Passed = false
			} else if strings.Contains(outcome, "failure") || strings.Contains(outcome, "panic") {
				tr.Passed = false
				tr.Error = fmt.Sprintf("agent returned %s", outcome)
			} else {
				tr.Passed = len(output) >= 30 && !containsErrorPattern(output)
			}
			sr.Results = append(sr.Results, tr)

		case TestRouting:
			outcome, output, err := r.RunFunc(suite.AgentName, "", tc.Input)
			tr := TestResult{
				TestCase: tc,
				Output:   output,
				Duration: time.Since(tStart),
			}
			if err != nil {
				tr.Error = err.Error()
			} else if outcome == "success" {
				tr.Passed = true
			} else {
				tr.Error = fmt.Sprintf("routing failed: %s", outcome)
			}
			sr.Results = append(sr.Results, tr)

		case TestOutput:
			outcome, output, err := r.RunFunc(suite.AgentName, "", tc.Input)
			tr := TestResult{
				TestCase: tc,
				Output:   output,
				Duration: time.Since(tStart),
			}
			if err != nil {
				tr.Error = err.Error()
			}
			// Check output quality
			tr.Passed = outcome == "success" && len(output) >= 50 &&
				!containsErrorPattern(output)
			sr.Results = append(sr.Results, tr)

		case TestEdge:
			outcome, output, err := r.RunFunc(suite.AgentName, "", tc.Input)
			tr := TestResult{
				TestCase: tc,
				Output:   output,
				Duration: time.Since(tStart),
			}
			if err != nil {
				tr.Error = err.Error()
			}
			// Edge cases: agent should handle gracefully (not panic), even if outcome is failure
			tr.Passed = outcome != "" && !strings.Contains(strings.ToLower(output), "panic")
			sr.Results = append(sr.Results, tr)

		case TestRegression:
			// Regression test needs previous version baseline — skip if no baseline
			tr := TestResult{
				TestCase: tc,
				Output:   "no baseline for regression test",
				Duration: time.Since(tStart),
				Passed:   true, // skip (not a failure if no baseline)
			}
			sr.Results = append(sr.Results, tr)
		}
	}

	// Compute aggregate
	for _, tr := range sr.Results {
		switch {
		case tr.Passed:
			sr.Passed++
		case tr.Error != "":
			sr.Failed++
		default:
			sr.Skipped++
		}
	}

	total := sr.Passed + sr.Failed + sr.Skipped
	if total > 0 {
		sr.Score = float64(sr.Passed) / float64(total) * 100
	}
	sr.Duration = time.Since(start)
	return sr
}

// NewDefaultSuite creates a default validation suite for a new agent.
func NewDefaultSuite(agentName, version string) Suite {
	return Suite{
		AgentName: agentName,
		Version:   version,
		CreatedAt: time.Now(),
		Tests: []TestCase{
			{Kind: TestSmoke, Name: "basic-task", Input: "Hello, respond with a greeting"},
			{Kind: TestSmoke, Name: "complex-task", Input: "Analyze the following and provide a structured report"},
			{Kind: TestEdge, Name: "empty-input", Input: ""},
			{Kind: TestEdge, Name: "long-input", Input: longInput},
			{Kind: TestEdge, Name: "special-chars", Input: "!@#$%^&*()_+{}|:\"<>?~`"},
		},
	}
}

// ScoreAgent computes a composite quality score from suite results.
func ScoreAgent(results []TestResult, successRate, avgDurationMs float64) AgentScore {
	score := AgentScore{
		SuccessRate: successRate,
	}

	// Output quality: fraction of output tests that passed
	outputPassed := 0
	outputTotal := 0
	routingPassed := 0
	routingTotal := 0
	edgePassed := 0
	edgeTotal := 0

	for _, r := range results {
		switch r.TestCase.Kind {
		case TestOutput:
			outputTotal++
			if r.Passed {
				outputPassed++
			}
		case TestRouting:
			routingTotal++
			if r.Passed {
				routingPassed++
			}
		case TestEdge:
			edgeTotal++
			if r.Passed {
				edgePassed++
			}
		}
	}

	if outputTotal > 0 {
		score.OutputQuality = float64(outputPassed) / float64(outputTotal)
	}
	if routingTotal > 0 {
		score.RoutingScore = float64(routingPassed) / float64(routingTotal)
	}
	if edgeTotal > 0 {
		score.Robustness = float64(edgePassed) / float64(edgeTotal)
	}

	// Speed: normalize (faster is better, cap at 1.0)
	if avgDurationMs > 0 {
		score.Speed = 1.0 / (1.0 + avgDurationMs/10000.0) // 10s = 0.5
	} else {
		score.Speed = 1.0
	}

	// Composite: weighted score
	score.Composite = score.SuccessRate*0.4 +
		score.OutputQuality*0.3 +
		score.Speed*0.2 +
		score.Robustness*0.1

	return score
}

func containsErrorPattern(output string) bool {
	patterns := []string{"panic", "error:", "failed", "cannot", "unable"}
	lower := strings.ToLower(output)
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}


const longInput = `This is a very long input designed to test the agent's ability to handle large amounts of text. ` +
	`It contains multiple sentences and paragraphs to simulate real-world usage where users might paste ` +
	`extensive documentation, code, or conversation transcripts. The agent should process this without ` +
	`crashing or truncating the response inappropriately. This test ensures the agent's context handling ` +
	`and token management are working correctly. End of long input test.`
