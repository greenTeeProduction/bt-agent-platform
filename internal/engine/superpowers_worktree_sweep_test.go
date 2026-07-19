package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sweepRunner is a CommandRunner fake for sweep tests: records every command,
// serves a canned `git worktree list --porcelain` output, and can force a
// failure on commands containing failOn.
type sweepRunner struct {
	calls        []string
	worktreeList string
	branchList   string
	diffOutput   string
	failOn       string
}

func (r *sweepRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	if r.failOn != "" && strings.Contains(cmd, r.failOn) {
		res.Err = errors.New("forced failure")
		res.Output = "forced failure output"
		return res
	}
	if name == "git" && len(args) >= 2 && args[0] == "worktree" && args[1] == "list" {
		res.Output = r.worktreeList
	}
	if name == "git" && len(args) >= 2 && args[0] == "branch" && args[1] == "--list" {
		res.Output = r.branchList
	}
	if name == "git" && len(args) >= 1 && args[0] == "diff" {
		res.Output = r.diffOutput
	}
	return res
}

// orderingSweepRunner wraps sweepRunner to observe whether archivePathToCheck
// already exists on disk at the moment a `git branch -D` command is issued —
// used to assert archiveAndDeleteSuperpowersBranch writes the archive patch
// before force-deleting the branch.
type orderingSweepRunner struct {
	sweepRunner
	archivePathToCheck     string
	archiveExistedAtDelete bool
}

func (r *orderingSweepRunner) Run(ctx context.Context, dir string, name string, args ...string) CommandResult {
	if name == "git" && len(args) >= 2 && args[0] == "branch" && args[1] == "-D" {
		if _, err := os.Stat(r.archivePathToCheck); err == nil {
			r.archiveExistedAtDelete = true
		}
	}
	return r.sweepRunner.Run(ctx, dir, name, args...)
}

func (r *sweepRunner) joined() string { return strings.Join(r.calls, "\n") }

// withSweepBase points superpowersWorktreeBase at a temp dir and returns the
// stale/fresh/keep/nonSuperpowers paths it created inside it.
func withSweepBase(t *testing.T) (stale, fresh, keep, other string) {
	t.Helper()
	base := t.TempDir()
	stale = filepath.Join(base, "superpowers-old-1234-stale-task")
	fresh = filepath.Join(base, "superpowers-new-5678-fresh-task")
	keep = filepath.Join(base, "superpowers-cur-9abc-current-run")
	other = filepath.Join(base, "unrelated-dir")
	for _, d := range []string{stale, fresh, keep, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	for _, d := range []string{stale, keep, other} {
		if err := os.Chtimes(d, old, old); err != nil {
			t.Fatal(err)
		}
	}
	orig := superpowersWorktreeBase
	superpowersWorktreeBase = base
	t.Cleanup(func() { superpowersWorktreeBase = orig })
	return stale, fresh, keep, other
}

func TestSweepStaleSuperpowersWorktreesRemovesOnlyStaleUnkeptDirs(t *testing.T) {
	stale, fresh, keep, other := withSweepBase(t)
	runner := &sweepRunner{worktreeList: strings.Join([]string{
		"worktree /tmp/repo",
		"branch refs/heads/master",
		"",
		"worktree " + stale,
		"branch refs/heads/superpowers/old-1234",
		"",
	}, "\n")}

	removed := sweepStaleSuperpowersWorktrees(context.Background(), runner, "/tmp/repo", keep, staleSuperpowersWorktreeMaxAge)

	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("expected exactly the stale worktree removed, got %v", removed)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git worktree remove --force "+stale) {
		t.Fatalf("expected worktree remove for stale dir; calls:\n%s", joined)
	}
	if !strings.Contains(joined, "git branch -d superpowers/old-1234") {
		t.Fatalf("expected merged-branch delete for stale worktree; calls:\n%s", joined)
	}
	for label, path := range map[string]string{"fresh": fresh, "keep": keep, "other": other} {
		if strings.Contains(joined, "remove --force "+path) {
			t.Fatalf("%s dir must not be swept; calls:\n%s", label, joined)
		}
	}
}

