package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// Keep implementation and verification deterministic, but run main-checkout
// commands against real Git so a destructive cleanup cannot hide in a mock.
type mainPreservationRunner struct {
	main  string
	inner *scriptedSuperpowersRunner
}

type blockedApplyRunner struct {
	inner   *applyScriptRunner
	entered chan struct{}
	release chan struct{}
}

func (r *blockedApplyRunner) Run(ctx context.Context, dir, name string, args ...string) CommandResult {
	if name == "git" && len(args) > 0 && args[0] == "status" {
		close(r.entered)
		<-r.release
	}
	return r.inner.Run(ctx, dir, name, args...)
}

func TestApplySuperpowersRun_SerializesRepositoryLandings(t *testing.T) {
	isolateSuperpowersRunsDir(t)
	main := t.TempDir()
	makeRun := func(id string) *SuperpowersRun {
		return &SuperpowersRun{ID: id, Mode: SuperpowersModeApply, RepoDir: main, WorktreePath: t.TempDir(), ArtifactDir: filepath.Join(t.TempDir(), "artifacts")}
	}
	first, second := makeRun("first"), makeRun("second")
	blocked := &blockedApplyRunner{
		inner:   &applyScriptRunner{patch: "diff --git a/a.go b/a.go\n", status: " M existing.go\n"},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() { done <- applySuperpowersRunToMainRepo(context.Background(), blocked, first) }()
	t.Cleanup(func() {
		close(blocked.release)
		if err := <-done; err == nil {
			t.Error("first landing should preserve the dirty checkout")
		}
	})
	select {
	case <-blocked.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first landing did not reach its checkout guard")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	runner := &applyScriptRunner{patch: "diff --git a/b.go b/b.go\n"}
	err := applySuperpowersRunToMainRepo(ctx, runner, second)
	if !errors.Is(err, context.DeadlineExceeded) || second.ApplyStatus != "pending_patch" {
		t.Fatalf("contending landing: err=%v, status=%q; want cancelled wait with pending patch", err, second.ApplyStatus)
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, main+" :: ") {
			t.Fatalf("second landing accessed the shared checkout before acquiring its lock: %s", call)
		}
	}
	if _, err := os.Stat(second.PatchPath); err != nil {
		t.Fatalf("contending landing lost its patch: %v", err)
	}
}

func (r *mainPreservationRunner) Run(ctx context.Context, dir, name string, args ...string) CommandResult {
	if dir == r.main {
		return (execCommandRunner{}).Run(ctx, dir, name, args...)
	}
	if name == "git" && strings.Join(args, " ") == "rev-parse HEAD" {
		return CommandResult{Err: errors.New("fixture has no snapshot base")}
	}
	if name == "bash" && len(args) == 2 && strings.Contains(args[1], "git diff --binary") {
		return CommandResult{Output: "diff --git a/candidate.go b/candidate.go\nnew file mode 100644\n--- /dev/null\n+++ b/candidate.go\n@@ -0,0 +1 @@\n+package candidate\n"}
	}
	return r.inner.Run(ctx, dir, name, args...)
}

func TestRunSuperpowersRuntime_PreservesDirtyMainCheckout(t *testing.T) {
	t.Chdir(t.TempDir())
	isolateBackoffStores(t)
	isolateSuperpowersRunsDir(t)
	main := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		res := (execCommandRunner{}).Run(context.Background(), main, "git", args...)
		if res.Err != nil {
			t.Fatalf("git %v: %v\n%s", args, res.Err, res.Output)
		}
		return res.Output
	}
	git("init", "-q")
	path := filepath.Join(main, "operator.txt")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("baseline\n")
	git("add", "operator.txt")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "core.hooksPath=/dev/null", "commit", "-qm", "baseline")
	write("staged work\n")
	git("add", "operator.txt")
	write("unstaged work\n")
	head, index := git("rev-parse", "HEAD"), git("show", ":operator.txt")

	previousRunner, previousClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner = previousRunner, previousClaude
	})
	results := make([]CommandResult, 12)
	results[0] = CommandResult{Output: "--- FAIL: TestGuard (0.00s)\nmissing guard\n", Err: errors.New("exit status 1")}
	defaultSuperpowersCommandRunner = &mainPreservationRunner{main: main, inner: &scriptedSuperpowersRunner{t: t, testResults: results}}
	defaultSuperpowersClaudeRunner = &scriptedClaudeRunner{}
	plan := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(plan, []byte(buildDeterministicImplementationPlan("preserve existing work")), 0o600); err != nil {
		t.Fatal(err)
	}
	run := &SuperpowersRun{ID: "preserve-main", Mode: SuperpowersModeApply, RepoDir: main, WorktreePath: t.TempDir(), ArtifactDir: filepath.Join(t.TempDir(), "artifacts")}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["goap_fusion_superpowers_plan_path"] = plan
	if got := runSuperpowersRuntimeFromExistingPlanAction(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 || run.ApplyStatus != "pending_patch" {
		t.Fatalf("result=%d apply=%q; expected a preserved pending patch: %s", got, run.ApplyStatus, bb.Result)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "unstaged work\n" {
		t.Errorf("main checkout work was discarded: content=%q err=%v", content, err)
	}
	if got := git("show", ":operator.txt"); got != index {
		t.Errorf("staged work was discarded: got %q, want %q", got, index)
	}
	if got := git("rev-parse", "HEAD"); got != head {
		t.Errorf("main HEAD changed: got %q, want %q", got, head)
	}
	if _, err := os.Stat(run.PatchPath); err != nil {
		t.Fatalf("verified candidate patch was not preserved: %v", err)
	}
}
