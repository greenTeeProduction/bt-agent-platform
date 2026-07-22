// Package reliability provides circuit breaker, exponential backoff,
// dead letter queue, worker pool, and task queue for the BT platform.
package reliability

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
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

// Graceful-degradation bounds for the dead letter queue. Under a sustained
// failure storm an unbounded DLQ would grow without limit (memory + disk), and
// a "poison pill" task that fails every replay would drive an infinite
// auto-requeue loop. These constants cap both.
const (
	// MaxDeadLetterEntries caps how many entries the DLQ retains. When Push
	// overflows this bound, the OLDEST entries are evicted first.
	MaxDeadLetterEntries = 1000
	// MaxReplayAttempts is the number of auto-requeues an entry may accrue
	// before it is terminally flagged Abandoned and excluded from further
	// requeue, breaking infinite replay loops on a poison pill.
	MaxReplayAttempts = 5
)

// DeadLetterEntry represents a failed task stored for inspection.
type DeadLetterEntry struct {
	ID       string    `json:"id"`
	Task     string    `json:"task"`
	Agent    string    `json:"agent"`
	Error    string    `json:"error"`
	Attempts int       `json:"attempts"`
	FailedAt time.Time `json:"failed_at"`
	Circuit  string    `json:"circuit,omitempty"`
	Category string    `json:"category,omitempty"` // ErrorCategory string, auto-classified on push
	// BuildRevision records the VCS revision of the process that produced this
	// dead letter (dashboard.ReadBuildIdentity().Revision, stamped at the push
	// site). Deploy-drift diagnosis (program 94b0b31) uses it to distinguish a
	// failure on a stale binary from one on current code. Optional: unstamped
	// builds and old entries omit it.
	BuildRevision string `json:"build_revision,omitempty"`
	// RequeuedAt is stamped by Requeue when a process without a tree runner (the
	// dashboard) flags this entry for retry. A non-zero value signals bt-agent's
	// executor to pick the task up on its next scan instead of leaving it dead.
	RequeuedAt time.Time `json:"requeued_at,omitempty"`
	// Abandoned is set once an entry's replay Attempts exceed MaxReplayAttempts.
	// An abandoned entry is retained for inspection but excluded from further
	// auto-requeue so a poison pill cannot drive an infinite replay loop.
	Abandoned bool `json:"abandoned,omitempty"`
	// LastReplayAt and LastReplayError record the most recent failed replay so
	// the outcome survives on disk for sibling processes; a successful replay
	// removes the entry, so a set value always describes a failure.
	LastReplayAt    time.Time `json:"last_replay_at,omitempty"`
	LastReplayError string    `json:"last_replay_error,omitempty"`
}

// DeadLetterQueue stores failed tasks for manual inspection and replay.
type DeadLetterQueue struct {
	mu      sync.Mutex
	entries []DeadLetterEntry
	path    string // persistence file

	executor    ReplayExecutor  // re-executes replayed tasks (SetReplayExecutor)
	replaying   map[string]bool // ids currently being replayed (guards doubled replays)
	pushCounter uint64          // disambiguates entries pushed within the same nanosecond
}

// ReplayExecutor re-executes one dead-lettered task. A nil error means the
// task succeeded and the entry may be removed; any error retains the entry.
type ReplayExecutor func(entry DeadLetterEntry) error

// SetReplayExecutor installs the function Replay uses to re-execute tasks.
func (dlq *DeadLetterQueue) SetReplayExecutor(fn ReplayExecutor) {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	dlq.executor = fn
}

