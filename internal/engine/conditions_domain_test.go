package engine

import (
	"strings"
	"testing"
)

// ─── Characterization tests for conditions_domain.go ───
//
// These tests pin the current exported (registry) behavior of every
// condition registered by conditions_domain.go's init(). Conditions are
// looked up via GetCondition (the same registry-first path conditionForName
// uses in production), so a test failure here reflects a real change to the
// registered predicate.

// domainCondCase is one assertion against a single registered condition.
type domainCondCase struct {
	cond string
	bb   *Blackboard
	want bool
}

// runDomainCondCases resolves each case's condition once per case (cheap;
// registry lookup is a map read) and compares against the current registered
// behavior.
func runDomainCondCases(t *testing.T, cases []domainCondCase) {
	t.Helper()
	for _, c := range cases {
		fn := GetCondition(c.cond)
		if fn == nil {
			t.Fatalf("GetCondition(%q) returned nil; expected it to be registered by conditions_domain.go", c.cond)
		}
		if got := fn(c.bb); got != c.want {
			t.Errorf("%s(%+v) = %v, want %v", c.cond, c.bb, got, c.want)
		}
	}
}

func TestConditionsDomain_TaskKeywordConditions(t *testing.T) {
	// Conditions matching bb.Task case-sensitively via util.ContainsAnyStr.
	runDomainCondCases(t, []domainCondCase{
		{"IsBoardCheck", &Blackboard{Task: "check the kanban board"}, true},
		{"IsBoardCheck", &Blackboard{Task: "write new feature"}, false},

		{"NeedsDispatch", &Blackboard{Task: "dispatch the next task"}, true},
		{"NeedsDispatch", &Blackboard{Task: "review the code"}, false},

		{"IsStandup", &Blackboard{Task: "daily standup meeting"}, true},
		{"IsStandup", &Blackboard{Task: "build the app"}, false},

		{"IsCreateTask", &Blackboard{Task: "create a new card"}, true},
		{"IsCreateTask", &Blackboard{Task: "review the PR"}, false},

		{"IsRefinement", &Blackboard{Task: "refine the requirements"}, true},
		{"IsRefinement", &Blackboard{Task: "deploy now"}, false},

		{"IsQA", &Blackboard{Task: "run qa checks"}, true},
		{"IsQA", &Blackboard{Task: "deploy the release"}, false},

		{"IsSessionStart", &Blackboard{Task: "session start routine"}, true},
		{"IsSessionStart", &Blackboard{Task: "close the day"}, false},

		{"HasNewContent", &Blackboard{Task: "ingest the new transcript"}, true},
		{"HasNewContent", &Blackboard{Task: "run tests"}, false},

		{"NeedsSynthesis", &Blackboard{Task: "synthesize a wiki note"}, true},
		{"NeedsSynthesis", &Blackboard{Task: "build project"}, false},

		{"NeedsCrossLinks", &Blackboard{Task: "audit orphan pages"}, true},
		{"NeedsCrossLinks", &Blackboard{Task: "write code"}, false},

		{"NeedsIndexUpdate", &Blackboard{Task: "update the index"}, true},
		{"NeedsIndexUpdate", &Blackboard{Task: "write tests"}, false},

		{"IsSessionEnd", &Blackboard{Task: "session end wrap up"}, true},
		{"IsSessionEnd", &Blackboard{Task: "start work"}, false},

		{"IsComparisonQuery", &Blackboard{Task: "compare A vs B"}, true},
		{"IsComparisonQuery", &Blackboard{Task: "explain the concept"}, false},

		{"IsCodeTask", &Blackboard{Task: "fix this code"}, true},
		{"IsCodeTask", &Blackboard{Task: "write documentation"}, false},

		{"IsBugCheck", &Blackboard{Task: "there's a bug to fix"}, true},
		{"IsBugCheck", &Blackboard{Task: "write docs"}, false},

		{"IsStyleCheck", &Blackboard{Task: "run lint checks"}, true},
		{"IsStyleCheck", &Blackboard{Task: "deploy binary"}, false},

		{"IsCIBuildTask", &Blackboard{Task: "run the ci pipeline"}, true},
		{"IsCIBuildTask", &Blackboard{Task: "write a poem"}, false},

		{"NeedsBuild", &Blackboard{Task: "build the project"}, true},
		{"NeedsBuild", &Blackboard{Task: "run tests"}, false},

		{"NeedsTestRun", &Blackboard{Task: "run tests now"}, true},
		{"NeedsTestRun", &Blackboard{Task: "build project"}, false},

		{"NeedsLinting", &Blackboard{Task: "run lint tool"}, true},
		{"NeedsLinting", &Blackboard{Task: "build project"}, false},

		{"NeedsDeploy", &Blackboard{Task: "deploy to prod"}, true},
		{"NeedsDeploy", &Blackboard{Task: "write tests"}, false},

		{"IsMonitorTask", &Blackboard{Task: "monitor agent health"}, true},
		{"IsMonitorTask", &Blackboard{Task: "write code"}, false},

		{"IsMetricsRequest", &Blackboard{Task: "show metrics report"}, true},
		{"IsMetricsRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsRestartRequest", &Blackboard{Task: "restart the dead service"}, true},
		{"IsRestartRequest", &Blackboard{Task: "write code"}, false},

		{"IsRefactorTask", &Blackboard{Task: "refactor the module"}, true},
		{"IsRefactorTask", &Blackboard{Task: "deploy code"}, false},

		{"IsSmellCheck", &Blackboard{Task: "check code smell"}, true},
		{"IsSmellCheck", &Blackboard{Task: "build project"}, false},

		{"IsPatternRequest", &Blackboard{Task: "review design pattern"}, true},
		{"IsPatternRequest", &Blackboard{Task: "deploy code"}, false},

		{"NeedsVerification", &Blackboard{Task: "verify the results"}, true},
		{"NeedsVerification", &Blackboard{Task: "deploy code"}, false},

		{"IsSecurityTask", &Blackboard{Task: "run security audit"}, true},
		{"IsSecurityTask", &Blackboard{Task: "write code"}, false},

		{"IsSASTRequest", &Blackboard{Task: "run sast scan"}, true},
		{"IsSASTRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsDepScanRequest", &Blackboard{Task: "scan dependency for cve"}, true},
		{"IsDepScanRequest", &Blackboard{Task: "write tests"}, false},

		{"IsSecretScan", &Blackboard{Task: "scan for secret leaks"}, true},
		{"IsSecretScan", &Blackboard{Task: "write code"}, false},

		{"IsThreatModel", &Blackboard{Task: "build threat model"}, true},
		{"IsThreatModel", &Blackboard{Task: "write tests"}, false},

		{"IsExtractRequest", &Blackboard{Task: "extract data from source"}, true},
		{"IsExtractRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsTransformRequest", &Blackboard{Task: "transform the dataset"}, true},
		{"IsTransformRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsLoadRequest", &Blackboard{Task: "load data into db"}, true},
		{"IsLoadRequest", &Blackboard{Task: "review code"}, false},

		{"IsActionExtraction", &Blackboard{Task: "extract action items"}, true},
		{"IsActionExtraction", &Blackboard{Task: "deploy code"}, false},

		{"IsSummaryRequest", &Blackboard{Task: "write meeting summary"}, true},
		{"IsSummaryRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsFollowUp", &Blackboard{Task: "send a follow up reminder"}, true},
		{"IsFollowUp", &Blackboard{Task: "deploy code"}, false},

		{"IsCrashTask", &Blackboard{Task: "app crash with stack trace"}, true},
		{"IsCrashTask", &Blackboard{Task: "write docs"}, false},

		{"HasStackTrace", &Blackboard{Task: "goroutine 5 panic"}, true},
		{"HasStackTrace", &Blackboard{Task: "write docs"}, false},

		{"IsRootCauseRequest", &Blackboard{Task: "find root cause"}, true},
		{"IsRootCauseRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsPreventionRequest", &Blackboard{Task: "prevent future issues"}, true},
		{"IsPreventionRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsGameTask", &Blackboard{Task: "update npc behavior"}, true},
		{"IsGameTask", &Blackboard{Task: "deploy code"}, false},

		{"IsPatrolState", &Blackboard{Task: "npc should patrol area"}, true},
		{"IsPatrolState", &Blackboard{Task: "deploy code"}, false},

		{"IsDetectState", &Blackboard{Task: "detect the player"}, true},
		{"IsDetectState", &Blackboard{Task: "deploy code"}, false},

		{"IsChaseState", &Blackboard{Task: "chase the target"}, true},
		{"IsChaseState", &Blackboard{Task: "deploy code"}, false},

		{"IsCombatState", &Blackboard{Task: "enter combat mode"}, true},
		{"IsCombatState", &Blackboard{Task: "deploy code"}, false},

		{"IsRetreatState", &Blackboard{Task: "retreat and heal"}, true},
		{"IsRetreatState", &Blackboard{Task: "deploy code"}, false},

		{"IsTradingTask", &Blackboard{Task: "trading signal detected"}, true},
		{"IsTradingTask", &Blackboard{Task: "write unit tests"}, false},

		{"IsDataRequest", &Blackboard{Task: "fetch market data"}, true},
		{"IsDataRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsTAPath", &Blackboard{Task: "check rsi indicator"}, true},
		{"IsTAPath", &Blackboard{Task: "deploy code"}, false},

		{"IsSignalRequest", &Blackboard{Task: "buy signal detected"}, true},
		{"IsSignalRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsRiskCheck", &Blackboard{Task: "check risk exposure"}, true},
		{"IsRiskCheck", &Blackboard{Task: "deploy code"}, false},

		{"IsAssessRequest", &Blackboard{Task: "assess system maturity"}, true},
		{"IsAssessRequest", &Blackboard{Task: "write poem"}, false},

		{"IsSyncRequest", &Blackboard{Task: "sync data across systems"}, true},
		{"IsSyncRequest", &Blackboard{Task: "write poem"}, false},

		{"IsResearchRequest", &Blackboard{Task: "research and analyze data"}, true},
		{"IsResearchRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsGraphifyRequest", &Blackboard{Task: "run graphify on codebase"}, true},
		{"IsGraphifyRequest", &Blackboard{Task: "deploy code"}, false},

		{"IsBuildRequest", &Blackboard{Task: "go build the binary"}, true},
		{"IsBuildRequest", &Blackboard{Task: "write docs"}, false},

		{"IsImplementRequest", &Blackboard{Task: "implement the new feature"}, true},
		{"IsImplementRequest", &Blackboard{Task: "review docs"}, false},
	})
}

