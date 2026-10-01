// Package reliability provides circuit breaker, exponential backoff,
// dead letter queue, worker pool, and task queue for the BT platform.
package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nico/go-bt-evolve/internal/util"
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
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
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
	categoryCounts  map[ErrorCategory]int // per-category failure counts
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
	cb.successCount++
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
	cb.recordFailure(ErrCatUnknown)
}

// RecordFailureWithCategory records a failed execution with its error category.
func (cb *CircuitBreaker) RecordFailureWithCategory(err error) {
	cb.recordFailure(ClassifyError(err))
}

// RecordOutcome resolves one Allow()-granted request with its final error.
// It is the record-once companion every Allow() caller needs: a granted
// half-open probe MUST reach exactly one Record* call or the breaker wedges
// HalfOpen forever (Allow() in HalfOpen always returns false). Semantics:
//
//   - err == nil: success.
//   - caller-side err (validation/auth per ClassifyError): NOT counted — a
//     malformed request or bad credential must not open the circuit against
//     well-formed requests — but a pending half-open probe is resolved as
//     success, because the backend answered; infrastructure is healthy.
//   - any other err (network/timeout/5xx/rate-limit, and unknown — junk
//     output is evidence of a broken backend): a categorized failure that
//     walks the breaker toward open.
func (cb *CircuitBreaker) RecordOutcome(err error) {
	if err == nil {
		cb.RecordSuccess()
		return
	}
	switch ClassifyError(err) {
	case ErrCatValidation, ErrCatAuth:
		cb.mu.Lock()
		halfOpen := cb.state == CircuitHalfOpen
		cb.mu.Unlock()
		if halfOpen {
			cb.RecordSuccess()
		}
	default:
		cb.RecordFailureWithCategory(err)
	}
}

func (cb *CircuitBreaker) recordFailure(cat ErrorCategory) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()
	if cb.categoryCounts == nil {
		cb.categoryCounts = make(map[ErrorCategory]int)
	}
	cb.categoryCounts[cat]++

	if cb.state == CircuitHalfOpen || (cb.state == CircuitClosed && cb.failureCount >= cb.threshold) {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
	}
}

// CategoryFailureCounts returns per-category failure counts for diagnostics.
// Returns nil if no categorized failures have been recorded.
func (cb *CircuitBreaker) CategoryFailureCounts() map[ErrorCategory]int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if len(cb.categoryCounts) == 0 {
		return nil
	}
	result := make(map[ErrorCategory]int, len(cb.categoryCounts))
	maps.Copy(result, cb.categoryCounts)
	return result
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
		if IsExecutionTerminalError(err) {
			return err
		}
		lastErr = err
		if attempt < maxRetries {
			time.Sleep(Backoff(attempt, base, maxDelay))
		}
	}
	return fmt.Errorf("retry exhausted after %d attempts: %w", maxRetries, lastErr)
}

// AcquireFileLock takes an exclusive advisory flock on the sidecar
// `<path>.lock`, and unlinks the sidecar on release so no stray artifact is
// left beside the guarded file. flock
// attaches to the open file description, so two separate opens of the same
// sidecar exclude each other even within one process — the same shape as the
// daemon/dashboard cross-process case. The lock is advisory and relies on
// Linux flock semantics (the platform target). The returned release func is
// safe to call more than once.
//
// This is the single owner of the sidecar-flock idiom. Exported so packages
// guarding their own persisted files (internal/evolution,
// research.ProgramStore) share it instead of re-implementing it. The
// unlink-on-release variant must re-verify the sidecar after acquisition: a
// waiter can acquire the flock on an inode the previous holder already
// unlinked, and that lock excludes nobody (a fresh open of the path creates a
// new inode), so it retries on the live path instead.
//
// Legacy persistence callers wait until the lock is available: they do not
// all retain or retry failed writes. Callers with a cancellation/retry policy
// should explicitly use AcquireFileLockWithContext instead.
func AcquireFileLock(path string) (func(), error) {
	return AcquireFileLockWithContext(context.Background(), path)
}

// AcquireFileLockWithContext acquires `<path>.lock` with context cancellation
// and deadline support.
func AcquireFileLockWithContext(ctx context.Context, path string) (func(), error) {
	root, name, err := util.OpenPersistenceRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open lock parent: %w", err)
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = root.Close()
		}
	}()
	lockPath := name + ".lock"
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		f, err := root.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("open lock %s: %w", lockPath, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = f.Close()
			if errno, ok := err.(syscall.Errno); !ok || (errno != syscall.EWOULDBLOCK && errno != syscall.EAGAIN) {
				return nil, fmt.Errorf("flock lock %s: %w", lockPath, err)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
			}
			continue
		}
		held, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("stat lock %s: %w", lockPath, err)
		}
		if current, err := root.Stat(lockPath); err != nil || !os.SameFile(held, current) {
			_ = f.Close() // locked an orphaned inode; retry on the live path
			continue
		}
		release := sync.OnceFunc(func() {
			// Unlink before close so no waiter still blocked on this
			// inode can mistake it for the lock guarding the path.
			_ = root.Remove(lockPath)
			_ = f.Close() // closing the descriptor releases the flock
			_ = root.Close()
		})
		transferred = true
		return release, nil
	}
}

