package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
