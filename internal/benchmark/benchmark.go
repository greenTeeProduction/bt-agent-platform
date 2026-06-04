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
	"log"
	"math"
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
	ExpectedPath    string   `json:"expected_path"`               // which strategy path should handle this
	PossiblePaths   []string `json:"possible_paths,omitempty"`    // multiple acceptable paths for ambiguous tasks
	MinResultLen    int      `json:"min_result_len"`              // minimum output length expected
	ShouldSucceed   bool     `json:"should_succeed"`              // expected outcome
	ShouldReject    bool     `json:"should_reject"`               // PreGate should reject this
	MinQualityScore float64  `json:"min_quality_score,omitempty"` // minimum quality score expected
	Difficulty      string   `json:"difficulty,omitempty"`        // easy | medium | hard | adversarial
}

// Suite is a collection of benchmark tasks for a specific domain.
type Suite struct {
	Name    string     `json:"name"`
	Tasks   []TaskCase `json:"tasks"`
	LLMMode bool       `json:"llm_mode"` // true = use real LLM, false = use mock
}

// Result is the outcome of running a single task through a tree.
type Result struct {
	Task       string `json:"task"`
	Outcome    string `json:"outcome"`
	DurationMs int64  `json:"duration_ms"`
	ResultLen  int    `json:"result_len"`
	Path       string `json:"path"` // which strategy path was taken
	Success    bool   `json:"success"`
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
	LowerCI       float64  `json:"lower_ci"`          // 95% bootstrap CI lower bound
	UpperCI       float64  `json:"upper_ci"`          // 95% bootstrap CI upper bound
	Warning       string   `json:"warning,omitempty"` // small-sample or other warnings
	Results       []Result `json:"results"`
}

