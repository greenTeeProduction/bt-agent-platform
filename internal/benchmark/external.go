package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// BFCLEntry mirrors a Berkeley Function Calling Leaderboard test case.
// Each entry tests whether the tree routes to the correct tool given a query and available tools.
type BFCLEntry struct {
	ID            string   `json:"id"`
	Query         string   `json:"query"`          // user's request
	Functions      []BFCLFunction `json:"functions"` // available tools
	ExpectedTool  string   `json:"expected_tool"`  // which tool should be called
	ExpectedArgs  map[string]interface{} `json:"expected_args"` // expected arguments
	Category      string   `json:"category"`       // simple, multiple, parallel, relevance
}

// BFCLFunction represents a tool definition in BFCL format.
type BFCLFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// BFCLEvalResult is the outcome of evaluating one BFCL entry against a tree.
type BFCLEvalResult struct {
	EntryID      string `json:"entry_id"`
	Query        string `json:"query"`
	ExpectedTool string `json:"expected_tool"`
	ActualPath   string `json:"actual_path"`
	Correct      bool   `json:"correct"`
	Success      bool   `json:"success"`
}

// BFCLSuite loads and evaluates BFCL-style tool routing benchmarks.
type BFCLSuite struct {
	Name    string
	Entries []BFCLEntry
}

// LoadBFCLSuite loads a BFCL benchmark suite from a JSON file.
func LoadBFCLSuite(path string) (*BFCLSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []BFCLEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return &BFCLSuite{Name: filepath.Base(path), Entries: entries}, nil
}

// Evaluate runs all BFCL entries against a tree and returns routing accuracy.
func (s *BFCLSuite) Evaluate(tree *evolution.SerializableNode, mock llm.LLM) *BFCLMetrics {
	var results []BFCLEvalResult
	correct := 0
	successes := 0

	for _, entry := range s.Entries {
		bb := &engine.Blackboard{
			Task: entry.Query,
			LLM:  mock,
		}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		path := detectPath(output, bb)
		correctTool := strings.EqualFold(path, entry.ExpectedTool) ||
			strings.Contains(strings.ToLower(path), strings.ToLower(entry.ExpectedTool))

		if correctTool {
			correct++
		}
		if bb.Outcome == "success" {
			successes++
		}

		results = append(results, BFCLEvalResult{
			EntryID:      entry.ID,
			Query:        entry.Query,
			ExpectedTool: entry.ExpectedTool,
			ActualPath:   path,
			Correct:      correctTool,
			Success:      bb.Outcome == "success",
		})
	}

	n := len(results)
	accuracy := 0.0
	if n > 0 {
		accuracy = float64(correct) / float64(n)
	}

	return &BFCLMetrics{
		SuiteName:     s.Name,
		TotalEntries:  n,
		CorrectRoutes: correct,
		Accuracy:      accuracy,
		SuccessRate:   float64(successes) / float64(max1(n)),
		Results:       results,
	}
}

// BFCLMetrics holds aggregate BFCL evaluation results.
type BFCLMetrics struct {
	SuiteName     string           `json:"suite_name"`
	TotalEntries  int              `json:"total_entries"`
	CorrectRoutes int              `json:"correct_routes"`
	Accuracy      float64          `json:"accuracy"`
	SuccessRate   float64          `json:"success_rate"`
	Results       []BFCLEvalResult `json:"results"`
}

