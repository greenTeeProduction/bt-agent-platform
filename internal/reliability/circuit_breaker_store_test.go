package reliability

import (
	"testing"
	"time"
)

// ─── CircuitBreaker.Reset ───────────────────────────────────────────────────
//
// internal/agent's duplicate AgentCircuitBreaker has a Reset method that
// internal/reliability.CircuitBreaker (the platform's canonical breaker) is
// missing. Consolidating the duplicate into this package requires Reset to
// exist here first.

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 5*time.Minute)
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("expected open")
	}
	cb.Reset()
	if cb.State() != CircuitClosed {
		t.Errorf("Reset should close circuit, got %v", cb.State())
	}
	if cb.FailureCount() != 0 {
		t.Errorf("FailureCount should be 0 after Reset, got %d", cb.FailureCount())
	}
}

// ─── CircuitBreakerStore ────────────────────────────────────────────────────
//
// internal/agent.AgentCircuitBreakerStore manages a named registry of
// breakers (Get/Allowed/RecordSuccess/RecordFailure/Status/ResetAll), but
// that logic is duplicated there instead of living on the canonical breaker
// package. Several other callers (internal/llm, internal/a2a,
// internal/agent/webhook_publisher.go) already hand-roll their own
// map[string]*CircuitBreaker registries, so a generic store belongs here.

func TestDefaultCircuitBreakerOptions(t *testing.T) {
	opts := DefaultCircuitBreakerOptions()
	if opts.Threshold != 3 {
		t.Errorf("default Threshold = %d, want 3", opts.Threshold)
	}
	if opts.Cooldown != 5*time.Minute {
		t.Errorf("default Cooldown = %v, want 5m", opts.Cooldown)
	}
}

func TestNewCircuitBreakerStore_Defaults(t *testing.T) {
	store := NewCircuitBreakerStore(CircuitBreakerOptions{})
	cb := store.Get("agent")
	if cb.threshold != 3 {
		t.Errorf("default threshold = %d, want 3", cb.threshold)
	}
	if cb.cooldown != 5*time.Minute {
		t.Errorf("default cooldown = %v, want 5m", cb.cooldown)
	}
}

func TestCircuitBreakerStore_Get_Creates(t *testing.T) {
	store := NewCircuitBreakerStore(CircuitBreakerOptions{})
	cb := store.Get("new-agent")
	if cb == nil {
		t.Fatal("Get should not return nil")
	}
	cb2 := store.Get("new-agent")
	if cb != cb2 {
		t.Error("second Get should return same instance")
	}
}

func TestCircuitBreakerStore_Allowed(t *testing.T) {
	store := NewCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	if !store.Allowed("fresh-agent") {
		t.Error("fresh agent should be allowed")
	}
	store.RecordFailure("fresh-agent")
	if store.Allowed("fresh-agent") {
		t.Error("agent with 1 failure and threshold=1 should be blocked")
	}
}

func TestCircuitBreakerStore_RecordSuccess(t *testing.T) {
	store := NewCircuitBreakerStore(CircuitBreakerOptions{})
	store.RecordFailure("test")
	store.RecordSuccess("test")
	if store.Get("test").FailureCount() != 0 {
		t.Errorf("RecordSuccess should reset failures, got %d", store.Get("test").FailureCount())
	}
}

func TestCircuitBreakerStore_Status(t *testing.T) {
	store := NewCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent-a")
	store.RecordSuccess("agent-b")
	status := store.Status()
	if status["agent-a"].State != CircuitOpen {
		t.Errorf("agent-a state = %v, want open", status["agent-a"].State)
	}
	if status["agent-b"].State != CircuitClosed {
		t.Errorf("agent-b state = %v, want closed", status["agent-b"].State)
	}
}

func TestCircuitBreakerStore_ResetAll(t *testing.T) {
	store := NewCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent-a")
	store.RecordFailure("agent-b")
	store.ResetAll()
	if store.Get("agent-a").State() != CircuitClosed {
		t.Errorf("after ResetAll, agent-a should be closed, got %v", store.Get("agent-a").State())
	}
	if store.Get("agent-b").State() != CircuitClosed {
		t.Errorf("after ResetAll, agent-b should be closed, got %v", store.Get("agent-b").State())
	}
}
