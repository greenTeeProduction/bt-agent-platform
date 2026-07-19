package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

type fakeGrillClaudeRunner struct {
	output string
	err    error
	calls  int
}

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
		for i := range batch {
			out[i] = "yes, because X"
		}
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
	if grill(ctx) != 1 {
		t.Fatal("round 1 should be needs_work success")
	}
	if grill(ctx) != 1 {
		t.Fatal("round 2 should be needs_work success (NoProgressRounds=1)")
	}
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
	if err := os.WriteFile(run.DesignPath, append(orig, []byte(appendix)...), 0o644); err != nil {
		t.Fatal(err)
	}
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
	oldEx := goapFusionRepoFileExistsFn
	goapFusionRepoFileExistsFn = func(string) bool { return true }
	t.Cleanup(func() { goapFusionRepoFileExistsFn = oldEx })
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

func TestParseSplitOutput(t *testing.T) {
	clearScope, followup, program, err := parseSplitOutput(splitFakeOutput)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(clearScope, "Clear part only") {
		t.Fatalf("clear = %q", clearScope)
	}
	if !strings.Contains(followup, "deferred persistence scope") {
		t.Fatalf("followup = %q", followup)
	}
	if !strings.Contains(program, "PROGRAM: Design follow-up: persistence hardening") {
		t.Fatalf("program = %q", program)
	}

	if _, _, _, err := parseSplitOutput("no markers here"); err == nil {
		t.Fatal("want error for missing markers")
	}
	if _, _, _, err := parseSplitOutput("=== FOLLOWUP ===\nx\n=== CLEAR DESIGN ===\ny\n=== PROGRAM ===\nz"); err == nil {
		t.Fatal("want error for out-of-order markers")
	}
}

// promptDispatchClaudeRunner dispatches on prompt content so a single fake can
// drive both the grill (question-generation) and revise (rewrite) Claude calls
// that ReviewCycle fires in alternation. (Named distinctly from the
// scriptedClaudeRunner in superpowers_task_executor_test.go, which records
// prompts/events rather than branching on them.)
type promptDispatchClaudeRunner struct {
	fn func(prompt string) CommandResult
}

func (s *promptDispatchClaudeRunner) RunClaude(_ context.Context, _ string, p string) CommandResult {
	return s.fn(p)
}

// TestGrillLoop_EndToEnd_ConvergesInTwoRounds drives the REAL BuildReviewCycle
// node (not the individual actions in isolation) through two grill rounds:
// round 1 leaves a critical question OPEN (needs_work), ReviseDesignArtifact
// then rewrites the body from that feedback, and round 2's grill answers
// everything (approved). This is regression coverage for the loop wiring
// itself — ChainState propagation between reviewer and child, MemSequence
// cursor reset across ReviewCycle iterations, and round/revision bookkeeping
// on the run — none of which the per-action unit tests above exercise.
func TestGrillLoop_EndToEnd_ConvergesInTwoRounds(t *testing.T) {
	bb, run := newGrillLoopTestRun(t)
	round := 0
	fake := &promptDispatchClaudeRunner{fn: func(prompt string) CommandResult {
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
		for i := range batch {
			out[i] = "fsync via tmp+rename"
		}
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
	if got := cmd.Run(newTestBTContext(bb)); got != 1 {
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
