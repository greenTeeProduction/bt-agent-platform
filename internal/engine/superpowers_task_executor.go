package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SuperpowersTaskExecutor struct {
	Runner CommandRunner
	Claude ClaudeRunner
}

// preRedStatusArtifact durably records the worktree's `git status` output
// captured immediately before the RED phase starts. superpowersTaskVerifyRed
// and superpowersTaskVerifyGreen read it back to diff against later snapshots,
// so the baseline survives even when each phase runs as an independent BT
// tick (e.g. via the SuperpowersTaskRed/VerifyRed/Green/VerifyGreen actions)
// rather than in one in-process call like ExecuteTask.
const preRedStatusArtifact = "pre-red-status.txt"

// ensureSuperpowersTaskSetup performs the one-time, idempotent per-task setup
// that both ExecuteTask and the standalone SuperpowersTaskRed action rely on:
// resolving/creating the task artifact directory, writing the combined task
// prompt artifact, and short-circuiting dry-run or no-test-command tasks
// exactly as the original ExecuteTask did before any RED/GREEN work began.
// Returns dryRun=true when the task is already fully handled (dry run mode).
func ensureSuperpowersTaskSetup(run *SuperpowersRun, task *SuperpowersTask) (dryRun bool, err error) {
	task.ArtifactDir = filepath.Join(run.ArtifactDir, "tasks", fmt.Sprintf("%02d-%s", task.Index, safeSlug(task.Title)))
	if err := os.MkdirAll(task.ArtifactDir, 0o755); err != nil {
		return false, err
	}
	prompt := buildSuperpowersTaskPrompt(run, *task)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "prompt.md"), []byte(prompt), 0o644)

	if run.Mode == SuperpowersModeDryRun {
		task.Status = "dry_run"
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, "claude-output.md"), []byte("DRY RUN: Claude Code not invoked; task prompt generated for approval."), 0o644)
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, "red.txt"), []byte("DRY RUN: RED command not executed."), 0o644)
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, "green.txt"), []byte("DRY RUN: GREEN command not executed."), 0o644)
		return true, nil
	}
	if len(task.Tests) == 0 {
		task.Status = "failed"
		return false, fmt.Errorf("superpowers task %q has no test command; refusing non-TDD implementation", task.Title)
	}
	return false, nil
}

// superpowersTaskRed executes the RED phase: it captures the pre-change
// worktree status (baseline for later drift checks) and asks Claude Code to
// add/update only the failing regression test for the task.
func superpowersTaskRed(ctx context.Context, runner CommandRunner, claude ClaudeRunner, run *SuperpowersRun, task *SuperpowersTask) error {
	before := runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--untracked-files=all")
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, preRedStatusArtifact), []byte(before.Output), 0o644)

	redPrompt := buildSuperpowersRedPrompt(run, *task)
	redClaudeRes := claude.RunClaude(ctx, run.WorktreePath, redPrompt)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "red-claude-output.md"), []byte(redClaudeRes.Output), 0o644)
	if redClaudeRes.Err != nil {
		task.Status = "failed"
		return fmt.Errorf("red-phase claude failed: %v\n%s", redClaudeRes.Err, redClaudeRes.Output)
	}
	return nil
}

// superpowersTaskVerifyRed runs the task's first test command and requires it
// to fail (proving the RED test is real), then confirms the RED phase touched
// only test files, not production Go files.
func superpowersTaskVerifyRed(ctx context.Context, runner CommandRunner, run *SuperpowersRun, task *SuperpowersTask) error {
	redCmd := task.Tests[0]
	redRes := runShellCommand(ctx, runner, run.WorktreePath, redCmd)
	redEvidence := formatCommandResult(redRes)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "red.txt"), []byte(redEvidence), 0o644)
	if redRes.Err == nil {
		task.Status = "failed"
		return fmt.Errorf("RED command unexpectedly passed; refusing to run GREEN without failing regression evidence: %s", redCmd)
	}

	before, _ := os.ReadFile(filepath.Join(task.ArtifactDir, preRedStatusArtifact))
	redStatus := runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--untracked-files=all")
	if nonTest := nonTestGoFiles(changedFilesDeltaText(string(before), redStatus.Output)); len(nonTest) > 0 {
		task.Status = "failed"
		return fmt.Errorf("RED phase modified production Go files before GREEN: %s", strings.Join(nonTest, ", "))
	}
	return nil
}

