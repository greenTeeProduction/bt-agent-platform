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
		// a rejected-but-not-exhausted RPC must not stamp the day-long quota
		// cache: this INVALID_ARGUMENT rejection (observed 2026-07-08→13)
		// re-poisoned the cache every morning for a week
		"Error: Google rejected the query (error code 3: INVALID_ARGUMENT). This may \nindicate account-level restrictions on programmatic access. Try \nre-authenticating with 'nlm login' or using a different account.",
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
	rc := gatherReviewContext(repo, first, filepath.Join(repo, "missing-report.md"), 0)
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
	rc := gatherReviewContext(repo, "deadbeef", filepath.Join(repo, "missing-report.md"), 0)
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
	rc := gatherReviewContext(repo, head, report, 0)
	if rc.mode != "structure" {
		t.Fatalf("mode = %q, want structure when range is empty", rc.mode)
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
		for _, marker := range []string{"GOAL1:", "GAP1:", "FILES1:", "TESTS:", "FINDINGS:", "PROGRAM:"} {
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
		!strings.Contains(string(data), "Add regression test") {
		t.Fatalf("synthesis content wrong:\n%s", data)
	}
	if !strings.Contains(filepath.Base(path), "goap-fusion-claude-review-") {
		t.Fatalf("synthesis filename convention violated: %s", path)
	}

	head, _ := runGoapGit(repo, 10*time.Second, "rev-parse", "HEAD")
	if got := loadLastReviewedSHA(bb); got != head {
		t.Fatalf("last reviewed SHA not advanced: got %q want %q", got, head)
	}
	if len(runner.prompts) != 1 || !strings.Contains(runner.prompts[0], "GOAL1:") {
		t.Fatalf("runner prompt malformed: %v", runner.prompts)
	}
}

func TestRunClaudeCodeReviewResearch_StructureModeLeavesSHAUnchanged(t *testing.T) {
	repo, first := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{output: `GOAL: Deepen the shallow engine module
GAP: structure report shows god nodes in internal/engine
FILES: internal/engine
TESTS: /usr/local/go/bin/go test ./internal/engine -short
FINDINGS: - architecture drift`}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}
	saveLastReviewedSHA(bb, first)
	// Force a structure-mode cycle (round % 3 == 1): the review looked at the
	// codebase structure, not commits, so it must NOT advance the last-reviewed
	// SHA — otherwise the next commit cycle skips the commits this cycle never
	// actually reviewed.
	saveReviewModeRound(bb, 1)

	deps := reviewTestDeps(t, repo, runner)
	if got := runClaudeCodeReviewResearch(bb, deps); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}

	if got := loadLastReviewedSHA(bb); got != first {
		head, _ := runGoapGit(repo, 10*time.Second, "rev-parse", "HEAD")
		t.Fatalf("structure cycle must leave last-reviewed SHA unchanged: got %q, want %q (HEAD is %q)", got, first, head)
	}
}

func TestRunClaudeCodeReviewResearch_RateLimited(t *testing.T) {
	isolateClaudeBackoffStore(t)
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

func TestRunClaudeCodeReviewResearch_RateLimitedRecordsBackoff(t *testing.T) {
	isolateClaudeBackoffStore(t)
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{
		output: "Claude AI usage limit reached|resets 3pm",
		err:    fmt.Errorf("exit status 1"),
	}
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}

	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != -1 {
		t.Fatalf("status = %d, want -1", got)
	}
	if bb.Outcome != "goap_fusion_claude_review_rate_limited" {
		t.Fatalf("outcome = %q, want goap_fusion_claude_review_rate_limited", bb.Outcome)
	}

	until, ok := loadClaudeBackoffState(bb)
	if !ok {
		t.Fatal("loadClaudeBackoffState after a rate-limited review = inactive, want a recorded deadline: the rate-limited outcome must call saveClaudeBackoffState so the NEXT tick short-circuits")
	}
	if !until.After(time.Now().Add(time.Minute)) {
		t.Fatalf("recorded backoff deadline %v is not meaningfully in the future: the window must actually close Claude attempts", until)
	}
}

