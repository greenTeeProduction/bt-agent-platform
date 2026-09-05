# GOAP Fusion Claude Code Review Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When NotebookLM is unavailable (quota exhausted or otherwise), the scheduled GOAP fusion cycle falls back to a Claude Code review of the daemon's recent commits, producing `GOAL:/GAP:/FILES:/TESTS:` findings the existing pipeline consumes unchanged.

**Architecture:** A tree-level Selector (`ResearchRouter`) wraps the phase-2 research action; a new engine action `RunClaudeCodeReviewResearch` runs as its fallback via the existing `ClaudeRunner` interface with read-only tools. Quota errors are cached on the agent-scope blackboard (grill-state pattern) so subsequent cycles skip `nlm` calls entirely until the Pacific-midnight quota reset.

**Tech Stack:** Go 1.26, existing `internal/engine` action registry, `internal/blackboard` agent-scope store, claude CLI via `execClaudeRunner`.

**Spec:** `docs/superpowers/specs/2026-07-02-goap-fusion-claude-review-fallback-design.md`

## Global Constraints

- Go is at `/usr/local/go/bin/go` — prefix every command: `PATH=/usr/local/go/bin:$PATH`
- Work happens in the worktree `/home/nico/go-bt-evolve-claude-review` (branch `feature/goap-fusion-claude-review`) — NEVER in `/home/nico/go-bt-evolve` (the live daemon checkout)
- All tests: `-short -count=1`; the pre-commit hook runs gofmt → vet → golangci-lint → mod tidy → doc drift → ci-doctor → short tests
- Follow project conventions in `.claude/skills/project-conventions/SKILL.md`
- New ChainState keys use the `goap_fusion_` prefix via the existing `setGoapState` helper
- Vault synthesis dir constant: `goapFusionSynthesesDir = "/mnt/ssd/clawd/wiki/bt-research/syntheses"`; repo constant: `goapFusionRepo = "/home/nico/go-bt-evolve"` (both in `internal/engine/actions_goap_fusion.go:22-26`)

---

### Task 1: Quota classification, reset time, and persisted quota/last-SHA state

**Files:**
- Create: `internal/engine/actions_goap_fusion_claude_review.go`
- Test: `internal/engine/actions_goap_fusion_claude_review_test.go`

**Interfaces:**
- Consumes: `isGoapNotebookLMFailure(out string) bool` (`actions_goap_fusion.go:610`), `setGoapState(bb *Blackboard, key, value string)` (`actions_goap_fusion.go:503`), `blackboard.Scope`/`bb.BB.Mgr.Get/Set` (grill-state pattern, `actions_goap_fusion.go:515-547`)
- Produces: `isGoapNotebookLMQuotaError(out string) bool`, `nextNlmQuotaReset(now time.Time) time.Time`, `nlmQuotaExhausted(bb *Blackboard) bool`, `saveNlmQuotaExhausted(bb *Blackboard, now time.Time)`, `loadLastReviewedSHA(bb *Blackboard) string`, `saveLastReviewedSHA(bb *Blackboard, sha string)`

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/actions_goap_fusion_claude_review_test.go`:

```go
package engine

import (
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
		// successful answer that merely MENTIONS quota terms mid-text must not
		// be classified (it is not a failure at all)
		`{"answer":"GOAL: Handle RESOURCE_EXHAUSTED gracefully\nGAP: quota errors like error code 8 leak into syntheses [1]"}`,
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/nico/go-bt-evolve-claude-review && PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestIsGoapNotebookLMQuotaError|TestNextNlmQuotaReset|TestNlmQuotaState|TestLastReviewedSHA' -v`
Expected: compile FAIL — `undefined: isGoapNotebookLMQuotaError` (and friends)

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/actions_goap_fusion_claude_review.go`:

```go
package engine

// Claude Code review fallback for the GOAP fusion runner: when NotebookLM is
// unavailable (daily quota exhausted or any other failure), the ResearchRouter
// selector in GoapFusionLoopTree falls through to RunClaudeCodeReviewResearch,
// which reviews the daemon's recent auto-approved commits and emits findings
// in the same GOAL/GAP/FILES/TESTS contract the downstream pipeline consumes.
// Spec: docs/superpowers/specs/2026-07-02-goap-fusion-claude-review-fallback-design.md

import (
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// isGoapNotebookLMQuotaError reports whether a NotebookLM CLI failure is the
// daily-quota kind (RESOURCE_EXHAUSTED / error code 8). It is strictly a
// subset of isGoapNotebookLMFailure: quota-looking text inside a successful
// answer is not a quota error.
func isGoapNotebookLMQuotaError(out string) bool {
	if !isGoapNotebookLMFailure(out) {
		return false
	}
	lower := strings.ToLower(out)
	return strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "error code 8") ||
		strings.Contains(lower, "google rejected the query")
}

// nextNlmQuotaReset returns when the NotebookLM daily quota next resets:
// midnight America/Los_Angeles (Google API daily quotas reset at midnight
// Pacific). If the tz database is unavailable, fall back to now+12h.
func nextNlmQuotaReset(now time.Time) time.Time {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return now.Add(12 * time.Hour)
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
}

// Quota state must survive across scheduled runs (same rationale and
// mechanism as the grill state in actions_goap_fusion.go): agent-scope
// blackboard first, ChainState fallback.

func saveNlmQuotaExhausted(bb *Blackboard, now time.Time) {
	until := nextNlmQuotaReset(now).Format(time.RFC3339)
	setGoapState(bb, "nlm_quota_until", until)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_nlm_quota_until", until,
			"NotebookLM daily quota exhausted until this RFC3339 timestamp", "text")
	}
}

