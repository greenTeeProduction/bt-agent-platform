package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bareApplyScriptRunner fakes a BARE main repo: rev-parse --is-bare-repository
// answers true, verification/build scripts succeed, staged changes exist, and
// failOn forces a failure on any command containing it.
//
// The apply-time rebase-then-retry landing is scripted with a few extra knobs:
//   - headBranch  → what `git symbolic-ref --short HEAD` reports (the guard
//     that refuses to rebase a detached/wrong HEAD compares this to the run
//     branch);
//   - ffFailUntilRebase → the non-forced `git fetch . <branch>:master` refuses
//     until a `git rebase master` has advanced the branch, modelling a master
//     that moved mid-cycle then a clean re-apply that lands on retry;
//   - failRebase  → forces `git rebase master` (but NOT `git rebase --abort`)
//     to fail, modelling a rebase conflict;
//   - failReverify → the re-verify of the REBASED tree (build/test after a
//     rebase has run) fails, exercising the "never land an unverified tree"
//     guard.
type bareApplyScriptRunner struct {
	calls             []string
	patch             string
	failOn            string
	headBranch        string
	ffFailUntilRebase bool
	failRebase        bool
	failReverify      bool
	sawRebase         bool // set once `git rebase master` has succeeded
}

func (r *bareApplyScriptRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	if r.failOn != "" && strings.Contains(cmd, r.failOn) {
		res.Err = errors.New("forced failure")
		res.Output = "forced failure output"
		return res
	}
	switch {
	case name == "git" && len(args) >= 2 && args[0] == "rev-parse" && args[1] == "--is-bare-repository":
		res.Output = "true\n"
	case name == "git" && len(args) >= 1 && args[0] == "rev-parse":
		res.Output = "abc1234\n"
	case name == "git" && len(args) >= 1 && args[0] == "symbolic-ref":
		res.Output = r.headBranch + "\n"
	case name == "git" && len(args) >= 2 && args[0] == "fetch" && args[1] == ".":
		// The master-writing non-forced ff of master to the run branch.
		if r.ffFailUntilRebase && !r.sawRebase {
			res.Err = errors.New("fast-forward refused")
			res.Output = "! [rejected]        master -> master (non-fast-forward)"
		}
	case name == "git" && len(args) >= 2 && args[0] == "rebase" && args[1] == "master":
		if r.failRebase {
			res.Err = errors.New("rebase conflict")
			res.Output = "CONFLICT (content): Merge conflict"
		} else {
			r.sawRebase = true
		}
	case name == "bash" && len(args) >= 2 && args[0] == "-c":
		script := args[1]
		switch {
		case strings.Contains(script, "git diff --binary"):
			res.Output = r.patch
		case strings.Contains(script, "go test"), strings.Contains(script, "go build"), strings.Contains(script, "graphify update"):
			if r.failReverify && r.sawRebase {
				res.Err = errors.New("forced verify failure")
				res.Output = "forced verify failure output"
			} else {
				res.Output = "ok\n"
			}
		case strings.Contains(script, "git diff --cached --quiet"):
			res.Err = errors.New("exit status 1") // staged changes exist
		}
	}
	return res
}

func (r *bareApplyScriptRunner) joined() string { return strings.Join(r.calls, "\n") }

// assertNoForcedMasterWrite fails if the command log contains any command that
// could FORCE-move master, bypassing the non-forced `git fetch . <branch>:master`
// that must remain the ONLY writer of the bare repo's master ref. Mirrored into
// every rebase-then-retry test — the whole point of the fix is that a moved
// master is re-applied onto, never clobbered.
func assertNoForcedMasterWrite(t *testing.T, joined, branch string) {
	t.Helper()
	forbidden := []string{
		"update-ref refs/heads/master",
		"branch -f master",
		"branch --force master",
		"push --force",
		"push -f ",
		"+" + branch + ":master", // forced ff refspec
		"+master",                // any +master forced refspec
	}
	for _, f := range forbidden {
		if strings.Contains(joined, f) {
			t.Fatalf("forbidden master-forcing command containing %q; calls:\n%s", f, joined)
		}
	}
}

func bareTestRun(t *testing.T) *SuperpowersRun {
	t.Helper()
	return &SuperpowersRun{
		ID:             "run-bare",
		Task:           "implement guard",
		Mode:           SuperpowersModeApply,
		RepoDir:        filepath.Join(t.TempDir(), "repo"),
		WorktreePath:   filepath.Join(t.TempDir(), "worktree"),
		WorktreeBranch: "superpowers/run-bare",
		ArtifactDir:    filepath.Join(t.TempDir(), "artifacts"),
		ChangedFiles:   []string{"internal/engine/actions_superpowers.go"},
	}
}

