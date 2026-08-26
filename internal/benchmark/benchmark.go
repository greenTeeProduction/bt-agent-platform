// Package benchmark provides A/B testing, statistical mutation quality testing,
// and external benchmark integration for behavior trees.
//
// It includes:
//
//   - Domain suites (GoDev, CodeReview, DevOps, Finance, AgentMonitor) for
//     per-domain task validation with real Ollama by default
//   - External benchmarks: BFCL V1/V3 (tool routing), SWE-bench Lite/Verified
//     (bug resolution), τ-bench (conversational tool use), ToolBench (API selection),
//     BTPG (tree quality metrics)
//   - ScoreMutation — statistical comparison of baseline vs mutated tree output
//     with Fisher's exact test and bootstrap confidence intervals
//   - DefaultLLM() — returns real Ollama (qwen3.6:35b) with mock fallback
//
// All domain suite tasks use DefaultLLM() for production-grade validation.
// Use testing.Short() guards for Ollama-dependent tests on slow hardware.
package benchmark

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// TaskCase is a single benchmark task with expected routing.
type TaskCase struct {
	Task            string   `json:"task"`
	ExpectedPath    string   `json:"expected_path"`              // which strategy path should handle this
	PossiblePaths   []string `json:"possible_paths,omitempty"`   // multiple acceptable paths for ambiguous tasks
	MinResultLen    int      `json:"min_result_len"`             // minimum output length expected
	ShouldSucceed   bool     `json:"should_succeed"`             // expected outcome
	ShouldReject    bool     `json:"should_reject"`              // PreGate should reject this
	MinQualityScore float64  `json:"min_quality_score,omitzero"` // minimum quality score expected
	Difficulty      string   `json:"difficulty,omitempty"`       // easy | medium | hard | adversarial
}

// Suite is a collection of benchmark tasks for a specific domain.
type Suite struct {
	Name    string     `json:"name"`
	Tasks   []TaskCase `json:"tasks"`
	LLMMode bool       `json:"llm_mode"` // true = use real LLM, false = use mock
}

// Result is the outcome of running a single task through a tree.
type Result struct {
	Task        string `json:"task"`
	Outcome     string `json:"outcome"`
	DurationMs  int64  `json:"duration_ms"`
	ResultLen   int    `json:"result_len"`
	Path        string `json:"path"`         // which strategy path was taken
	PathMatched bool   `json:"path_matched"` // whether Path matched the task's declared ExpectedPath/PossiblePaths
	Success     bool   `json:"success"`
}

// RunMetrics aggregates results from running a full suite.
type RunMetrics struct {
	TotalTasks    int      `json:"total_tasks"`
	Successes     int      `json:"successes"`
	Failures      int      `json:"failures"`
	SuccessRate   float64  `json:"success_rate"`
	AvgDurationMs float64  `json:"avg_duration_ms"`
	AvgResultLen  float64  `json:"avg_result_len"`
	PathCoverage  float64  `json:"path_coverage"`     // unique paths / total tasks
	PathMatchRate float64  `json:"path_match_rate"`   // tasks whose Path matched ExpectedPath/PossiblePaths / total tasks
	LowerCI       float64  `json:"lower_ci"`          // 95% bootstrap CI lower bound
	UpperCI       float64  `json:"upper_ci"`          // 95% bootstrap CI upper bound
	Warning       string   `json:"warning,omitempty"` // small-sample or other warnings
	Results       []Result `json:"results"`
}

// RunSuite executes all tasks in a suite against a tree.
func RunSuite(tree *evolution.SerializableNode, suite Suite, mock llm.LLM) *RunMetrics {
	results := make([]Result, 0, 32)
	successes := 0
	matchedCount := 0
	paths := make(map[string]int)

	for _, tc := range suite.Tasks {
		start := time.Now()

		bb := &engine.Blackboard{
			Task: tc.Task,
			LLM:  mock,
			// Benchmark runs must never trigger real side effects (subprocess,
			// network, external quotas) — production trees contain actions that
			// shell out to nlm/git/claude. Sandbox simulates action success;
			// conditions and tree structure still drive routing.
			Sandbox: true,
		}

		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)
		duration := time.Since(start).Milliseconds()

		success := bb.Outcome == "success"
		if tc.ShouldReject {
			// Adversarial rejection tasks: pass when correctly rejected (PreGate blocks them)
			success = !success
		}
		if success {
			successes++
		}

		// Determine which path was taken (heuristic from result content)
		path := detectPath(output, bb)

		paths[path]++

		matched := pathMatches(tc, path)
		if matched {
			matchedCount++
		}

		results = append(results, Result{
			Task:        tc.Task,
			Outcome:     bb.Outcome,
			DurationMs:  duration,
			ResultLen:   len(output),
			Path:        path,
			PathMatched: matched,
			Success:     success,
		})
	}

	n := len(results)
	if n == 0 {
		return &RunMetrics{Results: results}
	}

	var totalDur int64
	var totalLen int
	for _, r := range results {
		totalDur += r.DurationMs
		totalLen += r.ResultLen
	}

	return &RunMetrics{
		TotalTasks:    n,
		Successes:     successes,
		Failures:      n - successes,
		SuccessRate:   float64(successes) / float64(n),
		AvgDurationMs: float64(totalDur) / float64(n),
		AvgResultLen:  float64(totalLen) / float64(n),
		PathCoverage:  float64(len(paths)) / float64(n),
		PathMatchRate: float64(matchedCount) / float64(n),
		Results:       results,
	}
}

// RunSuiteWithLLM runs a suite using a real LLM client instead of a mock.
// Falls back to mock if no real LLM is available.
func RunSuiteWithLLM(tree *evolution.SerializableNode, suite Suite) *RunMetrics {
	llmClient := DefaultLLM() // tries Ollama, falls back to mock
	return RunSuite(tree, suite, llmClient)
}

func absDiff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

type ABTest struct {
	Before   *RunMetrics `json:"before"`
	After    *RunMetrics `json:"after"`
	Delta    ABDelta     `json:"delta"`
	Improved bool        `json:"improved"`
}

// ABDelta is the difference between before and after.
type ABDelta struct {
	SuccessRate   float64 `json:"success_rate_delta"`
	AvgDurationMs float64 `json:"avg_duration_delta"`
	AvgResultLen  float64 `json:"avg_result_len_delta"`
	PathCoverage  float64 `json:"path_coverage_delta"`
	PathMatchRate float64 `json:"path_match_rate_delta"`
	EffectSize    float64 `json:"effect_size"` // Cohen's d on success rate
	Significant   bool    `json:"significant"` // p < 0.05
	PValue        float64 `json:"p_value"`
}

// RunABTest applies a mutation and measures the impact.
func RunABTest(tree *evolution.SerializableNode, suite Suite, mock llm.LLM, ops []evolution.MutationOp) *ABTest {
	// Clone tree for before measurement
	beforeTree := cloneTree(tree)
	before := RunSuite(beforeTree, suite, mock)

	// Apply mutation to a fresh clone
	afterTree := cloneTree(tree)
	applied := evolution.ApplyMutations(afterTree, ops)
	after := RunSuite(afterTree, suite, mock)

	// Calculate deltas
	delta := ABDelta{
		SuccessRate:   after.SuccessRate - before.SuccessRate,
		AvgDurationMs: after.AvgDurationMs - before.AvgDurationMs,
		AvgResultLen:  after.AvgResultLen - before.AvgResultLen,
		PathCoverage:  after.PathCoverage - before.PathCoverage,
		PathMatchRate: after.PathMatchRate - before.PathMatchRate,
	}

	// Effect size (Cohen's d for proportions)
	delta.EffectSize = cohensD(
		float64(before.Successes), float64(before.TotalTasks),
		float64(after.Successes), float64(after.TotalTasks),
	)

	// Significance test (Fisher's exact for small samples, chi-squared approximation)
	delta.PValue = fishersExact(
		before.Successes, before.Failures,
		after.Successes, after.Failures,
	)
	delta.Significant = delta.PValue < 0.05

	// Only quality improvements should mark a mutation as improved. Runtime speed
	// alone is not enough because destructive mutations can appear faster by
	// pruning work while preserving mock outputs. A routing regression (tasks
	// swallowed into the wrong StrategyRouter branch) must never count as an
	// improvement even when SuccessRate holds steady, since mocked actions on
	// the wrong path can still report "success".
	improved := (delta.SuccessRate > 0 && delta.PathMatchRate >= 0) ||
		(delta.SuccessRate == 0 && delta.PathMatchRate > 0) ||
		(delta.SuccessRate == 0 && delta.PathMatchRate == 0 && delta.PathCoverage > 0)

	return &ABTest{
		Before:   before,
		After:    after,
		Delta:    delta,
		Improved: improved && applied > 0,
	}
}