// BuiltinBFCLSimple returns a representative BFCL Simple suite (single function calls).
// These are curated from the BFCL V1 simple category patterns.
func BuiltinBFCLSimple() *BFCLSuite {
	entries := []BFCLEntry{
		{ID: "simple-001", Query: "Get the current weather in San Francisco", ExpectedTool: "FetchCompsData", Category: "simple"},
		{ID: "simple-002", Query: "build and compile the Go project", ExpectedTool: "BuildPath", Category: "simple"},
		{ID: "simple-003", Query: "find bugs in this code", ExpectedTool: "BugDetection", Category: "simple"},
		{ID: "simple-004", Query: "run the test suite with coverage", ExpectedTool: "TestPath", Category: "simple"},
		{ID: "simple-005", Query: "build a DCF model with 3 scenarios", ExpectedTool: "DCFPath", Category: "simple"},
		{ID: "simple-006", Query: "review Q3 earnings results for Apple", ExpectedTool: "EarningsIngestPath", Category: "simple"},
		{ID: "simple-007", Query: "run KYC screening for new client", ExpectedTool: "KYCPath", Category: "simple"},
		{ID: "simple-008", Query: "explain how Go interfaces work", ExpectedTool: "GoKnowledgePath", Category: "simple"},
		{ID: "simple-009", Query: "patrol the area for threats", ExpectedTool: "PatrolPath", Category: "simple"},
		{ID: "simple-010", Query: "reconcile the general ledger entries", ExpectedTool: "ReconPath", Category: "simple"},
		{ID: "simple-011", Query: "check health of all running agents", ExpectedTool: "HealthCheckPath", Category: "simple"},
		{ID: "simple-012", Query: "scan for security vulnerabilities in this code", ExpectedTool: "SecurityReview", Category: "simple"},
		{ID: "simple-013", Query: "deploy the application to staging", ExpectedTool: "DeployPath", Category: "simple"},
		{ID: "simple-014", Query: "research how AI agents compare to traditional software", ExpectedTool: "SynthesisPhase", Category: "simple"},
		{ID: "simple-015", Query: "parse this crash stack trace for root cause", ExpectedTool: "ParseStackTrace", Category: "simple"},
	}
	return &BFCLSuite{Name: "bfcl_simple", Entries: entries}
}

// BuiltinBFCLRelevance returns a BFCL Relevance suite (no relevant tools).
func BuiltinBFCLRelevance() *BFCLSuite {
	entries := []BFCLEntry{
		{ID: "rel-001", Query: "what is the meaning of life?", ExpectedTool: "ExecutionPath", Category: "relevance"},
		{ID: "rel-002", Query: "tell me a joke about programmers", ExpectedTool: "ExecutionPath", Category: "relevance"},
		{ID: "rel-003", Query: "write a haiku about autumn", ExpectedTool: "ExecutionPath", Category: "relevance"},
		{ID: "rel-004", Query: "what is the capital of Mongolia?", ExpectedTool: "ExecutionPath", Category: "relevance"},
		{ID: "rel-005", Query: "convert 42 Celsius to Fahrenheit", ExpectedTool: "ExecutionPath", Category: "relevance"},
	}
	return &BFCLSuite{Name: "bfcl_relevance", Entries: entries}
}

// BuiltinBFCLMultiple returns a BFCL Multiple suite (pick from 2-4 tools).
func BuiltinBFCLMultiple() *BFCLSuite {
	entries := []BFCLEntry{
		{ID: "multi-001", Query: "review this Go code and also check for security issues", ExpectedTool: "CodeReviewPath", Category: "multiple"},
		{ID: "multi-002", Query: "build the project then deploy it", ExpectedTool: "BuildPath", Category: "multiple"},
		{ID: "multi-003", Query: "run tests and report coverage", ExpectedTool: "TestPath", Category: "multiple"},
		{ID: "multi-004", Query: "extract key metrics from earnings then update the model", ExpectedTool: "EarningsIngestPath", Category: "multiple"},
		{ID: "multi-005", Query: "detect enemies then attack them", ExpectedTool: "CombatPath", Category: "multiple"},
	}
	return &BFCLSuite{Name: "bfcl_multiple", Entries: entries}
}

// --- GAIA Benchmark Integration ---

// GAIAEntry mirrors a GAIA benchmark question.
type GAIAEntry struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Level    int    `json:"level"`    // 1, 2, or 3
	Answer   string `json:"answer"`   // ground truth
}

// GAIAMetrics holds GAIA evaluation results.
type GAIAMetrics struct {
	TotalQuestions int     `json:"total_questions"`
	CorrectAnswers int     `json:"correct_answers"`
	Accuracy       float64 `json:"accuracy"`
	ByLevel        map[int]GAIALevelMetrics `json:"by_level"`
}

