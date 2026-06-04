package agent

import (
	"testing"
	"time"
)

// ── AgentCircuitBreaker Unit Tests ──────────────────────────────────────────

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half_open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestNewAgentCircuitBreaker_Defaults(t *testing.T) {
	cb := NewAgentCircuitBreaker("test-agent", 0, 0)
	if cb.name != "test-agent" {
		t.Errorf("name = %q, want %q", cb.name, "test-agent")
	}
	if cb.threshold != 3 {
		t.Errorf("default threshold = %d, want 3", cb.threshold)
	}
	if cb.cooldown != 5*time.Minute {
		t.Errorf("default cooldown = %v, want 5m", cb.cooldown)
	}
	if cb.state != CircuitClosed {
		t.Errorf("initial state = %v, want closed", cb.state)
	}
}

func TestNewAgentCircuitBreaker_Custom(t *testing.T) {
	cb := NewAgentCircuitBreaker("custom", 2, 30*time.Second)
	if cb.threshold != 2 {
		t.Errorf("threshold = %d, want 2", cb.threshold)
	}
	if cb.cooldown != 30*time.Second {
		t.Errorf("cooldown = %v, want 30s", cb.cooldown)
	}
}

func TestAgentCircuitBreaker_Allow_Closed(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 3, 5*time.Minute)
	if !cb.Allow() {
		t.Error("closed breaker should allow execution")
	}
}

func TestAgentCircuitBreaker_Allow_Open_NotExpired(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 2, 5*time.Minute)
	// Trigger open: 2 failures
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("expected circuit to be open after 2 failures")
	}
	if cb.Allow() {
		t.Error("open breaker should deny execution during cooldown")
	}
}

func TestAgentCircuitBreaker_Allow_Open_Expired(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 1, 1*time.Millisecond)
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("expected circuit open after 1 failure")
	}
	time.Sleep(2 * time.Millisecond)
	if !cb.Allow() {
		t.Error("expired open breaker should allow half-open test request")
	}
	if cb.State() != CircuitHalfOpen {
		t.Errorf("expected half-open after Allow on expired breaker, got %v", cb.State())
	}
}

func TestAgentCircuitBreaker_Allow_HalfOpen_SecondCall(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 1, 1*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(2 * time.Millisecond)
	cb.Allow() // transitions to half-open, returns true
	// Second call while still half-open
	if cb.Allow() {
		t.Error("half-open breaker should deny second concurrent request")
	}
}

func TestAgentCircuitBreaker_RecordSuccess_ClosesCircuit(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 2, 5*time.Minute)
	cb.RecordFailure()
	cb.RecordFailure() // now open
	if cb.State() != CircuitOpen {
		t.Fatal("expected open")
	}
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("RecordSuccess should close circuit, got %v", cb.State())
	}
	if cb.FailureCount() != 0 {
		t.Errorf("FailureCount should be 0 after success, got %d", cb.FailureCount())
	}
}

func TestAgentCircuitBreaker_RecordSuccess_FromHalfOpen(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 1, 1*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(2 * time.Millisecond)
	cb.Allow() // → half-open
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("RecordSuccess from half-open should close, got %v", cb.State())
	}
}

func TestAgentCircuitBreaker_RecordFailure_ToOpen(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 3, 5*time.Minute)
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Error("below threshold should stay closed")
	}
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Error("below threshold should stay closed")
	}
	cb.RecordFailure() // 3rd failure → open
	if cb.State() != CircuitOpen {
		t.Errorf("3 failures should open circuit, got %v", cb.State())
	}
}

func TestAgentCircuitBreaker_RecordFailure_FromHalfOpen(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 1, 1*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(2 * time.Millisecond)
	cb.Allow() // → half-open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("failure from half-open should reopen, got %v", cb.State())
	}
}

func TestAgentCircuitBreaker_Reset(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 1, 5*time.Minute)
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

func TestAgentCircuitBreaker_FailureCount(t *testing.T) {
	cb := NewAgentCircuitBreaker("test", 3, 5*time.Minute)
	if cb.FailureCount() != 0 {
		t.Errorf("initial failure count should be 0, got %d", cb.FailureCount())
	}
	cb.RecordFailure()
	if cb.FailureCount() != 1 {
		t.Errorf("failure count should be 1, got %d", cb.FailureCount())
	}
	cb.RecordFailure()
	if cb.FailureCount() != 2 {
		t.Errorf("failure count should be 2, got %d", cb.FailureCount())
	}
}

// ── AgentCircuitBreakerStore Tests ──────────────────────────────────────────

func TestDefaultCircuitBreakerOptions(t *testing.T) {
	opts := DefaultCircuitBreakerOptions()
	if opts.Threshold != 3 {
		t.Errorf("default Threshold = %d, want 3", opts.Threshold)
	}
	if opts.Cooldown != 5*time.Minute {
		t.Errorf("default Cooldown = %v, want 5m", opts.Cooldown)
	}
}

func TestNewAgentCircuitBreakerStore_Defaults(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	if store.options.Threshold != 3 {
		t.Errorf("default threshold = %d, want 3", store.options.Threshold)
	}
	if store.options.Cooldown != 5*time.Minute {
		t.Errorf("default cooldown = %v, want 5m", store.options.Cooldown)
	}
}

