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

func (e SuperpowersTaskExecutor) ExecuteTask(ctx context.Context, run *SuperpowersRun, task SuperpowersTask) (SuperpowersTask, error) {
	if e.Runner == nil {
		e.Runner = defaultSuperpowersCommandRunner
	}
	if e.Claude == nil {
		e.Claude = defaultSuperpowersClaudeRunner
	}
	task.ArtifactDir = filepath.Join(run.ArtifactDir, "tasks", fmt.Sprintf("%02d-%s", task.Index, safeSlug(task.Title)))
	if err := os.MkdirAll(task.ArtifactDir, 0o755); err != nil {
		return task, err
	}
	prompt := buildSuperpowersTaskPrompt(run, task)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "prompt.md"), []byte(prompt), 0o644)

	if run.Mode == SuperpowersModeDryRun {
		task.Status = "dry_run"
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, "claude-output.md"), []byte("DRY RUN: Claude Code not invoked; task prompt generated for approval."), 0o644)
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, "red.txt"), []byte("DRY RUN: RED command not executed."), 0o644)
		_ = os.WriteFile(filepath.Join(task.ArtifactDir, "green.txt"), []byte("DRY RUN: GREEN command not executed."), 0o644)
		return task, nil
	}
	if len(task.Tests) == 0 {
		task.Status = "failed"
		return task, fmt.Errorf("superpowers task %q has no test command; refusing non-TDD implementation", task.Title)
	}

	before := e.Runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--untracked-files=all")

	redPrompt := buildSuperpowersRedPrompt(run, task)
	redClaudeRes := e.Claude.RunClaude(ctx, run.WorktreePath, redPrompt)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "red-claude-output.md"), []byte(redClaudeRes.Output), 0o644)
	if redClaudeRes.Err != nil {
		task.Status = "failed"
		return task, fmt.Errorf("red-phase claude failed: %v\n%s", redClaudeRes.Err, redClaudeRes.Output)
	}

	redCmd := task.Tests[0]
	redRes := runShellCommand(ctx, e.Runner, run.WorktreePath, redCmd)
	redEvidence := formatCommandResult(redRes)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "red.txt"), []byte(redEvidence), 0o644)
	if redRes.Err == nil {
		task.Status = "failed"
		return task, fmt.Errorf("RED command unexpectedly passed; refusing to run GREEN without failing regression evidence: %s", redCmd)
	}

	redStatus := e.Runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--untracked-files=all")
	if nonTest := nonTestGoFiles(changedFilesDeltaText(before.Output, redStatus.Output)); len(nonTest) > 0 {
		task.Status = "failed"
		return task, fmt.Errorf("RED phase modified production Go files before GREEN: %s", strings.Join(nonTest, ", "))
	}

	greenPrompt := buildSuperpowersGreenPrompt(run, task, redEvidence)
	greenClaudeRes := e.Claude.RunClaude(ctx, run.WorktreePath, greenPrompt)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "green-claude-output.md"), []byte(greenClaudeRes.Output), 0o644)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "claude-output.md"), []byte("# RED phase\n\n"+redClaudeRes.Output+"\n\n# GREEN phase\n\n"+greenClaudeRes.Output), 0o644)
	if greenClaudeRes.Err != nil {
		task.Status = "failed"
		return task, fmt.Errorf("green-phase claude failed: %v\n%s", greenClaudeRes.Err, greenClaudeRes.Output)
	}

	for i, cmd := range task.Tests {
		res := runShellCommand(ctx, e.Runner, run.WorktreePath, cmd)
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
			return task, fmt.Errorf("task GREEN verification failed: %s\n%s", cmd, res.Output)
		}
	}
	after := e.Runner.Run(ctx, run.WorktreePath, "git", "status", "--short", "--untracked-files=all")
	run.ChangedFiles = mergeChangedFiles(run.ChangedFiles, changedFilesDeltaText(before.Output, after.Output))
	task.Status = "done"
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

func buildSuperpowersGreenPrompt(run *SuperpowersRun, task SuperpowersTask, redOutput string) string {
	return fmt.Sprintf(`You are Claude Code executing the GREEN/REFACTOR phase of one Superpowers SDLC task.

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
}
