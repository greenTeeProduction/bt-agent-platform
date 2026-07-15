# Brainstorm Grill-Driven Design-Improvement Loop — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn BrainstormBranch's one-shot grill gate into a bounded revise→validate→grill loop (10 rounds) that improves `design.md` each round, with an exhaustion split that implements the clear scope and defers the rest to a goap-program follow-up.

**Architecture:** The existing `ReviewCycle` node drives the loop: its child (`MemSequence GrillRound` = revise + validate) re-runs while the reviewer (`GrillDesignArtifact`, adopting the verdict protocol) returns `needs_work`; reviewer failure (protocol error or no-progress breaker) routes the enclosing `Selector` to `SplitPath`. All round state persists in the Superpowers run JSON.

**Tech Stack:** Go 1.26 (`/usr/local/go/bin/go`), go-bt v0.1.0 behavior-tree library, existing Superpowers runtime (`internal/engine`), goap program store (`internal/research`).

**Spec:** `docs/superpowers/specs/2026-07-15-brainstorm-grill-loop-design.md` — read it first.

## Global Constraints

- Engine action return contract: **1 = Success, -1 = Failure, 0 = Running (re-tick!)** — never return 0 for "non-fatal continue".
- The `"Retry"` node type maps to a **repeat-on-success** decorator (fails immediately on child failure). Do NOT use it for this loop; `ReviewCycle` is the retry-with-feedback primitive.
- Every Action/Condition node name must have a registered function of exactly that name (`bb.actionForName(node.Name)`); node names must be unique per tree.
- All pre-HITL artifact writes must be idempotent (BT re-ticks can re-run actions).
- Any test touching goap program state MUST isolate `goapProgramsPath` (see `isolateGoapProgramStore(t)` in `internal/engine/actions_goap_fusion_test.go`); any test invoking Claude/nlm paths must inject fakes — no network, no real binaries.
- Go invocations need `PATH=/usr/local/go/bin:$PATH`.
- Commit after every task; the pre-commit hook runs the full gate (~10 min).

## Existing interfaces you will use (verbatim from the codebase)

```go
// internal/engine/superpowers_runner.go
type CommandResult struct { Command, Dir, Output string; Err error; Duration time.Duration }
type ClaudeRunner interface { RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult }
var defaultSuperpowersClaudeRunner ClaudeRunner = execClaudeRunner{}   // swap in tests

// internal/engine/superpowers_grill.go
type grillQuestion struct { Critical bool; Branch string; Text string }
type grillAnswerers struct {
    NotebookLM func(ctx context.Context, batch []grillQuestion) (map[int]string, error)
    Web        func(ctx context.Context, batch []grillQuestion) (map[int]string, error)
}
func parseGrillQuestions(out string) []grillQuestion
func resolveGrillQuestions(ctx context.Context, qs []grillQuestion, a grillAnswerers) grillResult
var grillNotebookLMAnswerer = nlmGrillAnswerer                          // swap in tests

// internal/engine/superpowers_artifacts.go
func writeSuperpowersRunJSON(run *SuperpowersRun) error
func writeArtifactOnce(path string, content []byte) (bool, error)

// internal/engine (run state)
func getSuperpowersRun(bb *Blackboard) (*SuperpowersRun, bool)

// internal/engine/goap_research_goals.go
type goapProgramSpec struct { Title string; Milestones []string }
func extractGoapProgram(answer string) *goapProgramSpec               // parses PROGRAM:/MILESTONEn:
func persistGoapProgram(bb *Blackboard, spec *goapProgramSpec, source string)

// internal/engine/goap_seed_improvement.go
func validateGoapProgramMilestones(milestones []string) goapSeedValidation // .Valid/.Malformed/.Ungrounded

// internal/engine/review_cycle.go — reviewer protocol:
//   ChainState["review_verdict"]  = "approved" | "needs_work"
//   ChainState["review_feedback"] = string carried to the child's next pass
//   reviewer returns <0 => ReviewCycle FAILS immediately (routes to SplitPath)
```

---

### Task 1: Grill pure-function extensions (branches, body/appendix split, hash)

**Files:**
- Modify: `internal/engine/superpowers_grill.go`
- Test: `internal/engine/superpowers_grill_test.go`

**Interfaces:**
- Consumes: existing `grillQuestion`, `grillResult`, `resolveGrillQuestions`.
- Produces (later tasks rely on these exact names):
  - `grillResult.OpenCriticalBranches []string` (new field)
  - `func splitDesignDocument(content string) (body string, appendix string)` — appendix starts at the first `\n## Grill Q&A` heading (any suffix), empty when absent
  - `func designBodyHash(body string) string` — sha256 hex of the trimmed body
  - `func grillRoundHeading(round int) string` — returns `"\n## Grill Q&A — round N\n\n"`
  - `func openCriticalDigest(qs []grillQuestion, answers map[int]string) string` — used for review_feedback (answered lines + open critical lines)

- [ ] **Step 1: Write the failing tests**

```go
// append to internal/engine/superpowers_grill_test.go
func TestResolveGrillQuestions_RecordsOpenCriticalBranches(t *testing.T) {
	qs := []grillQuestion{
		{Critical: true, Branch: "persistence", Text: "what fsyncs?"},
		{Critical: false, Branch: "ux", Text: "colors?"},
		{Critical: true, Branch: "auth", Text: "who signs?"},
	}
	// answerer resolves only index 2 (auth); persistence stays OPEN
	res := resolveGrillQuestions(context.Background(), qs, grillAnswerers{
		NotebookLM: func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
			out := map[int]string{}
			for i, q := range batch {
				if q.Branch == "auth" {
					out[i] = "the gateway signs"
				}
			}
			return out, nil
		},
	})
	if res.OpenCritical != 1 {
		t.Fatalf("OpenCritical = %d, want 1", res.OpenCritical)
	}
	if len(res.OpenCriticalBranches) != 1 || res.OpenCriticalBranches[0] != "persistence" {
		t.Fatalf("OpenCriticalBranches = %v, want [persistence]", res.OpenCriticalBranches)
	}
}

func TestSplitDesignDocument(t *testing.T) {
	body := "# Design\n\n## Goal\nX\n"
	appendix := "\n## Grill Q&A — round 1\n\n**Q (critical, a):** q?\n\n**A:** OPEN\n"
	gotBody, gotAppendix := splitDesignDocument(body + appendix)
	if strings.TrimSpace(gotBody) != strings.TrimSpace(body) {
		t.Fatalf("body = %q", gotBody)
	}
	if !strings.Contains(gotAppendix, "round 1") {
		t.Fatalf("appendix = %q", gotAppendix)
	}
	b2, a2 := splitDesignDocument(body)
	if a2 != "" || strings.TrimSpace(b2) != strings.TrimSpace(body) {
		t.Fatalf("no-appendix split wrong: body=%q appendix=%q", b2, a2)
	}
}

func TestDesignBodyHash_StableAndTrimmed(t *testing.T) {
	if designBodyHash("x\n") != designBodyHash("x") {
		t.Fatal("hash must trim")
	}
	if designBodyHash("x") == designBodyHash("y") {
		t.Fatal("hash must differ")
	}
}

func TestOpenCriticalDigest(t *testing.T) {
	qs := []grillQuestion{
		{Critical: true, Branch: "p", Text: "q1?"},
		{Critical: false, Branch: "u", Text: "q2?"},
	}
	d := openCriticalDigest(qs, map[int]string{1: "answered"})
	if !strings.Contains(d, "OPEN CRITICAL [p]: q1?") || !strings.Contains(d, "ANSWERED [u]: q2? — answered") {
		t.Fatalf("digest = %q", d)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestResolveGrillQuestions_RecordsOpenCriticalBranches|TestSplitDesignDocument|TestDesignBodyHash|TestOpenCriticalDigest' -count=1`
