package engine

import (
	"context"
	"strings"
	"testing"
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
