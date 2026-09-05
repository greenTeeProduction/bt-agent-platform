package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// runDomainAction invokes a registered action by name against bb and returns
// the BT status code the action reported.
func runDomainAction(t *testing.T, name string, bb *Blackboard) int {
	t.Helper()
	fn := GetAction(name)
	if fn == nil {
		t.Fatalf("action %q not registered", name)
	}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	return fn(ctx)
}

// ─── Fixed-output domain actions ────────────────────────────────────────────
//
// The bulk of registerDomainActions' ~77 code-review/devops/monitoring/
// refactoring/security/meeting-notes/crash/game-AI/trading/arc42-fallback
// actions follow one of two shapes:
//   - bb.Result = "<fixed text>"   (overwrites whatever Result held before)
//   - bb.Result += "<fixed text>"  (appends to the existing Result)
// Both shapes are pinned here, including which ones overwrite vs. append and
// which ones additionally set bb.Outcome = "success".

type domainActionCase struct {
	name        string
	appendMode  bool
	text        string
	wantOutcome string
	wantStatus  int
}

func domainActionCases() []domainActionCase {
	return []domainActionCase{
		{name: "ScanForBugs", appendMode: false, text: "## Bug Scan\n\nAnalyzing code for null derefs, off-by-one, race conditions.", wantOutcome: "", wantStatus: 1},
		{name: "SuggestBugFixes", appendMode: true, text: "\n\n## Suggested Fix\n- Before/after code with explanation", wantOutcome: "success", wantStatus: 1},
		{name: "ScanForVulns", appendMode: false, text: "## Security Scan\n\nOWASP Top 10, injection, auth bypass checked.", wantOutcome: "", wantStatus: 1},
		{name: "SuggestSecurityFixes", appendMode: true, text: "\n\n## Secure Alternative\n- Parameterized queries, input validation", wantOutcome: "success", wantStatus: 1},
		{name: "CheckCodeStyle", appendMode: false, text: "## Style Check\n\nNaming conventions, formatting, idiomatic patterns verified.", wantOutcome: "", wantStatus: 1},
		{name: "SuggestStyleFixes", appendMode: true, text: "\n\n## Style Corrections\n- Rename, reformat, restructure", wantOutcome: "success", wantStatus: 1},
		{name: "RunBuild", appendMode: false, text: "## Build Output\n\nExecuting build command...", wantOutcome: "", wantStatus: 1},
		{name: "CheckBuildErrors", appendMode: true, text: "\n\n0 errors, 3 warnings.", wantOutcome: "", wantStatus: 1},
		{name: "FixBuildIssues", appendMode: true, text: "\n\n## Fixes Applied\n- Missing import, type mismatch resolved", wantOutcome: "", wantStatus: 1},
		{name: "RunTests", appendMode: false, text: "## Test Results\n\n42 passed, 0 failed, 2 skipped.", wantOutcome: "", wantStatus: 1},
		{name: "RunLinter", appendMode: false, text: "## Lint Output\n\n5 issues: 2 warnings, 3 info.", wantOutcome: "", wantStatus: 1},
		{name: "AnalyzeLintOutput", appendMode: true, text: "\n\nCategorized: 2 style, 1 complexity, 2 naming.", wantOutcome: "", wantStatus: 1},
		{name: "RunDeploy", appendMode: false, text: "## Deploy\n\nDeployment started to staging.", wantOutcome: "", wantStatus: 1},
		{name: "VerifyDeploy", appendMode: true, text: "\n\nHealth check: 200 OK, smoke tests passed.", wantOutcome: "", wantStatus: 1},
		{name: "RollbackOnFailure", appendMode: true, text: "\n\nRollback: not needed (deploy succeeded).", wantOutcome: "", wantStatus: 1},
		{name: "CheckAllAgents", appendMode: false, text: "## Agent Health\n\nPinging all MCP servers...", wantOutcome: "", wantStatus: 1},
		{name: "IdentifyDeadAgents", appendMode: true, text: "\n\nDead: 0, Slow: 1 (td-agent 2.3s response).", wantOutcome: "", wantStatus: 1},
		{name: "VerifyRestart", appendMode: true, text: "\n\nRe-check: all agents healthy.", wantOutcome: "success", wantStatus: 1},
		{name: "SendAlert", appendMode: true, text: "\n\n⚠ Alert sent to operator.", wantOutcome: "", wantStatus: 1},
		{name: "EscalateToOperator", appendMode: true, text: "\n\nEscalated for human intervention.", wantOutcome: "", wantStatus: 1},
		{name: "CollectAgentMetrics", appendMode: false, text: "## Agent Metrics\n\nUptime, tool calls, error rates collected.", wantOutcome: "", wantStatus: 1},
		{name: "GenerateHealthReport", appendMode: true, text: "\n\nDashboard-ready health report generated.", wantOutcome: "success", wantStatus: 1},
		{name: "DetectCodeSmells", appendMode: false, text: "## Code Smells\n\nLong functions (3), deep nesting (2), duplication (1).", wantOutcome: "", wantStatus: 1},
		{name: "SuggestRefactorings", appendMode: true, text: "\n\n## Suggestions\n- Extract method, simplify condition, DRY.", wantOutcome: "", wantStatus: 1},
		{name: "RecommendPatterns", appendMode: false, text: "## Pattern Recommendations\n\nStrategy, Factory, Observer applicable.", wantOutcome: "", wantStatus: 1},
		{name: "GeneratePatternCode", appendMode: true, text: "\n\nImplementation template generated.", wantOutcome: "", wantStatus: 1},
		{name: "VerifyBehavior", appendMode: true, text: "\n\nExisting tests: 42/42 pass. No regression.", wantOutcome: "", wantStatus: 1},
		{name: "ReportRefactoringImpact", appendMode: true, text: "\n\nRisk: Low. Files changed: 3. Lines: +15/-8.", wantOutcome: "success", wantStatus: 1},
		{name: "RunSASTScan", appendMode: false, text: "## SAST Results\n\nInjection: 0, XSS: 0, Auth: 1 (medium).", wantOutcome: "", wantStatus: 1},
		{name: "GenerateSASTReport", appendMode: true, text: "\n\nPrioritized: 0 critical, 1 medium, 2 low.", wantOutcome: "success", wantStatus: 1},
		{name: "ScanDependencies", appendMode: false, text: "## Dependency Scan\n\nCVE check: 0 critical, 2 moderate.", wantOutcome: "", wantStatus: 1},
		{name: "SuggestDependencyFixes", appendMode: true, text: "\n\nRecommend: bump xyz to v1.2.3, replace abc.", wantOutcome: "", wantStatus: 1},
		{name: "ScanForSecrets", appendMode: false, text: "## Secret Scan\n\nAPI keys found: 0, tokens: 0, passwords: 0.", wantOutcome: "", wantStatus: 1},
		{name: "ReportExposedSecrets", appendMode: true, text: "\n\nNo exposed secrets detected.", wantOutcome: "success", wantStatus: 1},
		{name: "BuildThreatModel", appendMode: false, text: "## Threat Model\n\nSTRIDE analysis complete. Attack surface mapped.", wantOutcome: "", wantStatus: 1},
		{name: "GenerateMitigations", appendMode: true, text: "\n\nControls: input validation, rate limiting, encryption.", wantOutcome: "", wantStatus: 1},
		{name: "ParseTranscript", appendMode: false, text: "## Transcript\n\nSpeakers: Alice (12 turns), Bob (8 turns).", wantOutcome: "", wantStatus: 1},
		{name: "IdentifyTopics", appendMode: true, text: "\n\nTopics: Q1 Review, Hiring, Budget, Timeline.", wantOutcome: "", wantStatus: 1},
		{name: "ExtractActionItems", appendMode: true, text: "\n\nActions: 5 items extracted with owners and deadlines.", wantOutcome: "", wantStatus: 1},
		{name: "AssignOwners", appendMode: true, text: "\n\nOwners assigned: Alice (2), Bob (2), Carol (1).", wantOutcome: "", wantStatus: 1},
		{name: "GenerateSummary", appendMode: true, text: "\n\n## Summary\nKey decisions, discussion points, outcomes.", wantOutcome: "success", wantStatus: 1},
		{name: "FormatMeetingNotes", appendMode: true, text: "\n\nFormatted: date, attendees, agenda, notes, actions.", wantOutcome: "", wantStatus: 1},
		{name: "DistributeNotes", appendMode: true, text: "\n\nDistributed to: team@example.com.", wantOutcome: "", wantStatus: 1},
		{name: "CheckActionStatus", appendMode: false, text: "## Follow-up\n\nActions: 3 complete, 1 in progress, 1 overdue.", wantOutcome: "", wantStatus: 1},
		{name: "SendReminders", appendMode: true, text: "\n\nReminders sent to Bob (overdue: Budget review).", wantOutcome: "", wantStatus: 1},
		{name: "ParseStackFrames", appendMode: false, text: "## Stack Trace\n\nFrames: 12, Crash at: main.go:42 (nil pointer deref).", wantOutcome: "", wantStatus: 1},
		{name: "IdentifyCrashSite", appendMode: true, text: "\n\nCrash site: processRequest(), nil config object.", wantOutcome: "", wantStatus: 1},
		{name: "TraceExecutionPath", appendMode: true, text: "\n\nExecution path: init() → loadConfig() → processRequest().", wantOutcome: "", wantStatus: 1},
		{name: "IdentifyRootCause", appendMode: true, text: "\n\nRoot cause: loadConfig() returns nil on file not found.", wantOutcome: "", wantStatus: 1},
		{name: "GenerateFix", appendMode: true, text: "\n\nFix: add nil check after loadConfig() call.", wantOutcome: "", wantStatus: 1},
		{name: "ApplyFix", appendMode: true, text: "\n\nFix applied: +3 lines, error handling added.", wantOutcome: "", wantStatus: 1},
		{name: "RunRegressionTests", appendMode: true, text: "\n\nRegression tests: 42/42 pass. No new failures.", wantOutcome: "", wantStatus: 1},
		{name: "VerifyCrashResolved", appendMode: true, text: "\n\nCrash reproduced: NO. Fix confirmed.", wantOutcome: "success", wantStatus: 1},
		{name: "SuggestGuards", appendMode: true, text: "\n\n## Guards Added\n- Null checks, bounds checks, error wrapping.", wantOutcome: "", wantStatus: 1},
		{name: "AddMonitoring", appendMode: true, text: "\n\nMonitoring: alert on nil config, file-not-found.", wantOutcome: "", wantStatus: 1},
		{name: "SetPatrolRoute", appendMode: false, text: "## Patrol\n\nRoute: waypoints A→B→C→D→A. Speed: walk.", wantOutcome: "", wantStatus: 1},
		{name: "ExecutePatrol", appendMode: true, text: "\n\nPatrolling... Interruption: none.", wantOutcome: "", wantStatus: 1},
		{name: "ScanEnvironment", appendMode: true, text: "\n\nScan: raycast 12m, proximity 5m, sound 0.", wantOutcome: "", wantStatus: 1},
		{name: "ClassifyThreat", appendMode: true, text: "\n\nThreat: player detected, threat level: 0.7.", wantOutcome: "", wantStatus: 1},
		{name: "CalculatePursuitPath", appendMode: true, text: "\n\nPursuit: A* path 24m, ETA 3.2s.", wantOutcome: "", wantStatus: 1},
		{name: "ExecutePursuit", appendMode: true, text: "\n\nPursuing... distance: 15m → 8m.", wantOutcome: "", wantStatus: 1},
		{name: "SelectTarget", appendMode: true, text: "\n\nTarget: player (threat 0.7, health 60, distance 8m).", wantOutcome: "", wantStatus: 1},
		{name: "ChooseAction", appendMode: true, text: "\n\nAction: melee attack (70% hit chance).", wantOutcome: "", wantStatus: 1},
		{name: "ExecuteCombatAction", appendMode: true, text: "\n\nCombat: 25 damage dealt. Enemy health: 35/60.", wantOutcome: "", wantStatus: 1},
		{name: "EvaluateCombatResult", appendMode: true, text: "\n\nEval: advantage, push forward.", wantOutcome: "", wantStatus: 1},
		{name: "FindSafePosition", appendMode: true, text: "\n\nRetreat: cover at (-12, 8, 2). ETA 1.8s.", wantOutcome: "", wantStatus: 1},
		{name: "ExecuteRetreat", appendMode: true, text: "\n\nRetreating... reached cover. Health: 15/100.", wantOutcome: "", wantStatus: 1},
		{name: "FetchMarketData", appendMode: false, text: "## Market Data\n\nOHLCV fetched: AAPL 2024-01 to 2024-12.", wantOutcome: "", wantStatus: 1},
		{name: "ValidateDataQuality", appendMode: true, text: "\n\nQuality: 0 gaps, 0 outliers, data fresh.", wantOutcome: "", wantStatus: 1},
		{name: "CalculateIndicators", appendMode: true, text: "\n\nIndicators: SMA(20)=185.3, RSI(14)=62, MACD: bullish.", wantOutcome: "", wantStatus: 1},
		{name: "DetectPatterns", appendMode: true, text: "\n\nPatterns: Ascending triangle (bullish), support at 180.", wantOutcome: "", wantStatus: 1},
		{name: "GenerateTASignals", appendMode: true, text: "\n\nSignals: BUY (RSI oversold exit + MACD cross).", wantOutcome: "", wantStatus: 1},
		{name: "ComputeSignal", appendMode: true, text: "\n\nSignal: BUY, strength: 0.72/1.0.", wantOutcome: "", wantStatus: 1},
		{name: "AssessSignalStrength", appendMode: true, text: "\n\nConfidence: 72%. Historical accuracy: 68%.", wantOutcome: "", wantStatus: 1},
		{name: "CheckPositionLimits", appendMode: true, text: "\n\nPosition: 5% of portfolio. Limit: 10%. OK.", wantOutcome: "", wantStatus: 1},
		{name: "CalculateStopLoss", appendMode: true, text: "\n\nStop-loss: $175.80 (ATR-based, 5% below entry).", wantOutcome: "", wantStatus: 1},
		{name: "AssessRiskReward", appendMode: true, text: "\n\nR:R = 2.1:1. Kelly = 15% allocation. Acceptable.", wantOutcome: "success", wantStatus: 1},
		{name: "FallbackSection1", appendMode: false, text: "# 1. Introduction and Goals\n\n## 1.1 Requirements Overview\n\ngo-bt-evolve is a behavior-tree-driven AI agent platform.\n\n## 1.2 Quality Goals\n\n| Goal | Scenario |\n|------|----------|\n| Correctness | Trees route tasks to correct domain paths |\n| Evolvability | 6 evolution algorithms continuously improve trees |\n| Reliability | Panic recovery, circuit breakers, retry with DLQ |\n\n## 1.3 Stakeholders\n\n| Role | Expectations |\n|------|-------------|\n| Nico | Platform architect and developer |\n| Hermes Agent | Automated operator via cron jobs |\n| Dashboard Users | Visual introspection of agents, trees, tasks |", wantOutcome: "success", wantStatus: 1},
	}
}

