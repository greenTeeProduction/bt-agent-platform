// Package reliability provides circuit breaker, exponential backoff,
// dead letter queue, worker pool, and task queue for the BT platform.
package reliability

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
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
	for k, v := range cb.categoryCounts {
		result[k] = v
	}
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
	Category  string    `json:"category,omitempty"` // ErrorCategory string, auto-classified on push
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
	// Auto-classify error if category not already set.
	if entry.Category == "" && entry.Error != "" {
		entry.Category = ClassifyError(fmt.Errorf("%s", entry.Error)).String()
	}
	dlq.entries = append(dlq.entries, entry)
	dlq.save()
}

// CategoryCounts returns the count of dead letter entries per error category.
func (dlq *DeadLetterQueue) CategoryCounts() map[string]int {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	counts := make(map[string]int)
	for _, e := range dlq.entries {
		cat := e.Category
		if cat == "" {
			cat = "unknown"
		}
		counts[cat]++
	}
	return counts
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
			// Recover from task panics so the worker stays alive.
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("WorkerPool: task panicked (worker recovered): %v", r)
					}
				}()
				task()
			}()
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

// ─── Priority ────────────────────────────────────────────────────────────────

// Priority represents the urgency of a task.
type Priority int

const (
	PriorityCritical  Priority = 0 // must execute immediately
	PriorityHigh      Priority = 1 // important, execute before normal tasks
	PriorityMedium    Priority = 2 // normal priority
	PriorityLow       Priority = 3 // best-effort
	PriorityBackground Priority = 4 // only when idle
)

func (p Priority) String() string {
	switch p {
	case PriorityCritical: return "critical"
	case PriorityHigh: return "high"
	case PriorityMedium: return "medium"
	case PriorityLow: return "low"
	case PriorityBackground: return "background"
	default: return "unknown"
	}
}

// PriorityTask is a task with priority and metadata for the priority queue.
type PriorityTask struct {
	ID       string   `json:"id"`
	Task     string   `json:"task"`
	Agent    string   `json:"agent"`
	Priority Priority `json:"priority"`
	QueuedAt time.Time `json:"queued_at"`
}

// PriorityQueue is a priority-ordered task queue backed by a min-heap.
// Lower priority values execute first (Critical=0 before Background=4).
type PriorityQueue struct {
	mu    sync.Mutex
	heap  []PriorityTask
	path  string
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
		fmt.Sscanf(t.ID, "pq-%d", &id)
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
	os.MkdirAll(filepath.Dir(pq.path), 0755)
	data, _ := json.Marshal(pq.heap)
	os.WriteFile(pq.path, data, 0644)
}

func (pq *PriorityQueue) load() {
	data, err := os.ReadFile(pq.path)
	if err != nil {
		return
	}
	json.Unmarshal(data, &pq.heap)
}

// ─── Agent Executor ──────────────────────────────────────────────────────────

