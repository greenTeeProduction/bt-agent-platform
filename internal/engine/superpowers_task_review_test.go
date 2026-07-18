package engine

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// fixedDiffRunner is a minimal CommandRunner fake that returns a fixed
// `git diff` style output regardless of the command invoked, used to prove
// SuperpowersTaskReview feeds the worktree diff into its review prompt.
type fixedDiffRunner struct {
	diff string
}

func (r *fixedDiffRunner) Run(_ context.Context, dir string, name string, _ ...string) CommandResult {
	return CommandResult{Command: name, Dir: dir, Output: r.diff}
}

// fakeSuperpowersReviewRunner is a minimal ClaudeRunner fake that records every
// prompt it receives and always returns a fixed canned response.
type fakeSuperpowersReviewRunner struct {
	response string
	prompts  []string
}

func (r *fakeSuperpowersReviewRunner) RunClaude(_ context.Context, repoDir string, prompt string) CommandResult {
	r.prompts = append(r.prompts, prompt)
	return CommandResult{Command: "claude <prompt>", Dir: repoDir, Output: r.response}
}

// TestParseSuperpowersReviewVerdict covers the verdict-parsing contract:
// "approved" and "needs_work" verdicts parse cleanly, and any unparseable or
// missing VERDICT line (garbage) defaults to "needs_work" — the safe default
// per the ReviewCycle decorator's protocol.
func TestParseSuperpowersReviewVerdict(t *testing.T) {
	cases := []struct {
		name         string
		output       string
		wantVerdict  string
		wantFeedback string
	}{
		{
			name:        "approved",
			output:      "Looks great, all tests pass and the diff is minimal.\n\nVERDICT: approved",
			wantVerdict: "approved",
		},
		{
			name:         "needs_work",
			output:       "Tighten error handling around nil pointers before merging.\n\nVERDICT: needs_work",
			wantVerdict:  "needs_work",
			wantFeedback: "Tighten error handling around nil pointers before merging.",
		},
		{
			name:        "garbage_missing_verdict_line",
			output:      "This response forgot the required VERDICT schema entirely.",
			wantVerdict: "needs_work",
		},
		{
			name:        "garbage_unrecognized_value",
			output:      "Not sure how to grade this.\n\nVERDICT: maybe later",
			wantVerdict: "needs_work",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verdict, feedback := parseSuperpowersReviewVerdict(tc.output)
			if verdict != tc.wantVerdict {
				t.Fatalf("verdict = %q, want %q", verdict, tc.wantVerdict)
			}
			if tc.wantFeedback != "" && !strings.Contains(feedback, tc.wantFeedback) {
				t.Fatalf("feedback = %q, want contains %q", feedback, tc.wantFeedback)
			}
		})
	}
}

// TestSuperpowersTaskReviewAction_SetsChainStateFromVerdict proves the
// registered SuperpowersTaskReview action makes a separate Claude call (fed
// the worktree `git diff`) and writes the parsed verdict/feedback onto
// ChainState["review_verdict"]/["review_feedback"] — the protocol the
// ReviewCycle decorator (Task 7 Steps 1-5) reads to decide whether to
// re-run the child or approve.
func TestSuperpowersTaskReviewAction_SetsChainStateFromVerdict(t *testing.T) {
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})

	cases := []struct {
		name         string
		claudeOutput string
		wantVerdict  string
	}{
		{name: "approved", claudeOutput: "All good, ship it.\n\nVERDICT: approved", wantVerdict: "approved"},
		{name: "needs_work", claudeOutput: "Add a nil check.\n\nVERDICT: needs_work", wantVerdict: "needs_work"},
	}

	act := GetAction("SuperpowersTaskReview")
	if act == nil {
		t.Fatal("SuperpowersTaskReview not registered")
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defaultSuperpowersCommandRunner = &fixedDiffRunner{diff: "diff --git a/foo.go b/foo.go\n+added line\n"}
			claude := &fakeSuperpowersReviewRunner{response: tc.claudeOutput}
			defaultSuperpowersClaudeRunner = claude

			run := &SuperpowersRun{
				ID:           "run-review-" + tc.name,
				Mode:         SuperpowersModeApply,
				RepoDir:      t.TempDir(),
				WorktreePath: t.TempDir(),
				ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
				Tasks: []SuperpowersTask{
					{Index: 1, Title: "Reviewed task", Tests: []string{"true"}},
				},
			}
			bb := newTestBlackboard()
			setSuperpowersRun(bb, run)
			bb.ChainState["superpowers_task_index"] = 0

			if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
				t.Fatalf("SuperpowersTaskReview result = %d, want SUCCESS; bb.Result=%s", result, bb.Result)
			}
			if got, _ := bb.ChainState["review_verdict"].(string); got != tc.wantVerdict {
				t.Fatalf("review_verdict = %q, want %q", got, tc.wantVerdict)
			}
			if len(claude.prompts) != 1 {
				t.Fatalf("expected exactly one separate Claude review call, got %d", len(claude.prompts))
			}
			if !strings.Contains(claude.prompts[0], "diff --git a/foo.go") {
				t.Fatalf("review prompt missing worktree git diff: %s", claude.prompts[0])
			}
			if !strings.Contains(claude.prompts[0], "Reviewed task") {
				t.Fatalf("review prompt missing task spec: %s", claude.prompts[0])
			}
		})
	}
}

