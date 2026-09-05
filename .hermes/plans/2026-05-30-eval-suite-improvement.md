# BT Platform Evaluation Suite — Improvement Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.
> **Date:** 2026-05-30
> **Commit base:** 57166f0c

**Goal:** Transform the BT platform's evaluation suite from a mock-driven, keyword-matched stub system into a rigorous, scientifically valid benchmark framework with real LLM evaluation, proper tool execution verification, and full benchmark dataset integration.

**Architecture:** Three-tier refactor. Tier 1 fixes the core evaluation validity bugs (path detection, mock LLM, statistical tests). Tier 2 replaces hand-curated stubs with real benchmark datasets (BFCL, SWE-bench, ToolBench, τ-bench, GAIA). Tier 3 adds tool execution verification and continuous benchmarking.

**Tech Stack:** Go 1.21+, btcore behavior tree library, DeepSeek v4 API / Ollama qwen3.6:35b, standard Go testing

---

## 🔬 Scientific Review Findings

Three FATAL flaws render current results scientifically invalid:

1. **`path_detect.go` measures task keywords, not tree behavior.** The `result` parameter (tree output) is accepted but never used. Only `bb.Task` is keyword-matched. A crashing tree and a perfect tree get identical "routing accuracy" scores.

2. **`DefaultMock()` returns identical outputs for all tasks.** `AnalyzeTask` → "medium", `ExecutePlan` → "1. Analyze input\n2. Execute...", always. Quality gates pass because output >20 chars. Zero LLM reasoning is tested.

3. **SWE-bench "evaluation" resolves by checking `len(output) > 50`.** No git operations, no test suite execution, no patch validation. Python repo issues evaluated by a Go behavior tree.

Additional gaps: 6 benchmark integrations cover only 0.3-5% of real datasets. Zero tools are actually executed (string-matching on tool names). AB test statistics are chi-squared approximations called "Fisher's exact" on N=5-8 samples.

---

## Implementation Plan

### Tier 1 — Core Validity Fixes (critical, no new data)

#### Task 1: Replace `detectPath()` with tree-internal path tracking

**Objective:** Make routing accuracy reflect what the tree *did*, not task keywords.

**Files:**
- Modify: `internal/benchmark/path_detect.go`
- Modify: `internal/engine/tree.go` (add path tracking to Blackboard)
- Modify: `internal/benchmark/benchmark.go` (update callers)

**Implementation:**

1. Add `CurrentPath string` and `VisitedPaths []string` fields to Blackboard:

```go
// In internal/engine/tree.go, Blackboard struct:
type Blackboard struct {
    // ... existing fields ...
    CurrentPath  string   // The strategy path currently being executed
    VisitedPaths []string // All paths visited during execution
}
```

2. In `buildNode()`, when entering a named Sequence under StrategyRouter, set `bb.CurrentPath`:

```go
// In BuildTree's recursive buildNode, when Type == "Sequence" inside a Selector named "StrategyRouter":
if parentName == "StrategyRouter" && node.Type == "Sequence" {
    bb.CurrentPath = node.Name
    bb.VisitedPaths = append(bb.VisitedPaths, node.Name)
}
```

3. Rewrite `detectPath()` to use `bb.CurrentPath`:

```go
func detectPath(result string, bb *engine.Blackboard) string {
    if bb.CurrentPath != "" {
        return bb.CurrentPath
    }
    // Fallback: first visited path
    if len(bb.VisitedPaths) > 0 {
        return bb.VisitedPaths[0]
    }
    return "UnknownPath"
}
```

4. Remove all keyword-matching logic from `detectPath()`.

5. Update test expectations in `benchmark_test.go` — tests that expected specific path strings from keyword matching now need to set `bb.CurrentPath` before calling `detectPath()`.

**Verification:**
```bash
cd /home/nico/go-bt-evolve
GOMODCACHE=/mnt/ssd/home-dirs/go-cache PATH="/usr/local/go/bin:$PATH" \
  go test ./internal/benchmark/ -run TestRouting -v -count=1
# Expected: tests still pass (or update assertions)
go test ./internal/engine/ -run TestPathTracking -v -count=1
# Expected: new test verifies CurrentPath is set during tree execution
```

---

#### Task 2: Add real-LLM benchmark mode alongside mock

**Objective:** Enable benchmarks that actually exercise LLM reasoning, not just tree structure.

**Files:**
- Modify: `internal/benchmark/benchmark.go`
- Modify: `internal/benchmark/external.go`  
- Create: `internal/benchmark/llm_eval.go`

**Implementation:**

1. Add `LLMMode` flag to `Suite`:

```go
type Suite struct {
    // ... existing fields ...
    LLMMode bool // true = use real LLM, false = use mock
}
```

2. Add `RunSuiteWithLLM()` function that uses `DefaultLLM()` instead of `DefaultMock()`:

```go
func RunSuiteWithLLM(suite *Suite, tree *evolution.SerializableNode) *SuiteResult {
    llmClient := DefaultLLM() // tries Ollama, falls back to mock
    results := make([]Result, len(suite.Tasks))
    for i, task := range suite.Tasks {
        bb := engine.NewBlackboard(task.Input)
        bb.LLM = llmClient
        bt := engine.BuildTree(tree, bb)
        engine.RunTask(bb, bt)
        results[i] = Result{
            Task:    task.Input,
            Outcome: bb.Outcome,
            Output:  bb.Result,
            Path:    detectPath(bb.Result, bb),
            Duration: bb.DurationMs,
        }
    }
    return scoreSuite(suite, results)
}
```

3. Add A/B comparison between mock and real LLM:

```go
type LLMDivergenceReport struct {
    Suite          string
    MockSuccessRate float64
    RealSuccessRate float64
    Divergence      float64 // absolute difference
    TasksDiverged   []string // tasks where mock≠real outcome
}
```

4. Verify with one quick suite:

```bash
GOMODCACHE=/mnt/ssd/home-dirs/go-cache PATH="/usr/local/go/bin:$PATH" \
  go test ./internal/benchmark/ -run TestLLMMode -v -count=1 -timeout 300s
# Expected: both mock and real LLM produce results
# Real LLM may differ from mock — that's the point of the comparison
```

---

#### Task 3: Fix statistical tests

**Objective:** Replace broken Fisher's exact implementation and add validity checks.

**Files:**
- Modify: `internal/benchmark/benchmark.go`

**Implementation:**

1. Replace `fishersExact()` with actual Fisher's exact test (use `gonum.org/v1/gonum/stat` if available, or implement correct 2×2):

```go
import "gonum.org/v1/gonum/stat/combin"

func fishersExact(a, b, c, d int) float64 {
    // Actual Fisher's exact test for 2×2 contingency table
    // [[a, b], [c, d]]
    // Returns two-tailed p-value
    n := a + b + c + d
    // Hypergeometric probability for observed table
    pObs := float64(combin.Binomial(b, a)) * float64(combin.Binomial(d, c)) / float64(combin.Binomial(n, a+c))
    
    pValue := pObs
    // Sum probabilities of tables more extreme
    minAD := a
    if d < minAD { minAD = d }
    for i := 0; i <= minAD; i++ {
        if i == a { continue }
        p := float64(combin.Binomial(b, i)) * float64(combin.Binomial(d, a+c-i)) / float64(combin.Binomial(n, a+c))
        if p <= pObs {
            pValue += p
        }
    }
    if pValue > 1.0 { pValue = 1.0 }
    return pValue
}
```

2. Add sample-size warning. If total tasks < 20, annotate result:

```go
if suite.TotalTasks < 20 {
    result.Warnings = append(result.Warnings, 
        fmt.Sprintf("Small sample (n=%d): p-values are suggestive, not conclusive", suite.TotalTasks))
}
```

3. Add bootstrap confidence intervals for success rate:

```go
func bootstrapCI(successes, total int, iterations int) (lower, upper float64) {
    rate := float64(successes) / float64(total)
    samples := make([]float64, iterations)
    for i := 0; i < iterations; i++ {
        bootSuccesses := 0
        for j := 0; j < total; j++ {
            if rand.Float64() < rate {
                bootSuccesses++
            }
        }
        samples[i] = float64(bootSuccesses) / float64(total)
    }
    sort.Float64s(samples)
    lower = samples[iterations*5/100]
    upper = samples[iterations*95/100]
    return
}
```

**Verification:**
```bash
go test ./internal/benchmark/ -run TestStatistics -v -count=1
# Expected: Fisher's exact returns p-values in [0,1]
# Bootstrap CI contains the point estimate
# Small-sample warning appears for n<20
```

---

### Tier 2 — Real Dataset Integration (high value)

#### Task 4: Load real BFCL V1/V3 JSON datasets

**Objective:** Replace 25/8 hand-curated BFCL entries with real benchmark data.

**Files:**
- Modify: `internal/benchmark/bfcl_v3.go`
- Modify: `internal/benchmark/external.go`
- Create: `internal/benchmark/bfcl_loader.go`

**Implementation:**

1. Create BFCL dataset loader that parses standard BFCL JSON format:

```go
type BFCLDataset struct {
    Entries    []BFCLEntry `json:"entries"`
    Functions  []BFCLFunction `json:"functions"`
    Categories []string    `json:"categories"`
}

func LoadBFCLDataset(path string) (*BFCLDataset, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read BFCL dataset: %w", err)
    }
    var dataset BFCLDataset
    if err := json.Unmarshal(data, &dataset); err != nil {
        return nil, fmt.Errorf("parse BFCL dataset: %w", err)
    }
    return &dataset, nil
}

// Download BFCL dataset from HuggingFace
func DownloadBFCL(version string, destDir string) error {
    // HF repo: gorilla-llm/Berkeley-Function-Calling-Leaderboard
    url := fmt.Sprintf("https://huggingface.co/datasets/gorilla-llm/Berkeley-Function-Calling-Leaderboard/resolve/main/%s", version)
    // Download and extract
    return downloadAndExtract(url, destDir)
}
```

2. Update BuiltinBFCLV3() to load from disk with fallback to builtins:

```go
func BuiltinBFCLV3() []BFCLV3Entry {
    // Try loading real dataset first
    if entries, err := LoadBFCLV3JSON("benchmark_data/bfcl_v3.json"); err == nil {
        return entries
    }
    // Fallback to minimal builtins for CI/testing
    return minimalBFCLV3Builtins()
}
```

3. BFCL scoring now maps expected tools from actual BFCL function names, not internal tree paths:

```go
// Before (broken): ExpectedTools: []string{"BugDetection", "TestPath"}
// After (correct): ExpectedTools: ["get_weather", "search_flights"]
```

**Verification:**
```bash
# Download test: requires ~50MB, network
go test ./internal/benchmark/ -run TestBFCLDownload -v -count=1 -timeout 600s
# Expected: loads 1000+ entries from HF dataset

# Run benchmark:
go test ./internal/benchmark/ -run TestBFCLV3Full -v -count=1 -timeout 3600s
# Expected: 500+ entries evaluated, detailed metrics
```

---

#### Task 5: Real SWE-bench verification infrastructure

**Objective:** Implement actual SWE-bench evaluation: git clone → apply patch → run tests.

**Files:**
- Modify: `internal/benchmark/swebench_verified.go`
- Create: `internal/benchmark/swebench_runner.go`

**Implementation:**

1. Create SWE-bench runner that operates on Go repositories:

```go
type SWEBenchRunner struct {
    WorkDir   string // temp directory for repo clones
    Timeout   time.Duration
}

func (r *SWEBenchRunner) Evaluate(entry SWEVerifiedEntry) SWEVerifiedResult {
    // 1. Clone repo at base commit
    repoDir := filepath.Join(r.WorkDir, entry.Repo)
    gitClone(entry.RepoURL, repoDir)
    gitCheckout(repoDir, entry.BaseCommit)
    
    // 2. Record failing tests before fix
    failingBefore := runTestSuite(repoDir, entry.TestCommand)
    
    // 3. Apply the agent's patch
    patchFile := filepath.Join(r.WorkDir, "fix.patch")
    os.WriteFile(patchFile, []byte(entry.Patch), 0644)
    gitApply(repoDir, patchFile)
    
    // 4. Run tests again
    failingAfter := runTestSuite(repoDir, entry.TestCommand)
    
    // 5. Compute resolved count
    resolved := setDifference(failingBefore, failingAfter)
    
    return SWEVerifiedResult{
        InstanceID:     entry.InstanceID,
        Resolved:       len(resolved) > 0 || (len(failingBefore) > 0 && len(failingAfter) == 0),
        TestsBefore:    failingBefore,
        TestsAfter:     failingAfter,
        FailingBefore:  len(failingBefore),
        FailingAfter:   len(failingAfter),
        ResolvedTests:  resolved,
    }
}
```

2. Replace Python datasets with Go-specific SWE-bench instances (or use Go projects from SWE-bench Multilingual):

3. If real SWE-bench is too heavy for Jetson, create a `GoSWEBenchLite` with 10 curated Go repo bugs that can clone and test quickly:

```go
func GoSWEBenchLite() []SWEVerifiedEntry {
    return []SWEVerifiedEntry{
        {
            InstanceID: "go-bt-evolve-001",
            Repo: "go-bt-evolve",
            RepoURL: "file:///home/nico/go-bt-evolve",
            BaseCommit: "57166f0c",
            Issue: "nil pointer dereference in RunTask when tree is nil",
            TestCommand: "go test ./internal/engine/ -run TestNilTree -v",
        },
        // ... 9 more instances
    }
}
```