func nlmQuotaExhaustedUntil(bb *Blackboard) (time.Time, bool) {
	var raw string
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_nlm_quota_until"); err == nil {
			raw = strings.TrimSpace(e.Value)
		}
	}
	if raw == "" {
		raw, _ = bb.ChainState["goap_fusion_nlm_quota_until"].(string)
	}
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return until, true
}

func nlmQuotaExhausted(bb *Blackboard) bool {
	until, ok := nlmQuotaExhaustedUntil(bb)
	return ok && until.After(time.Now())
}

// Last-reviewed SHA tracks how far the Claude review fallback has covered the
// daemon's commit history, so consecutive fallback cycles review disjoint
// ranges instead of the same commits.

func saveLastReviewedSHA(bb *Blackboard, sha string) {
	setGoapState(bb, "last_reviewed_sha", sha)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_last_reviewed_sha", sha,
			"HEAD SHA covered by the last Claude Code review fallback", "text")
	}
}

func loadLastReviewedSHA(bb *Blackboard) string {
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_last_reviewed_sha"); err == nil {
			return strings.TrimSpace(e.Value)
		}
	}
	s, _ := bb.ChainState["goap_fusion_last_reviewed_sha"].(string)
	return strings.TrimSpace(s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestIsGoapNotebookLMQuotaError|TestNextNlmQuotaReset|TestNlmQuotaState|TestLastReviewedSHA' -v`
Expected: PASS (6 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/engine/actions_goap_fusion_claude_review.go internal/engine/actions_goap_fusion_claude_review_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(engine): NotebookLM quota classification and persisted quota/review-SHA state"
```

---

### Task 2: `execClaudeRunner` gains an `AllowedTools` override

**Files:**
- Modify: `internal/engine/superpowers_runner.go:57-70`
- Test: `internal/engine/superpowers_runner_test.go`

**Interfaces:**
- Consumes: existing `execClaudeRunner{Bin string}` and its `RunClaude` method
- Produces: `execClaudeRunner{Bin, AllowedTools string}` — when `AllowedTools` is non-empty it is passed as `--allowedTools` verbatim, bypassing the `BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS` env/default; empty field preserves current behavior exactly

- [ ] **Step 1: Write the failing test**

Existing tests in `superpowers_runner_test.go` build a fake `claude` script that echoes its args (see `TestExecClaudeRunnerPassesDefaultModel` at line 29 for the pattern). Append:

```go
func TestExecClaudeRunnerAllowedToolsOverride(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	script := "#!/bin/bash\necho \"$@\"\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	result := (execClaudeRunner{Bin: fakeClaude, AllowedTools: "Read,Grep"}).
		RunClaude(context.Background(), dir, "review this")
	if result.Err != nil {
		t.Fatalf("fake claude failed: %v\n%s", result.Err, result.Output)
	}
	if !strings.Contains(result.Output, "--allowedTools Read,Grep") {
		t.Fatalf("AllowedTools override not passed through: %s", result.Output)
	}
	if strings.Contains(result.Output, "Write") {
		t.Fatalf("override must replace the default tool list: %s", result.Output)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run TestExecClaudeRunnerAllowedToolsOverride -v`
Expected: compile FAIL — `unknown field AllowedTools in struct literal`

- [ ] **Step 3: Implement**

In `internal/engine/superpowers_runner.go`, change the struct and the `allowed` resolution:

```go
type execClaudeRunner struct {
	Bin string
	// AllowedTools, when non-empty, replaces the env/default --allowedTools
	// list. Used by the GOAP review fallback to run Claude read-only.
	AllowedTools string
}
```

and inside `RunClaude`, replace

```go
	allowed := getenvDefault("BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS", defaultSuperpowersAllowedTools)
```

with

```go
	allowed := r.AllowedTools
	if allowed == "" {
		allowed = getenvDefault("BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS", defaultSuperpowersAllowedTools)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestExecClaudeRunner' -v`
Expected: PASS (all execClaudeRunner tests, including the two pre-existing model tests)

- [ ] **Step 5: Commit**

```bash
git add internal/engine/superpowers_runner.go internal/engine/superpowers_runner_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(engine): execClaudeRunner AllowedTools override for read-only runs"
```

---

### Task 3: Review context gathering and prompt builder

**Files:**
- Modify: `internal/engine/actions_goap_fusion_claude_review.go`
- Test: `internal/engine/actions_goap_fusion_claude_review_test.go`

**Interfaces:**
- Consumes: `truncateGoap(s string, limit int) string` (`actions_goap_fusion.go:568`)
- Produces:
  - `type goapReviewContext struct { mode, rangeDesc, body string }` — `mode` is `"commits"` or `"graphify"`
  - `gatherReviewContext(repoDir, lastSHA, graphReportPath string) goapReviewContext`
  - `buildClaudeReviewPrompt(task string, rc goapReviewContext) string`
  - `runGoapGit(repoDir string, timeout time.Duration, args ...string) (string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `actions_goap_fusion_claude_review_test.go` (new imports: `os`, `os/exec`, `path/filepath`):

```go
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
	run("init", "-q", "-b", "master")
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestGatherReviewContext|TestBuildClaudeReviewPrompt' -v`
Expected: compile FAIL — `undefined: gatherReviewContext`

- [ ] **Step 3: Implement**

Append to `actions_goap_fusion_claude_review.go` (new imports: `context`, `fmt`, `os`, `os/exec`):

```go
// goapReviewContext is what the Claude review fallback will look at: either a
// concrete commit range ("commits") or, when nothing new was committed, the
// graphify structure report ("graphify").
type goapReviewContext struct {
	mode      string
	rangeDesc string
	body      string
}

const (
	goapReviewDiffLimit   = 12000
	goapReviewStatLimit   = 4000
	goapReviewReportLimit = 8000
)

func runGoapGit(repoDir string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoDir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// gatherReviewContext picks the review target. Priority: commits after the
// last reviewed SHA; else commits from the last 24h; else the graphify report.
func gatherReviewContext(repoDir, lastSHA, graphReportPath string) goapReviewContext {
	const gitTimeout = 30 * time.Second

	logArgs := []string{"log", "--stat", "--since=24 hours ago"}
	diffArgs := []string{"log", "-p", "--since=24 hours ago"}
	rangeDesc := "commits from the last 24 hours"
	if lastSHA != "" {
		if _, err := runGoapGit(repoDir, gitTimeout, "merge-base", "--is-ancestor", lastSHA, "HEAD"); err == nil {
			spec := lastSHA + "..HEAD"
			logArgs = []string{"log", "--stat", spec}
			diffArgs = []string{"diff", spec}
			rangeDesc = spec
		}
	}

	stat, statErr := runGoapGit(repoDir, gitTimeout, logArgs...)
	if statErr == nil && strings.TrimSpace(stat) != "" {
		diff, _ := runGoapGit(repoDir, gitTimeout, diffArgs...)
		body := fmt.Sprintf("### Commits (%s)\n%s\n\n### Diff\n%s",
			rangeDesc, truncateGoap(stat, goapReviewStatLimit), truncateGoap(diff, goapReviewDiffLimit))
		return goapReviewContext{mode: "commits", rangeDesc: rangeDesc, body: body}
	}

	report, _ := os.ReadFile(graphReportPath)
	return goapReviewContext{
		mode:      "graphify",
		rangeDesc: "codebase structure (no unreviewed commits)",
		body:      truncateGoap(string(report), goapReviewReportLimit),
	}
}

func buildClaudeReviewPrompt(task string, rc goapReviewContext) string {
	var focus string
	if rc.mode == "commits" {
		focus = fmt.Sprintf(`Review the following recent commits to this repository (%s). They were
implemented by an automated pipeline and auto-committed WITHOUT human review.
Hunt for: bugs, regressions, missing or weak tests, convention violations,
and half-finished work.

%s`, rc.rangeDesc, rc.body)
	} else {
		focus = fmt.Sprintf(`There are no unreviewed commits. Instead, review the codebase structure
report below (%s) and identify the single highest-impact structural fix.

%s`, rc.rangeDesc, rc.body)
	}

	return fmt.Sprintf(`You are the code-review fallback of an autonomous GOAP fusion improvement cycle
(NotebookLM research is unavailable). You may Read/Glob/Grep files and run
read-only git commands to verify what you see. Do not edit any files — a later
pipeline stage implements fixes.

Task context: %s

%s

Return EXACTLY this format:
GOAL: <one specific code change the next automated Superpowers/Claude run should implement>
GAP: <why the current go-bt-evolve codebase needs it — cite the commit or file you reviewed>
FILES: <likely files or packages to inspect/change>
TESTS: <specific Go tests/build commands to verify it>
FINDINGS: <bullet list of everything else you found, most severe first>

Rules:
- Prefer fixing a concrete defect you actually found over generic improvements.
- The goal must be small enough for one scheduled coding run.
- If the reviewed code is clean, say so in FINDINGS and put the best
  code-level next step in GOAL.`, task, focus)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestGatherReviewContext|TestBuildClaudeReviewPrompt' -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/engine/actions_goap_fusion_claude_review.go internal/engine/actions_goap_fusion_claude_review_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(engine): review-context gathering and prompt builder for Claude review fallback"
```

---

### Task 4: `RunClaudeCodeReviewResearch` action

**Files:**
- Modify: `internal/engine/actions_goap_fusion_claude_review.go`
- Test: `internal/engine/actions_goap_fusion_claude_review_test.go`

**Interfaces:**
- Consumes: Task 1–3 helpers; `ClaudeRunner`/`CommandResult` (`superpowers_runner.go`); `extractGoapNotebookLMRecommendation`, `firstNonEmptyGoapLine`, `truncateGoap`, `setGoapState` (`actions_goap_fusion.go`); `isClaudeRateLimit` (`actions_superpowers_prod.go:500`); `writeString` (`actions_notebooklm.go:205`); `RegisterAction` (`registry.go:39`); `goapReviewAllowedTools` (new const); `btcore.BTContext[Blackboard]`
- Produces: registered action `"RunClaudeCodeReviewResearch"`; testable core `runClaudeCodeReviewResearch(bb *Blackboard, deps goapReviewDeps) int`; `goapReviewDeps{runner, repoDir, synthesesDir, graphReport string fields as below}`; ChainState outputs identical to the NotebookLM action (`goap_fusion_notebooklm_research`, `_goal`, `_gap`, `_research_path`) plus `goap_fusion_research_source=claude_code_review`

- [ ] **Step 1: Write the failing tests**

Append to the test file:

```go
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
```

(new test-file import: `fmt`)

- [ ] **Step 2: Run tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestRunClaudeCodeReviewResearch' -v`
Expected: compile FAIL — `undefined: goapReviewDeps`

- [ ] **Step 3: Implement**

Append to `actions_goap_fusion_claude_review.go` (new imports: `fmt` if not present, `path/filepath`, `github.com/nico/go-bt-evolve/internal/btcore`):

```go
// goapReviewAllowedTools keeps the review run read-only: the review must not
// edit code — the implementation phase does that. One command prefix per
// Bash() rule (see defaultSuperpowersAllowedTools).
const goapReviewAllowedTools = "Read,Glob,Grep," +
	"Bash(git log:*),Bash(git show:*),Bash(git diff:*),Bash(git status:*)"

type goapReviewDeps struct {
	runner       ClaudeRunner
	repoDir      string
	synthesesDir string
	graphReport  string
	timeout      time.Duration
}

func defaultGoapReviewDeps() goapReviewDeps {
	return goapReviewDeps{
		runner: execClaudeRunner{
			AllowedTools: getenvDefault("BT_GOAP_REVIEW_ALLOWED_TOOLS", goapReviewAllowedTools),
		},
		repoDir:      goapFusionRepo,
		synthesesDir: goapFusionSynthesesDir,
		graphReport:  goapFusionGraphReport,
		timeout:      15 * time.Minute,
	}
}

func init() {
	RegisterAction("RunClaudeCodeReviewResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		return runClaudeCodeReviewResearch(ctx.Blackboard, defaultGoapReviewDeps())
	})
}

// runClaudeCodeReviewResearch is the ResearchRouter fallback: Claude Code
// reviews the daemon's recent commits (or graphify hotspots) and its findings
// feed the pipeline through the exact ChainState keys the NotebookLM research
// action produces, so downstream phases need no changes.
func runClaudeCodeReviewResearch(bb *Blackboard, deps goapReviewDeps) int {
	rc := gatherReviewContext(deps.repoDir, loadLastReviewedSHA(bb), deps.graphReport)
	prompt := buildClaudeReviewPrompt(bb.Task, rc)

	runCtx, cancel := context.WithTimeout(context.Background(), deps.timeout)
	defer cancel()
	result := deps.runner.RunClaude(runCtx, deps.repoDir, prompt)

	combined := result.Output
	if result.Err != nil {
		combined += " " + result.Err.Error()
	}
	if result.Err != nil || strings.TrimSpace(result.Output) == "" {
		if isClaudeRateLimit(combined) {
			bb.Result = fmt.Sprintf("## Claude Review Fallback Rate-Limited\n\n```\n%s\n```", truncateGoap(combined, 2000))
			bb.Outcome = "goap_fusion_claude_review_rate_limited"
			return -1
		}
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\n```\n%s\n```", truncateGoap(combined, 2000))
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}

	answer := strings.TrimSpace(result.Output)
	goal, gap := extractGoapNotebookLMRecommendation(answer)
	if goal == "" {
		goal = firstNonEmptyGoapLine(answer)
	}
	if goal == "" {
		bb.Result = "## Claude Review Fallback Failed\n\nClaude returned no parseable recommendation."
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}
	if gap == "" {
		gap = "Claude Code review produced a recommendation; see raw findings."
	}

	skipReason, _ := bb.ChainState["goap_fusion_notebooklm_skip_reason"].(string)
	if skipReason == "" {
		skipReason = "NotebookLM research step failed or was skipped"
	}

	ts := time.Now().Format("2006-01-02T150405")
	path := filepath.Join(deps.synthesesDir, fmt.Sprintf("goap-fusion-claude-review-%s.md", ts))
	report := fmt.Sprintf(`# GOAP Fusion Claude Code Review — %s

## Source
claude_code_review (fallback; NotebookLM unavailable)

## Why NotebookLM Was Skipped
%s

## Reviewed
%s (%s mode)

## Recommendation
GOAL: %s
GAP: %s

## Raw Claude Review Findings
%s
`, ts, truncateGoap(skipReason, 1500), rc.rangeDesc, rc.mode, goal, gap, answer)
	if err := writeString(path, report); err != nil {
		bb.Result = fmt.Sprintf("## Claude Review Fallback Failed\n\nCould not write `%s`: %v", path, err)
		bb.Outcome = "goap_fusion_claude_review_failed"
		return -1
	}

	setGoapState(bb, "notebooklm_research", report)
	setGoapState(bb, "notebooklm_goal", goal)
	setGoapState(bb, "notebooklm_gap", gap)
	setGoapState(bb, "notebooklm_research_path", path)
	setGoapState(bb, "research_source", "claude_code_review")

	if head, err := runGoapGit(deps.repoDir, 10*time.Second, "rev-parse", "HEAD"); err == nil && head != "" {
		saveLastReviewedSHA(bb, head)
	}

	bb.Result = fmt.Sprintf("## Claude Code Review Fallback Complete\n\nReviewed: %s (%s)\n\nPath: `%s`\n\nGOAL: %s\n\nGAP: %s",
		rc.rangeDesc, rc.mode, path, goal, gap)
	return 1
}
```

Note: `actions_goap_fusion.go` already has `func init()` — a second `init` in this new file is legal Go and matches `actions_goap_fusion_prod_additions.go`'s pattern.

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestRunClaudeCodeReviewResearch' -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/engine/actions_goap_fusion_claude_review.go internal/engine/actions_goap_fusion_claude_review_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(engine): RunClaudeCodeReviewResearch fallback action"
```

---

### Task 5: Quota memory wiring in the two NotebookLM actions

**Files:**
- Modify: `internal/engine/actions_goap_fusion.go` (GrillMeNotebookLM ~line 353; RunGoapFusionNotebookLMResearch ~line 75)
- Test: `internal/engine/actions_goap_fusion_claude_review_test.go`

**Interfaces:**
- Consumes: Task 1 helpers (`nlmQuotaExhausted`, `nlmQuotaExhaustedUntil`, `saveNlmQuotaExhausted`, `isGoapNotebookLMQuotaError`)
- Produces: behavior only — while the quota window is cached, GrillMe returns 0 without calling `nlm` and the research action returns -1 without calling `nlm` (Selector then runs the Claude fallback); fresh quota errors populate the cache and `goap_fusion_notebooklm_skip_reason`

- [ ] **Step 1: Write the failing test**

The `nlmRun` call itself is not seam-able, so test the observable pre-check behavior: with the quota window active, both actions must return without attempting NotebookLM (which in a test environment would hit the real `nlm` binary path and the circuit breaker — returning instantly with the right outcome proves the pre-check fired). Append:

```go
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
	if got := grill(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 0 {
		t.Fatalf("GrillMe status = %d, want 0 (soft skip)", got)
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
```

(new test-file import: `github.com/nico/go-bt-evolve/internal/btcore` — check the actual `BTContext` construction used in `internal/engine`'s existing tests, e.g. `planner_node_test.go`, and mirror it if the literal differs.)

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run TestNotebookLMActions_SkipWhileQuotaCached -v`
Expected: FAIL — the actions currently call `nlmRun` (test either times out slowly or fails on outcome mismatch; the outcome assertion fails first)

- [ ] **Step 3: Implement**

In `actions_goap_fusion.go`, at the top of the `GrillMeNotebookLM` action body (line ~354, before reading the graph report):

```go
		if nlmQuotaExhausted(bb) {
			until, _ := nlmQuotaExhaustedUntil(bb)
			bb.Result = fmt.Sprintf("## GrillMe Skipped\n\nNotebookLM daily quota window exhausted until %s; skipping to preserve calls.", until.Format(time.RFC3339))
			bb.Outcome = "goap_fusion_grill_skipped_quota"
			return 0 // non-fatal, same as a grill failure
		}
```

and after its existing `isGoapNotebookLMFailure(out)` check (inside the failure branch, before `return 0`):

```go
			if isGoapNotebookLMQuotaError(out) {
				saveNlmQuotaExhausted(bb, time.Now())
			}
```

In the `RunGoapFusionNotebookLMResearch` action body (line ~76, before `os.ReadFile`):

```go
		if nlmQuotaExhausted(bb) {
			until, _ := nlmQuotaExhaustedUntil(bb)
			reason := fmt.Sprintf("NotebookLM daily quota window exhausted until %s (cached from an earlier cycle)", until.Format(time.RFC3339))
			setGoapState(bb, "notebooklm_skip_reason", reason)
			bb.Result = "## GOAP NotebookLM Research Skipped\n\n" + reason
			bb.Outcome = "goap_fusion_notebooklm_quota_cached"
			return -1 // fail fast so ResearchRouter runs the Claude review fallback
		}
```

and inside its existing failure branch (`if isGoapNotebookLMFailure(out) { ... }`), before `return -1`:

```go
			if isGoapNotebookLMQuotaError(out) {
				saveNlmQuotaExhausted(bb, time.Now())
			}
			setGoapState(bb, "notebooklm_skip_reason", truncateGoap(out, 2000))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/engine -run 'TestNotebookLMActions_SkipWhileQuotaCached|TestGrillState|TestGoapFusionNotebookLM' -v`
Expected: PASS (new test plus all pre-existing grill/NotebookLM tests unchanged)

- [ ] **Step 5: Commit**

```bash
git add internal/engine/actions_goap_fusion.go internal/engine/actions_goap_fusion_claude_review_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(engine): cache NotebookLM quota exhaustion and skip nlm calls until reset"
```

---

### Task 6: ResearchRouter Selector in the fusion tree

**Files:**
- Modify: `internal/domains/goap_fusion_loop.go:34-36`
- Test: `internal/domains/domains_test.go`

**Interfaces:**
- Consumes: `sel(name string, children ...evolution.SerializableNode)` / `act(name, desc string)` helpers (`trees.go:15,25`); the Task 4 action name `"RunClaudeCodeReviewResearch"` (must match exactly)
- Produces: `GoapFusionLoopTree()` phase 2 wrapped in `sel("ResearchRouter", ...)`

- [ ] **Step 1: Write the failing test**

Append to `internal/domains/domains_test.go`:

```go
func TestGoapFusionLoopTree_ClaudeReviewFallback(t *testing.T) {
	tree := GoapFusionLoopTree()

	var router *evolution.SerializableNode
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if n.Name == "ResearchRouter" {
			router = n
			return
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)

	if router == nil {
		t.Fatal("GoapFusionLoopTree has no ResearchRouter node")
	}
	if router.Type != "Selector" {
		t.Fatalf("ResearchRouter type = %q, want Selector", router.Type)
	}
	if len(router.Children) != 2 ||
		router.Children[0].Name != "RunGoapFusionNotebookLMResearch" ||
		router.Children[1].Name != "RunClaudeCodeReviewResearch" {
		t.Fatalf("ResearchRouter children wrong: %+v", router.Children)
	}
}
```

(Match existing imports in `domains_test.go`; it already imports `evolution` for tree assertions — verify and mirror.)

- [ ] **Step 2: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/domains -run TestGoapFusionLoopTree_ClaudeReviewFallback -v`
Expected: FAIL — "GoapFusionLoopTree has no ResearchRouter node"

- [ ] **Step 3: Implement**

In `internal/domains/goap_fusion_loop.go`, replace

```go
			// ── Phase 2: Fresh Research ──
			act("RunGoapFusionNotebookLMResearch",
				"Query BT Platform Research notebook directly and save GOAP-owned findings to vault"),
```

with

```go
			// ── Phase 2: Fresh Research ──
			// NotebookLM first; when it is unavailable (daily quota exhausted,
			// auth expired, circuit open) Claude Code reviews the daemon's
			// recent commits instead so the cycle still produces findings.
			sel("ResearchRouter",
				act("RunGoapFusionNotebookLMResearch",
					"Query BT Platform Research notebook directly and save GOAP-owned findings to vault"),
				act("RunClaudeCodeReviewResearch",
					"Fallback when NotebookLM is unavailable: Claude Code reviews recent daemon commits (or graphify hotspots) and emits GOAL/GAP/FILES/TESTS findings to the vault"),
			),
```

Also update the tree doc comment (lines 5-12) — step 2 becomes: "2. Runs NotebookLM research for fresh recommendations (falls back to a Claude Code review of recent commits when NotebookLM is unavailable)".

- [ ] **Step 4: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -count=1 ./internal/domains -v -run 'TestGoapFusion|TestGoapFusionLoopTree'`
Expected: PASS (new structural test plus all pre-existing domains goap tests)

- [ ] **Step 5: Commit**

```bash
git add internal/domains/goap_fusion_loop.go internal/domains/domains_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(domains): ResearchRouter selector — Claude review fallback in goap fusion tree"
```

---

### Task 7: Full gate and CHANGELOG

**Files:**
- Modify: `CHANGELOG.md` (Unreleased → Added)

**Interfaces:**
- Consumes: everything above
- Produces: green `make check-quick`; changelog entry

- [ ] **Step 1: Add CHANGELOG entry**

Under `## [Unreleased]` / `### Added`, append:

```markdown
- **(engine):** GOAP fusion NotebookLM research falls back to a read-only Claude Code review of recent daemon commits when NotebookLM is unavailable (`ResearchRouter` selector). Quota errors (`RESOURCE_EXHAUSTED`) are cached on the agent-scope blackboard until the Pacific-midnight reset so subsequent cycles skip `nlm` calls entirely. Env: `BT_GOAP_REVIEW_ALLOWED_TOOLS` overrides the review's read-only tool list.
```

- [ ] **Step 2: Run the full pre-commit gate**

Run: `cd /home/nico/go-bt-evolve-claude-review && PATH=/usr/local/go/bin:$PATH make check-quick`
Expected: PASS (gofmt, vet, golangci-lint, mod tidy, doc drift, ci-doctor, short tests). Known flake: `TestCMAESOptimizer_Convergence` — if it is the sole failure, retry it once in isolation.

- [ ] **Step 3: Run the touched packages once more, race-clean**

Run: `PATH=/usr/local/go/bin:$PATH go test -short -race -count=1 ./internal/engine ./internal/domains`
Expected: `ok` for both packages

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md
PATH=/usr/local/go/bin:$PATH git commit -m "docs(changelog): goap fusion Claude review fallback"
```
