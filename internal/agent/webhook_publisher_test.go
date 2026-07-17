package agent

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// ─── WebhookPublisher Coverage ───

// TestWebhookPublisher_MarshalErrorDoesNotWedgeHalfOpenBreaker reproduces the
// permanent-wedge bug: once a subscription's breaker has tripped open and its
// cooldown elapses, the next event consumes the single half-open probe via
// breaker.Allow(). If that event's Data fails to JSON-marshal, handleEvent
// returned before recording any breaker outcome, leaving the breaker stuck
// HalfOpen — where Allow() always returns false — so every subsequent
// deliverable event was silently dropped until process restart.
func TestWebhookPublisher_MarshalErrorDoesNotWedgeHalfOpenBreaker(t *testing.T) {
	var delivered int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&delivered, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())

	const sub = "bt-agent-alert" // service_down events route here
	// Replace the minute-cooldown breaker with a short-cooldown one already
	// tripped open and past its cooldown — primed to serve exactly one
	// half-open probe on the next Allow().
	cb := reliability.NewCircuitBreaker(sub, 1, time.Millisecond)
	cb.RecordFailure() // threshold 1 -> Open
	pub.breakers[sub] = cb
	time.Sleep(10 * time.Millisecond) // let the cooldown elapse

	// An event whose Data cannot be JSON-marshaled (a channel value) must NOT
	// consume the half-open probe.
	pub.handleEvent(AgentEvent{Type: "service_down", Source: "x", Message: "m", Data: make(chan int)})

	// A subsequent well-formed event must be delivered, not dropped by a
	// breaker the marshal failure wedged half-open.
	pub.handleEvent(AgentEvent{Type: "service_down", Source: "x", Message: "ok", Data: map[string]interface{}{"k": "v"}})

	if got := atomic.LoadInt32(&delivered); got != 1 {
		t.Fatalf("valid event after a marshal-failing one was not delivered (delivered=%d); the marshal failure wedged the half-open breaker", got)
	}
}

// TestWebhookPublisher_ClientErrorsDoNotTripBreaker verifies delivery
// failures Hermes rejects as bad requests (4xx, typed non-retryable by
// postSigned) do not walk the subscription breaker open: five consecutive
// payload rejections must not suppress the next deliverable event for the
// whole cooldown window.
func TestWebhookPublisher_ClientErrorsDoNotTripBreaker(t *testing.T) {
	var serves int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&serves, 1)
		if n <= 5 { // breaker threshold is 5
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	for i := 0; i < 6; i++ {
		pub.handleEvent(AgentEvent{Type: "service_down", Source: "x", Message: "m", Data: map[string]interface{}{"i": i}})
	}
	if got := atomic.LoadInt32(&serves); got != 6 {
		t.Fatalf("server saw %d requests, want 6 — the 6th deliverable event must not be skipped by a breaker opened on 4xx rejections", got)
	}
}

