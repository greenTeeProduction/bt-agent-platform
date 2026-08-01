package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/research"
)

// 2026-08-01, measured on the live fleet: precheckGoapStaleMilestones re-runs a
// milestone's recorded RED command with cmd.Dir = goapFusionRepo — the BARE main
// checkout. Recorded RED commands are whole-package runs, e.g.
//
//	/usr/local/go/bin/go test ./internal/engine -short -count=1 -timeout 300s
//
// Run there it exits 1 with exactly two failures, both TestNewGoBuildTool_*:
// "error obtaining VCS status: exit status 128 — Use -buildvcs=false". That is a
// property of the bare repo, not of any milestone. So the pre-check ALWAYS took
// its "RED still fails → the work is genuinely missing" branch, always called
// ResetRedPassStreak, and goapRedPassCompleteStreak = 2 became unreachable:
// 216 red-pass/pre-check events in 48h, milestone 2fcc016e:0 alternating for 17h.
//
// Two independent defects are pinned here:
//
//	B1 — the pre-check burns an 8-minute shell to re-derive a question the file
//	     system answers instantly. When the goal's named _test.go deliverable is
//	     provably absent at HEAD the work definitively has not been done, so the
//	     "already landed" hypothesis is dead without running anything.
//	B2 — a RED command that could not produce a verdict (timeout, fork failure)
//	     was read as "the predicted regression exists" and destroyed evidence.
//	     Only an error carrying an exit code is a test verdict.

// stubPrecheckExitError carries an exit code the way *exec.ExitError does, so a
// fake can express "the test binary ran and failed" without forking.
type stubPrecheckExitError struct{ code int }

func (e *stubPrecheckExitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *stubPrecheckExitError) ExitCode() int { return e.code }

// seedRedEvidenceProgram seeds an isolated store whose head milestone carries a
// red-pass streak plus a recorded RED command — the exact state the pre-check
// selects on — with goal text the caller controls.
func seedRedEvidenceProgram(t *testing.T, goal string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "programs.json")
	ps, err := research.OpenPrograms(path)
	if err != nil {
		t.Fatal(err)
	}
	p := ps.Add("Red evidence program", "auto-seed:coverage", []string{goal, "tail milestone"})
	ps.Programs[0].Milestones[0].RedPassStreak = 1
	ps.Programs[0].Milestones[0].LastRedCmd = "go test ./internal/engine -short -count=1"
	if err := ps.Save(); err != nil {
		t.Fatal(err)
	}
	prev := goapProgramsPath
	goapProgramsPath = path
	t.Cleanup(func() { goapProgramsPath = prev })
	return p.ID
}

// stubRedEvidenceFileState replaces the HEAD deliverable probe for one test.
func stubRedEvidenceFileState(t *testing.T, exists, determined bool) {
	t.Helper()
	prev := goapFusionRepoFileStateFn
	goapFusionRepoFileStateFn = func(string) (bool, bool) { return exists, determined }
	t.Cleanup(func() { goapFusionRepoFileStateFn = prev })
}

// countingRedPrecheck stubs the RED runner and records every invocation.
func countingRedPrecheck(t *testing.T, out string, err error) *[]string {
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

func headRedEvidence(t *testing.T) research.Milestone {
	t.Helper()
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		t.Fatal(err)
	}
	return ps.Programs[0].Milestones[0]
}

const coverageDeliverableGoal = "Add characterization tests pinning the current exported behavior of " +
	"internal/engine/metrics_hooks.go in sibling internal/engine/metrics_hooks_test.go — table-driven where natural."

// B1. The deliverable the goal names is provably absent, so the work has not
// been done and no test run can change that. The pre-check must reach that
// verdict from the file system and never shell the 8-minute RED command.
func TestPrecheck_MissingDeliverableSkipsTheRedCommand(t *testing.T) {
	seedRedEvidenceProgram(t, coverageDeliverableGoal)
	stubRedEvidenceFileState(t, false, true) // certain: metrics_hooks_test.go is absent
	calls := countingRedPrecheck(t, "ok", nil)

	precheckGoapStaleMilestones(&Blackboard{RunID: "runPre", ChainState: map[string]any{}})

	if len(*calls) != 0 {
		t.Fatalf("pre-check shelled the RED command %v — when the named deliverable is provably "+
			"absent the hypothesis is already dead; running an 8-minute package suite to re-derive "+
			"that is the per-cycle burn this fix removes", *calls)
	}
	m := headRedEvidence(t)
	if m.Status == "done" {
		t.Fatalf("milestone completed although its deliverable is missing (completed_run=%q)", m.CompletedRun)
	}
	if m.RedPassStreak != 0 || m.LastRedCmd != "" {
		t.Fatalf("dead hypothesis kept (streak=%d cmd=%q); it must be discarded so the pre-check "+
			"stops re-selecting this milestone every cycle", m.RedPassStreak, m.LastRedCmd)
	}
}