// ─── Worker Pool ────────────────────────────────────────────────────────────

// WorkerPool manages a fixed pool of goroutines for task execution.
type WorkerPool struct {
	admission sync.RWMutex
	closed    bool
	shutdown  sync.Once
	workers   int
	tasks     chan func()
	wg        sync.WaitGroup
	quit      chan struct{}
	mu        sync.Mutex
	active    int
	total     uint64
	completed uint64
}

// NewWorkerPool creates a worker pool with N workers.
func NewWorkerPool(workers int) *WorkerPool {
	workers = max(1, workers)
	wp := &WorkerPool{
		workers: workers,
		tasks:   make(chan func(), workers*100),
		quit:    make(chan struct{}),
	}
	for range workers {
		wp.wg.Add(1)
		go wp.worker()
	}
	return wp
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for task := range wp.tasks {
		wp.mu.Lock()
		wp.active++
		wp.mu.Unlock()
		// Recover from task panics so the worker stays alive.
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("workerpool: task panicked (worker recovered)", "panic", r)
				}
			}()
			task()
		}()
		wp.mu.Lock()
		wp.active--
		wp.completed++
		wp.mu.Unlock()
	}
}

// Submit queues a task for execution. Returns false if the pool is closed.
func (wp *WorkerPool) Submit(task func()) bool {
	return wp.SubmitWithContext(context.Background(), task) == nil
}

var (
	ErrWorkerPoolClosed = errors.New("worker pool is closed")
	ErrNilWorkerTask    = errors.New("worker task is nil")
)

// SubmitWithContext waits for queue capacity until cancellation or shutdown.
// A nil error acknowledges admission; shutdown drains every admitted task.
func (wp *WorkerPool) SubmitWithContext(ctx context.Context, task func()) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if task == nil {
		return ErrNilWorkerTask
	}
	wp.admission.RLock()
	defer wp.admission.RUnlock()
	if wp.closed {
		return ErrWorkerPoolClosed
	}
	select {
	case <-wp.quit:
		return ErrWorkerPoolClosed
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case wp.tasks <- task:
	case <-ctx.Done():
		return ctx.Err()
	case <-wp.quit:
		return ErrWorkerPoolClosed
	}
	wp.mu.Lock()
	wp.total++
	wp.mu.Unlock()
	return nil
}

// Stats returns worker pool statistics.
func (wp *WorkerPool) Stats() (active int, queued int, total uint64, completed uint64) {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.active, len(wp.tasks), wp.total, wp.completed
}

// Shutdown stops admission and drains every accepted task, including queued
// work. Tasks must eventually return; this method can be called concurrently.
func (wp *WorkerPool) Shutdown() {
	wp.shutdown.Do(func() {
		// Wake submissions holding the read lock while waiting for queue
		// capacity before acquiring the exclusive close lock.
		close(wp.quit)
		wp.admission.Lock()
		wp.closed = true
		close(wp.tasks)
		wp.admission.Unlock()
	})
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
	if err := util.SaveJSONAtomic(tq.path, tq.items); err != nil {
		slog.Error("task queue: atomic save failed", "path", tq.path, "error", err)
	}
}

func (tq *TaskQueue) load() {
	data, err := os.ReadFile(tq.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &tq.items)
}

// ─── Scheduler Persistence ──────────────────────────────────────────────────

// SchedulerState persists scheduler job state across restarts.
type SchedulerState struct {
	mu   sync.Mutex
	jobs map[string]JobState
	path string
}

// JobState represents a persisted job's runtime state.
type JobState struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Schedule   string    `json:"schedule"`
	LastRun    time.Time `json:"last_run"`
	NextRun    time.Time `json:"next_run"`
	RunCount   int       `json:"run_count"`
	ErrorCount int       `json:"error_count"`
	Enabled    bool      `json:"enabled"`
	LastError  string    `json:"last_error,omitempty"`
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
	result := slices.Collect(maps.Values(ss.jobs))
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
	if err := util.SaveJSONAtomic(ss.path, ss.jobs); err != nil {
		slog.Error("scheduler state: atomic persist failed", "path", ss.path, "error", err)
	}
}

func (ss *SchedulerState) load() {
	data, err := os.ReadFile(ss.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &ss.jobs)
}

// ─── Priority ────────────────────────────────────────────────────────────────

// Priority represents the urgency of a task.
type Priority int

const (
	PriorityCritical   Priority = 0 // must execute immediately
	PriorityHigh       Priority = 1 // important, execute before normal tasks
	PriorityMedium     Priority = 2 // normal priority
	PriorityLow        Priority = 3 // best-effort
	PriorityBackground Priority = 4 // only when idle
)

func (p Priority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	case PriorityBackground:
		return "background"
	default:
		return "unknown"
	}
}

// PriorityTask is a task with priority and metadata for the priority queue.
type PriorityTask struct {
	ID       string    `json:"id"`
	Task     string    `json:"task"`
	Agent    string    `json:"agent"`
	Priority Priority  `json:"priority"`
	QueuedAt time.Time `json:"queued_at"`
}

