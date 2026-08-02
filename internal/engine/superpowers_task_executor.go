package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// superpowersBudgetKillError converts a claude-phase failure that happened
// after the run's budget context died into the "cycle budget exhausted"
// infrastructure marker goapInfraResultMarkers refunds — the subprocess was
// SIGKILLed by the deadline, which is no evidence against the milestone.
// Returns nil while the context is alive: the failure is then the phase's own
// and the caller reports its genuine error. (2026-07-18: nine consecutive
// cycles were killed mid GREEN-claude at the run budget, the generic
// "green-phase claude failed: signal: killed" classified genuine, and
// milestones 8e47518e:0-2 were wrongly blocked.)
func superpowersBudgetKillError(ctx context.Context, phase string, err error, output string) error {
	if ctx.Err() == nil {
		return nil
	}
	return fmt.Errorf("%s aborted: cycle budget exhausted (%v)\nerror: %v\n%s", phase, ctx.Err(), err, output)
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
		if kill := superpowersBudgetKillError(ctx, "red-phase claude", redClaudeRes.Err, redClaudeRes.Output); kill != nil {
			return kill
		}
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
	// A RED command killed by the dead cycle budget looks exactly like the
	// legit "RED correctly failed" outcome (non-nil Err) — without this guard
	// the executor marches into a doomed GREEN claude call on a dead context.
	if redRes.Err != nil && ctx.Err() != nil {
		task.Status = "failed"
		return fmt.Errorf("task RED verification aborted: cycle budget exhausted (%v) during: %s\nerror: %v\n%s", ctx.Err(), redCmd, redRes.Err, redRes.Output)
	}
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
		if kill := superpowersBudgetKillError(ctx, "green-phase claude", greenClaudeRes.Err, greenClaudeRes.Output); kill != nil {
			return kill
		}
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
			// A verification process killed because the CYCLE's own deadline
			// expired is no evidence against the milestone. Without this
			// branch the kill surfaced as a generic GREEN failure and charged
			// the milestone-abandon budget (2026-07-18, run 20260718T164339:
			// task 2's engine suite was SIGKILLed 28s in at the 59-minute
			// cycle mark, after task 1 had passed the identical suite twice).
			// goapInfraResultMarkers classifies this marker as infrastructure
			// so the cycle refunds the attempt instead.
			if ctx.Err() != nil {
				return fmt.Errorf("task GREEN verification aborted: cycle budget exhausted (%v) during: %s\nerror: %v\n%s", ctx.Err(), cmd, res.Err, res.Output)
			}
			return fmt.Errorf("task GREEN verification failed: %s\nerror: %v\n%s", cmd, res.Err, res.Output)
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
	return executeSuperpowersTaskBatch(ctx, executor, run)
}