**Verification:**
```bash
go test ./internal/benchmark/ -run TestSWEBenchRunner -v -count=1 -timeout 300s
# Expected: clones repos, applies patches, runs tests, reports resolved count
```

---

#### Task 6: Load full ToolBench + τ-bench datasets

**Objective:** Scale from 15 ToolBench entries and 10 τ-bench entries to hundreds.

**Files:**
- Modify: `internal/benchmark/toolbench.go`
- Modify: `internal/benchmark/taubench.go`

**Implementation:**

1. ToolBench: Add `LoadToolBenchJSON(path string)` that parses the standard ToolBench JSON test format with 2,500+ test cases across 8 categories (I1-I3 instruction, C1-C3 category, D1-D2 difficulty).

2. τ-bench: Extend `LoadTauBenchTasks()` to load all 200+ scenarios from the τ-bench repo. The infrastructure already exists — just loop over all task files:

```go
func LoadAllTauBenchTasks(repoPath string) ([]TauBenchEntry, error) {
    domains := []string{"airline", "retail"}
    var allEntries []TauBenchEntry
    for _, domain := range domains {
        taskDir := filepath.Join(repoPath, "data", "tasks", domain)
        entries, err := filepath.Glob(filepath.Join(taskDir, "*.json"))
        if err != nil { return nil, err }
        for _, entryPath := range entries {
            entry, err := LoadTauBenchTask(entryPath, domain)
            if err != nil { continue }
            allEntries = append(allEntries, entry)
        }
    }
    return allEntries, nil
}
```

3. Add `DownloadTauBench(repoURL string)` to clone the τ-bench repository if not present.

**Verification:**
```bash
go test ./internal/benchmark/ -run TestToolBenchFull -v -count=1 -timeout 3600s
# Expected: 2000+ ToolBench entries, coverage across all categories

go test ./internal/benchmark/ -run TestTauBenchFull -v -count=1 -timeout 3600s
# Expected: 200+ τ-bench scenarios, airline + retail domains
```

---

### Tier 3 — Tool Execution Verification (medium)

#### Task 7: Mock tool servers for τ-bench and ToolBench

**Objective:** Replace string-matching "did the agent say the right tool name?" with actual tool call verification.

**Files:**
- Create: `internal/benchmark/mock_tools.go`
- Modify: `internal/benchmark/taubench.go`
- Modify: `internal/benchmark/toolbench.go`

**Implementation:**

1. Create mock tool servers that the agent's chain can actually call:

```go
type MockToolServer struct {
    tools map[string]MockToolHandler
    calls []ToolCall  // recorded calls for verification
}

type MockToolHandler func(params map[string]any) (any, error)
type ToolCall struct {
    ToolName string
    Params   map[string]any
    Result   any
    Timestamp time.Time
}

func NewAirlinesMockServer() *MockToolServer {
    s := &MockToolServer{tools: make(map[string]MockToolHandler)}
    s.tools["book_reservation"] = func(params map[string]any) (any, error) {
        return map[string]any{
            "reservation_id": "ABC123",
            "status": "confirmed",
            "price": params["num_passengers"].(float64) * 350.0,
        }, nil
    }
    s.tools["lookup_reservation"] = func(params map[string]any) (any, error) {
        return map[string]any{
            "reservation_id": params["reservation_id"],
            "status": "confirmed",
            "flight": "UA123",
        }, nil
    }
    // ... all 14 airline tools
    return s
}
```

2. Verify tool calls by comparing actual `s.calls` against expected actions:

```go
func (e *TauBenchEntry) VerifyToolCalls(actual []ToolCall) TauBenchResult {
    matched := 0
    for _, expected := range e.Actions {
        for _, actualCall := range actual {
            if actualCall.ToolName == expected.Tool {
                // Check key parameters match
                if paramsMatch(actualCall.Params, expected.Params) {
                    matched++
                    break
                }
            }
        }
    }
    return TauBenchResult{
        GoalAchieved:    matched == len(e.Actions),
        ToolCallAccuracy: float64(matched) / float64(len(e.Actions)),
        TotalCalls:      len(actual),
    }
}
```

3. Mount mock servers into the tree's ChainAction toolset so the agent can actually call them.

**Verification:**
```bash
go test ./internal/benchmark/ -run TestMockToolServer -v -count=1
# Expected: tools respond correctly, calls are recorded
go test ./internal/benchmark/ -run TestTauBenchWithMockTools -v -count=1 -timeout 300s
# Expected: agent calls tools, mock server responds, verification passes
```

---

#### Task 8: LLM-as-judge for GAIA and semantic evaluation

**Objective:** Replace substring matching with proper semantic evaluation.