func TestConditionsDomain_LowercasedTaskConditions(t *testing.T) {
	// Conditions that lowercase bb.Task before matching.
	runDomainCondCases(t, []domainCondCase{
		{"IsStudioTask", &Blackboard{Task: "Let's create a podcast episode"}, true},
		{"IsStudioTask", &Blackboard{Task: "Run the deployment pipeline"}, false},

		{"IsResearchTask", &Blackboard{Task: "Please research this topic"}, true},
		{"IsResearchTask", &Blackboard{Task: "Deploy to production"}, false},

		{"IsKanbanTask", &Blackboard{Task: "Move the card to Done column"}, true},
		{"IsKanbanTask", &Blackboard{Task: "Run unit tests"}, false},

		{"IsSecurityCheck", &Blackboard{Task: "Run a Security Audit"}, true},
		{"IsSecurityCheck", &Blackboard{Task: "Write unit tests"}, false},

		{"IsResearchQuery", &Blackboard{Task: "How does this work"}, true},
		{"IsResearchQuery", &Blackboard{Task: "Ship the release"}, false},
	})
}

func TestConditionsDomain_ResultKeywordConditions(t *testing.T) {
	// Conditions matching bb.Result rather than bb.Task.
	runDomainCondCases(t, []domainCondCase{
		{"CheckCitationFormat", &Blackboard{Result: "[1] source: http://example.com"}, true},
		{"CheckCitationFormat", &Blackboard{Result: "plain text"}, false},

		{"HasDeadAgents", &Blackboard{Result: "agent x is dead"}, true},
		{"HasDeadAgents", &Blackboard{Result: "all agents healthy"}, false},

		{"HasProposedFix", &Blackboard{Result: "apply this patch"}, true},
		{"HasProposedFix", &Blackboard{Result: "no resolution yet"}, false},
	})
}

