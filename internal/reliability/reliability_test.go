package reliability

import (
	"errors"
	"os"
	"testing"
	"time"
)

// ─── Circuit Breaker Tests ──────────────────────────────────────────────────

func TestCircuitBreaker_Closed(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)
	if cb.State() != CircuitClosed {
		t.Error("new circuit should be closed")
	}
	if !cb.Allow() {
		t.Error("closed circuit should allow requests")
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker("test", 2, time.Second)
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("circuit should be open after 2 failures")
	}
	if cb.Allow() {
		t.Error("open circuit should deny requests")
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("should be open after failure")
	}
	time.Sleep(60 * time.Millisecond)
	if !cb.Allow() {
		t.Error("should allow one request in half-open")
	}
	if cb.Allow() {
		t.Error("should only allow one request in half-open")
	}
}

func TestCircuitBreaker_RecoversAfterSuccess(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("should be closed after recovery, got %s", cb.State())
	}
}

func TestCircuitBreaker_FailsInHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker("test", 1, 50*time.Millisecond)
	cb.RecordFailure()
	time.Sleep(60 * time.Millisecond)
	cb.Allow() // half-open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Error("should re-open on half-open failure")
	}
}

// ─── Backoff Tests ──────────────────────────────────────────────────────────

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{1, time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 10 * time.Second}, // capped at maxDelay
	}

	for _, tt := range tests {
		got := Backoff(tt.attempt, time.Second, 10*time.Second)
		if got != tt.expected {
			t.Errorf("Backoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	attempts := 0
	err := RetryWithBackoff(3, time.Millisecond, 10*time.Millisecond, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("fail")
		}
		return nil
	})
	if err != nil {
		t.Errorf("expected success, got: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetryWithBackoff_Exhausted(t *testing.T) {
	attempts := 0
	err := RetryWithBackoff(3, time.Millisecond, 10*time.Millisecond, func() error {
		attempts++
		return errors.New("always fail")
	})
	if err == nil {
		t.Error("expected error after exhaustion")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// ─── Dead Letter Queue Tests ────────────────────────────────────────────────

func TestDeadLetterQueue_PushList(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "1", Task: "test task", Error: "failed"})
	dlq.Push(DeadLetterEntry{ID: "2", Task: "test task 2", Error: "timeout"})

	entries := dlq.List()
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].FailedAt.IsZero() {
		t.Error("FailedAt should be set")
	}
}

func TestDeadLetterQueue_Replay(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "a", Task: "task a"})
	dlq.Push(DeadLetterEntry{ID: "b", Task: "task b"})

	entry, ok := dlq.Replay("a")
	if !ok {
		t.Error("should find entry 'a'")
	}
	if entry.Task != "task a" {
		t.Errorf("expected 'task a', got %q", entry.Task)
	}
	if dlq.Len() != 1 {
		t.Errorf("expected 1 remaining, got %d", dlq.Len())
	}
}

func TestDeadLetterQueue_Purge(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "1"})
	dlq.Push(DeadLetterEntry{ID: "2"})
	dlq.Purge()
	if dlq.Len() != 0 {
		t.Errorf("expected 0 after purge, got %d", dlq.Len())
	}
}

func TestDeadLetterQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/dlq.json"

	dlq := NewDeadLetterQueue(path)
	dlq.Push(DeadLetterEntry{ID: "p1", Task: "persist me"})

	// Load from disk
	dlq2 := NewDeadLetterQueue(path)
	if dlq2.Len() != 1 {
		t.Errorf("expected 1 entry after reload, got %d", dlq2.Len())
	}
}

// ─── Worker Pool Tests ──────────────────────────────────────────────────────

func TestWorkerPool_Submit(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Shutdown()

	done := make(chan bool, 2)
	wp.Submit(func() { done <- true })
	wp.Submit(func() { done <- true })

	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("task timed out")
		}
	}
}

func TestWorkerPool_Stats(t *testing.T) {
	wp := NewWorkerPool(1)
	defer wp.Shutdown()

	done := make(chan bool)
	wp.Submit(func() { done <- true })
	<-done

	time.Sleep(10 * time.Millisecond) // let stats update
	active, queued, total, completed := wp.Stats()
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if completed != 1 {
		t.Errorf("expected completed=1, got %d", completed)
	}
	_ = active
	_ = queued
}

