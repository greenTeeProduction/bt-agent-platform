// Package reliability provides abstract queue interfaces enabling
// pluggable backends (file-backed, Redis, in-memory) for horizontal scaling.
package reliability

import "fmt"

// Queue is a FIFO task queue. Implementations must be thread-safe.
//
// Implementations:
//   - *TaskQueue — file-backed JSON persistence
//   - RedisQueue (future) — Redis LIST-backed distributed queue
type Queue interface {
	// Enqueue adds a task to the tail of the queue.
	Enqueue(task string)

	// Dequeue removes and returns the task at the head of the queue.
	// Returns empty string if the queue is empty.
	Dequeue() string

	// Peek returns the task at the head without removing it.
	// Returns empty string if the queue is empty.
	Peek() string

	// Len returns the number of tasks currently in the queue.
	Len() int
}

// PriorityTaskQueue is a priority-ordered task queue where lower
// Priority values execute first (Critical=0 before Background=4).
// Implementations must be thread-safe.
//
// Implementations:
//   - *PriorityQueue — file-backed JSON persistence with min-heap
//   - RedisPriorityQueue (future) — Redis ZSET-backed distributed priority queue
type PriorityTaskQueue interface {
	// Enqueue adds a task with the given priority and returns its unique ID.
	// Lower priority values execute first.
	Enqueue(task, agent string, priority Priority) string

	// Dequeue removes and returns the highest-priority task.
	// Returns an empty PriorityTask if the queue is empty.
	Dequeue() PriorityTask

	// Peek returns the highest-priority task without removing it.
	// Returns an empty PriorityTask if the queue is empty.
	Peek() PriorityTask

	// Len returns the number of tasks currently in the queue.
	Len() int
}

// QueueError represents a backend-specific queue error.
type QueueError struct {
	Backend string
	Op      string
	Err     error
}

func (e *QueueError) Error() string {
	return fmt.Sprintf("queue[%s] %s: %v", e.Backend, e.Op, e.Err)
}

func (e *QueueError) Unwrap() error {
	return e.Err
}

// Compile-time interface compliance checks.
// These ensure the file-backed implementations satisfy the interfaces,
// and will catch regressions if the method signatures change.
var (
	_ Queue             = (*TaskQueue)(nil)
	_ PriorityTaskQueue = (*PriorityQueue)(nil)
)