// PriorityQueue is a priority-ordered task queue backed by a min-heap.
// Lower priority values execute first (Critical=0 before Background=4).
type PriorityQueue struct {
	mu     sync.Mutex
	heap   []PriorityTask
	path   string
	nextID int
}

// NewPriorityQueue creates a priority queue with optional persistence.
func NewPriorityQueue(path string) *PriorityQueue {
	pq := &PriorityQueue{path: path}
	if path != "" {
		pq.load()
	}
	// Seed nextID from loaded entries to avoid collisions
	for _, t := range pq.heap {
		var id int
		_, _ = fmt.Sscanf(t.ID, "pq-%d", &id)
		if id >= pq.nextID {
			pq.nextID = id + 1
		}
	}
	return pq
}

// Enqueue adds a task with a given priority.
func (pq *PriorityQueue) Enqueue(task, agent string, priority Priority) string {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	id := fmt.Sprintf("pq-%d", pq.nextID)
	pq.nextID++

	pt := PriorityTask{
		ID:       id,
		Task:     task,
		Agent:    agent,
		Priority: priority,
		QueuedAt: time.Now(),
	}

	pq.heap = append(pq.heap, pt)
	pq.siftUp(len(pq.heap) - 1)
	pq.save()
	return id
}

// Dequeue removes and returns the highest-priority task.
// Returns empty PriorityTask if the queue is empty.
func (pq *PriorityQueue) Dequeue() PriorityTask {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.heap) == 0 {
		return PriorityTask{}
	}

	task := pq.heap[0]
	n := len(pq.heap) - 1
	pq.heap[0] = pq.heap[n]
	pq.heap = pq.heap[:n]
	if n > 0 {
		pq.siftDown(0)
	}
	pq.save()
	return task
}

// Peek returns the highest-priority task without removing it.
func (pq *PriorityQueue) Peek() PriorityTask {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	if len(pq.heap) == 0 {
		return PriorityTask{}
	}
	return pq.heap[0]
}

// Len returns the number of tasks in the queue.
func (pq *PriorityQueue) Len() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.heap)
}

// List returns a copy of all tasks, sorted by priority.
func (pq *PriorityQueue) List() []PriorityTask {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	result := make([]PriorityTask, len(pq.heap))
	copy(result, pq.heap)
	// heap is min-heap ordered by priority; copy preserves order
	return result
}

// Purge removes all tasks.
func (pq *PriorityQueue) Purge() {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.heap = nil
	pq.save()
}

// siftUp restores heap order after insertion at index i.
func (pq *PriorityQueue) siftUp(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if pq.heap[i].Priority >= pq.heap[parent].Priority {
			break
		}
		pq.heap[i], pq.heap[parent] = pq.heap[parent], pq.heap[i]
		i = parent
	}
}

// siftDown restores heap order after removal at index i.
func (pq *PriorityQueue) siftDown(i int) {
	n := len(pq.heap)
	for {
		smallest := i
		left := 2*i + 1
		right := 2*i + 2

		if left < n && pq.heap[left].Priority < pq.heap[smallest].Priority {
			smallest = left
		}
		if right < n && pq.heap[right].Priority < pq.heap[smallest].Priority {
			smallest = right
		}
		if smallest == i {
			break
		}
		pq.heap[i], pq.heap[smallest] = pq.heap[smallest], pq.heap[i]
		i = smallest
	}
}

func (pq *PriorityQueue) save() {
	if pq.path == "" {
		return
	}
	if err := util.SaveJSONAtomic(pq.path, pq.heap); err != nil {
		slog.Error("priority queue: atomic save failed", "path", pq.path, "error", err)
	}
}

func (pq *PriorityQueue) load() {
	data, err := os.ReadFile(pq.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &pq.heap)
}

// ─── Agent Executor ──────────────────────────────────────────────────────────

