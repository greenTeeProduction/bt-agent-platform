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
type bareApplyScriptRunner struct {
	calls  []string
	patch  string
	failOn string
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
	case name == "bash" && len(args) >= 2 && args[0] == "-c":
		script := args[1]
		switch {
		case strings.Contains(script, "git diff --binary"):
			res.Output = r.patch
		case strings.Contains(script, "go test"), strings.Contains(script, "go build"), strings.Contains(script, "graphify update"):
			res.Output = "ok\n"
		case strings.Contains(script, "git diff --cached --quiet"):
			res.Err = errors.New("exit status 1") // staged changes exist
		}
	}
	return res
}

func (r *bareApplyScriptRunner) joined() string { return strings.Join(r.calls, "\n") }

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

func TestSyncSuperpowersRepoBareRepoFastForwardsMasterViaFetch(t *testing.T) {
	runner := &bareApplyScriptRunner{}
	if err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo"); err != nil {
		t.Fatalf("bare-repo sync returned error: %v", err)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git fetch origin master:master") {
		t.Fatalf("bare sync must ff-update master via fetch refspec; calls:\n%s", joined)
	}
	for _, forbidden := range []string{"git status", "git checkout", "git pull"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("bare sync must not run working-tree command %q; calls:\n%s", forbidden, joined)
		}
	}
}

func TestSyncSuperpowersRepoBareRepoSurfacesFetchFailure(t *testing.T) {
	runner := &bareApplyScriptRunner{failOn: "fetch origin master:master"}
	err := syncSuperpowersRepoForWorktree(context.Background(), runner, "/tmp/bare-repo")
	if err == nil || !strings.Contains(err.Error(), "fast-forward") {
		t.Fatalf("expected fast-forward failure to surface, got %v", err)
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
}

func TestApplySuperpowersRunBareRepoRefusedFastForwardParksPendingPatch(t *testing.T) {
	run := bareTestRun(t)
	runner := &bareApplyScriptRunner{
		patch:  "diff --git a/internal/engine/actions_superpowers.go b/x\n",
		failOn: "fetch . superpowers/run-bare:master",
	}

	err := applySuperpowersRunToMainRepo(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "pending_patch") {
		t.Fatalf("expected pending_patch error on refused fast-forward, got %v", err)
	}
	if run.ApplyStatus != "pending_patch" {
		t.Fatalf("ApplyStatus = %q, want pending_patch", run.ApplyStatus)
	}
	if strings.Contains(runner.joined(), "git push") {
		t.Fatalf("must not push after refused fast-forward; calls:\n%s", runner.joined())
	}
}
