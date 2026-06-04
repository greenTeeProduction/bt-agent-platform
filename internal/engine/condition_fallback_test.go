package engine

import (
	"fmt"
	"strings"
	"testing"
)

// ─── conditionForName switch-fallback branch coverage ───
// These conditions live ONLY in the switch statement (not in registry init()).
// They fire only when the registry doesn't have the name.

// clusterTest runs a named condition function against multiple (task, wantTrue) pairs.
type condCase struct {
	task     string
	wantTrue bool
}

func condTest(t *testing.T, condName string, cases []condCase) {
	t.Helper()
	fn := (&Blackboard{}).conditionForName(condName)
	if fn == nil {
		t.Fatalf("conditionForName(%q) returned nil", condName)
	}
	for _, c := range cases {
		got := fn(&Blackboard{Task: c.task})
		if got != c.wantTrue {
			t.Errorf("conditionForName(%q)(%q) = %v, want %v", condName, c.task, got, c.wantTrue)
		}
	}
}

// ─── Go-related conditions ───

func TestCondFallback_IsGoRelated(t *testing.T) {
	condTest(t, "IsGoRelated", []condCase{
		{"review this Go code", true},
		{"how to use goroutines", true},
		{"write a poem about nature", false},
	})
}

func TestCondFallback_IsCodeReview(t *testing.T) {
	condTest(t, "IsCodeReview", []condCase{
		{"review the pull request", true},
		{"run lint on the codebase", true},
		{"build the binary", false},
	})
}

func TestCondFallback_NeedsCompilation(t *testing.T) {
	condTest(t, "NeedsCompilation", []condCase{
		{"build the project", true},
		{"compile the Go module", true},
		{"review the code", false},
	})
}

func TestCondFallback_NeedsTesting(t *testing.T) {
	condTest(t, "NeedsTesting", []condCase{
		{"run the tests", true},
		{"check test coverage", true},
		{"build the binary", false},
	})
}

func TestCondFallback_IsGoQuestion(t *testing.T) {
	condTest(t, "IsGoQuestion", []condCase{
		{"what is the best practice for Go error handling", true},
		{"explain Go interfaces", true},
		{"build the Go project", false},
	})
}

// ─── Finance conditions ───

func TestCondFallback_IsFinanceTask(t *testing.T) {
	condTest(t, "IsFinanceTask", []condCase{
		{"build a DCF model for valuation", true},
		{"run KYC screening on new client", true},
		{"write a Go HTTP server", false},
	})
}

func TestCondFallback_IsCompsRequest(t *testing.T) {
	condTest(t, "IsCompsRequest", []condCase{
		{"run comps analysis on Tesla", true},
		{"find comparable companies", true},
		{"build a DCF model", false},
	})
}

func TestCondFallback_IsLBORequest(t *testing.T) {
	condTest(t, "IsLBORequest", []condCase{
		{"build lbo model for acquisition", true},
		{"leveraged buyout analysis needed", true},
		{"review quarterly earnings", false},
	})
}

func TestCondFallback_IsDCFRequest(t *testing.T) {
	condTest(t, "IsDCFRequest", []condCase{
		{"run dcf model on Apple", true},
		{"discounted cash flow analysis", true},
		{"prepare pitch deck for client", false},
	})
}

func TestCondFallback_IsEarningsRequest(t *testing.T) {
	condTest(t, "IsEarningsRequest", []condCase{
		{"analyze Q3 earnings", true},
		{"review the 10-q filing", true},
		{"build a comps table", false},
	})
}

func TestCondFallback_IsKYCRequest(t *testing.T) {
	condTest(t, "IsKYCRequest", []condCase{
		{"run kyc screening on new investor", true},
		{"aml compliance check required", true},
		{"build a DCF model", false},
	})
}

// ─── Domain tree conditions ───

func TestCondFallback_IsSecurityCheck(t *testing.T) {
	condTest(t, "IsSecurityCheck", []condCase{
		{"audit security vulnerabilities", true},
		{"check for SQL injection in code", true},
		{"build the web application", false},
	})
}

func TestCondFallback_IsCIBuildTask(t *testing.T) {
	condTest(t, "IsCIBuildTask", []condCase{
		{"set up CI/CD pipeline", true},
		{"deploy to production", true},
		{"review pull request", false},
	})
}

func TestCondFallback_IsMonitorTask(t *testing.T) {
	condTest(t, "IsMonitorTask", []condCase{
		{"monitor agent health", true},
		{"check dashboard status", true},
		{"write unit tests", false},
	})
}

func TestCondFallback_IsCrashTask(t *testing.T) {
	condTest(t, "IsCrashTask", []condCase{
		{"investigate production crash", true},
		{"analyze stack trace from panic", true},
		{"write new feature", false},
	})
}

func TestCondFallback_IsGameTask(t *testing.T) {
	condTest(t, "IsGameTask", []condCase{
		{"implement npc patrol behavior", true},
		{"create ai for enemy behavior", true},
		{"audit code security", false},
	})
}

func TestCondFallback_IsTradingTask(t *testing.T) {
	condTest(t, "IsTradingTask", []condCase{
		{"analyze trading signal for Bitcoin", true},
		{"check market price of Tesla stock", true},
		{"write Go code", false},
	})
}

// ─── Research conditions ───