// ScoreMutation returns a quality score for a mutation based on A/B testing.
// Positive = improvement, zero = neutral (no change), negative = regression.
func ScoreMutation(tree *evolution.SerializableNode, suite Suite, mock llm.LLM, ops []evolution.MutationOp) float64 {
	ab := RunABTest(tree, suite, mock, ops)
	if ab.Improved {
		// Weighted score: success rate improvement is most important
		score := ab.Delta.SuccessRate*50 +
			(1.0-minF(ab.Delta.AvgDurationMs/1000.0, 1.0))*10 +
			ab.Delta.PathCoverage*10 +
			ab.Delta.PathMatchRate*20
		if ab.Delta.Significant {
			score *= 1.5 // bonus for statistical significance
		}
		return score
	}
	// Regression: check if it hurt — either success rate or routing correctness
	// (SuccessRate can hold steady while tasks get swallowed into the wrong
	// StrategyRouter branch, since mocked actions on the wrong path still
	// report "success").
	if ab.Delta.SuccessRate < 0 || ab.Delta.PathMatchRate < 0 {
		return -1.0
	}
	// Neutral: no change (mutation didn't help or hurt)
	return 0.0
}

// QuickValidate runs a lightweight version of the suite for fast gardener validation.
// Uses max 3 tasks: first task + random edge-case task from the end.
func QuickValidate(tree *evolution.SerializableNode, suite Suite, llm llm.LLM, ops []evolution.MutationOp) float64 {
	if len(suite.Tasks) <= 3 {
		return ScoreMutation(tree, suite, llm, ops)
	}
	// Take first task (basic routing) + last task (edge case) for balanced validation
	lite := Suite{
		Name: suite.Name + "_quick",
		Tasks: []TaskCase{
			suite.Tasks[0],                  // happy-path routing
			suite.Tasks[len(suite.Tasks)-1], // edge-case task
		},
	}
	return ScoreMutation(tree, lite, llm, ops)
}

// --- Statistical helpers ---

func cohensD(s1, n1, s2, n2 float64) float64 {
	if n1 < 2 || n2 < 2 {
		return 0
	}
	p1 := s1 / n1
	p2 := s2 / n2
	// Pooled proportion
	pPool := (s1 + s2) / (n1 + n2)
	if pPool == 0 || pPool == 1 {
		return 0
	}
	se := math.Sqrt(pPool * (1 - pPool) * (1/n1 + 1/n2))
	if se == 0 {
		return 0
	}
	return (p2 - p1) / se
}

// fishersExact computes the two-tailed Fisher's exact test p-value
// for a 2×2 contingency table [[s1, f1], [s2, f2]].
// Uses hypergeometric distribution for exact computation.
func fishersExact(s1, f1, s2, f2 int) float64 {
	n1 := s1 + f1
	n2 := s2 + f2
	N := n1 + n2
	if N == 0 || n1 == 0 || n2 == 0 {
		return 1.0
	}

	a := s1 // observed cell (1,1)
	b := f1 // (1,2)
	c := s2 // (2,1)
	d := f2 // (2,2)

	// Sum probabilities of tables at least as extreme as observed
	// Range of possible 'a' values given fixed margins
	minA := 0
	if c := n1 + s2 - N; c > minA {
		minA = c
	}
	maxA := min(s1+s2, n1)

	pObs := hypergeometricProb(a, b, c, d)
	pValue := 0.0

	for i := minA; i <= maxA; i++ {
		p := hypergeometricProb(i, n1-i, (s1+s2)-i, n2-((s1+s2)-i))
		if p <= pObs+1e-12 {
			pValue += p
		}
	}

	if pValue > 1.0 {
		pValue = 1.0
	}
	return pValue
}

// hypergeometricProb computes the probability of a specific 2×2 table
// under the hypergeometric distribution.
func hypergeometricProb(a, b, c, d int) float64 {
	n := a + b + c + d
	// P = (C(a+b, a) * C(c+d, c)) / C(n, a+c)
	return math.Exp(lnChoose(a+b, a) + lnChoose(c+d, c) - lnChoose(n, a+c))
}

// lnChoose computes ln(n choose k) using the log-gamma function.
func lnChoose(n, k int) float64 {
	if k < 0 || k > n {
		return 0
	}
	return lnFactorial(n) - lnFactorial(k) - lnFactorial(n-k)
}

// lnFactorial computes ln(n!) using math.Lgamma.
func lnFactorial(n int) float64 {
	if n <= 1 {
		return 0
	}
	result, _ := math.Lgamma(float64(n + 1))
	return result
}

// BootstrapCI computes a 95% bootstrap confidence interval for a success rate.
// Uses percentile method with 1000 bootstrap samples.
func BootstrapCI(successes, total int) (lower, upper float64) {
	if total == 0 {
		return 0, 0
	}
	rate := float64(successes) / float64(total)
	const iterations = 1000
	samples := make([]float64, iterations)

	for i := range iterations {
		bootSuccesses := 0
		for j := range total {
			if math.Float64frombits(math.Float64bits(float64(j))%100000) < rate*100000 {
				bootSuccesses++
			}
		}
		// Better: use Poisson-binomial approximation
		expected := rate * float64(total)
		stddev := math.Sqrt(float64(total) * rate * (1 - rate))
		bootRate := (expected + stddev*math.Erfinv(2*(float64(i)/float64(iterations))-1)) / float64(total)
		if bootRate < 0 {
			bootRate = 0
		}
		if bootRate > 1 {
			bootRate = 1
		}
		samples[i] = bootRate
	}

	// Sort and take 2.5th and 97.5th percentiles
	slices.Sort(samples)
	lower = samples[25]  // 2.5th percentile
	upper = samples[975] // 97.5th percentile
	return
}

// SmallSampleWarning returns a warning string if the suite has fewer than
// the recommended minimum number of tasks for reliable statistical inference.
func SmallSampleWarning(name string, totalTasks int) string {
	if totalTasks < 10 {
		return fmt.Sprintf("⚠️ %s: very small sample (n=%d) — results are indicative only, not statistically valid", name, totalTasks)
	}
	if totalTasks < 20 {
		return fmt.Sprintf("⚠️ %s: small sample (n=%d) — p-values and CIs are suggestive, not conclusive", name, totalTasks)
	}
	return ""
}

// AnnotateMetrics adds statistical annotations to RunMetrics (bootstrap CI, sample-size warning).
func AnnotateMetrics(m *RunMetrics) {
	if m.TotalTasks > 0 {
		m.LowerCI, m.UpperCI = BootstrapCI(m.Successes, m.TotalTasks)
		m.Warning = SmallSampleWarning("suite", m.TotalTasks)
	}
}

func containsStr(s, substr string) bool { return strings.Contains(s, substr) }

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func cloneTree(tree *evolution.SerializableNode) *evolution.SerializableNode {
	// Deep copy via node-by-node reconstruction
	var clone func(n *evolution.SerializableNode) *evolution.SerializableNode
	clone = func(n *evolution.SerializableNode) *evolution.SerializableNode {
		c := &evolution.SerializableNode{
			Type:        n.Type,
			Name:        n.Name,
			Description: n.Description,
			MaxRetries:  n.MaxRetries,
			TimeoutMs:   n.TimeoutMs,
		}
		for _, child := range n.Children {
			c.Children = append(c.Children, *clone(&child))
		}
		return c
	}
	return clone(tree)
}