// AgentResult encapsulates the result of an agent execution.
type AgentResult struct {
	Agent  string `json:"agent"`
	Task   string `json:"task"`
	Output string `json:"output"`
	// Outcome carries the originating runner's raw outcome string (e.g.
	// "success" or a sentinel like the scheduler's rate-limit carryover) when
	// the executor backend has one to report, so callers that dispatch
	// through an AgentExecutor/AgentRouter — not just agent.RunDeps.RunOnce
	// directly — can still distinguish those dispositions instead of
	// collapsing everything to the Success bool. Empty when the backend
	// (e.g. a remote node that hasn't been updated to populate it) has none.
	Outcome  string        `json:"outcome,omitempty"`
	Duration time.Duration `json:"duration"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
	// ErrorKind preserves completed, stopped, or uncertain execution diagnostics.
	ErrorKind    string  `json:"error_kind,omitempty"`
	QualityScore float64 `json:"quality_score"`
}

// AgentExecutor defines the interface for executing agent tasks.
// Implementations can be local (in-process), HTTP remote, or gRPC remote,
// enabling horizontal scaling and distributed execution.
type AgentExecutor interface {
	// Execute runs a task on the named agent and returns the result. The
	// caller's context bounds the whole attempt: executors must propagate it
	// into the work they dispatch (in-process runs, remote calls) so a
	// scheduler job deadline actually limits its attempts — the seam
	// previously dropped the context and a routed attempt ran 2h29m against
	// a 2h job ctx (2026-07-22, DLQ #239).
	Execute(ctx context.Context, agent, task string) (*AgentResult, error)

	// Health checks whether the executor backend is reachable and healthy.
	Health() error

	// String returns a human-readable identifier for this executor.
	String() string
}

// LocalExecutor executes agent tasks in-process via a callback function.
// This is the default executor for single-node deployments.
type LocalExecutor struct {
	name    string
	execute func(ctx context.Context, agent, task string) (*AgentResult, error)
	healthy func() error
}

// NewLocalExecutor creates a local executor with the given execute callback.
func NewLocalExecutor(name string, executeFn func(ctx context.Context, agent, task string) (*AgentResult, error)) *LocalExecutor {
	return &LocalExecutor{
		name:    name,
		execute: executeFn,
		healthy: func() error { return nil },
	}
}

// WithHealthCheck sets a custom health check function.
func (le *LocalExecutor) WithHealthCheck(fn func() error) *LocalExecutor {
	le.healthy = fn
	return le
}

// Execute runs the agent task via the local callback.
func (le *LocalExecutor) Execute(ctx context.Context, agent, task string) (*AgentResult, error) {
	return le.execute(ctx, agent, task)
}

// Health checks the local executor's health.
func (le *LocalExecutor) Health() error {
	if le.healthy != nil {
		return le.healthy()
	}
	return nil
}

// String returns the executor identifier.
func (le *LocalExecutor) String() string {
	return le.name
}

// executorFailureState tracks per-executor failure history for zombie detection.
// An executor that passes health checks but consistently fails Execute() calls
// enters a cooldown period where it's skipped during routing. This prevents
// wasted attempts on degraded peers in multi-node deployments.
type executorFailureState struct {
	consecutiveFailures int
	lastFailure         time.Time
	coolingDown         bool
	coolDownUntil       time.Time
}

// ExecutorHealthDetail provides per-executor health and failure statistics
// for monitoring and diagnostics in multi-node deployments.
type ExecutorHealthDetail struct {
	Index               int       `json:"index"`
	Name                string    `json:"name"`
	Healthy             bool      `json:"healthy"`
	CoolingDown         bool      `json:"cooling_down"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastFailure         time.Time `json:"last_failure,omitzero"`
	CoolDownUntil       time.Time `json:"cool_down_until,omitzero"`
}

// AgentRouter distributes agent tasks across multiple executors with
// health-aware routing, failover retry, and graceful degradation.
// Supports two routing strategies: round-robin (default) and least-connections.
// When an executor's Execute() call fails, the router tries the next healthy
// executor. When all remote executors are exhausted, falls back to local execution.
//
// Per-executor failure tracking detects "zombie" peers that pass health checks
// but consistently fail on actual task execution. Executors that exceed the
// failure threshold enter a cooldown period where they're skipped during routing.
type AgentRouter struct {
	mu               sync.RWMutex
	executors        []AgentExecutor
	next             int
	local            AgentExecutor // fallback
	MaxFailover      int           // max executors to try per Execute() call (0 = try all)
	strategy         RoutingStrategy
	activeCounts     []int64                       // per-executor in-flight count (atomic, least-connections)
	executorFailures map[int]*executorFailureState // per-executor failure tracking
	failureThreshold int                           // consecutive failures before cooldown (default 5)
	failureCooldown  time.Duration                 // cooldown duration after threshold exceeded (default 30s)
	heartbeat        *AgentRouterHeartbeat         // async health tracking (optional, nil = synchronous only)
}

// NewAgentRouter creates a router with the given executors.
// The first executor is used as the local fallback if none is explicitly set.
// Default failure threshold is 5 consecutive failures; default cooldown is 30s.
func NewAgentRouter(executors ...AgentExecutor) *AgentRouter {
	r := &AgentRouter{
		executors:        executors,
		failureThreshold: 5,
		failureCooldown:  30 * time.Second,
		executorFailures: make(map[int]*executorFailureState),
	}
	if len(executors) > 0 {
		r.local = executors[0]
	}
	return r
}

// AgentEndpoint describes a remote peer discovered from the live A2A card
// registry: the peer's name and the base URL of its HTTP interface (plus an
// optional API key). It is the reduced, transport-agnostic shape the daemon
// hands to reliability so this package does not depend on the A2A card types.
type AgentEndpoint struct {
	Name    string
	BaseURL string
	APIKey  string
}

// NewRouterFromEndpoints constructs an AgentRouter that distributes agent tasks
// across the given remote peers, with the supplied local in-process executor
// installed as the fallback used when no remote peer is healthy.
//
// This is the production seam that adopts the RemoteExecutor + AgentRouter
// horizontal-scaling substrate: the daemon reduces its live A2A card registry
// to a set of AgentEndpoints and hands them here. Each endpoint with a non-empty
// BaseURL becomes a RemoteExecutor; endpoints without a URL (peers that expose
// no reachable interface) are skipped. An empty or nil endpoint list yields a
// router with no remote executors that routes every task to the local executor,
// so single-node deployments behave exactly as before adopting the substrate.
func NewRouterFromEndpoints(local AgentExecutor, endpoints []AgentEndpoint) *AgentRouter {
	router := NewAgentRouter()
	// The passed-in local executor is always the fallback — set it before
	// adding remotes so Add's "adopt first executor as local" fallback never
	// overrides it.
	router.SetLocal(local)
	router.AddEndpoints(endpoints)
	return router
}

