package benchmark

import (
	"log"
	"math"
	"sort"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// TaskCase is a single benchmark task with expected routing.
type TaskCase struct {
	Task           string   `json:"task"`
	ExpectedPath   string   `json:"expected_path"`   // which strategy path should handle this
	PossiblePaths  []string `json:"possible_paths,omitempty"` // multiple acceptable paths for ambiguous tasks
	MinResultLen   int      `json:"min_result_len"`  // minimum output length expected
	ShouldSucceed  bool     `json:"should_succeed"`   // expected outcome
	ShouldReject   bool     `json:"should_reject"`    // PreGate should reject this
	MinQualityScore float64 `json:"min_quality_score,omitempty"` // minimum quality score expected
	Difficulty     string   `json:"difficulty,omitempty"` // easy | medium | hard | adversarial
}

// Suite is a collection of benchmark tasks for a specific domain.
type Suite struct {
	Name  string     `json:"name"`
	Tasks []TaskCase `json:"tasks"`
}

// Result is the outcome of running a single task through a tree.
type Result struct {
	Task       string  `json:"task"`
	Outcome    string  `json:"outcome"`
	DurationMs int64   `json:"duration_ms"`
	ResultLen  int     `json:"result_len"`
	Path       string  `json:"path"`        // which strategy path was taken
	Success    bool    `json:"success"`
}

// RunMetrics aggregates results from running a full suite.
type RunMetrics struct {
	TotalTasks    int     `json:"total_tasks"`
	Successes     int     `json:"successes"`
	Failures      int     `json:"failures"`
	SuccessRate   float64 `json:"success_rate"`
	AvgDurationMs float64 `json:"avg_duration_ms"`
	AvgResultLen  float64 `json:"avg_result_len"`
	PathCoverage  float64 `json:"path_coverage"` // unique paths / total tasks
	Results       []Result `json:"results"`
}

// RunSuite executes all tasks in a suite against a tree.
func RunSuite(tree *evolution.SerializableNode, suite Suite, mock llm.LLM) *RunMetrics {
	var results []Result
	successes := 0
	paths := make(map[string]int)

	for _, tc := range suite.Tasks {
		start := time.Now()

		bb := &engine.Blackboard{
			Task:    tc.Task,
			LLM:     mock,
		}

		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)
		duration := time.Since(start).Milliseconds()

		success := bb.Outcome == "success"
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

// ABTest compares tree performance before and after a mutation.
type ABTest struct {
	Before    *RunMetrics `json:"before"`
	After     *RunMetrics `json:"after"`
	Delta     ABDelta     `json:"delta"`
	Improved  bool        `json:"improved"`
}

// ABDelta is the difference between before and after.
type ABDelta struct {
	SuccessRate   float64 `json:"success_rate_delta"`
	AvgDurationMs float64 `json:"avg_duration_delta"`
	AvgResultLen  float64 `json:"avg_result_len_delta"`
	PathCoverage  float64 `json:"path_coverage_delta"`
	EffectSize    float64 `json:"effect_size"`    // Cohen's d on success rate
	Significant   bool    `json:"significant"`    // p < 0.05
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

	improved := delta.SuccessRate > 0 || (delta.SuccessRate == 0 && delta.AvgDurationMs < 0)

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
			suite.Tasks[0],                      // happy-path routing
			suite.Tasks[len(suite.Tasks)-1],     // edge-case task
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

func fishersExact(s1, f1, s2, f2 int) float64 {
	// Chi-squared approximation for 2x2 contingency table
	n1 := s1 + f1
	n2 := s2 + f2
	N := n1 + n2
	if N == 0 {
		return 1.0
	}

	// Expected values
	e11 := float64((s1+s2)*n1) / float64(N)
	e12 := float64((f1+f2)*n1) / float64(N)
	e21 := float64((s1+s2)*n2) / float64(N)
	e22 := float64((f1+f2)*n2) / float64(N)

	// Chi-squared
	chi2 := 0.0
	if e11 > 0 { chi2 += math.Pow(float64(s1)-e11, 2) / e11 }
	if e12 > 0 { chi2 += math.Pow(float64(f1)-e12, 2) / e12 }
	if e21 > 0 { chi2 += math.Pow(float64(s2)-e21, 2) / e21 }
	if e22 > 0 { chi2 += math.Pow(float64(f2)-e22, 2) / e22 }

	// Approximate p-value from chi-squared distribution (1 df)
	// Using Wilson-Hilferty approximation
	if chi2 <= 0 {
		return 1.0
	}
	p := 1.0 - chi2CDF(chi2, 1)
	return p
}

func chi2CDF(x float64, df int) float64 {
	// Simple Wilson-Hilferty approximation
	if x <= 0 {
		return 0
	}
	z := math.Pow(x/float64(df), 1.0/3.0) - (1 - 2.0/(9.0*float64(df)))
	z = z / math.Sqrt(2.0/(9.0*float64(df)))
	// Normal CDF approximation
	return 0.5 * (1 + math.Erf(z/math.Sqrt2))
}

// --- Path detection ---

func detectPath(result string, bb *engine.Blackboard) string {
	// Heuristic: check result content for domain-specific markers
	markers := map[string]string{
		"Code Review":         "CodeReviewPath",
		"Bug Scan":            "BugDetection",
		"Security Scan":       "SecurityReview",
		"Style Check":         "StyleReview",
		"Compilation":         "BuildPath",
		"Test Results":        "TestPath",
		"Lint Output":         "LintPath",
		"Deploy":              "DeployPath",
		"Go Explanation":      "GoKnowledgePath",
		"Research Report":     "SynthesisPhase",
		"DCF Model":           "DCFPath",
		"LBO Model":           "LBOPath",
		"Pitch Deck":          "DeckAssemblyPath",
		"KYC":                 "KYCPath",
		"GL Reconciliation":   "ReconPath",
		"Patrol":              "PatrolPath",
		"Combat":              "CombatPath",
		"Market Data":         "DataCollectionPath",
		"Stack Trace":         "ParseStackTrace",
		"Meeting":             "TranscribePath",
		"Threat Model":        "ThreatModeling",
		"SAST":                "SASTPath",
		"ETL":                 "ExtractPath",
	}
	for marker, path := range markers {
		if containsStr(result, marker) {
			return path
		}
	}
	// Fallback: check blackboard state
	if bb.KgResults != "" {
		return "KnowledgePath"
	}
	if bb.CachedResult != "" {
		return "CachePath"
	}
	if bb.Plan != "" {
		return "ExecutionPath"
	}
	return "UnknownPath"
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && indexOfStr(s, substr) >= 0
}

func indexOfStr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func minF(a, b float64) float64 {
	if a < b { return a }
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

func (m *MockLLM) AnalyzeComplexity(task string) string { return m.Complexity }
func (m *MockLLM) GeneratePlan(task, complexity string) string { return m.Plan }
func (m *MockLLM) Reflect(task, outcome, plan string) (string, string) { return m.WentWell, m.ToImprove }
func (m *MockLLM) Generate(prompt string) (string, error) { return m.Plan, nil }

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

// SuiteForTree returns the best benchmark suite for a given tree name.
func SuiteForTree(treeName string) Suite {
	switch {
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
