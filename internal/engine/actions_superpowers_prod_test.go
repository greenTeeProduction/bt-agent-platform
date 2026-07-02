package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// TestSuperpowersTaskRedAction_PopulatesPerTaskArtifactDir proves the
// SuperpowersTaskRed action is self-contained: driving two different tasks
// through it (as ForEachTask would, one BT tick per task index) must leave
// each task with its own distinct, non-empty ArtifactDir instead of the two
// tasks colliding on an empty path. Before the fix, currentSuperpowersForEachTask
// never populated task.ArtifactDir, so both tasks would share the same
// (empty) artifact directory.
func TestSuperpowersTaskRedAction_PopulatesPerTaskArtifactDir(t *testing.T) {
	// Guard against relative-path writes escaping into the repo if
	// ArtifactDir is ever empty (pre-fix behavior) — run from a scratch dir.
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	runner := &scriptedSuperpowersRunner{t: t}
	claude := &scriptedClaudeRunner{}
	defaultSuperpowersCommandRunner = runner
	defaultSuperpowersClaudeRunner = claude

	run := &SuperpowersRun{
		ID:           "run-red-artifact",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "First task", Tests: []string{"true"}},
			{Index: 2, Title: "Second task", Tests: []string{"true"}},
		},
	}

	act := GetAction("SuperpowersTaskRed")
	if act == nil {
		t.Fatal("SuperpowersTaskRed not registered")
	}

	for i := range run.Tasks {
		bb := newTestBlackboard()
		setSuperpowersRun(bb, run)
		bb.ChainState["superpowers_task_index"] = i
		if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
			t.Fatalf("task %d: SuperpowersTaskRed result = %d, want SUCCESS; bb.Result=%s", i, result, bb.Result)
		}
	}

	if run.Tasks[0].ArtifactDir == "" {
		t.Fatalf("task 0 ArtifactDir was not populated")
	}
	if run.Tasks[1].ArtifactDir == "" {
		t.Fatalf("task 1 ArtifactDir was not populated")
	}
	if run.Tasks[0].ArtifactDir == run.Tasks[1].ArtifactDir {
		t.Fatalf("expected distinct per-task ArtifactDir, both tasks got %q", run.Tasks[0].ArtifactDir)
	}
}

// TestSuperpowersTaskVerifyRed_NoTestsFailsWithoutPanic proves that running
// SuperpowersTaskVerifyRed against a task with no Tests degrades to a clear
// FAILURE instead of panicking on the unguarded task.Tests[0] index. This
// mirrors ensureSuperpowersTaskSetup / ExecuteTask's own semantics for a
// no-test task: FAILURE with an explanatory message, not a silent skip.
func TestSuperpowersTaskVerifyRed_NoTestsFailsWithoutPanic(t *testing.T) {
	t.Chdir(t.TempDir())

	run := &SuperpowersRun{
		ID:           "run-notests",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "No tests task", Files: []string{"internal/engine/foo.go"}},
		},
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["superpowers_task_index"] = 0

	act := GetAction("SuperpowersTaskVerifyRed")
	if act == nil {
		t.Fatal("SuperpowersTaskVerifyRed not registered")
	}

	var result int
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SuperpowersTaskVerifyRed panicked on a task with no test commands: %v", r)
			}
		}()
		result = act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	}()

	if result != -1 {
		t.Fatalf("result = %d, want -1 (FAILURE) for a task with no test commands", result)
	}
	if !strings.Contains(bb.Result, "no test command") {
		t.Fatalf("bb.Result = %q, want a failure message mentioning the missing test command", bb.Result)
	}
}

// commitScopeFakeRunner records every command it is asked to run so the test
// can assert on the exact `git add` invocation SuperpowersTaskCommit issues.
type commitScopeFakeRunner struct {
	calls []string
}

func (r *commitScopeFakeRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, dir+" :: "+cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch {
	case name == "bash" && len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "git diff --cached --quiet"):
		// Report staged changes present so the action proceeds to commit.
		res.Err = errors.New("exit status 1")
		return res
	default:
		return res
	}
}

// TestSuperpowersTaskCommitAction_ExcludesGeneratedPaths proves the per-task
// commit scopes `git add -A` away from generated Superpowers/graphify
// artifacts (task evidence dirs, graphify-out/, docs/superpowers/**),
// mirroring the exclusion pathspecs commitAppliedSuperpowersRun already uses
// for the whole-run apply commit. Before the fix, the action ran a bare
// `git add -A` with no pathspec at all.
func TestSuperpowersTaskCommitAction_ExcludesGeneratedPaths(t *testing.T) {
	t.Chdir(t.TempDir())

	prevRunner := defaultSuperpowersCommandRunner
	t.Cleanup(func() { defaultSuperpowersCommandRunner = prevRunner })
	runner := &commitScopeFakeRunner{}
	defaultSuperpowersCommandRunner = runner

	run := &SuperpowersRun{
		ID:           "run-commit-scope",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "Scoped commit task", Tests: []string{"true"}},
		},
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["superpowers_task_index"] = 0

	act := GetAction("SuperpowersTaskCommit")
	if act == nil {
		t.Fatal("SuperpowersTaskCommit not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("SuperpowersTaskCommit result = %d, want SUCCESS; bb.Result=%s", result, bb.Result)
	}

	var addCall string
	for _, c := range runner.calls {
		if strings.Contains(c, "git add") {
			addCall = c
			break
		}
	}
	if addCall == "" {
		t.Fatalf("expected a git add call, calls=%v", runner.calls)
	}
	for _, want := range []string{"graphify-out", "docs/superpowers/runs", "docs/superpowers/plans"} {
		if !strings.Contains(addCall, want) {
			t.Fatalf("git add call missing exclusion for %q: %s", want, addCall)
		}
	}

	foundCommit := false
	for _, c := range runner.calls {
		if strings.Contains(c, "git commit") {
			foundCommit = true
		}
	}
	if !foundCommit {
		t.Fatalf("expected a git commit call once changes are staged, calls=%v", runner.calls)
	}
}
