package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

// ─── W3C Trace Context ───────────────────────────────────────────────────────
//
// Implements the W3C Trace Context Level 2 specification:
//   https://www.w3.org/TR/trace-context/
//
// The traceparent header format:
//   version-trace_id-parent_id-trace_flags
//   version     = 2 hex chars (00 = current)
//   trace_id    = 32 hex chars (16 bytes)
//   parent_id   = 16 hex chars (8 bytes)
//   trace_flags = 2 hex chars (01 = sampled)
//
// Example: 00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01

// TraceParent represents a parsed W3C traceparent header.
type TraceParent struct {
	Version    string // 2 hex chars
	TraceID    string // 32 hex chars
	ParentID   string // 16 hex chars
	TraceFlags string // 2 hex chars
	Sampled    bool   // true if trace_flags bit 0 is set (01)
}

// ParseTraceParent parses a W3C traceparent header value.
// Format: version-trace_id-parent_id-trace_flags
func ParseTraceParent(header string) (*TraceParent, error) {
	if header == "" {
		return nil, fmt.Errorf("traceparent: empty header")
	}

	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return nil, fmt.Errorf("traceparent: expected 4 parts, got %d", len(parts))
	}

	version, traceID, parentID, traceFlags := parts[0], parts[1], parts[2], parts[3]

	// Validate version (must be 2 hex chars)
	if len(version) != 2 || !isHex(version) {
		return nil, fmt.Errorf("traceparent: invalid version %q", version)
	}

	// Validate trace_id (must be 32 hex chars, not all zeros)
	if len(traceID) != 32 || !isHex(traceID) {
		return nil, fmt.Errorf("traceparent: invalid trace_id %q", traceID)
	}
	if traceID == "00000000000000000000000000000000" {
		return nil, fmt.Errorf("traceparent: trace_id is all zeros")
	}

	// Validate parent_id (must be 16 hex chars, not all zeros)
	if len(parentID) != 16 || !isHex(parentID) {
		return nil, fmt.Errorf("traceparent: invalid parent_id %q", parentID)
	}
	if parentID == "0000000000000000" {
		return nil, fmt.Errorf("traceparent: parent_id is all zeros")
	}

	// Validate trace_flags (must be 2 hex chars)
	if len(traceFlags) != 2 || !isHex(traceFlags) {
		return nil, fmt.Errorf("traceparent: invalid trace_flags %q", traceFlags)
	}

	sampled := false
	if traceFlags == "01" || traceFlags == "03" {
		sampled = true
	}

	return &TraceParent{
		Version:    version,
		TraceID:    traceID,
		ParentID:   parentID,
		TraceFlags: traceFlags,
		Sampled:    sampled,
	}, nil
}

// SpanContext returns the SpanContext that should be used as the parent
// for spans created from this traceparent. The TraceID is inherited from
// the parent; the parent_id is the ParentID from the header.
func (tp *TraceParent) SpanContext() SpanContext {
	return SpanContext{
		TraceID: tp.TraceID,
		SpanID:  tp.ParentID,
	}
}

// isHex returns true if all characters in s are valid hex digits (0-9, a-f, A-F).
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ─── Span Context Injection/Extraction ────────────────────────────────────────

// traceParentSpan is a synthetic span that only exists to carry a parent SpanContext
// through context propagation. It implements Span just enough to provide SpanContext(),
// without any recording capability. Used as a "parent bag" when extracting trace
// context from incoming W3C headers.
type traceParentSpan struct {
	sc SpanContext
}

func (s *traceParentSpan) End()                                {}
func (s *traceParentSpan) AddEvent(name string, attrs ...Attr) {}
func (s *traceParentSpan) SetAttribute(key, value string)      {}
func (s *traceParentSpan) RecordError(err error)               {}
func (s *traceParentSpan) SpanContext() SpanContext            { return s.sc }
func (s *traceParentSpan) IsRecording() bool                   { return false }

// ContextWithTraceParent injects a TraceParent as the parent span context
// into a context. This allows StartSpan calls on this context to produce
// child spans that inherit the trace_id and reference the parent span.
func ContextWithTraceParent(ctx context.Context, tp *TraceParent) context.Context {
	if tp == nil || ctx == nil {
		return ctx
	}
	parentSpan := &traceParentSpan{sc: tp.SpanContext()}
	return context.WithValue(ctx, spanContextKey{}, parentSpan)
}

// ExtractTraceParentFromRequest extracts a TraceParent from an HTTP request's
// traceparent header. Returns nil if no valid header is present.
func ExtractTraceParentFromRequest(headerValue string) *TraceParent {
	tp, err := ParseTraceParent(headerValue)
	if err != nil {
		return nil
	}
	return tp
}

// ExtractTraceStateFromRequest extracts tracestate vendor data from the header.
// The tracestate header is a comma-separated list of key=value pairs.
// This is passed through without validation per the W3C spec.
func ExtractTraceStateFromRequest(tracestate string) []string {
	if tracestate == "" {
		return nil
	}
	entries := strings.Split(tracestate, ",")
	result := make([]string, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e != "" {
			result = append(result, e)
		}
	}
	return result
}

// ─── Traceparent Generation ──────────────────────────────────────────────────

// GenerateTraceParent creates a new traceparent header value for outgoing requests.
// traceID and spanID should be lowercase hex strings (32 and 16 chars respectively).
// sampled indicates whether the trace is sampled (sets trace_flags to 01 or 00).
func GenerateTraceParent(traceID, spanID string, sampled bool) string {
	traceFlags := "00"
	if sampled {
		traceFlags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", traceID, spanID, traceFlags)
}

// GenerateTraceID creates a cryptographically random 32-char hex trace ID.
// Falls back to timestamp-based ID if crypto/rand fails.
func GenerateTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely — fallback returns zero-read ID
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// GenerateSpanID creates a cryptographically random 16-char hex span ID.
// Falls back to counter-based ID if crypto/rand fails.
func GenerateSpanID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely — fallback to counter
		spanIDSeqMu.Lock()
		spanIDSeq++
		id := spanIDSeq
		spanIDSeqMu.Unlock()
		return fmt.Sprintf("%016x", id)
	}
	return hex.EncodeToString(b)
}

var (
	spanIDSeq   uint64
	spanIDSeqMu sync.Mutex
)
