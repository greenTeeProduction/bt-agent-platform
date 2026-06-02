package tracing

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestEndToEndOTLPExport(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e OTLP test in short mode")
	}

	// Start the bt-otlp-collector by POSTing a trace directly
	// and verifying the collector logs it.
	collectorURL := os.Getenv("BT_OTLP_ENDPOINT")
	if collectorURL == "" {
		collectorURL = "http://localhost:4318"
	}

	// Use the OTLPHTTPExporter to send a span to the real collector
	exporter := NewOTLPHTTPExporter(collectorURL, "e2e-test")

	// Send a test span
	span := ExportedSpan{
		TraceID:    "e2e-trace-1",
		SpanID:     "e2e-span-1",
		Name:       "e2e:validation_span",
		StartTime:  time.Now().Add(-time.Second),
		EndTime:    time.Now(),
		Duration:   time.Second,
		Attributes: map[string]string{"dimension": "observability", "test": "e2e-otlp"},
		Events:     []ExportedEvent{{Name: "validation_event", Time: time.Now(), Attributes: map[string]string{"step": "complete"}}},
	}

	err := exporter.ExportSpan(context.Background(), span)
	if err != nil {
		t.Fatalf("OTLP export failed: %v", err)
	}

	// Verify the collector received it
	resp, err := http.Get(collectorURL + "/api/otlp-stats")
	if err != nil {
		t.Fatalf("collector stats request failed: %v", err)
	}
	defer resp.Body.Close()

	var stats map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	t.Logf("collector stats: %+v", stats)

	spans := stats["spans_received"].(float64)
	if spans < 1 {
		t.Fatalf("expected at least 1 span, got %v", spans)
	}
	t.Logf("PASS: end-to-end OTLP trace export validated — collector received %v spans", spans)
}