// AddEndpoints reduces the given remote peer endpoints to RemoteExecutors and
// adds them to the router, using the same BaseURL-required filtering as
// NewRouterFromEndpoints (endpoints with no reachable interface are
// skipped). This lets a router built before peer discovery has run — e.g. a
// daemon's scheduler/replay closures that must capture the router variable
// ahead of A2A server startup — adopt newly-discovered peers in place once
// the live card registry is available, without reconstructing the router or
// disturbing its already-configured local fallback.
func (r *AgentRouter) AddEndpoints(endpoints []AgentEndpoint) {
	for _, ep := range endpoints {
		if ep.BaseURL == "" {
			continue
		}
		r.Add(NewRemoteExecutor(RemoteExecutorConfig{
			Name:    ep.Name,
			BaseURL: ep.BaseURL,
			APIKey:  ep.APIKey,
		}))
	}
}

// Add adds an executor to the router. New executors start with zero failures
// and are immediately eligible for routing.
func (r *AgentRouter) Add(e AgentExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executors = append(r.executors, e)
	if r.local == nil {
		r.local = e
	}
	r.ensureActiveCounts()
}

// SetLocal sets the fallback executor used when all others are unhealthy.
func (r *AgentRouter) SetLocal(e AgentExecutor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.local = e
}

// Execute routes a task to a healthy executor using the configured strategy.
// Round-robin (default): distributes evenly across executors.
// Least-connections: picks the executor with fewest in-flight requests.
// Retryable execution failures may try the next healthy executor; completed
// persistence diagnostics and uncertain remote outcomes are terminal.
// Falls back to local executor if all remote executors are exhausted.
// MaxFailover caps how many executors to try (0 = try all).
//
// Per-executor failure tracking: consecutive Execute() failures on an executor
// increment a counter. When it exceeds failureThreshold, the executor enters
// a cooldown period and is skipped for failureCooldown duration. A successful
// Execute() resets the counter and clears any cooldown.
func (r *AgentRouter) Execute(ctx context.Context, agent, task string) (*AgentResult, error) {
	// Snapshot router state under lock, then release before Health() calls.
	r.mu.Lock()
	executors := slices.Clone(r.executors)
	strategy := r.strategy
	maxFailover := r.MaxFailover

	var start int
	activeIdx := -1 // executor index for active-count tracking (least-connections)

	if strategy == RoutingLeastConnections || strategy == RoutingAuction {
		// Snapshot active counts before releasing lock.
		activeSnapshot := make([]int64, len(r.activeCounts))
		for i := range r.activeCounts {
			activeSnapshot[i] = atomic.LoadInt64(&r.activeCounts[i])
		}
		r.mu.Unlock()

		// Health() / Bid() may make network calls — do NOT hold lock.
		if strategy == RoutingAuction {
			start = r.pickAuctionWinner(executors, activeSnapshot, agent, task)
		} else {
			start = r.pickLeastConnections(executors, activeSnapshot)
		}
		if start < 0 {
			if r.local != nil {
				return r.local.Execute(ctx, agent, task)
			}
			return nil, fmt.Errorf("no healthy executor available for agent %q", agent)
		}

		// Re-acquire lock to increment active count.
		r.mu.Lock()
		if start < len(r.activeCounts) {
			atomic.AddInt64(&r.activeCounts[start], 1)
		}
		activeIdx = start
		r.next = (start + 1) % max(1, len(executors))
		r.mu.Unlock()

		defer func() {
			r.mu.Lock()
			if activeIdx >= 0 && activeIdx < len(r.activeCounts) {
				atomic.AddInt64(&r.activeCounts[activeIdx], -1)
			}
			r.mu.Unlock()
		}()
	} else {
		start = r.next
		r.next = (r.next + 1) % max(1, len(executors))
		r.mu.Unlock()
	}

	if maxFailover <= 0 {
		maxFailover = len(executors)
	}

	// Failover loop: try executors starting from `start`.
	// Each executor's Health() must pass before we try Execute().
	// Cooling-down executors are skipped regardless of Health().
	// A failed executor's RESULT is preserved alongside its error: graceful
	// non-success runs (e.g. the goap rate-limit carryover) return a
	// populated result the caller needs for classification — dropping it
	// once turned a healthy pause into 3 retries and a dead-letter entry
	// (2026-07-16 20:30).
	var lastErr error
	var lastResult *AgentResult
	tried := 0
	for i := 0; i < len(executors) && tried < maxFailover; i++ {
		// The caller's context bounds the whole routed call: once it is
		// done, starting another executor attempt just burns a full run the
		// scheduler will already classify as failed.
		if ctx.Err() != nil {
			break
		}
		idx := (start + i) % len(executors)
		e := executors[idx]

		// Skip executors in cooldown (zombie detection).
		if r.isCoolingDown(idx) {
			continue
		}

		if err := r.executeHealthCheck(idx, e); err != nil {
			continue // skip unhealthy executors
		}
		tried++
		result, err := e.Execute(ctx, agent, task)
		if err == nil {
			// Success resets failure counter and clears cooldown.
			r.recordSuccess(idx)
			// Refresh heartbeat timestamp after successful execution.
			r.pingHeartbeatAfterSuccess(idx)
			return result, nil
		}
		if IsExecutionPersistenceError(err) {
			// Execution completed; another backend would repeat its effects.
			r.recordSuccess(idx)
			r.pingHeartbeatAfterSuccess(idx)
			return result, err
		}
		if IsExecutionStoppedError(err) && IsExecutionPause(ExecutionStopOutcome(err), err) {
			r.recordSuccess(idx)
			r.pingHeartbeatAfterSuccess(idx)
			return result, err // owner is waiting; another peer would start new work
		}
		// Record failure for zombie detection.
		r.recordFailure(idx)
		if IsExecutionTerminalError(err) {
			// The peer may still be working. Preserve uncertainty through the
			// outer retry policy instead of trying another peer or local copy.
			return result, err
		}
		lastErr = err
		if result != nil {
			lastResult = result
		}
	}

	// Context done: no more attempts of any kind — return what the tried
	// executors produced (result preserved for outcome classification), or
	// the context error if nothing ran at all.
	if ctx.Err() != nil {
		if lastErr != nil {
			return lastResult, lastErr
		}
		return nil, fmt.Errorf("agent router: context done before any executor attempt for agent %q: %w", agent, ctx.Err())
	}

	// If we have a specific error, include it; otherwise fall back to local
	if lastErr != nil {
		// Try local as last resort
		if r.local != nil {
			result, localErr := r.local.Execute(ctx, agent, task)
			if localErr == nil {
				return result, nil
			}
			if IsExecutionTerminalError(localErr) {
				return result, localErr
			}
			if result != nil {
				lastResult = result
			}
			lastErr = fmt.Errorf("all executors failed (last remote: %w; local: %v)", lastErr, localErr)
		}
		return lastResult, lastErr
	}

	// No remote executor was healthy — fall back to local
	if r.local != nil {
		return r.local.Execute(ctx, agent, task)
	}

	return nil, fmt.Errorf("no healthy executor available for agent %q", agent)
}