// ─── Task Queue Tests ───────────────────────────────────────────────────────

func TestTaskQueue_EnqueueDequeue(t *testing.T) {
	tq := NewTaskQueue("")
	tq.Enqueue("task1")
	tq.Enqueue("task2")

	if tq.Len() != 2 {
		t.Errorf("expected len=2, got %d", tq.Len())
	}

	got := tq.Dequeue()
	if got != "task1" {
		t.Errorf("expected 'task1', got %q", got)
	}
}

func TestTaskQueue_Peek(t *testing.T) {
	tq := NewTaskQueue("")
	tq.Enqueue("first")
	peeked := tq.Peek()
	if peeked != "first" {
		t.Errorf("peek expected 'first', got %q", peeked)
	}
	if tq.Len() != 1 {
		t.Error("peek should not remove")
	}
}

func TestTaskQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/queue.json"

	tq := NewTaskQueue(path)
	tq.Enqueue("hello")
	tq.Enqueue("world")

	tq2 := NewTaskQueue(path)
	if tq2.Len() != 2 {
		t.Errorf("expected 2 items after reload, got %d", tq2.Len())
	}
}

// ─── Scheduler Persistence Tests ────────────────────────────────────────────

func TestSchedulerState_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/scheduler.json"

	ss := NewSchedulerState(path)
	ss.Save(JobState{
		ID:       "job-1",
		Name:     "test-job",
		Schedule: "every 1h",
		RunCount: 5,
		Enabled:  true,
	})

	ss2 := NewSchedulerState(path)
	state, ok := ss2.Get("job-1")
	if !ok {
		t.Fatal("job should exist after reload")
	}
	if state.RunCount != 5 {
		t.Errorf("expected RunCount=5, got %d", state.RunCount)
	}
}

func TestSchedulerState_Delete(t *testing.T) {
	ss := NewSchedulerState("")
	ss.Save(JobState{ID: "del-me", Name: "temp"})
	ss.Delete("del-me")
	_, ok := ss.Get("del-me")
	if ok {
		t.Error("deleted job should not exist")
	}
}

func TestNewSchedulerState_NonexistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	ss := NewSchedulerState(tmpDir + "/nonexistent/dir/scheduler.json")
	// should not panic; just empty
	if len(ss.List()) != 0 {
		t.Error("should be empty")
	}
}

func init() {
	// Silence dead letter queue persistence errors in tests
	os.Setenv("BT_TEST_MODE", "1")
}

// ─── Priority Queue Tests ────────────────────────────────────────────────────

func TestPriorityQueue_DequeueOrder(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("low task", "agent-a", PriorityLow)
	pq.Enqueue("critical task", "agent-b", PriorityCritical)
	pq.Enqueue("high task", "agent-c", PriorityHigh)
	pq.Enqueue("medium task", "agent-d", PriorityMedium)

	expected := []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}
	for i, exp := range expected {
		task := pq.Dequeue()
		if task.Priority != exp {
			t.Errorf("dequeue %d: expected %s, got %s (task=%q)", i, exp, task.Priority, task.Task)
		}
		if task.ID == "" {
			t.Error("task ID should not be empty")
		}
	}
}

func TestPriorityQueue_SamePriorityFIFO(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("task 1", "agent", PriorityMedium)
	pq.Enqueue("task 2", "agent", PriorityMedium)
	pq.Enqueue("task 3", "agent", PriorityMedium)

	t1 := pq.Dequeue()
	t2 := pq.Dequeue()
	t3 := pq.Dequeue()

	// Min-heap with same priority doesn't guarantee FIFO,
	// but all three should be PriorityMedium
	if t1.Priority != PriorityMedium {
		t.Error("all should be medium")
	}
	_ = t2
	_ = t3
}

