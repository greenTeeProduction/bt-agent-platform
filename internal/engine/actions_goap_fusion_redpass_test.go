package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
)

// A RED command passing before GREEN means the plan's predicted regression
// does not exist at HEAD — either the milestone's work already landed
// out-of-band or the plan wrote a weak test. Neither is an unbuildable
// milestone, so it must never consume the milestone-abandon budget, and it
// must win over the infra markers that can share the same failure text.
func TestClassifyGoapCycleFailurePrecedence(t *testing.T) {
	cases := []struct {
		name    string
		outcome string
		result  string
		want    string
	}{
		{"red-pass plain", "failure",
			"## GOAP Superpowers Execution Failed\n\nRED command unexpectedly passed; refusing to run GREEN without failing regression evidence: go test ./internal/evolution",
			goapCycleFailureRedPass},
		{"red-pass wins over infra markers", "pending_patch",
			"RED command unexpectedly passed; refusing to run GREEN\npending_patch: fast-forward refused",
			goapCycleFailureRedPass},
		{"infra stays infra", "failure", "Superpowers Worktree Failed", goapCycleFailureInfra},
		{"own-code gate failure is genuine", "failure", "✗ golangci-lint found issues", goapCycleFailureGenuine},
		{"plain decline is genuine", "failure", "agent declined milestone", goapCycleFailureGenuine},
	}
	for _, tc := range cases {
		if got := classifyGoapCycleFailure(tc.outcome, tc.result); got != tc.want {
			t.Errorf("%s: classifyGoapCycleFailure = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func seedRedPassProgram(t *testing.T) *research.Program {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("Red-pass test program", "test", []string{
		"Wrap X in `internal/engine/foo.go`",
		"Do Y in `internal/engine/bar.go`",
	})
	ps.RecordAttemptAndMaybeBlock(p.ID, 0, goapProgramMaxMilestoneAttempts)
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	return p
}

func redPassMilestone(t *testing.T) research.Milestone {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	return ps.Programs[0].Milestones[0]
}

func chargeRedPassMilestone(t *testing.T, programID string) {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	ps.RecordAttemptAndMaybeBlock(programID, 0, goapProgramMaxMilestoneAttempts)
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
}

func redPassBlackboard(runID, chargedRef string) *Blackboard {
	return &Blackboard{
		RunID:      runID,
		ChainState: map[string]any{"goap_fusion_program_milestone_charged": chargedRef},
	}
}

// One red-pass refunds the queue-time charge and records evidence; the second
// consecutive red-pass — two independently written failing-test plans both
// passing at HEAD — completes the milestone instead of retrying forever
// (loop-breaker for the 2026-07-15 re-attempt-of-landed-work treadmill).
func TestRepeatedRedPassRefundsThenCompletesMilestone(t *testing.T) {
	isolateGoapProgramStore(t)
	p := seedRedPassProgram(t)

	handleGoapRedPassCycleFailure(redPassBlackboard("runA", p.ID+":0"))

	m := redPassMilestone(t)
	if m.Attempts != 0 {
		t.Fatalf("first red-pass must refund the charge, attempts = %d", m.Attempts)
	}
	if m.Status != "pending" {
		t.Fatalf("first red-pass must not complete the milestone, status = %q", m.Status)
	}
	if m.RedPassStreak != 1 {
		t.Fatalf("RedPassStreak = %d, want 1", m.RedPassStreak)
	}

	chargeRedPassMilestone(t, p.ID)
	bb2 := redPassBlackboard("runB", p.ID+":0")
	handleGoapRedPassCycleFailure(bb2)

	m2 := redPassMilestone(t)
	if m2.Status != "done" {
		t.Fatalf("second consecutive red-pass must complete the milestone, status = %q", m2.Status)
	}
	if m2.CompletedRun != "red-evidence:runB" {
		t.Fatalf("CompletedRun = %q, want red-evidence:runB", m2.CompletedRun)
	}
	if !strings.Contains(strings.ToLower(bb2.Result), "red-pass") {
		t.Fatalf("completion must be surfaced in the cycle report, got: %s", bb2.Result)
	}
}

// A genuine implementation failure between red-passes proves the milestone's
// tests CAN fail — the already-landed hypothesis is dead, so the streak
// resets and the next red-pass starts over instead of completing.
func TestGenuineFailureResetsRedPassStreak(t *testing.T) {
	isolateGoapProgramStore(t)
	p := seedRedPassProgram(t)

	handleGoapRedPassCycleFailure(redPassBlackboard("runA", p.ID+":0"))
	if m := redPassMilestone(t); m.RedPassStreak != 1 {
		t.Fatalf("setup: RedPassStreak = %d, want 1", m.RedPassStreak)
	}

	resetGoapMilestoneRedPassStreak(redPassBlackboard("runC", p.ID+":0"))
	if m := redPassMilestone(t); m.RedPassStreak != 0 {
		t.Fatalf("genuine failure must reset the streak, got %d", m.RedPassStreak)
	}

	chargeRedPassMilestone(t, p.ID)
	handleGoapRedPassCycleFailure(redPassBlackboard("runD", p.ID+":0"))
	m := redPassMilestone(t)
	if m.Status != "pending" || m.RedPassStreak != 1 {
		t.Fatalf("post-reset red-pass must start a fresh streak (pending/1), got %q/%d", m.Status, m.RedPassStreak)
	}
}

// isolateBtFusionKnowledge points the shared research knowledge store at a
// temp file so goap:implemented records from tests never touch the live one.
func isolateBtFusionKnowledge(t *testing.T) {
	t.Helper()
	prev := btFusionKnowledgePath
	btFusionKnowledgePath = filepath.Join(t.TempDir(), "knowledge.json")
	t.Cleanup(func() { btFusionKnowledgePath = prev })
}

func goalRedPassBlackboard(runID, goal string) *Blackboard {
	return &Blackboard{
		RunID: runID,
		ChainState: map[string]any{
			"goap_fusion_research_goal_charged":      goapResearchGoalKey(goal),
			"goap_fusion_research_goal_charged_text": goal,
		},
	}
}

func knowledgeHasImplemented(t *testing.T, fragment string) bool {
	t.Helper()
	store, err := research.Open(btFusionKnowledgePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range store.Entries {
		if strings.HasPrefix(e.Source, "goap:implemented") && strings.Contains(e.Title, fragment) {
			return true
		}
	}
	return false
}

// When no program milestone was charged, the head plan task came from the
// charged research goal, so red-pass evidence is genuinely about it. The
// second consecutive red-pass records the goal goap:implemented (research
// stops re-proposing it) and clears its budget — the research-goal
// counterpart of the milestone loop-breaker.
func TestRepeatedRedPassClosesResearchGoalWhenNoMilestoneCharged(t *testing.T) {
	isolateGoapProgramStore(t)
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	goal := "Fix the frobnicator fallback (files: internal/engine/frob.go)"
	key := goapResearchGoalKey(goal)

	handleGoapRedPassCycleFailure(goalRedPassBlackboard("runA", goal))
	if got := reloadGoalBudget(t).RedPassStreak(key); got != 1 {
		t.Fatalf("goal streak after first red-pass = %d, want 1", got)
	}
	if knowledgeHasImplemented(t, "frobnicator") {
		t.Fatal("first red-pass must not close the goal")
	}

	bb2 := goalRedPassBlackboard("runB", goal)
	handleGoapRedPassCycleFailure(bb2)
	if !knowledgeHasImplemented(t, "frobnicator") {
		t.Fatal("second consecutive red-pass must record the goal goap:implemented")
	}
	if got := reloadGoalBudget(t).Count(key); got != 0 {
		t.Fatalf("closed goal's budget must be cleared, count = %d", got)
	}
	if !strings.Contains(strings.ToLower(bb2.Result), "red-pass") {
		t.Fatalf("closure must be surfaced in the cycle report, got: %s", bb2.Result)
	}
}

// A red-pass on a cycle that charged a program milestone is attributed to the
// milestone (the head plan task); the research goal queued behind it was not
// attempted and must not accrue red-pass evidence.
func TestMilestoneChargedRedPassDoesNotTouchResearchGoal(t *testing.T) {
	isolateGoapProgramStore(t)
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	p := seedRedPassProgram(t)
	goal := "Fix the frobnicator fallback (files: internal/engine/frob.go)"

	bb := redPassBlackboard("runA", p.ID+":0")
	bb.ChainState["goap_fusion_research_goal_charged"] = goapResearchGoalKey(goal)
	bb.ChainState["goap_fusion_research_goal_charged_text"] = goal
	handleGoapRedPassCycleFailure(bb)

	if got := reloadGoalBudget(t).RedPassStreak(goapResearchGoalKey(goal)); got != 0 {
		t.Fatalf("milestone-charged cycle must not streak the research goal, got %d", got)
	}
	if m := redPassMilestone(t); m.RedPassStreak != 1 {
		t.Fatalf("milestone streak = %d, want 1", m.RedPassStreak)
	}
}

// A genuine implementation failure kills the already-landed hypothesis for
// the goal too: RecordFailure resets the streak, and closure starts over.
func TestGenuineFailureResetsResearchGoalRedPassStreak(t *testing.T) {
	isolateGoapProgramStore(t)
	isolateBtFusionKnowledge(t)
	seedGoalBudget(t)
	goal := "Fix the frobnicator fallback (files: internal/engine/frob.go)"
	key := goapResearchGoalKey(goal)

	handleGoapRedPassCycleFailure(goalRedPassBlackboard("runA", goal))

	chargeBB := &Blackboard{RunID: "runB", Result: "commit gate: nilerr", ChainState: map[string]any{
		"goap_fusion_research_goal_charged": key,
	}}
	if !chargeGoapResearchGoalFailure(chargeBB) {
		t.Fatal("setup: genuine charge must land")
	}
	if got := reloadGoalBudget(t).RedPassStreak(key); got != 0 {
		t.Fatalf("genuine failure must reset the goal streak, got %d", got)
	}

	handleGoapRedPassCycleFailure(goalRedPassBlackboard("runC", goal))
	if got := reloadGoalBudget(t).RedPassStreak(key); got != 1 {
		t.Fatalf("post-reset red-pass must start a fresh streak, got %d", got)
	}
	if knowledgeHasImplemented(t, "frobnicator") {
		t.Fatal("goal must not be closed on a fresh streak of 1")
	}
}
