package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

type fakeGrillClaude struct{ out string }

func (f fakeGrillClaude) RunClaude(_ context.Context, _ string, _ string) CommandResult {
	return CommandResult{Output: f.out}
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
