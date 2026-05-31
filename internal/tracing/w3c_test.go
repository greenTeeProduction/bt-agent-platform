package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── ParseTraceParent Tests ──────────────────────────────────────────────────

func TestParseTraceParent_Valid(t *testing.T) {
	header := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	tp, err := ParseTraceParent(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp.Version != "00" {
		t.Errorf("expected version=00, got %s", tp.Version)
	}
	if tp.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("unexpected trace_id: %s", tp.TraceID)
	}
	if tp.ParentID != "b7ad6b7169203331" {
		t.Errorf("unexpected parent_id: %s", tp.ParentID)
	}
	if tp.TraceFlags != "01" {
		t.Errorf("expected trace_flags=01, got %s", tp.TraceFlags)
	}
	if !tp.Sampled {
		t.Error("expected Sampled=true for flags=01")
	}
}

func TestParseTraceParent_NotSampled(t *testing.T) {
	header := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-00"
	tp, err := ParseTraceParent(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp.Sampled {
		t.Error("expected Sampled=false for flags=00")
	}
}

func TestParseTraceParent_SampledWithFlags03(t *testing.T) {
	// trace_flags=03 means sampled (bit 0 set) + a vendor flag (bit 1 set)
	header := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-03"
	tp, err := ParseTraceParent(header)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tp.Sampled {
		t.Error("expected Sampled=true for flags=03 (bit 0 set)")
	}
}

func TestParseTraceParent_Empty(t *testing.T) {
	_, err := ParseTraceParent("")
	if err == nil {
		t.Fatal("expected error for empty header")
	}
}

func TestParseTraceParent_InvalidFormat(t *testing.T) {
	invalidCases := []struct {
		name   string
		header string
	}{
		{"too few parts", "00-abc-def"},
		{"too many parts", "00-abc-def-ghi-jkl"},
		{"no dashes", "00abcdef1234567890abcdef1234567890abcdef123456789001"},
		{"invalid version chars", "zz-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		{"version too short", "0-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		{"version too long", "000-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		{"trace_id too short", "00-abc-b7ad6b7169203331-01"},
		{"trace_id too long", "00-0af7651916cd43dd8448eb211c80319c000-b7ad6b7169203331-01"},
		{"trace_id all zeros", "00-00000000000000000000000000000000-b7ad6b7169203331-01"},
		{"trace_id invalid chars", "00-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-b7ad6b7169203331-01"},
		{"parent_id too short", "00-0af7651916cd43dd8448eb211c80319c-abc-01"},
		{"parent_id too long", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331000-01"},
		{"parent_id all zeros", "00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01"},
		{"flags too short", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-0"},
		{"flags invalid chars", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-zz"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseTraceParent(tc.header)
			if err == nil {
				t.Errorf("expected error for %q", tc.header)
			}
		})
	}
}

func TestParseTraceParent_UppercaseHex(t *testing.T) {
	// W3C spec allows both upper and lowercase hex
	header := "00-0AF7651916CD43DD8448EB211C80319C-B7AD6B7169203331-01"
	tp, err := ParseTraceParent(header)
	if err != nil {
		t.Fatalf("unexpected error for uppercase hex: %v", err)
	}
	if tp.TraceID != "0AF7651916CD43DD8448EB211C80319C" {
		t.Errorf("expected trace_id preserved: %s", tp.TraceID)
	}
}

func TestParseTraceParent_MixedCase(t *testing.T) {
	header := "00-0aF7651916cD43dd8448EB211c80319c-b7AD6b7169203331-01"
	tp, err := ParseTraceParent(header)
	if err != nil {
		t.Fatalf("unexpected error for mixed case: %v", err)
	}
	if !tp.Sampled {
		t.Error("expected Sampled=true")
	}
}

// ─── TraceParent.SpanContext Tests ───────────────────────────────────────────

func TestTraceParent_SpanContext(t *testing.T) {
	tp := &TraceParent{
		Version:  "00",
		TraceID:  "0af7651916cd43dd8448eb211c80319c",
		ParentID: "b7ad6b7169203331",
	}
	sc := tp.SpanContext()
	if sc.TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Errorf("expected TraceID=%q, got %q", tp.TraceID, sc.TraceID)
	}
	if sc.SpanID != "b7ad6b7169203331" {
		t.Errorf("expected SpanID=%q, got %q", tp.ParentID, sc.SpanID)
	}
}

// ─── ContextWithTraceParent Tests ────────────────────────────────────────────

func TestContextWithTraceParent_Nil(t *testing.T) {
	ctx := context.Background()
	result := ContextWithTraceParent(ctx, nil)
	if result != ctx {
		t.Error("expected unchanged context for nil TraceParent")
	}
}

func TestContextWithTraceParent_NilContext(t *testing.T) {
	tp := &TraceParent{
		TraceID:  "0af7651916cd43dd8448eb211c80319c",
		ParentID: "b7ad6b7169203331",
	}
	result := ContextWithTraceParent(nil, tp)
	if result != nil {
		t.Error("expected nil when context is nil")
	}
}

func TestContextWithTraceParent_Integration(t *testing.T) {
	// Full flow: parse traceparent → inject into context → StartSpan inherits trace_id
	tracer, output := TestTracer("w3c-test")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	header := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	tp, err := ParseTraceParent(header)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	ctx := ContextWithTraceParent(context.Background(), tp)
	ctx, span := StartSpan(ctx, "child-operation")
	if span == nil {
		t.Fatal("expected non-nil span")
	}

	sc := span.SpanContext()
	if sc.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("expected child span to inherit trace_id from parent, got %q", sc.TraceID)
	}
	if sc.SpanID == "00f067aa0ba902b7" {
		t.Error("expected child span to have a DIFFERENT span_id from parent")
	}
	span.End()

	out := output()
	if !strings.Contains(out, "parent=00f067aa0ba902b7") {
		t.Errorf("expected parent=00f067aa0ba902b7 in output: %s", out)
	}
	if !strings.Contains(out, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("expected trace_id in output: %s", out)
	}
	if !strings.Contains(out, "op=child-operation") {
		t.Errorf("expected op=child-operation in output: %s", out)
	}
}

func TestContextWithTraceParent_WithoutParent(t *testing.T) {
	// No parent in context → StartSpan generates a new trace_id
	tracer, output := TestTracer("no-parent")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	ctx := context.Background()
	ctx, span := StartSpan(ctx, "root-operation")
	span.End()

	out := output()
	if strings.Contains(out, "parent=") {
		t.Errorf("expected no parent= in output (root span): %s", out)
	}
}

// ─── ExtractTraceStateFromRequest Tests ──────────────────────────────────────

func TestExtractTraceStateFromRequest_Empty(t *testing.T) {
	result := ExtractTraceStateFromRequest("")
	if result != nil {
		t.Errorf("expected nil for empty tracestate, got %v", result)
	}
}

func TestExtractTraceStateFromRequest_Single(t *testing.T) {
	result := ExtractTraceStateFromRequest("vendor=value")
	if len(result) != 1 || result[0] != "vendor=value" {
		t.Errorf("expected [vendor=value], got %v", result)
	}
}

func TestExtractTraceStateFromRequest_Multiple(t *testing.T) {
	result := ExtractTraceStateFromRequest("vendor1=value1, vendor2=value2")
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(result), result)
	}
	if result[0] != "vendor1=value1" || result[1] != "vendor2=value2" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestExtractTraceStateFromRequest_Spaces(t *testing.T) {
	result := ExtractTraceStateFromRequest(" a=b , c=d , e=f ")
	if len(result) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(result), result)
	}
	if result[0] != "a=b" {
		t.Errorf("expected a=b, got %q", result[0])
	}
}