// Health returns nil if at least one executor is healthy.
func (r *AgentRouter) Health() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.executors {
		if e.Health() == nil {
			return nil
		}
	}
	if r.local != nil {
		return r.local.Health()
	}
	return fmt.Errorf("no executors configured")
}

// String returns a summary of the router configuration, including failure and
// cooldown statistics for multi-node diagnostics.
func (r *AgentRouter) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cooling := 0
	failed := 0
	for _, fs := range r.executorFailures {
		if fs.coolingDown {
			cooling++
		}
		if fs.consecutiveFailures > 0 {
			failed++
		}
	}
	return fmt.Sprintf("AgentRouter(executors=%d, strategy=%s, local=%s, failures=%d, cooling=%d)",
		len(r.executors), r.strategy, r.local.String(), failed, cooling)
}

// Executors returns the current list of executors.
func (r *AgentRouter) Executors() []AgentExecutor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := slices.Clone(r.executors)
	return result
}

// HealthyExecutors returns only executors that pass their health check
// AND are not in a cooldown period (zombie detection).
// Automatically clears expired cooldowns. Uses write lock for safe mutation.
func (r *AgentRouter) HealthyExecutors() []AgentExecutor {
	r.mu.Lock()
	defer r.mu.Unlock()
	var healthy []AgentExecutor
	for i, e := range r.executors {
		if r.isCoolingDownLocked(i) {
			continue
		}
		if e.Health() == nil {
			healthy = append(healthy, e)
		}
	}
	return healthy
}

// isCoolingDown checks whether executor `idx` is in a cooldown period.
// If the cooldown has expired, the executor is automatically cleared.
// Must NOT hold r.mu (acquires RLock internally).
func (r *AgentRouter) isCoolingDown(idx int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isCoolingDownLocked(idx)
}

// isCoolingDownLocked is the internal version that assumes r.mu is held.
func (r *AgentRouter) isCoolingDownLocked(idx int) bool {
	fs, ok := r.executorFailures[idx]
	if !ok || !fs.coolingDown {
		return false
	}
	if time.Now().After(fs.coolDownUntil) {
		// Cooldown expired — clear it.
		fs.coolingDown = false
		fs.consecutiveFailures = 0
		return false
	}
	return true
}

// recordSuccess resets the failure counter and clears any cooldown for executor `idx`.
func (r *AgentRouter) recordSuccess(idx int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fs, ok := r.executorFailures[idx]
	if !ok {
		return
	}
	fs.consecutiveFailures = 0
	fs.coolingDown = false
}

// recordFailure increments the failure counter for executor `idx`.
// If consecutive failures exceed the threshold, the executor enters cooldown.
func (r *AgentRouter) recordFailure(idx int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fs, ok := r.executorFailures[idx]
	if !ok {
		fs = &executorFailureState{}
		r.executorFailures[idx] = fs
	}
	fs.consecutiveFailures++
	fs.lastFailure = time.Now()
	if fs.consecutiveFailures >= r.failureThreshold {
		fs.coolingDown = true
		fs.coolDownUntil = time.Now().Add(r.failureCooldown)
	}
}

