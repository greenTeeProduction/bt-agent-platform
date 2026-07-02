package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracedLLM_EmitsSpanWithErrorClass(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	traced := NewTracedLLM(&stubLLM{name: "s", err: errors.New("rate limit exceeded")}, "stub")
	if _, err := traced.Generate("p"); err == nil {
		t.Fatal("expected error")
	}
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "llm.generate/stub" {
		t.Fatalf("spans = %v", spans)
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs["llm.error_class"] != "rate_limited" {
		t.Fatalf("error_class = %q, want rate_limited", attrs["llm.error_class"])
	}
}

func TestTracedLLM_SuccessSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	traced := NewTracedLLM(&stubLLM{name: "ok"}, "stub")
	if _, err := traced.Generate("p"); err != nil {
		t.Fatal(err)
	}
	if len(rec.Ended()) != 1 {
		t.Fatalf("want 1 span, got %d", len(rec.Ended()))
	}
}

// TestTracedLLM_NestsUnderProvidedParent guards the fix for LLM spans not
// nesting under the active BT node span: NewTracedLLMWithParent lets
// context-less calls (Generate) resolve their parent from a caller-supplied
// closure (standing in for engine.Blackboard.TraceContext) instead of always
// rooting at context.Background().
func TestTracedLLM_NestsUnderProvidedParent(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	parentCtx, parentSpan := tracing.StartSpan(context.Background(), "bt.node/test")

	traced := NewTracedLLMWithParent(&stubLLM{name: "ok"}, "stub", func() context.Context {
		return parentCtx
	})
	if _, err := traced.Generate("p"); err != nil {
		t.Fatal(err)
	}
	parentSpan.End()

	spans := rec.Ended()
	if len(spans) != 2 {
		t.Fatalf("want 2 spans (llm.generate + bt.node), got %d", len(spans))
	}

	var llmSpan, wantParent sdktrace.ReadOnlySpan
	for _, s := range spans {
		switch s.Name() {
		case "llm.generate/stub":
			llmSpan = s
		case "bt.node/test":
			wantParent = s
		}
	}
	if llmSpan == nil || wantParent == nil {
		t.Fatalf("missing expected spans: %v", spans)
	}
	if llmSpan.Parent().SpanID() != wantParent.SpanContext().SpanID() {
		t.Fatalf("llm span parent = %s, want %s", llmSpan.Parent().SpanID(), wantParent.SpanContext().SpanID())
	}
}