func TestExtractTraceStateFromRequest_EmptyEntries(t *testing.T) {
	result := ExtractTraceStateFromRequest("a=b,,,c=d")
	if len(result) != 2 {
		t.Fatalf("expected 2 entries (empty stripped), got %d: %v", len(result), result)
	}
}

// ─── Generate Functions Tests ────────────────────────────────────────────────

func TestGenerateTraceParent(t *testing.T) {
	result := GenerateTraceParent("0af7651916cd43dd8448eb211c80319c", "b7ad6b7169203331", true)
	expected := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestGenerateTraceParent_NotSampled(t *testing.T) {
	result := GenerateTraceParent("0af7651916cd43dd8448eb211c80319c", "b7ad6b7169203331", false)
	if !strings.HasSuffix(result, "-00") {
		t.Errorf("expected trace_flags=00 for unsampled, got %q", result)
	}
}

func TestGenerateTraceID(t *testing.T) {
	id1 := GenerateTraceID()
	id2 := GenerateTraceID()

	if id1 == id2 {
		t.Error("expected unique trace IDs")
	}
	if len(id1) != 32 {
		t.Errorf("expected 32-char trace ID, got %d: %q", len(id1), id1)
	}
	if !isHex(id1) {
		t.Errorf("expected hex trace ID, got %q", id1)
	}
}

func TestGenerateSpanID(t *testing.T) {
	id1 := GenerateSpanID()
	id2 := GenerateSpanID()

	if id1 == id2 {
		t.Error("expected unique span IDs")
	}
	if len(id1) != 16 {
		t.Errorf("expected 16-char span ID, got %d: %q", len(id1), id1)
	}
	if !isHex(id1) {
		t.Errorf("expected hex span ID, got %q", id1)
	}
}

// ─── TracingMiddleware W3C Tests ─────────────────────────────────────────────

func TestTracingMiddleware_WithTraceParent(t *testing.T) {
	tracer, output := TestTracer("http-w3c")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	var capturedSpan Span
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedSpan = SpanFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if capturedSpan == nil {
		t.Fatal("expected span in request context")
	}

	sc := capturedSpan.SpanContext()
	if sc.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("expected inherited trace_id, got %q", sc.TraceID)
	}

	out := output()
	if !strings.Contains(out, "parent=00f067aa0ba902b7") {
		t.Errorf("expected parent=00f067aa0ba902b7 in output: %s", out)
	}
	if !strings.Contains(out, "w3c.traceparent=00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01") {
		t.Errorf("expected w3c.traceparent attribute in output: %s", out)
	}
}