// RequeuedReady returns the ids of entries flagged for retry (RequeuedAt set)
// that are not abandoned — the background scan's work list.
func (dlq *DeadLetterQueue) RequeuedReady() []string {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	var ids []string
	for _, e := range dlq.entries {
		if !e.RequeuedAt.IsZero() && !e.Abandoned {
			ids = append(ids, e.ID)
		}
	}
	return ids
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
	// Callers such as pushToDLQAction never set ID, and mergeFromDisk's byID map
	// collapses entries that share an ID — so an empty ID must get a default
	// that cannot collide with another entry pushed in the same nanosecond.
	if entry.ID == "" {
		dlq.pushCounter++
		entry.ID = fmt.Sprintf("%s-%d-%d", entry.Agent, time.Now().UnixNano(), dlq.pushCounter)
	}
	// Auto-classify error if category not already set.
	if entry.Category == "" && entry.Error != "" {
		entry.Category = ClassifyError(fmt.Errorf("%s", entry.Error)).String()
	}
	dlq.entries = append(dlq.entries, entry)
	// Graceful degradation: cap retained entries, evicting oldest-first so the
	// DLQ never grows without bound under a failure storm.
	if len(dlq.entries) > MaxDeadLetterEntries {
		trimmed := make([]DeadLetterEntry, MaxDeadLetterEntries)
		copy(trimmed, dlq.entries[len(dlq.entries)-MaxDeadLetterEntries:])
		dlq.entries = trimmed
	}
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

// Replay re-executes the entry with the given id through the configured
// executor and removes it ONLY on success — drop-safe (c8094002 ms1). The old
// Replay removed the entry and returned it for the caller to run: any caller
// without a tree runner, or one that crashed mid-replay, silently dropped the
// task. Without an executor, on an abandoned entry, or when the executor
// fails, the entry is retained. A failed replay clears RequeuedAt so the
// background scan does not hot-loop the same failure (Requeue counts the
// attempts that gate abandonment), and abandons the entry once its attempts
// are exhausted. Concurrent replays of the same id are refused while one is in
// flight; the executor runs without holding the queue lock (a replay is a full
// agent run and may take minutes).
func (dlq *DeadLetterQueue) Replay(id string) (*DeadLetterEntry, bool) {
	dlq.mu.Lock()
	if dlq.executor == nil || dlq.replaying[id] {
		dlq.mu.Unlock()
		return nil, false
	}
	var entry DeadLetterEntry
	found := false
	for _, e := range dlq.entries {
		if e.ID == id {
			entry, found = e, true
			break
		}
	}
	if !found || entry.Abandoned {
		dlq.mu.Unlock()
		return nil, false
	}
	if dlq.replaying == nil {
		dlq.replaying = make(map[string]bool)
	}
	dlq.replaying[id] = true
	executor := dlq.executor
	dlq.mu.Unlock()

	err := executor(entry)

	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	delete(dlq.replaying, id)
	for i := range dlq.entries {
		if dlq.entries[i].ID != id {
			continue
		}
		if err == nil {
			replayed := dlq.entries[i]
			dlq.entries = append(dlq.entries[:i], dlq.entries[i+1:]...)
			dlq.save()
			return &replayed, true
		}
		dlq.entries[i].RequeuedAt = time.Time{}
		dlq.entries[i].LastReplayAt = time.Now()
		dlq.entries[i].LastReplayError = err.Error()
		if dlq.entries[i].Attempts >= MaxReplayAttempts {
			dlq.entries[i].Abandoned = true
		}
		dlq.save()
		return nil, false
	}
	return nil, false
}

// Reload discards the in-memory view and re-reads the queue from its
// persistence file. The dashboard runs in a separate process from bt-agent's
// executor, so it must reload before mutating shared on-disk state — otherwise
// a stale in-memory copy would clobber entries the executor added or updated
// since the dashboard last read the file. No-op when persistence is disabled.
func (dlq *DeadLetterQueue) Reload() {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()
	if dlq.path == "" {
		return
	}
	dlq.entries = nil
	dlq.load()
}

// Requeue flags the entry with the given id for retry by stamping RequeuedAt and
// persisting, WITHOUT removing it from the queue. Unlike Replay (which removes
// and returns an entry for an in-process runner), Requeue lets a process with no
// tree runner — the dashboard — mark a dead-lettered task so bt-agent's executor
// picks it up on its next scan, instead of silently dropping it cross-process.
func (dlq *DeadLetterQueue) Requeue(id string) (*DeadLetterEntry, bool) {
	dlq.mu.Lock()
	defer dlq.mu.Unlock()

	for i := range dlq.entries {
		if dlq.entries[i].ID == id {
			// Poison-pill guard: an entry already abandoned, or one that has
			// exhausted its replay budget, must never be auto-requeued again.
			// Exhausting the budget terminally flags the entry Abandoned so it
			// is retained for inspection but excluded from further requeue,
			// breaking infinite replay loops.
			if dlq.entries[i].Abandoned {
				return nil, false
			}
			if dlq.entries[i].Attempts >= MaxReplayAttempts {
				dlq.entries[i].Abandoned = true
				dlq.save()
				return nil, false
			}
			dlq.entries[i].Attempts++
			dlq.entries[i].RequeuedAt = time.Now()
			dlq.save()
			e := dlq.entries[i]
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

// acquireFileLock takes an exclusive advisory flock on the sidecar
// `<path>.lock`, blocking until the lock is available, and unlinks the sidecar
// on release so no stray artifact is left beside the queue file. flock
// attaches to the open file description, so two separate opens of the same
// sidecar exclude each other even within one process — the same shape as the
// daemon/dashboard cross-process case. The lock is advisory and relies on
// Linux flock semantics (the platform target). The returned release func is
// safe to call more than once.
//
// This replicates the internal/evolution file-lock idiom locally: reliability
// imports zero internal packages, so it cannot borrow that helper. The
// unlink-on-release variant must re-verify the sidecar after acquisition: a
// waiter can acquire the flock on an inode the previous holder already
// unlinked, and that lock excludes nobody (a fresh open of the path creates a
// new inode), so it retries on the live path instead.
func acquireFileLock(path string) (func(), error) {
	lockPath := path + ".lock"
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			return nil, fmt.Errorf("open dlq lock %s: %w", lockPath, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			f.Close()
			return nil, fmt.Errorf("flock dlq lock %s: %w", lockPath, err)
		}
		held, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("stat dlq lock %s: %w", lockPath, err)
		}
		if current, err := os.Stat(lockPath); err != nil || !os.SameFile(held, current) {
			_ = f.Close() // locked an orphaned inode; retry on the live path
			continue
		}
		var once sync.Once
		release := func() {
			once.Do(func() {
				// Unlink before close so no waiter still blocked on this
				// inode can mistake it for the lock guarding the path.
				_ = os.Remove(lockPath)
				_ = f.Close() // closing the descriptor releases the flock
			})
		}
		return release, nil
	}
}

// mergeFromDisk folds sibling-process state from the shared persistence file
// into the in-memory entries before a save. The daemon, the dashboard, and MCP
// siblings each hold an independent DeadLetterQueue over the same file, so a
// whole-file rewrite from a view that predates a sibling's Requeue would erase
// its RequeuedAt stamp and the executor's next scan would never see the
// flagged task — replay stays dead cross-process. Membership stays
// memory-authoritative (Replay removes entries on success and Purge clears
// wholesale; resurrecting disk-only entries would undo both), while per-entry
// replay state merges monotonically:
//
//   - Requeue bumps Attempts every time it stamps RequeuedAt, so a strictly
//     higher on-disk Attempts marks the disk entry as the newer write — adopt
//     its Attempts and RequeuedAt together. At equal Attempts memory is at
//     least as recent, which preserves Replay's deliberate clear of RequeuedAt
//     after a failed replay (the scan hot-loop guard).
//   - Abandoned is terminal (nothing ever clears it), so it merges as OR: a
//     sibling's abandoned poison pill must not be resurrected into the
//     auto-requeue pool by a stale save.
//
// Callers must hold dlq.mu and the cross-process file lock.
func (dlq *DeadLetterQueue) mergeFromDisk() {
	data, err := os.ReadFile(dlq.path)
	if err != nil {
		return // no sibling state yet (first save)
	}
	var disk []DeadLetterEntry
	if err := json.Unmarshal(data, &disk); err != nil {
		return // corrupt on-disk state; the load path quarantines it
	}
	byID := make(map[string]DeadLetterEntry, len(disk))
	for _, e := range disk {
		byID[e.ID] = e
	}
	for i := range dlq.entries {
		d, ok := byID[dlq.entries[i].ID]
		if !ok {
			continue
		}
		if d.Attempts > dlq.entries[i].Attempts {
			dlq.entries[i].Attempts = d.Attempts
			dlq.entries[i].RequeuedAt = d.RequeuedAt
		}
		if d.Abandoned {
			dlq.entries[i].Abandoned = true
		}
	}
}

// save persists the queue per ADR-003 via the canonical util.SaveJSONAtomic
// helper: it writes a complete temp file in the same directory, then renames
// it over the destination. An in-place rewrite would let a crash mid-write
// leave a truncated queue at dlq.path; rename swaps a fully written file in
// one atomic step.
func (dlq *DeadLetterQueue) save() {
	if dlq.path == "" {
		return
	}
	// Serialize the read-merge-write against sibling processes sharing this
	// file, then fold their newer per-entry state into memory before writing
	// (see mergeFromDisk). A lock failure degrades to the merged-but-
	// unserialized write rather than dropping the save: an unserialized write
	// can still lose a concurrent sibling stamp, but an unsaved queue loses
	// this process's own entries for certain.
	if release, err := acquireFileLock(dlq.path); err == nil {
		defer release()
	} else {
		slog.Error("dlq: lock for merged save (writing unserialized)", "path", dlq.path, "error", err)
	}
	dlq.mergeFromDisk()
	if err := util.SaveJSONAtomic(dlq.path, dlq.entries); err != nil {
		slog.Error("dlq: atomic save failed", "path", dlq.path, "error", err)
	}
}

func (dlq *DeadLetterQueue) load() {
	data, err := os.ReadFile(dlq.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &dlq.entries); err != nil {
		// A corrupt persistence file must not silently become an empty queue:
		// the next save would persist the wipe over the only copy of the
		// dead-lettered tasks. Quarantine the payload beside the queue so the
		// queue restarts empty while the evidence survives subsequent saves.
		dlq.entries = nil
		quarantine := dlq.path + ".corrupt"
		if renameErr := os.Rename(dlq.path, quarantine); renameErr != nil {
			slog.Error("dlq: quarantine corrupt queue file", "path", dlq.path, "error", renameErr)
			return
		}
		slog.Error("dlq: corrupt queue file quarantined", "path", dlq.path, "quarantine", quarantine, "error", err)
	}
}

// ─── Worker Pool ────────────────────────────────────────────────────────────

// WorkerPool manages a fixed pool of goroutines for task execution.
type WorkerPool struct {
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
						slog.Error("workerpool: task panicked (worker recovered)", "panic", r)
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
	Outcome      string        `json:"outcome,omitempty"`
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
	// A failed executor's RESULT is preserved alongside its error: graceful
	// non-success runs (e.g. the goap rate-limit carryover) return a
	// populated result the caller needs for classification — dropping it
	// once turned a healthy pause into 3 retries and a dead-letter entry
	// (2026-07-16 20:30).
	var lastErr error
	var lastResult *AgentResult
	tried := 0
	for i := 0; i < len(executors) && tried < maxFailover; i++ {
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
		result, err := e.Execute(agent, task)
		if err == nil {
			// Success resets failure counter and clears cooldown.
			r.recordSuccess(idx)
			// Refresh heartbeat timestamp after successful execution.
			r.pingHeartbeatAfterSuccess(idx)
			return result, nil
		}
		// Record failure for zombie detection.
		r.recordFailure(idx)
		lastErr = err
		if result != nil {
			lastResult = result
		}
	}

	// If we have a specific error, include it; otherwise fall back to local
	if lastErr != nil {
		// Try local as last resort
		if r.local != nil {
			result, localErr := r.local.Execute(agent, task)
			if localErr == nil {
				return result, nil
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

// ─── Outcome Scoring ────────────────────────────────────────────────────────

// ScoreOutcome derives a 0-100 fitness score from execution state: the
// quality score scaled to a percentage, falling back to 75 (success) or 25
// (failure) when that scaled score is zero or below, then clamped to
// [0,100]. This is the canonical formula behind the block-fitness scoring
// duplicated across internal/blocks, internal/engine, and internal/dashboard.
func ScoreOutcome(outcome string, qualityScore float64, success bool) float64 {
	score := qualityScore * 100
	if score <= 0 {
		if success || strings.EqualFold(outcome, "success") || strings.EqualFold(outcome, "completed") {
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
