package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"
)

// withTempSelfReview points the self-review state dir, the self-fix ledger
// dir (seedCodeFixProgram's own guards), and the goap program store at
// per-test temp paths, and clears the relevant env so tests are deterministic
// regardless of the host environment. Mirrors withTempSelfFix.
func withTempSelfReview(t *testing.T) (stateDir, programsPath string) {
	t.Helper()
	stateDir = t.TempDir()
	oldStateDir := selfReviewDirOverride
	selfReviewDirOverride = stateDir
	t.Cleanup(func() { selfReviewDirOverride = oldStateDir })

	oldSelfFixDir := selfFixDirOverride
	selfFixDirOverride = t.TempDir()
	t.Cleanup(func() { selfFixDirOverride = oldSelfFixDir })

	programsPath = filepath.Join(t.TempDir(), "programs.json")
	oldProgramsPath := goapProgramsPath
	goapProgramsPath = programsPath
	t.Cleanup(func() { goapProgramsPath = oldProgramsPath })

	t.Setenv("BT_SELF_FIX", "")
	t.Setenv("BT_SELF_FIX_COOLDOWN", "")
	t.Setenv("BT_SELF_FIX_MAX_OPEN", "")
	t.Setenv("BT_SELF_REVIEW_ALLOWED_TOOLS", "")
	return stateDir, programsPath
}

func selfReviewTestDeps(stateDir string, runner ClaudeRunner, scanner func(repoDir, lastSHA string) (string, string, string, string, error)) selfReviewDeps {
	return selfReviewDeps{
		runner:        runner,
		repoDir:       "/tmp/self-review-test-repo",
		stateDir:      stateDir,
		timeout:       time.Minute,
		now:           time.Now,
		commitScanner: scanner,
	}
}

// countSelfReviewPrograms counts programs tagged with the self-review source
// prefix specifically (not just any self-fix: program), so tests pin the
// exact source tag seedCodeFixProgram is called with.
func countSelfReviewPrograms(t *testing.T, programsPath string) int {
	t.Helper()
	ps, err := research.OpenPrograms(programsPath)
	if err != nil {
		t.Fatalf("open programs: %v", err)
	}
	n := 0
	for _, p := range ps.Programs {
		if strings.HasPrefix(p.Source, "self-fix:self-review:") {
			n++
		}
	}
	return n
}

const validFinding1 = `{"title":"Fix nil deref","milestone":"fix internal/foo/bar.go: guard nil deref before use","files":["internal/foo/bar.go"],"severity":"high","signature":"foo-bar-nil-deref"}`
const validFinding2 = `{"title":"Fix leaked handle","milestone":"fix internal/foo/baz.go: close file handle on error path","files":["internal/foo/baz.go"],"severity":"med","signature":"foo-baz-leak"}`

func TestRunSelfReview_UpToDateNoNewCommits(t *testing.T) {
	stateDir, programsPath := withTempSelfReview(t)
	scanner := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "", "", "headsha", "", nil
	}
	deps := selfReviewTestDeps(stateDir, &fakeReviewClaudeRunner{}, scanner)
	bb := &Blackboard{Task: "self-review"}

	if got := runSelfReview(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if bb.Outcome != "self_review_up_to_date" {
		t.Fatalf("outcome = %q, want self_review_up_to_date", bb.Outcome)
	}
	if bb.OutcomeRefinement != "no_change" {
		t.Fatalf("refinement = %q, want no_change", bb.OutcomeRefinement)
	}
	if !bb.QualityAuthoritative {
		t.Fatal("QualityAuthoritative must be set on the healthy skip")
	}
	if n := countSelfReviewPrograms(t, programsPath); n != 0 {
		t.Fatalf("expected no programs seeded, got %d", n)
	}
	state := loadSelfReviewState(stateDir)
	if state.LastReviewedSHA != "" {
		t.Fatalf("state SHA must remain unchanged (empty), got %q", state.LastReviewedSHA)
	}
}

func TestRunSelfReview_SeedsProgramsForConfirmedFindings(t *testing.T) {
	stateDir, programsPath := withTempSelfReview(t)
	scanner := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff body", "abc123", "beginning..HEAD", nil
	}
	output := "[" + validFinding1 + "," + validFinding2 + "]"
	runner := &fakeReviewClaudeRunner{output: output}
	deps := selfReviewTestDeps(stateDir, runner, scanner)
	bb := &Blackboard{Task: "self-review"}

	if got := runSelfReview(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1; result=%s", got, bb.Result)
	}
	if bb.Outcome != "self_review_seeded" {
		t.Fatalf("outcome = %q, want self_review_seeded", bb.Outcome)
	}
	if n := countSelfReviewPrograms(t, programsPath); n != 2 {
		t.Fatalf("expected 2 self-review programs, got %d", n)
	}
	state := loadSelfReviewState(stateDir)
	if state.LastReviewedSHA != "abc123" {
		t.Fatalf("state SHA = %q, want abc123", state.LastReviewedSHA)
	}
}

