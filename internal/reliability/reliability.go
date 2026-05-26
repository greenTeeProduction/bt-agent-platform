// Package reliability provides circuit breaker, exponential backoff,
// dead letter queue, worker pool, and task queue for the BT platform.
package reliability

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ─── Circuit Breaker ────────────────────────────────────────────────────────

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // normal operation
	CircuitOpen                         // failing, reject requests
	CircuitHalfOpen                     // testing if recovered
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed: return "closed"
	case CircuitOpen: return "open"
	case CircuitHalfOpen: return "half_open"
	default: return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
// After `threshold` consecutive failures, opens the circuit for `cooldown`.
// Then enters half-open to test with a single request before fully closing.
type CircuitBreaker struct {
	mu              sync.Mutex
	name            string
	state           CircuitState
	failureCount    int
	successCount    int
	threshold       int           // consecutive failures to open
	cooldown        time.Duration // time to stay open
	lastFailureTime time.Time
	lastStateChange time.Time
}

// NewCircuitBreaker creates a circuit breaker.
// threshold: failures to open. cooldown: time to stay open before half-open.
func NewCircuitBreaker(name string, threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		name:      name,
		state:     CircuitClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Allow checks if a request should be allowed through the circuit.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastStateChange) >= cb.cooldown {
			cb.state = CircuitHalfOpen
			cb.lastStateChange = time.Now()
			return true // allow one test request
		}
		return false
	case CircuitHalfOpen:
		return false // only allow one; this is the second request
	}
	return false
}

// RecordSuccess records a successful execution.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount = 0
	switch cb.state {
	case CircuitHalfOpen:
		cb.state = CircuitClosed
		cb.lastStateChange = time.Now()
	case CircuitOpen:
		// Shouldn't happen, but reset
		cb.state = CircuitClosed
		cb.lastStateChange = time.Now()
	}
}

// RecordFailure records a failed execution.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.state == CircuitHalfOpen || (cb.state == CircuitClosed && cb.failureCount >= cb.threshold) {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	}
}

// ─── Exponential Backoff ────────────────────────────────────────────────────

// Backoff computes exponential backoff delay.
// delay = base * 2^(attempt-1), capped at maxDelay.
func Backoff(attempt int, base, maxDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay > maxDelay {
			return maxDelay
		}
	}
	return delay
}

// RetryWithBackoff executes fn with exponential backoff retries.
// Returns the result and any final error after maxRetries.
func RetryWithBackoff(maxRetries int, base, maxDelay time.Duration, fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt < maxRetries {
			time.Sleep(Backoff(attempt, base, maxDelay))
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", maxRetries, lastErr)
}

// ─── Dead Letter Queue ──────────────────────────────────────────────────────

// DeadLetterEntry represents a failed task stored for inspection.
type DeadLetterEntry struct {
	ID        string    `json:"id"`
	Task      string    `json:"task"`
	Agent     string    `json:"agent"`
	Error     string    `json:"error"`
	Attempts  int       `json:"attempts"`
	FailedAt  time.Time `json:"failed_at"`
	Circuit   string    `json:"circuit,omitempty"`
}

// DeadLetterQueue stores failed tasks for manual inspection and replay.
type DeadLetterQueue struct {
	mu      sync.Mutex
	entries []DeadLetterEntry
	path    string // persistence file
}

// NewDeadLetterQueue creates a dead letter queue with optional persistence.
func NewDeadLetterQueue(persistencePath string) *DeadLetterQueue {
	dlq := &DeadLetterQueue{path: persistencePath}
	if persistencePath != "" {
		dlq.load()
	}
	return dlq
}

// Push adds a failed task to the dead letter queue.
func (dlq *DeadLetterQueue) Push(entry DeadLetterEntry) {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	entry.FailedAt = time.Now()
	dlq.entries = append(dlq.entries, entry)
	dlq.save()
}

// List returns all dead letter entries.
func (dlq *DeadLetterQueue) List() []DeadLetterEntry {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	result := make([]DeadLetterEntry, len(dlq.entries))
	copy(result, dlq.entries)
	return result
}

// Replay removes an entry from the DLQ and returns it for re-execution.
func (dlq *DeadLetterQueue) Replay(id string) (*DeadLetterEntry, bool) {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()

	for i, e := range dlq.entries {
		if e.ID == id {
			dlq.entries = append(dlq.entries[:i], dlq.entries[i+1:]...)
			dlq.save()
			return &e, true
		}
	}
	return nil, false
}

// Purge removes all entries from the dead letter queue.
func (dlq *DeadLetterQueue) Purge() {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	dlq.entries = nil
	dlq.save()
}

// Len returns the number of entries in the dead letter queue.
func (dlq *DeadLetterQueue) Len() int {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	return len(dlq.entries)
}

func (dlq *DeadLetterQueue) save() {
	if dlq.path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(dlq.path), 0755)
	data, _ := json.Marshal(dlq.entries)
	os.WriteFile(dlq.path, data, 0644)
}

