package security

import (
	"encoding/json"
	"testing"
)

func TestNewAuditBuffer_DefaultCapacity(t *testing.T) {
	ab := NewAuditBuffer(0)
	if ab.capacity != 100 {
		t.Fatalf("expected capacity 100, got %d", ab.capacity)
	}
}

func TestNewAuditBuffer_CustomCapacity(t *testing.T) {
	ab := NewAuditBuffer(50)
	if ab.capacity != 50 {
		t.Fatalf("expected capacity 50, got %d", ab.capacity)
	}
}

func TestAuditBuffer_PushAndCount(t *testing.T) {
	ab := NewAuditBuffer(5)
	ab.Push("test_event", map[string]string{"key": "val"})
	if ab.Count() != 1 {
		t.Fatalf("expected count 1, got %d", ab.Count())
	}
}

func TestAuditBuffer_RecentOrder(t *testing.T) {
	ab := NewAuditBuffer(10)
	for i := 0; i < 5; i++ {
		ab.Push("event", map[string]string{"idx": string(rune('0' + i))})
	}
	recent := ab.Recent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(recent))
	}
}

func TestAuditBuffer_RecentReverseChrono(t *testing.T) {
	ab := NewAuditBuffer(10)
	ab.Push("first", nil)
	ab.Push("second", nil)
	ab.Push("third", nil)
	recent := ab.Recent(10)
	if len(recent) != 3 {
		t.Fatalf("expected 3 events, got %d", len(recent))
	}
	if recent[0].Event != "third" {
		t.Fatalf("expected first (newest) to be 'third', got %q", recent[0].Event)
	}
	if recent[1].Event != "second" {
		t.Fatalf("expected second to be 'second', got %q", recent[1].Event)
	}
	if recent[2].Event != "first" {
		t.Fatalf("expected last (oldest) to be 'first', got %q", recent[2].Event)
	}
}

func TestAuditBuffer_WrapAround(t *testing.T) {
	ab := NewAuditBuffer(3)
	for i := 0; i < 7; i++ {
		ab.Push("event", nil)
	}
	if ab.Count() != 7 {
		t.Fatalf("expected count 7, got %d", ab.Count())
	}
	recent := ab.Recent(10)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent (ring buffer), got %d", len(recent))
	}
}

func TestAuditBuffer_RecentNegative(t *testing.T) {
	ab := NewAuditBuffer(5)
	ab.Push("test", nil)
	recent := ab.Recent(-1)
	if len(recent) != 1 {
		t.Fatalf("expected 1 event for negative n, got %d", len(recent))
	}
}

func TestAuditBuffer_RecentZero(t *testing.T) {
	ab := NewAuditBuffer(5)
	ab.Push("test", nil)
	recent := ab.Recent(0)
	if len(recent) != 1 {
		t.Fatalf("expected 1 event for n=0, got %d", len(recent))
	}
}

func TestAuditBuffer_NilPush(t *testing.T) {
	var ab *AuditBuffer
	ab.Push("test", nil) // should not panic
}

func TestAuditBuffer_NilRecent(t *testing.T) {
	var ab *AuditBuffer
	recent := ab.Recent(10)
	if recent != nil {
		t.Fatalf("expected nil from nil buffer, got %v", recent)
	}
}

func TestAuditBuffer_EmptyBuffer(t *testing.T) {
	ab := NewAuditBuffer(10)
	recent := ab.Recent(5)
	if recent != nil {
		t.Fatalf("expected nil from empty buffer, got %v", recent)
	}
	if ab.Count() != 0 {
		t.Fatalf("expected count 0, got %d", ab.Count())
	}
}

func TestAuditBuffer_Capacity(t *testing.T) {
	ab := NewAuditBuffer(50)
	if ab.Capacity() != 50 {
		t.Fatalf("expected capacity 50, got %d", ab.Capacity())
	}
}

func TestAuditBuffer_NilCapacity(t *testing.T) {
	var ab *AuditBuffer
	if ab.Capacity() != 0 {
		t.Fatalf("expected capacity 0 for nil buffer, got %d", ab.Capacity())
	}
}

func TestAuditBuffer_NilCount(t *testing.T) {
	var ab *AuditBuffer
	if ab.Count() != 0 {
		t.Fatalf("expected count 0 for nil buffer, got %d", ab.Count())
	}
}