// executeSuperpowersTaskBatch runs the plan's tasks with PARTIAL-LANDING
// semantics: each completed task is snapshot-committed in the run worktree;
// when a later task fails, the failed task's edits are discarded
// (reset --hard + clean), remaining tasks are skipped, and the snapshots are
// mixed-reset back to the pre-batch base so the completed tasks' verified
// work sits uncommitted in the worktree — exactly the shape the verify and
// apply stages expect. The failed goal is NOT recorded as implemented, so
// the next research cycle re-proposes it (carry-forward). All-or-nothing is
// preserved when the FIRST task fails or when snapshots are unavailable
// (dry-run, main-repo mode, or a snapshot command failure).
//
// Regression context: before this mode, one failed task discarded every
// completed task's verified work — cycle 20260704T023012 lost tasks 1-2
// because task 3 failed, then a later cycle had to redo them.
func executeSuperpowersTaskBatch(ctx context.Context, executor SuperpowersTaskExecutor, run *SuperpowersRun) error {
	if executor.Runner == nil {
		executor.Runner = defaultSuperpowersCommandRunner
	}
	snapshots := run.Mode == SuperpowersModeApply && run.WorktreePath != "" && run.WorktreePath != run.RepoDir
	base := ""
	if snapshots {
		res := executor.Runner.Run(ctx, run.WorktreePath, "git", "rev-parse", "HEAD")
		if res.Err != nil {
			snapshots = false
		} else {
			base = strings.TrimSpace(res.Output)
		}
	}
	completed := 0
	for i := range run.Tasks {
		if reason, insufficient := superpowersTaskBudgetInsufficient(ctx, run.Tasks[i]); insufficient {
			// The remaining cycle budget can no longer safely cover this
			// task's own verification commands: starting its RED phase now
			// risks the cycle's outer deadline SIGKILLing a test process
			// mid-run, which only gets classified as a kill AFTER the fact
			// (superpowersBudgetKillError). Stop cleanly instead — mark this
			// and every later task skipped, and unwrap any snapshots already
			// committed so completed work still lands. Regression: run
			// 20260718T164339 lost a green milestone to exactly this
			// mid-flight SIGKILL.
			for j := i; j < len(run.Tasks); j++ {
				run.Tasks[j].Status = "skipped"
			}
			run.PartialFailure = fmt.Sprintf("task %d %q stopped before RED phase: batch-stopped-insufficient-budget (%s)", run.Tasks[i].Index, run.Tasks[i].Title, reason)
			if base != "" {
				executor.Runner.Run(ctx, run.WorktreePath, "git", "reset", base)
			}
			_ = writeSuperpowersRunJSON(run)
			return nil
		}
		task, err := executor.ExecuteTask(ctx, run, run.Tasks[i])
		run.Tasks[i] = task
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			if completed == 0 || !snapshots {
				return err
			}
			// Partial landing: drop the failed task's edits (tracked and
			// untracked), skip the rest, and unwrap the snapshots so the
			// completed work is uncommitted again. Every recovery command's
			// result is checked: if `git reset --hard HEAD` fails, the failed
			// task's broken edits survive the subsequent mixed `git reset base`
			// and land mixed into the completed work as a bogus "success". If
			// any cleanup step fails, abort to all-or-nothing by returning the
			// original task error — the worktree is not in the clean
			// partial-landing shape, so no partial success may be reported.
			cleanupSteps := [][]string{
				{"reset", "--hard", "HEAD"},
				{"clean", "-fd"},
				{"reset", base},
			}
			for _, step := range cleanupSteps {
				if res := executor.Runner.Run(ctx, run.WorktreePath, "git", step...); res.Err != nil {
					return err
				}
			}
			for j := i + 1; j < len(run.Tasks); j++ {
				run.Tasks[j].Status = "skipped"
			}
			run.PartialFailure = fmt.Sprintf("task %d %q failed and was carried forward for a future cycle: %v", task.Index, task.Title, err)
			_ = writeSuperpowersRunJSON(run)
			return nil
		}
		if snapshots {
			executor.Runner.Run(ctx, run.WorktreePath, "git", "add", "-A")
			commit := executor.Runner.Run(ctx, run.WorktreePath, "git", "commit", "--no-verify", "-m", fmt.Sprintf("superpowers snapshot: task %d", task.Index))
			if commit.Err != nil {
				// Without a snapshot for this task, a later partial landing
				// could not separate its work from a failed task's — degrade
				// to all-or-nothing for the rest of the batch.
				snapshots = false
			}
		}
		completed++
	}
	if base != "" {
		// Unwrap all snapshots: worktree keeps every change, uncommitted.
		// Keyed on whether a snapshot base was ever established (base != ""),
		// NOT the live `snapshots` flag — a mid-batch snapshot-commit failure
		// disables the flag but leaves earlier tasks committed above base, and
		// those must still be unwrapped or the git-diff apply stage drops them.
		executor.Runner.Run(ctx, run.WorktreePath, "git", "reset", base)
	}
	return nil
}

// superpowersTaskBudgetMultiplier is the safety margin applied to a task's
// own largest -timeout value when checking whether the remaining
// cycle-context budget can still safely cover its verification commands.
// 2x leaves room for process startup and the difference between a test
// binary's own -timeout expiring cleanly and the outer cycle deadline
// SIGKILLing it mid-run — a close call is treated as insufficient rather
// than gambling on that race (2026-07-18 run 20260718T164339 lost a green
// milestone to exactly that SIGKILL).
const superpowersTaskBudgetMultiplier = 2

