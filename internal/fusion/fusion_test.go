package fusion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

type fakeCaller struct {
	mu      sync.Mutex
	calls   []string
	delay   time.Duration
	answers map[string]string
	errs    map[string]error
	panics  map[string]bool
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
	if f.panics[model] {
		panic("intentional test panic for model " + model)
	}
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

func TestRunPanel_RecoversFromModelPanic(t *testing.T) {
	caller := &fakeCaller{
		answers: map[string]string{"a": "ok"},
		errs:    map[string]error{},
		panics:  map[string]bool{"b": true},
	}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a", "b"}

	responses, err := RunPanel(context.Background(), caller, cfg, "prompt", nil)
	if err != nil {
		t.Fatalf("RunPanel should not fail entirely when only one model panics: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("responses=%d, want 2", len(responses))
	}
	if responses[0].Error != "" {
		t.Fatalf("expected model a to succeed, got %#v", responses[0])
	}
	if responses[1].Error == "" {
		t.Fatalf("expected panicking model b to record a response error instead of crashing, got %#v", responses[1])
	}
	if !strings.Contains(responses[1].Error, "panic") {
		t.Fatalf("expected recorded error to mention panic, got %q", responses[1].Error)
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

// flakyCaller fails the first failN calls per model with err, then succeeds
// with result. Used to simulate a transient post-panel failure that should
// be absorbed by a retry instead of discarding an otherwise-successful run.
type flakyCaller struct {
	mu     sync.Mutex
	calls  map[string]int
	failN  int
	err    error
	result string
}

func (f *flakyCaller) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error) {
	f.mu.Lock()
	f.calls[model]++
	n := f.calls[model]
	f.mu.Unlock()
	if n <= f.failN {
		return "", f.err
	}
	return f.result, nil
}

func (f *flakyCaller) callCount(model string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[model]
}

func TestJudge_RetriesTransientFailure(t *testing.T) {
	caller := &flakyCaller{
		calls:  map[string]int{},
		failN:  1,
		err:    errors.New("transient upstream error"),
		result: `{"consensus":["agree"],"contradictions":[],"partial_coverage":[],"unique_insights":[],"blind_spots":[]}`,
	}
	cfg := DefaultConfig()
	cfg.JudgeModel = "judge"
	analysis, err := Judge(context.Background(), caller, cfg, "prompt", []Response{{Model: "a", Content: "A"}})
	if err != nil {
		t.Fatalf("Judge should retry a transient failure and succeed instead of discarding the panel, got err: %v", err)
	}
	if len(analysis.Consensus) != 1 {
		t.Fatalf("unexpected analysis after retry: %#v", analysis)
	}
	if calls := caller.callCount("judge"); calls < 2 {
		t.Fatalf("expected Judge to retry the LLM call at least once, got %d call(s)", calls)
	}
}

func TestSynthesize_RetriesTransientFailure(t *testing.T) {
	caller := &flakyCaller{
		calls:  map[string]int{},
		failN:  1,
		err:    errors.New("transient upstream error"),
		result: "final synthesized answer",
	}
	cfg := DefaultConfig()
	cfg.JudgeModel = "judge"
	result := Result{Analysis: Analysis{Consensus: []string{"c"}}, Responses: []Response{{Model: "a", Content: "A"}}}
	final, err := Synthesize(context.Background(), caller, cfg, "prompt", result)
	if err != nil {
		t.Fatalf("Synthesize should retry a transient failure and succeed instead of discarding the panel, got err: %v", err)
	}
	if final != "final synthesized answer" {
		t.Fatalf("unexpected final answer after retry: %q", final)
	}
	if calls := caller.callCount("judge"); calls < 2 {
		t.Fatalf("expected Synthesize to retry the LLM call at least once, got %d call(s)", calls)
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

func TestRun_RespectsTimeout(t *testing.T) {
	caller := &fakeCaller{delay: 300 * time.Millisecond, answers: map[string]string{}, errs: map[string]error{}}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a"}
	cfg.JudgeModel = "judge"
	cfg.Timeout = 50 * time.Millisecond

	start := time.Now()
	result, err := Run(context.Background(), caller, cfg, "prompt", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected Run to fail once cfg.Timeout elapses")
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Run did not respect cfg.Timeout=%s, took %s (caller delay=%s)", cfg.Timeout, elapsed, caller.delay)
	}
	if len(result.Responses) != 1 || !strings.Contains(result.Responses[0].Error, "context deadline exceeded") {
		t.Fatalf("expected panel response to fail with context deadline exceeded, got %#v", result.Responses)
	}
}

// stageDeadlineCaller records the ctx deadline remaining at the moment Judge
// and Synthesize issue their LLM call, and stalls the panel-stage call long
// enough to nearly exhaust the shared cfg.Timeout budget. It distinguishes
// stages by system-prompt content rather than by model name, since Judge and
// Synthesize both use cfg.JudgeModel.
type stageDeadlineCaller struct {
	mu         sync.Mutex
	panelDelay time.Duration
	sawJudge   bool
	sawSynth   bool
	judgeRem   time.Duration
	synthRem   time.Duration
}

func remainingDeadline(ctx context.Context) time.Duration {
	dl, ok := ctx.Deadline()
	if !ok {
		return time.Hour
	}
	return time.Until(dl)
}

func (c *stageDeadlineCaller) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error) {
	switch {
	case strings.Contains(system, "independent Fusion panel model"):
		select {
		case <-time.After(c.panelDelay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "panel response", nil
	case strings.Contains(system, "Fusion judge"):
		rem := remainingDeadline(ctx)
		c.mu.Lock()
		c.sawJudge, c.judgeRem = true, rem
		c.mu.Unlock()
		return `{"consensus":["agree"],"contradictions":[],"partial_coverage":[],"unique_insights":[],"blind_spots":[]}`, nil
	case strings.Contains(system, "synthesize final answers"):
		rem := remainingDeadline(ctx)
		c.mu.Lock()
		c.sawSynth, c.synthRem = true, rem
		c.mu.Unlock()
		return "final synthesized answer", nil
	}
	return "", fmt.Errorf("stageDeadlineCaller: unexpected system prompt %q", system)
}

// TestRun_JudgeAndSynthesizeGetOwnTimeoutBudget verifies that a slow RunPanel
// stage cannot starve Judge/Synthesize's retry policies. Judge and Synthesize
// must derive their timeout from the original caller ctx (a fresh budget),
// not from the already-shrinking panel ctx that RunPanel consumed most of.
func TestRun_JudgeAndSynthesizeGetOwnTimeoutBudget(t *testing.T) {
	withFreshFusionBreaker(t, 3, time.Minute)

	caller := &stageDeadlineCaller{panelDelay: 400 * time.Millisecond}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a"}
	cfg.JudgeModel = "judge"
	cfg.Timeout = 500 * time.Millisecond

	result, err := Run(context.Background(), caller, cfg, "prompt", nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Status != "ok" {
		t.Fatalf("expected ok status, got %#v", result)
	}

	caller.mu.Lock()
	defer caller.mu.Unlock()
	if !caller.sawJudge {
		t.Fatal("Judge was never called")
	}
	if !caller.sawSynth {
		t.Fatal("Synthesize was never called")
	}

	// The panel stage consumed 400ms of the 500ms shared cfg.Timeout, leaving
	// only ~100ms on that context. Judge and Synthesize must instead observe a
	// fresh budget derived from the original ctx (~cfg.Timeout), not the ~100ms
	// remainder of the panel's already-shrunk context.
	const minFreshBudget = 250 * time.Millisecond
	if caller.judgeRem < minFreshBudget {
		t.Errorf("Judge got a starved context: %s remaining, want a fresh budget close to cfg.Timeout=%s derived from the original ctx, not the shrunk panel ctx", caller.judgeRem, cfg.Timeout)
	}
	if caller.synthRem < minFreshBudget {
		t.Errorf("Synthesize got a starved context: %s remaining, want a fresh budget close to cfg.Timeout=%s derived from the original ctx, not the shrunk panel ctx", caller.synthRem, cfg.Timeout)
	}
}

// =============================================================================
// Circuit breaker (short-circuit repeated all-panel failures instead of
// dispatching a doomed model panel on every call)
// =============================================================================

// withFreshFusionBreaker swaps the package-level fusion circuit breaker for a
// freshly closed one for the duration of the test, restoring the prior
// breaker afterward so this test cannot leak open/tripped state into other
// tests in the package that expect a working panel.
func withFreshFusionBreaker(t *testing.T, threshold int, cooldown time.Duration) {
	t.Helper()
	orig := fusionBreaker
	t.Cleanup(func() { fusionBreaker = orig })
	fusionBreaker = reliability.NewCircuitBreaker("fusion-panel", threshold, cooldown)
}

// TestRunPanel_CircuitBreakerOpensAfterConsecutiveFailures verifies that
// after enough consecutive all-models-failed panel runs, the breaker opens
// and short-circuits further RunPanel calls instead of dispatching the panel
// (and hitting a dead model backend) on every single call.
func TestRunPanel_CircuitBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	withFreshFusionBreaker(t, 3, time.Minute)

	caller := &fakeCaller{errs: map[string]error{"a": errors.New("boom")}}
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a"}

	const attempts = 6
	for i := 0; i < attempts; i++ {
		if _, err := RunPanel(context.Background(), caller, cfg, "prompt", nil); err == nil {
			t.Fatalf("attempt %d: expected RunPanel to fail while all models fail", i)
		}
	}

	if state := fusionBreaker.State(); state != reliability.CircuitOpen {
		t.Fatalf("expected fusion circuit breaker to be open after %d consecutive failed panel runs, got state %v", attempts, state)
	}

	caller.mu.Lock()
	calls := len(caller.calls)
	caller.mu.Unlock()
	if calls >= attempts {
		t.Errorf("expected the circuit breaker to short-circuit some RunPanel calls instead of dispatching the panel on every call; got %d model calls for %d attempts", calls, attempts)
	}

	if _, err := RunPanel(context.Background(), caller, cfg, "prompt", nil); err == nil || !strings.Contains(err.Error(), "circuit breaker") {
		t.Fatalf("expected an open-breaker RunPanel call to fail mentioning the circuit breaker, got %v", err)
	}
}

type fakeTool struct{ name, out string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "fake" }
func (f fakeTool) Call(string) string  { return f.out }
