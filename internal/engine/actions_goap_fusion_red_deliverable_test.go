package engine

import (
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
)

// 2026-08-01: red-evidence completion inferred "the RED command passed, so the
// work already landed at HEAD". For an "add characterization tests to X_test.go"
// milestone the recorded RED command is the EXISTING package suite, which passes
// precisely BECAUSE the new test file was never written — the inference is
// inverted. A live audit found 41 of 47 checkable red-evidence completions named
// a _test.go that does not exist and, per git log, never did. These tests pin the
// deliverable post-condition that closes that hole, and — just as importantly —
// pin that an UNDETERMINED probe never decides anything either way.

// seedDeliverableProgram points goapProgramsPath at a temp store holding one
// program whose head milestone goal names testFile as its deliverable.
func seedDeliverableProgram(t *testing.T, goal string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("Deliverable probe program", "auto-seed:coverage", []string{goal, "tail milestone"})
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	prev := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = prev })
	return p.ID
}

// stubRepoFileState replaces the HEAD file probe for one test.
func stubRepoFileState(t *testing.T, exists, known bool) {
	t.Helper()
	prev := goapFusionRepoFileStateFn
	goapFusionRepoFileStateFn = func(string) (bool, bool) { return exists, known }
	t.Cleanup(func() { goapFusionRepoFileStateFn = prev })
}

// redPassBB builds a blackboard charged against programID:0 whose Result carries
// a RED command, i.e. exactly what a red-pass cycle hands the refund path.
func redPassBB(programID string) *Blackboard {
	return &Blackboard{
		RunID: "runX",
		ChainState: map[string]any{
			"goap_fusion_program_milestone_charged": programID + ":0",
		},
		Result: "## GOAP Superpowers Execution Failed\n\nRED command unexpectedly passed; " +
			"refusing to run GREEN without failing regression evidence: go test ./internal/engine -short\n",
	}
}

const deliverableGoal = "Add characterization tests pinning the current exported behavior of " +
	"internal/engine/delegate_hooks.go in sibling internal/engine/delegate_hooks_test.go — table-driven where natural."

// The whole point: two red-passes must NOT complete a milestone whose named
// deliverable provably does not exist at HEAD.
func TestRedPass_MissingDeliverableNeverCompletesMilestone(t *testing.T) {
	id := seedDeliverableProgram(t, deliverableGoal)
	stubRepoFileState(t, false, true) // probe is certain: the test file is absent

	for range goapRedPassCompleteStreak + 2 {
		handleGoapRedPassCycleFailure(redPassBB(id))
	}

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	m := ps.Programs[0].Milestones[0]
	if m.Status == "done" {
		t.Fatalf("milestone completed on red-evidence although its deliverable %q is missing "+
			"(completed_run=%q) — this is the 41-false-completion defect", "delegate_hooks_test.go", m.CompletedRun)
	}
}

// The legitimate case must keep working: when the deliverable IS at HEAD, a
// repeated red-pass really is evidence the work landed out-of-band.
func TestRedPass_PresentDeliverableStillCompletesMilestone(t *testing.T) {
	id := seedDeliverableProgram(t, deliverableGoal)
	stubRepoFileState(t, true, true) // probe is certain: the test file exists

	for range goapRedPassCompleteStreak {
		handleGoapRedPassCycleFailure(redPassBB(id))
	}

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := ps.Programs[0].Milestones[0].Status; got != "done" {
		t.Fatalf("milestone status = %q, want \"done\": a landed deliverable plus %d red-passes "+
			"is the case red-evidence completion exists for", got, goapRedPassCompleteStreak)
	}
}

// An UNDETERMINED probe (git timed out, ENOSPC, index.lock) must not be read as
// "absent" — that is the same unknown-means-failed inversion this change removes
// from the pre-check. It must not complete, and must not destroy evidence.
func TestRedPass_UndeterminedProbeCompletesNothingAndKeepsEvidence(t *testing.T) {
	id := seedDeliverableProgram(t, deliverableGoal)
	stubRepoFileState(t, false, false) // could not determine

	for range goapRedPassCompleteStreak + 1 {
		handleGoapRedPassCycleFailure(redPassBB(id))
	}

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	m := ps.Programs[0].Milestones[0]
	if m.Status == "done" {
		t.Fatalf("milestone completed although the deliverable probe was undetermined — "+
			"an unreadable probe is not evidence (completed_run=%q)", m.CompletedRun)
	}
	if m.RedPassStreak == 0 {
		t.Fatal("red-pass evidence was destroyed by an undetermined probe; " +
			"an undetermined probe must decide nothing in either direction")
	}
}

