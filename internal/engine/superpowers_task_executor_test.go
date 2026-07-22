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

type scriptedSuperpowersRunner struct {
	t           *testing.T
	events      *[]string
	testResults []CommandResult
	testCalls   int
	statusCalls int
}

func (r *scriptedSuperpowersRunner) Run(_ context.Context, dir string, name string, args ...string) CommandResult {
	cmd := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if r.events != nil {
		*r.events = append(*r.events, "cmd:"+cmd)
	}
	res := CommandResult{Command: cmd, Dir: dir, Duration: time.Millisecond}
	switch name {
	case "git":
		if len(args) >= 1 && args[0] == "status" {
			r.statusCalls++
			switch r.statusCalls {
			case 1:
				res.Output = ""
			case 2:
				res.Output = " M internal/engine/foo_test.go\n"
			default:
				res.Output = " M internal/engine/foo.go\n M internal/engine/foo_test.go\n"
			}
			return res
		}
	case "bash":
		if len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "go test") {
			if r.testCalls >= len(r.testResults) {
				r.t.Fatalf("unexpected test command %q", args[1])
			}
			out := r.testResults[r.testCalls]
			r.testCalls++
			out.Command = cmd
			out.Dir = dir
			if out.Duration == 0 {
				out.Duration = time.Millisecond
			}
			return out
		}
	}
	return res
}

type scriptedClaudeRunner struct {
	events  *[]string
	prompts []string
}

func (r *scriptedClaudeRunner) RunClaude(_ context.Context, repoDir string, prompt string) CommandResult {
	r.prompts = append(r.prompts, prompt)
	phase := "green"
	if strings.Contains(prompt, "RED phase") {
		phase = "red"
	}
	if r.events != nil {
		*r.events = append(*r.events, "claude:"+phase)
	}
	return CommandResult{
		Command:  "claude <prompt>",
		Dir:      repoDir,
		Output:   strings.ToUpper(phase) + " output",
		Duration: time.Millisecond,
	}
}