func TestDomainActions_FixedOutputs(t *testing.T) {
	for _, c := range domainActionCases() {
		t.Run(c.name, func(t *testing.T) {
			bb := &Blackboard{Result: "SEED"}
			status := runDomainAction(t, c.name, bb)

			if status != c.wantStatus {
				t.Errorf("status = %d, want %d", status, c.wantStatus)
			}

			wantResult := c.text
			if c.appendMode {
				wantResult = "SEED" + c.text
			}
			if bb.Result != wantResult {
				t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
			}

			if bb.Outcome != c.wantOutcome {
				t.Errorf("Outcome = %q, want %q", bb.Outcome, c.wantOutcome)
			}
		})
	}
}

// ─── Data pipeline actions ──────────────────────────────────────────────────

func TestValidateDataSource(t *testing.T) {
	t.Run("blocked when task references no existing local file", func(t *testing.T) {
		bb := &Blackboard{Task: "please analyze recent sales performance"}
		status := runDomainAction(t, "ValidateDataSource", bb)

		if status != 1 {
			t.Fatalf("status = %d, want 1", status)
		}
		if bb.Outcome != "blocked_no_source" {
			t.Errorf("Outcome = %q, want %q", bb.Outcome, "blocked_no_source")
		}
		wantResult := fmt.Sprintf("## Data Pipeline Report\n\nstatus: blocked\nreason: %s\navailable_tools: %s\nevidence: checked task text for existing local data files; no extraction was performed.\n",
			"no existing local source file path found in task", "(none)")
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
		// inspectDataSource unconditionally initializes ChainState even on the
		// blocked path, but never populates data_source_path when blocked.
		if bb.ChainState == nil {
			t.Error("ChainState should be initialized to a non-nil map even when blocked")
		}
		if _, ok := bb.ChainState["data_source_path"]; ok {
			t.Error("ChainState[data_source_path] should not be set when blocked")
		}
	})

	t.Run("validates an existing file referenced in the task text", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sales.csv")
		content := "date,amount\n2024-01-01,100\n2024-01-02,200\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		bb := &Blackboard{Task: "please validate the source at " + path}
		status := runDomainAction(t, "ValidateDataSource", bb)

		if status != 1 {
			t.Fatalf("status = %d, want 1", status)
		}
		if bb.Outcome != "" {
			t.Errorf("Outcome = %q, want unset on the source_validated path", bb.Outcome)
		}
		wantMetrics, err := dataFileMetrics(path)
		if err != nil {
			t.Fatalf("dataFileMetrics: %v", err)
		}
		wantResult := fmt.Sprintf("## Data Pipeline Report\n\nstatus: source_validated\nsource: `%s`\nevidence:\n%s\n", path, wantMetrics)
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
		if got, _ := bb.ChainState["data_source_path"].(string); got != path {
			t.Errorf("ChainState[data_source_path] = %q, want %q", got, path)
		}
		if got, _ := bb.ChainState["data_source_metrics"].(string); got != wantMetrics {
			t.Errorf("ChainState[data_source_metrics] = %q, want %q", got, wantMetrics)
		}
	})

	t.Run("reuses an already-set ChainState source path without re-parsing the task", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "reuse.csv")
		if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0644); err != nil {
			t.Fatal(err)
		}

		bb := &Blackboard{
			Task:       "totally unrelated task text with no path",
			ChainState: map[string]any{"data_source_path": path},
		}
		status := runDomainAction(t, "ValidateDataSource", bb)

		if status != 1 {
			t.Fatalf("status = %d, want 1", status)
		}
		if bb.Outcome != "" {
			t.Errorf("Outcome = %q, want unset", bb.Outcome)
		}
		if !strings.Contains(bb.Result, "status: source_validated") {
			t.Errorf("Result = %q, want source_validated status", bb.Result)
		}
	})

	t.Run("falls back to task text when the ChainState path no longer exists", func(t *testing.T) {
		dir := t.TempDir()
		stalePath := filepath.Join(dir, "gone.csv")
		freshPath := filepath.Join(dir, "fresh.csv")
		if err := os.WriteFile(freshPath, []byte("x\n1\n"), 0644); err != nil {
			t.Fatal(err)
		}

		bb := &Blackboard{
			Task:       "use the fresh source at " + freshPath,
			ChainState: map[string]any{"data_source_path": stalePath},
		}
		status := runDomainAction(t, "ValidateDataSource", bb)

		if status != 1 {
			t.Fatalf("status = %d, want 1", status)
		}
		if got, _ := bb.ChainState["data_source_path"].(string); got != freshPath {
			t.Errorf("ChainState[data_source_path] = %q, want fallback to %q", got, freshPath)
		}
	})
}

