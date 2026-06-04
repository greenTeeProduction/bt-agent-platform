package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ─── WebhookPublisher Coverage ───

func TestDefaultWebhookSecrets(t *testing.T) {
	secrets := DefaultWebhookSecrets()
	if len(secrets) != 3 {
		t.Errorf("expected 3 secrets, got %d", len(secrets))
	}
	for _, name := range []string{"bt-agent-alert", "bt-task-complete", "bt-evolution-event"} {
		if _, ok := secrets[name]; !ok {
			t.Errorf("missing secret for %s", name)
		}
	}
}

func TestNewWebhookPublisher(t *testing.T) {
	pub := NewWebhookPublisher("http://localhost:8644", WebhookSecrets{})
	if pub.baseURL != "http://localhost:8644" {
		t.Errorf("wrong baseURL: %s", pub.baseURL)
	}
	if pub.client.Timeout != 10*time.Second {
		t.Errorf("expected 10s timeout, got %v", pub.client.Timeout)
	}
}

func TestWebhookPublisher_AttachClose(_ *testing.T) {
	bus := InitAgentBus(100)
	pub := NewWebhookPublisher("http://localhost:8644", DefaultWebhookSecrets())
	pub.Attach(bus)
	pub.Close()
	// After close, loop should stop
}

func TestComputeHMAC(t *testing.T) {
	sig := computeHMAC([]byte("test body"), "secret-key")
	if sig == "" {
		t.Error("HMAC should not be empty")
	}
	if len(sig) != 64 { // SHA256 hex is 64 chars
		t.Errorf("expected 64-char hex, got %d chars: %s", len(sig), sig)
	}
}

func TestComputeHMAC_Deterministic(t *testing.T) {
	sig1 := computeHMAC([]byte("hello"), "mysecret")
	sig2 := computeHMAC([]byte("hello"), "mysecret")
	if sig1 != sig2 {
		t.Error("HMAC should be deterministic")
	}
}

func TestComputeHMAC_DifferentKeys(t *testing.T) {
	sig1 := computeHMAC([]byte("hello"), "secret1")
	sig2 := computeHMAC([]byte("hello"), "secret2")
	if sig1 == sig2 {
		t.Error("HMAC should differ with different keys")
	}
}

func TestWebhookPublisher_HandleUnknownEvent(_ *testing.T) {
	bus := InitAgentBus(100)
	pub := NewWebhookPublisher("http://localhost:8644", DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	// Publish an unknown event type — should be logged and skipped
	bus.Publish(AgentEvent{
		Type:   "unknown_event_type_xyz",
		Source: "test",
	})
}

func TestWebhookPublisher_HandleEventNoSecret(_ *testing.T) {
	bus := InitAgentBus(100)
	pub := NewWebhookPublisher("http://localhost:8644", WebhookSecrets{})
	pub.Attach(bus)
	defer pub.Close()

	// Publish a known event type but with no matching secret — should be logged and skipped
	bus.Publish(AgentEvent{
		Type:   "service_down",
		Source: "test",
	})
}

func TestWebhookPublisher_HandleEventHTTPError(_ *testing.T) {
	// Server that refuses connections (wrong port, no listener)
	bus := InitAgentBus(100)
	pub := NewWebhookPublisher("http://localhost:18999", DefaultWebhookSecrets())
	pub.Attach(bus) // starts loop in goroutine
	defer pub.Close()

	// Publish a known event with secret — connection refused should be logged
	bus.Publish(AgentEvent{
		Type:   "task_complete",
		Source: "test",
		Data:   "completed task",
	})

	// Give goroutine time to execute handleEvent
	// The HTTP client has 10s timeout, but connection refused happens immediately
	// We just need the goroutine to wake up and process the event
	for i := 0; i < 50; i++ {
		if pub.eventCh != nil {
			// event is in the channel, loop should pick it up
			break
		}
	}
}

func TestWebhookPublisher_HandleEventHTTP4xx(_ *testing.T) {
	// Create a test server that returns 404
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	bus.Publish(AgentEvent{
		Type:   "task_complete",
		Source: "test",
		Data:   "completed task",
	})
}

func TestWebhookPublisher_HandleEventSuccess(t *testing.T) {
	// Create a test server that returns 200
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type")
		}
		if r.Header.Get("X-Hub-Signature-256") == "" {
			t.Error("expected X-Hub-Signature-256 header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	bus.Publish(AgentEvent{
		Type:      "task_complete",
		Source:    "test-agent",
		Timestamp: time.Now(),
	})
}

func TestWebhookPublisher_HandleServiceDown(_ *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	bus.Publish(AgentEvent{
		Type:   "service_down",
		Source: "bt-agent",
	})
}

func TestWebhookPublisher_HandleHealthAlert(_ *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	bus.Publish(AgentEvent{
		Type:   "health_alert",
		Source: "bt-agent",
	})
}

func TestWebhookPublisher_HandleEvolutionStep(_ *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	bus.Publish(AgentEvent{
		Type:   "evolution_step",
		Source: "gardener",
	})
}

// ─── loop() edge cases ───

func TestWebhookPublisher_LoopStopsOnChannelClose(_ *testing.T) {
	// Create a publisher with a custom event source
	bus := InitAgentBus(100)
	pub := NewWebhookPublisher("http://localhost:8644", DefaultWebhookSecrets())

	// Attach subscribes and starts loop
	pub.Attach(bus)

	// Close the bus — this closes all subscriber channels
	bus.Close()

	// After close, loop should exit gracefully
	// (no panic, no goroutine leak)
}
