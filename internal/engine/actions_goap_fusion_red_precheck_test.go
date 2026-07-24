package engine

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

// Gap 5 of the 2026-07-23 fleet review: 14/20 cycles (70%) were the stale-
// plan treadmill — a milestone whose work already landed burned a full
// Claude plan phase per cycle just to discover "RED unexpectedly passed",
// twice, before the RedPassStreak self-completed it. The red PRE-CHECK
// re-runs the recorded RED command at charge time (seconds, no Claude): a
// second pass completes the milestone before any plan is written, and a
// failing RED kills the already-landed hypothesis so the cycle plans real
// work.

// seedPrecheckProgram seeds an isolated program store whose head milestone
// carries prior red-pass evidence (streak 1 + the recorded RED command).
func seedPrecheckProgram(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("Precheck probe program", "test", []string{"stale head milestone", "fresh tail milestone"})
	ps.Programs[0].Milestones[0].RedPassStreak = 1
	ps.Programs[0].Milestones[0].LastRedCmd = "go test ./internal/foo -run TestBar"
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	prev := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = prev })
	return p.ID
}

func stubRedPrecheck(t *testing.T, out string, err error) *[]string {
	t.Helper()
	var calls []string
	prev := goapRedPrecheckRunFn
	goapRedPrecheckRunFn = func(cmd string) (string, error) {
		calls = append(calls, cmd)
		return out, err
	}
	t.Cleanup(func() { goapRedPrecheckRunFn = prev })
	return &calls
}

func reloadPrecheckMilestone(t *testing.T, idx int) research.Milestone {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	return ps.Programs[0].Milestones[idx]
}

// A stale head milestone (streak 1 + recorded RED) whose RED passes again at
// charge time is completed on the spot — no Claude plan phase — and the
// cycle charges the NEXT pending milestone instead.
func TestPrioritizeGoapGoals_RedPrecheckCompletesStaleMilestone(t *testing.T) {
	id := seedPrecheckProgram(t)
	calls := stubRedPrecheck(t, "ok: TestBar passed", nil)

	prioritize := GetAction("PrioritizeGoapGoals")
	bb := &Blackboard{ChainState: map[string]any{}}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	if len(*calls) != 1 || (*calls)[0] != "go test ./internal/foo -run TestBar" {
		t.Fatalf("pre-check invocations = %v, want exactly the recorded RED command once", *calls)
	}
	head := reloadPrecheckMilestone(t, 0)
	if head.Status != "done" {
		t.Fatalf("stale milestone status = %q, want done (completed on pre-check evidence)", head.Status)
	}
	if !strings.HasPrefix(head.CompletedRun, "red-evidence-precheck:") {
		t.Fatalf("CompletedRun = %q, want the red-evidence-precheck: evidence tag", head.CompletedRun)
	}
	charged, _ := bb.ChainState["goap_fusion_program_milestone_charged"].(string)
	if charged != id+":1" {
		t.Fatalf("charged stamp = %q, want %q (the fresh milestone, not the completed one)", charged, id+":1")
	}
	if tail := reloadPrecheckMilestone(t, 1); tail.Attempts != 1 {
		t.Fatalf("fresh milestone attempts = %d, want 1 (charged for this cycle)", tail.Attempts)
	}
}