func TestExtractData(t *testing.T) {
	t.Run("skips when no source path is available", func(t *testing.T) {
		bb := &Blackboard{Result: "seed"}
		status := runDomainAction(t, "ExtractData", bb)

		wantResult := "seed" + "\nextract: skipped — no existing source file was provided.\n"
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
		if bb.Outcome != "" {
			t.Errorf("Outcome = %q, want unset", bb.Outcome)
		}
	})

	t.Run("extracts from an existing source path", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "d.csv")
		if err := os.WriteFile(path, []byte("x\n1\n"), 0644); err != nil {
			t.Fatal(err)
		}
		metrics, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}

		bb := &Blackboard{
			Result:     "seed",
			ChainState: map[string]any{"data_source_path": path, "data_source_metrics": metrics},
		}
		status := runDomainAction(t, "ExtractData", bb)

		wantResult := "seed" + fmt.Sprintf("\nextract: completed from `%s`\n%s\n", path, metrics)
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
		if bb.Outcome != "success" {
			t.Errorf("Outcome = %q, want success", bb.Outcome)
		}
	})
}

func TestValidateTransform(t *testing.T) {
	t.Run("skips when no real source data is available", func(t *testing.T) {
		bb := &Blackboard{Result: "seed"}
		status := runDomainAction(t, "ValidateTransform", bb)

		wantResult := "seed" + "\ntransform_validation: skipped — no real source data available.\n"
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})

	t.Run("acknowledges real source data present", func(t *testing.T) {
		bb := &Blackboard{Result: "seed", ChainState: map[string]any{"data_source_path": "/tmp/x.csv"}}
		status := runDomainAction(t, "ValidateTransform", bb)

		wantResult := "seed" + "\ntransform_validation: real source present; transformations must be explicit before mutation/write.\n"
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})
}

