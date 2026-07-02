package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceContextHandler_InjectsTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTraceContextHandler(slog.NewJSONHandler(&buf, nil)))

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	tr := tracing.NewOTelTracer(tp.Tracer("test"))
	ctx, span := tr.StartSpan(context.Background(), "op")
	defer span.End()

	logger.InfoContext(ctx, "with span")
	logger.Info("without span")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var first, second map[string]any
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatal(err)
	}
	if first["trace_id"] != span.SpanContext().TraceID {
		t.Fatalf("trace_id = %v, want %s", first["trace_id"], span.SpanContext().TraceID)
	}
	if first["span_id"] == "" || first["span_id"] == nil {
		t.Fatal("span_id missing")
	}
	if _, ok := second["trace_id"]; ok {
		t.Fatal("record without span ctx must not carry trace_id")
	}
}
