package tracing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultExportTimeout = 2 * time.Second

// SpanExporter receives completed spans from ConsoleTracer. Implementations may
// forward spans to an OpenTelemetry collector, persist them, or fan them out to
// another telemetry backend.
type SpanExporter interface {
	ExportSpan(ctx context.Context, span ExportedSpan) error
}

// ExportedSpan is a backend-neutral representation of a completed span.
type ExportedSpan struct {
	ServiceName  string            `json:"service_name"`
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Duration     time.Duration     `json:"duration"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	Events       []ExportedEvent   `json:"events,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// ExportedEvent is a timestamped event attached to an exported span.
type ExportedEvent struct {
	Name       string            `json:"name"`
	Time       time.Time         `json:"time"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// OTLPHTTPExporter sends completed spans to an OpenTelemetry Collector using the
// OTLP/HTTP JSON endpoint, typically http://collector:4318/v1/traces.
type OTLPHTTPExporter struct {
	Endpoint    string
	ServiceName string
	Headers     map[string]string
	Client      *http.Client
}

// NewOTLPHTTPExporter creates an OTLP/HTTP JSON exporter. If endpoint omits the
// /v1/traces path it is appended automatically.
func NewOTLPHTTPExporter(endpoint, serviceName string) *OTLPHTTPExporter {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimRight(endpoint, "/")
	if endpoint != "" && !strings.HasSuffix(endpoint, "/v1/traces") {
		endpoint += "/v1/traces"
	}
	if serviceName == "" {
		serviceName = "bt-platform"
	}
	return &OTLPHTTPExporter{
		Endpoint:    endpoint,
		ServiceName: serviceName,
		Client:      &http.Client{Timeout: defaultExportTimeout},
	}
}

// ExportSpan posts one completed span as an OTLP JSON trace batch.
func (e *OTLPHTTPExporter) ExportSpan(ctx context.Context, span ExportedSpan) error {
	if e == nil || e.Endpoint == "" {
		return nil
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: defaultExportTimeout}
	}
	service := e.ServiceName
	if span.ServiceName != "" {
		service = span.ServiceName
	}
	if service == "" {
		service = "bt-platform"
	}

	payload := otlpTracePayload(span, service)
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("otlp export failed: status %d", resp.StatusCode)
	}
	return nil
}

// ConfigureOTLPFromEnv attaches a batched OTLP exporter to t when
// BT_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT is set.
// Spans are batched (64 per batch or every 5s) before being sent to the
// OTLP collector, reducing per-span HTTP overhead.
// It returns true when an exporter was wired.
func ConfigureOTLPFromEnv(t *ConsoleTracer) bool {
	if t == nil {
		return false
	}
	endpoint := firstNonEmpty(os.Getenv("BT_OTLP_ENDPOINT"), os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"), os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		return false
	}
	service := firstNonEmpty(os.Getenv("OTEL_SERVICE_NAME"), os.Getenv("BT_SERVICE_NAME"), t.prefix)
	exporter := NewOTLPHTTPExporter(endpoint, service)
	if headers := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS"); headers != "" {
		exporter.Headers = parseOTLPHeaders(headers)
	}
	// Wrap in BatchExporter so spans are buffered and flushed in batches
	// instead of one HTTP request per span.
	t.SetExporter(NewBatchExporter(exporter))
	return true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseOTLPHeaders(raw string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) != "" {
			out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return out
}

func stableHexID(value string, bytesLen int) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:bytesLen])
}

func otlpTracePayload(span ExportedSpan, service string) map[string]any {
	attrs := []map[string]any{{"key": "service.name", "value": map[string]string{"stringValue": service}}}
	spanAttrs := make([]map[string]any, 0, len(span.Attributes)+1)
	for k, v := range span.Attributes {
		spanAttrs = append(spanAttrs, map[string]any{"key": k, "value": map[string]string{"stringValue": v}})
	}
	if span.Error != "" {
		spanAttrs = append(spanAttrs, map[string]any{"key": "error.message", "value": map[string]string{"stringValue": span.Error}})
	}
	events := make([]map[string]any, 0, len(span.Events))
	for _, ev := range span.Events {
		evAttrs := make([]map[string]any, 0, len(ev.Attributes))
		for k, v := range ev.Attributes {
			evAttrs = append(evAttrs, map[string]any{"key": k, "value": map[string]string{"stringValue": v}})
		}
		events = append(events, map[string]any{
			"name":         ev.Name,
			"timeUnixNano": fmt.Sprintf("%d", ev.Time.UnixNano()),
			"attributes":   evAttrs,
		})
	}
	otlpSpan := map[string]any{
		"traceId":           stableHexID(span.TraceID, 16),
		"spanId":            stableHexID(span.SpanID, 8),
		"name":              span.Name,
		"kind":              1,
		"startTimeUnixNano": fmt.Sprintf("%d", span.StartTime.UnixNano()),
		"endTimeUnixNano":   fmt.Sprintf("%d", span.EndTime.UnixNano()),
		"attributes":        spanAttrs,
		"events":            events,
	}
	if span.ParentSpanID != "" {
		otlpSpan["parentSpanId"] = stableHexID(span.ParentSpanID, 8)
	}
	if span.Error != "" {
		otlpSpan["status"] = map[string]any{"code": 2, "message": span.Error}
	}
	return map[string]any{
		"resourceSpans": []map[string]any{{
			"resource": map[string]any{"attributes": attrs},
			"scopeSpans": []map[string]any{{
				"scope": map[string]string{"name": "go-bt-evolve/internal/tracing"},
				"spans": []map[string]any{otlpSpan},
			}},
		}},
	}
}
