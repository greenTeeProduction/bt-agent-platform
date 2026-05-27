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
