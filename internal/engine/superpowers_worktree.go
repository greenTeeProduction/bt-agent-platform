package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func planSuperpowersWorktree(run *SuperpowersRun) (string, string) {
	slug := safeSlug(run.Task)
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return filepath.Join("/tmp/worktrees", "superpowers-"+run.ID+"-"+slug), "superpowers/" + run.ID
}

func validateSuperpowersWorktreePath(path string) error {
	if path == "" {
		return fmt.Errorf("empty worktree path")
	}
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, "/tmp/worktrees/superpowers-") {
		return fmt.Errorf("unsafe worktree path %q", path)
	}
	if strings.Contains(clean, "..") || clean == "/" || clean == "/tmp" || clean == superpowersRepoDir || clean == "/home/nico" {
		return fmt.Errorf("unsafe worktree path %q", path)
	}
	return nil
}

func createSuperpowersWorktree(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	if run.Mode == SuperpowersModeDryRun {
		run.WorktreePath = run.RepoDir
		run.WorktreeBranch = "dry-run"
		return nil
	}
	if err := syncSuperpowersRepoForWorktree(ctx, runner, run.RepoDir); err != nil {
		return err
	}
	path, branch := planSuperpowersWorktree(run)
	if err := validateSuperpowersWorktreePath(path); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		run.WorktreePath = path
		run.WorktreeBranch = branch
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	res := runner.Run(ctx, run.RepoDir, "git", "worktree", "add", path, "-b", branch, "HEAD")
	if res.Err != nil {
		return fmt.Errorf("git worktree add failed: %v\n%s", res.Err, res.Output)
	}
	run.WorktreePath = path
	run.WorktreeBranch = branch
	return nil
}

func syncSuperpowersRepoForWorktree(ctx context.Context, runner CommandRunner, repoDir string) error {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	status := runner.Run(ctx, repoDir, "git", "status", "--short", "--untracked-files=all")
	if status.Err != nil {
		return fmt.Errorf("could not inspect repo status before Superpowers worktree sync: %v\n%s", status.Err, status.Output)
	}
	if hasBlockingMainRepoDirty(status.Output) {
		return fmt.Errorf("main repo has blocking dirty files before Superpowers worktree sync; refusing to run on non-reproducible state:\n%s", blockingMainRepoDirtySummary(status.Output))
	}
	if checkoutGraph := runner.Run(ctx, repoDir, "git", "checkout", "--", "graphify-out/"); checkoutGraph.Err != nil {
		return fmt.Errorf("could not reset generated graphify-out before Superpowers worktree sync: %v\n%s", checkoutGraph.Err, checkoutGraph.Output)
	}
	if checkout := runner.Run(ctx, repoDir, "git", "checkout", "master"); checkout.Err != nil {
		return fmt.Errorf("could not checkout master before Superpowers worktree sync: %v\n%s", checkout.Err, checkout.Output)
	}
	if fetch := runner.Run(ctx, repoDir, "git", "fetch", "origin"); fetch.Err != nil {
		return fmt.Errorf("could not fetch origin before Superpowers worktree sync: %v\n%s", fetch.Err, fetch.Output)
	}
	if pull := runner.Run(ctx, repoDir, "git", "pull", "origin", "master", "--ff-only"); pull.Err != nil {
		return fmt.Errorf("local master is not safely up to date with origin/master before Superpowers worktree sync: %v\n%s", pull.Err, pull.Output)
	}
	return nil
}

func cleanupSuperpowersWorktree(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	if run == nil || run.Mode == SuperpowersModeDryRun || run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		return nil
	}
	if err := validateSuperpowersWorktreePath(run.WorktreePath); err != nil {
		return err
	}
	res := runner.Run(ctx, run.RepoDir, "git", "worktree", "remove", run.WorktreePath, "--force")
	if res.Err != nil {
		return fmt.Errorf("git worktree remove failed: %v\n%s", res.Err, res.Output)
	}
	return nil
}