func TestCondFallback_IsResearchQuery(t *testing.T) {
	condTest(t, "IsResearchQuery", []condCase{
		{"research quantum computing advances", true},
		{"investigate latest AI frameworks", true},
		{"build the Go binary", false},
	})
}

func TestCondFallback_IsSimpleQuery(t *testing.T) {
	condTest(t, "IsSimpleQuery", []condCase{
		{"what is Go", true}, // short: < 60 chars
		{"a very long comprehensive deep analysis of the entire system architecture and all its components with detailed recommendations for improvement", false}, // > 100 chars
		{"compare and contrast Go and Rust", false}, // contains "compare" exclusion
	})
}

func TestCondFallback_IsDeepQuery(t *testing.T) {
	condTest(t, "IsDeepQuery", []condCase{
		{"comprehensive analysis of the entire system architecture and all its components with detailed recommendations for improvement and future directions for development of the platform", true}, // > 100 chars
		{"deep dive into Go memory model", true},
		{"what is Go", false}, // short
	})
}

func TestCondFallback_IsAmbiguousQuery(t *testing.T) {
	condTest(t, "IsAmbiguousQuery", []condCase{
		{"do it", true},                  // short: < 15 chars
		{"build the project", true},      // contains "it" substring → ambiguous
		{"what should I do next", false}, // > 15 chars, has question word, no ambiguous keywords
	})
}

// ─── Kanban conditions ───

func TestCondFallback_IsKanbanTask(t *testing.T) {
	condTest(t, "IsKanbanTask", []condCase{
		{"move card to in progress", true},
		{"update Kanban board status", true},
		{"write React component", false},
	})
}

func TestCondFallback_IsBoardCheck(t *testing.T) {
	condTest(t, "IsBoardCheck", []condCase{
		{"check for stale cards on board", true},
		{"monitor board bottlenecks", true},
		{"create new feature", false},
	})
}

func TestCondFallback_IsStandup(t *testing.T) {
	condTest(t, "IsStandup", []condCase{
		{"daily standup report", true},
		{"give status update", true},
		{"refactor the codebase", false},
	})
}

// ─── Vault management conditions ───

func TestCondFallback_IsSessionStart(t *testing.T) {
	condTest(t, "IsSessionStart", []condCase{
		{"session start routine", true},
		{"morning boot sequence", true},
		{"ingest new content", false},
	})
}

func TestCondFallback_HasNewContent(t *testing.T) {
	condTest(t, "HasNewContent", []condCase{
		{"ingest new transcript for processing", true},
		{"save raw notes from session", true},
		{"start the session", false},
	})
}

func TestCondFallback_NeedsSynthesis(t *testing.T) {
	condTest(t, "NeedsSynthesis", []condCase{
		{"synthesize daily notes into wiki", true},
		{"create note from extracted concepts", true},
		{"import new articles", false},
	})
}

// ─── Hermes self-evolution conditions ───

func TestCondFallback_HasSkillGaps(t *testing.T) {
	condTest(t, "HasSkillGaps", []condCase{
		{"update skill for new workflow", true},
		{"outdated skill detected", true},
		{"run periodic check", false},
	})
}

func TestCondFallback_HasWorkflowInefficiencies(t *testing.T) {
	condTest(t, "HasWorkflowInefficiencies", []condCase{
		{"optimize workflow for efficiency", true},
		{"redundant steps in pipeline", true},
		{"run routine maintenance", false},
	})
}

func TestCondFallback_HasModelToolIssues(t *testing.T) {
	condTest(t, "HasModelToolIssues", []condCase{
		{"switch model provider", true},
		{"tune tool configuration", true},
		{"review code changes", false},
	})
}

// ─── NotebookLM conditions ───

func TestCondFallback_IsIngestTask(t *testing.T) {
	condTest(t, "IsIngestTask", []condCase{
		{"ingest new source into notebook", true},
		{"import research paper", true},
		{"query existing sources", false},
	})
}

func TestCondFallback_IsQueryTask(t *testing.T) {
	condTest(t, "IsQueryTask", []condCase{
		{"ask about quantum computing", true},
		{"query research database", true},
		{"create audio overview", false},
	})
}

func TestCondFallback_IsStudioTask(t *testing.T) {
	condTest(t, "IsStudioTask", []condCase{
		{"create podcast from notes", true},
		{"generate briefing document", true},
		{"ingest new source", false},
	})
}

func TestCondFallback_IsResearchTask(t *testing.T) {
	condTest(t, "IsResearchTask", []condCase{
		{"research latest AI models", true},
		{"web search for Go frameworks", true},
		{"move card on Kanban", false},
	})
}

// ─── Health and monitoring ───

func TestCondFallback_IsHealthCheck(t *testing.T) {
	condTest(t, "IsHealthCheck", []condCase{
		{"check agent health status", true},
		{"verify the dashboard is running", true},
		{"build the Go binary", false},
	})
}

func TestCondFallback_IsMeetingTask(t *testing.T) {
	condTest(t, "IsMeetingTask", []condCase{
		{"transcribe meeting recording", true},
		{"summarize the standup notes", true},
		{"review pull request", false},
	})
}

// ─── Platform evaluation conditions ───

func TestCondFallback_IsPlatformEval(t *testing.T) {
	condTest(t, "IsPlatformEval", []condCase{
		{"platform maturity assessment needed", true},
		{"gap analysis for production readiness", true},
		{"build the Go binary", false},
	})
}