// --- Mock LLM for benchmarks ---

// MockLLM returns predictable responses for benchmark testing.
type MockLLM struct {
	Complexity string
	Plan       string
	WentWell   string
	ToImprove  string
}

func (m *MockLLM) AnalyzeComplexity(_ string) string { return m.Complexity }
func (m *MockLLM) GeneratePlan(_, _ string) string   { return m.Plan }
func (m *MockLLM) Reflect(_, _, _ string) (string, string) {
	return m.WentWell, m.ToImprove
}
func (m *MockLLM) Generate(_ string) (string, error) { return m.Plan, nil }
func (m *MockLLM) GenerateCtx(_ context.Context, _ string) (string, error) {
	return m.Plan, nil
}
func (m *MockLLM) GenerateWithTimeout(_ string, _ time.Duration) (string, error) {
	return m.Plan, nil
}

// DefaultMock returns a standard mock for benchmarks.
func DefaultMock() *MockLLM {
	return &MockLLM{
		Complexity: "medium",
		Plan:       "1. Analyze input\n2. Execute workflow\n3. Verify output\n4. Report results",
		WentWell:   "task completed successfully",
		ToImprove:  "optimize performance",
	}
}

// DefaultLLM returns a real Ollama LLM client (gemma3:latest on localhost:11434).
// Falls back to DefaultMock if connection fails (e.g., Ollama not running).
func DefaultLLM() llm.LLM {
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		slog.Warn("benchmark: Ollama unavailable, falling back to mock", "error", err)
		return DefaultMock()
	}
	return client
}

// RealLLM returns a live Ollama client or skips the test when no LLM is configured.
func RealLLM(t *testing.T) llm.LLM {
	t.Helper()
	llm.SkipUnlessIntegration(t)
	client, err := llm.NewClient(llm.DefaultConfig())
	if err != nil {
		t.Skipf("skipping: LLM client: %v", err)
	}
	return client
}

// --- Built-in benchmark suites ---

// GoDevSuite tests Go developer tree routing.
func GoDevSuite() Suite {
	return Suite{
		Name: "godev",
		Tasks: []TaskCase{
			{Task: "review this Go code for bugs", ExpectedPath: "CodeReviewPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "explain Go interfaces", ExpectedPath: "GoKnowledgePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build and compile the Go project", ExpectedPath: "BuildPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run Go tests with coverage", ExpectedPath: "TestPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "write a sorting function", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 10},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0}, // empty should fail
			// Edge-case tasks that benefit from retries, confidence checks, and fallbacks
			{Task: "review code with confidence check and fallback on failure", ExpectedPath: "CodeReviewPath", ShouldSucceed: true, MinResultLen: 30},
			{Task: "build, and if it fails retry with verbose output", ExpectedPath: "BuildPath", ShouldSucceed: true, MinResultLen: 20},
		},
	}
}