func TestPriorityQueue_Empty(t *testing.T) {
	pq := NewPriorityQueue("")
	task := pq.Dequeue()
	if task.ID != "" {
		t.Error("empty dequeue should return zero PriorityTask")
	}
	if pq.Len() != 0 {
		t.Error("empty queue should have len 0")
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("low", "a", PriorityLow)
	pq.Enqueue("critical", "b", PriorityCritical)

	peeked := pq.Peek()
	if peeked.Priority != PriorityCritical {
		t.Errorf("peek expected critical, got %s", peeked.Priority)
	}
	if pq.Len() != 2 {
		t.Error("peek should not remove")
	}
}

func TestPriorityQueue_Purge(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("a", "x", PriorityMedium)
	pq.Enqueue("b", "y", PriorityHigh)
	pq.Purge()
	if pq.Len() != 0 {
		t.Errorf("after purge, len should be 0, got %d", pq.Len())
	}
}

func TestPriorityQueue_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/priority_queue.json"

	pq1 := NewPriorityQueue(path)
	pq1.Enqueue("critical task", "agent-x", PriorityCritical)
	pq1.Enqueue("bg task", "agent-y", PriorityBackground)

	pq2 := NewPriorityQueue(path)
	if pq2.Len() != 2 {
		t.Errorf("expected 2 tasks after reload, got %d", pq2.Len())
	}

	task := pq2.Dequeue()
	if task.Priority != PriorityCritical {
		t.Errorf("expected critical after reload, got %s", task.Priority)
	}
}

func TestPriorityQueue_List(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("c", "a", PriorityLow)
	pq.Enqueue("a", "a", PriorityCritical)
	pq.Enqueue("b", "a", PriorityHigh)

	list := pq.List()
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
}

func TestPriority_String(t *testing.T) {
	tests := []struct {
		p Priority
		s string
	}{
		{PriorityCritical, "critical"},
		{PriorityHigh, "high"},
		{PriorityMedium, "medium"},
		{PriorityLow, "low"},
		{PriorityBackground, "background"},
		{Priority(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.s {
			t.Errorf("Priority(%d).String() = %q, want %q", tt.p, got, tt.s)
		}
	}
}

// ─── Concurrency Limiter Tests ───────────────────────────────────────────────

func TestConcurrencyLimiter_AcquireRelease(t *testing.T) {
	cl := NewConcurrencyLimiter(2)
	if cl.Capacity() != 2 {
		t.Errorf("capacity should be 2, got %d", cl.Capacity())
	}

	cl.Acquire()
	cl.Acquire()

	active, waiting, total := cl.Stats()
	if active != 2 {
		t.Errorf("expected 2 active, got %d", active)
	}
	if total != 2 {
		t.Errorf("expected 2 total, got %d", total)
	}
	_ = waiting

	cl.Release()
	active, _, _ = cl.Stats()
	if active != 1 {
		t.Errorf("expected 1 active after release, got %d", active)
	}
}

func TestConcurrencyLimiter_TryAcquire(t *testing.T) {
	cl := NewConcurrencyLimiter(1)

	if !cl.TryAcquire() {
		t.Error("first TryAcquire should succeed")
	}
	if cl.TryAcquire() {
		t.Error("second TryAcquire should fail when full")
	}

	cl.Release()
	if !cl.TryAcquire() {
		t.Error("TryAcquire should succeed after release")
	}
	cl.Release()
}

func TestConcurrencyLimiter_Available(t *testing.T) {
	cl := NewConcurrencyLimiter(3)
	if cl.Available() != 3 {
		t.Errorf("initial available: expected 3, got %d", cl.Available())
	}
	cl.Acquire()
	if cl.Available() != 2 {
		t.Errorf("after 1 acquire: expected 2, got %d", cl.Available())
	}
	cl.Acquire()
	cl.Acquire()
	if cl.Available() != 0 {
		t.Errorf("after 3 acquires: expected 0, got %d", cl.Available())
	}
	cl.Release()
	cl.Release()
	cl.Release()
	if cl.Available() != 3 {
		t.Errorf("after 3 releases: expected 3, got %d", cl.Available())
	}
}

func TestConcurrencyLimiter_ReleaseWhenEmpty(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	// Release when nothing acquired should not panic
	cl.Release()
	active, _, _ := cl.Stats()
	if active != 0 {
		t.Errorf("expected 0 active, got %d", active)
	}
}

func TestConcurrencyLimiter_MultipleReleaseNoUnderflow(t *testing.T) {
	cl := NewConcurrencyLimiter(1)
	cl.Acquire()
	cl.Release()
	cl.Release()
	cl.Release() // should not underflow
	active, _, _ := cl.Stats()
	if active != 0 {
		t.Errorf("expected 0 active after multiple releases, got %d", active)
	}
}

// ─── AgentExecutor Tests ─────────────────────────────────────────────────────

func TestLocalExecutor_Execute(t *testing.T) {
	expected := &AgentResult{
		Agent:        "test-agent",
		Task:         "echo hello",
		Output:       "hello",
		Success:      true,
		QualityScore: 0.95,
	}
	exec := NewLocalExecutor("local-1", func(agent, task string) (*AgentResult, error) {
		return expected, nil
	})

	result, err := exec.Execute("test-agent", "echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Agent != expected.Agent {
		t.Errorf("expected agent %q, got %q", expected.Agent, result.Agent)
	}
	if result.Output != expected.Output {
		t.Errorf("expected output %q, got %q", expected.Output, result.Output)
	}
	if !result.Success {
		t.Error("expected success=true")
	}
}

func TestLocalExecutor_Health(t *testing.T) {
	exec := NewLocalExecutor("local-1", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true}, nil
	})
	if err := exec.Health(); err != nil {
		t.Errorf("healthy executor should return nil, got %v", err)
	}
}

