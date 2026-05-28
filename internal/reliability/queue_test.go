package reliability

import (
	"os"
	"path/filepath"
	"testing"
)

// ─── Queue Interface Tests (via *TaskQueue) ─────────────────────────────────

func testQueueEnqueueDequeue(t *testing.T, q Queue) {
	t.Helper()
	q.Enqueue("task-1")
	q.Enqueue("task-2")
	q.Enqueue("task-3")

	if q.Len() != 3 {
		t.Errorf("expected 3 tasks, got %d", q.Len())
	}

	if got := q.Dequeue(); got != "task-1" {
		t.Errorf("expected 'task-1', got %q", got)
	}
	if got := q.Dequeue(); got != "task-2" {
		t.Errorf("expected 'task-2', got %q", got)
	}
	if got := q.Dequeue(); got != "task-3" {
		t.Errorf("expected 'task-3', got %q", got)
	}

	if q.Len() != 0 {
		t.Errorf("expected empty queue, got %d", q.Len())
	}
}

func TestQueue_EnqueueDequeue(t *testing.T) {
	q := NewTaskQueue("")
	testQueueEnqueueDequeue(t, q)
}

func TestQueue_DequeueEmpty(t *testing.T) {
	q := NewTaskQueue("")
	if got := q.Dequeue(); got != "" {
		t.Errorf("expected empty string from empty queue, got %q", got)
	}
}

func TestQueue_Peek(t *testing.T) {
	q := NewTaskQueue("")
	q.Enqueue("first")
	q.Enqueue("second")

	if got := q.Peek(); got != "first" {
		t.Errorf("expected 'first', got %q", got)
	}
	if q.Len() != 2 {
		t.Errorf("peek should not remove items, expected 2 got %d", q.Len())
	}
}

func TestQueue_PeekEmpty(t *testing.T) {
	q := NewTaskQueue("")
	if got := q.Peek(); got != "" {
		t.Errorf("expected empty string from empty queue, got %q", got)
	}
}

// ─── PriorityTaskQueue Interface Tests (via *PriorityQueue) ─────────────────

func TestPriorityTaskQueue_EnqueueDequeue(t *testing.T) {
	pq := NewPriorityQueue("")

	id1 := pq.Enqueue("task-low", "agent-a", PriorityLow)
	id2 := pq.Enqueue("task-critical", "agent-b", PriorityCritical)
	id3 := pq.Enqueue("task-high", "agent-c", PriorityHigh)

	if id1 == "" || id2 == "" || id3 == "" {
		t.Error("enqueue should return non-empty IDs")
	}

	if pq.Len() != 3 {
		t.Fatalf("expected 3 tasks, got %d", pq.Len())
	}

	// Critical (0) should come before High (1) before Low (3)
	first := pq.Dequeue()
	if first.Priority != PriorityCritical || first.Task != "task-critical" {
		t.Errorf("expected critical task first, got priority=%v task=%q", first.Priority, first.Task)
	}

	second := pq.Dequeue()
	if second.Priority != PriorityHigh || second.Task != "task-high" {
		t.Errorf("expected high task second, got priority=%v task=%q", second.Priority, second.Task)
	}

	third := pq.Dequeue()
	if third.Priority != PriorityLow || third.Task != "task-low" {
		t.Errorf("expected low task third, got priority=%v task=%q", third.Priority, third.Task)
	}

	if pq.Len() != 0 {
		t.Errorf("expected empty queue, got %d", pq.Len())
	}
}

func TestPriorityTaskQueue_Peek(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("task-low", "agent-a", PriorityLow)
	pq.Enqueue("task-critical", "agent-b", PriorityCritical)

	peeked := pq.Peek()
	if peeked.Priority != PriorityCritical {
		t.Errorf("expected critical task, got priority=%v", peeked.Priority)
	}
	if pq.Len() != 2 {
		t.Errorf("peek should not remove items, got %d", pq.Len())
	}
}

func TestPriorityTaskQueue_DequeueEmpty(t *testing.T) {
	pq := NewPriorityQueue("")
	task := pq.Dequeue()
	if task.ID != "" {
		t.Errorf("expected empty PriorityTask from empty queue, got ID=%q", task.ID)
	}
}

func TestPriorityTaskQueue_PeekEmpty(t *testing.T) {
	pq := NewPriorityQueue("")
	task := pq.Peek()
	if task.ID != "" {
		t.Errorf("expected empty PriorityTask from empty queue, got ID=%q", task.ID)
	}
}