Expected: FAIL — `undefined: splitDesignDocument` etc. (compile errors count as the right failure here; the OpenCriticalBranches test must fail on a missing field).

- [ ] **Step 3: Implement in `superpowers_grill.go`**

```go
// add import "crypto/sha256", "encoding/hex"

type grillResult struct {
	Markdown             string
	OpenCritical         int
	OpenCriticalBranches []string // branches of critical questions left OPEN
}

// inside resolveGrillQuestions' loop, in the `else` (unanswered) branch:
			if q.Critical {
				open++
				openBranches = append(openBranches, q.Branch)
			}
// declare `var openBranches []string` before the loop and return it:
	return grillResult{Markdown: b.String(), OpenCritical: open, OpenCriticalBranches: openBranches}

const grillAppendixMarker = "\n## Grill Q&A"

// splitDesignDocument separates the design body from the append-only Grill
// Q&A appendix (everything from the first Grill Q&A heading onward).
func splitDesignDocument(content string) (string, string) {
	if idx := strings.Index(content, grillAppendixMarker); idx >= 0 {
		return content[:idx], content[idx:]
	}
	return content, ""
}

func designBodyHash(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return hex.EncodeToString(sum[:])
}

func grillRoundHeading(round int) string {
	return fmt.Sprintf("\n## Grill Q&A — round %d\n\n", round)
}

// openCriticalDigest renders the round outcome for review_feedback: what got
// answered (the reviser folds it in) and which criticals are still open (the
// reviser must answer them from the codebase or redesign them away).
func openCriticalDigest(qs []grillQuestion, answers map[int]string) string {
	var b strings.Builder
	for i, q := range qs {
		if ans, ok := answers[i]; ok {
			fmt.Fprintf(&b, "ANSWERED [%s]: %s — %s\n", q.Branch, q.Text, ans)
		} else if q.Critical {
			fmt.Fprintf(&b, "OPEN CRITICAL [%s]: %s\n", q.Branch, q.Text)
		} else {
			fmt.Fprintf(&b, "OPEN [%s]: %s\n", q.Branch, q.Text)
		}
	}
	return b.String()
}
```

Note: `resolveGrillQuestions` currently builds the markdown and answers map internally; `openCriticalDigest` needs the answers map — return it too: add field `Answers map[int]string` to `grillResult` and set it. Update the digest test call site accordingly (`openCriticalDigest(qs, res.Answers)` is the intended production use).

- [ ] **Step 4: Run tests, verify they pass**

Run: same command as Step 2. Expected: PASS. Also run `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run TestIsGoapNotebookLM -count=1` to confirm no grill regression.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/superpowers_grill.go internal/engine/superpowers_grill_test.go
git commit -m "superpowers grill: open-critical branches, body/appendix split, body hash, feedback digest"
```

---

### Task 2: Run-state fields + GrillDesignArtifact reviewer protocol

**Files:**
- Modify: `internal/engine/superpowers_runtime_types.go` (SuperpowersRun fields)
- Modify: `internal/engine/actions_superpowers_prod.go:182-262` (GrillDesignArtifact)
- Test: `internal/engine/actions_superpowers_design_loop_test.go` (create)

**Interfaces:**
- Consumes: Task 1 helpers; `getSuperpowersRun`, `writeSuperpowersRunJSON`; ReviewCycle protocol keys `review_verdict` / `review_feedback`.
- Produces: `SuperpowersRun` fields `GrillRound int`, `DesignRevision int`, `OpenCriticalBranches []string`, `DesignBodyHash string`, `NoProgressRounds int`, `NoProgressTripped bool`, `FollowupPath string`, `FollowupProgramID string` (JSON tags snake_case, `omitempty` for the last two). Reviewer outcomes: `grill_no_progress`, `grill_round_bound`.

- [ ] **Step 1: Add the run-state fields** (no test needed — compile-checked, exercised by Step 2 tests)

```go
// superpowers_runtime_types.go, inside SuperpowersRun after DocDriftSync:
	// Grill-loop bookkeeping (spec 2026-07-15-brainstorm-grill-loop-design.md)
	GrillRound           int      `json:"grill_round,omitempty"`
	DesignRevision       int      `json:"design_revision,omitempty"`
	OpenCriticalBranches []string `json:"open_critical_branches,omitempty"`
	DesignBodyHash       string   `json:"design_body_hash,omitempty"`
	NoProgressRounds     int      `json:"no_progress_rounds,omitempty"`
	NoProgressTripped    bool     `json:"no_progress_tripped,omitempty"`
	FollowupPath         string   `json:"followup_path,omitempty"`
	FollowupProgramID    string   `json:"followup_program_id,omitempty"`
