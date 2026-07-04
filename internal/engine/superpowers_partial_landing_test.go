package engine

import (
	"context"
	"errors"
	"fmt"
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
	t              *testing.T
	calls          []string
	testResults    []CommandResult
	testCalls      int
	failCommitTask int    // when >0, the snapshot commit for this task index fails
	failCleanup    string // when non-empty, any git command whose args contain this substring fails
}

func (r *partialLandingRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.calls = append(r.calls, cmd)
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch {
	case name == "git" && r.failCleanup != "" && strings.Contains(strings.Join(args, " "), r.failCleanup):
		res.Err = errors.New("cleanup command failed")
		return res
	case name == "git" && len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD":
		res.Output = "basesha\n"
	case name == "git" && len(args) >= 1 && args[0] == "status":
		res.Output = ""
	case name == "git" && len(args) >= 1 && args[0] == "commit" && r.failCommitTask > 0 &&
		strings.Contains(cmd, fmt.Sprintf("superpowers snapshot: task %d", r.failCommitTask)):
		res.Err = errors.New("commit failed")
		return res
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

// If the partial-landing cleanup itself fails, the failed task's broken edits
// could survive the mixed `git reset base` and land mixed into the completed
// work as a bogus "successful" partial batch. The three recovery commands
// (`git reset --hard HEAD`, `git clean -fd`, `git reset base`) must have their
// results checked: if any fails, the batch must abort to all-or-nothing by
// returning the original task error instead of reporting nil success.
func TestBatchPartialLandingCleanupFailureAbortsAllOrNothing(t *testing.T) {
	runner := &partialLandingRunner{t: t, failCleanup: "reset --hard HEAD", testResults: []CommandResult{
		redFail(), greenPass(), // task 1: done, snapshot committed
		redFail(), greenFail(), // task 2: GREEN verification fails -> triggers cleanup
	}}
	run := partialLandingRun(t, 3)
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	err := executeSuperpowersTaskBatch(context.Background(), executor, run)
	if err == nil {
		t.Fatal("cleanup failure must abort the batch (all-or-nothing), not report nil success")
	}
	// The original task-2 execution error must be surfaced, not swallowed.
	if !strings.Contains(err.Error(), "task GREEN verification failed") {
		t.Fatalf("aborted batch must return the original task error; got: %v", err)
	}
	// A failed cleanup means the worktree is NOT in the clean partial-landing
	// shape, so no partial-success must be recorded.
	if run.PartialFailure != "" {
		t.Fatalf("failed cleanup must not record a partial-landing success: %q", run.PartialFailure)
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

// A mid-batch snapshot-commit failure degrades to all-or-nothing for the
// REST of the batch, but the tasks that were ALREADY snapshot-committed
// before the degrade must still be unwrapped. The final `git reset base`
// must run whenever a snapshot commit was ever created (base != ""), not
// only while the live `snapshots` flag is still true. Otherwise task 1's
// verified work stays as a commit above base and the git-diff apply stage
// silently drops it while the batch reports success.
// Regression: memory superpowers-partial-landing-snapshot-degrade-loses-work.
func TestBatchMidBatchSnapshotDegradeStillUnwraps(t *testing.T) {
	runner := &partialLandingRunner{t: t, failCommitTask: 2, testResults: []CommandResult{
		redFail(), greenPass(), // task 1: done, snapshot commit succeeds
		redFail(), greenPass(), // task 2: done, but its snapshot commit FAILS -> degrade
		redFail(), greenPass(), // task 3: done, snapshots already disabled
	}}
	run := partialLandingRun(t, 3)
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	if err := executeSuperpowersTaskBatch(context.Background(), executor, run); err != nil {
		t.Fatalf("all tasks completed; batch must succeed: %v", err)
	}
	joined := runner.joined()
	// Task 1 was committed above base before the degrade.
	if !strings.Contains(joined, "git commit --no-verify -m superpowers snapshot: task 1") {
		t.Fatalf("task 1 must be snapshot-committed before the degrade; calls:\n%s", joined)
	}
	// The batch must still unwrap so task 1's committed work returns to the
	// working tree for the git-diff apply stage — this is the behavior the
	// live-flag guard drops.
	if !strings.Contains(joined, "git reset basesha") {
		t.Fatalf("mid-batch degrade must still unwrap committed snapshots to base; calls:\n%s", joined)
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