func TestTracingMiddleware_WithTraceState(t *testing.T) {
	tracer, output := TestTracer("http-w3c-ts")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("POST", "/api/submit", nil)
	req.Header.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaabbbbbbbbbbbbbbbb-01")
	req.Header.Set("tracestate", "vendor1=opaqueValue1, vendor2=opaqueValue2")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	out := output()
	if !strings.Contains(out, "w3c.tracestate=vendor1=opaqueValue1, vendor2=opaqueValue2") {
		t.Errorf("expected w3c.tracestate attribute in output: %s", out)
	}
}

func TestTracingMiddleware_InvalidTraceParent(t *testing.T) {
	// Invalid traceparent should not break the middleware — just fall back to new trace
	tracer, output := TestTracer("http-w3c-invalid")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/health", nil)
	req.Header.Set("traceparent", "invalid-garbage")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	out := output()
	// Should still produce a trace, just not as child of the invalid parent
	if !strings.Contains(out, "TRACE") {
		t.Errorf("expected TRACE output even with invalid traceparent: %s", out)
	}
	// Should NOT contain a " parent=" (console tracer format: " parent=spanID")
	// Note: w3c.traceparent= with raw header is recorded, but that's an attribute, not a span parent.
	if strings.Contains(out, " parent=") {
		t.Errorf("expected no span parent= for invalid traceparent: %s", out)
	}
}

func TestTracingMiddleware_NoTraceParent(t *testing.T) {
	// Without traceparent header, span should be a root span
	tracer, output := TestTracer("http-no-w3c")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/summary", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	out := output()
	if strings.Contains(out, "parent=") {
		t.Errorf("expected no parent= without traceparent header: %s", out)
	}
	if strings.Contains(out, "w3c.traceparent=") {
		t.Errorf("expected no w3c.traceparent without header: %s", out)
	}
}

func TestTracingMiddleware_TraceParentWithTracestate(t *testing.T) {
	tracer, output := TestTracer("http-w3c-full")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the context has the injected parent
		span := SpanFromContext(r.Context())
		if span == nil {
			t.Error("expected span in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("traceparent", "00-abcdef1234567890abcdef1234567890-1234567890abcdef-01")
	req.Header.Set("tracestate", "dd=test-value")
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	out := output()
	if !strings.Contains(out, "w3c.traceparent=") {
		t.Errorf("expected w3c.traceparent attribute: %s", out)
	}
	if !strings.Contains(out, "w3c.tracestate=dd=test-value") {
		t.Errorf("expected w3c.tracestate attribute: %s", out)
	}
	if !strings.Contains(out, "parent=1234567890abcdef") {
		t.Errorf("expected parent=1234567890abcdef: %s", out)
	}
}

// ─── TraceParentSpan Interface Compliance ────────────────────────────────────

func TestTraceParentSpan_Interface(t *testing.T) {
	var s Span = &traceParentSpan{sc: SpanContext{TraceID: "abc", SpanID: "def"}}

	if s.IsRecording() {
		t.Error("traceParentSpan should never be recording")
	}

	sc := s.SpanContext()
	if sc.TraceID != "abc" || sc.SpanID != "def" {
		t.Errorf("unexpected SpanContext: %+v", sc)
	}

	// These should be no-ops (no panics)
	s.End()
	s.AddEvent("test")
	s.SetAttribute("key", "value")
	s.RecordError(nil)
}