// seedChargedDeliverableProgram seeds a program whose head milestone is already
// charged one attempt, as it is at queue time when a cycle picks it up.
func seedChargedDeliverableProgram(t *testing.T, goal string, attempts int) string {
	t.Helper()
	id := seedDeliverableProgram(t, goal)
	if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
		ps.Programs[0].Milestones[0].Attempts = attempts
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func headMilestoneAttempts(t *testing.T) int {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	return ps.Programs[0].Milestones[0].Attempts
}

// The loop-breaker. A provably-missing deliverable must CHARGE the attempt, so
// the milestone exhausts its budget and blocks visibly. Refunding here is what
// let 2fcc016e319863da treadmill 82 cycles / ~40h: the refund meant the budget
// never depleted, so the same milestone was re-selected forever.
func TestRedPass_MissingDeliverableChargesTheAttempt(t *testing.T) {
	id := seedChargedDeliverableProgram(t, deliverableGoal, 1)
	stubRepoFileState(t, false, true) // certain: absent

	handleGoapRedPassCycleFailure(redPassBB(id))

	if got := headMilestoneAttempts(t); got != 1 {
		t.Fatalf("attempts = %d, want 1: a provably-missing deliverable must NOT refund, "+
			"or the milestone treadmills forever without ever exhausting its budget", got)
	}
}

// Charging the attempt must not ALSO wedge a sibling agent out of the whole
// program. The missing-deliverable branch deliberately skips the refund, which
// is the only other caller of ReleaseClaim on this path, so it has to release
// the claim itself — otherwise the program stays claimed by a dead RunID for
// the full lease and its OTHER pending milestones are skipped too.
func TestRedPass_MissingDeliverableReleasesTheProgramClaim(t *testing.T) {
	id := seedChargedDeliverableProgram(t, deliverableGoal, 1)
	if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
		ps.Programs[0].ClaimedBy = "runX"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stubRepoFileState(t, false, true) // certain: absent

	handleGoapRedPassCycleFailure(redPassBB(id))

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := ps.Programs[0].ClaimedBy; got != "" {
		t.Fatalf("ClaimedBy = %q, want empty: giving up on a milestone must release the program "+
			"claim, or a sibling cycle is locked out of the program's other milestones for the lease", got)
	}
}

// The converse: an UNDETERMINED probe is an outage, and an outage must never
// consume the milestone's abandon budget.
func TestRedPass_UndeterminedProbeRefundsTheAttempt(t *testing.T) {
	id := seedChargedDeliverableProgram(t, deliverableGoal, 1)
	stubRepoFileState(t, false, false) // could not determine

	handleGoapRedPassCycleFailure(redPassBB(id))

	if got := headMilestoneAttempts(t); got != 0 {
		t.Fatalf("attempts = %d, want 0: an undetermined probe is an infrastructure "+
			"failure and must refund, never charge the abandon budget", got)
	}
}

// A goal naming no _test.go deliverable keeps the original behavior: for a
// "fix the bug in Y.go" milestone the plan writes its own failing regression
// test, so a repeated red-pass genuinely means the fix already landed.
func TestRedPass_GoalWithoutTestDeliverableIsUnaffected(t *testing.T) {
	id := seedDeliverableProgram(t, "Fix the off-by-one in internal/engine/executor.go retry accounting.")
	stubRepoFileState(t, false, true) // certain-absent, but no _test.go is named

	for range goapRedPassCompleteStreak {
		handleGoapRedPassCycleFailure(redPassBB(id))
	}

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := ps.Programs[0].Milestones[0].Status; got != "done" {
		t.Fatalf("milestone status = %q, want \"done\": a goal that names no _test.go deliverable "+
			"must keep the pre-existing red-evidence behavior", got)
	}
}

// The SECOND door. precheckGoapStaleMilestones re-runs a milestone's recorded
// RED command at charge time and completes it on a second pass — earlier in the
// cycle than handleGoapRedPassCycleFailure, and via its own MarkDone. Gating
// only the refund path leaves this one wide open.
func TestPrecheck_MissingDeliverableNeitherCompletesNorKeepsEvidence(t *testing.T) {
	seedDeliverableProgram(t, deliverableGoal)
	// Seed the state the pre-check selects on: a prior red-pass streak plus the
	// recorded RED command.
	if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
		ps.Programs[0].Milestones[0].RedPassStreak = 1
		ps.Programs[0].Milestones[0].LastRedCmd = "go test ./internal/engine -short"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stubRepoFileState(t, false, true) // certain: the deliverable is absent

	// The recorded RED command passes again — as it always will, because the
	// deliverable was never written.
	prevRun := goapRedPrecheckRunFn
	goapRedPrecheckRunFn = func(string) (string, error) { return "ok", nil }
	t.Cleanup(func() { goapRedPrecheckRunFn = prevRun })

	precheckGoapStaleMilestones(&Blackboard{RunID: "runPre", ChainState: map[string]any{}})

	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	m := ps.Programs[0].Milestones[0]
	if m.Status == "done" {
		t.Fatalf("pre-check completed a milestone whose deliverable is missing (completed_run=%q) — "+
			"this is the second red-evidence door", m.CompletedRun)
	}
	if m.RedPassStreak != 0 || m.LastRedCmd != "" {
		t.Fatalf("pre-check kept a dead hypothesis (streak=%d, cmd=%q); it must be discarded so the "+
			"pre-check stops re-running an 8-minute command every cycle", m.RedPassStreak, m.LastRedCmd)
	}
}