var superpowersTestTimeoutPattern = regexp.MustCompile(`-timeout[= ](\S+)`)

// superpowersTaskMinBudget derives the minimum context budget required to
// safely start task's RED phase from its own verification commands:
// superpowersTaskBudgetMultiplier x the largest -timeout found among them.
// Returns 0 when no command carries a parseable -timeout — there is then
// nothing safe to compare the remaining budget against, so the task is not
// gated.
func superpowersTaskMinBudget(task SuperpowersTask) time.Duration {
	var maxTimeout time.Duration
	for _, cmd := range task.Tests {
		m := superpowersTestTimeoutPattern.FindStringSubmatch(cmd)
		if m == nil {
			continue
		}
		d, err := time.ParseDuration(m[1])
		if err != nil || d <= maxTimeout {
			continue
		}
		maxTimeout = d
	}
	if maxTimeout <= 0 {
		return 0
	}
	return superpowersTaskBudgetMultiplier * maxTimeout
}

// superpowersTaskBudgetInsufficient reports whether the remaining
// cycle-context budget can no longer safely cover task's own verification
// commands, so its RED phase must never be STARTED. A context with no
// deadline, or a task whose commands carry no parseable -timeout, is never
// gated (superpowersTaskMinBudget returns 0). When insufficient, the
// returned string names the shortfall for the batch's PartialFailure note.
func superpowersTaskBudgetInsufficient(ctx context.Context, task SuperpowersTask) (string, bool) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return "", false
	}
	required := superpowersTaskMinBudget(task)
	if required <= 0 {
		return "", false
	}
	remaining := time.Until(deadline)
	if remaining >= required {
		return "", false
	}
	return fmt.Sprintf("remaining budget %s < required %s", remaining.Round(time.Second), required.Round(time.Second)), true
}

// superpowersVerificationCheck is one named verification command; the run
// fails on the first check whose command exits non-zero.
type superpowersVerificationCheck struct {
	name string
	cmd  string
}

// buildSuperpowersVerificationChecks assembles the run's verification suite.
func buildSuperpowersVerificationChecks(run *SuperpowersRun) []superpowersVerificationCheck {
	checks := []superpowersVerificationCheck{
		{"focused-tests", "/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestGoapFusion_Structure|TestValidateOutputQuality' -timeout 180s"},
		{"build", "/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli"},
	}
	// Verification scales with the run's blast radius: bigger goal-driven
	// runs touch more packages, and every touched package gets its full
	// suite — not just the fixed contract set above.
	if cmd := changedPackagesTestCommand(run.ChangedFiles); cmd != "" {
		checks = append(checks, superpowersVerificationCheck{"changed-packages-tests", cmd})
	}
	// Lint parity with the hook-gated landing commit: catch lint failures
	// here, with evidence, instead of at the final commit.
	if cmd := changedPackagesLintCommand(run.ChangedFiles); cmd != "" {
		checks = append(checks, superpowersVerificationCheck{"changed-packages-lint", cmd})
	}
	// Documentation parity: the trees own the docs. A run that leaves the
	// drift-checked documentation inconsistent with the code FAILS here —
	// syncDriftDocs (which runs before verification) is the writer that
	// satisfies this gate; this check is what keeps it honest.
	if _, err := os.Stat(filepath.Join(run.WorktreePathOrRepo(), docDriftScriptRelPath)); err == nil {
		checks = append(checks, superpowersVerificationCheck{"doc-drift", "bash " + docDriftScriptRelPath})
	}
	return checks
}