// TestSuperpowersTaskGreenAction_InjectsReviewFeedbackWhenPresent proves that
// when ChainState["review_feedback"] is non-empty (set by a prior
// SuperpowersTaskReview "needs_work" verdict), the SuperpowersTaskGreen
// action appends it to the GREEN prompt as "Address this review feedback:".
func TestSuperpowersTaskGreenAction_InjectsReviewFeedbackWhenPresent(t *testing.T) {
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	defaultSuperpowersCommandRunner = &fixedDiffRunner{}
	claude := &fakeSuperpowersReviewRunner{response: "GREEN output"}
	defaultSuperpowersClaudeRunner = claude

	run := &SuperpowersRun{
		ID:           "run-green-feedback",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "Green with feedback", Tests: []string{"true"}},
		},
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["superpowers_task_index"] = 0
	bb.ChainState["review_feedback"] = "tighten error handling"

	act := GetAction("SuperpowersTaskGreen")
	if act == nil {
		t.Fatal("SuperpowersTaskGreen not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("SuperpowersTaskGreen result = %d, want SUCCESS; bb.Result=%s", result, bb.Result)
	}
	if len(claude.prompts) != 1 {
		t.Fatalf("expected exactly one GREEN Claude call, got %d", len(claude.prompts))
	}
	if !strings.Contains(claude.prompts[0], "Address this review feedback: tighten error handling") {
		t.Fatalf("GREEN prompt missing injected review feedback: %s", claude.prompts[0])
	}
}

// TestSuperpowersTaskGreenAction_NoFeedback_PromptUnchanged proves the
// injection is opt-in: with no review_feedback on ChainState, the GREEN
// prompt is unchanged (existing SuperpowersTaskExecutor tests must keep
// passing with the old, feedback-less prompt shape).
func TestSuperpowersTaskGreenAction_NoFeedback_PromptUnchanged(t *testing.T) {
	t.Chdir(t.TempDir())

	prevRunner, prevClaude := defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner
	t.Cleanup(func() {
		defaultSuperpowersCommandRunner = prevRunner
		defaultSuperpowersClaudeRunner = prevClaude
	})
	defaultSuperpowersCommandRunner = &fixedDiffRunner{}
	claude := &fakeSuperpowersReviewRunner{response: "GREEN output"}
	defaultSuperpowersClaudeRunner = claude

	run := &SuperpowersRun{
		ID:           "run-green-no-feedback",
		Mode:         SuperpowersModeApply,
		RepoDir:      t.TempDir(),
		WorktreePath: t.TempDir(),
		ArtifactDir:  filepath.Join(t.TempDir(), "artifacts"),
		Tasks: []SuperpowersTask{
			{Index: 1, Title: "Green without feedback", Tests: []string{"true"}},
		},
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	bb.ChainState["superpowers_task_index"] = 0

	act := GetAction("SuperpowersTaskGreen")
	if act == nil {
		t.Fatal("SuperpowersTaskGreen not registered")
	}
	if result := act(&btcore.BTContext[Blackboard]{Blackboard: bb}); result != 1 {
		t.Fatalf("SuperpowersTaskGreen result = %d, want SUCCESS; bb.Result=%s", result, bb.Result)
	}
	if len(claude.prompts) != 1 {
		t.Fatalf("expected exactly one GREEN Claude call, got %d", len(claude.prompts))
	}
	if strings.Contains(claude.prompts[0], "Address this review feedback:") {
		t.Fatalf("GREEN prompt should not inject feedback when none is set: %s", claude.prompts[0])
	}
}

// A review-phase claude call killed by the dead cycle budget must carry the
// "cycle budget exhausted" marker (goapInfraResultMarkers refunds it); a
// live-ctx claude failure keeps the legacy message. The verdict stays
// needs_work either way — a killed review must never approve.
func TestSuperpowersTaskReviewBudgetExhaustedMarker(t *testing.T) {
	run := &SuperpowersRun{WorktreePath: t.TempDir()}

	expired, cancel := context.WithCancel(context.Background())
	cancel()
	task := &SuperpowersTask{ArtifactDir: t.TempDir()}
	verdict, _, err := superpowersTaskReview(expired, &fixedDiffRunner{diff: "diff --git a/x b/x"}, killedClaudeRunner{errors.New("signal: killed")}, run, task)
	if err == nil || !strings.Contains(err.Error(), "cycle budget exhausted") {
		t.Fatalf("expired ctx: want budget-exhausted marker, got %v", err)
	}
	if verdict != "needs_work" {
		t.Fatalf("verdict = %q, want needs_work", verdict)
	}

	task2 := &SuperpowersTask{ArtifactDir: t.TempDir()}
	verdict2, _, err2 := superpowersTaskReview(context.Background(), &fixedDiffRunner{diff: "diff --git a/x b/x"}, killedClaudeRunner{errors.New("exit status 1")}, run, task2)
	if err2 == nil || strings.Contains(err2.Error(), "cycle budget exhausted") {
		t.Fatalf("live ctx: must stay a plain claude failure, got %v", err2)
	}
	if !strings.Contains(err2.Error(), "review-phase claude failed") || verdict2 != "needs_work" {
		t.Fatalf("live ctx: legacy message + needs_work verdict required, got verdict=%q err=%v", verdict2, err2)
	}
}