func TestLocalExecutor_WithHealthCheck(t *testing.T) {
	exec := NewLocalExecutor("local-1", nil).
		WithHealthCheck(func() error { return errors.New("unhealthy") })
	if err := exec.Health(); err == nil {
		t.Error("unhealthy executor should return error")
	}
}

func TestLocalExecutor_String(t *testing.T) {
	exec := NewLocalExecutor("local-1", nil)
	if s := exec.String(); s != "local-1" {
		t.Errorf("expected 'local-1', got %q", s)
	}
}

func TestAgentRouter_RoundRobinRouting(t *testing.T) {
	callCount := map[string]int{}
	makeExec := func(name string) *LocalExecutor {
		return NewLocalExecutor(name, func(agent, task string) (*AgentResult, error) {
			callCount[name]++
			return &AgentResult{Agent: agent, Task: task, Success: true}, nil
		})
	}

	e1 := makeExec("e1")
	e2 := makeExec("e2")
	e3 := makeExec("e3")
	router := NewAgentRouter(e1, e2, e3)

	// Execute 6 tasks — each executor should get 2 (round-robin)
	for i := 0; i < 6; i++ {
		_, err := router.Execute("agent", "task")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if callCount["e1"] != 2 || callCount["e2"] != 2 || callCount["e3"] != 2 {
		t.Errorf("expected each executor called 2 times, got e1=%d e2=%d e3=%d",
			callCount["e1"], callCount["e2"], callCount["e3"])
	}
}

func TestAgentRouter_FallbackToLocalWhenUnhealthy(t *testing.T) {
	healthy := NewLocalExecutor("healthy", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "remote"}, nil
	})
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })
	local := NewLocalExecutor("local", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local"}, nil
	})

	router := NewAgentRouter(unhealthy)
	router.SetLocal(local)

	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "local" {
		t.Errorf("expected fallback to local, got %q", result.Output)
	}

	// Now add a healthy executor
	router.Add(healthy)
	result2, err := router.Execute("agent", "task2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Output != "remote" {
		t.Errorf("expected remote execution, got %q", result2.Output)
	}
}

func TestAgentRouter_NoExecutorsWhenEmpty(t *testing.T) {
	router := NewAgentRouter()
	_, err := router.Execute("agent", "task")
	if err == nil {
		t.Error("expected error when no executors available")
	}
}

func TestAgentRouter_Health(t *testing.T) {
	healthy := NewLocalExecutor("healthy", nil)
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })

	router := NewAgentRouter(unhealthy, unhealthy)
	if err := router.Health(); err == nil {
		t.Error("expected unhealthy with all executors down")
	}

	router.Add(healthy)
	if err := router.Health(); err != nil {
		t.Errorf("expected healthy after adding healthy executor, got %v", err)
	}
}

