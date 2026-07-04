package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// partialLandingRunner scripts a multi-task batch: git commands succeed and
// are recorded, rev-parse yields a stable base SHA, and each `go test`
// invocation consumes the next scripted result (RED runs must fail, GREEN
// runs pass or fail per script).
type partialLandingRunner struct {
	t           *testing.T
	calls       []string
	testResults []CommandResult
	testCalls   int
}

func (r *partialLandingRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch {
	case name == "git" && len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
		res.Output = "basesha\n"
	case name == "git" && len(args) >= 1 && args[0] == "status":
		res.Output = ""
	case name == "bash" && len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "go test"):
		if r.testCalls >= len(r.testResults) {
			r.t.Fatalf("unexpected test command %q", args[1])
		}
		out := r.testResults[r.testCalls]
		r.testCalls++
		out.Command = cmd
		out.Dir = dir
		out.Duration = time.Millisecond
		return out
	}
	return res
}

func (r *partialLandingRunner) joined() string { return strings.Join(r.calls, "\n") }

func partialLandingRun(t *testing.T, taskCount int) *SuperpowersRun {
	t.Helper()
	run := &SuperpowersRun{
		ID:           "run-partial",
		Task:         "improve",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	for i := 1; i <= taskCount; i++ {
		run.Tasks = append(run.Tasks, SuperpowersTask{
			Index:     i,
			Title:     "task " + strings.Repeat("i", i),
			Objective: "objective",
			Files:     []string{"internal/engine/x.go"},
			Tests:     []string{"go test ./internal/engine -count=1"},
		})
	}
	return run
}

func redFail() CommandResult {
	return CommandResult{Output: "--- FAIL: TestX\n", Err: errors.New("exit status 1")}
}
func greenPass() CommandResult { return CommandResult{Output: "ok\n"} }
func greenFail() CommandResult {
	return CommandResult{Output: "--- FAIL: TestX still failing\n", Err: errors.New("exit status 1")}
}

// A later task's failure must not discard the completed tasks' verified
// work: the failed task's edits are dropped, remaining tasks skipped, the
// snapshots unwrapped, and the batch returns SUCCESS so verify/apply land
// the completed work. Regression: cycle 20260704T023012 lost tasks 1-2
// because task 3 failed.
func TestBatchPartialLandingKeepsCompletedTasks(t *testing.T) {
	runner := &partialLandingRunner{t: t, testResults: []CommandResult{
		redFail(), greenPass(), // task 1: done
		redFail(), greenFail(), // task 2: GREEN verification fails
	}}
	run := partialLandingRun(t, 3)
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	if err := executeSuperpowersTaskBatch(context.Background(), executor, run); err != nil {
		t.Fatalf("partial landing must not fail the batch: %v", err)
	}
	if run.Tasks[0].Status != "done" || run.Tasks[1].Status != "failed" || run.Tasks[2].Status != "skipped" {
		t.Fatalf("statuses = %s/%s/%s, want done/failed/skipped",
			run.Tasks[0].Status, run.Tasks[1].Status, run.Tasks[2].Status)
	}
	if !strings.Contains(run.PartialFailure, "task 2") || !strings.Contains(run.PartialFailure, "carried forward") {
		t.Fatalf("PartialFailure must name the carried-forward task: %q", run.PartialFailure)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git commit --no-verify -m superpowers snapshot: task 1") {
		t.Fatalf("task 1 must be snapshot-committed; calls:\n%s", joined)
	}
	for _, want := range []string{"git reset --hard HEAD", "git clean -fd", "git reset basesha"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("failure cleanup must run %q; calls:\n%s", want, joined)
		}
	}
}

// A first-task failure keeps the all-or-nothing contract: nothing landed,
// the batch fails.
func TestBatchFirstTaskFailureStaysAllOrNothing(t *testing.T) {
	runner := &partialLandingRunner{t: t, testResults: []CommandResult{
		redFail(), greenFail(), // task 1 fails
	}}
	run := partialLandingRun(t, 2)
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	if err := executeSuperpowersTaskBatch(context.Background(), executor, run); err == nil {
		t.Fatal("first-task failure must fail the batch")
	}
	if run.PartialFailure != "" {
		t.Fatalf("no partial landing without completed tasks: %q", run.PartialFailure)
	}
	if strings.Contains(runner.joined(), "git reset --hard") {
		t.Fatalf("no cleanup reset without completed tasks; calls:\n%s", runner.joined())
	}
}

// A fully successful batch unwraps its snapshots so the worktree holds all
// changes uncommitted — the exact shape verify/apply expected before this
// mode existed.
func TestBatchFullSuccessUnwrapsSnapshots(t *testing.T) {
	runner := &partialLandingRunner{t: t, testResults: []CommandResult{
		redFail(), greenPass(),
		redFail(), greenPass(),
	}}
	run := partialLandingRun(t, 2)
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	if err := executeSuperpowersTaskBatch(context.Background(), executor, run); err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	joined := runner.joined()
	if strings.Count(joined, "superpowers snapshot: task") != 2 {
		t.Fatalf("each task must snapshot; calls:\n%s", joined)
	}
	if !strings.HasSuffix(strings.TrimSpace(joined), "git reset basesha") {
		t.Fatalf("batch must end by unwrapping snapshots to base; calls:\n%s", joined)
	}
	if run.PartialFailure != "" {
		t.Fatalf("full success must not record a partial failure: %q", run.PartialFailure)
	}
}

// Dry-run and main-repo modes must never snapshot-commit (no worktree to
// protect) and keep all-or-nothing semantics.
func TestBatchNoSnapshotsOutsideWorktreeApplyMode(t *testing.T) {
	runner := &partialLandingRunner{t: t, testResults: []CommandResult{
		redFail(), greenPass(),
		redFail(), greenFail(),
	}}
	run := partialLandingRun(t, 2)
	run.WorktreePath = run.RepoDir // main-repo mode
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	if err := executeSuperpowersTaskBatch(context.Background(), executor, run); err == nil {
		t.Fatal("without snapshots a task failure must fail the batch")
	}
	if strings.Contains(runner.joined(), "superpowers snapshot") {
		t.Fatalf("must not snapshot-commit in the main repo; calls:\n%s", runner.joined())
	}
}