// TestRunClaudeCodeReviewResearch_RateLimitBackoffHonorsResetHint pins the
// deadline SHAPE: the CLI names its own reset ("resets 3pm"), so the stamp
// must be that reset plus the boundary margin — not now+goapClaudeBackoffWindow.
// The fixed 1h window kept re-arming hour-long oversleeps all of 2026-07-14
// while the quota actually reset earlier.
func TestRunClaudeCodeReviewResearch_RateLimitBackoffHonorsResetHint(t *testing.T) {
	isolateClaudeBackoffStore(t)
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{
		output: "Claude AI usage limit reached|resets 3pm",
		err:    fmt.Errorf("exit status 1"),
	}
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}

	fixedNow := time.Date(2026, 7, 15, 10, 0, 0, 0, time.Local)
	deps := reviewTestDeps(t, repo, runner)
	deps.now = func() time.Time { return fixedNow }

	if got := runClaudeCodeReviewResearch(bb, deps); got != -1 {
		t.Fatalf("status = %d, want -1", got)
	}
	until, ok := loadClaudeBackoffState(bb)
	if !ok {
		t.Fatal("no backoff recorded after a rate-limited review")
	}
	want := time.Date(2026, 7, 15, 15, 0, 0, 0, time.Local).Add(claudeResetMargin)
	if !until.Equal(want) {
		t.Fatalf("backoff deadline = %v, want CLI-reported reset+margin %v (not now+%v): the stamp must honor the reset hint", until, want, goapClaudeBackoffWindow)
	}
}

func TestRunClaudeCodeReviewResearch_ActiveBackoffSkipsClaude(t *testing.T) {
	isolateClaudeBackoffStore(t)
	repo, _ := newReviewTestRepo(t)
	// A parseable success output: if the action wrongly invokes Claude despite
	// the active backoff, it returns 1 and the failure is unmistakable.
	runner := &fakeReviewClaudeRunner{output: `GOAL: Should never be produced
GAP: backoff was active
FILES: a.go
TESTS: /usr/local/go/bin/go test ./internal/engine -short
FINDINGS: - none`}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}
	until := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	saveClaudeBackoffState(bb, until)
	roundBefore := loadReviewModeRound(bb)

	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != -1 {
		t.Fatalf("status = %d, want -1 (fail fast so ResearchRouter falls through to ResearchOptional)", got)
	}
	if bb.Outcome != "goap_fusion_claude_review_rate_limited" {
		t.Fatalf("outcome = %q, want goap_fusion_claude_review_rate_limited", bb.Outcome)
	}
	if len(runner.prompts) != 0 {
		t.Fatalf("runner invoked %d time(s) during an active backoff, want 0: the entry guard must not spend a 15-minute Claude run against a quota known to be closed", len(runner.prompts))
	}
	if !strings.Contains(strings.ToLower(bb.Result), "backoff active until") {
		t.Fatalf("Result must say the backoff is active and until when, got: %s", bb.Result)
	}
	if !strings.Contains(bb.Result, until.Format(time.RFC3339)) {
		t.Fatalf("Result must name the backoff deadline %s, got: %s", until.Format(time.RFC3339), bb.Result)
	}
	if got := loadReviewModeRound(bb); got != roundBefore {
		t.Fatalf("review mode round advanced %d -> %d on a skipped tick, want unchanged: a skip must not consume a review rotation slot", roundBefore, got)
	}
}

