package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
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
		// Hermetic git env: when this test itself runs under a git hook
		// (pre-commit go test), git exports GIT_DIR/GIT_INDEX_FILE, which
		// would redirect these commands at the PARENT repo and run its
		// hooks. Strip all inherited GIT_* and isolate config via HOME.
		env := []string{}
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, "GIT_") {
				env = append(env, kv)
			}
		}
		cmd.Env = append(env,
			"HOME="+dir, "GIT_CONFIG_NOSYSTEM=1",
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

type fakeReviewClaudeRunner struct {
	output  string
	err     error
	prompts []string
}

func (f *fakeReviewClaudeRunner) RunClaude(_ context.Context, _ string, prompt string) CommandResult {
	f.prompts = append(f.prompts, prompt)
	return CommandResult{Output: f.output, Err: f.err}
}

func reviewTestDeps(t *testing.T, repo string, runner ClaudeRunner) goapReviewDeps {
	t.Helper()
	return goapReviewDeps{
		runner:       runner,
		repoDir:      repo,
		synthesesDir: t.TempDir(),
		graphReport:  filepath.Join(repo, "missing-report.md"),
		timeout:      time.Minute,
	}
}

func TestRunClaudeCodeReviewResearch_Success(t *testing.T) {
	repo, first := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{output: `GOAL: Add regression test for var X initialization
GAP: second commit added var X without any test coverage
FILES: a.go, a_test.go
TESTS: /usr/local/go/bin/go test ./... -run TestX
FINDINGS: - a.go: exported var without doc comment`}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}
	saveLastReviewedSHA(bb, first)

	deps := reviewTestDeps(t, repo, runner)
	if got := runClaudeCodeReviewResearch(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}

	goal, _ := bb.ChainState["goap_fusion_notebooklm_goal"].(string)
	if !strings.Contains(goal, "regression test for var X") {
		t.Fatalf("goal not extracted: %q", goal)
	}
	if src, _ := bb.ChainState["goap_fusion_research_source"].(string); src != "claude_code_review" {
		t.Fatalf("research_source = %q", src)
	}

	path, _ := bb.ChainState["goap_fusion_notebooklm_research_path"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("synthesis file not written: %v", err)
	}
	if !strings.Contains(string(data), "claude_code_review") ||
		!strings.Contains(string(data), "GOAL: Add regression test") {
		t.Fatalf("synthesis content wrong:\n%s", data)
	}
	if !strings.Contains(filepath.Base(path), "goap-fusion-claude-review-") {
		t.Fatalf("synthesis filename convention violated: %s", path)
	}

	head, _ := runGoapGit(repo, 10*time.Second, "rev-parse", "HEAD")
	if got := loadLastReviewedSHA(bb); got != head {
		t.Fatalf("last reviewed SHA not advanced: got %q want %q", got, head)
	}
	if len(runner.prompts) != 1 || !strings.Contains(runner.prompts[0], "GOAL:") {
		t.Fatalf("runner prompt malformed: %v", runner.prompts)
	}
}

func TestRunClaudeCodeReviewResearch_RateLimited(t *testing.T) {
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{
		output: "Claude AI usage limit reached|resets 3pm",
		err:    fmt.Errorf("exit status 1"),
	}
	bb := &Blackboard{Task: "improve"}
	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != -1 {
		t.Fatalf("status = %d, want -1", got)
	}
	if bb.Outcome != "goap_fusion_claude_review_rate_limited" {
		t.Fatalf("outcome = %q", bb.Outcome)
	}
}

func TestRunClaudeCodeReviewResearch_NoParseableGoalFails(t *testing.T) {
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{output: ""}
	bb := &Blackboard{Task: "improve"}
	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != -1 {
		t.Fatalf("status = %d, want -1", got)
	}
	if bb.Outcome != "goap_fusion_claude_review_failed" {
		t.Fatalf("outcome = %q", bb.Outcome)
	}
}

func TestRunClaudeCodeReviewResearchActionRegistered(t *testing.T) {
	if GetAction("RunClaudeCodeReviewResearch") == nil {
		t.Fatal("RunClaudeCodeReviewResearch not registered")
	}
}

func TestNotebookLMActions_SkipWhileQuotaCached(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}
	saveNlmQuotaExhausted(bb, time.Now())

	grill := GetAction("GrillMeNotebookLM")
	research := GetAction("RunGoapFusionNotebookLMResearch")
	if grill == nil || research == nil {
		t.Fatal("goap fusion NotebookLM actions not registered")
	}

	start := time.Now()
	if got := grill(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		// 1 (Success), not 0: 0 is Running in this engine and re-ticks the
		// memoryless Sequence until the runner stamps the run "partial".
		t.Fatalf("GrillMe status = %d, want 1 (skip, continue to ResearchRouter)", got)
	}
	if bb.Outcome != "goap_fusion_grill_skipped_quota" {
		t.Fatalf("GrillMe outcome = %q", bb.Outcome)
	}

	if got := research(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("research status = %d, want -1 (fail fast to selector fallback)", got)
	}
	if bb.Outcome != "goap_fusion_notebooklm_quota_cached" {
		t.Fatalf("research outcome = %q", bb.Outcome)
	}
	if reason, _ := bb.ChainState["goap_fusion_notebooklm_skip_reason"].(string); !strings.Contains(reason, "quota") {
		t.Fatalf("skip reason not recorded: %q", reason)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("quota-cached actions must not call nlm (took %s)", elapsed)
	}
}