func (dlq *DeadLetterQueue) load() {
	data, err := os.ReadFile(dlq.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &dlq.entries)
}

// ─── Worker Pool ────────────────────────────────────────────────────────────

// WorkerPool manages a fixed pool of goroutines for task execution.
type WorkerPool struct {
	workers  int
	tasks    chan func()
	wg       sync.WaitGroup
	quit     chan struct{}
	mu       sync.Mutex
	active   int
	total    uint64
	completed uint64
}

// NewWorkerPool creates a worker pool with N workers.
func NewWorkerPool(workers int) *WorkerPool {
	wp := &WorkerPool{
		workers: workers,
		tasks:   make(chan func(), workers*100),
		quit:    make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
	return wp
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for {
		select {
		case task, ok := <-wp.tasks:
			if !ok {
				return
			}
			wp.mu.Lock()
			wp.active++
			wp.mu.Unlock()
			task()
			wp.mu.Lock()
			wp.active--
			wp.completed++
			wp.mu.Unlock()
		case <-wp.quit:
			return
		}
	}
}

// Submit queues a task for execution. Returns false if the pool is closed.
func (wp *WorkerPool) Submit(task func()) bool {
	wp.mu.Lock()
	wp.total++
	wp.mu.Unlock()
	select {
	case wp.tasks <- task:
		return true
	case <-wp.quit:
		return false
	}
}

// Stats returns worker pool statistics.
func (wp *WorkerPool) Stats() (active int, queued int, total uint64, completed uint64) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.active, len(wp.tasks), wp.total, wp.completed
}

// Shutdown gracefully stops the worker pool, waiting for active tasks.
func (wp *WorkerPool) Shutdown() {
	close(wp.quit)
	close(wp.tasks)
	wp.wg.Wait()
}

// ─── Task Queue ─────────────────────────────────────────────────────────────

// TaskQueue provides a file-backed persistent task queue.
type TaskQueue struct {
	mu    sync.Mutex
	items []string
	path  string
}

// NewTaskQueue creates a file-backed task queue.
func NewTaskQueue(path string) *TaskQueue {
	tq := &TaskQueue{path: path}
	tq.load()
	return tq
}

// Enqueue adds a task to the queue.
func (tq *TaskQueue) Enqueue(task string) {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	tq.items = append(tq.items, task)
	tq.save()
}

// Dequeue removes and returns the next task. Returns empty string if empty.
func (tq *TaskQueue) Dequeue() string {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	if len(tq.items) == 0 {
		return ""
	}
	task := tq.items[0]
	tq.items = tq.items[1:]
	tq.save()
	return task
}

// Peek returns the next task without removing it.
func (tq *TaskQueue) Peek() string {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	if len(tq.items) == 0 {
		return ""
	}
	return tq.items[0]
}

// Len returns the number of tasks in the queue.
func (tq *TaskQueue) Len() int {
	tq.mu.Lock()
	defer tq.mu.Unlock()
	return len(tq.items)
}

func (tq *TaskQueue) save() {
	if tq.path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(tq.path), 0755)
	data, _ := json.Marshal(tq.items)
	os.WriteFile(tq.path, data, 0644)
}

func (tq *TaskQueue) load() {
	data, err := os.ReadFile(tq.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &tq.items)
}

// ─── Scheduler Persistence ──────────────────────────────────────────────────

// SchedulerState persists scheduler job state across restarts.
type SchedulerState struct {
	mu        sync.Mutex
	jobs      map[string]JobState
	path      string
}

// JobState represents a persisted job's runtime state.
type JobState struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Schedule    string    `json:"schedule"`
	LastRun     time.Time `json:"last_run"`
	NextRun     time.Time `json:"next_run"`
	RunCount    int       `json:"run_count"`
	ErrorCount  int       `json:"error_count"`
	Enabled     bool      `json:"enabled"`
	LastError   string    `json:"last_error,omitempty"`
}

// NewSchedulerState creates scheduler persistence.
func NewSchedulerState(path string) *SchedulerState {
	ss := &SchedulerState{
		jobs: make(map[string]JobState),
		path: path,
	}
	ss.load()
	return ss
}

// Save records a job's state.
func (ss *SchedulerState) Save(state JobState) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.jobs[state.ID] = state
	ss.persist()
}

// Get retrieves a job's state.
func (ss *SchedulerState) Get(id string) (JobState, bool) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	state, ok := ss.jobs[id]
	return state, ok
}

// List returns all job states.
func (ss *SchedulerState) List() []JobState {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	result := make([]JobState, 0, len(ss.jobs))
	for _, s := range ss.jobs {
		result = append(result, s)
	}
	return result
}

// Delete removes a job from persistence.
func (ss *SchedulerState) Delete(id string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.jobs, id)
	ss.persist()
}

func (ss *SchedulerState) persist() {
	if ss.path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(ss.path), 0755)
	data, _ := json.Marshal(ss.jobs)
	os.WriteFile(ss.path, data, 0644)
}

func (ss *SchedulerState) load() {
	data, err := os.ReadFile(ss.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &ss.jobs)
}