func TestRunSelfReview_ReviewFailureDoesNotAdvanceSHA(t *testing.T) {
	stateDir, _ := withTempSelfReview(t)
	if err := saveSelfReviewState(stateDir, selfReviewState{LastReviewedSHA: "prevsha"}); err != nil {
		t.Fatal(err)
	}
	scanner := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "newsha", "prevsha..HEAD", nil
	}
	runner := &fakeReviewClaudeRunner{output: ""} // empty output => review failure
	deps := selfReviewTestDeps(stateDir, runner, scanner)
	bb := &Blackboard{Task: "self-review"}

	if got := runSelfReview(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if bb.Outcome != "self_review_failed" {
		t.Fatalf("outcome = %q, want self_review_failed", bb.Outcome)
	}
	state := loadSelfReviewState(stateDir)
	if state.LastReviewedSHA != "prevsha" {
		t.Fatalf("SHA must not advance on review failure: got %q, want prevsha", state.LastReviewedSHA)
	}
}

func TestRunSelfReview_DropsInvalidFindings(t *testing.T) {
	stateDir, programsPath := withTempSelfReview(t)
	scanner := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "abc999", "beginning..HEAD", nil
	}
	invalid := `{"title":"","milestone":"","files":[],"severity":"low","signature":"invalid-sig"}`
	output := "[" + validFinding1 + "," + invalid + "]"
	runner := &fakeReviewClaudeRunner{output: output}
	deps := selfReviewTestDeps(stateDir, runner, scanner)
	bb := &Blackboard{Task: "self-review"}

	if got := runSelfReview(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if n := countSelfReviewPrograms(t, programsPath); n != 1 {
		t.Fatalf("expected 1 program seeded, got %d", n)
	}
	if !strings.Contains(bb.Result, "dropped 1") && !strings.Contains(bb.Result, "1 invalid") {
		t.Fatalf("report must count the dropped invalid finding: %s", bb.Result)
	}
}

func TestRunSelfReview_DedupSameSignatureWithinCooldown(t *testing.T) {
	stateDir, programsPath := withTempSelfReview(t)
	finding := `[{"title":"Fix X","milestone":"fix internal/foo/bar.go: guard nil deref","files":["internal/foo/bar.go"],"severity":"high","signature":"dup-sig"}]`
	runner := &fakeReviewClaudeRunner{output: finding}

	scanner1 := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "abc001", "beginning..HEAD", nil
	}
	deps1 := selfReviewTestDeps(stateDir, runner, scanner1)
	bb1 := &Blackboard{Task: "self-review"}
	if got := runSelfReview(bb1, deps1); got != 1 {
		t.Fatalf("first run status = %d, want 1", got)
	}

	scanner2 := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "def5678 superpowers: apply verified run y", "diff2", "abc002", "abc001..HEAD", nil
	}
	deps2 := selfReviewTestDeps(stateDir, runner, scanner2)
	bb2 := &Blackboard{Task: "self-review"}
	if got := runSelfReview(bb2, deps2); got != 1 {
		t.Fatalf("second run status = %d, want 1", got)
	}

	if n := countSelfReviewPrograms(t, programsPath); n != 1 {
		t.Fatalf("dedup: expected 1 program across two reviews with the same signature, got %d", n)
	}
}