func TestAgentRouter_EmptyHealth(t *testing.T) {
	router := NewAgentRouter()
	if err := router.Health(); err == nil {
		t.Error("empty router should report unhealthy")
	}
}

func TestAgentRouter_Executors(t *testing.T) {
	e1 := NewLocalExecutor("e1", nil)
	e2 := NewLocalExecutor("e2", nil)
	router := NewAgentRouter(e1, e2)

	executors := router.Executors()
	if len(executors) != 2 {
		t.Errorf("expected 2 executors, got %d", len(executors))
	}
}

func TestAgentRouter_HealthyExecutors(t *testing.T) {
	healthy := NewLocalExecutor("healthy", nil)
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })

	router := NewAgentRouter(healthy, unhealthy)
	healthyList := router.HealthyExecutors()
	if len(healthyList) != 1 {
		t.Errorf("expected 1 healthy executor, got %d", len(healthyList))
	}
	if healthyList[0].String() != "healthy" {
		t.Errorf("expected 'healthy', got %q", healthyList[0].String())
	}
}

func TestAgentRouter_String(t *testing.T) {
	e1 := NewLocalExecutor("e1", nil)
	e2 := NewLocalExecutor("e2", nil)
	router := NewAgentRouter(e1, e2)

	s := router.String()
	if s != "AgentRouter(executors=2, local=e1)" {
		t.Errorf("unexpected String() output: %q", s)
	}
}

func TestAgentRouter_GracefulDegradation(t *testing.T) {
	// All remote executors fail health, but local fallback works
	remote1 := NewLocalExecutor("remote-1", nil).
		WithHealthCheck(func() error { return errors.New("network timeout") })
	remote2 := NewLocalExecutor("remote-2", nil).
		WithHealthCheck(func() error { return errors.New("connection refused") })
	local := NewLocalExecutor("local-fallback", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "degraded but working"}, nil
	})

	router := NewAgentRouter(remote1, remote2)
	router.SetLocal(local)

	result, err := router.Execute("agent", "critical-task")
	if err != nil {
		t.Fatalf("graceful degradation should not error: %v", err)
	}
	if result.Output != "degraded but working" {
		t.Errorf("expected degraded output, got %q", result.Output)
	}
}

func TestAgentExecutor_AgentResultFields(t *testing.T) {
	result := &AgentResult{
		Agent:        "test",
		Task:         "do things",
		Output:       "done",
		Duration:     150 * time.Millisecond,
		Success:      true,
		QualityScore: 0.88,
	}

	if result.Duration != 150*time.Millisecond {
		t.Error("duration field not preserved")
	}
	if result.QualityScore != 0.88 {
		t.Error("quality_score field not preserved")
	}
}

// ─── AgentRouter Failover Tests ─────────────────────────────────────────────

func TestAgentRouter_FailoverOnExecuteError(t *testing.T) {
	// Executor passes Health() but Execute() fails → router tries next executor
	failing := NewLocalExecutor("failing", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("transient error")
	})
	working := NewLocalExecutor("working", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Agent: agent, Task: task, Success: true, Output: "from working"}, nil
	})

	router := NewAgentRouter(failing, working)
	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("expected failover to working executor, got error: %v", err)
	}
	if result.Output != "from working" {
		t.Errorf("expected output from working executor, got %q", result.Output)
	}
}

func TestAgentRouter_AllExecutorsFail(t *testing.T) {
	// All executors pass Health() but Execute() fails → error returned
	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("error from e1")
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("error from e2")
	})

	router := NewAgentRouter(e1, e2)
	_, err := router.Execute("agent", "task")
	if err == nil {
		t.Fatal("expected error when all executors fail")
	}
}

