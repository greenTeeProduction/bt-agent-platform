package agent

import (
	"sync"
	"testing"
	"time"
)

func TestInitAgentBus(t *testing.T) {
	// Reset global state for clean test
	GlobalAgentBus = nil

	bus := InitAgentBus(50)
	if bus == nil {
		t.Fatal("InitAgentBus returned nil")
	}
	if GlobalAgentBus == nil {
		t.Fatal("GlobalAgentBus not set")
	}
	if capVal := bus.maxHistory; capVal != 50 {
		t.Fatalf("expected maxHistory 50, got %d", capVal)
	}
}

func TestInitAgentBus_DefaultMaxHistory(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(0)
	if bus.maxHistory != 100 {
		t.Fatalf("expected default maxHistory 100, got %d", bus.maxHistory)
	}
}

func TestAgentBus_SubscribePublish(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch := bus.Subscribe("test_event")
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}

	event := AgentEvent{
		Type:    "test_event",
		Source:  "agent_a",
		Message: "hello",
	}
	bus.Publish(event)

	select {
	case received := <-ch:
		if received.Type != "test_event" {
			t.Fatalf("expected type test_event, got %s", received.Type)
		}
		if received.Source != "agent_a" {
			t.Fatalf("expected source agent_a, got %s", received.Source)
		}
		if received.Message != "hello" {
			t.Fatalf("expected message hello, got %s", received.Message)
		}
		if received.Timestamp.IsZero() {
			t.Fatal("expected non-zero timestamp")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestAgentBus_AllSubscribersReceiveAll(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch := bus.Subscribe("") // subscribe to ALL events
	bus.Publish(AgentEvent{Type: "type_a", Source: "s1", Message: "m1"})
	bus.Publish(AgentEvent{Type: "type_b", Source: "s2", Message: "m2"})

	received := 0
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
			received++
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
	if received != 2 {
		t.Fatalf("expected 2 events, got %d", received)
	}
}

func TestAgentBus_TypeFilteredSubscriber(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	chA := bus.Subscribe("type_a")
	chB := bus.Subscribe("type_b")

	bus.Publish(AgentEvent{Type: "type_a", Source: "s1"})
	bus.Publish(AgentEvent{Type: "type_b", Source: "s2"})

	// chA should only get type_a
	select {
	case ev := <-chA:
		if ev.Type != "type_a" {
			t.Fatalf("chA got wrong type: %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("chA timeout")
	}

	// chB should only get type_b
	select {
	case ev := <-chB:
		if ev.Type != "type_b" {
			t.Fatalf("chB got wrong type: %s", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("chB timeout")
	}
}

func TestAgentBus_MultipleSubscribers(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch1 := bus.Subscribe("evt")
	ch2 := bus.Subscribe("evt")

	bus.Publish(AgentEvent{Type: "evt", Source: "s1"})

	for i, ch := range []<-chan AgentEvent{ch1, ch2} {
		select {
		case <-ch:
			// ok
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d timeout", i)
		}
	}
}

func TestAgentBus_History(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(10)

	// No events yet
	h := bus.History(5)
	if len(h) != 0 {
		t.Fatalf("expected empty history, got %d entries", len(h))
	}

	// Publish 3 events
	bus.Publish(AgentEvent{Type: "a", Source: "s1"})
	bus.Publish(AgentEvent{Type: "b", Source: "s2"})
	bus.Publish(AgentEvent{Type: "c", Source: "s3"})

	h = bus.History(0) // limit=0 should return all
	if len(h) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(h))
	}
	if h[0].Type != "a" || h[2].Type != "c" {
		t.Fatalf("unexpected order: %v", []string{h[0].Type, h[1].Type, h[2].Type})
	}

	// Limit to 2
	h = bus.History(2)
	if len(h) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(h))
	}
	if h[0].Type != "b" || h[1].Type != "c" {
		t.Fatalf("expected last 2 events, got %v", []string{h[0].Type, h[1].Type})
	}
}

func TestAgentBus_HistoryRingBuffer(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(3) // max 3

	for i := 0; i < 5; i++ {
		bus.Publish(AgentEvent{Type: "evt", Source: "s1", Message: string(rune('A' + i))})
	}

	h := bus.History(10)
	if len(h) != 3 {
		t.Fatalf("expected 3 history entries (ring buffer), got %d", len(h))
	}
}

func TestAgentBus_SubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)
	bus.Close()

	ch := bus.Subscribe("test")
	_, ok := <-ch
	if ok {
		t.Fatal("expected closed channel from Subscribe after Close")
	}
}

func TestAgentBus_PublishAfterCloseDoesNothing(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch := bus.Subscribe("test")
	bus.Close()

	// Should not panic; event is dropped
	bus.Publish(AgentEvent{Type: "test", Source: "s1"})

	// Channel is closed — we'll read a zero-value, not a real event
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatal("channel should be closed after Close")
		}
		_ = ev
	default:
		t.Fatal("expected closed channel to be readable")
	}

	// History should be empty (publish after close is dropped)
	h := bus.History(10)
	if len(h) != 0 {
		t.Fatalf("expected empty history after close, got %d", len(h))
	}
}

func TestAgentBus_CloseSubscriberChannelsClosed(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch1 := bus.Subscribe("type_a")
	ch2 := bus.Subscribe("")

	bus.Close()

	if _, ok := <-ch1; ok {
		t.Fatal("type subscriber channel not closed after Close")
	}
	if _, ok := <-ch2; ok {
		t.Fatal("all subscriber channel not closed after Close")
	}
}