func TestApplyTransform(t *testing.T) {
	t.Run("blocked until a source path exists", func(t *testing.T) {
		bb := &Blackboard{Result: "seed"}
		status := runDomainAction(t, "ApplyTransform", bb)

		wantResult := "seed" + "\ntransform: skipped — blocked until source path exists.\n"
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})

	t.Run("dry-runs against an existing source path", func(t *testing.T) {
		bb := &Blackboard{Result: "seed", ChainState: map[string]any{"data_source_path": "/tmp/x.csv"}}
		status := runDomainAction(t, "ApplyTransform", bb)

		wantResult := "seed" + fmt.Sprintf("\ntransform: dry-run only on `%s`; no rows invented and no file written without explicit target.\n", "/tmp/x.csv")
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})
}

func TestVerifyOutput(t *testing.T) {
	fabricationCases := []string{"10,420", "10,418"}
	for _, marker := range fabricationCases {
		t.Run("fails on fabricated count "+marker, func(t *testing.T) {
			bb := &Blackboard{Result: "rows processed: " + marker}
			status := runDomainAction(t, "VerifyOutput", bb)

			if status != -1 {
				t.Errorf("status = %d, want -1", status)
			}
			if bb.Outcome != "failure_fabricated_count" {
				t.Errorf("Outcome = %q, want failure_fabricated_count", bb.Outcome)
			}
			wantResult := "rows processed: " + marker + "\nverification: FAILED — fabricated canned row count detected.\n"
			if bb.Result != wantResult {
				t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
			}
		})
	}

	t.Run("passes when no fabrication marker is present", func(t *testing.T) {
		bb := &Blackboard{Result: "clean observed output"}
		status := runDomainAction(t, "VerifyOutput", bb)

		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Outcome != "success" {
			t.Errorf("Outcome = %q, want success", bb.Outcome)
		}
		wantResult := "clean observed output" + "\nverification: passed anti-fabrication gate; output only contains observed file metrics or explicit blocked status.\n"
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})
}

