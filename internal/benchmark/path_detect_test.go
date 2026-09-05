package benchmark

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestDetectPath_CurrentPathTakesPriority pins priority 1: when the tree
// traversal already recorded bb.CurrentPath, detectPath must return it
// unconditionally, even when VisitedPaths and a keyword-matching Task are
// also present.
func TestDetectPath_CurrentPathTakesPriority(t *testing.T) {
	bb := &engine.Blackboard{
		CurrentPath:  "ExplicitPath",
		VisitedPaths: []string{"OtherPath", "AnotherPath"},
		Task:         "meeting recap", // would keyword-match MeetingPath if reached
	}
	if got := detectPath("result", bb); got != "ExplicitPath" {
		t.Errorf("detectPath() = %q, want %q", got, "ExplicitPath")
	}
}

// TestDetectPath_VisitedPathsUsedWhenCurrentPathEmpty pins priority 2: with
// CurrentPath empty, detectPath falls back to the FIRST entry of
// VisitedPaths, ignoring later entries and any keyword-matching Task.
func TestDetectPath_VisitedPathsUsedWhenCurrentPathEmpty(t *testing.T) {
	bb := &engine.Blackboard{
		VisitedPaths: []string{"FirstVisited", "SecondVisited"},
		Task:         "meeting recap",
	}
	if got := detectPath("result", bb); got != "FirstVisited" {
		t.Errorf("detectPath() = %q, want %q", got, "FirstVisited")
	}
}

// TestDetectPath_EmptyVisitedPathsSliceFallsThroughToKeyword pins the
// len(bb.VisitedPaths) > 0 guard: a non-nil but empty slice must not be
// treated as populated, so detectPath falls through to the keyword
// fallback on bb.Task.
func TestDetectPath_EmptyVisitedPathsSliceFallsThroughToKeyword(t *testing.T) {
	bb := &engine.Blackboard{
		VisitedPaths: []string{},
		Task:         "meeting recap",
	}
	if got := detectPath("result", bb); got != "MeetingPath" {
		t.Errorf("detectPath() = %q, want %q", got, "MeetingPath")
	}
}

// TestDetectPath_DefaultsToGeneralPath pins the terminal default: with no
// CurrentPath, no VisitedPaths, and a Task that matches no keyword branch
// (including the empty string), detectPath returns GeneralPath.
func TestDetectPath_DefaultsToGeneralPath(t *testing.T) {
	tests := []struct {
		name string
		task string
	}{
		{"empty task", ""},
		{"no keyword match", "flurbnaxle wibbledorp zzyzx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task}
			if got := detectPath("result", bb); got != "GeneralPath" {
				t.Errorf("detectPath(task=%q) = %q, want %q", tt.task, got, "GeneralPath")
			}
		})
	}
}