// TestWebhookPublisher_ReplaysDeadLettersOnRecovery closes the write-only-DLQ
// gap: dead-lettered deliveries were pushed but nothing ever replayed them, so
// failed webhooks were silently evicted oldest-first. A successful delivery is
// the recovery signal — after it, queued dead letters must be replayed through
// the same signed single-shot path and removed from the queue on success.
func TestWebhookPublisher_ReplaysDeadLettersOnRecovery(t *testing.T) {
	var mu sync.Mutex
	var fail bool
	var bodies []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		bodies = append(bodies, string(b))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())

	// First event: backend down -> delivery fails after retries -> dead letter.
	mu.Lock()
	fail = true
	mu.Unlock()
	pub.handleEvent(AgentEvent{Type: "service_down", Source: "x", Message: "first", Data: map[string]interface{}{"n": 1}})
	if pub.dlq.Len() != 1 {
		t.Fatalf("dlq.Len() = %d, want 1 after a failed delivery", pub.dlq.Len())
	}

	// Backend recovers; the next successful delivery must trigger a replay.
	mu.Lock()
	fail = false
	mu.Unlock()
	pub.handleEvent(AgentEvent{Type: "service_down", Source: "x", Message: "second", Data: map[string]interface{}{"n": 2}})

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pub.dlq.Len() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := pub.dlq.Len(); got != 0 {
		t.Fatalf("dlq.Len() = %d after recovery, want 0 (queued dead letters must be replayed once delivery succeeds)", got)
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, b := range bodies {
		if strings.Contains(b, `"first"`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the dead-lettered payload was never re-delivered; server saw: %v", bodies)
	}
}

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

// ─── panic recovery regression ───

// webhookPublisherPanicSubprocessEnv gates the child-process body of
// TestWebhookPublisherLoop_PanicRecovered. An unrecovered panic inside the
// `go p.loop()` goroutine started by Attach() crashes the entire process —
// it cannot be caught by the parent test's own recover() — so this test
// re-execs itself and asserts the child survives instead of crashing. This
// mirrors TestHealthMonitorStart_PanicRecovered in internal/llm/health_test.go.
const webhookPublisherPanicSubprocessEnv = "BT_WEBHOOK_PUBLISHER_PANIC_SUBPROCESS"

// panickyPayload simulates a malformed event.Data payload from an AgentBus
// producer: its MarshalJSON always panics, which propagates uncaught through
// json.Marshal (confirmed: encoding/json only recovers panics it wraps in
// its own internal jsonError sentinel; a plain panic value re-panics).
type panickyPayload struct{}

func (panickyPayload) MarshalJSON() ([]byte, error) {
	panic("boom: malformed event payload")
}

// TestWebhookPublisherLoop_PanicRecovered is the regression test for
// WebhookPublisher.Attach()'s background loop lacking panic recovery: a
// panic inside handleEvent (e.g. triggered by json.Marshal on a malformed
// event.Data payload forwarded from an AgentBus producer) must be recovered
// and logged instead of crashing the bt-agent daemon that attached the
// publisher, and the loop must keep forwarding subsequent events afterward.
func TestWebhookPublisherLoop_PanicRecovered(t *testing.T) {
	if os.Getenv(webhookPublisherPanicSubprocessEnv) == "1" {
		var requests int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&requests, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		bus := InitAgentBus(100)
		pub := NewWebhookPublisher(server.URL, DefaultWebhookSecrets())
		pub.Attach(bus)
		defer pub.Close()

		// Publish events whose Data panics during json.Marshal, interleaved
		// with normal events. If the panics are recovered, the normal
		// events still reach the server; if not, the process crashes and
		// none of this ever runs.
		for i := 0; i < 3; i++ {
			bus.Publish(AgentEvent{
				Type:   "task_complete",
				Source: "test",
				Data:   panickyPayload{},
			})
			bus.Publish(AgentEvent{
				Type:   "task_complete",
				Source: "test",
				Data:   "ok",
			})
		}

		time.Sleep(150 * time.Millisecond)

		if got := atomic.LoadInt64(&requests); got < 3 {
			fmt.Fprintf(os.Stderr, "expected at least 3 successful posts despite panicking "+
				"payloads (publisher loop should keep running), got %d\n", got)
			os.Exit(3)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestWebhookPublisherLoop_PanicRecovered")
	cmd.Env = append(os.Environ(), webhookPublisherPanicSubprocessEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("WebhookPublisher loop: a panicking event handler crashed the process (or "+
			"the loop stopped forwarding events) instead of being recovered via "+
			"reliability.SafeGo and continuing to run; exit error=%v output=%s", err, out)
	}
}

// ─── retry-with-backoff regression ───

// TestWebhookPublisher_HandleEventRetry is the regression test for
// handleEvent's outbound POST lacking retry: transport errors and 5xx
// responses from Hermes must be retried with backoff until they eventually
// succeed, but non-retryable 4xx responses must NOT be retried (Hermes is
// telling us the request itself is bad, so retrying just wastes attempts).
func TestWebhookPublisher_HandleEventRetry(t *testing.T) {
	tests := []struct {
		name            string
		failCount       int  // requests to fail before the server starts succeeding
		failStatus      int  // 0 means simulate a transport error (hijack + close, no response)
		wantEventually  bool // event must eventually be delivered (server sees a 200)
		wantMaxRequests int64
	}{
		{
			name:           "retries on 5xx then succeeds",
			failCount:      1,
			failStatus:     http.StatusInternalServerError,
			wantEventually: true,
		},
		{
			name:           "retries on transport error then succeeds",
			failCount:      1,
			failStatus:     0,
			wantEventually: true,
		},
		{
			name:            "does not retry non-retryable 4xx",
			failCount:       1 << 30, // always fails
			failStatus:      http.StatusBadRequest,
			wantEventually:  false,
			wantMaxRequests: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests int64
			var delivered int64

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := atomic.AddInt64(&requests, 1)
				if int(n) <= tt.failCount {
					if tt.failStatus == 0 {
						hj, ok := w.(http.Hijacker)
						if !ok {
							t.Fatalf("ResponseWriter does not support hijacking")
						}
						conn, _, err := hj.Hijack()
						if err != nil {
							t.Fatalf("hijack: %v", err)
						}
						conn.Close() // abrupt close simulates a transport error
						return
					}
					w.WriteHeader(tt.failStatus)
					return
				}
				atomic.StoreInt64(&delivered, 1)
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			bus := InitAgentBus(100)
			pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
			pub.Attach(bus)
			defer pub.Close()

			bus.Publish(AgentEvent{
				Type:      "service_down",
				Source:    "test",
				Timestamp: time.Now(),
			})

			if tt.wantEventually {
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) && atomic.LoadInt64(&delivered) == 0 {
					time.Sleep(10 * time.Millisecond)
				}
				if atomic.LoadInt64(&delivered) == 0 {
					t.Fatalf("event was not eventually delivered after %d failing attempt(s); got %d requests",
						tt.failCount, atomic.LoadInt64(&requests))
				}
				return
			}

			// Non-retryable case: give handleEvent time to run its (single,
			// non-retried) attempt, then confirm no retry followed.
			time.Sleep(300 * time.Millisecond)
			if got := atomic.LoadInt64(&requests); got > tt.wantMaxRequests {
				t.Fatalf("expected non-retryable status %d to result in at most %d request(s), got %d",
					tt.failStatus, tt.wantMaxRequests, got)
			}
		})
	}
}

// ─── circuit breaker regression ───

// TestWebhookPublisher_CircuitBreakerTripsAndRecovers is the regression test
// for milestone 2 of the Q3 reliability program: WebhookPublisher must keep
// a *reliability.CircuitBreaker per webhook subscription, checked before
// each delivery attempt in handleEvent (before the retry-wrapped POST), so
// a persistently-failing Hermes endpoint stops being hammered by every
// subsequent event once the breaker trips — and resumes deliveries once the
// breaker's cooldown elapses.
func TestWebhookPublisher_CircuitBreakerTripsAndRecovers(t *testing.T) {
	const failThreshold = 3 // matches webhookRetryPolicy's MaxRetries: every attempt of the first delivery fails

	var requests int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt64(&requests, 1)
		if n <= failThreshold {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	// Inject a fast breaker (threshold=1, short cooldown) for the
	// "bt-agent-alert" subscription so the test doesn't have to wait out a
	// production-sized cooldown window.
	pub.breakers = map[string]*reliability.CircuitBreaker{
		"bt-agent-alert": reliability.NewCircuitBreaker("bt-agent-alert", 1, 250*time.Millisecond),
	}
	pub.Attach(bus)
	defer pub.Close()

	// First event: every retried attempt fails (5xx), so once the delivery
	// attempt is exhausted the breaker (threshold=1) trips open.
	bus.Publish(AgentEvent{Type: "service_down", Source: "test", Timestamp: time.Now()})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt64(&requests) < failThreshold {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&requests); got != failThreshold {
		t.Fatalf("expected exactly %d requests from the first (failing) delivery attempt, got %d", failThreshold, got)
	}

	breaker := pub.breakers["bt-agent-alert"]
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && breaker.State() != reliability.CircuitOpen {
		time.Sleep(10 * time.Millisecond)
	}
	if breaker.State() != reliability.CircuitOpen {
		t.Fatalf("expected circuit breaker to be open after the first delivery attempt exhausted its retries, got %v", breaker.State())
	}

	// Second event while the breaker is still open (cooldown not yet
	// elapsed): handleEvent must skip the HTTP call entirely instead of
	// hammering the persistently-failing endpoint again.
	bus.Publish(AgentEvent{Type: "service_down", Source: "test", Timestamp: time.Now()})
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt64(&requests); got != failThreshold {
		t.Fatalf("expected no further HTTP calls once the breaker tripped, got %d requests (want %d)", got, failThreshold)
	}

	// Wait for the cooldown to elapse, then publish a third event. The
	// breaker should allow this single delivery attempt through
	// (half-open); the server now answers 200, so it succeeds and the
	// breaker should recover to closed. The successful delivery also
	// triggers the dead-letter replay sweep, which re-delivers the first
	// (dead-lettered) event: one delivery + one replay request.
	time.Sleep(300 * time.Millisecond)
	bus.Publish(AgentEvent{Type: "service_down", Source: "test", Timestamp: time.Now()})

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt64(&requests) < failThreshold+2 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&requests); got != failThreshold+2 {
		t.Fatalf("expected the post-cooldown delivery plus the dead-letter replay (%d requests total), got %d", failThreshold+2, got)
	}

	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) && breaker.State() != reliability.CircuitClosed {
		time.Sleep(10 * time.Millisecond)
	}
	if breaker.State() != reliability.CircuitClosed {
		t.Fatalf("expected circuit breaker to recover to closed after a successful delivery post-cooldown, got %v", breaker.State())
	}
}

