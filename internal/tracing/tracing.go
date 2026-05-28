// Package tracing provides a lightweight distributed tracing abstraction for the BT platform.
// It follows the OpenTelemetry API pattern with a console-based implementation that can be
// swapped for full OTel SDK in production. All spans are written as structured text lines.
package tracing

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
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

func (n noopSpan) End()                               {}
func (n noopSpan) AddEvent(name string, attrs ...Attr) {}
func (n noopSpan) SetAttribute(key, value string)      {}
func (n noopSpan) RecordError(err error)               {}
func (n noopSpan) SpanContext() SpanContext             { return SpanContext{} }
func (n noopSpan) IsRecording() bool                   { return false }

type noopTracer struct{}

func (n noopTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return ctx, noopSpan{}
}

// ─── Console Tracer ──────────────────────────────────────────────────────────

// ConsoleTracer writes span lifecycle events as text lines to an io.Writer.
// Format: "TRACE [timestamp] trace_id span_id operation DURATION [event] key=value..."
type ConsoleTracer struct {
	mu     sync.Mutex
	w      io.Writer
	seq    uint64
	prefix string // short prefix for the tracer source (e.g., "bt-agent")
}

// NewConsoleTracer creates a ConsoleTracer that writes to the given writer.
// If w is nil, os.Stderr is used.
func NewConsoleTracer(prefix string, w io.Writer) *ConsoleTracer {
	if w == nil {
		w = os.Stderr
	}
	return &ConsoleTracer{w: w, prefix: prefix, seq: 0}
}

func (t *ConsoleTracer) nextID() string {
	t.seq++
	return fmt.Sprintf("%s-%06d", t.prefix, t.seq)
}

func (t *ConsoleTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	// Get parent span context from ctx
	if ctx == nil {
		ctx = context.Background()
	}
	parent := SpanFromContext(ctx)
	parentID := ""
	traceID := t.nextID() + "-trace"
	if parent != nil {
		if pc := parent.SpanContext(); pc.TraceID != "" {
			traceID = pc.TraceID
		}
		if pc := parent.SpanContext(); pc.SpanID != "" {
			parentID = pc.SpanID
		}
	}

	span := &consoleSpan{
		tracer:    t,
		name:      name,
		startTime: time.Now(),
		spanCtx: SpanContext{
			TraceID: traceID,
			SpanID:  t.nextID(),
		},
		parentSpanID: parentID,
		attrs:        make(map[string]string),
	}
	ctx = context.WithValue(ctx, spanContextKey{}, span)
	return ctx, span
}

// ─── Console Span ────────────────────────────────────────────────────────────

type consoleSpan struct {
	mu           sync.Mutex
	tracer       *ConsoleTracer
	name         string
	startTime    time.Time
	spanCtx      SpanContext
	parentSpanID string
	attrs        map[string]string
	events       []spanEvent
	err          error
	ended        bool
}

type spanEvent struct {
	Time  time.Time
	Name  string
	Attrs []Attr
}

func (s *consoleSpan) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended = true
	duration := time.Since(s.startTime)

	// Build log line: TRACE ts trace_id span_id parent_id operation DURATION [events]
	s.tracer.mu.Lock()
	defer s.tracer.mu.Unlock()

	fmt.Fprintf(s.tracer.w, "TRACE %s %s %s",
		s.startTime.Format(time.RFC3339Nano),
		s.spanCtx.TraceID,
		s.spanCtx.SpanID,
	)
	if s.parentSpanID != "" {
		fmt.Fprintf(s.tracer.w, " parent=%s", s.parentSpanID)
	}
	fmt.Fprintf(s.tracer.w, " op=%s duration=%s", s.name, duration.Round(time.Microsecond))

	// Attributes
	for k, v := range s.attrs {
		fmt.Fprintf(s.tracer.w, " %s=%s", k, v)
	}

	// Events
	for _, ev := range s.events {
		fmt.Fprintf(s.tracer.w, " [%s", ev.Name)
		for _, a := range ev.Attrs {
			fmt.Fprintf(s.tracer.w, " %s=%s", a.Key, a.Value)
		}
		fmt.Fprint(s.tracer.w, "]")
	}

	// Error
	if s.err != nil {
		fmt.Fprintf(s.tracer.w, " error=%q", s.err.Error())
	}

	fmt.Fprintln(s.tracer.w)
}

func (s *consoleSpan) AddEvent(name string, attrs ...Attr) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.events = append(s.events, spanEvent{Time: time.Now(), Name: name, Attrs: attrs})
}

func (s *consoleSpan) SetAttribute(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.attrs[key] = value
}

func (s *consoleSpan) RecordError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.err = err
}

func (s *consoleSpan) SpanContext() SpanContext { return s.spanCtx }

func (s *consoleSpan) IsRecording() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.ended
}

// ─── Context Propagation ─────────────────────────────────────────────────────

type spanContextKey struct{}

// SpanFromContext extracts a Span from context, or nil if none exists.
func SpanFromContext(ctx context.Context) Span {
	if s, ok := ctx.Value(spanContextKey{}).(Span); ok {
		return s
	}
	return nil
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

// GetGlobalTracer returns the current global tracer.
func GetGlobalTracer() Tracer {
	globalTracerMu.RLock()
	defer globalTracerMu.RUnlock()
	return globalTracer
}

// StartSpan creates a span using the global tracer. Returns the new context
// containing the span, and the span itself.
func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return GetGlobalTracer().StartSpan(ctx, name)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// StringAttr creates a string-valued attribute.
func StringAttr(key, value string) Attr { return Attr{Key: key, Value: value} }

// IntAttr creates an integer-valued attribute (formatted as string).
func IntAttr(key string, value int) Attr { return Attr{Key: key, Value: fmt.Sprintf("%d", value)} }

// BoolAttr creates a boolean-valued attribute (formatted as string).
func BoolAttr(key string, value bool) Attr { return Attr{Key: key, Value: fmt.Sprintf("%t", value)} }

// DurationAttr creates a duration-valued attribute.
func DurationAttr(key string, d time.Duration) Attr {
	return Attr{Key: key, Value: d.String()}
}

// ─── Testing Helpers ─────────────────────────────────────────────────────────

// TestTracer creates a ConsoleTracer that writes to an in-memory buffer.
// Returns the tracer and a function to get captured output.
func TestTracer(prefix string) (*ConsoleTracer, func() string) {
	var buf syncBuffer
	t := NewConsoleTracer(prefix, &buf)
	return t, buf.String
}

type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *syncBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