```

- [ ] **Step 2: Write the failing reviewer tests**

Create `internal/engine/actions_superpowers_design_loop_test.go`. Test helper pattern (fake runner emitting Q-lines, fake answerer, temp run):

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

type fakeGrillClaudeRunner struct{ output string; err error; calls int }

func (f *fakeGrillClaudeRunner) RunClaude(_ context.Context, _ string, _ string) CommandResult {
	f.calls++
	return CommandResult{Output: f.output, Err: f.err}
}

func newGrillLoopTestRun(t *testing.T) (*Blackboard, *SuperpowersRun) {
	t.Helper()
	dir := t.TempDir()
	run := &SuperpowersRun{ID: "t", Task: "improve", Mode: SuperpowersModeApply,
		ArtifactDir: dir, DesignPath: filepath.Join(dir, "design.md"), RepoDir: dir}
	if err := os.WriteFile(run.DesignPath,
		[]byte("# Superpowers Design\n\n## Goal\nX\n\n## Architecture\nA\n\n## Acceptance Criteria\nC\n\n## Test Strategy\nT\n\n## Risks\nR\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bb := &Blackboard{ChainState: map[string]any{}}
	setSuperpowersRun(bb, run)
	return bb, run
}

func TestGrillDesign_ApprovedWhenNoOpenCriticals(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	fake := &fakeGrillClaudeRunner{output: "Q [critical] core: is it safe?"}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })
	origAns := grillNotebookLMAnswerer
	grillNotebookLMAnswerer = func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
		out := map[int]string{}
		for i := range batch { out[i] = "yes, because X" }
		return out, nil
	}
	t.Cleanup(func() { grillNotebookLMAnswerer = origAns })

	grill := GetAction("GrillDesignArtifact")
	if got := grill(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if v, _ := bb.ChainState["review_verdict"].(string); v != "approved" {
		t.Fatalf("verdict = %q, want approved", v)
	}
	if run2, _ := getSuperpowersRun(bb); run2.GrillRound != 1 {
		t.Fatalf("GrillRound = %d, want 1", run2.GrillRound)
	}
	data, _ := os.ReadFile(run.DesignPath)
	if !strings.Contains(string(data), "## Grill Q&A — round 1") {
		t.Fatalf("design missing round-tagged appendix: %s", data)
	}
}

func TestGrillDesign_NeedsWorkWithFeedbackWhenCriticalsOpen(t *testing.T) {
	bb, _ := newGrillLoopTestRun(t)
	fake := &fakeGrillClaudeRunner{output: "Q [critical] persistence: what fsyncs?"}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })
	origAns := grillNotebookLMAnswerer
	grillNotebookLMAnswerer = func(_ context.Context, _ []grillQuestion) (map[int]string, error) {
		return nil, errAnswererUnavailable
	}
	t.Cleanup(func() { grillNotebookLMAnswerer = origAns })

	grill := GetAction("GrillDesignArtifact")
	if got := grill(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1 (needs_work is a reviewer SUCCESS)", got)
	}
	if v, _ := bb.ChainState["review_verdict"].(string); v != "needs_work" {
		t.Fatalf("verdict = %q, want needs_work", v)
	}
	fb, _ := bb.ChainState["review_feedback"].(string)
	if !strings.Contains(fb, "OPEN CRITICAL [persistence]") {
		t.Fatalf("feedback = %q", fb)
	}
}

func TestGrillDesign_NoProgressBreakerFailsAfterTwoStaleRounds(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	fake := &fakeGrillClaudeRunner{output: "Q [critical] persistence: what fsyncs?"}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })
	origAns := grillNotebookLMAnswerer
	grillNotebookLMAnswerer = func(_ context.Context, _ []grillQuestion) (map[int]string, error) {
		return nil, errAnswererUnavailable
	}
	t.Cleanup(func() { grillNotebookLMAnswerer = origAns })

	grill := GetAction("GrillDesignArtifact")
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}
	if grill(ctx) != 1 { t.Fatal("round 1 should be needs_work success") }
	if grill(ctx) != 1 { t.Fatal("round 2 should be needs_work success (NoProgressRounds=1)") }
	if got := grill(ctx); got != -1 {
		t.Fatalf("round 3 = %d, want -1 (breaker: 2 consecutive stale rounds)", got)
	}
	if bb.Outcome != "grill_no_progress" {
		t.Fatalf("outcome = %q", bb.Outcome)
	}
	if run2, _ := getSuperpowersRun(bb); !run2.NoProgressTripped {
		t.Fatal("NoProgressTripped not stamped")
	}
	_ = run
}

func TestGrillDesign_RefusesRoundsBeyondBound(t *testing.T) {
	bb, _ := newGrillLoopTestRun(t)
	run, _ := getSuperpowersRun(bb)
	run.GrillRound = 10
	setSuperpowersRun(bb, run)
	fake := &fakeGrillClaudeRunner{output: "Q [critical] x: y?"}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })

	grill := GetAction("GrillDesignArtifact")
	if got := grill(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("status = %d, want -1 (round bound)", got)
	}
	if bb.Outcome != "grill_round_bound" {
		t.Fatalf("outcome = %q", bb.Outcome)
	}
	if fake.calls != 0 {
		t.Fatalf("bound must refuse BEFORE any Claude call, got %d calls", fake.calls)
	}
}
```

- [ ] **Step 3: Run tests, verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestGrillDesign_' -count=1`
Expected: FAIL — verdicts never set, GrillRound stays 0, breaker/bound absent.

- [ ] **Step 4: Rework GrillDesignArtifact (actions_superpowers_prod.go:190)**

Keep the existing prelude (run lookup, design read, dry-run short-circuit — dry-run additionally sets `review_verdict = "approved"` now). Replace the body from the `grillPrompt` on:

```go
		// Round bound: run.GrillRound is authoritative across restarts.
		const grillMaxRounds = 10
		if run.GrillRound >= grillMaxRounds {
			bb.Outcome = "grill_round_bound"
			bb.Result = fmt.Sprintf("## Grill Design Halted\n\nRound bound reached (%d).", grillMaxRounds)
			return -1
		}
		round := run.GrillRound + 1

		grillPrompt := fmt.Sprintf(`Interview this design relentlessly (grill-me). Walk every design-tree branch. Output ONLY lines "Q [critical|normal] <branch>: <question>". Max 12 questions. Mark [critical] only where a wrong answer breaks correctness, data, or security.

## Design