// superpowersTaskGreen executes the GREEN/REFACTOR phase: it feeds the RED
// evidence back to Claude Code and asks it to implement the minimal
// production code needed to pass the RED test. When feedback is non-empty
// (set by a prior SuperpowersTaskReview "needs_work" verdict via
// ChainState["review_feedback"]), it is appended to the prompt so Claude
// addresses the reviewer's concerns on the next pass; an empty feedback
// leaves the prompt unchanged from before the ReviewCycle decorator existed.
func superpowersTaskGreen(ctx context.Context, runner CommandRunner, claude ClaudeRunner, run *SuperpowersRun, task *SuperpowersTask, feedback string) error {
	redEvidence, _ := os.ReadFile(filepath.Join(task.ArtifactDir, "red.txt"))
	greenPrompt := buildSuperpowersGreenPrompt(run, *task, string(redEvidence), feedback)
	greenClaudeRes := claude.RunClaude(ctx, run.WorktreePath, greenPrompt)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "green-claude-output.md"), []byte(greenClaudeRes.Output), 0o644)
	redClaudeOutput, _ := os.ReadFile(filepath.Join(task.ArtifactDir, "red-claude-output.md"))
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "claude-output.md"), []byte("# RED phase\n\n"+string(redClaudeOutput)+"\n\n# GREEN phase\n\n"+greenClaudeRes.Output), 0o644)
	if greenClaudeRes.Err != nil {
		task.Status = "failed"
		return fmt.Errorf("green-phase claude failed: %v\n%s", greenClaudeRes.Err, greenClaudeRes.Output)
	}
	return nil
}

// superpowersTaskVerifyGreen runs every listed test command and requires all
// of them to pass, then records the net set of files changed by the task and
// marks it done.
func superpowersTaskVerifyGreen(ctx context.Context, runner CommandRunner, run *SuperpowersRun, task *SuperpowersTask) error {
	for i, cmd := range task.Tests {
		res := runShellCommand(ctx, runner, run.WorktreePath, cmd)
		name := "green.txt"
		if len(task.Tests) > 1 {
			name = fmt.Sprintf("green-%02d.txt", i+1)
		}
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, name), []byte(formatCommandResult(res)), 0o644)
		if i == len(task.Tests)-1 && name != "green.txt" {
			_ = os.WriteFile(filepath.Join(task.ArtifactDir, "green.txt"), []byte(formatCommandResult(res)), 0o644)
		}
		if res.Err != nil {
			task.Status = "failed"
			return fmt.Errorf("task GREEN verification failed: %s\n%s", cmd, res.Output)
		}
	}
	before, _ := os.ReadFile(filepath.Join(task.ArtifactDir, preRedStatusArtifact))
	after := runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--untracked-files=all")
	run.ChangedFiles = mergeChangedFiles(run.ChangedFiles, changedFilesDeltaText(string(before), after.Output))
	task.Status = "done"
	return nil
}

// ExecuteTask runs one Superpowers task end-to-end through the RED -> verify
// RED -> GREEN -> verify GREEN phases. It is a thin sequential caller of the
// four phase funcs above; the phase funcs themselves also back the
// SuperpowersTaskRed/VerifyRed/Green/VerifyGreen BT actions so a single task
// can alternatively be driven one phase per tick from a ForEachTask loop.
func (e SuperpowersTaskExecutor) ExecuteTask(ctx context.Context, run *SuperpowersRun, task SuperpowersTask) (SuperpowersTask, error) {
	if e.Runner == nil {
		e.Runner = defaultSuperpowersCommandRunner
	}
	if e.Claude == nil {
		e.Claude = defaultSuperpowersClaudeRunner
	}

	dryRun, err := ensureSuperpowersTaskSetup(run, &task)
	if err != nil {
		return task, err
	}
	if dryRun {
		return task, nil
	}

	if err := superpowersTaskRed(ctx, e.Runner, e.Claude, run, &task); err != nil {
		return task, err
	}
	if err := superpowersTaskVerifyRed(ctx, e.Runner, run, &task); err != nil {
		return task, err
	}
	if err := superpowersTaskGreen(ctx, e.Runner, e.Claude, run, &task, ""); err != nil {
		return task, err
	}
	if err := superpowersTaskVerifyGreen(ctx, e.Runner, run, &task); err != nil {
		return task, err
	}
	return task, nil
}

func ExecuteSuperpowersTaskBatchRuntime(ctx context.Context, run *SuperpowersRun) error {
	executor := SuperpowersTaskExecutor{Runner: defaultSuperpowersCommandRunner, Claude: defaultSuperpowersClaudeRunner}
	for i := range run.Tasks {
		task, err := executor.ExecuteTask(ctx, run, run.Tasks[i])
		run.Tasks[i] = task
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			return err
		}
	}
	return nil
}

func VerifySuperpowersRunRuntime(ctx context.Context, run *SuperpowersRun) error {
	checks := []struct {
		name string
		cmd  string
	}{
		{"focused-tests", "/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestGoapFusion_Structure|TestValidateOutputQuality' -timeout 180s"},
		{"build", "/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli"},
	}
	for _, check := range checks {
		res := runShellCommand(ctx, defaultSuperpowersCommandRunner, run.WorktreePathOrRepo(), check.cmd)
		vc := VerificationCheck{Name: check.name, Command: check.cmd, Passed: res.Err == nil, Output: res.Output, Duration: res.Duration.String()}
		run.Verification = append(run.Verification, vc)
		_ = os.WriteFile(filepath.Join(run.ArtifactDir, "verification", check.name+".txt"), []byte(formatCommandResult(res)), 0o644)
		if res.Err != nil {
			return fmt.Errorf("verification %s failed: %v\n%s", check.name, res.Err, res.Output)
		}
	}
	return writeSuperpowersRunJSON(run)
}

