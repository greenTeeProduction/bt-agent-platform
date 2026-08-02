package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 2026-08-01, from ~/.go-bt-evolve/history/self-review.jsonl: EVERY recorded
// self-review run — including the four that seeded real code-fix programs
// (2026-07-23, -24, -28, -30) — stored the output
//
//	## Self-Review Skipped
//	No new autonomous commits since <SHA>.
//
// with outcome=no_change, quality=0.5, and a multi-MINUTE duration. The quoted
// SHA is the one that same run had just advanced to. So the action runs twice
// per scheduled run (the scheduler retries the attempt), the second pass sees
// the SHA its own first pass advanced, reports "Skipped", and overwrites the
// real report. The agent that produced every confirmed defect the fleet fixed
// looked inert in every log line, dashboard and notification for two weeks.
//
// The 2026-07-23 QualityAuthoritative stamp was aimed at this and does not
// reach it: it fixes the recorded SCORE, not the second pass erasing the report.

type srReportRunner struct {
	out   string
	calls *int
}

func (r srReportRunner) RunClaude(ctx context.Context, dir, prompt string) CommandResult {
	if r.calls != nil {
		*r.calls++
	}
	return CommandResult{Output: r.out}
}

func srReportDeps(t *testing.T, dir, claudeOut string, calls *int, commitLog string) selfReviewDeps {
	t.Helper()
	return selfReviewDeps{
		runner:   srReportRunner{out: claudeOut, calls: calls},
		repoDir:  dir,
		stateDir: dir,
		timeout:  time.Minute,
		now:      time.Now,
		commitScanner: func(repoDir, lastSHA string) (string, string, string, string, error) {
			if lastSHA == "headsha" {
				return "", "", "headsha", "", nil // nothing new since our own advance
			}
			return commitLog, "diff --git a/x b/x", "headsha", "old..headsha", nil
		},
	}
}

const srOneFinding = `[{"title":"Fleet-PR branch/state writes race outside prShepherdMu",` +
	`"milestone":"In internal/engine/actions_pr_shepherd.go guard the state write with prShepherdMu",` +
	`"files":["internal/engine/actions_pr_shepherd.go"],"severity":"high","signature":"race-1"}]`

// The second pass of the SAME scheduled run must not erase the first pass's
// review. It re-emits what was actually found instead of claiming a skip.
func TestSelfReview_RetriedPassDoesNotEraseTheCompletedReview(t *testing.T) {
	dir := t.TempDir()
	isolateSRReportStores(t, dir)
	calls := 0
	deps := srReportDeps(t, dir, srOneFinding, &calls, "abc superpowers: apply verified run 1")

	first := &Blackboard{ChainState: map[string]any{}}
	if got := runSelfReview(first, deps); got != 1 {
		t.Fatalf("first pass status = %d", got)
	}
	if !strings.Contains(first.Result, "Self-Review Complete") {
		t.Fatalf("first pass did not review: %s", first.Result)
	}

	second := &Blackboard{ChainState: map[string]any{}}
	if got := runSelfReview(second, deps); got != 1 {
		t.Fatalf("second pass status = %d", got)
	}

	if strings.Contains(second.Result, "Skipped") {
		t.Fatalf("the retried pass reported a SKIP and erased the completed review:\n%s\n"+
			"this is what made every seeding run record as no_change/0.5", second.Result)
	}
	if second.OutcomeRefinement == "no_change" {
		t.Fatal("a run whose review seeded a code-fix program must not be recorded as no_change — " +
			"it is real work and must reach the operator's notifications")
	}
	if !strings.Contains(second.Result, "Fleet-PR branch/state writes race") {
		t.Fatalf("the re-emitted report lost the finding:\n%s", second.Result)
	}
	if calls != 1 {
		t.Fatalf("Claude was invoked %d times; the retried pass must reuse the completed review, not re-review", calls)
	}
}

// A genuinely up-to-date run — no recent review to re-emit — still reports the
// healthy skip, so the notification throttle keeps suppressing quiet days.
func TestSelfReview_GenuinelyUpToDateStillReportsHealthySkip(t *testing.T) {
	dir := t.TempDir()
	isolateSRReportStores(t, dir)
	deps := srReportDeps(t, dir, srOneFinding, nil, "")
	deps.commitScanner = func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "", "", "headsha", "", nil
	}

	bb := &Blackboard{ChainState: map[string]any{}}
	if got := runSelfReview(bb, deps); got != 1 {
		t.Fatalf("status = %d", got)
	}
	if !strings.Contains(bb.Result, "Skipped") {
		t.Fatalf("a genuine no-op must still read as a skip: %s", bb.Result)
	}
	if bb.OutcomeRefinement != "no_change" {
		t.Fatalf("OutcomeRefinement = %q, want no_change so the throttle can suppress quiet days", bb.OutcomeRefinement)
	}
}

// A stale stored review (older than the re-emit window) must not be resurrected
// — that would report last week's findings as though they were this run's.
func TestSelfReview_StaleStoredReviewIsNotReEmitted(t *testing.T) {
	dir := t.TempDir()
	isolateSRReportStores(t, dir)
	deps := srReportDeps(t, dir, srOneFinding, nil, "abc superpowers: apply verified run 1")

	first := &Blackboard{ChainState: map[string]any{}}
	runSelfReview(first, deps)

	// Age the stored review past the window.
	st := loadSelfReviewState(dir)
	st.LastReviewedAt = time.Now().Add(-48 * time.Hour)
	if err := saveSelfReviewState(dir, st); err != nil {
		t.Fatal(err)
	}

	second := &Blackboard{ChainState: map[string]any{}}
	runSelfReview(second, deps)
	if !strings.Contains(second.Result, "Skipped") {
		t.Fatalf("a review from two days ago must not be re-emitted as this run's result:\n%s", second.Result)
	}
}

// The commit filter must cover the PR shepherd's own Claude-authored CI fixes.
// They land on master through fleet/landing exactly like the apply commits, are
// written by Claude with no human review, and were silently out of scope:
// "fleet: fix CI for PR #53 head aaf6d966 (attempt 1)" (4b7e025, 2026-08-01).
func TestFilterAutonomousCommits_CoversShepherdCIFixes(t *testing.T) {
	log := strings.Join([]string{
		"aaa superpowers: apply verified run 20260801T212439-4544e7a7",
		"bbb fleet: fix CI for PR #53 head aaf6d966 (attempt 1)",
		"ccc docs: hand-written note",
		"ddd Merge pull request #55 from greenTeeProduction/fleet/landing",
	}, "\n")

	got := filterAutonomousCommits(log)
	for _, want := range []string{"superpowers: apply", "fleet: fix CI"} {
		if !strings.Contains(got, want) {
			t.Errorf("autonomous commit %q was filtered out; it is Claude-authored and unreviewed", want)
		}
	}
	if strings.Contains(got, "docs: hand-written note") {
		t.Error("hand-authored commits must stay out of scope")
	}
}

// isolateSRReportStores points the seed ledger and program store at dir so the
// tests never touch the live ~/.go-bt-evolve state.
func isolateSRReportStores(t *testing.T, dir string) {
	t.Helper()
	prevSF := selfFixDirOverride
	selfFixDirOverride = filepath.Join(dir, "selffix")
	prevP := goapProgramsPath
	goapProgramsPath = filepath.Join(dir, "programs.json")
	t.Cleanup(func() {
		selfFixDirOverride = prevSF
		goapProgramsPath = prevP
	})
}