// ExecutorHealthStatus returns detailed health and failure statistics for
// all executors. This is the primary diagnostic API for multi-node deployments.
// Automatically clears expired cooldowns. Uses write lock for safe mutation.
func (r *AgentRouter) ExecutorHealthStatus() []ExecutorHealthDetail {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	result := make([]ExecutorHealthDetail, len(r.executors))
	for i, e := range r.executors {
		healthy := e.Health() == nil
		fs, ok := r.executorFailures[i]
		detail := ExecutorHealthDetail{
			Index:   i,
			Name:    e.String(),
			Healthy: healthy,
		}
		if ok {
			// Auto-expire cooldowns that have passed.
			if fs.coolingDown && now.After(fs.coolDownUntil) {
				fs.coolingDown = false
				fs.consecutiveFailures = 0
			}
			detail.ConsecutiveFailures = fs.consecutiveFailures
			if !fs.lastFailure.IsZero() {
				detail.LastFailure = fs.lastFailure
			}
			if fs.coolingDown {
				detail.CoolingDown = true
				detail.CoolDownUntil = fs.coolDownUntil
			}
		}
		result[i] = detail
	}
	return result
}

// ResetExecutor clears the failure counter and cooldown for a specific executor.
// Use this to manually re-enable an executor after underlying issues are resolved.
func (r *AgentRouter) ResetExecutor(idx int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.executorFailures, idx)
}

// SetFailureThreshold sets the number of consecutive Execute() failures before
// an executor enters cooldown. Default is 5.
func (r *AgentRouter) SetFailureThreshold(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failureThreshold = n
}

// SetFailureCooldown sets the duration an executor stays in cooldown after
// exceeding the failure threshold. Default is 30s.
func (r *AgentRouter) SetFailureCooldown(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failureCooldown = d
}

// ─── Concurrency Limiter ─────────────────────────────────────────────────────

// ConcurrencyLimiter caps concurrent execution to maxConcurrent.
// Uses a buffered channel as a semaphore. Acquire blocks when at capacity;
// Release frees a slot.
type ConcurrencyLimiter struct {
	sem     chan struct{}
	mu      sync.Mutex
	active  int
	waiting int
	total   uint64
}

// NewConcurrencyLimiter creates a concurrency limiter with max slots.
func NewConcurrencyLimiter(maxConcurrent int) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{
		sem: make(chan struct{}, max(1, maxConcurrent)),
	}
}

// Acquire blocks until a concurrency slot is available.
func (cl *ConcurrencyLimiter) Acquire() {
	_ = cl.AcquireWithContext(context.Background())
}

// AcquireWithContext reserves a slot or returns the caller's cancellation.
// A successful reservation must be released exactly once by its owner.
func (cl *ConcurrencyLimiter) AcquireWithContext(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cl.mu.Lock()
	cl.waiting++
	cl.mu.Unlock()

	select {
	case cl.sem <- struct{}{}:
	case <-ctx.Done():
		cl.mu.Lock()
		cl.waiting--
		cl.mu.Unlock()
		return ctx.Err()
	}

	cl.mu.Lock()
	cl.waiting--
	cl.active++
	cl.total++
	cl.mu.Unlock()
	return nil
}

// TryAcquire attempts to acquire a slot without blocking.
// Returns true if a slot was available, false otherwise.
func (cl *ConcurrencyLimiter) TryAcquire() bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	select {
	case cl.sem <- struct{}{}:
		cl.active++
		cl.total++
		return true
	default:
		return false
	}
}

// Release frees a concurrency slot.
func (cl *ConcurrencyLimiter) Release() {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.active == 0 {
		return
	}
	cl.active--

	select {
	case <-cl.sem:
	default:
	}
}

// Stats returns current limiter statistics.
func (cl *ConcurrencyLimiter) Stats() (active, waiting int, total uint64) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.active, cl.waiting, cl.total
}

// Capacity returns the maximum concurrent slots.
func (cl *ConcurrencyLimiter) Capacity() int {
	return cap(cl.sem)
}

// Available returns the number of free concurrency slots.
func (cl *ConcurrencyLimiter) Available() int {
	return cap(cl.sem) - len(cl.sem)
}

// SuccessCount returns successful executions recorded on the breaker.
func (cb *CircuitBreaker) SuccessCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.successCount
}

// FailureCount returns the current consecutive failure count.
func (cb *CircuitBreaker) FailureCount() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.failureCount
}

// LastFailureTime returns the timestamp of the most recently recorded
// failure, or the zero Time if no failure has been recorded yet.
func (cb *CircuitBreaker) LastFailureTime() time.Time {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.lastFailureTime
}

// Threshold returns the configured consecutive-failure threshold.
func (cb *CircuitBreaker) Threshold() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.threshold
}

// Cooldown returns the configured open-state cooldown duration.
func (cb *CircuitBreaker) Cooldown() time.Duration {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.cooldown
}

// Reset resets the circuit breaker to closed state, clearing the failure
// count. Used by callers (e.g. an operator-triggered reset) that need to
// force a breaker back to healthy without waiting out its cooldown.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.lastStateChange = time.Now()
}