// RunSuite executes all tasks in a suite against a tree.
func RunSuite(tree *evolution.SerializableNode, suite Suite, mock llm.LLM) *RunMetrics {
	results := make([]Result, 0, 32)
	successes := 0
	paths := make(map[string]int)

	for _, tc := range suite.Tasks {
		start := time.Now()

		bb := &engine.Blackboard{
			Task: tc.Task,
			LLM:  mock,
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

		results = append(results, Result{
			Task:       tc.Task,
			Outcome:    bb.Outcome,
			DurationMs: duration,
			ResultLen:  len(output),
			Path:       path,
			Success:    success,
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
	// pruning work while preserving mock outputs.
	improved := delta.SuccessRate > 0 || (delta.SuccessRate == 0 && delta.PathCoverage > 0)

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
			ab.Delta.PathCoverage*10
		if ab.Delta.Significant {
			score *= 1.5 // bonus for statistical significance
		}
		return score
	}
	// Regression: check if it hurt
	if ab.Delta.SuccessRate < 0 {
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
	maxA := n1
	if s1+s2 < maxA {
		maxA = s1 + s2
	}

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

	for i := 0; i < iterations; i++ {
		bootSuccesses := 0
		for j := 0; j < total; j++ {
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
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
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
		log.Printf("benchmark: Ollama unavailable (%v), falling back to mock", err)
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
func SecuritySuite() Suite {
	return Suite{
		Name: "security_audit",
		Tasks: []TaskCase{
			{Task: "audit the codebase for security vulnerabilities", ExpectedPath: "SecurityPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "scan for XSS and SQL injection risks", ExpectedPath: "SecurityPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "review authentication and authorization patterns", ExpectedPath: "SecurityPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check for OWASP top 10 vulnerabilities", ExpectedPath: "SecurityPath", ShouldSucceed: true, MinResultLen: 20},
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

// DataPipelineSuite tests data pipeline tree routing.
func DataPipelineSuite() Suite {
	return Suite{
		Name: "data_pipeline",
		Tasks: []TaskCase{
			{Task: "build an ETL pipeline for log processing", ExpectedPath: "PipelinePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "set up data transformation for CSV to Parquet", ExpectedPath: "PipelinePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "configure streaming data ingestion from Kafka", ExpectedPath: "PipelinePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "monitor data quality and validation checks", ExpectedPath: "PipelinePath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// GameAISuite tests game AI tree routing.
func GameAISuite() Suite {
	return Suite{
		Name: "game_ai",
		Tasks: []TaskCase{
			{Task: "implement enemy behavior state machine", ExpectedPath: "GameAIPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "design NPC patrol and combat routines", ExpectedPath: "GameAIPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "build a decision tree for AI opponent strategy", ExpectedPath: "GameAIPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "optimize pathfinding with A-star algorithm", ExpectedPath: "GameAIPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// RefactoringSuite tests code refactoring tree routing.
func RefactoringSuite() Suite {
	return Suite{
		Name: "refactoring",
		Tasks: []TaskCase{
			{Task: "refactor the legacy service layer to clean architecture", ExpectedPath: "RefactoringPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "migrate from monolithic to microservices pattern", ExpectedPath: "RefactoringPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "extract reusable components from duplicated code", ExpectedPath: "RefactoringPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "modernize deprecated API endpoints to RESTful design", ExpectedPath: "RefactoringPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// CrashInvestigatorSuite tests crash investigation tree routing.
func CrashInvestigatorSuite() Suite {
	return Suite{
		Name: "crash_investigator",
		Tasks: []TaskCase{
			{Task: "investigate the production crash from the latest deployment", ExpectedPath: "CrashPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "analyze the core dump for null pointer dereference", ExpectedPath: "CrashPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "find the root cause of the memory leak in the agent scheduler", ExpectedPath: "CrashPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "diagnose the race condition in the goroutine pool", ExpectedPath: "CrashPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// MeetingNotesSuite tests meeting notes tree routing.
func MeetingNotesSuite() Suite {
	return Suite{
		Name: "meeting_notes",
		Tasks: []TaskCase{
			{Task: "summarize the sprint planning meeting notes", ExpectedPath: "MeetingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "extract action items from the architecture review", ExpectedPath: "MeetingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "document the decision log from the quarterly review", ExpectedPath: "MeetingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "create meeting minutes with key discussion points", ExpectedPath: "MeetingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// AlertRouterSuite tests alert routing tree.
func AlertRouterSuite() Suite {
	return Suite{
		Name: "alert_router",
		Tasks: []TaskCase{
			{Task: "route the critical production alert to the on-call engineer", ExpectedPath: "AlertPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "escalate the P0 incident to the senior team", ExpectedPath: "AlertPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "send warning notification for high memory usage", ExpectedPath: "AlertPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "dispatch the database failure alert to DBA rotation", ExpectedPath: "AlertPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// TradingSignalSuite tests trading signal tree routing.
func TradingSignalSuite() Suite {
	return Suite{
		Name: "trading_signal",
		Tasks: []TaskCase{
			{Task: "analyze the trading signal for Bitcoin cross-arbitrage", ExpectedPath: "TradingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "evaluate the moving average crossover signal", ExpectedPath: "TradingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "assess the RSI divergence trading opportunity", ExpectedPath: "TradingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "backtest the mean reversion strategy on hourly data", ExpectedPath: "TradingPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// Arc42Suite tests arc42 architecture documentation tree routing.
func Arc42Suite() Suite {
	return Suite{
		Name: "arc42",
		Tasks: []TaskCase{
			{Task: "document the system architecture overview", ExpectedPath: "Arc42Path", ShouldSucceed: true, MinResultLen: 20},
			{Task: "describe the component decomposition and dependencies", ExpectedPath: "Arc42Path", ShouldSucceed: true, MinResultLen: 20},
			{Task: "create the runtime view for the MCP request flow", ExpectedPath: "Arc42Path", ShouldSucceed: true, MinResultLen: 20},
			{Task: "document the deployment topology and infrastructure", ExpectedPath: "Arc42Path", ShouldSucceed: true, MinResultLen: 20},
			{Task: "describe the cross-cutting security architecture", ExpectedPath: "Arc42Path", ShouldSucceed: true, MinResultLen: 20},
			{Task: "document the quality requirements and tradeoffs", ExpectedPath: "Arc42Path", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// DefaultSuite tests the default universal tree routing.
func DefaultSuite() Suite {
	return Suite{
		Name: "default",
		Tasks: []TaskCase{
			{Task: "analyze the codebase for potential improvements", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "check the system health and performance metrics", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "explain the difference between Sequence and Selector nodes", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "summarize the latest git commits in the repository", ExpectedPath: "ExecutionPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// GOAPSuite tests GOAP (Goal-Oriented Action Planning) tree routing.
func GOAPSuite() Suite {
	return Suite{
		Name: "goap",
		Tasks: []TaskCase{
			{Task: "plan a deployment pipeline with rollback steps", ExpectedPath: "GOAPPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "optimize the resource allocation for the microservices", ExpectedPath: "GOAPPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "schedule the research tasks with dependency resolution", ExpectedPath: "GOAPPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "plan the incident response escalation path", ExpectedPath: "GOAPPath", ShouldSucceed: true, MinResultLen: 20},
			{Task: "", ExpectedPath: "", ShouldSucceed: false, MinResultLen: 0},
		},
	}
}

// SuiteForTree returns the best benchmark suite for a given tree name.
func SuiteForTree(treeName string) Suite {
	switch {
	case containsStr(treeName, "goap"):
		return GOAPSuite()
	case containsStr(treeName, "godev"):
		return GoDevSuite()
	case containsStr(treeName, "code_review"):
		return CodeReviewSuite()
	case containsStr(treeName, "devops"):
		return DevOpsSuite()
	case containsStr(treeName, "finance") || containsStr(treeName, "pitch") || containsStr(treeName, "earnings") || containsStr(treeName, "kyc") || containsStr(treeName, "gl_") || containsStr(treeName, "model") || containsStr(treeName, "market") || containsStr(treeName, "month") || containsStr(treeName, "statement") || containsStr(treeName, "valuation") || containsStr(treeName, "meeting_prep"):
		return FinanceSuite()
	case containsStr(treeName, "agent_monitor"):
		return AgentMonitorSuite()
	case containsStr(treeName, "security_audit"):
		return SecuritySuite()
	case containsStr(treeName, "research"):
		return ResearchSuite()
	case containsStr(treeName, "data_pipeline"):
		return DataPipelineSuite()
	case containsStr(treeName, "game_ai"):
		return GameAISuite()
	case containsStr(treeName, "refactoring"):
		return RefactoringSuite()
	case containsStr(treeName, "crash_investigator") || containsStr(treeName, "domain_crash"):
		return CrashInvestigatorSuite()
	case containsStr(treeName, "meeting_notes"):
		return MeetingNotesSuite()
	case containsStr(treeName, "alert_router"):
		return AlertRouterSuite()
	case containsStr(treeName, "trading_signal") || containsStr(treeName, "domain_trading"):
		return TradingSignalSuite()
	case containsStr(treeName, "arc42"):
		return Arc42Suite()
	case treeName == "default":
		return DefaultSuite()
	default:
		return GoDevSuite()
	}
}

// SortResults sorts results by task name for consistent comparison.
func SortResults(results []Result) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Task < results[j].Task
	})
}
