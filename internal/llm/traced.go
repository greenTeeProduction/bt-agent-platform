package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// TracedLLM decorates an LLM with spans per Generate* call. Kept separate
// from ErrorRecorder — one concern each; RunOnce stacks them.
type TracedLLM struct {
	LLM
	provider string
	parent   func() context.Context
}

// NewTracedLLM wraps client; provider labels the span (e.g. "fallback-chain").
func NewTracedLLM(client LLM, provider string) *TracedLLM {
	return &TracedLLM{LLM: client, provider: provider}
}

// NewTracedLLMWithParent wraps client like NewTracedLLM, but sources the span
// parent from parent() for the context-less LLM interface methods (Generate,
// GenerateWithTimeout) instead of context.Background(). Those methods have no
// ctx parameter to carry the caller's active span, so without this, their
// spans always start a new trace root and never nest under the BT node span
// that invoked them. Callers that track an active trace context elsewhere
// (e.g. engine.Blackboard.TraceContext, updated per node) can supply a
// closure that reads it at call time, letting LLM spans nest under the
// current node span. GenerateCtx is unaffected — it already takes an
// explicit ctx and uses that as its parent.
func NewTracedLLMWithParent(client LLM, provider string, parent func() context.Context) *TracedLLM {
	return &TracedLLM{LLM: client, provider: provider, parent: parent}
}

// parentCtx resolves the parent context for context-less span starts: the
// result of t.parent() when set and non-nil, else context.Background().
func (t *TracedLLM) parentCtx() context.Context {
	if t.parent != nil {
		if ctx := t.parent(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

func (t *TracedLLM) span(ctx context.Context) (context.Context, tracing.Span) {
	spanCtx, span := tracing.StartSpan(ctx, "llm.generate/"+t.provider)
	span.SetAttribute("llm.provider", t.provider)
	return spanCtx, span
}

func (t *TracedLLM) finish(span tracing.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetAttribute("llm.error_class", reliability.ClassifyError(err).String())
		if ra := reliability.RetryAfterFromError(err); ra > 0 {
			span.SetAttribute("llm.retry_after", ra.String())
		}
	}
	span.End()
}

func (t *TracedLLM) Generate(prompt string) (string, error) {
	_, span := t.span(t.parentCtx())
	result, err := t.LLM.Generate(prompt)
	t.finish(span, err)
	return result, err
}

func (t *TracedLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	spanCtx, span := t.span(ctx)
	result, err := t.LLM.GenerateCtx(spanCtx, prompt)
	t.finish(span, err)
	return result, err
}

func (t *TracedLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	_, span := t.span(t.parentCtx())
	span.SetAttribute("llm.timeout", timeout.String())
	result, err := t.LLM.GenerateWithTimeout(prompt, timeout)
	t.finish(span, err)
	return result, err
}

// GenerateWithMaxTokens forwards to the wrapped LLM's GenerateWithMaxTokens
// when it supports capping output tokens, else falls back to its unbounded
// Generate — see generateWithMaxTokens (provider.go).
func (t *TracedLLM) GenerateWithMaxTokens(prompt string, maxTokens int) (string, error) {
	_, span := t.span(t.parentCtx())
	if maxTokens > 0 {
		span.SetAttribute("llm.max_tokens", fmt.Sprintf("%d", maxTokens))
	}
	result, err := generateWithMaxTokens(t.LLM, prompt, maxTokens)
	t.finish(span, err)
	return result, err
}

// AnalyzeComplexity, GeneratePlan, and Reflect are intentionally untraced
// pass-throughs. They are convenience methods that, on every concrete LLM
// implementation in this package (e.g. *ollama.Client, *DeepSeekClient,
// *OpenAICompatClient), call an internal, unexported generate helper
// directly rather than routing through the exported, traced Generate method.
// So delegating to t.LLM here reaches the underlying client's own
// implementation, whose LLM work never passes back through this wrapper and
// is therefore invisible to it — no span is emitted for the generation these
// methods perform. Tracing them properly would require adding ctx (or a
// parent-context hook) to these LLM interface methods too; left as
// deliberate debt rather than widening the interface for this fix.
func (t *TracedLLM) AnalyzeComplexity(task string) string {
	return t.LLM.AnalyzeComplexity(task)
}

func (t *TracedLLM) GeneratePlan(task, complexity string) string {
	return t.LLM.GeneratePlan(task, complexity)
}

func (t *TracedLLM) Reflect(task, outcome, plan string) (wentWell string, toImprove string) {
	return t.LLM.Reflect(task, outcome, plan)
}

var _ LLM = (*TracedLLM)(nil)
