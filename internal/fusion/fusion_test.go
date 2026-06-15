package fusion

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeCaller struct {
	mu      sync.Mutex
	calls   []string
	delay   time.Duration
	answers map[string]string
	errs    map[string]error
}

func (f *fakeCaller) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, model+":"+prompt)
	f.mu.Unlock()
	if err := f.errs[model]; err != nil {
		return "", err
	}
	if ans, ok := f.answers[model]; ok {
		return ans, nil
	}
	return "answer from " + model, nil
}

func TestRunPanel_RunsModelsConcurrently(t *testing.T) {
	caller := &fakeCaller{delay: 100 * time.Millisecond, answers: map[string]string{}, errs: map[string]error{}}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a", "b", "c"}
	start := time.Now()
	responses, err := RunPanel(context.Background(), caller, cfg, "prompt", nil)
	if err != nil {
		t.Fatalf("RunPanel error: %v", err)
	}
	if len(responses) != 3 {
		t.Fatalf("responses=%d, want 3", len(responses))
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("panel did not run concurrently, elapsed=%s", elapsed)
	}
}

func TestRunPanel_PreservesPerModelErrors(t *testing.T) {
	caller := &fakeCaller{answers: map[string]string{"a": "ok"}, errs: map[string]error{"b": errors.New("boom")}}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a", "b"}
	responses, err := RunPanel(context.Background(), caller, cfg, "prompt", nil)
	if err != nil {
		t.Fatalf("partial failure should not fail panel: %v", err)
	}
	if responses[1].Error == "" {
		t.Fatalf("expected per-model error, got %#v", responses[1])
	}
}

func TestRunPanel_FailsWhenAllModelsFail(t *testing.T) {
	caller := &fakeCaller{errs: map[string]error{"a": errors.New("a"), "b": errors.New("b")}}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a", "b"}
	if _, err := RunPanel(context.Background(), caller, cfg, "prompt", nil); err == nil {
		t.Fatal("expected all-panel-failure error")
	}
}

func TestJudge_ParsesStructuredJSON(t *testing.T) {
	caller := &fakeCaller{answers: map[string]string{"judge": `{"consensus":["agree"],"contradictions":[{"topic":"x","stances":[{"model":"a","stance":"yes"}]}],"partial_coverage":[{"models":["a"],"point":"only a"}],"unique_insights":[{"model":"b","insight":"novel"}],"blind_spots":["missing"]}`}}
	cfg := DefaultConfig()
	cfg.JudgeModel = "judge"
	analysis, err := Judge(context.Background(), caller, cfg, "prompt", []Response{{Model: "a", Content: "A"}})
	if err != nil {
		t.Fatalf("Judge error: %v", err)
	}
	if len(analysis.Consensus) != 1 || len(analysis.Contradictions) != 1 || len(analysis.BlindSpots) != 1 {
		t.Fatalf("analysis missing fields: %#v", analysis)
	}
}

func TestJudge_RejectsInvalidJSON(t *testing.T) {
	caller := &fakeCaller{answers: map[string]string{"judge": `not json`}}
	cfg := DefaultConfig()
	cfg.JudgeModel = "judge"
	if _, err := Judge(context.Background(), caller, cfg, "prompt", []Response{{Model: "a", Content: "A"}}); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestToolLoop_StopsAtMaxToolCalls(t *testing.T) {
	caller := &fakeCaller{answers: map[string]string{"m": "Thought: search\nAction: web_search\nAction Input: x"}}
	tools := []Tool{fakeTool{name: "web_search", out: "result"}}
	cfg := DefaultConfig()
	cfg.MaxToolCalls = 2
	answer, calls, err := RunToolLoop(context.Background(), caller, "m", "sys", "prompt", tools, cfg)
	if err != nil {
		t.Fatalf("tool loop error: %v", err)
	}
	if calls != 2 || !strings.Contains(answer, "result") {
		t.Fatalf("calls=%d answer=%q", calls, answer)
	}
}

func TestSynthesize_UsesAnalysisAndResponses(t *testing.T) {
	caller := &fakeCaller{answers: map[string]string{"judge": "final with consensus and contradiction"}}
	cfg := DefaultConfig()
	cfg.JudgeModel = "judge"
	final, err := Synthesize(context.Background(), caller, cfg, "prompt", Result{Analysis: Analysis{Consensus: []string{"c"}, BlindSpots: []string{"b"}}, Responses: []Response{{Model: "a", Content: "A"}}})
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	if !strings.Contains(final, "consensus") {
		t.Fatalf("unexpected final: %q", final)
	}
}

func TestRun_EndToEnd(t *testing.T) {
	caller := &fakeCaller{answers: map[string]string{
		"a":     "panel A",
		"b":     "panel B",
		"judge": `{"consensus":["shared"],"contradictions":[],"partial_coverage":[],"unique_insights":[],"blind_spots":[]}`,
	}}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a", "b"}
	cfg.JudgeModel = "judge"
	result, err := Run(context.Background(), caller, cfg, "prompt", nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Status != "ok" || len(result.Responses) != 2 || result.Final == "" {
		t.Fatalf("bad result: %#v", result)
	}
}

type fakeTool struct{ name, out string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "fake" }
func (f fakeTool) Call(string) string  { return f.out }
