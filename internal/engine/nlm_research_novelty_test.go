package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// Gap 7 of the 2026-07-23 fleet review: the scheduled researcher ran the SAME
// daily-rotation topic on every 2-hour tick — four near-identical 17.7KB
// syntheses a day, web-research quota burned to re-learn what the morning run
// already imported. Two fixes pinned here: the idle topic rotates per 2-hour
// SLOT (not per day), and a novelty gate skips the whole pipeline when the
// derived query was already researched within the recency window.

func isolateNlmResearchClock(t *testing.T, at time.Time) {
	t.Helper()
	prev := nlmResearchNowFn
	nlmResearchNowFn = func() time.Time { return at }
	t.Cleanup(func() { nlmResearchNowFn = prev })
}

func isolateEmptyPrograms(t *testing.T) {
	t.Helper()
	prev := goapProgramsPath
	goapProgramsPath = filepath.Join(t.TempDir(), "programs.json")
	t.Cleanup(func() { goapProgramsPath = prev })
}

// Idle-rotation topics must differ across the day's 2-hour slots — the old
// YearDay-only index served one topic all day.
func TestDeriveNotebookLMResearchQuery_RotatesWithinTheDay(t *testing.T) {
	withNlmEconomy(t)
	withArc42Doc(t, arc42GoalsTestDoc)
	isolateEmptyPrograms(t)

	boiler := "Production NotebookLM researcher — domain:notebooklm tree. Runs every 2 hours."
	day := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)

	isolateNlmResearchClock(t, day.Add(9*time.Hour))
	q1 := deriveNotebookLMResearchQuery(boiler)
	isolateNlmResearchClock(t, day.Add(11*time.Hour))
	q2 := deriveNotebookLMResearchQuery(boiler)

	if q1 == q2 {
		t.Fatalf("adjacent 2-hour slots derived the identical idle topic %q — rotation must advance within the day", q1)
	}
}

// A query already researched within the recency window must short-circuit
// BEFORE any nlm invocation: the quota is preserved and the run reports a
// healthy no_change skip.
func TestResearchNotebookLM_NoveltyGateSkipsRecentDuplicate(t *testing.T) {
	withNlmEconomy(t)
	isolateEmptyPrograms(t)
	isolateNlmResearchClock(t, time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))

	var calls []string
	prevRun := nlmRun
	nlmRun = func(_ time.Duration, args ...string) string {
		calls = append(calls, strings.Join(args, " "))
		return ""
	}
	t.Cleanup(func() { nlmRun = prevRun })

	action := GetAction("ResearchNotebookLM")
	if action == nil {
		t.Fatal("ResearchNotebookLM not registered")
	}

	task := "auction protocols for task allocation"
	nlmMarkResearchQueryDone(task) // researched moments ago

	bb := &Blackboard{Task: task, ChainState: map[string]any{}}
	if got := action(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("gated run status = %d, want 1 (healthy skip); result: %s", got, bb.Result)
	}
	if len(calls) != 0 {
		t.Fatalf("nlm invoked %v despite the novelty gate — the quota burn is exactly what the gate exists to prevent", calls)
	}
	if bb.Outcome != "no_change" {
		t.Fatalf("outcome = %q, want no_change", bb.Outcome)
	}
	if !strings.Contains(bb.Result, "novelty gate") {
		t.Fatalf("skip report must name the novelty gate; got: %s", bb.Result)
	}
}

// A successful research run marks its query done, arming the gate for the
// rest of the window; a fresh query passes the gate.
func TestResearchNotebookLM_SuccessfulRunMarksQueryDone(t *testing.T) {
	withNlmEconomy(t)
	isolateEmptyPrograms(t)
	isolateNlmResearchClock(t, time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC))

	prevDir := nlmResearchSynthesesDir
	nlmResearchSynthesesDir = t.TempDir()
	t.Cleanup(func() { nlmResearchSynthesesDir = prevDir })

	prevRun := nlmRun
	nlmRun = func(_ time.Duration, args ...string) string {
		if len(args) >= 2 && args[0] == "research" && args[1] == "start" {
			return "task_id: abc-123"
		}
		if len(args) >= 2 && args[0] == "research" && args[1] == "status" {
			return "status: completed, 4 cited sources"
		}
		return "{}"
	}
	t.Cleanup(func() { nlmRun = prevRun })

	task := "behavior tree evolution benchmarks"
	if nlmResearchQueryRecentlySeen(task) {
		t.Fatal("fresh query must not be pre-seen")
	}

	action := GetAction("ResearchNotebookLM")
	bb := &Blackboard{Task: task, ChainState: map[string]any{}}
	if got := action(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("outcome = %q, want success", bb.Outcome)
	}
	if !nlmResearchQueryRecentlySeen(task) {
		t.Fatal("a successful run must mark its query done so the next tick's duplicate is gated")
	}
}