func TestRunSelfReview_KillSwitchSkipsSeedingButStillReports(t *testing.T) {
	stateDir, programsPath := withTempSelfReview(t)
	t.Setenv("BT_SELF_FIX", "off")
	scanner := func(repoDir, lastSHA string) (string, string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "abc777", "beginning..HEAD", nil
	}
	finding := `[{"title":"Fix X","milestone":"fix internal/foo/bar.go: guard nil deref","files":["internal/foo/bar.go"],"severity":"high","signature":"killed-sig"}]`
	runner := &fakeReviewClaudeRunner{output: finding}
	deps := selfReviewTestDeps(stateDir, runner, scanner)
	bb := &Blackboard{Task: "self-review"}

	if got := runSelfReview(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if n := countSelfReviewPrograms(t, programsPath); n != 0 {
		t.Fatalf("kill switch: expected 0 programs, got %d", n)
	}
	if bb.Outcome == "" {
		t.Fatal("must still report an outcome with the kill switch on")
	}
}

func TestParseSelfReviewFindings_ExtractsArrayFromFencesAndProse(t *testing.T) {
	single := `{"title":"T","milestone":"fix a.go: guard x","files":["a.go"],"severity":"low","signature":"s1"}`
	cases := []string{
		"Here is my review:\n```json\n[" + single + "]\n```\nThanks.",
		"Some prose before [" + single + "] and after.",
	}
	for _, c := range cases {
		findings, dropped := parseSelfReviewFindings(c)
		if len(findings) != 1 || dropped != 0 {
			t.Fatalf("parse failed for %q: findings=%v dropped=%d", c, findings, dropped)
		}
		if findings[0].Signature != "s1" {
			t.Fatalf("wrong signature parsed: %+v", findings[0])
		}
	}
}

func TestParseSelfReviewFindings_CleanReviewEmptyArray(t *testing.T) {
	findings, dropped := parseSelfReviewFindings("No issues found.\n[]")
	if len(findings) != 0 || dropped != 0 {
		t.Fatalf("expected clean review, got findings=%v dropped=%d", findings, dropped)
	}
}

func TestRunSelfReviewActionRegistered(t *testing.T) {
	if GetAction("RunSelfReview") == nil {
		t.Fatal("RunSelfReview not registered")
	}
}

// TestSelfReviewDiffRange is the regression pin for the `git diff
// --since="24 hours ago"` bug: `--since` is a `git log` flag, `git diff`
// silently ignores it and diffs the WORKING TREE instead of the intended
// commit range. selfReviewDiffRange is the pure range-construction helper
// scanSelfReviewCommits calls — it must never produce "--since" in the diff
// args, on either path, and must always build a real two-endpoint range.
func TestSelfReviewDiffRange(t *testing.T) {
	assertNoSince := func(t *testing.T, diffArgs []string, rangeDesc string) {
		t.Helper()
		for _, a := range diffArgs {
			if strings.Contains(a, "--since") {
				t.Fatalf("diffArgs must never contain --since (git diff silently ignores it): %v", diffArgs)
			}
		}
		if strings.Contains(rangeDesc, "--since") {
			t.Fatalf("rangeDesc must never contain --since: %q", rangeDesc)
		}
	}

	t.Run("ancestor path diffs lastSHA..HEAD", func(t *testing.T) {
		diffArgs, rangeDesc := selfReviewDiffRange("lastsha123", "", true)
		assertNoSince(t, diffArgs, rangeDesc)
		joined := strings.Join(diffArgs, " ")
		if !strings.Contains(joined, "lastsha123..HEAD") {
			t.Fatalf("diffArgs must contain the real two-endpoint range lastsha123..HEAD, got %v", diffArgs)
		}
		if rangeDesc != "lastsha123..HEAD" {
			t.Fatalf("rangeDesc = %q, want lastsha123..HEAD", rangeDesc)
		}
	})

	t.Run("first-run/non-ancestor path diffs oldest^..HEAD", func(t *testing.T) {
		// The caller (scanSelfReviewCommits) has already resolved the
		// oldest-in-window hash and confirmed its parent resolves, so it
		// passes the caret-adjusted hash straight through.
		diffArgs, rangeDesc := selfReviewDiffRange("", "oldsha456^", false)
		assertNoSince(t, diffArgs, rangeDesc)
		joined := strings.Join(diffArgs, " ")
		if !strings.Contains(joined, "oldsha456^..HEAD") {
			t.Fatalf("diffArgs must contain the real two-endpoint range oldsha456^..HEAD, got %v", diffArgs)
		}
		if rangeDesc != "oldsha456^..HEAD" {
			t.Fatalf("rangeDesc = %q, want oldsha456^..HEAD", rangeDesc)
		}
	})

	t.Run("root-commit fallback diffs oldest..HEAD (no caret)", func(t *testing.T) {
		// The caller found that oldestInWindow^ does not resolve (oldest
		// commit in the window IS the repo root) and passes the bare hash
		// through as the documented fallback.
		diffArgs, rangeDesc := selfReviewDiffRange("", "rootsha789", false)
		assertNoSince(t, diffArgs, rangeDesc)
		joined := strings.Join(diffArgs, " ")
		if !strings.Contains(joined, "rootsha789..HEAD") {
			t.Fatalf("diffArgs must contain the root-commit fallback range rootsha789..HEAD, got %v", diffArgs)
		}
		if rangeDesc != "rootsha789..HEAD" {
			t.Fatalf("rangeDesc = %q, want rootsha789..HEAD", rangeDesc)
		}
	})

	t.Run("unresolved lower bound never falls back to --since", func(t *testing.T) {
		// Defensive: if the caller couldn't resolve an oldest-in-window hash
		// at all, the helper must still never reach for --since; it may
		// simply produce no diff args (the commit list alone still goes to
		// the reviewer).
		diffArgs, rangeDesc := selfReviewDiffRange("", "", false)
		assertNoSince(t, diffArgs, rangeDesc)
	})

	// The empty-24h-window case (nothing to review at all) is handled by the
	// caller (scanSelfReviewCommits returns early on an empty filtered
	// commit list, before ever calling selfReviewDiffRange) and is covered
	// by TestRunSelfReview_UpToDateNoNewCommits via the faked commitScanner.
}
