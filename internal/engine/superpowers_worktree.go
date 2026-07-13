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
		// just make sure local master contains everything origin has. A plain
		// `git fetch origin master:master` is wrong here: it rejects when
		// local master is AHEAD of origin (a locally landed but not yet
		// pushed commit), which stalled every scheduled cycle on 2026-07-03.
		// Fetch origin's tip without touching master, then decide by
		// ancestry; only a genuine divergence fails the cycle.
		if fetch := runner.Run(ctx, repoDir, "git", "fetch", "origin", "master"); fetch.Err != nil {
			return fmt.Errorf("could not fetch origin master before Superpowers worktree sync: %v\n%s", fetch.Err, fetch.Output)
		}
		originTip := runner.Run(ctx, repoDir, "git", "rev-parse", "FETCH_HEAD")
		if originTip.Err != nil {
			return fmt.Errorf("could not resolve fetched origin master tip before Superpowers worktree sync: %v\n%s", originTip.Err, originTip.Output)
		}
		origin := strings.TrimSpace(originTip.Output)
		// Local master already contains origin's tip (equal or ahead): the
		// sync goal is met. Ahead commits reach origin via the apply-stage
		// push; failing here would deadlock the loop on its own landed work.
		if anc := runner.Run(ctx, repoDir, "git", "merge-base", "--is-ancestor", origin, "master"); anc.Err == nil {
			return nil
		}
		// Local master is strictly behind: fast-forward it, guarded by the
		// old value so a concurrent ref update loses cleanly instead of
		// being clobbered. This preserves the ff-only guarantee.
		if anc := runner.Run(ctx, repoDir, "git", "merge-base", "--is-ancestor", "master", origin); anc.Err == nil {
			local := runner.Run(ctx, repoDir, "git", "rev-parse", "master")
			if local.Err != nil {
				return fmt.Errorf("could not resolve local master before Superpowers worktree sync: %v\n%s", local.Err, local.Output)
			}
			if upd := runner.Run(ctx, repoDir, "git", "update-ref", "refs/heads/master", origin, strings.TrimSpace(local.Output)); upd.Err != nil {
				return fmt.Errorf("could not fast-forward bare repo master to origin tip %s before Superpowers worktree sync: %v\n%s", origin, upd.Err, upd.Output)
			}
			return nil
		}
		return fmt.Errorf("bare repo master and origin/master have diverged (neither contains the other) before Superpowers worktree sync; refusing to guess — reconcile manually (origin tip %s)", origin)
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

// reapOrphanedSuperpowersBranches deletes superpowers/* branches that have no
// registered worktree. This is the leak sweepStaleSuperpowersWorktrees cannot
// reach: that sweep only deletes a branch while reaping its still-present
// worktree dir, so once the dir is gone (landed-run cleanup, an earlier sweep,
// or an OS /tmp wipe) the branch is orphaned forever and accumulates unbounded.
// `git branch -d` deletes only branches merged into HEAD (master in the bare
// repo), so an unmerged orphan holding un-landed work survives for manual
// triage; a branch still attached to a worktree is skipped. Best-effort by
// design: returns the reaped branch names and never fails the calling run.
func reapOrphanedSuperpowersBranches(ctx context.Context, runner CommandRunner, repoDir string) []string {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	held := map[string]bool{}
	for _, b := range superpowersWorktreeBranches(ctx, runner, repoDir) {
		held[b] = true
	}
	list := runner.Run(ctx, repoDir, "git", "branch", "--list", "superpowers/*", "--format=%(refname:short)")
	if list.Err != nil {
		return nil
	}
	var reaped []string
	for _, line := range strings.Split(list.Output, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" || held[branch] {
			continue
		}
		if res := runner.Run(ctx, repoDir, "git", "branch", "-d", branch); res.Err == nil {
			reaped = append(reaped, branch)
		}
	}
	return reaped
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