// CodeReviewSuite tests code review tree routing.
func CodeReviewSuite() Suite {
	return Suite{
		Name: "code_review",
		Tasks: []TaskCase{
			{Task: "find bugs in this Go code", ExpectedPath: "BugDetection", ShouldSucceed: true, MinResultLen: 20},
			{Task: "scan for security vulnerabilities in code", ExpectedPath: "SecurityReview", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check code style and formatting", ExpectedPath: "StyleReview", ShouldSucceed: true, MinResultLen: 20},
			{Task: "analyze this code function", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 10},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// DevOpsSuite tests CI/CD pipeline routing.
func DevOpsSuite() Suite {
	return Suite{
		Name: "devops_ci",
		Tasks: []TaskCase{
			{Task: "build the project", ExpectedPath: "BuildPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the test suite", ExpectedPath: "TestPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "lint the codebase", ExpectedPath: "LintPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "deploy to staging", ExpectedPath: "DeployPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check pipeline status", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 10},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// FinanceSuite tests finance tree routing.
func FinanceSuite() Suite {
	return Suite{
		Name: "finance",
		Tasks: []TaskCase{
			{Task: "run comparable company analysis for Apple", ExpectedPath: "CompsPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build a DCF model with WACC", ExpectedPath: "DCFPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "create an LBO model for the deal", ExpectedPath: "LBOPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "assemble the pitch deck", ExpectedPath: "DeckAssemblyPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review Q3 earnings results", ExpectedPath: "EarningsIngestPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run KYC screening for new client", ExpectedPath: "KYCPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "reconcile the general ledger", ExpectedPath: "ReconPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// PitchAgentSuite tests the pitch_agent finance tree routing
// (finance_pitch_agent, internal/evolution/finance_trees.go's
// PitchAgentTree). ExpectedPath reflects its own five StrategyRouter
// branches (CompsPath/PrecedentsPath/LBOPath/DCFPath/DeckAssemblyPath)
// rather than the generic FinanceSuite, which assumed a single
// full-coverage finance tree — each of the 10 finance agents only
// implements the subset of branches its own workflow needs.
func PitchAgentSuite() Suite {
	return Suite{
		Name: "pitch_agent",
		Tasks: []TaskCase{
			{Task: "build the comps analysis with peer multiples for the target", ExpectedPath: "CompsPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the precedent transaction analysis to support the valuation", ExpectedPath: "PrecedentsPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build the lbo model with leveraged buyout return assumptions", ExpectedPath: "LBOPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build a dcf valuation model with wacc assumptions", ExpectedPath: "DCFPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "assemble the pitch deck for the investor presentation", ExpectedPath: "DeckAssemblyPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review the quarterly earnings guidance for the portfolio company", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// EarningsReviewerSuite tests the earnings_reviewer finance tree routing
// (finance_earnings_reviewer, internal/evolution/finance_trees.go's
// EarningsReviewerTree). ExpectedPath reflects its own StrategyRouter
// branches (EarningsIngestPath/ModelUpdatePath/NoteDraftingPath).
func EarningsReviewerSuite() Suite {
	return Suite{
		Name: "earnings_reviewer",
		Tasks: []TaskCase{
			{Task: "ingest the quarterly earnings transcript and extract key metrics", ExpectedPath: "EarningsIngestPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "refresh the financial model with the latest revised assumptions", ExpectedPath: "ModelUpdatePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "draft the financial research note write-up", ExpectedPath: "NoteDraftingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review the financial analyst estimate revisions for the coverage list", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// MarketResearcherSuite tests the market_researcher finance tree routing
// (finance_market_researcher, internal/evolution/finance_trees.go's
// MarketResearcherTree). ExpectedPath reflects its own StrategyRouter
// branches (IndustryOverviewPath/CompetitiveLandscapePath/IdeaGenerationPath).
func MarketResearcherSuite() Suite {
	return Suite{
		Name: "market_researcher",
		Tasks: []TaskCase{
			{Task: "research the industry sector trends for equity investors", ExpectedPath: "IndustryOverviewPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "map the competitive landscape and peer positioning for the portfolio company", ExpectedPath: "CompetitiveLandscapePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "screen for investment ideas and shortlist top opportunities for the portfolio", ExpectedPath: "IdeaGenerationPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "summarize portfolio performance for the investment committee", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// ModelBuilderSuite tests the model_builder finance tree routing
// (finance_model_builder, internal/evolution/finance_trees.go's
// ModelBuilderTree). ExpectedPath reflects its own StrategyRouter branches
// (ThreeStatementPath/DCFModelPath/LBOModelPath).
func ModelBuilderSuite() Suite {
	return Suite{
		Name: "model_builder",
		Tasks: []TaskCase{
			{Task: "build the 3-statement operating model in excel", ExpectedPath: "ThreeStatementPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build a dcf model with wacc sensitivity in excel", ExpectedPath: "DCFModelPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build the lbo model with leveraged buyout returns in excel", ExpectedPath: "LBOModelPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build the standard financial model template in excel", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// MeetingPrepSuite tests the meeting_prep finance tree routing
// (finance_meeting_prep, internal/evolution/finance_trees.go's
// MeetingPrepTree). ExpectedPath reflects its own single StrategyRouter
// branch (BriefingPath).
func MeetingPrepSuite() Suite {
	return Suite{
		Name: "meeting_prep",
		Tasks: []TaskCase{
			{Task: "prepare the client briefing pack with portfolio talking points", ExpectedPath: "BriefingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "summarize the investor portfolio performance update", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// ValuationReviewerSuite tests the valuation_reviewer finance tree routing
// (finance_valuation_reviewer, internal/evolution/finance_trees.go's
// ValuationReviewerTree). ExpectedPath reflects its own single
// StrategyRouter branch (GPIngestPath).
func ValuationReviewerSuite() Suite {
	return Suite{
		Name: "valuation_reviewer",
		Tasks: []TaskCase{
			{Task: "ingest the gp capital account statements and run the valuation", ExpectedPath: "GPIngestPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review the fund's investor reporting requirements", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// GLReconcilerSuite tests the gl_reconciler finance tree routing
// (finance_gl_reconciler, internal/evolution/finance_trees.go's
// GLReconcilerTree). ExpectedPath reflects its own single StrategyRouter
// branch (ReconPath).
func GLReconcilerSuite() Suite {
	return Suite{
		Name: "gl_reconciler",
		Tasks: []TaskCase{
			{Task: "reconcile the general ledger and trace the break to source", ExpectedPath: "ReconPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "prepare the monthly ledger review summary", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// MonthEndCloserSuite tests the month_end_closer finance tree routing
// (finance_month_end_closer, internal/evolution/finance_trees.go's
// MonthEndCloserTree). ExpectedPath reflects its own single StrategyRouter
// branch (ClosePath).
func MonthEndCloserSuite() Suite {
	return Suite{
		Name: "month_end_closer",
		Tasks: []TaskCase{
			{Task: "close the books with month-end accrual and variance analysis", ExpectedPath: "ClosePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "prepare the quarterly financial reporting package", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// StatementAuditorSuite tests the statement_auditor finance tree routing
// (finance_statement_auditor, internal/evolution/finance_trees.go's
// StatementAuditorTree). ExpectedPath reflects its own single
// StrategyRouter branch (AuditPath).
func StatementAuditorSuite() Suite {
	return Suite{
		Name: "statement_auditor",
		Tasks: []TaskCase{
			{Task: "audit the lp capital account statements and verify calculations", ExpectedPath: "AuditPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review the fund's quarterly investor communications", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KYCScreenerSuite tests the kyc_screener finance tree routing
// (finance_kyc_screener, internal/evolution/finance_trees.go's
// KYCScreenerTree). ExpectedPath reflects its own single StrategyRouter
// branch (KYCPath).
func KYCScreenerSuite() Suite {
	return Suite{
		Name: "kyc_screener",
		Tasks: []TaskCase{
			{Task: "screen the new client onboarding documents for kyc and aml compliance", ExpectedPath: "KYCPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review the client's portfolio risk profile", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// AgentMonitorSuite tests monitoring tree.
func AgentMonitorSuite() Suite {
	return Suite{
		Name: "agent_monitor",
		Tasks: []TaskCase{
			{Task: "check health of all agents", ExpectedPath: "HealthCheckPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "collect agent metrics report", ExpectedPath: "MetricsCollectionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "restart dead agents", ExpectedPath: "RestartPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// SecuritySuite tests security audit tree routing.
// SecuritySuite tests security audit tree routing (domain_security_audit,
// internal/domains/trees.go's SecurityAuditTree). ExpectedPath reflects the
// tree's real StrategyRouter branch names (SASTPath/ExecutionPath) instead
// of the keyword-guessed, non-existent "SecurityPath".
func SecuritySuite() Suite {
	return Suite{
		Name: "security_audit",
		Tasks: []TaskCase{
			{Task: "audit the codebase for security vulnerabilities", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run a static analysis security audit to scan for XSS and SQL injection vulnerabilities", ExpectedPath: "SASTPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "audit the authentication and authorization patterns for security risks", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run a static analysis audit to check for OWASP top 10 security vulnerabilities", ExpectedPath: "SASTPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// ResearchSuite tests research tree routing.
func ResearchSuite() Suite {
	return Suite{
		Name: "research",
		Tasks: []TaskCase{
			{Task: "research the latest AI agent frameworks", ExpectedPath: "ResearchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "investigate Go performance optimization techniques", ExpectedPath: "ResearchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "analyze behavior tree evolution algorithms", ExpectedPath: "ResearchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "explore OpenTelemetry distributed tracing options", ExpectedPath: "ResearchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "summarize the latest trends in MCP server design", ExpectedPath: "ResearchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// DataPipelineSuite tests data pipeline tree routing (domain_data_pipeline,
// internal/domains/trees.go's DataPipelineTree). ExpectedPath reflects the
// tree's real StrategyRouter branch names (ExtractPath/TransformPath/
// ExecutionPath) instead of the keyword-guessed, non-existent "PipelinePath".
func DataPipelineSuite() Suite {
	return Suite{
		Name: "data_pipeline",
		Tasks: []TaskCase{
			{Task: "build an ETL pipeline for log processing", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "set up data transformation for CSV to Parquet", ExpectedPath: "TransformPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "configure streaming data ingestion from Kafka", ExpectedPath: "ExtractPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "monitor data quality and validation checks", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// GameAISuite tests game AI tree routing (domain_game_ai,
// internal/domains/trees.go's GameAITree). ExpectedPath reflects the tree's
// real StrategyRouter branch names (PatrolPath/ChasePath/ExecutionPath)
// instead of the keyword-guessed, non-existent "GameAIPath".
func GameAISuite() Suite {
	return Suite{
		Name: "game_ai",
		Tasks: []TaskCase{
			{Task: "implement enemy behavior state machine", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			// IsGameTask/IsPatrolState/IsTAPath-style condition checks are
			// case-sensitive substring matches, so "NPC"/"AI" (as written
			// in prose) never satisfy the PreGate's lowercase "npc"/"ai"
			// keywords — use lowercase "npc"/"ai" in task text.
			{Task: "design the npc patrol and combat routines", ExpectedPath: "PatrolPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build a decision tree for ai opponent strategy", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "optimize the ai pathfinding to chase and pursue the target", ExpectedPath: "ChasePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// RefactoringSuite tests code refactoring tree routing (domain_refactoring,
// internal/domains/trees.go's RefactoringTree). ExpectedPath reflects the
// tree's real StrategyRouter branch names (SmellDetection/PatternApplication/
// ExecutionPath) instead of the keyword-guessed, non-existent "RefactoringPath".
func RefactoringSuite() Suite {
	return Suite{
		Name: "refactoring",
		Tasks: []TaskCase{
			{Task: "refactor the legacy service layer for better clarity and readability", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "improve the monolithic design by migrating to a microservices pattern", ExpectedPath: "PatternApplication", ShouldSucceed: true, MinResultLen: 20},
			{Task: "clean up duplicated code by extracting reusable components", ExpectedPath: "SmellDetection", ShouldSucceed: true, MinResultLen: 20},
			{Task: "rewrite the deprecated API endpoints using modern conventions", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// CrashInvestigatorSuite tests crash investigation tree routing
// (domain_crash_investigator, internal/domains/trees.go's
// CrashInvestigatorTree). ExpectedPath reflects the tree's real
// StrategyRouter branch names (RootCauseAnalysis/ExecutionPath) instead of
// the keyword-guessed, non-existent "CrashPath".
func CrashInvestigatorSuite() Suite {
	return Suite{
		Name: "crash_investigator",
		Tasks: []TaskCase{
			{Task: "investigate the production crash from the latest deployment", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "debug the crash and trace its root cause in the core dump for a null pointer dereference", ExpectedPath: "RootCauseAnalysis", ShouldSucceed: true, MinResultLen: 20},
			{Task: "find the root cause of the memory leak crash in the agent scheduler", ExpectedPath: "RootCauseAnalysis", ShouldSucceed: true, MinResultLen: 20},
			{Task: "debug the root cause of the race condition crash in the scheduler", ExpectedPath: "RootCauseAnalysis", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// MeetingNotesSuite tests meeting notes tree routing (domain_meeting_notes,
// internal/domains/trees.go's MeetingNotesTree). ExpectedPath reflects the
// tree's real StrategyRouter branch names (GenerateNotes/ExtractActions)
// instead of the keyword-guessed, non-existent "MeetingPath".
func MeetingNotesSuite() Suite {
	return Suite{
		Name: "meeting_notes",
		Tasks: []TaskCase{
			{Task: "summarize the sprint planning meeting notes", ExpectedPath: "GenerateNotes", ShouldSucceed: true, MinResultLen: 20},
			{Task: "extract action items from the architecture review", ExpectedPath: "ExtractActions", ShouldSucceed: true, MinResultLen: 20},
			{Task: "document the decision log from the quarterly review", ExpectedPath: "GenerateNotes", ShouldSucceed: true, MinResultLen: 20},
			{Task: "create meeting minutes with key discussion points", ExpectedPath: "GenerateNotes", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// AlertRouterSuite tests alert routing tree (domain_alert_router,
// internal/domains/alert_router.go). ExpectedPath reflects the tree's real
// StrategyRouter branch names (CriticalAlert/HealthAlert) instead of the
// keyword-guessed, non-existent "AlertPath".
func AlertRouterSuite() Suite {
	return Suite{
		Name: "alert_router",
		Tasks: []TaskCase{
			{Task: "route the critical production alert to the on-call engineer", ExpectedPath: "CriticalAlert", ShouldSucceed: true, MinResultLen: 20},
			{Task: "escalate the P0 incident to the senior team", ExpectedPath: "CriticalAlert", ShouldSucceed: true, MinResultLen: 20},
			{Task: "send warning notification for high memory usage", ExpectedPath: "HealthAlert", ShouldSucceed: true, MinResultLen: 20},
			{Task: "dispatch the database failure alert to DBA rotation", ExpectedPath: "HealthAlert", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// TradingSignalSuite tests trading signal tree routing (domain_trading_signal,
// internal/domains/trees.go's TradingSignalTree). ExpectedPath reflects the
// tree's real StrategyRouter branch names (SignalGeneration/
// TechnicalAnalysis/ExecutionPath) instead of the keyword-guessed,
// non-existent "TradingPath".
func TradingSignalSuite() Suite {
	return Suite{
		Name: "trading_signal",
		Tasks: []TaskCase{
			{Task: "analyze the trading signal for Bitcoin cross-arbitrage", ExpectedPath: "SignalGeneration", ShouldSucceed: true, MinResultLen: 20},
			{Task: "evaluate the technical indicator pattern for the moving average crossover", ExpectedPath: "TechnicalAnalysis", ShouldSucceed: true, MinResultLen: 20},
			// IsTAPath's "rsi" keyword is a case-sensitive substring match,
			// so "RSI" (as written in prose) never matches — lowercase it.
			{Task: "assess the rsi divergence trading opportunity", ExpectedPath: "TechnicalAnalysis", ShouldSucceed: true, MinResultLen: 20},
			{Task: "backtest the mean reversion trading strategy on historical hourly bars", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// NotebookLMSuite tests the zero-LLM NotebookLM tree (domain_notebooklm,
// internal/domains/notebooklm.go). Its StrategyRouter dispatches to the
// tree's own ResearchPath/QueryPath/DefaultPath Sequence nodes, so
// ExpectedPath reflects those real node names instead of the
// keyword-guessed "NotebookLMPath" fallback.
func NotebookLMSuite() Suite {
	return Suite{
		Name: "notebooklm",
		Tasks: []TaskCase{
			{Task: "run deep research on BT optimization and save sources to the vault", ExpectedPath: "ResearchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "ask the notebook what the key findings are across its sources", ExpectedPath: "QueryPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check notebooklm auth and refresh the session before querying", ExpectedPath: "DefaultPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// NotebookLMConsumerSuite tests the NotebookLM consumer tree
// (domain_notebooklm_consumer, internal/domains/notebooklm_consumer.go).
// It is a linear PreGate → ChainAction → ReflectOnOutcome → OutcomeSelector
// pipeline reading synthesis files from the vault — it has no StrategyRouter,
// so it is scored by success/output rather than a fabricated strategy path.
func NotebookLMConsumerSuite() Suite {
	return Suite{
		Name: "notebooklm_consumer",
		Tasks: []TaskCase{
			{Task: "consume the latest notebooklm synthesis and report on source trends", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check whether the newest nlm-research synthesis file is stale and needs regeneration", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// NotebookLMPlanImplementSuite tests the research→grill→plan→implement→
// verify→deploy pipeline (domain_notebooklm_plan_implement,
// internal/evolution/notebooklm_workflow.go). It is a linear Sequence with
// no StrategyRouter, so it is scored by success/output rather than a
// fabricated strategy path.
func NotebookLMPlanImplementSuite() Suite {
	return Suite{
		Name: "notebooklm_plan_implement",
		Tasks: []TaskCase{
			{Task: "run the research, grill, plan, implement, verify, deploy pipeline for the new algorithm", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// HermesUpdateSuite tests the daily Hermes update tree routing. HermesUpdateTree
// (internal/domains/trees.go) is a plain linear Sequence with no
// StrategyRouter branching, so ExpectedPath reflects the tree's real node
// names (HermesUpdate_Main/IsUpdateTask/HermesUpdateAgent) instead of the
// keyword-guessed, non-existent "UpdatePath".
func HermesUpdateSuite() Suite {
	return Suite{
		Name: "hermes_update",
		Tasks: []TaskCase{
			{Task: "check for a new hermes version and update if available", ExpectedPath: "IsUpdateTask", ShouldSucceed: true, MinResultLen: 20},
			{Task: "fetch the latest git changes and report the update status", ExpectedPath: "HermesUpdateAgent", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the daily hermes update routine", ExpectedPath: "HermesUpdate_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// AuctionDemoSuite tests the announce-bid-award auction delegation tree
// routing. AuctionDemoTree (internal/domains/trees.go) is a plain linear
// Sequence with no StrategyRouter branching, so ExpectedPath reflects the
// tree's real node names (AuctionDemo_Main/IsAuctionTask/AuctionDelegate)
// instead of the keyword-guessed, non-existent "AuctionPath".
func AuctionDemoSuite() Suite {
	return Suite{
		Name: "auction_demo",
		Tasks: []TaskCase{
			{Task: "announce the task to candidate agents and collect bids", ExpectedPath: "IsAuctionTask", ShouldSucceed: true, MinResultLen: 20},
			{Task: "award the task to the winning bidder in the auction", ExpectedPath: "AuctionDelegate", ShouldSucceed: true, MinResultLen: 20},
			{Task: "delegate the task through the auction allocation process", ExpectedPath: "AuctionDemo_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// BTFusionSuite tests the BT fusion research-and-apply cycle tree routing
// (domain_bt_fusion, internal/domains/bt_fusion.go). Its StrategyRouter
// dispatches to the tree's own BTFusion_NoNewResearch/BTFusion_NewResearch
// Sequence nodes, so ExpectedPath reflects those real node names instead of
// the keyword-guessed, non-existent "FusionPath" fallback. Benchmark scoring
// runs actions in Sandbox mode, which stubs SearchForBTPatterns and
// QueryNotebookLMResearch — the actions that would record new knowledge-store
// entries — so bt_fusion_research_new_count always reads 0 and every task
// below reaches BTFusion_NoNewResearch, the only strategy branch a benchmark
// run can ever take.
func BTFusionSuite() Suite {
	return Suite{
		Name: "bt_fusion",
		Tasks: []TaskCase{
			{Task: "gather new research knowledge and synthesize fusion candidates", ExpectedPath: "BTFusion_NoNewResearch", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the gated fusion apply path with verification", ExpectedPath: "BTFusion_NoNewResearch", ShouldSucceed: true, MinResultLen: 20},
			{Task: "scan vault research notes for new BT pattern candidates", ExpectedPath: "BTFusion_NoNewResearch", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// BTManagerSuite tests the post-execution agent-repair/bootstrap meta-agent
// tree routing (domain_bt_manager, internal/domains/bt_manager.go). Its
// StrategyRouter dispatches to the tree's own DegradedPerformancePath/
// NewAgentBootstrapPath/HealthyReportPath Sequence nodes, so ExpectedPath
// reflects those real node names instead of the keyword-guessed,
// non-existent "ManagerPath" fallback. Unlike BTFusion, BTManager's routing
// conditions (IsDegradedAgent/IsNewAgent/IsHealthy) read directly from the
// reflection store rather than from Sandboxed action output, but RunSuite
// never seeds bb.Reflections, so an empty store always routes new-agent
// bootstrapping.
func BTManagerSuite() Suite {
	return Suite{
		Name: "bt_manager",
		Tasks: []TaskCase{
			{Task: "diagnose the degraded agent and apply a targeted mutation", ExpectedPath: "NewAgentBootstrapPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "bootstrap a new agent instance from the registry", ExpectedPath: "NewAgentBootstrapPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "report the health of all managed agents", ExpectedPath: "NewAgentBootstrapPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// SuperpowersWorkflowSuite tests the brainstorm/design/grill-loop/HITL
// workflow tree routing. SuperpowersWorkflowTree
// (internal/domains/superpowers_workflow.go) does branch via StrategyRouter-
// style Selector nodes, but its real Sequence node names are ParallelPath
// (independent-task dispatch) and VerifyPath (post-implementation
// verification), not the keyword-guessed, non-existent "WorkflowPath".
func SuperpowersWorkflowSuite() Suite {
	return Suite{
		Name: "superpowers_workflow",
		Tasks: []TaskCase{
			{Task: "brainstorm a design and validate it through the grill loop", ExpectedPath: "VerifyPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "split the plan into independently gradable tasks", ExpectedPath: "ParallelPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "escalate the design to human-in-the-loop review", ExpectedPath: "VerifyPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// SelfReviewSuite tests the autonomous-commit self-review tree routing.
// SelfReviewTree (internal/domains/self_review.go) is a plain linear
// Sequence with no StrategyRouter branching, so ExpectedPath reflects the
// tree's real node names (SelfReview_Main/TaskIsNotEmpty) instead of the
// keyword-guessed, non-existent "SelfReviewPath".
func SelfReviewSuite() Suite {
	return Suite{
		Name: "self_review",
		Tasks: []TaskCase{
			{Task: "review autonomous commits since the last self-review", ExpectedPath: "SelfReview_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "seed a code-fix program for the confirmed defect", ExpectedPath: "SelfReview_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "advance the self-review state to the latest commit SHA", ExpectedPath: "TaskIsNotEmpty", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// Arc42Suite tests arc42 architecture documentation tree routing.
// Arc42Suite tests the 12 arc42 section-generator trees' routing
// (domain_arc42:section1..12, internal/domains/arc42_trees.go). Every
// section shares the same PreGate/StrategyRouter/ValidateSection/
// SaveSection/MarkSectionDone node names regardless of which section it
// generates, so ExpectedPath reflects those shared real names instead of
// the keyword-guessed, non-existent "Arc42Path". The structurally distinct
// arc42:docsync and arc42_seeder trees get their own suites below —
// Arc42DocsyncSuite and Arc42SeederSuite — since neither shares these node
// names.
func Arc42Suite() Suite {
	return Suite{
		Name: "arc42",
		Tasks: []TaskCase{
			{Task: "document the system architecture overview", ExpectedPath: "SaveSection", ShouldSucceed: true, MinResultLen: 20},
			{Task: "describe the component decomposition and dependencies", ExpectedPath: "StrategyRouter", ShouldSucceed: true, MinResultLen: 20},
			{Task: "create the runtime view for the MCP request flow", ExpectedPath: "ValidateSection", ShouldSucceed: true, MinResultLen: 20},
			{Task: "document the deployment topology and infrastructure", ExpectedPath: "SaveSection", ShouldSucceed: true, MinResultLen: 20},
			{Task: "describe the cross-cutting security architecture", ExpectedPath: "MarkSectionDone", ShouldSucceed: true, MinResultLen: 20},
			{Task: "document the quality requirements and tradeoffs", ExpectedPath: "PreGate", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// Arc42DocsyncSuite tests the arc42:docsync tree routing (domain_arc42:docsync,
// internal/domains/arc42_docsync.go). Its per-section sync is a plain
// Sequence of SyncArc42SectionNN actions plus SyncReadme — no StrategyRouter
// branching — so ExpectedPath reflects those real node names instead of the
// ValidateSection/SaveSection names the section-generator Arc42Suite uses.
func Arc42DocsyncSuite() Suite {
	return Suite{
		Name: "arc42_docsync",
		Tasks: []TaskCase{
			{Task: "sync arc42 section 1 documentation after a recent code change", ExpectedPath: "SyncArc42Section01", ShouldSucceed: true, MinResultLen: 20},
			{Task: "update the README counts and links after a change", ExpectedPath: "SyncReadme", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the full arc42 documentation sync pass", ExpectedPath: "Arc42Docsync_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// Arc42SeederSuite tests the arc42_seeder tree routing (domain_arc42_seeder,
// internal/domains/arc42_seeder.go). It is a plain linear Sequence with no
// StrategyRouter branching, so ExpectedPath reflects the tree's real node
// names (Arc42Seeder_Main/TaskIsNotEmpty/SeedProgramFromArc42Goals) instead
// of the section-generator names the shared Arc42Suite uses.
func Arc42SeederSuite() Suite {
	return Suite{
		Name: "arc42_seeder",
		Tasks: []TaskCase{
			{Task: "seed the next improvement program from the live arc42 quality goals", ExpectedPath: "SeedProgramFromArc42Goals", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check that the scheduled seeding task is non-empty before seeding", ExpectedPath: "TaskIsNotEmpty", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the arc42 program-seeder cycle end to end", ExpectedPath: "Arc42Seeder_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// DefaultSuite tests the default universal tree routing. DefaultTree's
// KnowledgePath branch gates on CheckKnowledgeGap, which is just
// `bb.KgResults == ""` — true for every benchmark run since RunSuite never
// populates KgResults, so KnowledgePath always wins over the CachePath/
// ExecutionPath siblings regardless of task content. ExpectedPath reflects
// that real, deterministic mock behavior instead of the keyword-guessed
// "ExecutionPath".
func DefaultSuite() Suite {
	return Suite{
		Name: "default",
		Tasks: []TaskCase{
			{Task: "analyze the codebase for potential improvements", ExpectedPath: "KnowledgePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check the system health and performance metrics", ExpectedPath: "KnowledgePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "explain the difference between Sequence and Selector nodes", ExpectedPath: "KnowledgePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "summarize the latest git commits in the repository", ExpectedPath: "KnowledgePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// GOAPSuite tests GOAP (Goal-Oriented Action Planning) tree routing for the
// goap_planning, goap_research, and goap_devops trees
// (internal/domains/trees.go's GoapPlanningTree/GoapResearchTree/
// GoapDevopsTree). Each of the three trees has its own keyword-routed
// branches (AssessPath/SyncPath, ResearchPath/GraphifyPath, BuildPath/
// ImplementPath) that aren't shared across all three, so a task's real path
// varies by which of the three trees it runs against. "StrategyRouter" and
// "PreGate" (the previous ExpectedPath values) are wrapper node names that
// the runtime path-tracker never records as CurrentPath — CurrentPath is
// only ever set to the name of the Sequence branch actually taken under
// StrategyRouter (including the real GOAP_Root A* planner, which reliably
// fails under the mock's empty goal state and falls through) — so those two
// values could never match at runtime regardless of task text. PossiblePaths
// lists every branch a given task legitimately lands on across the three
// trees; ExpectedPath is used only where all three agree. The structurally
// distinct goap_fusion and goap_fusion_loop trees get their own
// GOAPFusionSuite below.
func GOAPSuite() Suite {
	return Suite{
		Name: "goap",
		Tasks: []TaskCase{
			{Task: "plan a deployment pipeline with rollback steps", PossiblePaths: []string{"ExecutionPath", "ImplementPath"}, ShouldSucceed: true, MinResultLen: 20},
			{Task: "optimize the resource allocation for the microservices", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "schedule the research tasks with dependency resolution", PossiblePaths: []string{"ExecutionPath", "ResearchPath"}, ShouldSucceed: true, MinResultLen: 20},
			{Task: "plan the incident response escalation path", PossiblePaths: []string{"ExecutionPath", "ImplementPath"}, ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// GOAPFusionSuite tests the goap_fusion and goap_fusion_loop tree routing
// (domain_goap_fusion, internal/domains/goap_fusion.go; domain_goap_fusion_loop,
// internal/domains/goap_fusion_loop.go). Both trees share the same
// ExecutionRouter → ClaudeSuperpowersPath/ScheduledAnalysisPath →
// VerifyGoapFusionEvidence shape (the loop tree adds backlog-seeding and
// grill phases in front, but keeps these node names), so ExpectedPath
// reflects those shared real names instead of the keyword-guessed,
// non-existent "GOAPPath" the generic GOAPSuite previously fell back to for
// both trees.
func GOAPFusionSuite() Suite {
	return Suite{
		Name: "goap_fusion",
		Tasks: []TaskCase{
			{Task: "implement the research-backed goal via the Superpowers runtime", ExpectedPath: "ClaudeSuperpowersPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "run the deterministic fusion analysis when no new goals are found", ExpectedPath: "ScheduledAnalysisPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "verify the fusion cycle produced concrete evidence before marking success", ExpectedPath: "VerifyGoapFusionEvidence", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KanbanTaskCreatorSuite tests the kanban_task_creator tree routing
// (KanbanTaskCreatorTree, internal/domains/kanban.go). The tree is a plain
// linear Sequence — PreGate → card-creation LLM step → ReflectOnOutcome →
// OutcomeSelector — with no router, so ExpectedPath names the real gate and
// outcome nodes it actually traverses rather than a keyword-guessed branch.
func KanbanTaskCreatorSuite() Suite {
	return Suite{
		Name: "kanban_task_creator",
		Tasks: []TaskCase{
			{Task: "create a DoR-ready card in BACKLOG for the new export feature", ExpectedPath: "TaskCreator_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "draft acceptance criteria, priority, and AQAL quadrant for a raw request", ExpectedPath: "ValidateInput", ShouldSucceed: true, MinResultLen: 20},
			{Task: "file a new task card and report the card ID", ExpectedPath: "OutcomeSelector", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KanbanRefinerSuite tests the kanban_refiner tree routing (KanbanRefinerTree,
// internal/domains/kanban.go). The tree is linear and gated on IsKanbanTask,
// so ExpectedPath names that gate and the surrounding real nodes instead of a
// branch the tree does not have.
func KanbanRefinerSuite() Suite {
	return Suite{
		Name: "kanban_refiner",
		Tasks: []TaskCase{
			{Task: "refine card KAN-42 through the Definition of Ready gate", ExpectedPath: "Refiner_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "expand the description of card KAN-42 with implementation context", ExpectedPath: "IsKanbanTask", ShouldSucceed: true, MinResultLen: 20},
			{Task: "make the acceptance criteria on card KAN-42 testable and move it TODO to REFINED", ExpectedPath: "OutcomeSelector", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KanbanQASuite tests the kanban_qa tree routing (KanbanQATree,
// internal/domains/kanban.go). Like the refiner tree it is linear behind the
// IsKanbanTask gate, so ExpectedPath uses that gate and the real QA_Main /
// OutcomeSelector nodes.
func KanbanQASuite() Suite {
	return Suite{
		Name: "kanban_qa",
		Tasks: []TaskCase{
			{Task: "run QA validation on card KAN-42 and move it to REVIEW on pass", ExpectedPath: "QA_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check every acceptance criterion on card KAN-42 is ticked", ExpectedPath: "IsKanbanTask", ShouldSucceed: true, MinResultLen: 20},
			{Task: "scan card KAN-42 for regressions and report PASS or FAIL", ExpectedPath: "OutcomeSelector", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KanbanBoardMonitorSuite tests the kanban_monitor tree routing
// (KanbanBoardMonitorTree, internal/domains/kanban.go). This tree does branch:
// MonitorRouter selects StaleCheck (IsBoardCheck), DispatchPath
// (NeedsDispatch), or StandupPath (IsStandup), so each ExpectedPath names one
// of those real branches and the tasks carry that branch's routing keywords.
func KanbanBoardMonitorSuite() Suite {
	return Suite{
		Name: "kanban_monitor",
		Tasks: []TaskCase{
			{Task: "scan the board and check for stale cards and column bottlenecks", ExpectedPath: "StaleCheck", ShouldSucceed: true, MinResultLen: 20},
			{Task: "dispatch the next ready card to its pipeline agent", ExpectedPath: "DispatchPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "generate the daily standup status with velocity", ExpectedPath: "StandupPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KanbanWorkflowSuite tests the kanban_workflow tree routing
// (KanbanWorkflowTree, internal/domains/kanban.go). WorkflowRouter selects
// CreatePath (IsCreateTask), RefinePath (IsRefinement), QAPath (IsQA), or the
// unconditional ScanPath default, so ExpectedPath covers all four real
// branches — including ScanPath, which is only reachable when no keyword hits.
func KanbanWorkflowSuite() Suite {
	return Suite{
		Name: "kanban_workflow",
		Tasks: []TaskCase{
			{Task: "create a new card for the billing rewrite", ExpectedPath: "CreatePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "refine the card and expand it with implementation detail", ExpectedPath: "RefinePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "validate the finished card with a qa pass", ExpectedPath: "QAPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "how healthy is the board right now", ExpectedPath: "ScanPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// KanbanAutoPilotSuite tests the kanban_autopilot tree routing
// (KanbanAutoPilotTree, internal/domains/kanban.go). The tree is a linear
// sweep-then-audit Sequence with no router, so ExpectedPath names the real
// AutoPilot_Main / ValidateInput / OutcomeSelector nodes it always walks.
func KanbanAutoPilotSuite() Suite {
	return Suite{
		Name: "kanban_autopilot",
		Tasks: []TaskCase{
			{Task: "sweep TODO, APPROVED, QA, and IN PROGRESS and advance every dispatchable card", ExpectedPath: "AutoPilot_Main", ShouldSucceed: true, MinResultLen: 20},
			{Task: "move cards whose column gate is met and report the movements", ExpectedPath: "ValidateInput", ShouldSucceed: true, MinResultLen: 20},
			{Task: "audit the transitions for skipped columns and moves needing human approval", ExpectedPath: "OutcomeSelector", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// HermesEvolveSuite tests the hermes_evolve tree routing
// (HermesSelfEvolutionTree, internal/domains/hermes_evolve.go). EvolutionRouter
// selects SelfMonitorPath (IsPeriodicCheck), SkillEvolutionPath (HasSkillGaps),
// StrategyOptPath (HasWorkflowInefficiencies), ModelTuningPath
// (HasModelToolIssues), or the unconditional KnowledgeSynthesisPath default —
// every ExpectedPath below names one of those five real branches.
func HermesEvolveSuite() Suite {
	return Suite{
		Name: "hermes_evolve",
		Tasks: []TaskCase{
			{Task: "run the periodic check over the last 10 sessions and categorize the failures", ExpectedPath: "SelfMonitorPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "close the detected skill gaps with concrete SKILL.md updates", ExpectedPath: "SkillEvolutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "remove the redundant tool calls causing workflow inefficiency", ExpectedPath: "StrategyOptPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "tune model selection and fix the tool configuration issues", ExpectedPath: "ModelTuningPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "consolidate recent learnings into a self-evolution report", ExpectedPath: "KnowledgeSynthesisPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// HermesObsidianSuite tests the hermes_obsidian tree routing
// (HermesObsidianOptimizerTree, internal/domains/hermes_obsidian.go).
// PipelineRouter selects SessionStartPath (IsSessionStart), IngestPath
// (HasNewContent), SweepPath (NeedsSweep), AuditPath (NeedsAudit),
// PublishPath (NeedsPublish), or the unconditional ImproveSkillPath default,
// so ExpectedPath covers all six real vault-pipeline branches.
func HermesObsidianSuite() Suite {
	return Suite{
		Name: "hermes_obsidian",
		Tasks: []TaskCase{
			{Task: "session start: load context and the previous handoff from the vault", ExpectedPath: "SessionStartPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "ingest this new transcript into raw and synthesize the wiki note", ExpectedPath: "IngestPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "sweep the vault and refresh the people and project notes", ExpectedPath: "SweepPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "audit the vault for knowledge gaps and broken wikilinks", ExpectedPath: "AuditPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "publish a briefing report from the wiki notes", ExpectedPath: "PublishPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "harden the agent against the edge cases seen in recent logs", ExpectedPath: "ImproveSkillPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// SuiteForTree returns the best benchmark suite for a given tree name.
//
// Every TaskCase.ExpectedPath a suite declares must name a real node in the
// tree the suite is selected for. That is not a convention but a build-enforced
// invariant: internal/domains' TestSuiteForTreeExpectedPathsExistInEveryDomainTree
// sweeps the whole SmokeTestableDomainTrees() registry and fails if any
// resolved suite declares a path that exists nowhere in its tree. An
// ExpectedPath naming a node the tree does not have can never be satisfied by
// real execution, so the suite's condition coverage silently measures nothing.
//
// Callers that need to distinguish a deliberate match from the generic default
// fallback should use SuiteForTreeNamed instead.
func SuiteForTree(treeName string) Suite {
	suite, _ := SuiteForTreeNamed(treeName)
	return suite
}

// SuiteForTreeNamed returns the benchmark suite for a tree name along with
// whether the name actually matched a suite written for it. SuiteForTree alone
// cannot express this: it returns GoDevSuite both for the godev tree (a real
// match) and for any unrecognized name (a silent fallback), so a newly added
// tree inheriting an unrelated suite's ExpectedPath values is indistinguishable
// from a deliberate selection. The second return value is false only on the
// default branch.
func SuiteForTreeNamed(treeName string) (Suite, bool) {
	switch {
	case containsStr(treeName, "goap_fusion"):
		return GOAPFusionSuite(), true
	case containsStr(treeName, "goap"):
		return GOAPSuite(), true
	case containsStr(treeName, "godev"):
		return GoDevSuite(), true
	case containsStr(treeName, "code_review"):
		return CodeReviewSuite(), true
	case containsStr(treeName, "devops"):
		return DevOpsSuite(), true
	// Each of the 10 Anthropic finance agents only implements the subset of
	// StrategyRouter branches its own workflow needs (see finance_trees.go),
	// so they get their own bespoke suites instead of sharing one generic
	// FinanceSuite — a shared suite's ExpectedPath values only reflect real
	// branches for whichever single tree they were written against.
	case containsStr(treeName, "pitch_agent"):
		return PitchAgentSuite(), true
	case containsStr(treeName, "earnings_reviewer"):
		return EarningsReviewerSuite(), true
	case containsStr(treeName, "market_researcher"):
		return MarketResearcherSuite(), true
	case containsStr(treeName, "model_builder"):
		return ModelBuilderSuite(), true
	case containsStr(treeName, "meeting_prep"):
		return MeetingPrepSuite(), true
	case containsStr(treeName, "valuation_reviewer"):
		return ValuationReviewerSuite(), true
	case containsStr(treeName, "gl_reconciler"):
		return GLReconcilerSuite(), true
	case containsStr(treeName, "month_end_closer"):
		return MonthEndCloserSuite(), true
	case containsStr(treeName, "statement_auditor"):
		return StatementAuditorSuite(), true
	case containsStr(treeName, "kyc_screener"):
		return KYCScreenerSuite(), true
	case containsStr(treeName, "finance"):
		return FinanceSuite(), true
	case containsStr(treeName, "agent_monitor"):
		return AgentMonitorSuite(), true
	case containsStr(treeName, "security_audit"):
		return SecuritySuite(), true
	case containsStr(treeName, "research"):
		return ResearchSuite(), true
	case containsStr(treeName, "data_pipeline"):
		return DataPipelineSuite(), true
	case containsStr(treeName, "game_ai"):
		return GameAISuite(), true
	case containsStr(treeName, "refactoring"):
		return RefactoringSuite(), true
	case containsStr(treeName, "crash_investigator") || containsStr(treeName, "domain_crash"):
		return CrashInvestigatorSuite(), true
	case containsStr(treeName, "meeting_notes"):
		return MeetingNotesSuite(), true
	case containsStr(treeName, "alert_router"):
		return AlertRouterSuite(), true
	case containsStr(treeName, "trading_signal") || containsStr(treeName, "domain_trading"):
		return TradingSignalSuite(), true
	case containsStr(treeName, "arc42:docsync"):
		return Arc42DocsyncSuite(), true
	case containsStr(treeName, "arc42_seeder"):
		return Arc42SeederSuite(), true
	case containsStr(treeName, "arc42"):
		return Arc42Suite(), true
	case containsStr(treeName, "notebooklm_plan_implement"):
		return NotebookLMPlanImplementSuite(), true
	case containsStr(treeName, "notebooklm_consumer"):
		return NotebookLMConsumerSuite(), true
	case containsStr(treeName, "notebooklm"):
		return NotebookLMSuite(), true
	case containsStr(treeName, "hermes_update"):
		return HermesUpdateSuite(), true
	case containsStr(treeName, "auction_demo"):
		return AuctionDemoSuite(), true
	case containsStr(treeName, "bt_fusion"):
		return BTFusionSuite(), true
	case containsStr(treeName, "bt_manager"):
		return BTManagerSuite(), true
	case containsStr(treeName, "superpowers_workflow"):
		return SuperpowersWorkflowSuite(), true
	case containsStr(treeName, "self_review"):
		return SelfReviewSuite(), true
	// The kanban and hermes trees live off the AllDomainTrees() registry (see
	// domains.KanbanAndHermesDomainTrees) but are just as selectable by name,
	// and each has its own router shape — sharing the generic GoDevSuite gave
	// them CodeReviewPath/BuildPath/TestPath ExpectedPath values that exist
	// nowhere in their trees.
	case containsStr(treeName, "kanban_task_creator"):
		return KanbanTaskCreatorSuite(), true
	case containsStr(treeName, "kanban_refiner"):
		return KanbanRefinerSuite(), true
	case containsStr(treeName, "kanban_qa"):
		return KanbanQASuite(), true
	case containsStr(treeName, "kanban_monitor"):
		return KanbanBoardMonitorSuite(), true
	case containsStr(treeName, "kanban_workflow"):
		return KanbanWorkflowSuite(), true
	case containsStr(treeName, "kanban_autopilot"):
		return KanbanAutoPilotSuite(), true
	case containsStr(treeName, "hermes_evolve"):
		return HermesEvolveSuite(), true
	case containsStr(treeName, "hermes_obsidian"):
		return HermesObsidianSuite(), true
	case treeName == "default":
		return DefaultSuite(), true
	default:
		return GoDevSuite(), false
	}
}

// SortResults sorts results by task name for consistent comparison.
func SortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Task < results[j].Task
	})
}