// AgentResult encapsulates the result of an agent execution.
type AgentResult struct {
	Agent        string        `json:"agent"`
	Task         string        `json:"task"`
	Output       string        `json:"output"`
	Duration     time.Duration `json:"duration"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	QualityScore float64       `json:"quality_score"`
}

// AgentExecutor defines the interface for executing agent tasks.
// Implementations can be local (in-process), HTTP remote, or gRPC remote,
// enabling horizontal scaling and distributed execution.
type AgentExecutor interface {
	// Execute runs a task on the named agent and returns the result.
	Execute(agent, task string) (*AgentResult, error)

	// Health checks whether the executor backend is reachable and healthy.
	Health() error

	// String returns a human-readable identifier for this executor.
	String() string
}

// LocalExecutor executes agent tasks in-process via a callback function.
// This is the default executor for single-node deployments.
type LocalExecutor struct {
	name    string
	execute func(agent, task string) (*AgentResult, error)
	healthy func() error
}

// NewLocalExecutor creates a local executor with the given execute callback.
func NewLocalExecutor(name string, executeFn func(agent, task string) (*AgentResult, error)) *LocalExecutor {
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
func (le *LocalExecutor) Execute(agent, task string) (*AgentResult, error) {
	return le.execute(agent, task)
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
	LastFailure         time.Time `json:"last_failure,omitempty"`
	CoolDownUntil       time.Time `json:"cool_down_until,omitempty"`
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
	activeCounts     []int64                 // per-executor in-flight count (atomic, least-connections)
	executorFailures map[int]*executorFailureState // per-executor failure tracking
	failureThreshold int                     // consecutive failures before cooldown (default 5)
	failureCooldown  time.Duration           // cooldown duration after threshold exceeded (default 30s)
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
// If an executor's Execute() call fails, the router tries the next healthy executor.
// Falls back to local executor if all remote executors are exhausted.
// MaxFailover caps how many executors to try (0 = try all).
//
// Per-executor failure tracking: consecutive Execute() failures on an executor
// increment a counter. When it exceeds failureThreshold, the executor enters
// a cooldown period and is skipped for failureCooldown duration. A successful
// Execute() resets the counter and clears any cooldown.
func (r *AgentRouter) Execute(agent, task string) (*AgentResult, error) {
	// Snapshot router state under lock, then release before Health() calls.
	r.mu.Lock()
	executors := make([]AgentExecutor, len(r.executors))
	copy(executors, r.executors)
	strategy := r.strategy
	maxFailover := r.MaxFailover

	var start int
	var activeIdx int = -1 // executor index for active-count tracking (least-connections)

	if strategy == RoutingLeastConnections {
		// Snapshot active counts before releasing lock.
		activeSnapshot := make([]int64, len(r.activeCounts))
		for i := range r.activeCounts {
			activeSnapshot[i] = atomic.LoadInt64(&r.activeCounts[i])
		}
		r.mu.Unlock()

		// Health() may make network calls — do NOT hold lock.
		start = r.pickLeastConnections(executors, activeSnapshot)
		if start < 0 {
			if r.local != nil {
				return r.local.Execute(agent, task)
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
	var lastErr error
	tried := 0
	for i := 0; i < len(executors) && tried < maxFailover; i++ {
		idx := (start + i) % len(executors)
		e := executors[idx]

		// Skip executors in cooldown (zombie detection).
		if r.isCoolingDown(idx) {
			continue
		}

		if err := e.Health(); err != nil {
			continue // skip unhealthy executors
		}
		tried++
		result, err := e.Execute(agent, task)
		if err == nil {
			// Success resets failure counter and clears cooldown.
			r.recordSuccess(idx)
			return result, nil
		}
		// Record failure for zombie detection.
		r.recordFailure(idx)
		lastErr = err
	}

	// If we have a specific error, include it; otherwise fall back to local
	if lastErr != nil {
		// Try local as last resort
		if r.local != nil {
			result, localErr := r.local.Execute(agent, task)
			if localErr == nil {
				return result, nil
			}
			lastErr = fmt.Errorf("all executors failed (last remote: %w; local: %v)", lastErr, localErr)
		}
		return nil, lastErr
	}

	// No remote executor was healthy — fall back to local
	if r.local != nil {
		return r.local.Execute(agent, task)
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
	result := make([]AgentExecutor, len(r.executors))
	copy(result, r.executors)
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
		sem: make(chan struct{}, maxConcurrent),
	}
}

// Acquire blocks until a concurrency slot is available.
// Returns false if the context-like stop is signaled.
func (cl *ConcurrencyLimiter) Acquire() {
	cl.mu.Lock()
	cl.waiting++
	cl.mu.Unlock()

	cl.sem <- struct{}{}

	cl.mu.Lock()
	cl.waiting--
	cl.active++
	cl.total++
	cl.mu.Unlock()
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
	if cl.active > 0 {
		cl.active--
	}
	cl.mu.Unlock()

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