func TestSweepStaleSuperpowersWorktreesFallsBackToPruneAndRemoveAll(t *testing.T) {
	stale, _, keep, _ := withSweepBase(t)
	runner := &sweepRunner{failOn: "worktree remove"}

	removed := sweepStaleSuperpowersWorktrees(context.Background(), runner, "/tmp/repo", keep, staleSuperpowersWorktreeMaxAge)

	if len(removed) != 1 || removed[0] != stale {
		t.Fatalf("expected stale worktree removed via fallback, got %v", removed)
	}
	if !strings.Contains(runner.joined(), "git worktree prune") {
		t.Fatalf("expected git worktree prune fallback; calls:\n%s", runner.joined())
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale dir deleted by fallback, stat err = %v", err)
	}
}

func TestCleanupAppliedSuperpowersWorktreeRemovesWorktreeAndMergedBranch(t *testing.T) {
	base := t.TempDir()
	orig := superpowersWorktreeBase
	superpowersWorktreeBase = base
	t.Cleanup(func() { superpowersWorktreeBase = orig })

	wt := filepath.Join(base, "superpowers-run-1-applied-task")
	run := &SuperpowersRun{
		ID: "run-1", Mode: SuperpowersModeApply, RepoDir: "/tmp/repo",
		WorktreePath: wt, WorktreeBranch: "superpowers/run-1",
	}
	runner := &sweepRunner{}
	if err := cleanupAppliedSuperpowersWorktree(context.Background(), runner, run); err != nil {
		t.Fatalf("cleanupAppliedSuperpowersWorktree returned error: %v", err)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git worktree remove "+wt+" --force") {
		t.Fatalf("expected worktree remove; calls:\n%s", joined)
	}
	if !strings.Contains(joined, "git branch -d superpowers/run-1") {
		t.Fatalf("expected merged-branch delete; calls:\n%s", joined)
	}
}

// TestReapOrphanedSuperpowersBranches: a branch attached to a worktree is kept,
// a merged orphan (no worktree) is deleted, and an unmerged orphan (git branch
// -d fails) survives for triage.
func TestReapOrphanedSuperpowersBranches(t *testing.T) {
	runner := &sweepRunner{
		worktreeList: strings.Join([]string{
			"worktree /tmp/repo",
			"branch refs/heads/master",
			"",
			"worktree /tmp/worktrees/superpowers-live",
			"branch refs/heads/superpowers/live-1",
			"",
		}, "\n"),
		branchList: strings.Join([]string{
			"superpowers/live-1",          // has a worktree -> must be kept
			"superpowers/orphan-merged",   // no worktree, merged -> delete
			"superpowers/orphan-stranded", // no worktree, unmerged -> -d fails -> keep
		}, "\n"),
		// git branch -d on the stranded branch fails (simulates unmerged).
		failOn: "branch -d superpowers/orphan-stranded",
	}
	runsDir := t.TempDir()

	reaped := reapOrphanedSuperpowersBranches(context.Background(), runner, "/tmp/repo", runsDir)

	if len(reaped) != 1 || reaped[0] != "superpowers/orphan-merged" {
		t.Fatalf("expected only the merged orphan reaped, got %v", reaped)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git branch -d superpowers/orphan-merged") {
		t.Fatalf("expected delete of merged orphan; calls:\n%s", joined)
	}
	if strings.Contains(joined, "git branch -d superpowers/live-1") {
		t.Fatalf("must not delete a branch that still has a worktree; calls:\n%s", joined)
	}
}

