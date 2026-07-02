package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

type fakeGrillClaude struct {
	out string
	err error
}

func (f fakeGrillClaude) RunClaude(_ context.Context, _ string, _ string) CommandResult {
	return CommandResult{Output: f.out, Err: f.err}
}

func TestParseGrillQuestions(t *testing.T) {
	out := `Q [critical] D4-persistence: How does the cursor survive JSON round-trips?
Q [normal] D2-routing: Why heuristics before LLM?
noise line`
	qs := parseGrillQuestions(out)
	if len(qs) != 2 {
		t.Fatalf("want 2 questions, got %d", len(qs))
	}
	if !qs[0].Critical || qs[0].Branch != "D4-persistence" {
		t.Fatalf("bad parse: %+v", qs[0])
	}
}

// TestParseGrillQuestions_SeverityTagIsTrimmedAndCaseFolded guards against a
// safety-gate downgrade: "[Critical]" or "[ CRITICAL ]" (mixed case, stray
// whitespace) must still be classified Critical, not silently treated as
// "normal" because the raw token didn't byte-for-byte equal "critical".
func TestParseGrillQuestions_SeverityTagIsTrimmedAndCaseFolded(t *testing.T) {
	out := `Q [Critical] D1-auth: does this leak credentials?
Q [ CRITICAL ] D2-data: does this corrupt data on retry?`
	qs := parseGrillQuestions(out)
	if len(qs) != 2 {
		t.Fatalf("want 2 questions, got %d: %+v", len(qs), qs)
	}
	if !qs[0].Critical {
		t.Fatalf("want [Critical] (mixed case) to be classified Critical: %+v", qs[0])
	}
	if !qs[1].Critical {
		t.Fatalf("want [ CRITICAL ] (padded) to be classified Critical: %+v", qs[1])
	}
}

func TestGrillDesign_FailsOnOpenCriticalWhenAllAnswerersDown(t *testing.T) {
	qs := []grillQuestion{{Critical: true, Branch: "D1", Text: "unanswerable"}}
	res := resolveGrillQuestions(context.Background(), qs, grillAnswerers{
		NotebookLM: func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
			return nil, errAnswererUnavailable
		},
		Web: func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
			return nil, errAnswererUnavailable
		},
	})
	if res.OpenCritical != 1 {
		t.Fatalf("want 1 open critical, got %d", res.OpenCritical)
	}
	if !strings.Contains(res.Markdown, "OPEN") {
		t.Fatal("open questions must be marked OPEN in the Q&A markdown")
	}
}

// TestGrillDesignArtifactAction_OpenCriticalFailsWithBothAnswerersDown drives
// the registered GrillDesignArtifact action end to end with a fake Claude
// runner and a NotebookLM answerer stubbed to errAnswererUnavailable (the
// Web answerer is nil in production — no compatible batched-question
// web-research action exists, see actions_superpowers_prod.go). It never
// touches the real nlm or claude binaries: defaultSuperpowersClaudeRunner and
// grillNotebookLMAnswerer are the same swappable package vars the sibling
// RED/GREEN/REVIEW phase actions use for this exact reason.
func TestGrillDesignArtifactAction_OpenCriticalFailsWithBothAnswerersDown(t *testing.T) {
	t.Chdir(t.TempDir())

	prevClaude := defaultSuperpowersClaudeRunner
	prevNLM := grillNotebookLMAnswerer
	t.Cleanup(func() {
		defaultSuperpowersClaudeRunner = prevClaude
		grillNotebookLMAnswerer = prevNLM
	})
	defaultSuperpowersClaudeRunner = fakeGrillClaude{out: "Q [critical] D1-persistence: does the cursor survive restarts?\n" +
		"Q [normal] D2-routing: why heuristics before LLM?\n"}
	grillNotebookLMAnswerer = func(_ context.Context, _ []grillQuestion) (map[int]string, error) {
		return nil, errAnswererUnavailable
	}

	artifactDir := t.TempDir()
	designPath := filepath.Join(artifactDir, "design.md")
	original := "## Goal\n\nDo the thing.\n"
	if err := os.WriteFile(designPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed design.md: %v", err)
	}

	run := &SuperpowersRun{
		ID:          "run-grill",
		Mode:        SuperpowersModeApply,
		ArtifactDir: artifactDir,
		DesignPath:  designPath,
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)

	act := GetAction("GrillDesignArtifact")
	if act == nil {
		t.Fatal("GrillDesignArtifact not registered")
	}

	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("result = %d, want -1 (FAILURE) with an open critical question", result)
	}

	openCritical, ok := bb.ChainState["grill_open_critical"].(int)
	if !ok || openCritical != 1 {
		t.Fatalf("ChainState[grill_open_critical] = %v (ok=%v), want 1", bb.ChainState["grill_open_critical"], ok)
	}

	data, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatalf("read design.md: %v", err)
	}
	content := string(data)
	if !strings.HasPrefix(content, original) {
		t.Fatalf("design.md original content was not preserved; got %q", content)
	}
	if !strings.Contains(content, "## Grill Q&A") {
		t.Fatalf("design.md missing '## Grill Q&A' section; got %q", content)
	}
	if !strings.Contains(content, "OPEN") {
		t.Fatalf("design.md missing OPEN marker for the unanswered critical question; got %q", content)
	}
}