func TestSyncSuperpowersRepoBareRepoFetchesOriginTipBare(t *testing.T) {
	runner := &bareApplyScriptRunner{}
	if err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo"); err != nil {
		t.Fatalf("bare-repo sync returned error: %v", err)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git fetch origin master") {
		t.Fatalf("bare sync must fetch origin's master tip; calls:\n%s", joined)
	}
	// The ff refspec fetch rejects when local master is ahead of origin,
	// stalling the loop on its own unpushed commits — never reintroduce it.
	if strings.Contains(joined, "master:master") {
		t.Fatalf("bare sync must not use the ff refspec fetch; calls:\n%s", joined)
	}
	for _, forbidden := range []string{"git status", "git checkout", "git pull"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("bare sync must not run working-tree command %q; calls:\n%s", forbidden, joined)
		}
	}
}

func TestSyncSuperpowersRepoBareRepoSurfacesFetchFailure(t *testing.T) {
	runner := &bareApplyScriptRunner{failOn: "fetch origin master"}
	err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo")
	if err == nil || !strings.Contains(err.Error(), "could not fetch origin master") {
		t.Fatalf("expected fetch failure to surface, got %v", err)
	}
}

func TestApplySuperpowersRunBareRepoVerifiesCommitsInWorktreeThenFastForwardsMaster(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{patch: "diff --git a/internal/engine/actions_superpowers.go b/x\n"}

	if err := applySuperpowersRunToMainRepo(context.Background(), runner, run); err != nil {
		t.Fatalf("bare apply returned error: %v", err)
	}
	if run.ApplyStatus != "committed" {
		t.Fatalf("ApplyStatus = %q, want committed", run.ApplyStatus)
	}
	if run.AppliedCommit != "abc1234" {
		t.Fatalf("AppliedCommit = %q, want abc1234", run.AppliedCommit)
	}
	joined := runner.joined()
	if strings.Contains(joined, "git apply") {
		t.Fatalf("bare apply must not patch a nonexistent main working tree; calls:\n%s", joined)
	}
	// A clean first fast-forward must NEVER enter the rebase-then-retry path.
	if strings.Contains(joined, "git rebase") {
		t.Fatalf("a clean first ff must not rebase; calls:\n%s", joined)
	}
	// Verification and the run commit happen in the worktree checkout.
	for _, want := range []string{"go test", "go build", "graphify update .", "git add -A", "git commit -m"} {
		found := false
		for _, call := range runner.calls {
			if strings.Contains(call, want) && strings.HasPrefix(call, run.WorktreePath+" :: ") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected %q to run in worktree %s; calls:\n%s", want, run.WorktreePath, joined)
		}
	}
	// The master ref update and push happen in the bare main repo.
	for _, want := range []string{"git fetch . superpowers/run-bare:master", "git push origin master"} {
		if !strings.Contains(joined, run.RepoDir+" :: "+want) {
			t.Fatalf("expected %q to run in bare repo %s; calls:\n%s", want, run.RepoDir, joined)
		}
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}

func TestApplySuperpowersRunBareRepoRefusedFastForwardParksPendingPatch(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:      "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		headBranch: "superpowers/run-bare",
		// Under the new behavior a refused ff triggers a rebase attempt; fail
		// that too so the run still parks. The rebase --abort restores the tip.
		failOn:     "fetch . superpowers/run-bare:master",
		failRebase: true,
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "pending_patch") {
		t.Fatalf("expected pending_patch error on refused fast-forward, got %v", err)
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	joined := runner.joined()
	if !strings.Contains(joined, run.WorktreePath+" :: git rebase --abort") {
		t.Fatalf("a failed rebase must abort before parking; calls:\n%s", joined)
	}
	if strings.Contains(joined, "git push") {
		t.Fatalf("must not push after refused fast-forward; calls:\n%s", joined)
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}

func TestApplySuperpowersRunBareRepoRefusedFfRebasesThenLands(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:             "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		headBranch:        "superpowers/run-bare",
		ffFailUntilRebase: true, // first ff refused, second (post-rebase) lands
	}

	if err := applySuperpowersRunToMainRepo(context.Background(), runner, run); err != nil {
		t.Fatalf("bare apply returned error: %v", err)
	}
	if run.ApplyStatus != "committed" {
		t.Fatalf("ApplyStatus = %q, want committed", run.ApplyStatus)
	}
	if run.AppliedCommit != "abc1234" {
		t.Fatalf("AppliedCommit = %q, want abc1234", run.AppliedCommit)
	}
	joined := runner.joined()
	if !strings.Contains(joined, run.WorktreePath+" :: git rebase master") {
		t.Fatalf("expected `git rebase master` to run in the worktree; calls:\n%s", joined)
	}
	if n := strings.Count(joined, "git fetch . superpowers/run-bare:master"); n != 2 {
		t.Fatalf("expected ff to run twice (refused, then post-rebase land), got %d; calls:\n%s", n, joined)
	}
	if !strings.Contains(joined, run.RepoDir+" :: git push origin master") {
		t.Fatalf("expected push after a successful land; calls:\n%s", joined)
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}

func TestApplySuperpowersRunBareRepoRebaseConflictAbortsAndParks(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:      "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		headBranch: "superpowers/run-bare",
		failOn:     "fetch . superpowers/run-bare:master",
		failRebase: true,
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "pending_patch") {
		t.Fatalf("expected pending_patch on rebase conflict, got %v", err)
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	joined := runner.joined()
	if !strings.Contains(joined, run.WorktreePath+" :: git rebase --abort") {
		t.Fatalf("a rebase conflict must abort to restore the branch tip; calls:\n%s", joined)
	}
	if strings.Contains(joined, "git push") {
		t.Fatalf("must not push after a parked rebase; calls:\n%s", joined)
	}
	// After the abort the ff must NOT be retried: exactly one ff attempt.
	if n := strings.Count(joined, "git fetch . superpowers/run-bare:master"); n != 1 {
		t.Fatalf("expected exactly one ff attempt before parking, got %d; calls:\n%s", n, joined)
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}

func TestApplySuperpowersRunBareRepoRebaseSucceedsButReverifyFails(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:        "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		headBranch:   "superpowers/run-bare",
		failOn:       "fetch . superpowers/run-bare:master",
		failReverify: true, // the rebased tree fails re-verify
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	// The re-verify failure surfaces the verification error (the diagnostic),
	// while run.ApplyStatus is driven to the terminal safe state — the same
	// contract the pre-ff verify path already upholds.
	if err == nil {
		t.Fatalf("expected an error when the rebased tree fails re-verify, got nil")
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	joined := runner.joined()
	if !strings.Contains(joined, run.WorktreePath+" :: git rebase master") {
		t.Fatalf("expected a rebase before the re-verify; calls:\n%s", joined)
	}
	if strings.Contains(joined, "git push") {
		t.Fatalf("must not push when the rebased tree is unverified; calls:\n%s", joined)
	}
	// The ff must never land the unverified tree: exactly one (refused) attempt.
	if n := strings.Count(joined, "git fetch . superpowers/run-bare:master"); n != 1 {
		t.Fatalf("expected exactly one ff attempt (no land on unverified tree), got %d; calls:\n%s", n, joined)
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}

func TestApplySuperpowersRunBareRepoDetachedHeadDoesNotRebase(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:      "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		headBranch: "some-other-branch", // symbolic-ref != run branch
		failOn:     "fetch . superpowers/run-bare:master",
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "pending_patch") {
		t.Fatalf("expected pending_patch when HEAD is not on the run branch, got %v", err)
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	joined := runner.joined()
	if strings.Contains(joined, "git rebase master") {
		t.Fatalf("must NOT rebase a detached/wrong HEAD (would move the wrong ref); calls:\n%s", joined)
	}
	if strings.Contains(joined, "git push") {
		t.Fatalf("must not push after a guarded park; calls:\n%s", joined)
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}

func TestApplySuperpowersRunBareRepoFfRebaseAttemptsBounded(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:      "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		headBranch: "superpowers/run-bare",
		failOn:     "fetch . superpowers/run-bare:master", // ff ALWAYS refused
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "pending_patch") {
		t.Fatalf("expected pending_patch after attempts exhausted, got %v", err)
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	joined := runner.joined()
	if got, want := strings.Count(joined, run.WorktreePath+" :: git rebase master"), ffRebaseMaxAttempts(); got != want {
		t.Fatalf("rebase attempts = %d, want ffRebaseMaxAttempts()=%d (bounded, no infinite loop); calls:\n%s", got, want, joined)
	}
	assertNoForcedMasterWrite(t, joined, run.WorktreeBranch)
}