// ─── DLQ replay regression (milestone 3) ───

// TestWebhookPublisher_DLQReplayRedeliversAfterRecovery is the regression
// test for milestone 3 of the Q3 reliability program: once a webhook
// delivery exhausts webhookRetryPolicy's retries, it must be pushed to the
// publisher's dead letter queue, and dlq.Replay(id) — wired via
// SetReplayExecutor to re-POST through the same signed-request path used by
// handleEvent — must successfully redeliver the event once the mock Hermes
// endpoint recovers, removing the entry from the DLQ.
func TestWebhookPublisher_DLQReplayRedeliversAfterRecovery(t *testing.T) {
	var requests int64
	var recovered int32
	var lastSig string
	var lastBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		if atomic.LoadInt32(&recovered) == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, _ := io.ReadAll(r.Body)
		lastBody = body
		lastSig = r.Header.Get("X-Hub-Signature-256")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	bus := InitAgentBus(100)
	pub := NewWebhookPublisher(ts.URL, DefaultWebhookSecrets())
	pub.Attach(bus)
	defer pub.Close()

	bus.Publish(AgentEvent{Type: "service_down", Source: "test", Timestamp: time.Now()})

	// Wait for the failing delivery to exhaust its retries and land in the DLQ.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && pub.dlq.Len() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := pub.dlq.Len(); got != 1 {
		t.Fatalf("expected exactly one dead-lettered webhook event after retries were exhausted, got %d", got)
	}
	entries := pub.dlq.List()
	entry := entries[0]

	// Hermes recovers.
	atomic.StoreInt32(&recovered, 1)

	redelivered, ok := pub.dlq.Replay(entry.ID)
	if !ok || redelivered == nil {
		t.Fatalf("expected dlq.Replay(%q) to succeed once Hermes recovered", entry.ID)
	}

	if lastSig == "" {
		t.Fatalf("expected replay to re-POST through the signed-request path (X-Hub-Signature-256 header), got none")
	}
	wantSig := computeHMAC(lastBody, DefaultWebhookSecrets()["bt-agent-alert"])
	if lastSig != "sha256="+wantSig {
		t.Fatalf("replay signature mismatch: got %q, want %q (body not signed with the subscription's own secret)", lastSig, "sha256="+wantSig)
	}

	if got := pub.dlq.Len(); got != 0 {
		t.Fatalf("expected the dead letter entry to be removed after a successful replay, got %d remaining", got)
	}
}
