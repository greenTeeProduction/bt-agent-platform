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

func (n noopSpan) End()                                {}
func (n noopSpan) AddEvent(name string, attrs ...Attr) {}
func (n noopSpan) SetAttribute(key, value string)      {}
func (n noopSpan) RecordError(err error)               {}
func (n noopSpan) SpanContext() SpanContext            { return SpanContext{} }
func (n noopSpan) IsRecording() bool                   { return false }

type noopTracer struct{}

func (n noopTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return ctx, noopSpan{}
}

// ─── Sampler ─────────────────────────────────────────────────────────────────

// Sampler decides whether a span should be recorded.
type Sampler interface {
	// ShouldSample returns true if the span with the given trace ID and operation name
	// should be recorded. Return false to drop the span (creates a noopSpan).
	ShouldSample(traceID, spanName string) bool
}

// alwaysSampler samples every span.
type alwaysSampler struct{}

func (alwaysSampler) ShouldSample(traceID, spanName string) bool { return true }

// neverSampler drops every span.
type neverSampler struct{}

func (neverSampler) ShouldSample(traceID, spanName string) bool { return false }

// AlwaysSample returns a Sampler that records every span.
func AlwaysSample() Sampler { return alwaysSampler{} }

// NeverSample returns a Sampler that drops every span.
func NeverSample() Sampler { return neverSampler{} }

// RatioSampler samples spans at the given ratio (0.0 to 1.0).
// Uses the trace ID for deterministic sampling — all spans within the same trace
// get the same sample decision. Ratio 1.0 means always sample, 0.0 means never.
type RatioSampler struct {
	ratio float64
}

// NewRatioSampler creates a RatioSampler that samples approximately ratio fraction
// of traces. Ratio is clamped to [0.0, 1.0].
func NewRatioSampler(ratio float64) *RatioSampler {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return &RatioSampler{ratio: ratio}
}

// Ratio returns the configured sampling ratio.
func (rs *RatioSampler) Ratio() float64 { return rs.ratio }

// ShouldSample deterministically decides whether to sample based on the trace ID.
// Uses the first 8 hex characters of the trace ID as a hash (0x00000000-0xFFFFFFFF).
// This ensures all spans in the same trace get the same decision.
func (rs *RatioSampler) ShouldSample(traceID, spanName string) bool {
	if rs.ratio >= 1.0 {
		return true
	}
	if rs.ratio <= 0.0 {
		return false
	}
	// Use first 8 hex chars of trace ID as a deterministic hash
	hash := hashTraceID(traceID)
	threshold := uint32(float64(0xFFFFFFFF) * rs.ratio)
	return hash <= threshold
}

// hashTraceID extracts a deterministic uint32 hash from the trace ID string.
// Uses the first 8 hex characters; falls back to 0 if the trace ID is too short
// or not hex-formatted.
func hashTraceID(traceID string) uint32 {
	if len(traceID) < 8 {
		return 0
	}
	var h uint32
	for i := 0; i < 8; i++ {
		c := traceID[i]
		h <<= 4
		switch {
		case c >= '0' && c <= '9':
			h |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			h |= uint32(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			h |= uint32(c - 'A' + 10)
		default:
			h |= uint32(i) // non-hex char: use position-based fallback
		}
	}
	return h
}

// ─── Console Tracer ──────────────────────────────────────────────────────────

// ConsoleTracer writes span lifecycle events as text lines to an io.Writer.
// Format: "TRACE [timestamp] trace_id span_id operation DURATION [event] key=value..."
type ConsoleTracer struct {
	mu            sync.Mutex
	w             io.Writer
	seq           uint64
	prefix        string       // short prefix for the tracer source (e.g., "bt-agent")
	sampler       Sampler      // nil means always sample
	exporter      SpanExporter // optional backend exporter (OTLP, custom collector, etc.)
	exportTimeout time.Duration
}

// NewConsoleTracer creates a ConsoleTracer that writes to the given writer.
// If w is nil, os.Stderr is used. All spans are sampled by default.
func NewConsoleTracer(prefix string, w io.Writer) *ConsoleTracer {
	if w == nil {
		w = os.Stderr
	}
	return &ConsoleTracer{w: w, prefix: prefix, seq: 0, exportTimeout: defaultExportTimeout}
}

// SetSampler configures the sampling strategy for this tracer.
// Set to nil to sample everything (default). Set to NeverSample() to disable tracing.
// Set to NewRatioSampler(0.1) to sample 10% of traces.
func (t *ConsoleTracer) SetSampler(s Sampler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sampler = s
}

// Sampler returns the current sampling strategy.
func (t *ConsoleTracer) Sampler() Sampler {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sampler
}

// SetExporter configures an optional backend exporter that receives each span
// after it is written to the local trace log. Set nil to disable backend export.
func (t *ConsoleTracer) SetExporter(exporter SpanExporter) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.exporter = exporter
}

