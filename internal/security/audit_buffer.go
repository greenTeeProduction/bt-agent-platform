package security

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ─── Security Audit Buffer ─────────────────────────────────────────────

// AuditEvent is a single captured security event with structured metadata.
type AuditEvent struct {
	Event     string            `json:"event"`
	Timestamp time.Time         `json:"timestamp"`
	Attrs     map[string]string `json:"attrs,omitempty"`
}

// AuditBuffer is a thread-safe ring buffer that keeps the N most recent
// security audit events in memory. It captures events emitted by
// AuditSecurityEvent so operators can inspect recent security activity
// without log scraping.
//
// Use SetGlobalAuditBuffer() at server startup to begin capturing.
// The buffer is designed for observability, not forensics — old events
// are dropped when the buffer fills.
type AuditBuffer struct {
	mu       sync.RWMutex
	events   []AuditEvent
	capacity int
	next     int // ring buffer write index
	count    int // total events ever written (monotonic)
}

// NewAuditBuffer creates a ring buffer with the given capacity.
func NewAuditBuffer(capacity int) *AuditBuffer {
	if capacity <= 0 {
		capacity = 100 // default: keep last 100 events
	}
	return &AuditBuffer{
		events:   make([]AuditEvent, capacity),
		capacity: capacity,
	}
}

// Push adds an event to the buffer. Thread-safe.
func (ab *AuditBuffer) Push(event string, attrs map[string]string) {
	if ab == nil {
		return
	}
	ab.mu.Lock()
	defer ab.mu.Unlock()
	ab.events[ab.next] = AuditEvent{
		Event:     event,
		Timestamp: time.Now().UTC(),
		Attrs:     attrs,
	}
	ab.next = (ab.next + 1) % ab.capacity
	ab.count++
}

// Recent returns up to n most recent events in reverse chronological order
// (newest first). Thread-safe.
func (ab *AuditBuffer) Recent(n int) []AuditEvent {
	if ab == nil {
		return nil
	}
	ab.mu.RLock()
	defer ab.mu.RUnlock()

	if n <= 0 || n > ab.capacity {
		n = ab.capacity
	}
	if ab.count == 0 {
		return nil
	}

	// Determine start and effective length in ring
	effectiveLen := ab.capacity
	if ab.count < ab.capacity {
		effectiveLen = ab.count
	}
	if n > effectiveLen {
		n = effectiveLen
	}

	result := make([]AuditEvent, 0, n)
	// Read backwards from (next-1) through ring
	idx := (ab.next - 1 + ab.capacity) % ab.capacity
	for i := 0; i < effectiveLen && len(result) < n; i++ {
		ev := ab.events[idx]
		if !ev.Timestamp.IsZero() { // skip zero-initialized slots
			result = append(result, ev)
		}
		idx = (idx - 1 + ab.capacity) % ab.capacity
	}
	return result
}

// Count returns the total number of events ever pushed. Thread-safe.
func (ab *AuditBuffer) Count() int {
	if ab == nil {
		return 0
	}
	ab.mu.RLock()
	defer ab.mu.RUnlock()
	return ab.count
}

// Capacity returns the ring buffer capacity. Thread-safe.
func (ab *AuditBuffer) Capacity() int {
	if ab == nil {
		return 0
	}
	return ab.capacity
}

// Snapshot returns a JSON-serializable summary of the buffer state. Thread-safe.
func (ab *AuditBuffer) Snapshot() map[string]any {
	if ab == nil {
		return map[string]any{"capacity": 0, "count": 0, "events": []AuditEvent{}}
	}
	return map[string]any{
		"capacity": ab.Capacity(),
		"count":    ab.Count(),
		"events":   ab.Recent(ab.capacity),
	}
}

// ─── Global audit buffer ────────────────────────────────────────────────

var (
	globalAuditMu  sync.RWMutex
	globalAuditBuf *AuditBuffer
)

// SetGlobalAuditBuffer sets the package-level audit buffer that
// CaptureAuditEvent writes to. Call once at server startup.
// Pass nil to disable.
func SetGlobalAuditBuffer(ab *AuditBuffer) {
	globalAuditMu.Lock()
	defer globalAuditMu.Unlock()
	globalAuditBuf = ab
}

// GlobalAuditBuffer returns the current global audit buffer, or nil.
func GlobalAuditBuffer() *AuditBuffer {
	globalAuditMu.RLock()
	defer globalAuditMu.RUnlock()
	return globalAuditBuf
}

// CaptureAuditEvent pushes an event to the global audit buffer (if set).
// This is designed to be called from AuditSecurityEvent so all security
// events are automatically captured without changing the existing API.
func CaptureAuditEvent(event string, attrs ...any) {
	buf := GlobalAuditBuffer()
	if buf == nil {
		return
	}
	am := make(map[string]string)
	for i := 0; i+1 < len(attrs); i += 2 {
		if key, ok := attrs[i].(string); ok {
			if val, ok := attrs[i+1].(string); ok {
				am[key] = val
			} else {
				am[key] = fmt.Sprintf("%v", attrs[i+1])
			}
		}
	}
	buf.Push(event, am)
}

// AuditEventCounts returns a breakdown of event types and their counts
// from the global audit buffer, for use in dashboard summaries.
type AuditEventCounts struct {
	Total       int            `json:"total"`
	EventCounts map[string]int `json:"event_counts"`
	BySeverity  map[string]int `json:"by_severity,omitempty"`
}

// CountEvents returns a summary of all events in the global audit buffer.
func CountEvents() AuditEventCounts {
	buf := GlobalAuditBuffer()
	if buf == nil {
		return AuditEventCounts{}
	}
	events := buf.Recent(buf.Capacity())
	result := AuditEventCounts{
		Total:       len(events),
		EventCounts: make(map[string]int),
	}
	for _, e := range events {
		result.EventCounts[e.Event]++
	}
	return result
}

// AuditBufferJSON is the JSON response type for the audit buffer endpoint.
type AuditBufferJSON struct {
	Capacity       int            `json:"capacity"`
	TotalEvents    int            `json:"total_events"`
	CapturedEvents int            `json:"captured_events"`
	Events         []AuditEvent   `json:"events"`
	EventCounts    map[string]int `json:"event_counts"`
	BufferEnabled  bool           `json:"buffer_enabled"`
}

// MarshalJSON implements json.Marshaler for AuditBufferJSON.
func (a AuditBufferJSON) MarshalJSON() ([]byte, error) {
	type alias AuditBufferJSON
	return json.Marshal(alias(a))
}
