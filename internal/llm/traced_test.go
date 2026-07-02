package llm

import (
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