func TestValidateTarget(t *testing.T) {
	t.Run("skips when task has no target path", func(t *testing.T) {
		bb := &Blackboard{Task: "no path mentioned here", Result: "seed"}
		status := runDomainAction(t, "ValidateTarget", bb)

		wantResult := "seed" + "\ntarget_validation: no existing target path supplied; load will be skipped.\n"
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
		if _, ok := bb.ChainState["data_target_path"]; ok {
			t.Error("ChainState[data_target_path] should not be set")
		}
	})

	t.Run("parses a target path from the task text even if it does not yet exist", func(t *testing.T) {
		bb := &Blackboard{Task: "write results to output.csv please", Result: "seed"}
		status := runDomainAction(t, "ValidateTarget", bb)

		wantResult := "seed" + fmt.Sprintf("\ntarget_validation: target path parsed `%s`.\n", "output.csv")
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
		if got, _ := bb.ChainState["data_target_path"].(string); got != "output.csv" {
			t.Errorf("ChainState[data_target_path] = %q, want %q", got, "output.csv")
		}
	})
}

func TestLoadData(t *testing.T) {
	t.Run("skips when no explicit target path was supplied", func(t *testing.T) {
		bb := &Blackboard{Result: "seed"}
		status := runDomainAction(t, "LoadData", bb)

		wantResult := "seed" + "\nload: skipped — no explicit target path supplied.\n"
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})

	t.Run("dry-runs against an explicit target path", func(t *testing.T) {
		bb := &Blackboard{Result: "seed", ChainState: map[string]any{"data_target_path": "output.csv"}}
		status := runDomainAction(t, "LoadData", bb)

		wantResult := "seed" + fmt.Sprintf("\nload: dry-run only; target `%s` was not modified without explicit write content.\n", "output.csv")
		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.Result != wantResult {
			t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
		}
	})
}