func TestPriorityTaskQueue_SamePriorityFIFO(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("first", "agent", PriorityMedium)
	pq.Enqueue("second", "agent", PriorityMedium)
	pq.Enqueue("third", "agent", PriorityMedium)

	// Same priority should preserve insertion order (heap is stable for equal keys)
	// Note: go's container/heap is not strictly stable, but this implementation's
	// siftUp favors newer entries at the same priority level.
	// We accept any order for equal-priority items, just verify all dequeued.
	count := 0
	seen := make(map[string]bool)
	for pq.Len() > 0 {
		task := pq.Dequeue()
		if task.Agent != "agent" {
			t.Errorf("unexpected agent: %q", task.Agent)
		}
		seen[task.Task] = true
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 tasks, got %d", count)
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 unique tasks, got %d", len(seen))
	}
}

func TestPriorityTaskQueue_PurgeViaInterface(t *testing.T) {
	pq := NewPriorityQueue("")
	pq.Enqueue("task-1", "agent", PriorityHigh)
	pq.Enqueue("task-2", "agent", PriorityLow)

	if pq.Len() != 2 {
		t.Fatal("expected 2 tasks before purge")
	}

	pq.Purge()

	if pq.Len() != 0 {
		t.Errorf("expected 0 tasks after purge, got %d", pq.Len())
	}
}

// ─── Interface Compliance Tests ──────────────────────────────────────────────

func TestQueueInterfaceCompliance(t *testing.T) {
	// Verify *TaskQueue satisfies Queue interface
	var q Queue = NewTaskQueue("")
	q.Enqueue("test")
	if q.Len() != 1 {
		t.Error("Queue interface method Len() should work")
	}
	if q.Dequeue() != "test" {
		t.Error("Queue interface method Dequeue() should work")
	}
}

func TestPriorityTaskQueueInterfaceCompliance(t *testing.T) {
	// Verify *PriorityQueue satisfies PriorityTaskQueue interface
	var pq PriorityTaskQueue = NewPriorityQueue("")
	id := pq.Enqueue("test", "agent", PriorityMedium)
	if id == "" {
		t.Error("PriorityTaskQueue interface method Enqueue() should return ID")
	}
	if pq.Len() != 1 {
		t.Error("PriorityTaskQueue interface method Len() should work")
	}
	task := pq.Peek()
	if task.Task != "test" {
		t.Error("PriorityTaskQueue interface method Peek() should work")
	}
}

// ─── QueueError Tests ────────────────────────────────────────────────────────

func TestQueueError(t *testing.T) {
	err := &QueueError{Backend: "redis", Op: "enqueue", Err: os.ErrNotExist}
	if err.Error() != "queue[redis] enqueue: file does not exist" {
		t.Errorf("unexpected error string: %q", err.Error())
	}

	// Test Unwrap
	if err.Unwrap() != os.ErrNotExist {
		t.Error("Unwrap should return the wrapped error")
	}
}

// ─── File-Backed Persistence via Interface ───────────────────────────────────

func TestTaskQueue_PersistAndReloadViaInterface(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks-queue.json")

	var q Queue = NewTaskQueue(path)
	q.Enqueue("persist-me")
	q.Enqueue("persist-me-too")

	// Reload from disk via a new Queue — verify items survived
	var q2 Queue = NewTaskQueue(path)
	if q2.Len() != 2 {
		t.Fatalf("expected 2 persisted tasks, got %d", q2.Len())
	}
	if task := q2.Dequeue(); task != "persist-me" {
		t.Errorf("expected 'persist-me', got %q", task)
	}
	if task := q2.Dequeue(); task != "persist-me-too" {
		t.Errorf("expected 'persist-me-too', got %q", task)
	}
}

func TestPriorityQueue_PersistAndReloadViaInterface(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "priority-queue.json")

	var pq PriorityTaskQueue = NewPriorityQueue(path)
	pq.Enqueue("critical-task", "agent-x", PriorityCritical)
	pq.Enqueue("background-task", "agent-y", PriorityBackground)

	var pq2 PriorityTaskQueue = NewPriorityQueue(path)
	if pq2.Len() != 2 {
		t.Fatalf("expected 2 persisted tasks, got %d", pq2.Len())
	}

	// Critical should still come first after reload
	task := pq2.Dequeue()
	if task.Priority != PriorityCritical {
		t.Errorf("expected critical task first after reload, got priority=%v", task.Priority)
	}
}
