package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

	// A bare main repo has no working tree to dirty-check, patch, verify, or
	// commit in — land the run through its own worktree instead.
	if isBareGitRepo(ctx, runner, run.RepoDir) {
		return applySuperpowersRunFromBareRepo(ctx, runner, run)
	}

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
	if err := verifySuperpowersRuntimeInDir(ctx, runner, run, run.RepoDir); err != nil {
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	if err := commitAppliedSuperpowersRun(ctx, runner, run); err != nil {
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	return writeSuperpowersRunJSON(run)
}

// ffRebaseMaxAttempts bounds the apply-time rebase-then-retry so a fast-moving
// sibling scheduler that keeps advancing master cannot spin the landing forever.
// Default 2; override with BT_SUPERPOWERS_FF_REBASE_ATTEMPTS. A value of 0 (or an
// unset/invalid env) disables the rebase entirely — the first refused
// fast-forward parks as pending_patch, i.e. the pre-fix behavior — while any
// n>=1 rebases-and-retries up to n times. 1 means "rebase once, retry the ff
// once, else park".
func ffRebaseMaxAttempts() int {
	if raw := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_FF_REBASE_ATTEMPTS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return 2
}

// applySuperpowersRunFromBareRepo lands a run when the main repo is bare. The
// run's worktree serves as the staging checkout: verification and the run
// commit happen there, then the bare repo's master ref is fast-forwarded to
// the run branch and pushed. The non-forced fetch refspec mirrors the
// checkout flow's `git pull --ff-only` guarantee — if master moved since the
// worktree was created, the ref update is refused and the run parks as
// pending_patch with its patch artifact intact.
func applySuperpowersRunFromBareRepo(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	branch := strings.TrimSpace(run.WorktreeBranch)
	if branch == "" {
		run.ApplyStatus = "pending_patch"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("pending_patch: bare-repo apply needs the run's worktree branch, but WorktreeBranch is empty; patch saved to %s", run.PatchPath)
	}
	run.ApplyStatus = "applied"
	if err := verifySuperpowersRuntimeInDir(ctx, runner, run, run.WorktreePath); err != nil {
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	committed, err := stageAndCommitSuperpowersRunInDir(ctx, runner, defaultSuperpowersClaudeRunner, run, run.WorktreePath)
	if err != nil {
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	if !committed {
		run.ApplyStatus = "applied_no_commit"
		return writeSuperpowersRunJSON(run)
	}
	// Land the run onto master. The non-forced ff refuses when master moved
	// during this cycle; instead of discarding the verified work, re-apply the
	// run branch onto the moved master (rebase in its worktree), re-verify, and
	// retry — bounded, and parking cleanly on any non-clean outcome.
	if err := reapplyRunBranchOntoMaster(ctx, runner, run); err != nil {
		run.ApplyStatus = "pending_patch"
		_ = writeSuperpowersRunJSON(run)
		return err
	}
	if head := runner.Run(ctx, run.RepoDir, "git", "rev-parse", "--short", "master"); head.Err == nil {
		run.AppliedCommit = strings.TrimSpace(head.Output)
	}
	run.ApplyStatus = "committed"
	push := runner.Run(ctx, run.RepoDir, "git", "push", "origin", "master")
	if push.Err != nil {
		run.ApplyStatus = "committed_unpushed"
		_ = writeSuperpowersRunJSON(run)
		return fmt.Errorf("committed_unpushed: git push origin master failed: %v\n%s", push.Err, push.Output)
	}
	return writeSuperpowersRunJSON(run)
}

// reapplyRunBranchOntoMaster fast-forwards the bare repo's master to the run
// branch, re-applying (rebasing) the branch onto a moved master and retrying
// when the non-forced fast-forward is refused. It returns nil once master has
// been fast-forwarded to the (re-verified) run branch, and a `pending_patch:`
// error — for the caller to park — for EVERY non-clean outcome. Master safety
// is the whole point of this helper; the invariants it upholds are, in order:
//
//  1. The NON-FORCED `git fetch . <branch>:master` is the ONLY command here
//     that writes master. It is inherently ff-guarded: a concurrent sibling
//     scheduler that advanced master since this worktree was created is REFUSED,
//     never clobbered. There is no update-ref / branch -f / push --force / `+`
//     refspec anywhere — a refusal must stay a refusal.
//  2. Never leave a rebase in progress: every rebase failure runs
//     `git rebase --abort` before returning, restoring the branch tip. Master
//     is untouched regardless, because a rebase never moves its base ref.
//  3. Never land an unverified tree: after a successful rebase the branch has a
//     NEW BASE (current master) that was never built/tested, so it is re-run
//     through verifySuperpowersRuntimeInDir before the retry ff.
//  4. Plain `git rebase master` only — no -s/-X strategy that could silently
//     drop master's or the run's commits.
//  5. A symbolic-ref guard refuses to rebase unless the worktree HEAD is exactly
//     the run branch; a detached or wrong HEAD would let rebase move the wrong
//     ref. On a guard/rebase/verify failure, or once ffRebaseMaxAttempts is
//     exhausted, it returns a pending_patch error (invariant 6: every non-clean
//     outcome reaches pending_patch).
//
// The worktree at run.WorktreePath is the run's LINKED worktree of the bare
// repo, checked out on run.WorktreeBranch (the same checkout the earlier verify
// and commit ran in) — so `git rebase master` there is valid even though the
// bare run.RepoDir has no working tree.
func reapplyRunBranchOntoMaster(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	branch := strings.TrimSpace(run.WorktreeBranch)
	attempts := ffRebaseMaxAttempts()
	for {
		// Invariant 1: the sole, non-forced writer of master.
		ff := runner.Run(ctx, run.RepoDir, "git", "fetch", ".", branch+":master")
		if ff.Err == nil {
			return nil
		}
		// master moved during this cycle; the non-forced fetch refused rather
		// than clobbering the sibling advance. Re-apply onto it if the budget
		// allows, otherwise park (attempts==0 reproduces the pre-fix behavior).
		if attempts < 1 {
			return fmt.Errorf("pending_patch: fast-forward of master to %s refused (master moved since the worktree was created): %v\npatch: %s\n%s", branch, ff.Err, run.PatchPath, ff.Output)
		}
		attempts--

		// Invariant 5 (guard): never rebase unless the worktree HEAD is exactly
		// the run branch — a detached/wrong HEAD would let rebase move the wrong
		// ref. Failing the guard parks (invariant 6) WITHOUT touching the branch.
		sym := runner.Run(ctx, run.WorktreePath, "git", "symbolic-ref", "--short", "HEAD")
		if sym.Err != nil || strings.TrimSpace(sym.Output) != branch {
			return fmt.Errorf("pending_patch: fast-forward of master to %s refused and re-apply skipped: worktree HEAD is not on %s (symbolic-ref=%q, err=%v)\npatch: %s\n%s", branch, branch, strings.TrimSpace(sym.Output), sym.Err, run.PatchPath, ff.Output)
		}

		// Invariant 4: PLAIN rebase only — no strategy flag that could drop
		// commits. master is the base; rebase never moves the base ref.
		rebase := runner.Run(ctx, run.WorktreePath, "git", "rebase", "master")
		if rebase.Err != nil {
			// Invariant 2: never leave a rebase in progress. Abort to restore
			// the branch tip, then park — whether or not the abort itself
			// succeeds, NEVER proceed to the ff.
			abort := runner.Run(ctx, run.WorktreePath, "git", "rebase", "--abort")
			writeApplyRebaseEvidence(run, rebase, abort)
			return fmt.Errorf("pending_patch: re-apply of %s onto moved master failed (rebase conflict) and was aborted: %v\npatch: %s\nrebase: %s\nabort: %s", branch, rebase.Err, run.PatchPath, rebase.Output, abort.Output)
		}

		// Invariant 3: the branch now descends from current master, but that new
		// base was never verified. Re-run the read-only build/test verification
		// before retrying the ff. Do NOT re-commit — the rebase already carries
		// the run's commits. A failure here sets pending_patch and returns.
		if err := verifySuperpowersRuntimeInDir(ctx, runner, run, run.WorktreePath); err != nil {
			return err
		}
		// Loop back and retry the ff against the freshly-rebased, re-verified
		// branch (invariant 1 again). Bounded by attempts.
	}
}

// prBodyFooter is the standard attribution footer the plan
// (docs/superpowers/plans/2026-07-02-superpowers-bt-workflow-nodes.md, Task 11)
// asks PushBranchAndCreatePR to append to the PR body, mirroring the repo's
// commit-message convention (CLAUDE.md).
const prBodyFooter = "🤖 Generated with [Claude Code](https://claude.com/claude-code)"

// pushBranchAndCreatePR pushes the run's worktree branch to origin and opens
// a GitHub pull request via `gh pr create --fill`, both run through runner in
// the worktree directory — the "push" finish option from
// finishing-a-development-branch (Part A of the plan).
//
// Footer note (documented per the task brief's explicit allowance): gh CLI's
// `--fill` derives the PR title/body from the branch's commit log, and `gh pr
// create` rejects combining `--fill` with `--title`/`--body`/`--body-file`
// (they are mutually exclusive) — there is no single-call way to both fill
// from commits and append prBodyFooter. A follow-up `gh pr view --json body`
// + `gh pr edit --body` round trip could append it, but that doubles this
// action's external command surface (and its failure modes: JSON field
// availability, `--jq` support) for a cosmetic footer. Per the brief's
// explicit allowance ("--fill alone is acceptable, document it"), this
// implementation uses `--fill` alone; prBodyFooter is defined for reuse if a
// future revision adds the edit round trip.
func pushBranchAndCreatePR(ctx context.Context, runner CommandRunner, run *SuperpowersRun) (string, error) {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	if run == nil {
		return "", fmt.Errorf("nil superpowers run")
	}
	dir := run.WorktreePathOrRepo()
	branch := strings.TrimSpace(run.WorktreeBranch)
	if branch == "" {
		head := runner.Run(ctx, dir, "git", "rev-parse", "--abbrev-ref", "HEAD")
		if head.Err != nil {
			return "", fmt.Errorf("could not resolve current branch for PR: %v\n%s", head.Err, head.Output)
		}
		branch = strings.TrimSpace(head.Output)
	}
	if branch == "" || branch == "HEAD" {
		return "", fmt.Errorf("no usable branch resolved for PushBranchAndCreatePR")
	}

	push := runner.Run(ctx, dir, "git", "push", "-u", "origin", branch)
	if push.Err != nil {
		return "", fmt.Errorf("git push -u origin %s failed: %v\n%s", branch, push.Err, push.Output)
	}

	create := runner.Run(ctx, dir, "gh", "pr", "create", "--fill")
	if create.Err != nil {
		return "", fmt.Errorf("gh pr create --fill failed: %v\n%s", create.Err, create.Output)
	}
	return strings.TrimSpace(create.Output), nil
}

// discardSuperpowersWorktree removes the run's worktree and deletes its
// branch, both run from the MAIN repo directory (run.RepoDir) — the
// "discard" finish option from finishing-a-development-branch. It is a
// stricter sibling of cleanupSuperpowersWorktree (superpowers_worktree.go:89):
// that helper silently no-ops on an empty/main-repo WorktreePath (a
// best-effort cleanup call), whereas this action-backing function is a HARD
// GUARD — a "discard" finish choice pointed at the main repo checkout must
// refuse outright (running zero commands) rather than silently doing
// nothing, since discard is destructive (branch deletion) and the caller
// needs to see the refusal, not a quiet no-op.
func discardSuperpowersWorktree(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	if runner == nil {
		runner = defaultSuperpowersCommandRunner
	}
	if run == nil {
		return fmt.Errorf("nil superpowers run")
	}
	if run.WorktreePath == "" || run.WorktreePath == run.RepoDir {
		return fmt.Errorf("refusing to discard: WorktreePath is empty or equals the main repo path (RepoDir=%q, WorktreePath=%q)", run.RepoDir, run.WorktreePath)
	}
	remove := runner.Run(ctx, run.RepoDir, "git", "worktree", "remove", "--force", run.WorktreePath)
	if remove.Err != nil {
		return fmt.Errorf("git worktree remove --force %s failed: %v\n%s", run.WorktreePath, remove.Err, remove.Output)
	}
	if branch := strings.TrimSpace(run.WorktreeBranch); branch != "" {
		branchDelete := runner.Run(ctx, run.RepoDir, "git", "branch", "-D", branch)
		if branchDelete.Err != nil {
			return fmt.Errorf("git branch -D %s failed: %v\n%s", branch, branchDelete.Err, branchDelete.Output)
		}
	}
	return nil
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

// verifySuperpowersRuntimeInDir runs the post-apply verification checks in
// dir — the main repo checkout in the classic flow, or the run's worktree
// when the main repo is bare (the worktree then holds exactly the tree master
// is about to fast-forward to).
func verifySuperpowersRuntimeInDir(ctx context.Context, runner CommandRunner, run *SuperpowersRun, dir string) error {
	// Prefer the canonical PATH-robust resolver (arc42 Q5: one owner) for the
	// graphify check; on resolve failure keep the historical bare-name command
	// so the apply verify's failure semantics are unchanged.
	graphifyCmd := goapFusionGraphifyTool + " update ."
	if bin, err := resolveGraphifyBin(); err == nil {
		graphifyCmd = bin + " update ."
	}
	checks := []struct {
		name string
		cmd  string
	}{
		{"main-focused-tests", "/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestGoapFusion_Structure|TestValidateOutputQuality' -timeout 180s"},
		{"main-build", "/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli"},
		{"graphify-update", graphifyCmd},
	}
	for _, check := range checks {
		res := runShellCommand(ctx, runner, dir, check.cmd)
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

// stageAndCommitSuperpowersRunInDir stages and commits the run's changes in
// dir with the standard artifact-path exclusions. It reports whether a commit
// was created; "nothing staged" is (false, nil), not an error.
func stageAndCommitSuperpowersRunInDir(ctx context.Context, runner CommandRunner, claude ClaudeRunner, run *SuperpowersRun, dir string) (bool, error) {
	add := runner.Run(ctx, dir, "git", gitStageArgs()...)
	if add.Err != nil {
		run.ApplyStatus = "applied_uncommitted"
		writeApplyCommitEvidence(run, "git add failed", add)
		return false, fmt.Errorf("applied_uncommitted: git add failed: %v\n%s", add.Err, add.Output)
	}
	staged := runShellCommand(ctx, runner, dir, "git diff --cached --quiet -- . ':!docs/superpowers/runs/**' ':!docs/superpowers/plans/**'")
	if staged.Err == nil {
		return false, nil
	}
	// When the run's verification already executed the hook-parity lint on
	// the changed packages, tell the pre-commit hook to skip its duplicate
	// golangci-lint pass (~2 min/cycle on this hardware).
	commitCmd := fmt.Sprintf("git commit -m %q", fmt.Sprintf("superpowers: apply verified run %s", run.ID))
	if runVerificationPassed(run, "changed-packages-lint") {
		commitCmd = "BT_SUPERPOWERS_PREVERIFIED=1 " + commitCmd
	}
	commit := runShellCommand(ctx, runner, dir, commitCmd)
	if commit.Err == nil {
		return true, nil
	}
	// The landing commit is hook-gated (gofmt/vet/golangci-lint/mod tidy/
	// doc-drift/short tests). Rather than abandon a verified run as
	// applied_uncommitted on the first rejection — the top refund-treadmill
	// cause — attempt a bounded auto-fix loop (deterministic reformats + a
	// Claude repair pass for residual test/vet/lint/doc failures). The failure
	// output is persisted as evidence either way (run 20260704T050842 was
	// undiagnosable without it).
	if commitFixMaxAttempts() == 0 {
		run.ApplyStatus = "applied_uncommitted"
		writeApplyCommitEvidence(run, "git commit failed (pre-commit hook?); auto-fix disabled", commit)
		return false, fmt.Errorf("applied_uncommitted: git commit failed: %v\n%s", commit.Err, commit.Output)
	}
	return commitWithAutoFix(ctx, runner, claude, run, dir, commitCmd, commit)
}

func commitAppliedSuperpowersRun(ctx context.Context, runner CommandRunner, run *SuperpowersRun) error {
	committed, err := stageAndCommitSuperpowersRunInDir(ctx, runner, defaultSuperpowersClaudeRunner, run, run.RepoDir)
	if err != nil {
		return err
	}
	if !committed {
		run.ApplyStatus = "applied_no_commit"
		return nil
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

// superpowersGeneratedPathPrefixes lists the repo-relative path prefixes that
// hold generated Superpowers/graphify artifacts (task evidence, run/plan
// bookkeeping, graphify output) rather than source changes.
// isGeneratedSuperpowersOrGraphifyPath (dirty-repo gating) and
// superpowersGeneratedCommitExclusions (commit pathspec scoping) both derive
// from this single list so they cannot drift apart.
var superpowersGeneratedPathPrefixes = []string{
	"graphify-out/",
	"docs/superpowers/runs/",
	"docs/superpowers/plans/",
}

func isGeneratedSuperpowersOrGraphifyPath(path string) bool {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "-> ")
	for _, prefix := range superpowersGeneratedPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// superpowersGeneratedCommitExclusions returns the git pathspec exclusion
// arguments (":(exclude)<prefix>**") for `git add -A`, scoped away from the
// generated paths in superpowersGeneratedPathPrefixes. Used by the per-task
// commit (SuperpowersTaskCommit action, actions_superpowers_prod.go) so it
// never stages generated Superpowers/graphify artifacts — mirroring the
// exclusion pathspecs the whole-run apply commit (commitAppliedSuperpowersRun
// below) already applies.
func superpowersGeneratedCommitExclusions() []string {
	out := make([]string, 0, len(superpowersGeneratedPathPrefixes))
	for _, prefix := range superpowersGeneratedPathPrefixes {
		out = append(out, ":(exclude)"+strings.TrimSuffix(prefix, "/")+"/**")
	}
	return out
}

// writeApplyCommitEvidence persists a failed stage/commit's full output as a
// verification artifact so applied_uncommitted runs are diagnosable.
func writeApplyCommitEvidence(run *SuperpowersRun, label string, res CommandResult) {
	if run == nil || run.ArtifactDir == "" {
		return
	}
	path := filepath.Join(run.ArtifactDir, "verification", "apply-commit.txt")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(label+"\n\n"+formatCommandResult(res)), 0o644)
}

// writeApplyRebaseEvidence persists a failed apply-time re-apply (the rebase of
// the run branch onto a moved master, plus its abort) as a verification
// artifact so a pending_patch run parked by a rebase conflict is diagnosable.
func writeApplyRebaseEvidence(run *SuperpowersRun, rebase, abort CommandResult) {
	if run == nil || run.ArtifactDir == "" {
		return
	}
	path := filepath.Join(run.ArtifactDir, "verification", "apply-rebase.txt")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	body := "rebase onto master failed:\n\n" + formatCommandResult(rebase) + "\n\nrebase --abort:\n\n" + formatCommandResult(abort)
	_ = os.WriteFile(path, []byte(body), 0o644)
}

// runVerificationPassed reports whether a named verification check ran and
// passed for this run.
func runVerificationPassed(run *SuperpowersRun, name string) bool {
	for _, v := range run.Verification {
		if v.Name == name && v.Passed {
			return true
		}
	}
	return false
}