func TestNewAgentCircuitBreakerStore_Custom(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{
		Threshold: 2,
		Cooldown:  10 * time.Second,
	})
	if store.options.Threshold != 2 {
		t.Errorf("threshold = %d, want 2", store.options.Threshold)
	}
	if store.options.Cooldown != 10*time.Second {
		t.Errorf("cooldown = %v, want 10s", store.options.Cooldown)
	}
}

func TestAgentCircuitBreakerStore_Get_Creates(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	cb := store.Get("new-agent")
	if cb == nil {
		t.Fatal("Get should not return nil")
	}
	if cb.name != "new-agent" {
		t.Errorf("cb.name = %q, want %q", cb.name, "new-agent")
	}
	// Second call should return same instance
	cb2 := store.Get("new-agent")
	if cb != cb2 {
		t.Error("second Get should return same instance")
	}
}

func TestAgentCircuitBreakerStore_Allowed(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	if !store.Allowed("fresh-agent") {
		t.Error("fresh agent should be allowed")
	}
	store.RecordFailure("fresh-agent")
	if store.Allowed("fresh-agent") {
		t.Error("agent with 1 failure and threshold=1 should be blocked")
	}
}

func TestAgentCircuitBreakerStore_RecordSuccess(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	store.RecordFailure("test")
	store.RecordSuccess("test")
	cb := store.Get("test")
	if cb.FailureCount() != 0 {
		t.Errorf("RecordSuccess should reset failures, got %d", cb.FailureCount())
	}
}

func TestAgentCircuitBreakerStore_Status(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent-a")
	store.RecordSuccess("agent-b")
	status := store.Status()
	if _, ok := status["agent-a"]; !ok {
		t.Error("agent-a should be in status")
	}
	if _, ok := status["agent-b"]; !ok {
		t.Error("agent-b should be in status")
	}
	if status["agent-a"].State != CircuitOpen {
		t.Errorf("agent-a state = %v, want open", status["agent-a"].State)
	}
	if status["agent-b"].State != CircuitClosed {
		t.Errorf("agent-b state = %v, want closed", status["agent-b"].State)
	}
}

func TestAgentCircuitBreakerStore_ResetAll(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent-a")
	store.RecordFailure("agent-b")
	if store.Get("agent-a").State() != CircuitOpen {
		t.Fatal("expected open")
	}
	store.ResetAll()
	if store.Get("agent-a").State() != CircuitClosed {
		t.Errorf("after ResetAll, agent-a should be closed, got %v", store.Get("agent-a").State())
	}
	if store.Get("agent-b").State() != CircuitClosed {
		t.Errorf("after ResetAll, agent-b should be closed, got %v", store.Get("agent-b").State())
	}
}

func TestAgentCircuitBreakerStore_StatusEmpty(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	status := store.Status()
	if status == nil {
		t.Fatal("Status should not return nil")
	}
	if len(status) != 0 {
		t.Errorf("empty store should return empty status, got %d entries", len(status))
	}
}

// ── validateAgentRun / reportAgentOutcome Tests ─────────────────────────────

func TestValidateAgentRun_NilStore(t *testing.T) {
	if err := validateAgentRun(nil, "agent"); err != nil {
		t.Errorf("nil store should return nil, got %v", err)
	}
}

func TestValidateAgentRun_Allowed(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	if err := validateAgentRun(store, "good-agent"); err != nil {
		t.Errorf("fresh agent should be allowed, got %v", err)
	}
}

func TestValidateAgentRun_Blocked(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("bad-agent")
	err := validateAgentRun(store, "bad-agent")
	if err == nil {
		t.Fatal("expected error for blocked agent")
	}
	if err.Error() != `agent "bad-agent" circuit breaker is open (1 consecutive failures, cooldown 5m0s)` {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestReportAgentOutcome_NilStore(_ *testing.T) {
	// Should not panic
	reportAgentOutcome(nil, "agent", true)
	reportAgentOutcome(nil, "agent", false)
}

func TestReportAgentOutcome_Success(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	reportAgentOutcome(store, "agent", true)
	cb := store.Get("agent")
	if cb.FailureCount() != 0 {
		t.Errorf("success should keep failure count at 0, got %d", cb.FailureCount())
	}
	if cb.State() != CircuitClosed {
		t.Errorf("success should keep state closed, got %v", cb.State())
	}
}

func TestReportAgentOutcome_Failure(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 3})
	reportAgentOutcome(store, "agent", false)
	cb := store.Get("agent")
	if cb.FailureCount() != 1 {
		t.Errorf("failure should increment count, got %d", cb.FailureCount())
	}
}

func TestReportAgentOutcome_OpensOnThreshold(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 2})
	reportAgentOutcome(store, "agent", false)
	reportAgentOutcome(store, "agent", false)
	cb := store.Get("agent")
	if cb.State() != CircuitOpen {
		t.Errorf("2 failures with threshold=2 should open circuit, got %v", cb.State())
	}
}
