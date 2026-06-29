package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func applySuperpowersRunToMainRepo(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	if run == nil {
		return fmt.Errorf("nil superpowers run")
	}
	if run.Mode == SuperpowersModeDryRun {
		run.ApplyStatus = "dry_run"
		return writeSuperpowersRunJSON(run)
	}
	if run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		run.ApplyStatus = "main_repo"
		return writeSuperpowersRunJSON(run)
	}

	patchText, err := captureSuperpowersWorktreePatch(ctx, runner, run)
	if err != nil {
		run.ApplyStatus = "patch_failed"
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	if strings.TrimSpace(patchText) == "" {
		run.ApplyStatus = "no_changes"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("no_changes: Superpowers worktree produced no patch to apply")
	}

	patchPath := filepath.Join(run.ArtifactDir, "verification", "worktree.patch")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(patchPath, []byte(patchText), 0o644); err != nil {
		return err
	}
	run.PatchPath = patchPath

	status := runner.Run(ctx, run.RepoDir, "git", "status", "--short", "--untracked-files=all")
	if status.Err != nil {
		run.ApplyStatus = "pending_patch"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("pending_patch: could not inspect main repo status: %v\npatch: %s\n%s", status.Err, patchPath, status.Output)
	}
	if hasBlockingMainRepoDirty(status.Output) {
		run.ApplyStatus = "pending_patch"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("pending_patch: main repo has blocking dirty files; patch saved to %s\n%s", patchPath, blockingMainRepoDirtySummary(status.Output))
	}

	check := runner.Run(ctx, run.RepoDir, "git", "apply", "--check", patchPath)
	if check.Err != nil {
		run.ApplyStatus = "pending_patch"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("pending_patch: git apply --check failed for %s: %v\n%s", patchPath, check.Err, check.Output)
	}
	apply := runner.Run(ctx, run.RepoDir, "git", "apply", patchPath)
	if apply.Err != nil {
		run.ApplyStatus = "pending_patch"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("pending_patch: git apply failed for %s: %v\n%s", patchPath, apply.Err, apply.Output)
	}
	run.ApplyStatus = "applied"
	if err := verifySuperpowersMainRepoRuntime(ctx, runner, run); err != nil {
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	if err := commitAppliedSuperpowersRun(ctx, runner, run); err != nil {
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	return writeSuperpowersRunJSON(run)
}

func captureSuperpowersWorktreePatch(ctx context.Context, runner CommandRunner, run *SuperpowersRun) (string, error) {
	// git add -N does not support :! exclusion pathspecs and fails when
	// graphify-out is in .gitignore. Skip the intent-to-add step — Claude
	// Code operations only modify existing tracked files, so git diff alone
	// captures all changes. If untracked files are ever needed, use:
	//   git add -N . && git diff --binary -- . ':!graphify-out/'
	res := runShellCommand(ctx, runner, run.WorktreePath, "git diff --binary -- . ':!graphify-out/'")
	if res.Err != nil {
		return "", fmt.Errorf("capture worktree patch failed: %v\n%s", res.Err, res.Output)
	}
	return res.Output, nil
}

func verifySuperpowersMainRepoRuntime(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	checks := []struct {
		name string
		cmd  string
	}{
		{"main-focused-tests", "/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestGoapFusion_Structure|TestValidateOutputQuality' -timeout 180s"},
		{"main-build", "/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli"},
		{"graphify-update", "graphify update ."},
	}
	for _, check := range checks {
		res := runShellCommand(ctx, runner, run.RepoDir, check.cmd)
		vc := VerificationCheck{Name: check.name, Command: check.cmd, Passed: res.Err == nil, Output: res.Output, Duration: res.Duration.String()}
		run.Verification = append(run.Verification, vc)
		_ = os.WriteFile(filepath.Join(run.ArtifactDir, "verification", check.name+".txt"), []byte(formatCommandResult(res)), 0o644)
		if res.Err != nil {
			run.ApplyStatus = "pending_patch"
			return fmt.Errorf("main repo verification %s failed after applying patch: %v\n%s", check.name, res.Err, res.Output)
		}
	}
	return nil
}

func commitAppliedSuperpowersRun(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	add := runner.Run(ctx, run.RepoDir, "git", "add", "-A", "--", ".", ":(exclude)docs/superpowers/runs/**", ":(exclude)docs/superpowers/plans/**")
	if add.Err != nil {
		run.ApplyStatus = "applied_uncommitted"
		return fmt.Errorf("applied_uncommitted: git add failed: %v\n%s", add.Err, add.Output)
	}
	staged := runShellCommand(ctx, runner, run.RepoDir, "git diff --cached --quiet -- . ':!docs/superpowers/runs/**' ':!docs/superpowers/plans/**'")
	if staged.Err == nil {
		run.ApplyStatus = "applied_no_commit"
		return nil
	}
	commit := runner.Run(ctx, run.RepoDir, "git", "commit", "-m", fmt.Sprintf("superpowers: apply verified run %s", run.ID))
	if commit.Err != nil {
		run.ApplyStatus = "applied_uncommitted"
		return fmt.Errorf("applied_uncommitted: git commit failed: %v\n%s", commit.Err, commit.Output)
	}
	head := runner.Run(ctx, run.RepoDir, "git", "rev-parse", "--short", "HEAD")
	if head.Err == nil {
		run.AppliedCommit = strings.TrimSpace(head.Output)
	}
	run.ApplyStatus = "committed"

	push := runner.Run(ctx, run.RepoDir, "git", "push", "origin", "master")
	if push.Err != nil {
		run.ApplyStatus = "committed_unpushed"
		return fmt.Errorf("committed_unpushed: git push origin master failed: %v\n%s", push.Err, push.Output)
	}
	return nil
}

func hasBlockingMainRepoDirty(status string) bool {
	return blockingMainRepoDirtySummary(status) != ""
}

func blockingMainRepoDirtySummary(status string) string {
	var blocking []string
	for _, line := range strings.Split(status, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || len(trimmed) < 4 {
			continue
		}
		path := strings.TrimSpace(trimmed[2:])
		if isGeneratedSuperpowersOrGraphifyPath(path) {
			continue
		}
		blocking = append(blocking, trimmed)
	}
	return strings.Join(blocking, "\n")
}

func isGeneratedSuperpowersOrGraphifyPath(path string) bool {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "-> ")
	return strings.HasPrefix(path, "graphify-out/") ||
		strings.HasPrefix(path, "docs/superpowers/runs/") ||
		strings.HasPrefix(path, "docs/superpowers/plans/")
}