func VerifySuperpowersRunRuntime(ctx context.Context, run *SuperpowersRun) error {
	run.Phase = SuperpowersPhaseVerification
	// Persist the phase BEFORE the first check runs. Verification is the
	// longest, most kill-prone stage of a cycle — a dead cycle budget or a
	// crash mid-suite is routine — and a run.json still advertising the
	// pre-verification phase misreports what the run was doing to the
	// dashboard and to crash recovery.
	_ = writeSuperpowersRunJSON(run)
	for _, check := range buildSuperpowersVerificationChecks(run) {
		res := runShellCommand(ctx, defaultSuperpowersCommandRunner, run.WorktreePathOrRepo(), check.cmd)
		recordSuperpowersVerification(run, check.name, check.cmd, res)
		if res.Err == nil {
			continue
		}
		// A changed-packages-lint failure gets ONE machine remediation before
		// failing the run: staticcheck's QF class ships applicable fixes, and
		// a single auto-fixable finding once stranded a full implementation
		// cycle (QF1008, 2026-07-15 22:29 — all three self-healing-envelope
		// milestones lost to one redundant selector). The autofixed files are
		// committed by the apply stage's blanket git add.
		if check.name == "changed-packages-lint" {
			if fixCmd := changedPackagesLintFixCommand(run.ChangedFiles); fixCmd != "" {
				fixRes := runShellCommand(ctx, defaultSuperpowersCommandRunner, run.WorktreePathOrRepo(), fixCmd)
				recordSuperpowersVerification(run, "changed-packages-lint-autofix", fixCmd, fixRes)
				retry := runShellCommand(ctx, defaultSuperpowersCommandRunner, run.WorktreePathOrRepo(), check.cmd)
				recordSuperpowersVerification(run, "changed-packages-lint-retry", check.cmd, retry)
				if retry.Err == nil {
					continue
				}
				// Deterministic --fix left findings it cannot repair (errcheck,
				// prealloc, revive empty-block, ...). Before discarding a
				// fully-tested cycle as "degraded", make ONE bounded,
				// rate-limit-guarded Claude self-correct pass that fixes the root
				// cause of each finding — mirroring commitWithAutoFix's Claude
				// repair. Bails cleanly to today's failure on rate limit, tight
				// ctx deadline, nil runner, or when disabled.
				finalRes, passed := claudeRepairVerifyLint(ctx, run, check.cmd, fixCmd, retry)
				if passed {
					continue
				}
				res = finalRes
			}
		}
		// A check killed by the dead cycle budget (not by its own findings)
		// carries the refund marker, mirroring superpowersTaskVerifyGreen.
		if ctx.Err() != nil {
			return fmt.Errorf("verification %s aborted: cycle budget exhausted (%v) during: %s\nerror: %v\n%s", check.name, ctx.Err(), check.cmd, res.Err, res.Output)
		}
		return fmt.Errorf("verification %s failed: %v\n%s", check.name, res.Err, res.Output)
	}
	return writeSuperpowersRunJSON(run)
}

// verifyLintFixMaxAttempts caps the bounded Claude self-correct passes attempted
// when a changed-packages-lint verification fails on findings golangci-lint
// --fix cannot repair (errcheck, prealloc, revive empty-block, ...). Default 1;
// 0 disables the Claude pass entirely (escape hatch back to today's
// deterministic --fix-only behavior). Override with
// BT_SUPERPOWERS_VERIFY_LINT_FIX_ATTEMPTS.
func verifyLintFixMaxAttempts() int {
	if raw := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_VERIFY_LINT_FIX_ATTEMPTS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return 1
}

// buildVerifyLintFixPrompt is the Claude repair prompt for a changed-packages
// verification-lint failure that `golangci-lint run --fix` could NOT resolve.
// It is deliberately lint-scoped and distinct from buildCommitFixPrompt (which
// is pre-commit-hook scoped): the goal is to fix the ROOT CAUSE of every finding
// by improving the code, never to suppress it or weaken behavior.
func buildVerifyLintFixPrompt(lintOutput string) string {
	return fmt.Sprintf("You are Claude Code repairing golangci-lint findings that `golangci-lint run --fix` could NOT auto-fix.\n"+
		"The change is already applied to THIS worktree; make the linter pass by improving the code.\n\n"+
		"golangci-lint output (the findings to fix):\n```\n%s\n```\n\n"+
		"Fix the ROOT CAUSE of every finding by improving the code:\n"+
		"- errcheck (unchecked error): actually handle the returned error — inspect it and act on it; do not discard it.\n"+
		"- prealloc: preallocate the slice with its known capacity.\n"+
		"- revive empty-block: remove the empty block, or fill it with the behavior it is missing.\n"+
		"- every other finding: fix exactly what the linter points at, by making the code correct.\n\n"+
		"STRICT RULES — you MUST NOT weaken the code to appease the linter:\n"+
		"- Do NOT add //nolint directives or any lint-suppression comment.\n"+
		"- Do NOT delete the error check, and do NOT remove or neuter the behavior/code that triggered the finding.\n"+
		"- Do NOT edit .golangci.yml or otherwise disable/relax any linter.\n"+
		"- Do NOT run git, do NOT commit, and do NOT write outside the source files — the pipeline stages and commits after you finish.\n\n"+
		"Verify with the affected packages, e.g. /usr/local/go/bin/go build ./... and /usr/local/go/bin/go test on the changed packages.",
		truncateGoap(lintOutput, 6000))
}