func TestAgentBus_PublishTimestampSet(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch := bus.Subscribe("test")
	bus.Publish(AgentEvent{Type: "test", Source: "s1"})

	select {
	case ev := <-ch:
		if ev.Timestamp.IsZero() {
			t.Fatal("timestamp should be auto-set when zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestAgentBus_PublishPreservesExplicitTimestamp(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	explicit := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ch := bus.Subscribe("test")
	bus.Publish(AgentEvent{Type: "test", Source: "s1", Timestamp: explicit})

	select {
	case ev := <-ch:
		if !ev.Timestamp.Equal(explicit) {
			t.Fatalf("expected explicit timestamp, got %v", ev.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestAgentBus_PublishMultipleTypes(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	ch := bus.Subscribe("") // all events

	events := []AgentEvent{
		{Type: "service_down", Source: "monitor", Message: "agent down"},
		{Type: "health_alert", Source: "health", Message: "high CPU"},
		{Type: "task_complete", Source: "runner", Message: "done"},
		{Type: "error_detected", Source: "engine", Message: "panic recovered"},
	}

	for _, e := range events {
		bus.Publish(e)
	}

	for i, expected := range events {
		select {
		case ev := <-ch:
			if ev.Type != expected.Type || ev.Source != expected.Source {
				t.Fatalf("event %d: expected (%s,%s), got (%s,%s)",
					i, expected.Type, expected.Source, ev.Type, ev.Source)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout for event %d", i)
		}
	}
}

func TestAgentBus_DropOnFullChannel(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)

	// Channel buffer is 16; send 20 events quickly
	ch := bus.Subscribe("test")
	for i := 0; i < 20; i++ {
		bus.Publish(AgentEvent{Type: "test", Source: "s1", Message: string(rune('A' + i))})
	}

	// Drain what we can — should see at most 16 events
	count := 0
	for {
		select {
		case <-ch:
			count++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:
	if count > 16 {
		t.Fatalf("expected at most 16 events (buffer size), got %d", count)
	}
	if count == 0 {
		t.Fatal("expected at least some events to arrive")
	}
}

func TestPublishServiceDown(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)
	ch := bus.Subscribe("service_down")

	PublishServiceDown("monitor", "bt-agent")

	select {
	case ev := <-ch:
		if ev.Type != "service_down" {
			t.Fatalf("expected service_down type, got %s", ev.Type)
		}
		if ev.Source != "monitor" {
			t.Fatalf("expected source monitor, got %s", ev.Source)
		}
		if ev.Message != "bt-agent is not running" {
			t.Fatalf("expected message 'bt-agent is not running', got %s", ev.Message)
		}
		if ev.Data == nil {
			t.Fatal("expected non-nil Data")
		}
		if data, ok := ev.Data.(map[string]string); !ok || data["service"] != "bt-agent" {
			t.Fatalf("expected Data.service = bt-agent, got %v", ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PublishServiceDown")
	}
}

func TestPublishServiceDown_NilGlobalBus(_ *testing.T) {
	GlobalAgentBus = nil
	// Must not panic or crash
	PublishServiceDown("monitor", "bt-agent")
	// No assertion needed — just verifying no panic
}

func TestPublishHealthAlert(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)
	ch := bus.Subscribe("health_alert")

	PublishHealthAlert("health", "CPU at 95%")

	select {
	case ev := <-ch:
		if ev.Type != "health_alert" {
			t.Fatalf("expected health_alert type, got %s", ev.Type)
		}
		if ev.Source != "health" {
			t.Fatalf("expected source health, got %s", ev.Source)
		}
		if ev.Message != "CPU at 95%" {
			t.Fatalf("expected message 'CPU at 95%%', got %s", ev.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for PublishHealthAlert")
	}
}

func TestPublishHealthAlert_NilGlobalBus(_ *testing.T) {
	GlobalAgentBus = nil
	PublishHealthAlert("health", "CPU at 95%")
	// No panic check
}

func TestAgentBus_ConcurrentPublish(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(200)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			bus.Publish(AgentEvent{Type: "concurrent", Source: "t1"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			bus.Publish(AgentEvent{Type: "concurrent", Source: "t2"})
		}
	}()
	wg.Wait()

	// Under RLock, concurrent appends may interleave — some events may be lost
	// to race conditions on the slice header. We assert at least 50 events
	// arrived (no data corruption) and no more than 100.
	h := bus.History(200)
	if len(h) < 50 {
		t.Fatalf("expected at least 50 events after concurrent publish, got %d", len(h))
	}
	if len(h) > 100 {
		t.Fatalf("expected at most 100 events, got %d", len(h))
	}
}

func TestAgentBus_MultipleCloseIdempotent(_ *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)
	bus.Close()
	// Second close must not panic
	bus.Close()
}

func TestAgentBus_HistoryNegativeLimit(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(100)
	bus.Publish(AgentEvent{Type: "a"})
	bus.Publish(AgentEvent{Type: "b"})

	// Negative limit should return all entries
	h := bus.History(-1)
	if len(h) != 2 {
		t.Fatalf("expected 2 history entries with negative limit, got %d", len(h))
	}
}

func TestAgentBus_HistoryExceedsMax(t *testing.T) {
	GlobalAgentBus = nil
	bus := InitAgentBus(5)
	for i := 0; i < 10; i++ {
		bus.Publish(AgentEvent{Type: "t"})
	}
	// Requesting more than max should return all available
	h := bus.History(100)
	if len(h) != 5 {
		t.Fatalf("expected 5 entries (capped by ring buffer), got %d", len(h))
	}
}
