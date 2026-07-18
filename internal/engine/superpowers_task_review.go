package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// superpowersTaskReview executes an independent code-review pass over the
// worktree's current diff for one Superpowers task: it makes a separate
// Claude call fed the task spec plus `git diff` of the worktree, and demands
// a final "VERDICT: approved" or "VERDICT: needs_work" line. The raw
// response is written to review-output.md in the task's ArtifactDir for
// durable evidence, mirroring red-claude-output.md/green-claude-output.md.
// The parsed verdict/feedback back the ReviewCycle decorator's ChainState
// protocol (review_verdict / review_feedback); an unparseable or missing
// verdict is treated as "needs_work" — see parseSuperpowersReviewVerdict.
func superpowersTaskReview(ctx context.Context, runner CommandRunner, claude ClaudeRunner, run *SuperpowersRun, task *SuperpowersTask) (verdict, feedback string, err error) {
	diffRes := runner.Run(ctx, run.WorktreePathOrRepo(), "git", "diff")
	reviewPrompt := buildSuperpowersReviewPrompt(run, *task, diffRes.Output)
	reviewClaudeRes := claude.RunClaude(ctx, run.WorktreePathOrRepo(), reviewPrompt)
	_ = os.WriteFile(filepath.Join(task.ArtifactDir, "review-output.md"), []byte(reviewClaudeRes.Output), 0o644)
	if reviewClaudeRes.Err != nil {
		if kill := superpowersBudgetKillError(ctx, "review-phase claude", reviewClaudeRes.Err, reviewClaudeRes.Output); kill != nil {
			return "needs_work", "", kill
		}
		return "needs_work", "", fmt.Errorf("review-phase claude failed: %v\n%s", reviewClaudeRes.Err, reviewClaudeRes.Output)
	}
	verdict, feedback = parseSuperpowersReviewVerdict(reviewClaudeRes.Output)
	return verdict, feedback, nil
}

func buildSuperpowersReviewPrompt(run *SuperpowersRun, task SuperpowersTask, diff string) string {
	return fmt.Sprintf(`You are Claude Code performing an independent code review of one Superpowers SDLC task's implementation.

Repo: %s
Task %d: %s
Objective: %s
Files: %s
Tests: %s

Worktree diff to review:
---
%s
---

Review rules:
- Check the diff against the task's objective, files, and tests.
- Verify tests were added/updated appropriately and production code is minimal and correct.
- Flag any regressions, scope creep, or missed edge cases as plain-text feedback.
- End your response with a final line, exactly one of:
  VERDICT: approved
  VERDICT: needs_work
`, run.WorktreePathOrRepo(), task.Index, task.Title, task.Objective, strings.Join(task.Files, ", "), strings.Join(task.Tests, "; "), truncateGoap(diff, 4000))
}

// parseSuperpowersReviewVerdict extracts the review verdict and feedback text
// from a SuperpowersTaskReview Claude response. It scans from the end of the
// output for the last line beginning with "VERDICT:" (case-insensitive); the
// value on that line selects "approved" or "needs_work", and everything
// before that line (trimmed) becomes the feedback. A missing VERDICT line, or
// a value that is neither "approved" nor "needs_work", is unparseable and
// defaults to "needs_work" — the safe default per the ReviewCycle decorator's
// contract, since silently approving on ambiguous output could ship a task
// that was never actually reviewed.
func parseSuperpowersReviewVerdict(output string) (verdict, feedback string) {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		lower := strings.ToLower(trimmed)
		if !strings.HasPrefix(lower, "verdict:") {
			continue
		}
		value := strings.ToLower(strings.TrimSpace(trimmed[len("verdict:"):]))
		switch {
		case strings.Contains(value, "approved"):
			verdict = "approved"
		case strings.Contains(value, "needs_work"), strings.Contains(value, "needs work"):
			verdict = "needs_work"
		default:
			verdict = "needs_work"
		}
		feedback = strings.TrimSpace(strings.Join(lines[:i], "\n"))
		return verdict, feedback
	}
	return "needs_work", strings.TrimSpace(output)
}