func (run *SuperpowersRun) WorktreePathOrRepo() string {
	if run.WorktreePath != "" {
		return run.WorktreePath
	}
	return run.RepoDir
}

func runShellCommand(ctx context.Context, runner CommandRunner, dir, command string) CommandResult {
	return runner.Run(ctx, dir, "bash", "-c", command)
}

func formatCommandResult(res CommandResult) string {
	status := "PASS"
	if res.Err != nil {
		status = "FAIL: " + res.Err.Error()
	}
	return fmt.Sprintf("Command: %s\nDir: %s\nDuration: %s\nStatus: %s\n\n%s", res.Command, res.Dir, res.Duration, status, res.Output)
}

func buildSuperpowersTaskPrompt(run *SuperpowersRun, task SuperpowersTask) string {
	return fmt.Sprintf(`You are Claude Code executing one task from the Superpowers SDLC.

Repo: %s
Task %d: %s
Objective: %s
Files: %s
Tests: %s

Plan body:
%s

Rules:
- Follow RED/GREEN/REFACTOR.
- Modify only the files listed for this task.
- Preserve existing functionality.
- Do not edit graphify-out or secrets.
- Return final schema: FILES_CHANGED, RED_COMMAND, RED_RESULT, GREEN_COMMAND, GREEN_RESULT, NOTES.
`, run.WorktreePathOrRepo(), task.Index, task.Title, task.Objective, strings.Join(task.Files, ", "), strings.Join(task.Tests, "; "), task.Body)
}

func mergeChangedFiles(existing, next []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range append(existing, next...) {
		f = strings.TrimSpace(f)
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func changedFilesDeltaText(before, after string) []string {
	base := map[string]bool{}
	for _, f := range changedFilesFromGitStatus(before) {
		base[f] = true
	}
	var delta []string
	for _, f := range changedFilesFromGitStatus(after) {
		if !base[f] {
			delta = append(delta, f)
		}
	}
	return delta
}

func changedFilesFromGitStatus(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || len(line) < 4 {
			continue
		}
		file := strings.TrimSpace(line[2:])
		if file != "" && !strings.HasPrefix(file, "graphify-out/") {
			files = append(files, file)
		}
	}
	return files
}

func nonTestGoFiles(files []string) []string {
	var out []string
	for _, f := range files {
		if strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, "_test.go") {
			out = append(out, f)
		}
	}
	return out
}

func nowRFC3339() string { return time.Now().Format(time.RFC3339) }

func buildSuperpowersRedPrompt(run *SuperpowersRun, task SuperpowersTask) string {
	return fmt.Sprintf(`You are Claude Code executing the RED phase of one Superpowers SDLC task.

Repo: %s
Task %d: %s
Objective: %s
Files: %s
Tests: %s

Plan body:
%s

RED phase rules:
- Add or update ONLY the failing regression test for this task.
- Do NOT implement production code yet.
- Run the first listed test command and leave it failing for the intended reason.
- Preserve existing functionality and do not edit graphify-out or secrets.
- Return final schema: FILES_CHANGED, RED_COMMAND, RED_RESULT, NOTES.
`, run.WorktreePathOrRepo(), task.Index, task.Title, task.Objective, strings.Join(task.Files, ", "), strings.Join(task.Tests, "; "), task.Body)
}

func buildSuperpowersGreenPrompt(run *SuperpowersRun, task SuperpowersTask, redOutput string, feedback string) string {
	prompt := fmt.Sprintf(`You are Claude Code executing the GREEN/REFACTOR phase of one Superpowers SDLC task.

Repo: %s
Task %d: %s
Objective: %s
Files: %s
Tests: %s

RED output proving the test failed first:
---
%s
---

Plan body:
%s

GREEN phase rules:
- Implement the minimal production code needed to pass the RED test.
- Run all listed test commands until they pass.
- Apply gofmt to changed Go files.
- Preserve existing functionality and do not edit graphify-out or secrets.
- Return final schema: FILES_CHANGED, GREEN_COMMANDS, GREEN_RESULTS, NOTES.
`, run.WorktreePathOrRepo(), task.Index, task.Title, task.Objective, strings.Join(task.Files, ", "), strings.Join(task.Tests, "; "), truncateGoap(redOutput, 4000), task.Body)
	if strings.TrimSpace(feedback) != "" {
		prompt += fmt.Sprintf("\nAddress this review feedback: %s\n", strings.TrimSpace(feedback))
	}
	return prompt
}