// RestoreState sets the breaker's state and failure/last-failure data
// directly, bypassing the normal transition rules. This is the seam callers
// that persist breaker state to disk (e.g. internal/agent's
// AgentCircuitBreakerStore.Load) use to rebuild a breaker from a saved
// snapshot; lastStateChange is reset to now so a restored Open state's
// cooldown clock starts fresh rather than replaying an already-expired one.
func (cb *CircuitBreaker) RestoreState(state CircuitState, failureCount int, lastFailureTime time.Time) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = state
	cb.failureCount = failureCount
	cb.lastFailureTime = lastFailureTime
	cb.lastStateChange = time.Now()
}

// CircuitSummary provides a snapshot of a circuit breaker's state, suitable
// for status reporting without exposing the breaker itself.
type CircuitSummary struct {
	State        CircuitState  `json:"state"`
	FailureCount int           `json:"failure_count"`
	SuccessCount int           `json:"success_count"`
	Threshold    int           `json:"threshold"`
	Cooldown     time.Duration `json:"cooldown"`
}

// CircuitBreakerOptions configures the default threshold and cooldown a
// CircuitBreakerStore applies to breakers it creates.
type CircuitBreakerOptions struct {
	// Threshold is the default consecutive failure count before opening (default: 3).
	Threshold int
	// Cooldown is the default duration the circuit stays open (default: 5m).
	Cooldown time.Duration
}

// DefaultCircuitBreakerOptions returns sensible defaults.
func DefaultCircuitBreakerOptions() CircuitBreakerOptions {
	return CircuitBreakerOptions{
		Threshold: 3,
		Cooldown:  5 * time.Minute,
	}
}

// CircuitBreakerStore manages a named registry of circuit breakers, creating
// one lazily per name on first use. This is the canonical form of the
// map[string]*CircuitBreaker registry pattern duplicated across callers
// (internal/agent's scheduler, internal/llm, internal/a2a); new callers
// needing a named breaker registry should use this instead of hand-rolling
// their own.
type CircuitBreakerStore struct {
	mu      sync.RWMutex
	agents  map[string]*CircuitBreaker
	options CircuitBreakerOptions
}

// NewCircuitBreakerStore creates a new circuit breaker store. Zero-value
// Threshold/Cooldown in opts fall back to DefaultCircuitBreakerOptions.
func NewCircuitBreakerStore(opts CircuitBreakerOptions) *CircuitBreakerStore {
	if opts.Threshold <= 0 {
		opts.Threshold = 3
	}
	if opts.Cooldown <= 0 {
		opts.Cooldown = 5 * time.Minute
	}
	return &CircuitBreakerStore{
		agents:  make(map[string]*CircuitBreaker),
		options: opts,
	}
}

// Get returns the circuit breaker for the named entry, creating it if needed.
func (s *CircuitBreakerStore) Get(name string) *CircuitBreaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	cb, ok := s.agents[name]
	if !ok {
		cb = NewCircuitBreaker(name, s.options.Threshold, s.options.Cooldown)
		s.agents[name] = cb
	}
	return cb
}

// Allowed checks whether the named entry is allowed to execute.
func (s *CircuitBreakerStore) Allowed(name string) bool {
	return s.Get(name).Allow()
}

// RecordSuccess records a successful execution for the named entry.
func (s *CircuitBreakerStore) RecordSuccess(name string) {
	s.Get(name).RecordSuccess()
}

// RecordFailure records a failed execution for the named entry.
func (s *CircuitBreakerStore) RecordFailure(name string) {
	s.Get(name).RecordFailure()
}

// Status returns circuit state summaries for all tracked entries.
func (s *CircuitBreakerStore) Status() map[string]CircuitSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]CircuitSummary, len(s.agents))
	for name, cb := range s.agents {
		cb.mu.Lock()
		result[name] = CircuitSummary{
			State:        cb.state,
			FailureCount: cb.failureCount,
			SuccessCount: cb.successCount,
			Threshold:    cb.threshold,
			Cooldown:     cb.cooldown,
		}
		cb.mu.Unlock()
	}
	return result
}

// ResetAll resets all tracked circuit breakers to closed state.
func (s *CircuitBreakerStore) ResetAll() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, cb := range s.agents {
		cb.Reset()
	}
}

// ─── Outcome Scoring ────────────────────────────────────────────────────────

// ScoreOutcome derives a 0-100 fitness score from execution state: the
// quality score scaled to a percentage, falling back to 75 (success) or 25
// (failure) when that scaled score is zero or below, then clamped to
// [0,100]. This is the canonical formula behind the block-fitness scoring
// duplicated across internal/blocks, internal/engine, and internal/dashboard.
//
// The fallback trusts success exclusively — it must not be overridden by
// outcome, since callers (e.g. internal/dashboard's recordBlockFitnessMetric)
// derive success from agent.IsBreakerSuccess, which already folds runErr
// into the classification. Re-deriving healthiness from outcome here let a
// non-nil runErr with a stale outcome string of "success"/"completed" score
// 75 instead of 25 (2026-07-22 fleet review).
func ScoreOutcome(outcome string, qualityScore float64, success bool) float64 {
	score := qualityScore * 100
	if score <= 0 {
		if success {
			score = 75
		} else {
			score = 25
		}
	}
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}
