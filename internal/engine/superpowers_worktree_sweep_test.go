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
	return res
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

	reaped := reapOrphanedSuperpowersBranches(context.Background(), runner, "/tmp/repo")

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