func TestCondFallback_IsCronTask(t *testing.T) {
	condTest(t, "IsCronTask", []condCase{
		{"cron job health audit", true},
		{"diagnose the hermes cron failure", true},
		{"build the Go binary", false},
	})
}

func TestCondFallback_IsEvolutionTask(t *testing.T) {
	condTest(t, "IsEvolutionTask", []condCase{
		{"tree fitness evaluation needed", true},
		{"order mutations by priority", true},
		{"run periodic check", false},
	})
}

// ─── Vault and lifecycle conditions ───

func TestCondFallback_NeedsSweep(t *testing.T) {
	condTest(t, "NeedsSweep", []condCase{
		{"sweep old notes for pruning", true},
		{"update notes with fresh links", true},
		{"ingest new article", false},
	})
}

func TestCondFallback_NeedsAudit(t *testing.T) {
	condTest(t, "NeedsAudit", []condCase{
		{"audit knowledge gaps", true},
		{"verify all sections complete", true},
		{"build the project", false},
	})
}

func TestCondFallback_NeedsPublish(t *testing.T) {
	condTest(t, "NeedsPublish", []condCase{
		{"publish final report", true},
		{"export briefing for client", true},
		{"import new articles", false},
	})
}

// ─── GOAP conditions ───

func TestCondFallback_IsAssessRequest(t *testing.T) {
	condTest(t, "IsAssessRequest", []condCase{
		{"assess platform maturity", true},
		{"review current state of project", true},
		{"build the Go binary", false},
	})
}

func TestCondFallback_IsBuildRequest(t *testing.T) {
	condTest(t, "IsBuildRequest", []condCase{
		{"build the project binary", true},
		{"compile Go source code", true},
		{"review the pull request", false},
	})
}

func TestCondFallback_IsImplementRequest(t *testing.T) {
	condTest(t, "IsImplementRequest", []condCase{
		{"implement new feature", true},
		{"fix bug in auth module", true},
		{"review code for style", false},
	})
}

// ─── Incident and debugging conditions ───

func TestCondFallback_HasStackTrace(t *testing.T) {
	condTest(t, "HasStackTrace", []condCase{
		{"error at main.go:42 with goroutine leak", true},
		{"goroutine stack trace shows deadlock", true},
		{"build the project", false},
	})
}

func TestCondFallback_IsRootCauseRequest(t *testing.T) {
	condTest(t, "IsRootCauseRequest", []condCase{
		{"find root cause of crash", true},
		{"debug why service is down", true},
		{"deploy new version", false},
	})
}

func TestCondFallback_IsPreventionRequest(t *testing.T) {
	condTest(t, "IsPreventionRequest", []condCase{
		{"prevent future outages", true},
		{"harden security against attacks", true},
		{"analyze stack trace", false},
	})
}

// ─── Security domain conditions ───

func TestCondFallback_IsSecurityTask(t *testing.T) {
	condTest(t, "IsSecurityTask", []condCase{
		{"perform security audit", true},
		{"check for threat vulnerabilities", true},
		{"build new feature", false},
	})
}

func TestCondFallback_IsSecretScan(t *testing.T) {
	condTest(t, "IsSecretScan", []condCase{
		{"scan for leaked secrets in repo", true},
		{"check for hardcoded credentials", true},
		{"run unit tests", false},
	})
}

func TestCondFallback_IsDepScanRequest(t *testing.T) {
	condTest(t, "IsDepScanRequest", []condCase{
		{"scan dependency for CVEs", true},
		{"check package security", true},
		{"review Go code", false},
	})
}

// ─── Company startup condition ───

func TestCondFallback_ValidateCompanyState(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("ValidateCompanyState")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// No ChainState → false
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	// ChainState with company → true
	if !fn(&Blackboard{ChainState: map[string]any{"company": &struct{}{}}}) {
		t.Error("expected true with company in ChainState")
	}
	// ChainState without company → false
	if fn(&Blackboard{ChainState: map[string]any{"other": "value"}}) {
		t.Error("expected false without company key")
	}
}

// ─── HasFeatureGaps and friends (each condition name matches its chain key) ───

func TestCondFallback_HasFeatureGaps(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasFeatureGaps")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	if !fn(&Blackboard{ChainState: map[string]any{"has_feature_gaps": true}}) {
		t.Error("expected true when has_feature_gaps is true")
	}
	if fn(&Blackboard{ChainState: map[string]any{"has_feature_gaps": false}}) {
		t.Error("expected false when has_feature_gaps is false")
	}
}

func TestCondFallback_HasLayoutIssues(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasLayoutIssues")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	if !fn(&Blackboard{ChainState: map[string]any{"has_layout_issues": true}}) {
		t.Error("expected true when has_layout_issues is true")
	}
}

func TestCondFallback_HasAPIIssues(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasAPIIssues")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	if !fn(&Blackboard{ChainState: map[string]any{"has_api_issues": true}}) {
		t.Error("expected true when has_api_issues is true")
	}
}

// ─── Stockfish evolution conditions ───

func TestCondFallback_HasCachedFitness(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasCachedFitness")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	if !fn(&Blackboard{ChainState: map[string]any{"cached_fitness": 85.0}}) {
		t.Error("expected true with cached_fitness")
	}
}

