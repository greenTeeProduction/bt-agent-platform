package tracing

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newRecordingTracer() (*tracetest.SpanRecorder, Tracer) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	return rec, NewOTelTracer(tp.Tracer("test"))
}

func TestOTelSpan_LifecycleAndAttributes(t *testing.T) {
	rec, tr := newRecordingTracer()
	ctx, span := tr.StartSpan(context.Background(), "op")
	if !span.IsRecording() {
		t.Fatal("span should be recording")
	}
	span.SetAttribute("k", "v")
	span.AddEvent("evt", Attr{Key: "a", Value: "b"})
	span.RecordError(errors.New("boom"))
	sc := span.SpanContext()
	if len(sc.TraceID) != 32 || len(sc.SpanID) != 16 {
		t.Fatalf("unexpected ids: %+v", sc)
	}
	// Child span nests under parent via ctx.
	_, child := tr.StartSpan(ctx, "child")
	if child.SpanContext().TraceID != sc.TraceID {
		t.Fatal("child must share parent trace id")
	}
	child.End()
	span.End()
	if got := len(rec.Ended()); got != 2 {
		t.Fatalf("ended spans = %d, want 2", got)
	}
}

func TestSpanContextFrom(t *testing.T) {
	_, tr := newRecordingTracer()
	ctx, span := tr.StartSpan(context.Background(), "op")
	defer span.End()
	sc, ok := SpanContextFrom(ctx)
	if !ok || sc.TraceID != span.SpanContext().TraceID {
		t.Fatalf("SpanContextFrom = %+v ok=%v", sc, ok)
	}
	if _, ok := SpanContextFrom(context.Background()); ok {
		t.Fatal("empty ctx must return ok=false")
	}
}

func TestInitFromEnv_NoEndpointIsNoop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("BT_OTLP_ENDPOINT", "")
	shutdown := InitFromEnv("test-svc")
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("noop shutdown: %v", err)
	}
}

func TestContextWithTraceParentHeader(t *testing.T) {
	_, tr := newRecordingTracer()
	ctx := ContextWithTraceParentHeader(context.Background(),
		"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	_, span := tr.StartSpan(ctx, "child")
	defer span.End()
	if span.SpanContext().TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("trace id not propagated: %+v", span.SpanContext())
	}
}