func TestVerifyLoad(t *testing.T) {
	bb := &Blackboard{Result: "seed"}
	status := runDomainAction(t, "VerifyLoad", bb)

	wantResult := "seed" + "\nload_verification: passed — no unverified writes claimed.\n"
	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
	if bb.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", bb.Outcome)
	}
}

// ─── extractExistingDataPath / dataFileMetrics helpers ──────────────────────

func TestExtractExistingDataPath(t *testing.T) {
	t.Run("empty task yields no path", func(t *testing.T) {
		if got := extractExistingDataPath("", true); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("task with no file-shaped token yields no path", func(t *testing.T) {
		if got := extractExistingDataPath("please summarize the meeting", true); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("resolves an existing absolute path, trimming trailing punctuation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.csv")
		if err := os.WriteFile(path, []byte("a\n"), 0644); err != nil {
			t.Fatal(err)
		}
		task := "the file is at `" + path + "`."
		if got := extractExistingDataPath(task, true); got != path {
			t.Errorf("got %q, want %q", got, path)
		}
	})

	t.Run("requireExists=true skips a non-existent absolute path", func(t *testing.T) {
		task := "the file is at /this/path/does/not/exist-12345.csv"
		if got := extractExistingDataPath(task, true); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("requireExists=false returns a non-existent absolute path as-is", func(t *testing.T) {
		task := "the file is at /this/path/does/not/exist-12345.csv"
		want := "/this/path/does/not/exist-12345.csv"
		if got := extractExistingDataPath(task, false); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("requireExists=true skips a non-existent relative filename", func(t *testing.T) {
		task := "load nonexistent-file-xyz123.csv please"
		if got := extractExistingDataPath(task, true); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("requireExists=false returns a non-existent relative filename verbatim", func(t *testing.T) {
		task := "load nonexistent-file-xyz123.csv please"
		if got := extractExistingDataPath(task, false); got != "nonexistent-file-xyz123.csv" {
			t.Errorf("got %q, want %q", got, "nonexistent-file-xyz123.csv")
		}
	})

	t.Run("resolves an existing relative filename against the working directory", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("local.csv", []byte("a\n"), 0644); err != nil {
			t.Fatal(err)
		}
		wantAbs, err := filepath.Abs("local.csv")
		if err != nil {
			t.Fatal(err)
		}
		task := "load local.csv please"
		if got := extractExistingDataPath(task, true); got != wantAbs {
			t.Errorf("got %q, want %q", got, wantAbs)
		}
	})
}

func TestDataFileMetrics(t *testing.T) {
	t.Run("reports bytes, lines, and mod_time for a plain text file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "notes.txt")
		content := "line one\nline two\nline three"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}

		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatalf("dataFileMetrics: %v", err)
		}
		want := fmt.Sprintf("- bytes: %d\n- lines: %d\n- mod_time: %s", info.Size(), 3, info.ModTime().Format("2006-01-02T15:04:05Z07:00"))
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("counts a trailing newline as ending the last line, not starting a new one", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "trailing.txt")
		if err := os.WriteFile(path, []byte("a\nb\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "- lines: 2\n") {
			t.Errorf("got %q, want lines: 2", got)
		}
	})

	t.Run("reports zero lines for an empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.txt")
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "- lines: 0\n") {
			t.Errorf("got %q, want lines: 0", got)
		}
	})

	t.Run("appends csv record/column counts for a .csv file, including the header row", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "table.csv")
		content := "a,b,c\n1,2,3\n4,5,6\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "\n- csv_records_observed: 3") {
			t.Errorf("got %q, want csv_records_observed: 3 (header + 2 data rows)", got)
		}
		if !strings.Contains(got, "\n- csv_columns_first_record: 3") {
			t.Errorf("got %q, want csv_columns_first_record: 3", got)
		}
	})

	t.Run("appends a csv_parse_error for malformed csv content", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "broken.csv")
		content := "a,b\n\"unterminated,1\n"
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "\n- csv_parse_error:") {
			t.Errorf("got %q, want a csv_parse_error entry", got)
		}
	})

	t.Run("csv extension check is case-insensitive", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "table.CSV")
		if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "csv_records_observed") {
			t.Errorf("got %q, want csv metrics for uppercase .CSV extension", got)
		}
	})

	t.Run("does not append csv metrics for a non-csv file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "notes.txt")
		if err := os.WriteFile(path, []byte("a,b\n1,2\n"), 0644); err != nil {
			t.Fatal(err)
		}
		got, err := dataFileMetrics(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got, "csv_records_observed") {
			t.Errorf("got %q, want no csv metrics for a .txt file", got)
		}
	})

	t.Run("errors on a directory path", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := dataFileMetrics(dir); err == nil {
			t.Error("expected an error for a directory path, got nil")
		}
	})

	t.Run("errors on a nonexistent path", func(t *testing.T) {
		if _, err := dataFileMetrics(filepath.Join(t.TempDir(), "missing.csv")); err == nil {
			t.Error("expected an error for a nonexistent path, got nil")
		}
	})
}