// Exporter returns the currently configured backend exporter, if any.
func (t *ConsoleTracer) Exporter() SpanExporter {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exporter
}

// SetExportTimeout changes the per-span export timeout. Non-positive durations
// reset to the default timeout.
func (t *ConsoleTracer) SetExportTimeout(timeout time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if timeout <= 0 {
		t.exportTimeout = defaultExportTimeout
		return
	}
	t.exportTimeout = timeout
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

	// Check sampler — if the sample is dropped, return a noopSpan.
	// New traces (no parent) are subject to sampling. Child spans always inherit
	// the parent's trace (already sampled), so we only sample at trace root.
	if parent == nil {
		t.mu.Lock()
		s := t.sampler
		t.mu.Unlock()
		if s != nil && !s.ShouldSample(traceID, name) {
			// Dropped: return a noopSpan. Still propagate a dropped sentinel
			// so children can detect the trace was dropped.
			return ctx, noopSpan{}
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
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	endTime := time.Now()
	duration := endTime.Sub(s.startTime)
	exported := s.exportedSpanLocked(endTime, duration)
	s.mu.Unlock()

	// Build log line: TRACE ts trace_id span_id parent_id operation DURATION [events]
	s.tracer.mu.Lock()
	exporter := s.tracer.exporter
	exportTimeout := s.tracer.exportTimeout

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
	s.tracer.mu.Unlock()

	if exporter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), exportTimeout)
		defer cancel()
		_ = exporter.ExportSpan(ctx, exported)
	}
}

func (s *consoleSpan) exportedSpanLocked(endTime time.Time, duration time.Duration) ExportedSpan {
	attrs := make(map[string]string, len(s.attrs))
	for k, v := range s.attrs {
		attrs[k] = v
	}
	events := make([]ExportedEvent, 0, len(s.events))
	for _, ev := range s.events {
		evAttrs := make(map[string]string, len(ev.Attrs))
		for _, attr := range ev.Attrs {
			evAttrs[attr.Key] = attr.Value
		}
		events = append(events, ExportedEvent{Name: ev.Name, Time: ev.Time, Attributes: evAttrs})
	}
	errMsg := ""
	if s.err != nil {
		errMsg = s.err.Error()
	}
	return ExportedSpan{
		ServiceName:  s.tracer.prefix,
		TraceID:      s.spanCtx.TraceID,
		SpanID:       s.spanCtx.SpanID,
		ParentSpanID: s.parentSpanID,
		Name:         s.name,
		StartTime:    s.startTime,
		EndTime:      endTime,
		Duration:     duration,
		Attributes:   attrs,
		Events:       events,
		Error:        errMsg,
	}
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
// containing the span, and the span itself. If no global tracer is set
// (nil), falls back to a noopSpan gracefully.
func StartSpan(ctx context.Context, name string) (context.Context, Span) {
	t := GetGlobalTracer()
	if t == nil {
		return ctx, noopSpan{}
	}
	return t.StartSpan(ctx, name)
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