func TestSuperpowersTaskExecutorRunsRedBeforeGreen(t *testing.T) {
	events := []string{}
	runner := &scriptedSuperpowersRunner{
		t:      t,
		events: &events,
		testResults: []CommandResult{
			{Output: "--- FAIL: TestGuard (0.00s)\nmissing guard\n", Err: errors.New("exit status 1")},
			{Output: "ok  \tgithub.com/nico/go-bt-evolve/internal/engine\t0.01s\n"},
		},
	}
	claude := &scriptedClaudeRunner{events: &events}
	run := &SuperpowersRun{
		ID:           "run-1",
		Task:         "implement guard",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	task := SuperpowersTask{
		Index:     1,
		Title:     "Add guard",
		Objective: "add a guard with strict TDD",
		Files:     []string{"internal/engine/foo.go", "internal/engine/foo_test.go"},
		Tests:     []string{"/usr/local/go/bin/go test ./internal/engine -count=1 -run TestGuard -timeout 120s"},
		Body:      "Write failing test first, then implement.",
	}

	got, err := (SuperpowersTaskExecutor{Runner: runner, Claude: claude}).ExecuteTask(context.Background(), run, task)
	if err != nil {
		t.Fatalf("ExecuteTask returned error: %v", err)
	}
	if got.Status != "done" {
		t.Fatalf("task status = %q, want done", got.Status)
	}
	if len(claude.prompts) != 2 {
		t.Fatalf("Claude calls = %d, want RED and GREEN calls", len(claude.prompts))
	}
	if !strings.Contains(claude.prompts[0], "RED phase") {
		t.Fatalf("first Claude prompt was not RED phase:\n%s", claude.prompts[0])
	}
	if !strings.Contains(claude.prompts[1], "GREEN/REFACTOR phase") {
		t.Fatalf("second Claude prompt was not GREEN phase:\n%s", claude.prompts[1])
	}
	joinedEvents := strings.Join(events, " -> ")
	redIdx := strings.Index(joinedEvents, "claude:red")
	cmdIdx := strings.Index(joinedEvents, "cmd:bash -c /usr/local/go/bin/go test")
	greenIdx := strings.Index(joinedEvents, "claude:green")
	if redIdx < 0 || cmdIdx < 0 || greenIdx < 0 || redIdx >= cmdIdx || cmdIdx >= greenIdx {
		t.Fatalf("expected RED Claude -> RED test -> GREEN Claude ordering, got %s", joinedEvents)
	}
	redBytes, err := readFileForTest(filepath.Join(got.ArtifactDir, "red.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(redBytes, "Status: FAIL") || !strings.Contains(redBytes, "missing guard") {
		t.Fatalf("red.txt did not capture failing RED evidence:\n%s", redBytes)
	}
	greenBytes, err := readFileForTest(filepath.Join(got.ArtifactDir, "green.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(greenBytes, "Status: PASS") {
		t.Fatalf("green.txt did not capture passing GREEN evidence:\n%s", greenBytes)
	}
}

func TestSuperpowersTaskExecutorAbortsWhenRedUnexpectedlyPasses(t *testing.T) {
	events := []string{}
	runner := &scriptedSuperpowersRunner{
		t:      t,
		events: &events,
		testResults: []CommandResult{
			{Output: "ok  \tgithub.com/nico/go-bt-evolve/internal/engine\t0.01s\n"},
		},
	}
	claude := &scriptedClaudeRunner{events: &events}
	run := &SuperpowersRun{
		ID:           "run-2",
		Task:         "implement guard",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
	}
	task := SuperpowersTask{
		Index:     1,
		Title:     "Add guard",
		Objective: "add a guard with strict TDD",
		Files:     []string{"internal/engine/foo.go", "internal/engine/foo_test.go"},
		Tests:     []string{"/usr/local/go/bin/go test ./internal/engine -count=1 -run TestGuard -timeout 120s"},
		Body:      "Write failing test first, then implement.",
	}

	_, err := (SuperpowersTaskExecutor{Runner: runner, Claude: claude}).ExecuteTask(context.Background(), run, task)
	if err == nil || !strings.Contains(err.Error(), "RED command unexpectedly passed") {
		t.Fatalf("ExecuteTask error = %v, want RED unexpectedly passed", err)
	}
	if len(claude.prompts) != 1 {
		t.Fatalf("Claude calls = %d, want only RED call", len(claude.prompts))
	}
	if strings.Contains(strings.Join(events, " -> "), "claude:green") {
		t.Fatalf("GREEN Claude ran despite RED passing: %v", events)
	}
}

func readFileForTest(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// A GREEN verification killed by the cycle deadline must surface the
// budget-exhausted marker (classified as infrastructure → the milestone
// attempt refunds), not a generic failure that charges the abandon budget;
// a live-context failure keeps the plain genuine-failure message and must
// include the underlying error for observability (a bare SIGKILL previously
// vanished because only res.Output was reported).
func TestSuperpowersTaskVerifyGreenBudgetExhaustedMarker(t *testing.T) {
	run := &SuperpowersRun{WorktreePath: t.TempDir()}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	task := &SuperpowersTask{ArtifactDir: t.TempDir(), Tests: []string{"go test ./internal/engine -short"}}
	runner := &scriptedSuperpowersRunner{t: t, testResults: []CommandResult{{Err: errors.New("signal: killed")}}}
	err := superpowersTaskVerifyGreen(expired, runner, run, task)
	if err == nil || !strings.Contains(err.Error(), "cycle budget exhausted") {
		t.Fatalf("expired ctx: want budget-exhausted marker, got %v", err)
	}
	if task.Status != "failed" {
		t.Fatalf("task status = %q, want failed", task.Status)
	}

	task2 := &SuperpowersTask{ArtifactDir: t.TempDir(), Tests: []string{"go test ./internal/engine -short"}}
	runner2 := &scriptedSuperpowersRunner{t: t, testResults: []CommandResult{{Err: errors.New("exit status 1"), Output: "--- FAIL: TestFoo"}}}
	err2 := superpowersTaskVerifyGreen(context.Background(), runner2, run, task2)
	if err2 == nil || strings.Contains(err2.Error(), "cycle budget exhausted") {
		t.Fatalf("live ctx: must stay a plain GREEN failure, got %v", err2)
	}
	if !strings.Contains(err2.Error(), "task GREEN verification failed") || !strings.Contains(err2.Error(), "exit status 1") {
		t.Fatalf("live ctx: must report the failure with the underlying error, got %v", err2)
	}
}

// killedClaudeRunner simulates a claude subprocess that died with the given
// error before producing a verdict — the exact shape a run-budget SIGKILL
// produces ("signal: killed", empty output).
type killedClaudeRunner struct{ err error }

func (r killedClaudeRunner) RunClaude(context.Context, string, string) CommandResult {
	return CommandResult{Command: "claude <prompt>", Err: r.err, Duration: time.Millisecond}
}

// A GREEN-phase claude call killed by the cycle deadline is no evidence
// against the milestone: without the budget marker the kill classifies as a
// genuine implementation failure and charges the milestone-abandon budget.
// On 2026-07-18 nine consecutive cycles (20260718T164339 … 20260718T232740)
// re-implemented the same program, were SIGKILLed mid-task-3 GREEN at exactly
// the 45-minute run budget, and wrongly blocked milestones 8e47518e:0-2.
func TestSuperpowersTaskGreenBudgetExhaustedMarker(t *testing.T) {
	run := &SuperpowersRun{WorktreePath: t.TempDir()}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	task := &SuperpowersTask{ArtifactDir: t.TempDir()}
	err := superpowersTaskGreen(expired, &scriptedSuperpowersRunner{t: t}, killedClaudeRunner{errors.New("signal: killed")}, run, task, "")
	if err == nil || !strings.Contains(err.Error(), "cycle budget exhausted") {
		t.Fatalf("expired ctx: want budget-exhausted marker, got %v", err)
	}
	if task.Status != "failed" {
		t.Fatalf("task status = %q, want failed", task.Status)
	}

	task2 := &SuperpowersTask{ArtifactDir: t.TempDir()}
	err2 := superpowersTaskGreen(context.Background(), &scriptedSuperpowersRunner{t: t}, killedClaudeRunner{errors.New("exit status 1")}, run, task2, "")
	if err2 == nil || strings.Contains(err2.Error(), "cycle budget exhausted") {
		t.Fatalf("live ctx: must stay a plain claude failure, got %v", err2)
	}
	if !strings.Contains(err2.Error(), "green-phase claude failed") {
		t.Fatalf("live ctx: must keep the legacy green failure message, got %v", err2)
	}
}

// Same contract for the RED-phase claude call: a deadline SIGKILL must carry
// the budget marker, a live-ctx claude failure must keep its legacy message.
func TestSuperpowersTaskRedBudgetExhaustedMarker(t *testing.T) {
	run := &SuperpowersRun{WorktreePath: t.TempDir()}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	task := &SuperpowersTask{ArtifactDir: t.TempDir()}
	err := superpowersTaskRed(expired, &scriptedSuperpowersRunner{t: t}, killedClaudeRunner{errors.New("signal: killed")}, run, task)
	if err == nil || !strings.Contains(err.Error(), "cycle budget exhausted") {
		t.Fatalf("expired ctx: want budget-exhausted marker, got %v", err)
	}
	if task.Status != "failed" {
		t.Fatalf("task status = %q, want failed", task.Status)
	}

	task2 := &SuperpowersTask{ArtifactDir: t.TempDir()}
	err2 := superpowersTaskRed(context.Background(), &scriptedSuperpowersRunner{t: t}, killedClaudeRunner{errors.New("exit status 1")}, run, task2)
	if err2 == nil || strings.Contains(err2.Error(), "cycle budget exhausted") {
		t.Fatalf("live ctx: must stay a plain claude failure, got %v", err2)
	}
	if !strings.Contains(err2.Error(), "red-phase claude failed") {
		t.Fatalf("live ctx: must keep the legacy red failure message, got %v", err2)
	}
}

// A RED verification command killed by the dead cycle budget looks exactly
// like the legit "RED correctly failed" outcome (non-nil Err), so without a
// ctx guard the executor marches into a doomed GREEN claude call on a dead
// context. The kill must surface as the budget marker instead.
func TestSuperpowersTaskVerifyRedBudgetExhaustedMarker(t *testing.T) {
	run := &SuperpowersRun{WorktreePath: t.TempDir()}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	task := &SuperpowersTask{ArtifactDir: t.TempDir(), Tests: []string{"go test ./internal/engine -run TestGuard"}}
	runner := &scriptedSuperpowersRunner{t: t, testResults: []CommandResult{{Err: errors.New("signal: killed")}}}
	err := superpowersTaskVerifyRed(expired, runner, run, task)
	if err == nil || !strings.Contains(err.Error(), "cycle budget exhausted") {
		t.Fatalf("expired ctx: want budget-exhausted marker, got %v", err)
	}
	if task.Status != "failed" {
		t.Fatalf("task status = %q, want failed", task.Status)
	}

	task2 := &SuperpowersTask{ArtifactDir: t.TempDir(), Tests: []string{"go test ./internal/engine -run TestGuard"}}
	runner2 := &scriptedSuperpowersRunner{t: t, testResults: []CommandResult{{Err: errors.New("exit status 1"), Output: "--- FAIL: TestGuard"}}}
	if err2 := superpowersTaskVerifyRed(context.Background(), runner2, run, task2); err2 != nil {
		t.Fatalf("live ctx: a genuinely failing RED command must stay the good path, got %v", err2)
	}
}

// A run-level verification check killed by the dead cycle budget must carry
// the budget marker too — "verification focused-tests failed: signal: killed"
// classifies as genuine and charges the abandon budget otherwise.
func TestVerifySuperpowersRunBudgetExhaustedMarker(t *testing.T) {
	runner := &autofixScriptRunner{t: t, steps: []autofixScriptStep{
		{match: "go test", res: CommandResult{Err: errors.New("signal: killed")}},
	}}
	withAutofixVerifyEnv(t, runner)
	run := &SuperpowersRun{RepoDir: t.TempDir(), ArtifactDir: t.TempDir()}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	err := VerifySuperpowersRunRuntime(expired, run)
	if err == nil || !strings.Contains(err.Error(), "cycle budget exhausted") {
		t.Fatalf("expired ctx: want budget-exhausted marker, got %v", err)
	}
}

// A later task's RED phase must never be STARTED once the remaining
// cycle-context budget can no longer cover that task's own verification
// commands (2x each command's own -timeout, e.g. -timeout 300s needs 600s
// remaining) — starting it anyway risks the cycle's outer deadline SIGKILLing
// a test process mid-run, which only gets classified as a kill AFTER the
// fact. The batch must instead stop cleanly before that task, recording a
// distinct "batch-stopped-insufficient-budget" result, while preserving
// already-completed tasks via the existing partial-landing snapshot unwrap.
// Regression: run 20260718T164339 lost a green milestone to exactly this
// mid-flight SIGKILL.
func TestExecuteSuperpowersTaskBatchStopsCleanlyOnInsufficientBudget(t *testing.T) {
	runner := &partialLandingRunner{t: t, testResults: []CommandResult{
		redFail(), greenPass(), // task 1: done
		// No results scripted for task 2 — it must never reach a test command.
	}}
	run := partialLandingRun(t, 2)
	run.Tasks[0].Tests = []string{"go test ./internal/engine -count=1 -timeout 10s"}
	run.Tasks[1].Tests = []string{"go test ./internal/engine -count=1 -timeout 300s"}
	executor := SuperpowersTaskExecutor{Runner: runner, Claude: &scriptedClaudeRunner{}}

	// 60s remaining covers task 1's 20s requirement (2x its 10s timeout) but
	// not task 2's 600s requirement (2x its 300s timeout).
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(60*time.Second))
	defer cancel()

	if err := executeSuperpowersTaskBatch(ctx, executor, run); err != nil {
		t.Fatalf("insufficient-budget stop must not fail the batch: %v", err)
	}
	if run.Tasks[0].Status != "done" {
		t.Fatalf("task 1 status = %q, want done", run.Tasks[0].Status)
	}
	if run.Tasks[1].Status != "skipped" {
		t.Fatalf("task 2 status = %q, want skipped (insufficient budget)", run.Tasks[1].Status)
	}
	if !strings.Contains(run.PartialFailure, "batch-stopped-insufficient-budget") {
		t.Fatalf("PartialFailure must carry the distinct batch-stopped-insufficient-budget marker: %q", run.PartialFailure)
	}
	if !strings.Contains(run.PartialFailure, "task 2") {
		t.Fatalf("PartialFailure must name the stopped task: %q", run.PartialFailure)
	}
	joined := runner.joined()
	if !strings.Contains(joined, "git commit --no-verify -m superpowers snapshot: task 1") {
		t.Fatalf("task 1 must still be snapshot-committed; calls:\n%s", joined)
	}
	if !strings.Contains(joined, "git reset basesha") {
		t.Fatalf("stopping must still unwrap task 1's snapshot to base; calls:\n%s", joined)
	}
}