func TestConditionsDomain_IsAmbiguousQuery(t *testing.T) {
	fn := GetCondition("IsAmbiguousQuery")
	if fn == nil {
		t.Fatal("GetCondition(\"IsAmbiguousQuery\") returned nil")
	}
	// Short tasks (<15 chars) are always ambiguous, regardless of content.
	if !fn(&Blackboard{Task: "fix bug"}) {
		t.Error("expected true for short task")
	}
	// Tasks containing the "it" substring (even inside another word) count as
	// ambiguous — this is a broad substring match, not a word-boundary match.
	if !fn(&Blackboard{Task: "why does the capital city matter"}) {
		t.Error("expected true when task contains the substring \"it\" (e.g. inside \"capital\")")
	}
	// A long, unambiguous task with a question keyword and no "it"/"this"
	// substring is not ambiguous.
	if fn(&Blackboard{Task: "why does the server crash today"}) {
		t.Error("expected false for a long task containing a question keyword and no ambiguous substrings")
	}
}

func TestConditionsDomain_IsSimpleQuery(t *testing.T) {
	fn := GetCondition("IsSimpleQuery")
	if fn == nil {
		t.Fatal("GetCondition(\"IsSimpleQuery\") returned nil")
	}
	if !fn(&Blackboard{Task: "short task"}) {
		t.Error("expected true for a short task with no complexity keywords")
	}
	if fn(&Blackboard{Task: "provide a comprehensive analysis of the entire system architecture and design"}) {
		t.Error("expected false for a long task containing 'comprehensive' and 'analysis'")
	}
}