func TestAuditBuffer_Snapshot(t *testing.T) {
	ab := NewAuditBuffer(5)
	ab.Push("test_event", map[string]string{"foo": "bar"})
	snap := ab.Snapshot()
	if snap["capacity"].(int) != 5 {
		t.Fatalf("expected capacity 5, got %v", snap["capacity"])
	}
	if snap["count"].(int) != 1 {
		t.Fatalf("expected count 1, got %v", snap["count"])
	}
	events := snap["events"].([]AuditEvent)
	if len(events) != 1 || events[0].Event != "test_event" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestAuditBuffer_NilSnapshot(t *testing.T) {
	var ab *AuditBuffer
	snap := ab.Snapshot()
	if snap["capacity"].(int) != 0 {
		t.Fatalf("expected capacity 0, got %v", snap["capacity"])
	}
}

func TestGlobalAuditBuffer_SetAndGet(t *testing.T) {
	ab := NewAuditBuffer(10)
	SetGlobalAuditBuffer(ab)
	got := GlobalAuditBuffer()
	if got != ab {
		t.Fatalf("SetGlobalAuditBuffer/GlobalAuditBuffer mismatch")
	}
	SetGlobalAuditBuffer(nil)
	if GlobalAuditBuffer() != nil {
		t.Fatalf("expected nil after SetGlobalAuditBuffer(nil)")
	}
}

func TestCaptureAuditEvent_Basic(t *testing.T) {
	ab := NewAuditBuffer(10)
	SetGlobalAuditBuffer(ab)
	defer SetGlobalAuditBuffer(nil)

	CaptureAuditEvent("test_event", "key1", "val1", "key2", "val2")
	if ab.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", ab.Count())
	}
	recent := ab.Recent(1)
	if recent[0].Event != "test_event" {
		t.Fatalf("expected event 'test_event', got %q", recent[0].Event)
	}
	if recent[0].Attrs["key1"] != "val1" || recent[0].Attrs["key2"] != "val2" {
		t.Fatalf("unexpected attrs: %v", recent[0].Attrs)
	}
}

func TestCaptureAuditEvent_NoBuffer(t *testing.T) {
	SetGlobalAuditBuffer(nil)
	// Should not panic
	CaptureAuditEvent("test", "key", "val")
}

func TestCaptureAuditEvent_OddAttrs(t *testing.T) {
	ab := NewAuditBuffer(10)
	SetGlobalAuditBuffer(ab)
	defer SetGlobalAuditBuffer(nil)

	// Odd number of attrs — last key has no value pair
	CaptureAuditEvent("odd_event", "key1", "val1", "orphan_key")
	if ab.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", ab.Count())
	}
	recent := ab.Recent(1)
	if recent[0].Attrs["key1"] != "val1" {
		t.Fatalf("expected key1=val1, got %v", recent[0].Attrs)
	}
}

func TestCaptureAuditEvent_NonStringValues(t *testing.T) {
	ab := NewAuditBuffer(10)
	SetGlobalAuditBuffer(ab)
	defer SetGlobalAuditBuffer(nil)

	CaptureAuditEvent("int_val", "count", "42", "maybe", "true")
	recent := ab.Recent(1)
	if recent[0].Attrs["count"] != "42" {
		t.Fatalf("expected count=42, got %q", recent[0].Attrs["count"])
	}
}

func TestAuditBufferJSON_Marshal(t *testing.T) {
	j := AuditBufferJSON{
		Capacity:       10,
		TotalEvents:    5,
		CapturedEvents: 3,
		Events: []AuditEvent{
			{Event: "test_event", Attrs: map[string]string{"k": "v"}},
		},
		EventCounts:   map[string]int{"test_event": 1},
		BufferEnabled: true,
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded AuditBufferJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Capacity != 10 || decoded.TotalEvents != 5 || decoded.BufferEnabled != true {
		t.Fatalf("roundtrip mismatch: %+v", decoded)
	}
	if len(decoded.Events) != 1 || decoded.Events[0].Event != "test_event" {
		t.Fatalf("event roundtrip mismatch: %+v", decoded.Events)
	}
}

func TestCountEvents_NoBuffer(t *testing.T) {
	SetGlobalAuditBuffer(nil)
	stats := CountEvents()
	if stats.Total != 0 {
		t.Fatalf("expected 0 total with no buffer, got %d", stats.Total)
	}
}

func TestCountEvents_WithEvents(t *testing.T) {
	ab := NewAuditBuffer(20)
	SetGlobalAuditBuffer(ab)
	defer SetGlobalAuditBuffer(nil)

	ab.Push("mcp_auth_failure", nil)
	ab.Push("mcp_auth_failure", nil)
	ab.Push("mcp_tool_call", nil)

	stats := CountEvents()
	if stats.Total != 3 {
		t.Fatalf("expected 3 events, got %d", stats.Total)
	}
	if stats.EventCounts["mcp_auth_failure"] != 2 {
		t.Fatalf("expected 2 mcp_auth_failure, got %d", stats.EventCounts["mcp_auth_failure"])
	}
	if stats.EventCounts["mcp_tool_call"] != 1 {
		t.Fatalf("expected 1 mcp_tool_call, got %d", stats.EventCounts["mcp_tool_call"])
	}
}