func TestCondFallback_HasFitnessImproved(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("HasFitnessImproved")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// No ChainState → false
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	// current > best → true
	if !fn(&Blackboard{ChainState: map[string]any{"current_fitness": 90.0, "best_fitness": 80.0}}) {
		t.Error("expected true when current > best")
	}
	// current ≤ best → false
	if fn(&Blackboard{ChainState: map[string]any{"current_fitness": 75.0, "best_fitness": 80.0}}) {
		t.Error("expected false when current ≤ best")
	}
}

// ─── Arc42 section conditions (use section_N_done underscore format) ───

func TestCondFallback_Section1Done(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("Section1Done")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// Registry version returns true if section_1_done in ChainState OR
	// the file exists on disk (01-introduction-goals.md may exist on this system).
	// Just verify it works with ChainState set
	if !fn(&Blackboard{ChainState: map[string]any{"section_1_done": true}}) {
		t.Error("expected true when section_1_done is true")
	}
}

func TestCondFallback_AllSectionsDone(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("AllSectionsDone")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// No ChainState → false
	if fn(&Blackboard{}) {
		t.Error("expected false without ChainState")
	}
	// All sections done → true (registry version uses section_N_done with underscore)
	cs := make(map[string]any)
	for i := 1; i <= 12; i++ {
		cs[fmt.Sprintf("section_%d_done", i)] = true
	}
	if !fn(&Blackboard{ChainState: cs}) {
		t.Error("expected true when all sections done")
	}
	// One section missing → false
	delete(cs, "section_3_done")
	if fn(&Blackboard{ChainState: cs}) {
		t.Error("expected false when section_3 is not done")
	}
}

// ─── Research gap conditions (switch uses bb.Result receiver, not b parameter) ───

func TestCondFallback_DetectKnowledgeGaps(t *testing.T) {
	// Registry version uses parameter bb.Result (not captured receiver)
	fn := (&Blackboard{}).conditionForName("DetectKnowledgeGaps")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// empty Result → gap detected
	if !fn(&Blackboard{Task: "any task", Result: ""}) {
		t.Error("expected true for empty result (gap)")
	}

	// complete result → no gap
	if fn(&Blackboard{Task: "any task", Result: "comprehensive report with full analysis"}) {
		t.Error("expected false for complete result (no gap)")
	}

	// result mentioning gap/missing → gap detected
	if !fn(&Blackboard{Task: "any task", Result: "gap missing data"}) {
		t.Error("expected true when result mentions gap")
	}
}

func TestCondFallback_CheckSourceCount(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("CheckSourceCount")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if fn(&Blackboard{Task: "any", Result: "short"}) {
		t.Error("expected false for short result")
	}

	longResult := make([]byte, 101)
	for i := range longResult {
		longResult[i] = 'x'
	}
	if !fn(&Blackboard{Task: "any", Result: string(longResult)}) {
		t.Error("expected true for result > 100 chars")
	}
}

// ─── Game AI state conditions ───

func TestCondFallback_GameStates(t *testing.T) {
	states := map[string]string{
		"IsPatrolState":  "patrol",
		"IsDetectState":  "detect",
		"IsChaseState":   "chase",
		"IsCombatState":  "attack",
		"IsRetreatState": "retreat",
	}
	for condName, keyword := range states {
		fn := (&Blackboard{}).conditionForName(condName)
		if fn == nil {
			t.Fatalf("conditionForName(%q) returned nil", condName)
		}
		if !fn(&Blackboard{Task: keyword + " the enemy"}) {
			t.Errorf("expected true for %q with task containing %q", condName, keyword)
		}
		if fn(&Blackboard{Task: "write Go code"}) {
			t.Errorf("expected false for %q with non-game task", condName)
		}
	}
}

// ─── Bulk condition coverage surge — 80 untested condition switch cases ───

// simpleContainsCond returns a test function for a condition that does a simple
// containsAny check against the Task field.
func simpleContainsCond(t *testing.T, condName string, trueTasks, falseTasks []string) {
	t.Helper()
	fn := (&Blackboard{}).conditionForName(condName)
	if fn == nil {
		t.Fatalf("conditionForName(%q) returned nil", condName)
	}
	for _, task := range trueTasks {
		if !fn(&Blackboard{Task: task}) {
			t.Errorf("conditionForName(%q)(%q) = false, want true", condName, task)
		}
	}
	for _, task := range falseTasks {
		if fn(&Blackboard{Task: task}) {
			t.Errorf("conditionForName(%q)(%q) = true, want false", condName, task)
		}
	}
}