**Files:**
- Modify: `internal/benchmark/external.go`
- Create: `internal/benchmark/llm_judge.go`

**Implementation:**

1. Add LLM-as-judge evaluator:

```go
type LLMJudge struct {
    llm llm.LLM
}

func (j *LLMJudge) EvaluateAnswer(expected, actual string) (bool, float64, string) {
    prompt := fmt.Sprintf(`You are evaluating an AI agent's answer against an expected answer.
    
Expected answer: %s
Agent's answer: %s

Is the agent's answer semantically equivalent to the expected answer?
Consider: factual correctness, completeness, no contradictions.

Respond in JSON: {"match": true/false, "confidence": 0.0-1.0, "explanation": "..."}`, expected, actual)
    
    response, err := j.llm.Generate(prompt)
    if err != nil {
        // Fallback to substring matching
        return strings.Contains(strings.ToLower(actual), strings.ToLower(expected)), 0.5, "fallback"
    }
    
    var result struct {
        Match       bool    `json:"match"`
        Confidence  float64 `json:"confidence"`
        Explanation string  `json:"explanation"`
    }
    json.Unmarshal([]byte(response), &result)
    return result.Match, result.Confidence, result.Explanation
}
```

2. Update GAIA evaluation to use LLM judge for non-exact answers:

```go
func evaluateGAIATask(entry GAIAEntry, output string) bool {
    if entry.ExactMatch {
        return strings.Contains(strings.ToLower(output), strings.ToLower(entry.Answer))
    }
    judge := &LLMJudge{llm: DefaultLLM()}
    match, confidence, _ := judge.EvaluateAnswer(entry.Answer, output)
    return match && confidence > 0.7
}
```

**Verification:**
```bash
go test ./internal/benchmark/ -run TestLLMJudge -v -count=1 -timeout 120s
# Expected: LLM judge correctly identifies semantic matches
# "The capital of France is Paris" matches "Paris" 
# "The capital of France is London" does not match "Paris"
```

---

### Tier 4 — Continuous Benchmarking (lower priority)

#### Task 9: Historical score tracking

**Objective:** Track benchmark scores across git history to detect regressions.

**Files:**
- Create: `internal/benchmark/history.go`
- Modify: `internal/eval/eval.go`

**Implementation:**

```go
type BenchmarkHistory struct {
    Commits []BenchmarkSnapshot
}

type BenchmarkSnapshot struct {
    CommitSHA string
    Timestamp time.Time
    Suites    map[string]SuiteResult
    Scorecard PlatformScorecard
}

func RecordBenchmark(sha string, scorecard PlatformScorecard) error {
    history := loadHistory()
    history.Commits = append(history.Commits, BenchmarkSnapshot{
        CommitSHA: sha,
        Timestamp: time.Now(),
        Scorecard: scorecard,
    })
    return saveHistory(history)
}

func DetectRegressions(history BenchmarkHistory) []RegressionAlert {
    // Compare last two commits, flag suites where success_rate dropped >10%
}
```

**Verification:**
```bash
go test ./internal/benchmark/ -run TestHistoryTracking -v -count=1
# Expected: records benchmark, detects regression
```

---

## Task Summary

| # | Task | Tier | Priority | Est. Time |
|---|------|------|----------|-----------|
| 1 | Fix `detectPath()` — tree-internal path tracking | 1 | 🔴 Critical | 30 min |
| 2 | Real-LLM benchmark mode + A/B comparison | 1 | 🔴 Critical | 45 min |
| 3 | Fix statistical tests (Fisher + bootstrap) | 1 | 🟠 Major | 30 min |
| 4 | Load real BFCL V1/V3 datasets | 2 | 🟠 Major | 60 min |
| 5 | Real SWE-bench verification | 2 | 🟠 Major | 90 min |
| 6 | Full ToolBench + τ-bench datasets | 2 | 🟡 Moderate | 45 min |
| 7 | Mock tool servers for execution verification | 3 | 🟡 Moderate | 60 min |
| 8 | LLM-as-judge for GAIA semantic evaluation | 3 | 🟡 Moderate | 30 min |
| 9 | Historical benchmark tracking | 4 | 🟢 Nice | 30 min |

**Total:** ~7 hours (can parallelize Tier 2 tasks)

---

## Dependencies

- **Tasks 1-3**: Independent, can run in parallel
- **Tasks 4-6**: Independent of each other, depend on Task 2 (real LLM mode)
- **Tasks 7-8**: Depend on Tasks 4-6 (need real data to verify against)
- **Task 9**: Depends on Tasks 1-3 (need valid metrics to track)
