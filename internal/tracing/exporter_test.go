package tracing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type captureExporter struct {
	mu    sync.Mutex
	spans []ExportedSpan
	err   error
}

func (e *captureExporter) ExportSpan(ctx context.Context, span ExportedSpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, span)
	return e.err
}

func (e *captureExporter) last() ExportedSpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.spans) == 0 {
		return ExportedSpan{}
	}
	return e.spans[len(e.spans)-1]
}

func TestConsoleTracer_ExporterReceivesCompletedSpan(t *testing.T) {
	tracer, output := TestTracer("svc")
	exporter := &captureExporter{}
	tracer.SetExporter(exporter)

	ctx, span := tracer.StartSpan(context.Background(), "operation")
	span.SetAttribute("component", "test")
	span.AddEvent("checkpoint", StringAttr("step", "one"))
	span.RecordError(errors.New("boom"))
	span.End()

	if !strings.Contains(output(), "TRACE") {
		t.Fatalf("expected local trace log to still be written, got %q", output())
	}
	exported := exporter.last()
	if exported.ServiceName != "svc" || exported.Name != "operation" {
		t.Fatalf("unexpected exported span identity: %+v", exported)
	}
	if exported.Attributes["component"] != "test" {
		t.Fatalf("missing exported attributes: %+v", exported.Attributes)
	}
	if len(exported.Events) != 1 || exported.Events[0].Name != "checkpoint" || exported.Events[0].Attributes["step"] != "one" {
		t.Fatalf("missing exported event: %+v", exported.Events)
	}
	if exported.Error != "boom" {
		t.Fatalf("expected exported error, got %q", exported.Error)
	}
	if SpanFromContext(ctx) == nil {
		t.Fatal("expected span propagated in context")
	}
}

func TestConsoleTracer_ExporterErrorDoesNotBreakEnd(t *testing.T) {
	tracer, output := TestTracer("svc")
	tracer.SetExporter(&captureExporter{err: errors.New("collector down")})
	_, span := tracer.StartSpan(context.Background(), "operation")
	span.End()
	if !strings.Contains(output(), "op=operation") {
		t.Fatalf("exporter error should not suppress local trace log: %q", output())
	}
}

func TestOTLPHTTPExporter_PostsOTLPJSON(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/traces" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type %q", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("X-Test") != "yes" {
			t.Fatalf("missing custom header")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	exporter := NewOTLPHTTPExporter(server.URL, "test-service")
	exporter.Headers = map[string]string{"X-Test": "yes"}
	err := exporter.ExportSpan(context.Background(), ExportedSpan{
		TraceID:    "trace-a",
		SpanID:     "span-a",
		Name:       "operation",
		StartTime:  time.Unix(10, 0),
		EndTime:    time.Unix(11, 0),
		Attributes: map[string]string{"http.method": "GET"},
		Events:     []ExportedEvent{{Name: "event", Time: time.Unix(10, 5), Attributes: map[string]string{"k": "v"}}},
	})
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}

	resourceSpans := got["resourceSpans"].([]any)
	resource := resourceSpans[0].(map[string]any)["resource"].(map[string]any)
	resourceAttrs := resource["attributes"].([]any)
	if resourceAttrs[0].(map[string]any)["key"] != "service.name" {
		t.Fatalf("missing service.name resource attr: %+v", got)
	}
	scopeSpans := resourceSpans[0].(map[string]any)["scopeSpans"].([]any)
	spans := scopeSpans[0].(map[string]any)["spans"].([]any)
	span := spans[0].(map[string]any)
	if span["name"] != "operation" {
		t.Fatalf("unexpected OTLP span: %+v", span)
	}
	if len(span["traceId"].(string)) != 32 || len(span["spanId"].(string)) != 16 {
		t.Fatalf("expected OTLP hex IDs, got trace=%q span=%q", span["traceId"], span["spanId"])
	}
}

func TestOTLPHTTPExporter_Non2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	exporter := NewOTLPHTTPExporter(server.URL+"/v1/traces", "svc")
	if err := exporter.ExportSpan(context.Background(), ExportedSpan{Name: "op"}); err == nil {
		t.Fatal("expected non-2xx export error")
	}
}

func TestConfigureOTLPFromEnv(t *testing.T) {
	t.Setenv("BT_OTLP_ENDPOINT", "http://collector:4318")
	t.Setenv("OTEL_SERVICE_NAME", "env-service")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Bearer token,X-Test=yes")
	tracer, _ := TestTracer("fallback")
	if !ConfigureOTLPFromEnv(tracer) {
		t.Fatal("expected OTLP exporter from env")
	}
	exporter, ok := tracer.Exporter().(*OTLPHTTPExporter)
	if !ok {
		t.Fatalf("unexpected exporter type %T", tracer.Exporter())
	}
	if exporter.Endpoint != "http://collector:4318/v1/traces" || exporter.ServiceName != "env-service" {
		t.Fatalf("unexpected exporter config: %+v", exporter)
	}
	if exporter.Headers["Authorization"] != "Bearer token" || exporter.Headers["X-Test"] != "yes" {
		t.Fatalf("unexpected headers: %+v", exporter.Headers)
	}
}

func TestConfigureOTLPFromEnv_DisabledWithoutEndpoint(t *testing.T) {
	for _, key := range []string{"BT_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_ENDPOINT"} {
		_ = os.Unsetenv(key)
	}
	tracer, _ := TestTracer("svc")
	if ConfigureOTLPFromEnv(tracer) {
		t.Fatal("did not expect OTLP exporter without endpoint")
	}
	if tracer.Exporter() != nil {
		t.Fatalf("expected nil exporter, got %T", tracer.Exporter())
	}
}
