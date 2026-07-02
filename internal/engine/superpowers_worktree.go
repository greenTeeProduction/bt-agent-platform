package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// superpowersWorktreeBase is the parent directory for all Superpowers run
// worktrees. A var (not const) so sweep tests can point it at a temp dir.
var superpowersWorktreeBase = "/tmp/worktrees"

// staleSuperpowersWorktreeMaxAge is the grace period before an abandoned run
// worktree is reaped by sweepStaleSuperpowersWorktrees. Failed runs keep
// their worktree for diagnosis; anything older than this is a leak.
const staleSuperpowersWorktreeMaxAge = 24 * time.Hour

func planSuperpowersWorktree(run *SuperpowersRun) (string, string) {
	slug := safeSlug(run.Task)
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return filepath.Join(superpowersWorktreeBase, "superpowers-"+run.ID+"-"+slug), "superpowers/" + run.ID
}

func validateSuperpowersWorktreePath(path string) error {
	if path == "" {
		return fmt.Errorf("empty worktree path")
	}
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, filepath.Clean(superpowersWorktreeBase)+"/superpowers-") {
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
	if isBareGitRepo(ctx, runner, repoDir) {
		// A bare repo has no working tree to dirty, reset, or check out —
		// just bring master up to date with origin. The non-forced refspec
		// preserves the `git pull --ff-only` guarantee: a non-fast-forward
		// master update is refused.
		if fetch := runner.Run(ctx, repoDir, "git", "fetch", "origin", "master:master"); fetch.Err != nil {
			return fmt.Errorf("could not fast-forward bare repo master from origin before Superpowers worktree sync: %v\n%s", fetch.Err, fetch.Output)
		}
		return nil
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

// isBareGitRepo reports whether dir is a bare git repository — one with refs
// but no working tree (worktree add/remove and ref updates work there;
// status/checkout/pull/apply do not).
func isBareGitRepo(ctx context.Context, runner CommandRunner, dir string) bool {
	res := runner.Run(ctx, dir, "git", "rev-parse", "--is-bare-repository")
	return res.Err == nil && strings.TrimSpace(res.Output) == "true"
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

// cleanupAppliedSuperpowersWorktree removes the run's worktree and deletes its
// branch after a successful apply. At that point the branch's commits are on
// master, so `git branch -d` succeeds; an unexpectedly unmerged branch makes
// -d fail and is surfaced (not forced) so nothing unmerged is ever destroyed.
func cleanupAppliedSuperpowersWorktree(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	if run == nil || run.Mode == SuperpowersModeDryRun || run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		return nil
	}
	if err := cleanupSuperpowersWorktree(ctx, runner, run); err != nil {
		return err
	}
	if branch := strings.TrimSpace(run.WorktreeBranch); branch != "" && branch != "dry-run" {
		if res := runner.Run(ctx, run.RepoDir, "git", "branch", "-d", branch); res.Err != nil {
			return fmt.Errorf("git branch -d %s failed: %v\n%s", branch, res.Err, res.Output)
		}
	}
	return nil
}

// sweepStaleSuperpowersWorktrees reaps run worktrees under
// superpowersWorktreeBase older than maxAge, except keepPath (the current
// run). It exists because failure paths deliberately keep their worktree for
// diagnosis — without a sweeper those leftovers accumulate until the disk
// fills. Best-effort by design: it returns the removed paths and never fails
// the calling run. Each swept worktree's branch is deleted with `git branch
// -d` only, so a branch holding unmerged commits survives for manual triage.
func sweepStaleSuperpowersWorktrees(ctx context.Context, runner CommandRunner, repoDir, keepPath string, maxAge time.Duration) []string {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	entries, err := os.ReadDir(superpowersWorktreeBase)
	if err != nil {
		return nil
	}
	branches := superpowersWorktreeBranches(ctx, runner, repoDir)
	var removed []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "superpowers-") {
			continue
		}
		path := filepath.Join(superpowersWorktreeBase, e.Name())
		if keepPath != "" && path == filepath.Clean(keepPath) {
			continue
		}
		if validateSuperpowersWorktreePath(path) != nil {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < maxAge {
			continue
		}
		if res := runner.Run(ctx, repoDir, "git", "worktree", "remove", "--force", path); res.Err != nil {
			// Not a registered worktree (orphan dir), or removal failed:
			// prune stale registrations and delete the directory directly —
			// path is guarded by validateSuperpowersWorktreePath above.
			runner.Run(ctx, repoDir, "git", "worktree", "prune")
			if os.RemoveAll(path) != nil {
				continue
			}
		}
		removed = append(removed, path)
		if branch := branches[path]; branch != "" {
			runner.Run(ctx, repoDir, "git", "branch", "-d", branch)
		}
	}
	return removed
}

// superpowersWorktreeBranches maps registered worktree paths to their branch
// names via `git worktree list --porcelain`.
func superpowersWorktreeBranches(ctx context.Context, runner CommandRunner, repoDir string) map[string]string {
	branches := map[string]string{}
	res := runner.Run(ctx, repoDir, "git", "worktree", "list", "--porcelain")
	if res.Err != nil {
		return branches
	}
	var current string
	for _, line := range strings.Split(res.Output, "\n") {
		line = strings.TrimSpace(line)
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			current = filepath.Clean(p)
		} else if b, ok := strings.CutPrefix(line, "branch "); ok && current != "" {
			branches[current] = strings.TrimPrefix(b, "refs/heads/")
		}
	}
	return branches
}
