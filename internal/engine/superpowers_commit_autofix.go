package engine

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Commit auto-fix loop (Item 3, 2026-07-13). A verified Superpowers run that the
// git pre-commit hook rejects (gofmt / go vet / golangci-lint / go mod tidy /
// doc-drift / short tests) used to be abandoned as `applied_uncommitted` —
// the top recurring cause of the refund-treadmill. Instead we now attempt a
// bounded fix loop in the apply worktree: cheap deterministic reformats every
// pass, plus a Claude repair pass for residual test/vet/lint/doc failures, then
// re-stage and re-commit — up to commitFixMaxAttempts() times.

// commitFixMaxAttempts is the number of auto-fix + re-commit passes attempted
// when the landing commit is rejected. Default 10; override with
// BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS. 0 disables the loop entirely (commit is
// attempted once then abandoned as applied_uncommitted — the pre-2026-07-13
// behavior, kept as an escape hatch).
func commitFixMaxAttempts() int {
	if raw := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_COMMIT_FIX_ATTEMPTS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return 10
}

// withToolPath prefixes a shell command with the toolchain PATH the daemon does
// not carry by default (Go + $HOME/go/bin), so gofmt/golangci-lint resolve.
func withToolPath(cmd string) string {
	return "PATH=/usr/local/go/bin:$HOME/go/bin:$PATH " + cmd
}

// hookFailureClass names the categories of pre-commit rejection present in the
// hook output. Multiple can be true at once.
type hookFailureClass struct {
	Gofmt    bool
	ModTidy  bool
	Lint     bool // golangci-lint findings (auto-fixable subset via --fix)
	Vet      bool
	Test     bool
	DocDrift bool
}

// needsCodeFix reports whether a residual failure requires code/doc
// understanding (a Claude pass) rather than a deterministic reformat.
func (c hookFailureClass) needsCodeFix() bool {
	return c.Vet || c.Test || c.DocDrift || c.Lint
}

// summary lists the active classes for evidence/error messages.
func (c hookFailureClass) summary() string {
	var s []string
	if c.Gofmt {
		s = append(s, "gofmt")
	}
	if c.ModTidy {
		s = append(s, "mod-tidy")
	}
	if c.Lint {
		s = append(s, "lint")
	}
	if c.Vet {
		s = append(s, "vet")
	}
	if c.Test {
		s = append(s, "test")
	}
	if c.DocDrift {
		s = append(s, "doc-drift")
	}
	if len(s) == 0 {
		return "unknown"
	}
	return strings.Join(s, ",")
}

// classifyHookFailure derives the failure classes from the hook output. It is
// deliberately liberal: a false positive only costs one extra fix attempt,
// while a missed class would leave the commit stuck.
func classifyHookFailure(output string) hookFailureClass {
	l := strings.ToLower(output)
	contains := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(l, s) {
				return true
			}
		}
		return false
	}
	return hookFailureClass{
		Gofmt:    contains("gofmt", "not formatted", "needs formatting", "not gofmt"),
		ModTidy:  contains("mod tidy", "go.sum", "missing go.sum", "inconsistent vendoring"),
		Lint:     contains("golangci", "lint:"),
		Vet:      contains("go vet", "vet:"),
		Test:     contains("--- fail", "test failed", "build failed", "\tfail\t", "tests failed"),
		DocDrift: contains("doc drift", "documentation drift", "doc-drift", "drift validation"),
	}
}

// applyDeterministicCommitFixes runs the cheap, idempotent, code-preserving
// fixers whose class is present. Returns the fixers actually run (for evidence).
func applyDeterministicCommitFixes(ctx context.Context, runner CommandRunner, dir string, class hookFailureClass) []string {
	var applied []string
	if class.Gofmt {
		if res := runShellCommand(ctx, runner, dir, withToolPath("gofmt -w .")); res.Err == nil {
			applied = append(applied, "gofmt")
		}
	}
	if class.ModTidy {
		if res := runShellCommand(ctx, runner, dir, "/usr/local/go/bin/go mod tidy"); res.Err == nil {
			applied = append(applied, "mod-tidy")
		}
	}
	if class.Lint {
		// --fix applies the auto-fixable subset; anything left routes to Claude.
		_ = runShellCommand(ctx, runner, dir, withToolPath("golangci-lint run --fix ./... 2>/dev/null || true"))
		applied = append(applied, "lint-fix")
	}
	return applied
}

