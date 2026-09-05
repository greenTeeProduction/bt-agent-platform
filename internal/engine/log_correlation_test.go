package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

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

func TestLogExportTarget(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		wantHost     string
		wantPath     string
		wantInsecure bool
		wantOK       bool
	}{
		{name: "http with path", endpoint: "http://localhost:3100/otlp", wantHost: "localhost:3100", wantPath: "/otlp/v1/logs", wantInsecure: true, wantOK: true},
		{name: "http with trailing slash", endpoint: "http://localhost:3100/otlp/", wantHost: "localhost:3100", wantPath: "/otlp/v1/logs", wantInsecure: true, wantOK: true},
		{name: "scheme-less host:port", endpoint: "localhost:3100", wantHost: "localhost:3100", wantPath: "", wantInsecure: true, wantOK: true},
		{name: "https with path", endpoint: "https://loki.example/otlp", wantHost: "loki.example", wantPath: "/otlp/v1/logs", wantInsecure: false, wantOK: true},
		{name: "empty is inactive", endpoint: "", wantOK: false},
		{name: "missing scheme name is inactive", endpoint: "://bad", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, urlPath, insecure, ok := logExportTarget(tt.endpoint)
			if ok != tt.wantOK {
				t.Fatalf("logExportTarget(%q) ok = %v, want %v", tt.endpoint, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if host != tt.wantHost || urlPath != tt.wantPath || insecure != tt.wantInsecure {
				t.Fatalf("logExportTarget(%q) = (host=%q, path=%q, insecure=%v), want (host=%q, path=%q, insecure=%v)",
					tt.endpoint, host, urlPath, insecure, tt.wantHost, tt.wantPath, tt.wantInsecure)
			}
		})
	}
}

func TestInitLogExport_Idempotent(t *testing.T) {
	t.Setenv("BT_OTLP_LOGS_ENDPOINT", "http://localhost:3100/otlp")

	mu.Lock()
	before := len(extraHandlers)
	mu.Unlock()

	shutdown1 := InitLogExport("test-svc")
	shutdown2 := InitLogExport("test-svc")

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = shutdown1(ctx)
		_ = shutdown2(ctx)
		// Restore global logger state for the rest of the package tests.
		mu.Lock()
		extraHandlers = extraHandlers[:before]
		buildLogger()
		mu.Unlock()
		logExportMu.Lock()
		logExportInitialized = false
		logExportMu.Unlock()
	})

	mu.Lock()
	after := len(extraHandlers)
	mu.Unlock()
	if after != before+1 {
		t.Fatalf("extraHandlers grew by %d after two InitLogExport calls, want exactly 1", after-before)
	}
}
