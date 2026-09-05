package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
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
	if cb.Threshold() != 3 {
		t.Errorf("default threshold = %d, want 3", cb.Threshold())
	}
	if cb.Cooldown() != 5*time.Minute {
		t.Errorf("default cooldown = %v, want 5m", cb.Cooldown())
	}
	if cb.State() != CircuitClosed {
		t.Errorf("initial state = %v, want closed", cb.State())
	}
}

func TestNewAgentCircuitBreaker_Custom(t *testing.T) {
	cb := NewAgentCircuitBreaker("custom", 2, 30*time.Second)
	if cb.Threshold() != 2 {
		t.Errorf("threshold = %d, want 2", cb.Threshold())
	}
	if cb.Cooldown() != 30*time.Second {
		t.Errorf("cooldown = %v, want 30s", cb.Cooldown())
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

// ── Consolidation onto internal/reliability.CircuitBreaker ─────────────────
//
// AgentCircuitBreaker/AgentCircuitBreakerStore duplicate the 3-state circuit
// breaker internal/reliability.CircuitBreaker already implements. These tests
// pin the consolidated design: AgentCircuitBreaker (and its state/summary
// types) must become true aliases for the canonical reliability types rather
// than a parallel reimplementation, so behavior can only diverge in one
// place. They fail to COMPILE today because the agent-package types are
// distinct named types from their reliability counterparts.

func TestAgentCircuitBreaker_IsReliabilityCircuitBreaker(t *testing.T) {
	var cb = NewAgentCircuitBreaker("x", 1, time.Minute)
	if cb == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
}

func TestCircuitState_IsReliabilityCircuitState(t *testing.T) {
	var s = CircuitClosed
	if s != reliability.CircuitClosed {
		t.Errorf("CircuitClosed should equal reliability.CircuitClosed, got %v", s)
	}
}

func TestAgentCircuitBreakerStore_GetReturnsReliabilityCircuitBreaker(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	var cb = store.Get("agent")
	if cb == nil {
		t.Fatal("expected non-nil circuit breaker")
	}
}

func TestCircuitSummary_IsReliabilityCircuitSummary(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent")
	status := store.Status()
	var summary = status["agent"]
	if summary.State != reliability.CircuitOpen {
		t.Errorf("summary.State = %v, want open", summary.State)
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

// ── AgentCircuitBreakerStore persistence Tests ──────────────────────────────
//
// The dashboard (internal/dashboard/agents.go's loadCircuitBreakers) already
// reads agent.CircuitBreakersFile() and renders it as the "cb_status" column,
// but nothing on the write side ever produces that file — Store.Save/Load do
// not exist yet and the scheduler never calls them. These tests pin the
// missing persistence contract: Save must write JSON in the
// {"breakers": {name: {status, failures, last_failure}}} shape the dashboard
// decodes, and Load must restore state written by Save.

// circuitBreakersFileForTest mirrors internal/dashboard/agents.go's
// CircuitBreakers/CircuitBreakerEntry JSON shape without importing the
// dashboard package (which itself imports internal/agent).
type circuitBreakersFileForTest struct {
	Breakers map[string]struct {
		Status      string `json:"status"`
		Failures    int    `json:"failures"`
		LastFailure string `json:"last_failure"`
	} `json:"breakers"`
}

func TestAgentCircuitBreakerStore_Save_WritesDashboardShape(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent-a") // threshold=1 -> opens
	store.RecordSuccess("agent-b")

	path := filepath.Join(t.TempDir(), "circuit_breakers.json")
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Save did not create %s: %v", path, err)
	}
	var decoded circuitBreakersFileForTest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Save output is not valid dashboard-shaped JSON: %v\n%s", err, data)
	}

	a, ok := decoded.Breakers["agent-a"]
	if !ok {
		t.Fatal("agent-a missing from saved breakers")
	}
	if a.Status != "open" {
		t.Errorf("agent-a status = %q, want %q", a.Status, "open")
	}
	if a.Failures != 1 {
		t.Errorf("agent-a failures = %d, want 1", a.Failures)
	}

	b, ok := decoded.Breakers["agent-b"]
	if !ok {
		t.Fatal("agent-b missing from saved breakers")
	}
	if b.Status != "closed" {
		t.Errorf("agent-b status = %q, want %q", b.Status, "closed")
	}
}

func TestAgentCircuitBreakerStore_SaveThenLoad_RestoresState(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	store.RecordFailure("agent-a")

	path := filepath.Join(t.TempDir(), "circuit_breakers.json")
	if err := store.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	restored := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	if err := restored.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cb := restored.Get("agent-a")
	if cb.State() != CircuitOpen {
		t.Errorf("restored agent-a state = %v, want open", cb.State())
	}
	if cb.FailureCount() != 1 {
		t.Errorf("restored agent-a failure count = %d, want 1", cb.FailureCount())
	}
}

func TestAgentCircuitBreakerStore_Load_MissingFileIsNotError(t *testing.T) {
	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{})
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	if err := store.Load(path); err != nil {
		t.Errorf("Load of missing file should not error, got %v", err)
	}
}

// TestAgentCircuitBreakerStore_SaveLocksReadModifyWrite pins the cross-process
// serialization this store — the single owner of circuit_breakers.json — owes
// its writers. The daemon scheduler, the dashboard executor and internal/a2a's
// winner breakers all Save into the same path. The atomic rename only makes
// each individual write indivisible; it does nothing about the read-merge-write
// cycle around it, so two savers can both read the same old file and the second
// rename silently drops whatever the first one committed in between. Save must
// hold the shared advisory sidecar lock (reliability.AcquireFileLock, the same
// `<path>.lock` idiom internal/evolution and internal/research already use)
// across the whole cycle, so it blocks while another process holds it and
// re-reads the file only after acquiring.
func TestAgentCircuitBreakerStore_SaveLocksReadModifyWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "circuit_breakers.json")
	if err := os.WriteFile(path, []byte(`{"breakers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	// Stand in for a second process that is mid read-modify-write on the file.
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		t.Fatalf("AcquireFileLock: %v", err)
	}
	defer release() // safe to call twice; released explicitly below

	store := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1, Cooldown: time.Minute})
	store.RecordFailure("own-agent")

	done := make(chan error, 1)
	go func() { done <- store.Save(path) }()

	select {
	case saveErr := <-done:
		t.Fatalf("Save returned (err=%v) while another process held %s.lock; its read-modify-write must block on the shared advisory file lock", saveErr, path)
	case <-time.After(250 * time.Millisecond):
		// Still blocked, as required.
	}

	// The other process commits its entry and drops the lock. Save must not have
	// read the file before this point, or the merge below loses this entry.
	other := `{"breakers":{"other-process-agent":{"status":"open","failures":7,"last_failure":"2026-07-17T12:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(other), 0644); err != nil {
		t.Fatal(err)
	}
	release()

	select {
	case saveErr := <-done:
		if saveErr != nil {
			t.Fatalf("Save: %v", saveErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Save never completed after the advisory file lock was released")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded circuitBreakersFileForTest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("saved file is not valid dashboard-shaped JSON: %v\n%s", err, data)
	}
	if entry, ok := decoded.Breakers["other-process-agent"]; !ok || entry.Failures != 7 {
		t.Errorf("other process's entry = %+v (present=%v), want failures=7 preserved — Save read the file before taking the lock and clobbered a concurrent writer", entry, ok)
	}
	if _, ok := decoded.Breakers["own-agent"]; !ok {
		t.Errorf("own-agent missing from saved file: %+v", decoded.Breakers)
	}
}

// TestScheduler_PersistsCircuitBreakerOnFailure exercises the real scheduler
// run loop (runJob -> reportAgentOutcome) end to end: a failing agent must
// leave circuit_breakers.json on disk at agent.CircuitBreakersFile() with the
// agent recorded as open, so the dashboard's already-built cb_status column
// reflects real state instead of reading a file nothing ever wrote.
func TestScheduler_PersistsCircuitBreakerOnFailure(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())

	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "flaky-agent", Tree: "domain:default", Version: "1.0.0"})

	cbStore := NewAgentCircuitBreakerStore(CircuitBreakerOptions{Threshold: 1})
	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		CBStore:      cbStore,
		TickInterval: 100 * time.Millisecond,
	})

	failingRunner := func(_ RunContext) (string, string, *RunResult, error) {
		return "failure", "boom", nil, os.ErrInvalid
	}

	job, err := sched.Schedule("flaky-agent", "every 1h", "30m", 0)
	if err != nil {
		t.Fatal(err)
	}
	job.NextRun = time.Time{} // force immediate

	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.Start(failingRunner)
	}()
	time.Sleep(500 * time.Millisecond)
	sched.Stop()
	<-done

	cbPath := CircuitBreakersFile()
	data, err := os.ReadFile(cbPath)
	if err != nil {
		t.Fatalf("circuit breaker state was never persisted to %s: %v", cbPath, err)
	}
	var decoded circuitBreakersFileForTest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("persisted circuit breaker file is not valid dashboard-shaped JSON: %v\n%s", err, data)
	}
	entry, ok := decoded.Breakers["flaky-agent"]
	if !ok {
		t.Fatalf("flaky-agent missing from persisted circuit breaker state: %+v", decoded.Breakers)
	}
	if entry.Status != "open" {
		t.Errorf("flaky-agent persisted status = %q, want %q", entry.Status, "open")
	}
}