// buildCommitFixPrompt is the Claude repair prompt for a hook-rejected commit.
func buildCommitFixPrompt(hookOutput string) string {
	return fmt.Sprintf("You are Claude Code repairing a change that a git pre-commit hook just REJECTED.\n"+
		"The change is already applied to THIS worktree; make the pre-commit gate pass.\n\n"+
		"Pre-commit hook output (the failure to fix):\n```\n%s\n```\n\n"+
		"Fix the underlying cause in the worktree:\n"+
		"- Failing tests: fix the code (or the test, if it is genuinely wrong) so it passes.\n"+
		"- go vet / build errors: fix them.\n"+
		"- golangci-lint findings that --fix could not resolve: resolve them.\n"+
		"- Documentation drift: update the affected docs.\n"+
		"Verify with the relevant checks (e.g. /usr/local/go/bin/go test ./... , go vet ./... , gofmt -l .).\n"+
		"Do NOT run any git command and do NOT commit — the pipeline commits after you finish.",
		truncateGoap(hookOutput, 6000))
}

// commitWithAutoFix attempts to land a hook-rejected commit by fixing the cause
// and re-committing, up to commitFixMaxAttempts() passes. firstFailure is the
// already-attempted commit that was rejected. Returns (true, nil) once the
// commit lands; otherwise sets ApplyStatus=applied_uncommitted, writes evidence,
// and returns the error (mirroring the original give-up contract).
func commitWithAutoFix(ctx context.Context, runner CommandRunner, claude ClaudeRunner, run *SuperpowersRun, dir, commitCmd string, firstFailure CommandResult) (bool, error) {
	maxAttempts := commitFixMaxAttempts()
	last := firstFailure
	claudeBlocked := false
	var lastClass hookFailureClass

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastClass = classifyHookFailure(last.Output)
		applied := applyDeterministicCommitFixes(ctx, runner, dir, lastClass)

		if lastClass.needsCodeFix() && claude != nil && !claudeBlocked {
			if isClaudeRateLimit(last.Output) {
				claudeBlocked = true
			} else {
				cres := claude.RunClaude(ctx, dir, buildCommitFixPrompt(last.Output))
				if isClaudeRateLimit(cres.Output) {
					claudeBlocked = true
				} else {
					applied = append(applied, "claude-fix")
				}
			}
		}

		reAdd := stageAllExceptGenerated(ctx, runner, dir)
		if reAdd.Err != nil {
			run.ApplyStatus = "applied_uncommitted"
			writeApplyCommitEvidence(run, fmt.Sprintf("git add failed during auto-fix attempt %d/%d", attempt, maxAttempts), reAdd)
			return false, fmt.Errorf("applied_uncommitted: git add failed during auto-fix: %v\n%s", reAdd.Err, reAdd.Output)
		}

		last = runShellCommand(ctx, runner, dir, commitCmd)
		if last.Err == nil {
			return true, nil
		}

		// Nothing left to try: no deterministic fix applied this pass and Claude
		// is blocked or absent. Spinning identical attempts only wastes cycles.
		if len(applied) == 0 && (claudeBlocked || claude == nil) {
			break
		}
	}

	run.ApplyStatus = "applied_uncommitted"
	note := fmt.Sprintf("git commit still rejected after %d auto-fix attempt(s); residual classes: %s", maxAttempts, lastClass.summary())
	if claudeBlocked {
		note += " (Claude repair skipped — rate limited)"
	}
	writeApplyCommitEvidence(run, note, last)
	return false, fmt.Errorf("applied_uncommitted: %s: %v\n%s", note, last.Err, last.Output)
}