%s`, designContent)

		claudeRes := defaultSuperpowersClaudeRunner.RunClaude(context.Background(), run.WorktreePathOrRepo(), grillPrompt)
		if claudeRes.Err != nil {
			bb.Result = "## Grill Design Failed\n\nClaude question-generation call failed: " + claudeRes.Err.Error() + "\n\n" + claudeRes.Output
			bb.Outcome = "grill_claude_failed"
			return -1
		}
		qs := parseGrillQuestions(claudeRes.Output)
		if len(qs) == 0 {
			bb.Result = "## Grill Design Failed\n\nClaude produced no parseable grill questions.\n\n" + claudeRes.Output
			bb.Outcome = "grill_no_questions_parsed"
			return -1
		}

		res := resolveGrillQuestions(ctx2, qs, grillAnswerers{NotebookLM: grillNotebookLMAnswerer, Web: nil})

		// Round-tagged, append-only Q&A appendix.
		section := grillRoundHeading(round) + strings.TrimPrefix(res.Markdown, "\n## Grill Q&A\n\n")
		if err := os.WriteFile(run.DesignPath, []byte(designContent+section), 0o644); err != nil {
			bb.Result = "## Grill Design Failed\n\n" + err.Error()
			return -1
		}

		// No-progress breaker: same open-critical set AND same body hash as
		// the previous round, twice in a row => reviewer failure => SplitPath.
		body, _ := splitDesignDocument(designContent)
		hash := designBodyHash(body)
		stale := hash == run.DesignBodyHash && slicesEqual(res.OpenCriticalBranches, run.OpenCriticalBranches)
		if stale {
			run.NoProgressRounds++
		} else {
			run.NoProgressRounds = 0
		}
		run.GrillRound = round
		run.DesignBodyHash = hash
		run.OpenCriticalBranches = res.OpenCriticalBranches
		if run.NoProgressRounds >= 2 {
			run.NoProgressTripped = true
			_ = writeSuperpowersRunJSON(run)
			setSuperpowersRun(bb, run)
			bb.Outcome = "grill_no_progress"
			bb.Result = fmt.Sprintf("## Grill Design Halted\n\nNo progress for 2 consecutive rounds (round %d, %d open criticals).", round, res.OpenCritical)
			return -1
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		setSuperpowersRun(bb, run)

		bb.ChainState["grill_open_critical"] = res.OpenCritical
		if res.OpenCritical == 0 {
			bb.ChainState["review_verdict"] = "approved"
			bb.Result = fmt.Sprintf("## Grill Design Approved\n\nRound %d: %d questions, 0 open criticals.", round, len(qs))
			return 1
		}
		bb.ChainState["review_verdict"] = "needs_work"
		bb.ChainState["review_feedback"] = fmt.Sprintf("Grill round %d results:\n%s", round, openCriticalDigest(qs, res.Answers))
		bb.Result = fmt.Sprintf("## Grill Design Needs Work\n\nRound %d: %d questions, %d open criticals.", round, len(qs), res.OpenCritical)
		return 1
```

Notes: `ctx2` = `context.Background()` (name it however the surrounding code does); add tiny helper `slicesEqual(a, b []string) bool` (or use `slices.Equal` — Go 1.26 stdlib) in the same file. The reviewer runs BEFORE the reviser in iteration order (ReviewCycle runs child first — the child's round-1 revise is a no-op).

- [ ] **Step 5: Run tests, verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestGrillDesign_' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/superpowers_runtime_types.go internal/engine/actions_superpowers_prod.go internal/engine/actions_superpowers_design_loop_test.go
git commit -m "superpowers grill: reviewer protocol, round bound, no-progress breaker"
```

---

### Task 3: ReviseDesignArtifact action

**Files:**
- Create: `internal/engine/actions_superpowers_design_loop.go`
- Test: `internal/engine/actions_superpowers_design_loop_test.go` (extend)

**Interfaces:**
- Consumes: `ChainState["review_feedback"]` (string), Task 1 helpers, `defaultSuperpowersClaudeRunner`.
- Produces: registered actions `ReviseDesignArtifact`, plus validation aliases `ValidateRevisedDesign` and `ValidateSplitDesign` (Task 5's tree needs all three names). Extract the heading check from `ValidateDesignArtifact` into `func validateDesignHeadings(content string) []string` (returns missing headings) in this new file and re-point `ValidateDesignArtifact` at it.

- [ ] **Step 1: Write the failing tests** (extend the Task 2 test file)

```go
func TestReviseDesign_NoOpWithoutFeedback(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	fake := &fakeGrillClaudeRunner{output: "IGNORED"}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })

	revise := GetAction("ReviseDesignArtifact")
	if got := revise(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	if fake.calls != 0 {
		t.Fatalf("round-1 revise must not call Claude, got %d", fake.calls)
	}
	if run2, _ := getSuperpowersRun(bb); run2.DesignRevision != 0 {
		t.Fatalf("DesignRevision = %d, want 0", run2.DesignRevision)
	}
	_ = run
}

func TestReviseDesign_RewritesBodyPreservesAppendix(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	appendix := "\n## Grill Q&A — round 1\n\n**Q (critical, p):** q?\n\n**A:** OPEN — no answerer available\n"
	orig, _ := os.ReadFile(run.DesignPath)
	os.WriteFile(run.DesignPath, append(orig, []byte(appendix)...), 0o644)
	bb.ChainState["review_feedback"] = "OPEN CRITICAL [p]: q?"

	revised := "# Superpowers Design\n\n## Goal\nX2\n\n## Architecture\nA2 (p resolved: uses fsync)\n\n## Acceptance Criteria\nC\n\n## Test Strategy\nT\n\n## Risks\nR\n"
	fake := &fakeGrillClaudeRunner{output: revised}
	origRunner := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = origRunner })

	revise := GetAction("ReviseDesignArtifact")
	if got := revise(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1", got)
	}
	data, _ := os.ReadFile(run.DesignPath)
	if !strings.Contains(string(data), "A2 (p resolved: uses fsync)") {
		t.Fatalf("body not rewritten: %s", data)
	}
	if !strings.Contains(string(data), "## Grill Q&A — round 1") {
		t.Fatalf("appendix lost: %s", data)
	}
	if run2, _ := getSuperpowersRun(bb); run2.DesignRevision != 1 {
		t.Fatalf("DesignRevision = %d, want 1", run2.DesignRevision)
	}
}

func TestReviseDesign_ClaudeFailureIsNoOp(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	bb.ChainState["review_feedback"] = "OPEN CRITICAL [p]: q?"
	fake := &fakeGrillClaudeRunner{err: context.DeadlineExceeded}
	origRunner := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = origRunner })
	before, _ := os.ReadFile(run.DesignPath)

	revise := GetAction("ReviseDesignArtifact")
	if got := revise(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1 (failed revision must no-op, not kill the loop)", got)
	}
	after, _ := os.ReadFile(run.DesignPath)
	if string(before) != string(after) {
		t.Fatal("design must be unchanged after failed revision")
	}
}