// setupGrillRun seeds a design.md and returns a run+blackboard wired for
// GrillDesignArtifact, mirroring TestGrillDesignArtifactAction_OpenCriticalFailsWithBothAnswerersDown's
// fixture so the finding-1/5(e)/5(f) tests below don't repeat the boilerplate.
func setupGrillRun(t *testing.T, mode SuperpowersMode) (*Blackboard, string) {
	t.Helper()
	artifactDir := t.TempDir()
	designPath := filepath.Join(artifactDir, "design.md")
	original := "## Goal\n\nDo the thing.\n"
	if err := os.WriteFile(designPath, []byte(original), 0o644); err != nil {
		t.Fatalf("seed design.md: %v", err)
	}
	run := &SuperpowersRun{
		ID:          "run-grill",
		Mode:        mode,
		ArtifactDir: artifactDir,
		DesignPath:  designPath,
	}
	bb := newTestBlackboard()
	setSuperpowersRun(bb, run)
	return bb, designPath
}

// TestGrillDesignArtifactAction_ClaudeErrorFailsInsteadOfSilentSuccess proves
// finding 1: a failed Claude question-generation call must not be silently
// swallowed into "0 questions parsed ⇒ SUCCESS". Before the fix,
// claudeRes.Err was never checked, so a Claude failure parsed to zero
// questions and the action returned 1 ("design clean").
func TestGrillDesignArtifactAction_ClaudeErrorFailsInsteadOfSilentSuccess(t *testing.T) {
	t.Chdir(t.TempDir())

	prevClaude := defaultSuperpowersClaudeRunner
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = prevClaude })
	defaultSuperpowersClaudeRunner = fakeGrillClaude{out: "", err: errors.New("claude: session limit reached")}

	bb, designPath := setupGrillRun(t, SuperpowersModeApply)

	act := GetAction("GrillDesignArtifact")
	if act == nil {
		t.Fatal("GrillDesignArtifact not registered")
	}
	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("result = %d, want -1 (FAILURE) when the Claude question-generation call errors", result)
	}
	if !strings.Contains(bb.Result, "session limit reached") {
		t.Fatalf("bb.Result = %q, want it to surface the Claude error", bb.Result)
	}
	data, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatalf("read design.md: %v", err)
	}
	if strings.Contains(string(data), "## Grill Q&A") {
		t.Fatalf("design.md must not gain a Grill Q&A section on a Claude error; got %q", string(data))
	}
}

// TestGrillDesignArtifactAction_ZeroQuestionsParsedFails proves finding 1's
// second half: even when Claude does not error, parsing zero grill questions
// out of its output is a protocol failure (the prompt demands up to 12
// Q-lines), not a clean design. Before the fix this silently returned 1.
func TestGrillDesignArtifactAction_ZeroQuestionsParsedFails(t *testing.T) {
	t.Chdir(t.TempDir())

	prevClaude := defaultSuperpowersClaudeRunner
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = prevClaude })
	defaultSuperpowersClaudeRunner = fakeGrillClaude{out: "I have no questions about this design, it looks great!"}

	bb, _ := setupGrillRun(t, SuperpowersModeApply)

	act := GetAction("GrillDesignArtifact")
	if act == nil {
		t.Fatal("GrillDesignArtifact not registered")
	}
	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != -1 {
		t.Fatalf("result = %d, want -1 (FAILURE) when zero grill questions are parsed from a non-error Claude run", result)
	}
	if bb.Outcome != "grill_no_questions_parsed" {
		t.Fatalf("bb.Outcome = %q, want %q", bb.Outcome, "grill_no_questions_parsed")
	}
}