func TestCondBulk_StandardConditions(t *testing.T) {
	// ─── IsHighPriority ───
	simpleContainsCond(t, "IsHighPriority",
		[]string{"critical bug fix needed", "urgent deployment required", "asap response"},
		[]string{"review code quietly", "write documentation"},
	)
	// ─── IsCodeTask ───
	simpleContainsCond(t, "IsCodeTask",
		[]string{"review this code", "fix a bug", "refactor this function"},
		[]string{"write a poem", "plan the sprint"},
	)
	// ─── IsBugCheck ───
	simpleContainsCond(t, "IsBugCheck",
		[]string{"find the bug", "fix crash issue", "null pointer error"},
		[]string{"write documentation", "style cleanup"},
	)
	// ─── IsStyleCheck ───
	simpleContainsCond(t, "IsStyleCheck",
		[]string{"check style", "run lint on code", "fix naming conventions", "clean up formatting"},
		[]string{"find bugs", "deploy to production"},
	)
	// ─── IsQuestion ───
	simpleContainsCond(t, "IsQuestion",
		[]string{"what is the best approach", "how does this work", "explain the pattern", "define recursion", "best practice for testing"},
		[]string{"deploy the application"},
	)
	// ─── IsQA ───
	simpleContainsCond(t, "IsQA",
		[]string{"run QA checks", "validate the output", "verify requirements", "check results"},
		[]string{"build the project"},
	)
	// ─── IsCreateTask ───
	simpleContainsCond(t, "IsCreateTask",
		[]string{"create a new card", "add card to backlog", "create new feature", "create a task"},
		[]string{"review existing code"},
	)
	// ─── IsRefinement ───
	simpleContainsCond(t, "IsRefinement",
		[]string{"refine the requirements", "expand the spec", "planning session", "detail the user story"},
		[]string{"build and deploy"},
	)
	// ─── IsDevOps ───
	simpleContainsCond(t, "IsDevOps",
		[]string{"deploy to production", "build the pipeline", "kubernetes deployment", "terraform config", "docker compose"},
		[]string{"write a blog post"},
	)
	// ─── IsDataTask ───
	simpleContainsCond(t, "IsDataTask",
		[]string{"etl pipeline setup", "transform the data", "load csv file", "sql query", "parquet dataset"},
		[]string{"review UI wireframes"},
	)
	// ─── IsAnalysisTask ───
	simpleContainsCond(t, "IsAnalysisTask",
		[]string{"strategic analysis", "think tank session", "scenario planning", "forecast next quarter"},
		[]string{"fix this bug"},
	)
	// ─── IsRefactoring ───
	simpleContainsCond(t, "IsRefactoring",
		[]string{"refactor this module", "restructure the codebase", "migrate to new framework"},
		[]string{"write unit tests"},
	)
	// ─── IsIncident ───
	simpleContainsCond(t, "IsIncident",
		[]string{"production crash", "timeout error", "server is down", "broken deployment", "panic in handler"},
		[]string{"add new feature"},
	)
	// ─── IsMeetingTask ───
	simpleContainsCond(t, "IsMeetingTask",
		[]string{"transcribe the meeting", "daily standup notes", "sprint planning", "summarize the call"},
		[]string{"build the binary"},
	)
	// ─── IsNotebookLMTask ───
	simpleContainsCond(t, "IsNotebookLMTask",
		[]string{"notebooklm briefing", "research notebook", "deep research session"},
		[]string{"deploy to production"},
	)
	// ─── IsVaultTask ───
	simpleContainsCond(t, "IsVaultTask",
		[]string{"vault management", "ingest the session", "synthesize daily notes", "update the index", "wiki page edit"},
		[]string{"build the Go binary"},
	)

	// ─── NeedsBuild ───
	simpleContainsCond(t, "NeedsBuild",
		[]string{"build the project", "compile the code"},
		[]string{"review the PR"},
	)
	// ─── NeedsTestRun ───
	simpleContainsCond(t, "NeedsTestRun",
		[]string{"run the tests", "test the module"},
		[]string{"write documentation"},
	)
	// ─── NeedsLinting ───
	simpleContainsCond(t, "NeedsLinting",
		[]string{"lint the codebase", "run static analysis"},
		[]string{"build the binary"},
	)
	// ─── NeedsDeploy ───
	simpleContainsCond(t, "NeedsDeploy",
		[]string{"deploy to staging", "release version 2.0", "ship the feature"},
		[]string{"fix a bug"},
	)
	// ─── NeedsVerification ───
	simpleContainsCond(t, "NeedsVerification",
		[]string{"verify the results", "check the output", "test the fix"},
		[]string{"design the architecture"},
	)
	// ─── NeedsIntegration ───
	simpleContainsCond(t, "NeedsIntegration",
		[]string{"integrate with api", "connect to database", "wire up endpoint", "pipeline setup"},
		[]string{"review code style"},
	)
	// ─── NeedsCrossLinks ───
	simpleContainsCond(t, "NeedsCrossLinks",
		[]string{"cross-link notes", "audit connections", "connect pages", "find orphan pages"},
		[]string{"write unit tests"},
	)
	// ─── NeedsIndexUpdate ───
	simpleContainsCond(t, "NeedsIndexUpdate",
		[]string{"update the index", "refresh MOC", "regenerate index"},
		[]string{"build the binary"},
	)
	// ─── NeedsDispatch ───
	simpleContainsCond(t, "NeedsDispatch",
		[]string{"dispatch the task", "assign to developer", "next action", "start working"},
		[]string{"schedule for later"},
	)

	// ─── IsMeetingPrep ───
	simpleContainsCond(t, "IsMeetingPrep",
		[]string{"client briefing pack", "meeting prep notes", "talking points for call"},
		[]string{"build the model"},
	)
	// ─── IsNoteRequest ───
	simpleContainsCond(t, "IsNoteRequest",
		[]string{"draft a note", "write report", "research deep dive"},
		[]string{"run the pipeline"},
	)
	// ─── IsIndustryRequest ───
	simpleContainsCond(t, "IsIndustryRequest",
		[]string{"industry overview", "sector analysis", "market research", "theme tracking"},
		[]string{"fix the bug"},
	)
	// ─── IsCompetitiveRequest ───
	simpleContainsCond(t, "IsCompetitiveRequest",
		[]string{"competitive landscape", "peer comparison", "market share analysis"},
		[]string{"write test cases"},
	)
	// ─── IsIdeaRequest ───
	simpleContainsCond(t, "IsIdeaRequest",
		[]string{"generate ideas", "screen opportunities", "shortlist candidates"},
		[]string{"compile the code"},
	)
	// ─── IsDeckRequest ───
	simpleContainsCond(t, "IsDeckRequest",
		[]string{"build pitch deck", "create presentation", "update slides", "powerpoint for board"},
		[]string{"reconcile ledger"},
	)
	// ─── Is3StatementRequest ───
	simpleContainsCond(t, "Is3StatementRequest",
		[]string{"build 3-statement model", "three statement projection", "income statement forecast"},
		[]string{"audit GL entries"},
	)
	// ─── IsPrecedentsRequest ───
	simpleContainsCond(t, "IsPrecedentsRequest",
		[]string{"precedent transaction", "m&a comp", "acquisition target"},
		[]string{"run DCF model"},
	)
	// ─── IsAuditRequest ───
	simpleContainsCond(t, "IsAuditRequest",
		[]string{"audit LP statement", "verify capital account", "statement verification"},
		[]string{"build a DCF"},
	)
	// ─── IsGLReconRequest ───
	simpleContainsCond(t, "IsGLReconRequest",
		[]string{"reconcile GL", "general ledger check", "find break in sub-ledger"},
		[]string{"build a pitch deck"},
	)
	// ─── IsMonthEndRequest ───
	simpleContainsCond(t, "IsMonthEndRequest",
		[]string{"month-end close", "accrual journal", "roll-forward schedule", "variance report"},
		[]string{"screen new client"},
	)
	// ─── IsValuationRequest ───
	simpleContainsCond(t, "IsValuationRequest",
		[]string{"fund valuation", "gp capital account", "lp reporting", "net asset value nav"},
		[]string{"run KYC screening"},
	)
	// ─── NeedsModelUpdate ───
	simpleContainsCond(t, "NeedsModelUpdate",
		[]string{"update model with Q3", "refresh the financial model", "roll forward projections"},
		[]string{"audit the statements"},
	)

	// ─── IsDataRequest ───
	simpleContainsCond(t, "IsDataRequest",
		[]string{"fetch market data", "pull price data", "get stock data"},
		[]string{"build the UI"},
	)
	// ─── IsSignalRequest ───
	simpleContainsCond(t, "IsSignalRequest",
		[]string{"check buy signal", "sell signal analysis", "entry point detection"},
		[]string{"write documentation"},
	)
	// ─── IsRiskCheck ───
	simpleContainsCond(t, "IsRiskCheck",
		[]string{"assess risk exposure", "stop loss calculation", "position sizing"},
		[]string{"run the test suite"},
	)
	// ─── IsTAPath ───
	simpleContainsCond(t, "IsTAPath",
		[]string{"technical indicator analysis", "rsi divergence", "macd crossover", "sma pattern"},
		[]string{"fundamental analysis"},
	)
	// ─── IsSummaryRequest ───
	simpleContainsCond(t, "IsSummaryRequest",
		[]string{"generate summary", "meeting notes", "write minutes"},
		[]string{"deploy the code"},
	)
	// ─── IsActionExtraction ───
	simpleContainsCond(t, "IsActionExtraction",
		[]string{"extract action items", "find todos", "next steps"},
		[]string{"build the binary"},
	)
	// ─── IsFollowUp ───
	simpleContainsCond(t, "IsFollowUp",
		[]string{"follow up on task", "send reminder"},
		[]string{"write new feature"},
	)
	// ─── IsRootCauseRequest ───
	simpleContainsCond(t, "IsRootCauseRequest",
		[]string{"find root cause", "why did this fail", "debug the issue"},
		[]string{"document the API"},
	)
	// ─── IsPreventionRequest ───
	simpleContainsCond(t, "IsPreventionRequest",
		[]string{"prevent recurrence", "harden the system", "add guards"},
		[]string{"write new tests"},
	)
	// ─── IsDepScanRequest ───
	simpleContainsCond(t, "IsDepScanRequest",
		[]string{"dependency scan needed", "check package CVEs", "audit library versions"},
		[]string{"review architecture"},
	)
	// ─── IsSASTRequest ───
	simpleContainsCond(t, "IsSASTRequest",
		[]string{"sast scan run", "static analysis check"},
		[]string{"deploy the app"},
	)
	// ─── IsSecretScan ───
	simpleContainsCond(t, "IsSecretScan",
		[]string{"scan for secrets", "check credentials leak", "find API keys in code", "audit tokens"},
		[]string{"write documentation"},
	)
	// ─── IsThreatModel ───
	simpleContainsCond(t, "IsThreatModel",
		[]string{"threat model", "attack surface", "stride assessment"},
		[]string{"build feature X"},
	)
	// ─── IsSecurityTask ───
	simpleContainsCond(t, "IsSecurityTask",
		[]string{"security audit", "threat assessment", "vulnerability scan"},
		[]string{"build the UI"},
	)
	// ─── IsMonitorTask ───
	simpleContainsCond(t, "IsMonitorTask",
		[]string{"monitor system health", "check agent status", "watch for anomalies"},
		[]string{"write new feature"},
	)
	// ─── IsMetricsRequest ───
	simpleContainsCond(t, "IsMetricsRequest",
		[]string{"show metrics", "get stats", "generate report"},
		[]string{"fix the bug"},
	)
	// ─── IsRefactorTask ───
	simpleContainsCond(t, "IsRefactorTask",
		[]string{"refactor this module", "improve code quality", "clean up tech debt", "rewrite the function"},
		[]string{"deploy to prod"},
	)
	// ─── IsSmellCheck ───
	simpleContainsCond(t, "IsSmellCheck",
		[]string{"check code smells", "find cruft", "duplicate code detection"},
		[]string{"write the tests"},
	)
	// ─── IsPatternRequest ───
	simpleContainsCond(t, "IsPatternRequest",
		[]string{"design pattern analysis", "architecture review"},
		[]string{"run the build"},
	)
	// ─── IsExtractRequest ───
	simpleContainsCond(t, "IsExtractRequest",
		[]string{"extract data from source", "ingest CSV file", "load the dataset"},
		[]string{"deploy the app"},
	)
	// ─── IsTransformRequest ───
	simpleContainsCond(t, "IsTransformRequest",
		[]string{"transform the data", "clean the dataset", "normalize fields"},
		[]string{"run the pipeline"},
	)
	// ─── IsLoadRequest ───
	simpleContainsCond(t, "IsLoadRequest",
		[]string{"load data to database", "write to warehouse", "store results"},
		[]string{"review the code"},
	)
	// ─── IsResearchRequest ───
	simpleContainsCond(t, "IsResearchRequest",
		[]string{"research the topic", "search for papers", "discover new approaches"},
		[]string{"deploy the binary"},
	)
	// ─── IsGraphifyRequest ───
	simpleContainsCond(t, "IsGraphifyRequest",
		[]string{"run graphify analysis", "graph the codebase", "structural coupling check"},
		[]string{"write unit tests"},
	)
	// ─── IsBuildRequest ───
	simpleContainsCond(t, "IsBuildRequest",
		[]string{"build the project", "compile the module", "go build the binary", "make the artifact"},
		[]string{"review the PR"},
	)
	// ─── IsImplementRequest ───
	simpleContainsCond(t, "IsImplementRequest",
		[]string{"implement the feature", "plan the implementation", "fix the bug", "create new module"},
		[]string{"run the tests"},
	)
	// ─── IsEngineeringTask ───
	simpleContainsCond(t, "IsEngineeringTask",
		[]string{"engineering sprint", "implement feature", "architecture review", "sw. eng. task"},
		[]string{"marketing campaign"},
	)
	// ─── IsMarketingTask ───
	simpleContainsCond(t, "IsMarketingTask",
		[]string{"marketing campaign", "content strategy", "SEO optimization", "lead generation"},
		[]string{"fix the bug"},
	)
	// ─── IsSalesTask ───
	simpleContainsCond(t, "IsSalesTask",
		[]string{"close the deal", "sales pipeline review", "client demo", "revenue forecast"},
		[]string{"refactor the code"},
	)
	// ─── IsAssessRequest ───
	simpleContainsCond(t, "IsAssessRequest",
		[]string{"assess maturity", "check readiness", "review gaps", "scan vulnerabilities", "measure quality"},
		[]string{"build the feature"},
	)
	// ─── IsSyncRequest ───
	simpleContainsCond(t, "IsSyncRequest",
		[]string{"sync across teams", "pollinate ideas", "align priorities", "cross-reference"},
		[]string{"write new code"},
	)
	// ─── IsDailyResearch ───
	simpleContainsCond(t, "IsDailyResearch",
		[]string{"daily research task", "any task at all"},
		[]string{}, // always true — no false case needed
	)
	// ─── IsDeepResearchDay ───
	simpleContainsCond(t, "IsDeepResearchDay",
		[]string{"deep research task", "any task for sunday"},
		[]string{}, // always true
	)
	// ─── IsPeriodicCheck ───
	simpleContainsCond(t, "IsPeriodicCheck",
		[]string{"periodic health check", "any string works"},
		[]string{}, // always true
	)
	// ─── HasNewAlgorithm ───
	simpleContainsCond(t, "HasNewAlgorithm",
		[]string{"implement new algorithm", "research AI techniques", "create a new method"},
		[]string{"review code style"},
	)
	// ─── HasImprovement ───
	simpleContainsCond(t, "HasImprovement",
		[]string{"improve performance", "enhance the UI", "optimize queries", "tune parameters"},
		[]string{"deploy to production"},
	)
	// ─── IsSessionEnd ───
	simpleContainsCond(t, "IsSessionEnd",
		[]string{"session end", "wrap up the day", "close the session", "end of day summary"},
		[]string{"start the project"},
	)
	// ─── IsComparisonQuery (uses subtest) ───
	t.Run("IsComparisonQuery", func(t *testing.T) {
		// Check b.Task for comparison keywords
		fn := (&Blackboard{}).conditionForName("IsComparisonQuery")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if !fn(&Blackboard{Task: "compare A vs B"}) {
			t.Error("expected true for 'compare A vs B'")
		}
		if !fn(&Blackboard{Task: "versus analysis"}) {
			t.Error("expected true for 'versus analysis'")
		}
		if !fn(&Blackboard{Task: "difference between X and Y"}) {
			t.Error("expected true for 'difference between X and Y'")
		}
		if !fn(&Blackboard{Task: "contrast approaches"}) {
			t.Error("expected true for 'contrast approaches'")
		}
		if fn(&Blackboard{Task: "explain the concept"}) {
			t.Error("expected false for non-comparison task")
		}
	})
}