// GAIALevelMetrics holds per-level GAIA results.
type GAIALevelMetrics struct {
	Total   int     `json:"total"`
	Correct int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

// BuiltinGAIADev returns a GAIA-style dev set with representative questions.
func BuiltinGAIADev() []GAIAEntry {
	return []GAIAEntry{
		{ID: "gaia-001", Question: "What was the revenue of Apple Inc. in Q3 2024 according to their earnings report?", Level: 1, Answer: "$85.8 billion"},
		{ID: "gaia-002", Question: "How many SpaceX Falcon 9 launches occurred in 2024?", Level: 1, Answer: "134"},
		{ID: "gaia-003", Question: "What is the chemical formula for lithium iron phosphate?", Level: 1, Answer: "LiFePO4"},
		{ID: "gaia-004", Question: "Compare the GDP growth rates of India and China in 2024 and explain the difference.", Level: 2, Answer: "India ~7.0%, China ~4.8%"},
		{ID: "gaia-005", Question: "What are the key differences between Kubernetes and Docker Swarm for container orchestration?", Level: 2, Answer: "K8s: complex, feature-rich. Swarm: simpler, Docker-native"},
		{ID: "gaia-006", Question: "Research the environmental impact of lithium mining and compare it to cobalt mining.", Level: 2, Answer: "Lithium: water usage, brine. Cobalt: child labor, tailings"},
		{ID: "gaia-007", Question: "Analyze the competitive landscape of the AI chip market in 2024-2025, including NVIDIA, AMD, and Intel.", Level: 3, Answer: "NVIDIA dominates 80%+, AMD growing, Intel lagging"},
		{ID: "gaia-008", Question: "How would a 2°C global temperature rise affect agricultural yields in Southeast Asia by 2050?", Level: 3, Answer: "Rice -15%, palm oil -30%, adaption needed"},
	}
}

// EvaluateGAIA runs GAIA entries through the deep research tree and compares to ground truth.
func EvaluateGAIA(tree *evolution.SerializableNode, entries []GAIAEntry, mock llm.LLM) *GAIAMetrics {
	byLevel := map[int]GAIALevelMetrics{}
	correct := 0

	for _, entry := range entries {
		bb := &engine.Blackboard{Task: entry.Question, LLM: mock}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		// Simple answer matching: check if output contains the answer
		isCorrect := strings.Contains(strings.ToLower(output), strings.ToLower(entry.Answer))
		if isCorrect {
			correct++
		}

		lm := byLevel[entry.Level]
		lm.Total++
		if isCorrect {
			lm.Correct++
		}
		lm.Accuracy = float64(lm.Correct) / float64(lm.Total)
		byLevel[entry.Level] = lm
	}

	n := len(entries)
	return &GAIAMetrics{
		TotalQuestions: n,
		CorrectAnswers: correct,
		Accuracy:       float64(correct) / float64(max1(n)),
		ByLevel:        byLevel,
	}
}

// --- SWE-bench Integration ---

// SWEEntry mirrors a SWE-bench task.
type SWEEntry struct {
	ID          string `json:"id"`
	Repo        string `json:"repo"`
	IssueTitle  string `json:"issue_title"`
	IssueBody   string `json:"issue_body"`
	BaseCommit  string `json:"base_commit"`
	TestPatch   string `json:"test_patch"` // tests that must pass
}

// SWEMetrics holds SWE-bench evaluation results.
type SWEMetrics struct {
	TotalTasks  int     `json:"total_tasks"`
	Resolved    int     `json:"resolved"`
	ResolveRate float64 `json:"resolve_rate"`
}

// BuiltinSWELite returns a SWE-bench Lite style suite for Go projects.
func BuiltinSWELite() []SWEEntry {
	return []SWEEntry{
		{
			ID: "swe-go-001", Repo: "golang/go",
			IssueTitle: "http: add missing error handling in request parsing",
			IssueBody:  "The request parser does not handle malformed Content-Type headers. Add proper error handling.",
		},
		{
			ID: "swe-go-002", Repo: "gin-gonic/gin",
			IssueTitle: "fix: memory leak in middleware chain when handler panics",
			IssueBody:  "When a middleware handler panics, the context is not properly cleaned up causing a memory leak.",
		},
		{
			ID: "swe-go-003", Repo: "go-bt-evolve",
			IssueTitle: "engine: ValidateInput condition should check minimum length",
			IssueBody:  "Currently ValidateInput only checks non-empty. Add minimum length check of 3 characters.",
		},
		{
			ID: "swe-go-004", Repo: "go-bt-evolve",
			IssueTitle: "gardener: infinite loop when all benchmark scores return 0",
			IssueBody:  "The gardener's evolveTree function may loop indefinitely when all mutations score 0.",
		},
		{
			ID: "swe-go-005", Repo: "go-bt-evolve",
			IssueTitle: "mcp: handleMessage should handle null JSON-RPC id",
			IssueBody:  "When a JSON-RPC notification has a null id, the server should not send a response.",
		},
	}
}

func max1(n int) int {
	if n < 1 { return 1 }
	return n
}