// claudeRepairVerifyLint makes ONE bounded, rate-limit-guarded Claude
// self-correct pass over a changed-packages-lint failure that
// `golangci-lint run --fix` could not resolve. On success the fully-tested
// cycle lands instead of degrading. It bails cleanly to the caller's existing
// failure (ok=false) on any of: the pass disabled (verifyLintFixMaxAttempts()
// <= 0), a nil runner, a rate-limited signal (in the lint output or in Claude's
// own output), or a ctx deadline too tight to finish a ~180s Claude call — so
// the worst case is a graceful degrade to today's deterministic behavior, never
// a hang.
//
// Returns (finalLintResult, passed). When passed, the plain lint retry after
// Claude's edits is green and verification may proceed. When !passed,
// finalLintResult is the result the caller should report as the failure: the
// pre-Claude retry when the pass was skipped, else the post-Claude retry.
func claudeRepairVerifyLint(ctx context.Context, run *SuperpowersRun, lintCmd, fixCmd string, retry CommandResult) (CommandResult, bool) {
	if verifyLintFixMaxAttempts() <= 0 {
		return retry, false
	}
	if defaultSuperpowersClaudeRunner == nil {
		return retry, false
	}
	if isClaudeRateLimit(retry.Output) {
		return retry, false
	}
	// A Claude call can take ~180s; never start one that cannot finish within the
	// remaining budget — bail to the existing failure instead of blocking.
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 200*time.Second {
		return retry, false
	}

	dir := run.WorktreePathOrRepo()
	claudeRes := defaultSuperpowersClaudeRunner.RunClaude(ctx, dir, buildVerifyLintFixPrompt(retry.Output))
	recordSuperpowersVerification(run, "changed-packages-lint-claude-fix", claudeRes.Command, claudeRes)
	if isClaudeRateLimit(claudeRes.Output) {
		return retry, false
	}

	// Fold in any newly auto-fixable state Claude's edits created, then re-run the
	// plain lint gate.
	refix := runShellCommand(ctx, defaultSuperpowersCommandRunner, dir, fixCmd)
	recordSuperpowersVerification(run, "changed-packages-lint-claude-refix", fixCmd, refix)
	claudeRetry := runShellCommand(ctx, defaultSuperpowersCommandRunner, dir, lintCmd)
	recordSuperpowersVerification(run, "changed-packages-lint-claude-retry", lintCmd, claudeRetry)
	return claudeRetry, claudeRetry.Err == nil
}

// recordSuperpowersVerification appends one check result to the run record and
// its evidence artifact, then persists the run so the trail survives the
// failure paths. Verification's error returns are the norm, not the exception:
// batching the run.json write to the all-green return left every failed run's
// evidence in memory only, and the per-check .txt artifacts cannot reconstruct
// it — they carry neither the phase nor the order the checks ran in.
func recordSuperpowersVerification(run *SuperpowersRun, name, cmd string, res CommandResult) {
	vc := VerificationCheck{Name: name, Command: cmd, Passed: res.Err == nil, Output: res.Output, Duration: res.Duration.String()}
	run.Verification = append(run.Verification, vc)
	_ = os.WriteFile(filepath.Join(run.ArtifactDir, "verification", name+".txt"), []byte(formatCommandResult(res)), 0o644)
	_ = writeSuperpowersRunJSON(run)
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