// B2. A RED command that never produced a verdict — killed by the 8-minute
// timeout, or a shell that could not start — is not evidence of anything. It
// must leave the recorded evidence exactly as it found it.
func TestPrecheck_UnrunnableRedCommandKeepsEvidence(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"killed by the shell timeout", fmt.Errorf("command killed by 8m0s shell timeout: %w", context.DeadlineExceeded)},
		{"shell never started", errors.New("fork/exec /bin/bash: cannot allocate memory")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedRedEvidenceProgram(t, "Fix the off-by-one in internal/engine/executor.go retry accounting.")
			stubRedEvidenceFileState(t, true, true) // deliverable question is not in play
			countingRedPrecheck(t, "", tc.err)

			precheckGoapStaleMilestones(&Blackboard{RunID: "runPre", ChainState: map[string]any{}})

			m := headRedEvidence(t)
			if m.RedPassStreak != 1 || m.LastRedCmd == "" {
				t.Fatalf("evidence destroyed by a verdict-less RED run (streak=%d cmd=%q): only an "+
					"error carrying an exit code is a test verdict; a timeout or a failed exec means "+
					"we do not know", m.RedPassStreak, m.LastRedCmd)
			}
			if m.Status == "done" {
				t.Fatal("a verdict-less RED run must not complete the milestone either")
			}
		})
	}
}

// The converse of B2: a RED command that really did run and really did fail
// carries an exit code, and that IS evidence — the predicted regression exists,
// so the already-landed hypothesis dies and the milestone takes a real attempt.
func TestPrecheck_FailingRedWithExitCodeStillClearsHypothesis(t *testing.T) {
	seedRedEvidenceProgram(t, "Fix the off-by-one in internal/engine/executor.go retry accounting.")
	stubRedEvidenceFileState(t, true, true)
	countingRedPrecheck(t, "--- FAIL: TestRetryAccounting", &stubPrecheckExitError{code: 1})

	precheckGoapStaleMilestones(&Blackboard{RunID: "runPre", ChainState: map[string]any{}})

	m := headRedEvidence(t)
	if m.RedPassStreak != 0 || m.LastRedCmd != "" {
		t.Fatalf("a genuinely failing RED must clear the hypothesis (streak=%d cmd=%q)", m.RedPassStreak, m.LastRedCmd)
	}
}

// Guard against over-blocking: a goal whose named deliverable IS at HEAD, and a
// goal that names no _test.go at all, must both still consult the RED command.
// Skipping it there would disable red-evidence completion entirely.
func TestPrecheck_SatisfiedDeliverableStillRunsRedCommand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		goal   string
		exists bool
	}{
		{"deliverable present at HEAD", coverageDeliverableGoal, true},
		{"goal names no _test.go deliverable", "Fix the off-by-one in internal/engine/executor.go retry accounting.", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seedRedEvidenceProgram(t, tc.goal)
			stubRedEvidenceFileState(t, tc.exists, true)
			calls := countingRedPrecheck(t, "ok", nil)

			precheckGoapStaleMilestones(&Blackboard{RunID: "runPre", ChainState: map[string]any{}})

			if len(*calls) == 0 {
				t.Fatal("pre-check skipped the RED command although the deliverable post-condition " +
					"does not rule the hypothesis out — red-evidence completion must still work here")
			}
		})
	}
}

// An UNDETERMINED deliverable probe (git error, ENOSPC, index.lock) must not be
// read as "absent". It falls through to the RED command exactly as before,
// because an unreadable probe decides nothing.
func TestPrecheck_UndeterminedDeliverableProbeFallsThroughToRed(t *testing.T) {
	seedRedEvidenceProgram(t, coverageDeliverableGoal)
	stubRedEvidenceFileState(t, false, false) // could not determine
	calls := countingRedPrecheck(t, "ok", nil)

	precheckGoapStaleMilestones(&Blackboard{RunID: "runPre", ChainState: map[string]any{}})

	if len(*calls) == 0 {
		t.Fatal("an undetermined deliverable probe must not be treated as certain-absent; " +
			"the pre-check falls through to the RED command")
	}
}

