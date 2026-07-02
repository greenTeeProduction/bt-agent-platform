package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func TestIsGoapNotebookLMQuotaError(t *testing.T) {
	quota := []string{
		"Error: Google rejected the query (error code 8: RESOURCE_EXHAUSTED) . This may \nindicate account-level restrictions on programmatic access.",
		`{"error": "Google rejected the query (error code 8: RESOURCE_EXHAUSTED) [type.googleapis.com/google.internal.labs.tailwind.orchestration.v1.UserDisplayableError]."}`,
	}
	for _, out := range quota {
		if !isGoapNotebookLMQuotaError(out) {
			t.Fatalf("expected quota error for %q", out)
		}
	}

	notQuota := []string{
		// non-quota failures
		"Error: Query failed: Authentication expired. Run 'nlm login' in your terminal",
		`{"error": "NotebookLM circuit breaker open", "retry_after": "5m0s"}`,
		// a clean successful answer is not a quota error (note: answers that
		// MENTION resource_exhausted are already failures per
		// isGoapNotebookLMFailure — the fix-bd8c5b6 defense against quota text
		// leaking into syntheses — so they classify as quota errors too)
		`{"answer":"GOAL: Add a test\nGAP: missing test evidence with error budgets [1]"}`,
	}
	for _, out := range notQuota {
		if isGoapNotebookLMQuotaError(out) {
			t.Fatalf("did not expect quota classification for %q", out)
		}
	}
}

func TestNextNlmQuotaReset_PacificMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("tz database unavailable")
	}
	// 2026-07-02 10:15 PT → reset at 2026-07-03 00:00 PT
	now := time.Date(2026, 7, 2, 10, 15, 0, 0, loc)
	got := nextNlmQuotaReset(now)
	want := time.Date(2026, 7, 3, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextNlmQuotaReset = %v, want %v", got, want)
	}
	if !got.After(now) {
		t.Fatalf("reset %v must be after now %v", got, now)
	}
}

func TestNlmQuotaState_PersistsAcrossRuns(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveNlmQuotaExhausted(run1, time.Now())

	if !nlmQuotaExhausted(run2) {
		t.Fatal("quota exhaustion saved by run 1 must be visible to run 2")
	}
}

func TestNlmQuotaState_ExpiredWindowNotExhausted(t *testing.T) {
	bb := &Blackboard{}
	// ChainState fallback path: store an already-past timestamp directly
	setGoapState(bb, "nlm_quota_until", time.Now().Add(-time.Hour).Format(time.RFC3339))
	if nlmQuotaExhausted(bb) {
		t.Fatal("a past quota-until timestamp must not report exhausted")
	}
}

func TestNlmQuotaState_EmptyAndGarbageNotExhausted(t *testing.T) {
	bb := &Blackboard{}
	if nlmQuotaExhausted(bb) {
		t.Fatal("empty state must not report exhausted")
	}
	setGoapState(bb, "nlm_quota_until", "not-a-timestamp")
	if nlmQuotaExhausted(bb) {
		t.Fatal("unparseable state must not report exhausted")
	}
}

func TestLastReviewedSHA_RoundTrip(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveLastReviewedSHA(run1, "abc123")
	if got := loadLastReviewedSHA(run2); got != "abc123" {
		t.Fatalf("loadLastReviewedSHA = %q, want abc123", got)
	}

	if got := loadLastReviewedSHA(&Blackboard{}); got != "" {
		t.Fatalf("empty blackboard must return empty SHA, got %q", got)
	}
	if !strings.HasPrefix("goap_fusion_last_reviewed_sha", "goap_fusion_") {
		t.Fatal("key naming sanity")
	}
}

// newReviewTestRepo creates a git repo with two commits and returns
// (repoDir, firstSHA).
func newReviewTestRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// no -b flag: the system git predates `git init -b`; old git defaults
	// to master anyway and the helpers only use HEAD-relative ranges
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.go")
	run("commit", "-q", "-m", "first commit")
	first := run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.go")
	run("commit", "-q", "-m", "second commit")
	return dir, first
}

func TestGatherReviewContext_CommitRangeSinceLastSHA(t *testing.T) {
	repo, first := newReviewTestRepo(t)
	rc := gatherReviewContext(repo, first, filepath.Join(repo, "missing-report.md"))
	if rc.mode != "commits" {
		t.Fatalf("mode = %q, want commits", rc.mode)
	}
	if !strings.Contains(rc.rangeDesc, first[:8]) {
		t.Fatalf("rangeDesc %q should reference the last reviewed SHA", rc.rangeDesc)
	}
	if !strings.Contains(rc.body, "second commit") || strings.Contains(rc.body, "first commit") {
		t.Fatalf("body must cover exactly the unreviewed range: %s", rc.body)
	}
	if !strings.Contains(rc.body, "var X = 1") {
		t.Fatalf("body must include the diff: %s", rc.body)
	}
}

func TestGatherReviewContext_InvalidSHAFallsBackToRecent(t *testing.T) {
	repo, _ := newReviewTestRepo(t)
	rc := gatherReviewContext(repo, "deadbeef", filepath.Join(repo, "missing-report.md"))
	if rc.mode != "commits" {
		t.Fatalf("mode = %q, want commits (recent-window fallback)", rc.mode)
	}
	// both commits are recent, so both appear
	if !strings.Contains(rc.body, "first commit") || !strings.Contains(rc.body, "second commit") {
		t.Fatalf("recent-window fallback must include recent commits: %s", rc.body)
	}
}

func TestGatherReviewContext_NoNewCommitsUsesGraphify(t *testing.T) {
	repo, _ := newReviewTestRepo(t)
	head, err := runGoapGit(repo, 10*time.Second, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(repo, "GRAPH_REPORT.md")
	if err := os.WriteFile(report, []byte("## Summary\ngod nodes everywhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := gatherReviewContext(repo, head, report)
	if rc.mode != "graphify" {
		t.Fatalf("mode = %q, want graphify when range is empty", rc.mode)
	}
	if !strings.Contains(rc.body, "god nodes everywhere") {
		t.Fatalf("graphify mode must carry the report: %s", rc.body)
	}
}

func TestBuildClaudeReviewPrompt_ContractMarkers(t *testing.T) {
	commits := buildClaudeReviewPrompt("improve platform", goapReviewContext{
		mode: "commits", rangeDesc: "abc..HEAD", body: "diff body"})
	graph := buildClaudeReviewPrompt("improve platform", goapReviewContext{
		mode: "graphify", rangeDesc: "codebase structure", body: "report body"})

	for _, p := range []string{commits, graph} {
		for _, marker := range []string{"GOAL:", "GAP:", "FILES:", "TESTS:", "FINDINGS:"} {
			if !strings.Contains(p, marker) {
				t.Fatalf("prompt missing %s contract marker:\n%s", marker, p)
			}
		}
	}
	if !strings.Contains(commits, "diff body") || !strings.Contains(commits, "abc..HEAD") {
		t.Fatal("commit prompt must embed range and diff")
	}
	if !strings.Contains(graph, "report body") {
		t.Fatal("graphify prompt must embed the report")
	}
	if !strings.Contains(commits, "Do not edit any files") {
		t.Fatal("review prompt must forbid edits")
	}
}