func TestConditionsDomain_IsDeepQuery(t *testing.T) {
	fn := GetCondition("IsDeepQuery")
	if fn == nil {
		t.Fatal("GetCondition(\"IsDeepQuery\") returned nil")
	}
	if !fn(&Blackboard{Task: "give me a deep dive on this topic"}) {
		t.Error("expected true for a task containing 'deep dive'")
	}
	if fn(&Blackboard{Task: "quick question"}) {
		t.Error("expected false for a short task with no depth keywords")
	}
	longTask := strings.Repeat("a", 101)
	if !fn(&Blackboard{Task: longTask}) {
		t.Error("expected true for a task longer than 100 characters")
	}
}

func TestConditionsDomain_DetectKnowledgeGaps(t *testing.T) {
	fn := GetCondition("DetectKnowledgeGaps")
	if fn == nil {
		t.Fatal("GetCondition(\"DetectKnowledgeGaps\") returned nil")
	}
	if !fn(&Blackboard{Result: ""}) {
		t.Error("expected true for empty result")
	}
	if !fn(&Blackboard{Result: "There's a gap in coverage"}) {
		t.Error("expected true when result mentions a gap")
	}
	if fn(&Blackboard{Result: "Everything is well documented and complete"}) {
		t.Error("expected false when result has no gap markers")
	}
}

func TestConditionsDomain_CheckSourceCount(t *testing.T) {
	fn := GetCondition("CheckSourceCount")
	if fn == nil {
		t.Fatal("GetCondition(\"CheckSourceCount\") returned nil")
	}
	if !fn(&Blackboard{Result: strings.Repeat("x", 150)}) {
		t.Error("expected true when result is longer than 100 characters")
	}
	if fn(&Blackboard{Result: "short"}) {
		t.Error("expected false when result is 100 characters or fewer")
	}
}