// TestGrillDesignArtifactAction_DryRunReturnsOpenDryRunMarker (finding 5e)
// pins the dry-run skip path GrillDesignArtifact must keep untouched by the
// finding-1 error handling above: no Claude/NotebookLM calls, SUCCESS, and
// the OPEN-dry-run markers land in design.md.
func TestGrillDesignArtifactAction_DryRunReturnsOpenDryRunMarker(t *testing.T) {
	t.Chdir(t.TempDir())

	prevClaude := defaultSuperpowersClaudeRunner
	t.Cleanup(func() { defaultSuperpowersClaudeRunner = prevClaude })
	// A Claude runner that errors proves dry-run truly never calls it.
	defaultSuperpowersClaudeRunner = fakeGrillClaude{err: errors.New("must not be called in dry-run")}

	bb, designPath := setupGrillRun(t, SuperpowersModeDryRun)

	act := GetAction("GrillDesignArtifact")
	if act == nil {
		t.Fatal("GrillDesignArtifact not registered")
	}
	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != 1 {
		t.Fatalf("result = %d, want 1 (SUCCESS) for dry-run", result)
	}
	data, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatalf("read design.md: %v", err)
	}
	if !strings.Contains(string(data), "OPEN-dry-run") {
		t.Fatalf("design.md missing OPEN-dry-run marker; got %q", string(data))
	}
	if openCritical, ok := bb.ChainState["grill_open_critical"].(int); !ok || openCritical != 0 {
		t.Fatalf("ChainState[grill_open_critical] = %v (ok=%v), want 0", bb.ChainState["grill_open_critical"], ok)
	}
}

// TestGrillDesignArtifactAction_HappyPathBothAnswered (finding 5f) drives the
// full apply-mode path with a fake Claude that emits two Q-lines and a fake
// NotebookLM answerer that answers both, proving design.md ends up
// containing both answers and the action reports SUCCESS.
func TestGrillDesignArtifactAction_HappyPathBothAnswered(t *testing.T) {
	t.Chdir(t.TempDir())

	prevClaude := defaultSuperpowersClaudeRunner
	prevNLM := grillNotebookLMAnswerer
	t.Cleanup(func() {
		defaultSuperpowersClaudeRunner = prevClaude
		grillNotebookLMAnswerer = prevNLM
	})
	defaultSuperpowersClaudeRunner = fakeGrillClaude{out: "Q [critical] D1-persistence: does the cursor survive restarts?\n" +
		"Q [normal] D2-routing: why heuristics before LLM?\n"}
	grillNotebookLMAnswerer = func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
		out := map[int]string{}
		for i := range batch {
			out[i] = fmt.Sprintf("answer for %s", batch[i].Branch)
		}
		return out, nil
	}

	bb, designPath := setupGrillRun(t, SuperpowersModeApply)

	act := GetAction("GrillDesignArtifact")
	if act == nil {
		t.Fatal("GrillDesignArtifact not registered")
	}
	result := act(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if result != 1 {
		t.Fatalf("result = %d, want 1 (SUCCESS) when all questions are answered; bb.Result=%s", result, bb.Result)
	}
	data, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatalf("read design.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "answer for D1-persistence") || !strings.Contains(content, "answer for D2-routing") {
		t.Fatalf("design.md missing one or both answers; got %q", content)
	}
	if strings.Contains(content, "OPEN") {
		t.Fatalf("design.md should have no OPEN questions when both are answered; got %q", content)
	}
}

// TestResolveGrillQuestions_MultiBatchSevenQuestionsSplitsFiveAndTwo (finding
// 5d) proves the free-plan 50/day batching cap is honored end to end: 7
// questions must be answered via exactly two NotebookLM calls (5 then 2),
// and answers must map back to the correct original question indices.
func TestResolveGrillQuestions_MultiBatchSevenQuestionsSplitsFiveAndTwo(t *testing.T) {
	var qs []grillQuestion
	for i := 0; i < 7; i++ {
		qs = append(qs, grillQuestion{Branch: fmt.Sprintf("D%d", i), Text: fmt.Sprintf("question %d?", i)})
	}
	var batchSizes []int
	res := resolveGrillQuestions(context.Background(), qs, grillAnswerers{
		NotebookLM: func(_ context.Context, batch []grillQuestion) (map[int]string, error) {
			batchSizes = append(batchSizes, len(batch))
			out := map[int]string{}
			for i, q := range batch {
				out[i] = "answer:" + q.Branch
			}
			return out, nil
		},
	})
	if len(batchSizes) != 2 || batchSizes[0] != 5 || batchSizes[1] != 2 {
		t.Fatalf("batch sizes = %v, want [5 2]", batchSizes)
	}
	if res.OpenCritical != 0 {
		t.Fatalf("OpenCritical = %d, want 0", res.OpenCritical)
	}
	for i := 0; i < 7; i++ {
		want := "answer:D" + fmt.Sprint(i)
		if !strings.Contains(res.Markdown, want) {
			t.Fatalf("markdown missing %q for question index %d; got %q", want, i, res.Markdown)
		}
	}
}