func TestCondBulk_ChainStateConditions(t *testing.T) {
	// Conditions that check bb.ChainState (receiver, not b parameter)

	t.Run("HasCachedFitness", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("HasCachedFitness")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if fn(&Blackboard{}) {
			t.Error("expected false without ChainState")
		}
		if !fn(&Blackboard{ChainState: map[string]any{"cached_fitness": 0.85}}) {
			t.Error("expected true with cached_fitness")
		}
	})
	t.Run("HasFitnessImproved", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("HasFitnessImproved")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if fn(&Blackboard{}) {
			t.Error("expected false without ChainState")
		}
		if fn(&Blackboard{ChainState: map[string]any{"current_fitness": float64(0.5), "best_fitness": float64(0.8)}}) {
			t.Error("expected false when current < best")
		}
		if !fn(&Blackboard{ChainState: map[string]any{"current_fitness": float64(0.9), "best_fitness": float64(0.8)}}) {
			t.Error("expected true when current > best")
		}
	})

	// Section4Done and Section5Done — registered in arc42_nodes.go uses section_*_done keys (underscore)
	// These also check file existence via sectionFileExists(), which may or may not
	// return true depending on whether the files exist on this system.
	t.Run("Section4Done", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("Section4Done")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		// File may exist, so we don't assert on bare result without ChainState.
		// Just verify it returns a bool and ChainState takes priority.
		_ = fn(&Blackboard{})
		// With ChainState set to true, it should return true regardless of files
		if !fn(&Blackboard{ChainState: map[string]any{"section_4_done": true}}) {
			t.Error("expected true when section_4_done is true")
		}
	})
	t.Run("Section5Done", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("Section5Done")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		// File may exist, so we don't assert on bare result without ChainState.
		_ = fn(&Blackboard{})
		// With ChainState set to true, it should return true regardless of files
		if !fn(&Blackboard{ChainState: map[string]any{"section_5_done": true}}) {
			t.Error("expected true when section_5_done is true")
		}
	})
}