func TestRunClaudeCodeReviewResearch_ExpiredBackoffDoesNotBlock(t *testing.T) {
	isolateClaudeBackoffStore(t)
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{output: `GOAL: Add regression test for var X initialization
GAP: second commit added var X without any test coverage
FILES: a.go, a_test.go
TESTS: /usr/local/go/bin/go test ./... -run TestX
FINDINGS: - none`}

	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}
	// Elapsed window: must NOT block — the permanent-wedge failure mode this
	// loop has already hit twice (circuit breaker, runaway backstop).
	saveClaudeBackoffState(bb, time.Now().Add(-time.Hour))

	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != 1 {
		t.Fatalf("status = %d, want 1: an expired backoff must reopen Claude attempts; result: %s", got, bb.Result)
	}
	if len(runner.prompts) != 1 {
		t.Fatalf("runner invoked %d time(s) after the backoff expired, want 1", len(runner.prompts))
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

// TestRunClaudeCodeReviewResearch_CodexRateLimitWritesCodexStoreOnly proves the
// review fallback's rate-limit accounting is provider-aware: a Codex rate limit
// lands in the Codex store and never touches the Claude cooldown.
func TestRunClaudeCodeReviewResearch_CodexRateLimitWritesCodexStoreOnly(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	isolateBackoffStores(t)
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{
		output: "429 too many requests",
		err:    fmt.Errorf("exit status 1"),
	}
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}

	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != -1 {
		t.Fatalf("status = %d, want -1; result: %s", got, bb.Result)
	}
	if bb.Outcome != "goap_fusion_claude_review_rate_limited" {
		t.Fatalf("outcome = %q, want goap_fusion_claude_review_rate_limited", bb.Outcome)
	}
	if _, ok := loadDelegationBackoffState(bb, DelegationProviderCodex); !ok {
		t.Fatal("codex backoff store inactive after a Codex rate limit, want a recorded deadline")
	}
	if _, ok := loadDelegationBackoffState(bb, DelegationProviderClaude); ok {
		t.Fatal("claude backoff store active after a Codex rate limit: providers must not share cooldown state")
	}
}

// TestRunClaudeCodeReviewResearch_ClaudeBackoffDoesNotBlockCodex proves the
// review fallback's entry guard reads the provider's OWN backoff state: a
// Claude cooldown must be invisible to a Codex-configured run (and the run must
// actually invoke the provider).
func TestRunClaudeCodeReviewResearch_ClaudeBackoffDoesNotBlockCodex(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	isolateBackoffStores(t)
	repo, _ := newReviewTestRepo(t)
	runner := &fakeReviewClaudeRunner{output: `GOAL: Add regression test for var X initialization
GAP: second commit added var X without any test coverage
FILES: a.go, a_test.go
TESTS: /usr/local/go/bin/go test ./... -run TestX
FINDINGS: - none`}
	mgr := blackboard.NewManager(nil)
	bb := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop"), Task: "improve"}
	// Arm ONLY the Claude backoff.
	saveClaudeBackoffState(bb, time.Now().Add(time.Hour))

	if got := runClaudeCodeReviewResearch(bb, reviewTestDeps(t, repo, runner)); got != 1 {
		t.Fatalf("status = %d, want 1: a Claude backoff must not block a Codex run; result: %s", got, bb.Result)
	}
	if len(runner.prompts) != 1 {
		t.Fatalf("runner invoked %d time(s), want 1: a Claude cooldown must be invisible to a Codex-configured run", len(runner.prompts))
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

func TestGatherReviewContextRotatesModes(t *testing.T) {
	repo, _ := newReviewTestRepo(t)
	report := filepath.Join(repo, "GRAPH_REPORT.md")
	if err := os.WriteFile(report, []byte("## Summary\nstructure body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Round 1 must run the structural review even though unreviewed commits
	// exist — under the old selection this mode was dead code because the
	// loop itself commits every cycle.
	rc := gatherReviewContext(repo, "", report, 1)
	if rc.mode != "structure" || !strings.Contains(rc.body, "structure body") {
		t.Fatalf("round 1 must be structure mode, got %q", rc.mode)
	}

	// Round 2 reviews recent failures when the dead-letter queue has records.
	dlq := filepath.Join(t.TempDir(), "dlq.json")
	if err := os.WriteFile(dlq, []byte(`[{"agent":"x","failed_at":"2026-07-03T19:08:50","error":"boom"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldDLQ := goapDeadLetterPath
	goapDeadLetterPath = dlq
	t.Cleanup(func() { goapDeadLetterPath = oldDLQ })
	rc2 := gatherReviewContext(repo, "", report, 2)
	if rc2.mode != "failures" || !strings.Contains(rc2.body, "boom") {
		t.Fatalf("round 2 with DLQ records must be failures mode, got %q", rc2.mode)
	}

	// Round 2 without failure records falls through to commit review.
	goapDeadLetterPath = filepath.Join(t.TempDir(), "missing.json")
	rc3 := gatherReviewContext(repo, "", report, 2)
	if rc3.mode != "commits" {
		t.Fatalf("round 2 without DLQ must fall back to commits, got %q", rc3.mode)
	}
}