// ─── NotebookLM plan-implement workflow actions ─────────────────────────────

func TestDoGrillMeReview(t *testing.T) {
	origNlmRun := nlmRun
	t.Cleanup(func() { nlmRun = origNlmRun })

	var gotArgs []string
	var gotTimeout time.Duration
	nlmRun = func(timeout time.Duration, args ...string) string {
		gotTimeout = timeout
		gotArgs = args
		return "MOCK NOTEBOOK OUTPUT"
	}

	bb := &Blackboard{Task: "investigate the outage", Result: "prior finding: disk full"}
	status := runDomainAction(t, "DoGrillMeReview", bb)

	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if bb.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", bb.Outcome)
	}
	if gotTimeout != 180*time.Second {
		t.Errorf("nlmRun timeout = %v, want 180s", gotTimeout)
	}

	wantQuery := fmt.Sprintf(
		"CRITICAL REVIEW — be brutally honest.\n\n"+
			"Original task: %s\n\n"+
			"Research findings so far: %s\n\n"+
			"Your job:\n"+
			"1. Identify every gap, missing detail, and unsupported assumption in the findings.\n"+
			"2. List what concrete information is still needed to produce a working implementation.\n"+
			"3. Demand a detailed implementation plan with: specific file paths to create/modify,\n"+
			"   exact function signatures, test cases, and a step-by-step task breakdown.\n"+
			"4. Output ONLY the gaps and required plan — no flattery, no summaries of what's good.\n"+
			"Be critical. Be specific. Be actionable.",
		bb.Task, "prior finding: disk full",
	)
	wantArgs := []string{"notebook", "query", defaultNotebook, wantQuery}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("nlmRun args =\n%#v\nwant:\n%#v", gotArgs, wantArgs)
	}
	if got, ok := bb.ChainState["nlm_grill_query"].(string); !ok || got != wantQuery {
		t.Errorf("ChainState[nlm_grill_query] =\n%q\nwant:\n%q", got, wantQuery)
	}
	wantResult := "## NotebookLM Grill-Me Review\n\n" + "MOCK NOTEBOOK OUTPUT" + "\n"
	if bb.Result != wantResult {
		t.Errorf("Result =\n%q\nwant:\n%q", bb.Result, wantResult)
	}
}