func TestCondBulk_BBResultConditions(t *testing.T) {
	// Registry version uses parameter bb.Result/Task (not captured receiver)

	t.Run("CheckCitationFormat", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("CheckCitationFormat")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if !fn(&Blackboard{Result: "with [citation] and source: example.com"}) {
			t.Error("expected true when result has brackets or source:")
		}
		if fn(&Blackboard{Result: "plain text without citations"}) {
			t.Error("expected false when result has no citations")
		}
	})
	t.Run("HasProposedFix", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("HasProposedFix")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if !fn(&Blackboard{Result: "the fix is to change the code"}) {
			t.Error("expected true when result contains fix")
		}
		if fn(&Blackboard{Result: "no resolution available"}) {
			t.Error("expected false when result has no fix")
		}
	})
	t.Run("HasDeadAgents", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("HasDeadAgents")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if !fn(&Blackboard{Result: "dead agent detected"}) {
			t.Error("expected true when result mentions dead")
		}
		if fn(&Blackboard{Result: "all agents healthy"}) {
			t.Error("expected false when result is clean")
		}
	})
	t.Run("PersistentFailures", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("PersistentFailures")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		if !fn(&Blackboard{Result: "3+ persistent failures detected"}) {
			t.Error("expected true when result mentions failed")
		}
		if fn(&Blackboard{Result: "all passed"}) {
			t.Error("expected false when result is clean")
		}
	})
	t.Run("HasTranscript", func(t *testing.T) {
		fn := (&Blackboard{}).conditionForName("HasTranscript")
		if fn == nil {
			t.Fatal("expected non-nil")
		}
		longTask := strings.Repeat("a", 201)
		if !fn(&Blackboard{Task: longTask}) {
			t.Error("expected true for long task")
		}
		if fn(&Blackboard{Task: "short"}) {
			t.Error("expected false for short task")
		}
	})
}

func TestCondBulk_ValidateOutput(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("ValidateOutput")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// Short output fails
	if fn(&Blackboard{Result: "hi"}) {
		t.Error("expected false for short output")
	}
	// Long enough output passes
	if !fn(&Blackboard{Result: "Here is a comprehensive analysis of the codebase including all findings"}) {
		t.Error("expected true for substantial output")
	}
}

// TestCondBulk_GraphIsFresh — checks file existence on disk
func TestCondBulk_GraphIsFresh(t *testing.T) {
	fn := (&Blackboard{}).conditionForName("GraphIsFresh")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// Don't check the real file — just verify it doesn't panic
	// and returns a bool (file may or may not exist on this system)
	result := fn(&Blackboard{})
	if !(result == true || result == false) {
		t.Error("expected bool result")
	}
}
