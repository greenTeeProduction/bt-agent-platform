package engine

import (
	"sync"
	"testing"
	"time"
)

func TestNewEventBus(t *testing.T) {
	eb := NewEventBus()
	if eb == nil {
		t.Fatal("NewEventBus returned nil")
	}
	if eb.subs == nil {
		t.Error("subs map not initialized")
	}
	if eb.fired == nil {
		t.Error("fired map not initialized")
	}
}

func TestEventBus_SubscribePublish(t *testing.T) {
	eb := NewEventBus()
	ch := make(chan EventMessage, 1)

	eb.Subscribe("test_key", ch)

	msg := EventMessage{
		Source:    "test",
		Timestamp: time.Now(),
		Type:      "info",
		Data:      map[string]any{"key": "value"},
		Priority:  1,
	}

	eb.Publish("test_key", msg)

	select {
	case received := <-ch:
		if received.Source != "test" {
			t.Errorf("expected Source='test', got %q", received.Source)
		}
		if received.Type != "info" {
			t.Errorf("expected Type='info', got %q", received.Type)
		}
		if received.Priority != 1 {
			t.Errorf("expected Priority=1, got %d", received.Priority)
		}
		if received.Data["key"] != "value" {
			t.Errorf("expected Data[key]='value', got %v", received.Data["key"])
		}
	default:
		t.Error("expected message on channel, got nothing")
	}
}

func TestEventBus_PublishMultipleSubscribers(_ *testing.T) {
	eb := NewEventBus()
	ch1 := make(chan EventMessage, 1)
	ch2 := make(chan EventMessage, 1)

	eb.Subscribe("multi", ch1)
	eb.Subscribe("multi", ch2)

	eb.Publish("multi", EventMessage{Source: "src", Type: "multi_test"})

	// Both channels should receive the message
	<-ch1
	<-ch2
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus()
	ch := make(chan EventMessage, 1)

	eb.Subscribe("unsub", ch)
	eb.Unsubscribe("unsub", ch)

	eb.Publish("unsub", EventMessage{Source: "src", Type: "after_unsub"})

	select {
	case <-ch:
		t.Error("received message after unsubscribe")
	default:
		// Expected: no message
	}
}

func TestEventBus_UnsubscribeNonExistent(_ *testing.T) {
	eb := NewEventBus()
	ch := make(chan EventMessage, 1)
	// Should not panic
	eb.Unsubscribe("nonexistent", ch)
}

func TestEventBus_HasFired(t *testing.T) {
	eb := NewEventBus()

	if eb.HasFired("event1") {
		t.Error("HasFired should be false before publish")
	}

	eb.Publish("event1", EventMessage{Source: "src", Type: "test"})

	if !eb.HasFired("event1") {
		t.Error("HasFired should be true after publish")
	}

	if eb.HasFired("never_fired") {
		t.Error("HasFired for unfired key should be false")
	}
}

func TestEventBus_ResetFired(t *testing.T) {
	eb := NewEventBus()

	eb.Publish("reset_me", EventMessage{Source: "src", Type: "test"})
	if !eb.HasFired("reset_me") {
		t.Error("should be fired after publish")
	}

	eb.ResetFired()

	if eb.HasFired("reset_me") {
		t.Error("should NOT be fired after ResetFired")
	}
}

func TestEventBus_ResetFiredClearsAll(t *testing.T) {
	eb := NewEventBus()
	eb.Publish("a", EventMessage{Source: "s", Type: "t"})
	eb.Publish("b", EventMessage{Source: "s", Type: "t"})
	eb.ResetFired()

	if eb.HasFired("a") || eb.HasFired("b") {
		t.Error("both keys should be cleared after reset")
	}
}

func TestEventBus_DropOnFullChannel(_ *testing.T) {
	eb := NewEventBus()
	ch := make(chan EventMessage, 1)

	eb.Subscribe("full", ch)

	// Fill the channel
	ch <- EventMessage{Source: "fill", Type: "filler"}

	// This should not block — Publish should drop when channel is full
	eb.Publish("full", EventMessage{Source: "overflow", Type: "dropped"})
}

func TestEventBus_Close(t *testing.T) {
	eb := NewEventBus()
	ch := make(chan EventMessage, 1)
	eb.Subscribe("close_me", ch)

	eb.Close()

	// After close, Publish should not panic
	eb.Publish("close_me", EventMessage{Source: "after", Type: "close"})

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after Close()")
	}
}

func TestEventBus_CloseMultipleSubscribers(t *testing.T) {
	eb := NewEventBus()
	ch1 := make(chan EventMessage, 1)
	ch2 := make(chan EventMessage, 1)
	eb.Subscribe("a", ch1)
	eb.Subscribe("b", ch2)

	eb.Close()

	_, ok1 := <-ch1
	_, ok2 := <-ch2
	if ok1 || ok2 {
		t.Error("all subscriber channels should be closed")
	}
}

func TestEventBus_ConcurrentAccess(_ *testing.T) {
	eb := NewEventBus()
	var wg sync.WaitGroup

	// Concurrent subscribe
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := make(chan EventMessage, 1)
			key := "concurrent_" + itoa(id%3)
			eb.Subscribe(key, ch)
		}(i)
	}

	// Concurrent publish
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "concurrent_" + itoa(id%3)
			eb.Publish(key, EventMessage{Source: "concurrent", Type: "test"})
		}(i)
	}

	// Concurrent has/close
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			eb.HasFired("concurrent_0")
		}()
	}

	wg.Wait()
	eb.Close()
}

func TestEventBus_SubscribeAfterClose(_ *testing.T) {
	eb := NewEventBus()
	eb.Close()

	// Should not panic
	ch := make(chan EventMessage, 1)
	eb.Subscribe("after_close", ch)
}

func TestEventBus_HasFiredAfterReset(t *testing.T) {
	eb := NewEventBus()
	eb.Publish("ephemeral", EventMessage{Source: "s", Type: "t"})
	eb.ResetFired()
	eb.Publish("ephemeral", EventMessage{Source: "s", Type: "t"})
	if !eb.HasFired("ephemeral") {
		t.Error("HasFired should be true after re-publish")
	}
}

// itoa is a simple int to string converter to avoid strconv import
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n == 1 {
		return "1"
	}
	return "2"
}