func TestDoGrillMeReview_FallsBackToLastAccumulatedResult(t *testing.T) {
	origNlmRun := nlmRun
	t.Cleanup(func() { nlmRun = origNlmRun })

	var gotArgs []string
	nlmRun = func(_ time.Duration, args ...string) string {
		gotArgs = args
		return "OUT"
	}

	bb := &Blackboard{Task: "t", Results: []string{"r1", "r2"}}
	status := runDomainAction(t, "DoGrillMeReview", bb)

	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if len(gotArgs) != 4 || !strings.Contains(gotArgs[3], "Research findings so far: r2") {
		t.Errorf("query did not use last accumulated result: %#v", gotArgs)
	}
}

func TestWriteImplementationPlan(t *testing.T) {
	t.Chdir(t.TempDir())

	bb := &Blackboard{
		Task:       "build the widget",
		Result:     "research findings here",
		ChainState: map[string]any{"nlm_grill_query": "grill query text"},
	}
	status := runDomainAction(t, "WriteImplementationPlan", bb)

	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if bb.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", bb.Outcome)
	}

	planPath, ok := bb.ChainState["plan_path"].(string)
	if !ok || planPath == "" {
		t.Fatalf("ChainState[plan_path] not set to a non-empty string: %#v", bb.ChainState["plan_path"])
	}
	if dir := filepath.Dir(planPath); dir != ".hermes/plans" {
		t.Errorf("plan_path directory = %q, want .hermes/plans", dir)
	}

	written, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("plan file was not written at %q: %v", planPath, err)
	}
	wantContent := fmt.Sprintf("# Implementation Plan\n\n"+
		"## Original Task\n%s\n\n"+
		"## Research Findings\n%s\n\n"+
		"## Grill-Me Review\n%s\n\n"+
		"---\n"+
		"## Implementation Plan\n\n"+
		"_(Fill in the detailed task breakdown, file paths, and test cases based on research and review above.)_\n\n"+
		"### File Checklist\n- [ ] \n\n"+
		"### Test Plan\n- [ ] \n\n",
		bb.Task, "research findings here", "grill query text")
	if string(written) != wantContent {
		t.Errorf("written plan content =\n%q\nwant:\n%q", string(written), wantContent)
	}
	if !strings.Contains(bb.Result, "## Implementation Plan Written") {
		t.Errorf("Result = %q, want it to report the plan was written", bb.Result)
	}
	if !strings.Contains(bb.Result, planPath) {
		t.Errorf("Result = %q, want it to include the plan path %q", bb.Result, planPath)
	}
}