// TestDetectPath_KeywordFallback_Domains characterizes the keyword-fallback
// switch in detectPath across the domains, pinning current routing for a
// representative phrase per keyword. Order in the table mirrors the switch
// statement's case order in path_detect.go.
func TestDetectPath_KeywordFallback_Domains(t *testing.T) {
	tests := []struct {
		task, expected string
	}{
		// HealthPath
		{"run a health check on all agents", "HealthPath"},
		{"give me agent status", "HealthPath"},
		{"report disk usage", "HealthPath"},
		{"capacity planning for Q4", "HealthPath"},
		{"sre runbook review", "HealthPath"},
		{"sla compliance check", "HealthPath"},
		{"chaos engineering drill", "HealthPath"},
		// MeetingPath
		{"meeting recap", "MeetingPath"},
		{"transcribe the call", "MeetingPath"},
		{"daily standup notes", "MeetingPath"},
		{"publish the minutes", "MeetingPath"},
		{"diarize the audio", "MeetingPath"},
		// CronPath
		{"list cron jobs", "CronPath"},
		{"cron audit report", "CronPath"},
		{"cron governance policy", "CronPath"},
		// EvolutionPath
		{"evaluate tree fitness", "EvolutionPath"},
		{"score the mutation candidate", "EvolutionPath"},
		{"evolution safety checks", "EvolutionPath"},
		{"ensemble evolution run", "EvolutionPath"},
		{"multi-objective evolution search", "EvolutionPath"},
		{"fleet-wide rollout", "EvolutionPath"},
		// PlatformEvalPath
		{"platform maturity scorecard", "PlatformEvalPath"},
		{"find the lowest-scoring domain", "PlatformEvalPath"},
		{"run a gap analysis", "PlatformEvalPath"},
		{"comparative maturity review", "PlatformEvalPath"},
		{"maturity trend over time", "PlatformEvalPath"},
		{"production readiness check", "PlatformEvalPath"},
		// NotebookLMPath (case precedes DevOpsPath, so "research pipeline"
		// below still routes here despite containing "pipeline").
		{"notebooklm summary", "NotebookLMPath"},
		{"chat query about the notebook", "NotebookLMPath"},
		{"generate a briefing doc", "NotebookLMPath"},
		{"build a mind map", "NotebookLMPath"},
		{"cross-notebook synthesis", "NotebookLMPath"},
		{"research pipeline setup", "NotebookLMPath"},
		// VaultPath
		{"check the vault status", "VaultPath"},
		{"ingest the session logs", "VaultPath"},
		{"synthesize daily notes", "VaultPath"},
		{"cross-link related notes", "VaultPath"},
		{"run the weekly sweep", "VaultPath"},
		{"identify a knowledge gap", "VaultPath"},
		// FinancePath (case precedes CodeReviewPath, so "review earnings
		// report" below still routes here despite containing "review").
		{"build a dcf model", "FinancePath"},
		{"structure an lbo deal", "FinancePath"},
		{"review earnings report", "FinancePath"},
		{"complete kyc checks", "FinancePath"},
		{"assess financial risk", "FinancePath"},
		// DataPipelinePath (case precedes DevOpsPath, so "streaming ingest
		// pipeline design" below still routes here despite containing
		// "pipeline").
		{"define a data contract", "DataPipelinePath"},
		{"streaming ingest pipeline design", "DataPipelinePath"},
		{"guarantee exactly-once delivery", "DataPipelinePath"},
		{"perform an incremental load", "DataPipelinePath"},
		{"track data lineage", "DataPipelinePath"},
		// WorkflowPath
		{"move card to backlog board", "WorkflowPath"},
		{"sprint planning session", "WorkflowPath"},
		// IncidentPath
		{"production outage crash report", "IncidentPath"},
		{"postmortem for the incident", "IncidentPath"},
		// KnowledgePath
		{"how does the scheduler work", "KnowledgePath"},
		{"why did the build fail", "KnowledgePath"},
		{"explain the retry logic", "KnowledgePath"},
		// ThinkTankPath
		{"analyze quarterly strategy", "ThinkTankPath"},
		{"forecast next quarter", "ThinkTankPath"},
		// RefactoringPath
		{"restructure the module", "RefactoringPath"},
		{"clean up dead code", "RefactoringPath"},
		// BuildPath
		{"go test coverage report", "BuildPath"},
		{"compile the binary", "BuildPath"},
	}
	for _, tt := range tests {
		t.Run(tt.task, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task}
			if got := detectPath("result", bb); got != tt.expected {
				t.Errorf("detectPath(task=%q) = %q, want %q", tt.task, got, tt.expected)
			}
		})
	}
}

// TestDetectPath_CronCapacityPlanningKeywordCollision guards a routing
// inconsistency in the same family as the SecurityPath/DataPipelinePath
// fixes in bfcl_v3_test.go: eval_suites.go declares CronPath as the
// ExpectedPath for the CronManagement() suite's "cron capacity planning: ..."
// task (internal/benchmark/eval_suites.go), but HealthPath's case in the
// keyword-fallback switch is evaluated first and its "capacity planning"
// keyword matches before the CronPath case's more specific "cron capacity"
// keyword is ever reached. In the backward-compat scoring path (no
// bb.CurrentPath/VisitedPaths), a cron capacity-planning task is silently
// misclassified as HealthPath instead of CronPath.
//
// A generic (non-cron) capacity-planning task must still route to
// HealthPath, pinned as a control so any fix keys on the "cron" prefix
// rather than removing "capacity planning" from HealthPath outright.
func TestDetectPath_CronCapacityPlanningKeywordCollision(t *testing.T) {
	tests := []struct {
		name, task, expected string
	}{
		{
			"cron capacity planning routes to CronPath",
			"cron capacity planning: analyze 8 jobs' resource consumption, identify peak load times, propose schedule staggering",
			"CronPath",
		},
		{
			"generic capacity planning stays HealthPath",
			"capacity planning for Q4 infrastructure",
			"HealthPath",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := &engine.Blackboard{Task: tt.task}
			if got := detectPath("result", bb); got != tt.expected {
				t.Errorf("detectPath(task=%q) = %q, want %q", tt.task, got, tt.expected)
			}
		})
	}
}
