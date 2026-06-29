package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type worktreeSyncRunner struct {
	calls  []string
	status string
	failOn string
}

func (r *worktreeSyncRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	if r.failOn != "" && strings.Contains(cmd, r.failOn) {
		res.Err = errors.New("forced failure")
		res.Output = "forced failure output"
		return res
	}
	if name == "git" && len(args) >= 1 && args[0] == "status" {
		res.Output = r.status
	}
	return res
}

func TestCreateSuperpowersWorktreeSyncsMainBeforeWorktreeAdd(t *testing.T) {
	run := &SuperpowersRun{ID: "sync-test", Task: "implement a small guard", Mode: SuperpowersModeApply, RepoDir: "/tmp/repo"}
	runner := &worktreeSyncRunner{}
	if err := createSuperpowersWorktree(context.Background(), runner, run); err != nil {
		t.Fatalf("createSuperpowersWorktree returned error: %v", err)
	}
	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"git status --short --untracked-files=all",
		"git checkout -- graphify-out/",
		"git checkout master",
		"git fetch origin",
		"git pull origin master --ff-only",
		"git worktree add",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command containing %q; calls:\n%s", want, joined)
		}
	}
	if strings.Index(joined, "git pull origin master --ff-only") > strings.Index(joined, "git worktree add") {
		t.Fatalf("git pull must happen before worktree add; calls:\n%s", joined)
	}
}

func TestCreateSuperpowersWorktreeBlocksDirtyMainRepoBeforeSync(t *testing.T) {
	run := &SuperpowersRun{ID: "dirty-test", Task: "implement a small guard", Mode: SuperpowersModeApply, RepoDir: "/tmp/repo"}
	runner := &worktreeSyncRunner{status: " M internal/engine/actions.go\n"}
	err := createSuperpowersWorktree(context.Background(), runner, run)
	if err == nil || !strings.Contains(err.Error(), "blocking dirty files") {
		t.Fatalf("expected blocking dirty files error, got %v", err)
	}
	joined := strings.Join(runner.calls, "\n")
	if strings.Contains(joined, "git fetch origin") || strings.Contains(joined, "git worktree add") {
		t.Fatalf("dirty repo must block before fetch/worktree add; calls:\n%s", joined)
	}
}