// The tri-state probe itself: exit 0 = present, exit 1 = absent, anything else
// (git error, timeout, exec failure) = undetermined.
func TestGoapFusionRepoFileState_TriState(t *testing.T) {
	cases := []struct {
		name           string
		err            error
		wantExists     bool
		wantDetermined bool
	}{
		{"exit 0 means present", nil, true, true},
		{"exit 1 means absent", &stubPrecheckExitError{code: 1}, false, true},
		{"exit 128 is a git error, not absence", &stubPrecheckExitError{code: 128}, false, false},
		{"exec failure is undetermined", errors.New("fork/exec: no such file or directory"), false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := goapRepoFileShellFn
			goapRepoFileShellFn = func(string) (string, error) { return "", tc.err }
			defer func() { goapRepoFileShellFn = prev }()

			exists, determined := goapFusionRepoFileState("internal/engine/x_test.go")
			if exists != tc.wantExists || determined != tc.wantDetermined {
				t.Fatalf("goapFusionRepoFileState() = (exists=%v, determined=%v), want (%v, %v)",
					exists, determined, tc.wantExists, tc.wantDetermined)
			}
		})
	}
}

// The probe's value rests on one empirical claim: that the git command it shells
// returns DISTINGUISHABLE exit codes for "absent" versus "git failed". Stubbing
// the exit code only replays that assumption — drive the real binary. This is
// the test that catches a wrong command choice (`git cat-file -e` exits 128 for
// an absent path, so "missing" would be unreachable) or a git upgrade.
func TestGoapFusionRepoFileState_AgainstRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "internal", "engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	present := filepath.Join("internal", "engine", "present_test.go")
	if err := os.WriteFile(filepath.Join(repo, present), []byte("package engine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "seed")

	prevRepo := goapFusionRepo
	goapFusionRepo = repo
	t.Cleanup(func() { goapFusionRepo = prevRepo })

	if exists, determined := goapFusionRepoFileState(present); !exists || !determined {
		t.Errorf("committed file: got (exists=%v, determined=%v), want (true, true)", exists, determined)
	}
	absent := filepath.Join("internal", "engine", "absent_test.go")
	if exists, determined := goapFusionRepoFileState(absent); exists || !determined {
		t.Errorf("absent file: got (exists=%v, determined=%v), want (false, true) — an absent "+
			"deliverable must be provably absent, or goapDeliverablesMissing is unreachable",
			exists, determined)
	}
}

// The deliverable verdict: "missing" must win over "unknown" (one provably-absent
// deliverable is enough to know the RED command did not discriminate), and a goal
// naming no _test.go is deliberately Satisfied.
func TestGoapRedPassDeliverableVerdict(t *testing.T) {
	cases := []struct {
		name  string
		goal  string
		state func(string) (bool, bool)
		want  goapDeliverableVerdict
	}{
		{"no _test.go named is satisfied", "Fix internal/engine/executor.go retry accounting.",
			func(string) (bool, bool) { return false, true }, goapDeliverablesSatisfied},
		{"named deliverable present", coverageDeliverableGoal,
			func(string) (bool, bool) { return true, true }, goapDeliverablesSatisfied},
		{"named deliverable absent", coverageDeliverableGoal,
			func(string) (bool, bool) { return false, true }, goapDeliverablesMissing},
		{"probe undetermined", coverageDeliverableGoal,
			func(string) (bool, bool) { return false, false }, goapDeliverablesUnknown},
		{"missing beats unknown", "Cover internal/engine/a_test.go and internal/engine/b_test.go",
			func(p string) (bool, bool) {
				if filepath.Base(p) == "a_test.go" {
					return false, false // undetermined
				}
				return false, true // provably absent
			}, goapDeliverablesMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := goapFusionRepoFileStateFn
			goapFusionRepoFileStateFn = tc.state
			defer func() { goapFusionRepoFileStateFn = prev }()

			if got := goapRedPassDeliverableVerdict(tc.goal); got != tc.want {
				t.Fatalf("goapRedPassDeliverableVerdict() = %v, want %v", got, tc.want)
			}
		})
	}
}