// A recorded RED that FAILS at charge time proves the predicted regression
// still exists — the already-landed hypothesis is dead. The evidence is
// cleared (streak + command) and the milestone is charged for a genuine
// implementation attempt.
func TestPrioritizeGoapGoals_RedPrecheckFailingRedClearsHypothesis(t *testing.T) {
	id := seedPrecheckProgram(t)
	calls := stubRedPrecheck(t, "--- FAIL: TestBar", errors.New("exit status 1"))

	prioritize := GetAction("PrioritizeGoapGoals")
	bb := &Blackboard{ChainState: map[string]any{}}
	if got := prioritize(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("PrioritizeGoapGoals status = %d, want 1; result: %s", got, bb.Result)
	}

	if len(*calls) != 1 {
		t.Fatalf("pre-check invocations = %v, want exactly one", *calls)
	}
	head := reloadPrecheckMilestone(t, 0)
	if head.Status != "pending" {
		t.Fatalf("milestone status = %q, want pending (needs real work)", head.Status)
	}
	if head.RedPassStreak != 0 || head.LastRedCmd != "" {
		t.Fatalf("red-pass evidence must be cleared when RED fails (streak=%d cmd=%q)", head.RedPassStreak, head.LastRedCmd)
	}
	charged, _ := bb.ChainState["goap_fusion_program_milestone_charged"].(string)
	if charged != id+":0" {
		t.Fatalf("charged stamp = %q, want %q (the head milestone proceeds to a real attempt)", charged, id+":0")
	}
	if head.Attempts != 1 {
		t.Fatalf("head milestone attempts = %d, want 1", head.Attempts)
	}
}

// A test that forgets to stub the pre-check runner must not execute real
// shell commands or mutate the store (the gap-1 pollution class): the
// DEFAULT runner is inert under `go test`, and its unavailability sentinel
// must not be mistaken for a failing RED (which would clear live evidence).
func TestPrecheckGoapStaleMilestones_DefaultRunnerInertUnderTest(t *testing.T) {
	seedPrecheckProgram(t)
	// Deliberately NO stubRedPrecheck: the default runner is in effect.

	precheckGoapStaleMilestones(&Blackboard{ChainState: map[string]any{}})

	head := reloadPrecheckMilestone(t, 0)
	if head.Status != "pending" || head.RedPassStreak != 1 || head.LastRedCmd == "" {
		t.Fatalf("unstubbed pre-check under go test mutated the store: %+v — the default runner must be inert in test processes", head)
	}
}

// precheckGoapStaleMilestones runs BEFORE this cycle's own ClaimActiveForCycle
// call (PrioritizeGoapGoals calls it at the top, claiming happens later in the
// same function) — so a stale head milestone it completes was necessarily
// claimed by an EARLIER cycle's RunID, not this cycle's. Releasing with this
// cycle's bb.RunID therefore compares against the wrong agent ID and can never
// match, leaking the prior cycle's claim forever (until the lease expires)
// even though the milestone is now done.
func TestPrecheckGoapStaleMilestones_ReleasesClaimHeldByPriorCycle(t *testing.T) {
	id := seedPrecheckProgram(t)
	if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
		if p := ps.ClaimActiveForCycle("prior-cycle-run-id", time.Hour); p == nil {
			t.Fatal("ClaimActiveForCycle: expected the seeded program to be claimable")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stubRedPrecheck(t, "ok: TestBar passed", nil)

	precheckGoapStaleMilestones(&Blackboard{RunID: "current-cycle-run-id", ChainState: map[string]any{}})

	head := reloadPrecheckMilestone(t, 0)
	if head.Status != "done" {
		t.Fatalf("stale milestone status = %q, want done", head.Status)
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	var prog *research.Program
	for _, p := range ps.Programs {
		if p.ID == id {
			prog = p
		}
	}
	if prog == nil {
		t.Fatalf("program %q not found after precheck", id)
	}
	if prog.ClaimedBy != "" {
		t.Fatalf("ClaimedBy = %q, want cleared — stale-milestone auto-completion must release whatever claim is on the program (here, the prior cycle's), not compare against this cycle's own different RunID", prog.ClaimedBy)
	}
}

// The extraction helper recovers the RED command from the executor's refusal
// line so the refund handler can persist it for the next cycle's pre-check.
func TestExtractRedPassCommand(t *testing.T) {
	result := "red-phase claude failed: task RED verification:\n" +
		"RED command unexpectedly passed; refusing to run GREEN without failing regression evidence: go test ./internal/foo -run TestBar\n" +
		"further output"
	if got := extractRedPassCommand(result); got != "go test ./internal/foo -run TestBar" {
		t.Fatalf("extractRedPassCommand = %q", got)
	}
	if got := extractRedPassCommand("no refusal in here"); got != "" {
		t.Fatalf("absent marker must yield empty, got %q", got)
	}
}
