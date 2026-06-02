package engine

import (
	"fmt"
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
	// switch version uses bb.Result (receiver), not b.Result (parameter)
	// Create receiver with Result set appropriately
	fn := (&Blackboard{Result: ""}).conditionForName("DetectKnowledgeGaps")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	// empty Result → gap detected
	if !fn(&Blackboard{Task: "any task"}) {
		t.Error("expected true for empty result (gap)")
	}

	fn2 := (&Blackboard{Result: "comprehensive report with full analysis"}).conditionForName("DetectKnowledgeGaps")
	// complete result → no gap
	if fn2(&Blackboard{Task: "any task"}) {
		t.Error("expected false for complete result (no gap)")
	}

	fn3 := (&Blackboard{Result: "gap missing data"}).conditionForName("DetectKnowledgeGaps")
	if !fn3(&Blackboard{Task: "any task"}) {
		t.Error("expected true when result mentions gap")
	}
}

func TestCondFallback_CheckSourceCount(t *testing.T) {
	fn := (&Blackboard{Result: "short"}).conditionForName("CheckSourceCount")
	if fn == nil {
		t.Fatal("expected non-nil")
	}
	if fn(&Blackboard{Task: "any"}) {
		t.Error("expected false for short result")
	}

	longResult := make([]byte, 101)
	for i := range longResult {
		longResult[i] = 'x'
	}
	fn2 := (&Blackboard{Result: string(longResult)}).conditionForName("CheckSourceCount")
	if !fn2(&Blackboard{Task: "any"}) {
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