func TestConditionsDomain_HasTranscript(t *testing.T) {
	fn := GetCondition("HasTranscript")
	if fn == nil {
		t.Fatal("GetCondition(\"HasTranscript\") returned nil")
	}
	if !fn(&Blackboard{Task: strings.Repeat("a", 201)}) {
		t.Error("expected true when task is longer than 200 characters")
	}
	if fn(&Blackboard{Task: "short task"}) {
		t.Error("expected false when task is 200 characters or fewer")
	}
}

func TestConditionsDomain_HasCachedFitness(t *testing.T) {
	fn := GetCondition("HasCachedFitness")
	if fn == nil {
		t.Fatal("GetCondition(\"HasCachedFitness\") returned nil")
	}
	if fn(&Blackboard{}) {
		t.Error("expected false when ChainState is nil")
	}
	if fn(&Blackboard{ChainState: map[string]any{}}) {
		t.Error("expected false when ChainState has no cached_fitness key")
	}
	// Only key presence is checked, not the value's type.
	if !fn(&Blackboard{ChainState: map[string]any{"cached_fitness": "not-a-number"}}) {
		t.Error("expected true purely from key presence, regardless of value type")
	}
}

func TestConditionsDomain_HasFitnessImproved(t *testing.T) {
	fn := GetCondition("HasFitnessImproved")
	if fn == nil {
		t.Fatal("GetCondition(\"HasFitnessImproved\") returned nil")
	}
	if fn(&Blackboard{}) {
		t.Error("expected false when ChainState is nil")
	}
	if fn(&Blackboard{ChainState: map[string]any{"current_fitness": 0.5, "best_fitness": 0.8}}) {
		t.Error("expected false when current_fitness < best_fitness")
	}
	if !fn(&Blackboard{ChainState: map[string]any{"current_fitness": 0.9, "best_fitness": 0.8}}) {
		t.Error("expected true when current_fitness > best_fitness")
	}
	// A non-float value fails the type assertion and is treated as 0.0, not
	// as an error — both sides silently default to zero.
	if fn(&Blackboard{ChainState: map[string]any{"current_fitness": "oops", "best_fitness": 0.8}}) {
		t.Error("expected false when current_fitness has the wrong type (treated as 0.0)")
	}
}

func TestConditionsDomain_PersistentFailures(t *testing.T) {
	fn := GetCondition("PersistentFailures")
	if fn == nil {
		t.Fatal("GetCondition(\"PersistentFailures\") returned nil")
	}
	if fn(nil) {
		t.Error("expected false for a nil Blackboard")
	}
	if fn(&Blackboard{FailureCount: 0, Result: "ok"}) {
		t.Error("expected false with no failures and no failure text")
	}
	if !fn(&Blackboard{FailureCount: 3}) {
		t.Error("expected true once FailureCount reaches the threshold (3)")
	}
	if !fn(&Blackboard{FailureCount: 0, Result: "run failed after retries"}) {
		t.Error("expected true when Result contains the 'failed' fallback marker")
	}
}

// TestConditionsDomain_IsRestartRequest_CaseInsensitive documents the intended
// contract for IsRestartRequest: like its sibling conditions in the same file
// (IsStudioTask, IsResearchTask, IsKanbanTask, IsSecurityCheck, IsResearchQuery
// all lowercase bb.Task before matching), restart intent should be detected
// regardless of how the task text is capitalized — a leading "Restart ..."
// (the natural capitalization at the start of a sentence) must still route.
func TestConditionsDomain_IsRestartRequest_CaseInsensitive(t *testing.T) {
	fn := GetCondition("IsRestartRequest")
	if fn == nil {
		t.Fatal("GetCondition(\"IsRestartRequest\") returned nil")
	}
	if !fn(&Blackboard{Task: "Restart it now"}) {
		t.Error("expected true for capitalized \"Restart it now\" (no other keyword present), matching the case-insensitive contract used by sibling conditions in conditions_domain.go")
	}
}
