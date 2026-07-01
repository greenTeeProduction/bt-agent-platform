// Package tracing is a thin facade over the OpenTelemetry SDK for the BT
// platform. Engine code imports only this package; the SDK is wired in
// otel.go and activated by InitFromEnv when an OTLP endpoint is configured.
package tracing

import (
	"context"
	"sync"
)

// ─── SpanContext ─────────────────────────────────────────────────────────────

// SpanContext carries trace identifiers across process boundaries.
type SpanContext struct {
	TraceID string `json:"trace_id"`
	SpanID  string `json:"span_id"`
}

// ─── Span Interface ──────────────────────────────────────────────────────────

// Span represents a single operation within a trace.
type Span interface {
	// End completes the span. After End(), the span is immutable.
	End()

	// AddEvent records a timestamped event within the span.
	AddEvent(name string, attrs ...Attr)

	// SetAttribute sets a key-value attribute on the span.
	SetAttribute(key, value string)

	// RecordError records an error that occurred during the span.
	RecordError(err error)

	// SpanContext returns the span's identifying context.
	SpanContext() SpanContext

	// IsRecording returns true if the span is still active (not ended).
	IsRecording() bool
}

// Attr is a key-value pair for span events and attributes.
type Attr struct {
	Key   string
	Value string
}

// ─── Tracer Interface ────────────────────────────────────────────────────────

// Tracer creates spans for named operations.
type Tracer interface {
	// StartSpan creates and starts a new span as a child of any span in ctx.
	// Returns a new context containing the span, and the span itself.
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// ─── Noop Implementations ────────────────────────────────────────────────────

type noopSpan struct{}

func (n noopSpan) End()                         {}
func (n noopSpan) AddEvent(_ string, _ ...Attr) {}
func (n noopSpan) SetAttribute(_, _ string)     {}
func (n noopSpan) RecordError(_ error)          {}
func (n noopSpan) SpanContext() SpanContext     { return SpanContext{} }
func (n noopSpan) IsRecording() bool            { return false }

type noopTracer struct{}

func (n noopTracer) StartSpan(ctx context.Context, _ string) (context.Context, Span) {
	return ctx, noopSpan{}
}

// ─── Global Tracer ───────────────────────────────────────────────────────────

var (
	globalTracer   Tracer = noopTracer{}
	globalTracerMu sync.RWMutex
)

// SetGlobalTracer sets the global tracer used by StartSpan and convenience functions.
func SetGlobalTracer(t Tracer) {
	globalTracerMu.Lock()
	defer globalTracerMu.Unlock()
	globalTracer = t
}

// GlobalTracer returns the currently installed global tracer.
func GlobalTracer() Tracer {
	globalTracerMu.RLock()
	defer globalTracerMu.RUnlock()
	return globalTracer
}

// StartSpan creates a span using the global tracer. Returns the new context
// containing the span, and the span itself. If no global tracer is set
// (nil), falls back to a noopSpan gracefully.
func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	t := GlobalTracer()
	if t == nil {
		return ctx, noopSpan{}
	}
	return t.StartSpan(ctx, name)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// StringAttr creates a string-valued attribute.
func StringAttr(key, value string) Attr { return Attr{Key: key, Value: value} }