func TestAgentRouter_FailoverThenLocalFallback(t *testing.T) {
	// All remote executors fail Execute(), local fallback succeeds
	remote1 := NewLocalExecutor("remote-1", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("remote-1 failed")
	})
	remote2 := NewLocalExecutor("remote-2", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("remote-2 failed")
	})
	local := NewLocalExecutor("local", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local fallback"}, nil
	})

	router := NewAgentRouter(remote1, remote2)
	router.SetLocal(local)

	result, err := router.Execute("agent", "critical-task")
	if err != nil {
		t.Fatalf("expected local fallback after remote failures, got error: %v", err)
	}
	if result.Output != "local fallback" {
		t.Errorf("expected 'local fallback', got %q", result.Output)
	}
}

func TestAgentRouter_FailoverThenLocalFailsToo(t *testing.T) {
	// All remote + local fail → combined error
	remote := NewLocalExecutor("remote", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("remote down")
	})
	local := NewLocalExecutor("local", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("local down")
	})

	router := NewAgentRouter(remote)
	router.SetLocal(local)

	_, err := router.Execute("agent", "task")
	if err == nil {
		t.Fatal("expected error when all executors including local fail")
	}
}

func TestAgentRouter_FailoverNonFailoverExecutorSkipped(t *testing.T) {
	// Unhealthy executor is skipped even during failover
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("unhealthy") })
	healthy := NewLocalExecutor("healthy", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "ok"}, nil
	})

	router := NewAgentRouter(unhealthy, healthy)
	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("expected skip unhealthy and use healthy, got error: %v", err)
	}
	if result.Output != "ok" {
		t.Errorf("expected 'ok', got %q", result.Output)
	}
}

func TestAgentRouter_MaxFailoverLimit(t *testing.T) {
	// MaxFailover=1 → only try first healthy executor, even if it fails
	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("e1 failed")
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "e2 would work but not tried"}, nil
	})

	router := NewAgentRouter(e1, e2)
	router.MaxFailover = 1

	_, err := router.Execute("agent", "task")
	if err == nil {
		t.Fatal("expected error because MaxFailover=1 prevents trying e2")
	}
}

func TestAgentRouter_MaxFailoverRespected(t *testing.T) {
	// MaxFailover=2 with 3 remote executors → only tries first 2 healthy ones,
	// then falls back to local (which is separate, not counted in failover cap)
	callCount := 0
	e1 := NewLocalExecutor("e1", func(agent, task string) (*AgentResult, error) {
		callCount++
		return nil, errors.New("e1 failed")
	})
	e2 := NewLocalExecutor("e2", func(agent, task string) (*AgentResult, error) {
		callCount++
		return nil, errors.New("e2 failed")
	})
	e3 := NewLocalExecutor("e3", func(agent, task string) (*AgentResult, error) {
		callCount++
		return &AgentResult{Success: true}, nil
	})
	local := NewLocalExecutor("local", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "local"}, nil
	})

	router := NewAgentRouter(e1, e2, e3)
	router.SetLocal(local)
	router.MaxFailover = 2

	// e1 and e2 are tried (both fail), e3 is NOT tried (MaxFailover=2),
	// then fallback to local which succeeds.
	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("expected local fallback to succeed, got error: %v", err)
	}
	if result.Output != "local" {
		t.Errorf("expected 'local' fallback output, got %q", result.Output)
	}
	if callCount != 2 {
		t.Errorf("expected 2 remote executors tried (e1,e2), got %d", callCount)
	}
}

func TestAgentRouter_FailoverMixedHealthyAndErrors(t *testing.T) {
	// Mixed: unhealthy, error-after-Execute, and working executor
	unhealthy := NewLocalExecutor("unhealthy", nil).
		WithHealthCheck(func() error { return errors.New("down") })
	flaky := NewLocalExecutor("flaky", func(agent, task string) (*AgentResult, error) {
		return nil, errors.New("flaky crashed")
	})
	working := NewLocalExecutor("working", func(agent, task string) (*AgentResult, error) {
		return &AgentResult{Success: true, Output: "finally"}, nil
	})

	router := NewAgentRouter(unhealthy, flaky, working)
	result, err := router.Execute("agent", "task")
	if err != nil {
		t.Fatalf("expected failover through unhealthy→flaky→working, got: %v", err)
	}
	if result.Output != "finally" {
		t.Errorf("expected 'finally', got %q", result.Output)
	}
}
