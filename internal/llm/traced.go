package llm

import (
	"context"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// TracedLLM decorates an LLM with spans per Generate* call. Kept separate
// from ErrorRecorder — one concern each; RunOnce stacks them.
type TracedLLM struct {
	LLM
	provider string
}

// NewTracedLLM wraps client; provider labels the span (e.g. "fallback-chain").
func NewTracedLLM(client LLM, provider string) *TracedLLM {
	return &TracedLLM{LLM: client, provider: provider}
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
	_, span := t.span(context.Background())
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
	_, span := t.span(context.Background())
	span.SetAttribute("llm.timeout", timeout.String())
	result, err := t.LLM.GenerateWithTimeout(prompt, timeout)
	t.finish(span, err)
	return result, err
}

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