func TestValidationAliasesRegistered(t *testing.T) {
	if GetAction("ValidateRevisedDesign") == nil || GetAction("ValidateSplitDesign") == nil {
		t.Fatal("validation alias actions not registered")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestReviseDesign_|TestValidationAliases' -count=1`
Expected: FAIL — `ReviseDesignArtifact` not registered.

- [ ] **Step 3: Implement `actions_superpowers_design_loop.go`**

```go
// Package engine — grill-driven design-improvement loop actions
// (spec: docs/superpowers/specs/2026-07-15-brainstorm-grill-loop-design.md).
package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() { registerSuperpowersDesignLoopActions() }

// validateDesignHeadings returns the required headings missing from content.
// (Task: also re-point ValidateDesignArtifact's inline loop at this helper.)
func validateDesignHeadings(content string) []string {
	var missing []string
	for _, h := range []string{"## Goal", "## Architecture", "## Acceptance Criteria", "## Test Strategy", "## Risks"} {
		if !strings.Contains(content, h) {
			missing = append(missing, h)
		}
	}
	return missing
}

func validateDesignAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	run, ok := getSuperpowersRun(bb)
	if !ok || run.DesignPath == "" {
		bb.Result = "## Design Validation Failed\n\nNo Superpowers design path in run state."
		return -1
	}
	data, err := os.ReadFile(run.DesignPath)
	if err != nil {
		bb.Result = "## Design Validation Failed\n\n" + err.Error()
		return -1
	}
	if missing := validateDesignHeadings(string(data)); len(missing) > 0 {
		bb.Result = "## Design Validation Failed\n\nMissing: " + strings.Join(missing, ", ")
		return -1
	}
	bb.Result = "## Design Validated"
	return 1
}

func registerSuperpowersDesignLoopActions() {
	RegisterAction("ValidateRevisedDesign", validateDesignAction)
	RegisterAction("ValidateSplitDesign", validateDesignAction)

	// ReviseDesignArtifact rewrites the design BODY from the previous grill
	// round's feedback; the Q&A appendix is append-only and preserved. A
	// failed Claude call no-ops (unchanged design) so the reviewer's
	// no-progress breaker — not a child failure — ends a stuck loop.
	RegisterAction("ReviseDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok || run.DesignPath == "" {
			bb.Result = "## Revise Design Failed\n\nNo Superpowers design path in run state."
			return -1
		}
		feedback, _ := bb.ChainState["review_feedback"].(string)
		if strings.TrimSpace(feedback) == "" {
			bb.Result = "## Revise Design Skipped\n\nNo grill feedback yet (round 1)."
			return 1
		}
		if run.Mode == SuperpowersModeDryRun {
			bb.Result = "## Revise Design (dry run)\n\nClaude call skipped."
			return 1
		}
		data, err := os.ReadFile(run.DesignPath)
		if err != nil {
			bb.Result = "## Revise Design Failed\n\n" + err.Error()
			return -1
		}
		body, appendix := splitDesignDocument(string(data))

		prompt := fmt.Sprintf(`Revise this design document based on the grill round results below. Incorporate every ANSWERED insight into the design body. For each OPEN CRITICAL question, either answer it from your knowledge of this repository or change the design so the risk it probes no longer exists — record which you did in the relevant section.

Rules:
- Output ONLY the revised design body markdown, starting with "# ".
- Keep the exact section headings: ## Goal, ## Architecture, ## Acceptance Criteria, ## Test Strategy, ## Risks.
- Do NOT output any "## Grill Q&A" section.

## Current design body

%s

## Grill round results

%s`, body, feedback)

		res := defaultSuperpowersClaudeRunner.RunClaude(context.Background(), run.WorktreePathOrRepo(), prompt)
		if res.Err != nil {
			bb.Result = "## Revise Design Degraded\n\nClaude revision failed; keeping design unchanged: " + res.Err.Error()
			bb.Outcome = "revise_claude_failed_noop"
			return 1
		}
		revised := strings.TrimSpace(res.Output)
		if !strings.HasPrefix(revised, "# ") || len(validateDesignHeadings(revised)) > 0 {
			bb.Result = "## Revise Design Degraded\n\nClaude output not a valid design body; keeping design unchanged."
			bb.Outcome = "revise_output_invalid_noop"
			return 1
		}
		// Defensive re-assembly: appendix always re-attached from the
		// pre-revision file, so a drifting rewrite can never eat the audit
		// trail.
		if err := os.WriteFile(run.DesignPath, []byte(revised+"\n"+appendix), 0o644); err != nil {
			bb.Result = "## Revise Design Failed\n\n" + err.Error()
			return -1
		}
		run.DesignRevision++
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		setSuperpowersRun(bb, run)
		bb.Result = fmt.Sprintf("## Design Revised\n\nRevision %d applied from grill feedback.", run.DesignRevision)
		return 1
	})
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestReviseDesign_|TestValidationAliases' -count=1`
Expected: PASS. Then refactor `ValidateDesignArtifact` (actions_superpowers_prod.go:159) to call `validateDesignHeadings` and re-run `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestValidate|TestSuperpowers' -count=1` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/actions_superpowers_design_loop.go internal/engine/actions_superpowers_design_loop_test.go internal/engine/actions_superpowers_prod.go
git commit -m "superpowers: ReviseDesignArtifact + shared design validation"
```

---

### Task 4: SplitDesignArtifact action + follow-up program persistence

**Files:**
- Modify: `internal/engine/actions_superpowers_design_loop.go`
- Test: `internal/engine/actions_superpowers_design_loop_test.go` (extend)

**Interfaces:**
- Consumes: `run.OpenCriticalBranches`, Task 1 helpers, `extractGoapProgram`, `validateGoapProgramMilestones`, `persistGoapProgram`, `isolateGoapProgramStore(t)` (test helper in `actions_goap_fusion_test.go`).
- Produces: registered action `SplitDesignArtifact`; artifacts `design.md` (clear scope + `## Grill Loop Summary`), `design-followup.md`; run fields `FollowupPath`, `FollowupProgramID` (= program title).

Claude output protocol for the split call (three fenced markers, parsed with `strings.Index`):

```
=== CLEAR DESIGN ===
<full design body, required headings, ONLY the implementable-now scope>
=== FOLLOWUP ===
<standalone follow-up spec markdown for the deferred scope>
=== PROGRAM ===
PROGRAM: <title>
MILESTONE1: <file-scoped milestone naming repo-relative Go files>
MILESTONE2: ...
```

- [ ] **Step 1: Write the failing tests**

```go
const splitFakeOutput = `=== CLEAR DESIGN ===
# Superpowers Design

## Goal
Clear part only

## Architecture
A-clear

## Acceptance Criteria
C

## Test Strategy
T

## Risks
R
=== FOLLOWUP ===
# Follow-up: deferred persistence scope

Open critical: what fsyncs? Deferred pending answer.
=== PROGRAM ===
PROGRAM: Design follow-up: persistence hardening
MILESTONE1: Answer the fsync question and harden internal/engine/superpowers_artifacts.go (files: internal/engine/superpowers_artifacts.go)
`

func TestSplitDesign_WritesArtifactsAndPersistsProgram(t *testing.T) {
	isolateGoapProgramStore(t)
	bb, run := newGrillLoopTestRun(t)
	run.OpenCriticalBranches = []string{"persistence"}
	run.GrillRound = 10
	setSuperpowersRun(bb, run)
	fake := &fakeGrillClaudeRunner{output: splitFakeOutput}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })

	split := GetAction("SplitDesignArtifact")
	if got := split(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1; result: %s", got, bb.Result)
	}
	design, _ := os.ReadFile(run.DesignPath)
	if !strings.Contains(string(design), "Clear part only") || strings.Contains(string(design), "Deferred pending answer") {
		t.Fatalf("design.md not reduced to clear scope: %s", design)
	}
	if !strings.Contains(string(design), "## Grill Loop Summary") {
		t.Fatalf("design.md missing grill loop summary: %s", design)
	}
	run2, _ := getSuperpowersRun(bb)
	followup, err := os.ReadFile(run2.FollowupPath)
	if err != nil || !strings.Contains(string(followup), "deferred persistence scope") {
		t.Fatalf("followup artifact missing: %v %s", err, followup)
	}
	if run2.FollowupProgramID != "Design follow-up: persistence hardening" {
		t.Fatalf("FollowupProgramID = %q", run2.FollowupProgramID)
	}
	if reg, _ := bb.ChainState["goap_fusion_program_registered"].(string); reg == "" {
		t.Fatal("program not persisted to store")
	}
}

func TestSplitDesign_NothingClearFails(t *testing.T) {
	isolateGoapProgramStore(t)
	bb, run := newGrillLoopTestRun(t)
	run.OpenCriticalBranches = []string{"everything"}
	setSuperpowersRun(bb, run)
	// Claude returns an empty clear section
	fake := &fakeGrillClaudeRunner{output: "=== CLEAR DESIGN ===\n\n=== FOLLOWUP ===\nall deferred\n=== PROGRAM ===\nPROGRAM: t\nMILESTONE1: x (files: internal/engine/tree.go)\n"}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })

	split := GetAction("SplitDesignArtifact")
	if got := split(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != -1 {
		t.Fatalf("status = %d, want -1 (nothing clear)", got)
	}
	if bb.Outcome != "split_nothing_clear" {
		t.Fatalf("outcome = %q", bb.Outcome)
	}
}

func TestSplitDesign_InvalidMilestonesStillWritesArtifacts(t *testing.T) {
	isolateGoapProgramStore(t)
	bb, run := newGrillLoopTestRun(t)
	run.OpenCriticalBranches = []string{"p"}
	setSuperpowersRun(bb, run)
	out := strings.Replace(splitFakeOutput,
		"MILESTONE1: Answer the fsync question and harden internal/engine/superpowers_artifacts.go (files: internal/engine/superpowers_artifacts.go)",
		"MILESTONE1: vague milestone touching no files", 1)
	fake := &fakeGrillClaudeRunner{output: out}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })

	split := GetAction("SplitDesignArtifact")
	if got := split(&btcore.BTContext[Blackboard]{Blackboard: bb}); got != 1 {
		t.Fatalf("status = %d, want 1 (artifact still lands; pickup manual)", got)
	}
	run2, _ := getSuperpowersRun(bb)
	if run2.FollowupProgramID != "" {
		t.Fatalf("FollowupProgramID = %q, want empty (program rejected)", run2.FollowupProgramID)
	}
	if !strings.Contains(bb.Result, "manual") {
		t.Fatalf("result must flag manual pickup: %s", bb.Result)
	}
}
```

Note: check the exact ChainState key `persistGoapProgram` sets (`setGoapState(bb, "program_registered", …)` prefixes keys with `goap_fusion_` — verify with `grep -n "func setGoapState" internal/engine/actions_goap_fusion*.go` and adjust the first test's assertion key to match).

- [ ] **Step 2: Run tests, verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestSplitDesign_' -count=1`
Expected: FAIL — `SplitDesignArtifact` not registered.

- [ ] **Step 3: Implement `SplitDesignArtifact`** (append to `registerSuperpowersDesignLoopActions`)

```go
	RegisterAction("SplitDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok || run.DesignPath == "" {
			bb.Result = "## Split Design Failed\n\nNo Superpowers design path in run state."
			return -1
		}
		if run.Mode == SuperpowersModeDryRun {
			bb.Result = "## Split Design (dry run)\n\nClaude call skipped."
			return 1
		}
		data, err := os.ReadFile(run.DesignPath)
		if err != nil {
			bb.Result = "## Split Design Failed\n\n" + err.Error()
			return -1
		}
		body, appendix := splitDesignDocument(string(data))

		prompt := fmt.Sprintf(`This design has unresolved critical questions on these branches: %s.
Partition it. The CLEAR DESIGN keeps only scope NOT blocked by those branches (same headings: ## Goal, ## Architecture, ## Acceptance Criteria, ## Test Strategy, ## Risks). The FOLLOWUP is a standalone spec for the deferred scope including the open questions. The PROGRAM turns the followup into 2-5 milestones, each naming the repo-relative Go files it touches.
Output EXACTLY:
=== CLEAR DESIGN ===
<markdown>
=== FOLLOWUP ===
<markdown>
=== PROGRAM ===
PROGRAM: <title>
MILESTONE1: <milestone (files: path1,path2)>

## Design body

%s

## Grill Q&A history

%s`, strings.Join(run.OpenCriticalBranches, ", "), body, appendix)

		res := defaultSuperpowersClaudeRunner.RunClaude(context.Background(), run.WorktreePathOrRepo(), prompt)
		if res.Err != nil {
			bb.Result = "## Split Design Failed\n\n" + res.Err.Error()
			bb.Outcome = "split_claude_failed"
			return -1
		}
		clear, followup, programText, perr := parseSplitOutput(res.Output)
		if perr != nil {
			bb.Result = "## Split Design Failed\n\n" + perr.Error() + "\n\n" + truncateGoap(res.Output, 1500)
			bb.Outcome = "split_output_invalid"
			return -1
		}
		if strings.TrimSpace(clear) == "" || len(validateDesignHeadings(clear)) > 0 {
			bb.Result = "## Split Design Failed\n\nNo implementable clear scope remained."
			bb.Outcome = "split_nothing_clear"
			return -1
		}

		summary := fmt.Sprintf("\n## Grill Loop Summary\n\n- Rounds used: %d\n- Design revisions: %d\n- Deferred branches: %s\n- Follow-up spec: design-followup.md\n",
			run.GrillRound, run.DesignRevision, strings.Join(run.OpenCriticalBranches, ", "))
		if err := os.WriteFile(run.DesignPath, []byte(strings.TrimSpace(clear)+"\n"+summary+appendix), 0o644); err != nil {
			bb.Result = "## Split Design Failed\n\n" + err.Error()
			return -1
		}
		run.FollowupPath = filepath.Join(run.ArtifactDir, "design-followup.md")
		if err := os.WriteFile(run.FollowupPath, []byte(followup), 0o644); err != nil {
			bb.Result = "## Split Design Failed\n\n" + err.Error()
			return -1
		}

		pickup := "manual (no valid program milestones)"
		if spec := extractGoapProgram(programText); spec != nil {
			v := validateGoapProgramMilestones(spec.Milestones)
			if len(v.Valid) >= 1 {
				spec.Milestones = v.Valid
				persistGoapProgram(bb, spec, "design-followup")
				run.FollowupProgramID = spec.Title
				pickup = fmt.Sprintf("goap program %q (%d milestones)", spec.Title, len(v.Valid))
			}
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		setSuperpowersRun(bb, run)
		bb.Result = fmt.Sprintf("## Design Split\n\nClear scope kept; deferred scope → %s; pickup: %s", run.FollowupPath, pickup)
		return 1
	})
```

And the parser (same file):

```go
func parseSplitOutput(out string) (clear, followup, program string, err error) {
	const mClear, mFollow, mProg = "=== CLEAR DESIGN ===", "=== FOLLOWUP ===", "=== PROGRAM ==="
	ci, fi, pi := strings.Index(out, mClear), strings.Index(out, mFollow), strings.Index(out, mProg)
	if ci < 0 || fi < 0 || pi < 0 || !(ci < fi && fi < pi) {
		return "", "", "", fmt.Errorf("split output missing ordered markers")
	}
	return out[ci+len(mClear) : fi], out[fi+len(mFollow) : pi], out[pi+len(mProg):], nil
}
```

(Imports: add `"path/filepath"`.)

- [ ] **Step 4: Run tests, verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run 'TestSplitDesign_|TestParseSplitOutput' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/actions_superpowers_design_loop.go internal/engine/actions_superpowers_design_loop_test.go
git commit -m "superpowers: SplitDesignArtifact with goap follow-up program persistence"
```

---

### Task 5: Rewire BrainstormBranch

**Files:**
- Modify: `internal/domains/superpowers_workflow.go:17-38` (brainstorm branch)
- Test: `internal/domains/superpowers_workflow_test.go`, plus run `internal/engine/superpowers_workflow_build_test.go`

**Interfaces:**
- Consumes: registered actions from Tasks 2-4; `ReviewCycle` node (`reviewer_action`, `max_iterations` metadata).
- Produces: the new tree shape below — node names are load-bearing for tests.

- [ ] **Step 1: Write the failing tree test**

```go
// extend superpowers_workflow_test.go
func TestSuperpowersWorkflowTree_GrillLoopShape(t *testing.T) {
	tree := SuperpowersWorkflowTree()
	brainstorm := findNode(tree, "BrainstormBranch") // add tiny recursive helper if absent
	if brainstorm == nil {
		t.Fatal("BrainstormBranch missing")
	}
	router := findNode(brainstorm, "GrillConvergenceRouter")
	if router == nil || router.Type != "Selector" {
		t.Fatalf("GrillConvergenceRouter missing or wrong type: %+v", router)
	}
	loop := findNode(router, "GrillLoop")
	if loop == nil || loop.Type != "ReviewCycle" {
		t.Fatal("GrillLoop ReviewCycle missing")
	}
	if ra, _ := loop.Metadata["reviewer_action"].(string); ra != "GrillDesignArtifact" {
		t.Fatalf("reviewer_action = %q", ra)
	}
	if mi, _ := loop.Metadata["max_iterations"].(int); mi != 10 {
		t.Fatalf("max_iterations = %v", loop.Metadata["max_iterations"])
	}
	round := findNode(loop, "GrillRound")
	if round == nil || round.Type != "MemSequence" || len(round.Children) != 2 ||
		round.Children[0].Name != "ReviseDesignArtifact" || round.Children[1].Name != "ValidateRevisedDesign" {
		t.Fatalf("GrillRound shape wrong: %+v", round)
	}
	split := findNode(router, "SplitPath")
	if split == nil || split.Children[0].Name != "SplitDesignArtifact" || split.Children[1].Name != "ValidateSplitDesign" {
		t.Fatalf("SplitPath shape wrong: %+v", split)
	}
	if findNode(brainstorm, "GrillDesignArtifact") != nil {
		t.Fatal("standalone GrillDesignArtifact leaf must be gone (it is the reviewer now)")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/domains -run TestSuperpowersWorkflowTree_GrillLoopShape -count=1`
Expected: FAIL — GrillConvergenceRouter missing.

- [ ] **Step 3: Rewire the branch** (replace the `brainstorm` literal in `superpowers_workflow.go`)

```go
	// grill loop: ReviewCycle re-runs [revise → validate] while the reviewer
	// (GrillDesignArtifact) verdicts needs_work; reviewer failure (protocol
	// error, no-progress breaker, round bound) fails the cycle and the
	// Selector falls through to SplitPath. NOTE: do NOT "simplify" this to a
	// Retry node — the engine's Retry maps to a repeat-on-success decorator
	// that fails immediately on child failure (see spec 2026-07-15).
	grillLoop := evolution.SerializableNode{
		Type:        "ReviewCycle",
		Name:        "GrillLoop",
		Description: "Bounded revise→validate→grill loop driven by grill verdicts (max 10 rounds)",
		Metadata: map[string]any{
			"reviewer_action": "GrillDesignArtifact",
			"max_iterations":  10,
		},
		Children: []evolution.SerializableNode{
			{
				Type:        "MemSequence",
				Name:        "GrillRound",
				Description: "One design-improvement round: revise from grill feedback, then re-validate",
				Children: []evolution.SerializableNode{
					act("ReviseDesignArtifact", "Rewrite the design body from the previous grill round's answers and open criticals (no-op on round 1)"),
					act("ValidateRevisedDesign", "Strictly validate the revised design's required sections"),
				},
			},
		},
	}

	splitPath := evolution.SerializableNode{
		Type:        "MemSequence",
		Name:        "SplitPath",
		Description: "Exhausted/stuck loop: keep the clear scope, defer open-critical scope to a goap follow-up program",
		Children: []evolution.SerializableNode{
			act("SplitDesignArtifact", "Partition the design: clear scope stays, deferred scope becomes design-followup.md + a design-followup goap program"),
			act("ValidateSplitDesign", "Strictly validate the reduced clear-scope design"),
		},
	}

	brainstorm := evolution.SerializableNode{
		Type:        "MemSequence",
		Name:        "BrainstormBranch",
		Description: "Creative path: generate → validate → grill-loop (revise until clear) or split, then approve",
		Metadata:    map[string]any{"match": "creative"},
		Children: []evolution.SerializableNode{
			act("GenerateDesignArtifact", "Write or reuse design.md with architecture, acceptance criteria, tests, and risks"),
			act("ValidateDesignArtifact", "Strictly validate design.md required sections"),
			sel("GrillConvergenceRouter", "Run the grill loop to convergence, else split the design into clear scope + follow-up",
				grillLoop,
				splitPath,
			),
			{
				Type:        "HumanApprovalGate",
				Name:        "ApproveDesign",
				Description: "Approve the grilled design artifact before proceeding",
				Metadata: map[string]any{
					"phase":       "pre",
					"hitl_prompt": "Approve the design artifact (after the grill loop; check the Grill Loop Summary section for deferred scope) before implementation?",
				},
			},
		},
	}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/domains ./internal/engine -run 'TestSuperpowersWorkflow' -count=1`
Expected: PASS (both the domains shape tests and the engine build/validate test).

- [ ] **Step 5: Commit**

```bash
git add internal/domains/superpowers_workflow.go internal/domains/superpowers_workflow_test.go
git commit -m "superpowers workflow: grill-driven design-improvement loop in BrainstormBranch"
```

---

### Task 6: End-to-end loop test + full gate

**Files:**
- Test: `internal/engine/actions_superpowers_design_loop_test.go` (extend)

**Interfaces:** consumes everything above; produces no new symbols.

- [ ] **Step 1: Write the failing integration test** — drive the REAL built tree's BrainstormBranch via the ReviewCycle node with scripted fakes: round 1 grill leaves 1 open critical (needs_work), round 2 revise rewrites body, round 2 grill answers everything (approved).

```go
func TestGrillLoop_EndToEnd_ConvergesInTwoRounds(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	round := 0
	fake := &scriptedClaudeRunner{fn: func(prompt string) CommandResult {
		if strings.Contains(prompt, "Interview this design relentlessly") {
			round++
			return CommandResult{Output: "Q [critical] persistence: what fsyncs?"}
		}
		// revision call
		return CommandResult{Output: "# Superpowers Design\n\n## Goal\nX\n\n## Architecture\nA2 fsync-safe\n\n## Acceptance Criteria\nC\n\n## Test Strategy\nT\n\n## Risks\nR\n"}
	}}
	orig := defaultSuperpowersClaudeRunner
	defaultSuperpowersClaudeRunner = fake
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = orig })
	origAns := grillNotebookLMAnswerer
	grillNotebookLMAnswerer = func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
		if round == 1 {
			return nil, errAnswererUnavailable // round 1: open critical
		}
		out := map[int]string{}
		for i := range batch { out[i] = "fsync via tmp+rename" }
		return out, nil
	}
	t.Cleanup(func() { grillNotebookLMAnswerer = origAns })

	node := evolution.SerializableNode{Type: "ReviewCycle", Name: "GrillLoop",
		Metadata: map[string]any{"reviewer_action": "GrillDesignArtifact", "max_iterations": 10},
		Children: []evolution.SerializableNode{{Type: "MemSequence", Name: "GrillRound",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: "ReviseDesignArtifact"},
				{Type: "Action", Name: "ValidateRevisedDesign"},
			}}}}
	cmd := BuildReviewCycle(&node, bb)
	if got := cmd.Run(newTestBTContext(bb)); got != 1 { // use the package's existing helper for BTContext construction if one exists; else build btcore context as other engine tests do
		t.Fatalf("loop = %d, want 1 (converged)", got)
	}
	run2, _ := getSuperpowersRun(bb)
	if run2.GrillRound != 2 || run2.DesignRevision != 1 {
		t.Fatalf("rounds/revisions = %d/%d, want 2/1", run2.GrillRound, run2.DesignRevision)
	}
	data, _ := os.ReadFile(run.DesignPath)
	if !strings.Contains(string(data), "A2 fsync-safe") ||
		!strings.Contains(string(data), "## Grill Q&A — round 1") ||
		!strings.Contains(string(data), "## Grill Q&A — round 2") {
		t.Fatalf("final design wrong: %s", data)
	}
}

type scriptedClaudeRunner struct{ fn func(prompt string) CommandResult }
func (s *scriptedClaudeRunner) RunClaude(_ context.Context, _ string, p string) CommandResult { return s.fn(p) }
```

(Adapt the BTContext construction to whatever `actions_superpowers_prod_test.go` already does — copy its pattern exactly.)

- [ ] **Step 2: Run test, verify it fails** (before any fix it may pass if Tasks 2-5 are complete — if it passes immediately, verify it by breaking the reviewer verdict temporarily; the test's value is regression coverage of the loop wiring.)

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine -run TestGrillLoop_EndToEnd -count=1`

- [ ] **Step 3: Full suite + gate**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine ./internal/domains -short -count=1 -timeout 600s`
Expected: PASS (known flake exception: `TestCMAESOptimizer_Convergence`, retry in isolation if sole failure).

- [ ] **Step 4: Commit**

```bash
git add internal/engine/actions_superpowers_design_loop_test.go
git commit -m "superpowers: end-to-end grill-loop convergence test"
```

---

## Self-review notes (already applied)

- Spec coverage: revise (T3), reviewer protocol + breaker + bound (T2), split + program store (T4), tree + HITL wording (T5), resume-safety via run JSON (T2) — resume TEST is covered by the round-bound test (authoritative `run.GrillRound`) rather than a process-kill harness; the loop summary lands in `design.md` because HITL prompts are static node metadata (verified `promptFromNode`, `hitl_gate.go:66`).
- Type consistency: `grillResult.Answers` added in T1 is consumed in T2's feedback digest; validation aliases registered in T3 are referenced by T5's tree.
- The exact ChainState key assertion in T4 Step 1 must be checked against `setGoapState`'s prefixing before running (noted inline).
