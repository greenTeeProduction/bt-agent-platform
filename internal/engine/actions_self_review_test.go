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

func selfReviewTestDeps(stateDir string, runner ClaudeRunner, scanner func(repoDir, lastSHA string) (string, string, string, error)) selfReviewDeps {
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
	scanner := func(repoDir, lastSHA string) (string, string, string, error) {
		return "", "", "headsha", nil
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
	scanner := func(repoDir, lastSHA string) (string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff body", "abc123", nil
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
	scanner := func(repoDir, lastSHA string) (string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "newsha", nil
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
	scanner := func(repoDir, lastSHA string) (string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "abc999", nil
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

	scanner1 := func(repoDir, lastSHA string) (string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "abc001", nil
	}
	deps1 := selfReviewTestDeps(stateDir, runner, scanner1)
	bb1 := &Blackboard{Task: "self-review"}
	if got := runSelfReview(bb1, deps1); got != 1 {
		t.Fatalf("first run status = %d, want 1", got)
	}

	scanner2 := func(repoDir, lastSHA string) (string, string, string, error) {
		return "def5678 superpowers: apply verified run y", "diff2", "abc002", nil
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
	scanner := func(repoDir, lastSHA string) (string, string, string, error) {
		return "abc1234 superpowers: apply verified run x", "diff", "abc777", nil
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