// TestReapOrphanedSuperpowersBranchesAppliesAgeAndAbandonmentGates: an
// unmerged orphan (git branch -d fails) is only force-reaped via
// archiveAndDeleteSuperpowersBranch once its commit is older than
// staleSuperpowersBranchMaxAge AND it is either an abandoned pending_patch
// run (recovery attempts exhausted) or missing its worktree.patch landing
// artifact. A branch that is merely young, or that still has recovery
// attempts remaining with its patch present, must never be touched — there
// is still something to recover.
func TestReapOrphanedSuperpowersBranchesAppliesAgeAndAbandonmentGates(t *testing.T) {
	runsDir := t.TempDir()
	now := time.Now()
	threeDaysAgo := now.Add(-3 * 24 * time.Hour).Format(time.RFC3339)
	tenDaysAgo := now.Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	writeRun := func(id string, applyStatus string, attempts int, writePatch bool) {
		t.Helper()
		checks := make([]VerificationCheck, attempts)
		for i := range checks {
			checks[i] = VerificationCheck{Name: pendingPatchRecoveryCheckName}
		}
		run := &SuperpowersRun{
			ID:           id,
			ArtifactDir:  filepath.Join(runsDir, id),
			ApplyStatus:  applyStatus,
			Verification: checks,
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			t.Fatal(err)
		}
		if writePatch {
			verDir := filepath.Join(runsDir, id, "verification")
			if err := os.MkdirAll(verDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(verDir, "worktree.patch"), []byte("diff"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// (a) 3 days old, abandoned AND missing its patch — would qualify on
	// abandonment/artifact grounds alone, but is too young to touch.
	writeRun("young-abandoned", "pending_patch", pendingPatchRecoveryMaxAttempts, false)
	// (b) 10 days old, abandoned (recovery attempts exhausted), patch present.
	writeRun("old-abandoned", "pending_patch", pendingPatchRecoveryMaxAttempts, true)
	// (c) 10 days old, recovery attempts remain, patch present — never touch.
	writeRun("old-remaining", "pending_patch", pendingPatchRecoveryMaxAttempts-1, true)
	// (d) 10 days old, no run.json and no patch artifact at all — nothing
	// left to recover, so the missing artifact alone is enough to reap.

	runner := &sweepRunner{
		branchList: strings.Join([]string{
			"superpowers/young-abandoned^" + threeDaysAgo,
			"superpowers/old-abandoned^" + tenDaysAgo,
			"superpowers/old-remaining^" + tenDaysAgo,
			"superpowers/old-missing^" + tenDaysAgo,
		}, "\n"),
		diffOutput: "diff --git a/foo b/foo\n+reaped change\n",
		failOn:     "branch -d",
	}

	reaped := reapOrphanedSuperpowersBranches(context.Background(), runner, "/tmp/repo", runsDir)

	wantReaped := []string{"superpowers/old-abandoned", "superpowers/old-missing"}
	if len(reaped) != len(wantReaped) {
		t.Fatalf("reaped = %v, want %v", reaped, wantReaped)
	}
	for i, b := range wantReaped {
		if reaped[i] != b {
			t.Fatalf("reaped = %v, want %v", reaped, wantReaped)
		}
	}

	joined := runner.joined()
	for _, b := range []string{"superpowers/young-abandoned", "superpowers/old-abandoned", "superpowers/old-remaining", "superpowers/old-missing"} {
		if !strings.Contains(joined, "git branch -d "+b) {
			t.Fatalf("expected git branch -d attempt for %s; calls:\n%s", b, joined)
		}
	}
	for _, b := range wantReaped {
		if !strings.Contains(joined, "git diff --binary master..."+b) {
			t.Fatalf("expected archive diff for reaped branch %s; calls:\n%s", b, joined)
		}
		if !strings.Contains(joined, "git branch -D "+b) {
			t.Fatalf("expected force delete for reaped branch %s; calls:\n%s", b, joined)
		}
		archivePath := filepath.Join(runsDir, strings.TrimPrefix(b, "superpowers/"), "verification", "reaped-branch.patch")
		if _, err := os.Stat(archivePath); err != nil {
			t.Fatalf("expected archive patch for reaped branch %s at %s, stat err = %v", b, archivePath, err)
		}
	}
	for _, b := range []string{"superpowers/young-abandoned", "superpowers/old-remaining"} {
		if strings.Contains(joined, "git diff --binary master..."+b) || strings.Contains(joined, "git branch -D "+b) {
			t.Fatalf("must not reap protected branch %s; calls:\n%s", b, joined)
		}
	}
}

func TestCleanupAppliedSuperpowersWorktreeNoOpsOnDryRun(t *testing.T) {
	run := &SuperpowersRun{
		ID: "run-2", Mode: SuperpowersModeDryRun, RepoDir: "/tmp/repo",
		WorktreePath: "/tmp/repo", WorktreeBranch: "dry-run",
	}
	runner := &sweepRunner{}
	if err := cleanupAppliedSuperpowersWorktree(context.Background(), runner, run); err != nil {
		t.Fatalf("dry-run cleanup must no-op, got error: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("dry-run cleanup must run zero commands, ran: %v", runner.calls)
	}
}

// TestSuperpowersBranchOlderThanCutoff: an empty or unparseable committer date
// is treated conservatively (never reapable on unknown age); a date past
// staleSuperpowersBranchMaxAge is old enough, a date within it is not.
func TestSuperpowersBranchOlderThanCutoff(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name          string
		committerDate string
		want          bool
	}{
		{"empty", "", false},
		{"unparseable", "not-a-date", false},
		{"eight days old", now.Add(-8 * 24 * time.Hour).Format(time.RFC3339), true},
		{"six days old", now.Add(-6 * 24 * time.Hour).Format(time.RFC3339), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := superpowersBranchOlderThanCutoff(tc.committerDate, staleSuperpowersBranchMaxAge)
			if got != tc.want {
				t.Fatalf("superpowersBranchOlderThanCutoff(%q) = %v, want %v", tc.committerDate, got, tc.want)
			}
		})
	}
}

// TestSuperpowersBranchRunAbandoned: a parked pending_patch run is abandoned
// only once its recorded pendingPatchRecoveryCheckName attempts reach
// pendingPatchRecoveryMaxAttempts; a missing run.json, a below-max attempt
// count, or a non-pending_patch status must all report not-abandoned.
func TestSuperpowersBranchRunAbandoned(t *testing.T) {
	runsDir := t.TempDir()

	writeRun := func(id string, applyStatus string, attempts int) {
		t.Helper()
		checks := make([]VerificationCheck, attempts)
		for i := range checks {
			checks[i] = VerificationCheck{Name: pendingPatchRecoveryCheckName}
		}
		run := &SuperpowersRun{
			ID:           id,
			ArtifactDir:  filepath.Join(runsDir, id),
			ApplyStatus:  applyStatus,
			Verification: checks,
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			t.Fatal(err)
		}
	}

	writeRun("below-max", "pending_patch", pendingPatchRecoveryMaxAttempts-1)
	writeRun("at-max", "pending_patch", pendingPatchRecoveryMaxAttempts)
	writeRun("not-pending", "applied", pendingPatchRecoveryMaxAttempts+3)

	if superpowersBranchRunAbandoned(runsDir, "no-such-run") {
		t.Fatalf("expected missing run.json to report not abandoned")
	}
	if superpowersBranchRunAbandoned(runsDir, "below-max") {
		t.Fatalf("expected below-max recovery attempts to report not abandoned")
	}
	if !superpowersBranchRunAbandoned(runsDir, "at-max") {
		t.Fatalf("expected at-max recovery attempts to report abandoned")
	}
	if superpowersBranchRunAbandoned(runsDir, "not-pending") {
		t.Fatalf("expected non-pending_patch status to report not abandoned")
	}
}

// TestSuperpowersBranchPatchArtifactMissing checks presence of the run's
// verification/worktree.patch artifact — the same path convention
// applySuperpowersRunToMainRepo writes to in superpowers_apply.go.
func TestSuperpowersBranchPatchArtifactMissing(t *testing.T) {
	runsDir := t.TempDir()
	present, missing := "run-present", "run-missing"

	verDir := filepath.Join(runsDir, present, "verification")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(verDir, "worktree.patch"), []byte("diff"), 0o644); err != nil {
		t.Fatal(err)
	}

	if superpowersBranchPatchArtifactMissing(runsDir, present) {
		t.Fatalf("expected present worktree.patch to report not missing")
	}
	if !superpowersBranchPatchArtifactMissing(runsDir, missing) {
		t.Fatalf("expected absent worktree.patch to report missing")
	}
}

// TestArchiveAndDeleteSuperpowersBranch: the three-dot diff against master is
// captured verbatim into the run's verification/reaped-branch.patch archive
// before the branch is force-deleted.
func TestArchiveAndDeleteSuperpowersBranch(t *testing.T) {
	runsDir := t.TempDir()
	runID := "reap-1"
	branch := "superpowers/" + runID
	diff := "diff --git a/foo b/foo\n+archived change\n"
	archivePath := filepath.Join(runsDir, runID, "verification", "reaped-branch.patch")

	runner := &orderingSweepRunner{archivePathToCheck: archivePath}
	runner.diffOutput = diff

	if err := archiveAndDeleteSuperpowersBranch(context.Background(), runner, "/tmp/repo", runsDir, branch); err != nil {
		t.Fatalf("archiveAndDeleteSuperpowersBranch returned error: %v", err)
	}

	joined := runner.joined()
	if !strings.Contains(joined, "git diff --binary master..."+branch) {
		t.Fatalf("expected three-dot diff capture; calls:\n%s", joined)
	}
	if !strings.Contains(joined, "git branch -D "+branch) {
		t.Fatalf("expected force branch delete; calls:\n%s", joined)
	}

	got, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("expected archive patch written, stat err = %v", err)
	}
	if string(got) != diff {
		t.Fatalf("archive patch = %q, want %q", got, diff)
	}
	if !runner.archiveExistedAtDelete {
		t.Fatalf("expected archive file to exist on disk before branch -D was issued")
	}
}
