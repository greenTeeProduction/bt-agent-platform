package tracing

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestEndToEndOTLPExport verifies spans reach a real OTLP collector (Tempo
// from monitoring/docker-compose.yml). Skips when nothing listens on :4318.
func TestEndToEndOTLPExport(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "localhost:4318", 500*time.Millisecond)
	if err != nil {
		t.Skip("no OTLP collector on localhost:4318 — run `make observability-up`")
	}
	_ = conn.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	shutdown := InitFromEnv("bt-e2e-test")
	_, span := StartSpan(context.Background(), "e2e-test-span")
	span.SetAttribute("test", "true")
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("export/shutdown failed: %v", err)
	}
	SetGlobalTracer(noopTracer{})
}
