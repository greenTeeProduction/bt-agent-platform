package reliability

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/util"
)

const (
	MaxDeadLetterEntries = 1000
	MaxReplayAttempts    = 5
	dlqTransactionBudget = 3 * time.Second
)

var (
	ErrDeadLetterNotFound = errors.New("dead letter entry not found")
	ErrReplayUnavailable  = errors.New("replay executor unavailable or already active")
	ErrReplayRecovery     = errors.New("replay requires explicit recovery reconciliation")
	ErrReplayExhausted    = errors.New("replay abandoned or attempts exhausted")
)

// DeadLetterEntry retains durable replay admission until its terminal record
// commits. A claim is not a lease that expires: lost ownership cannot prove
// whether an action occurred. Ordinary requeue, purge and eviction retain it.
type DeadLetterEntry struct {
	ID               string    `json:"id"`
	Task             string    `json:"task"`
	Agent            string    `json:"agent"`
	Error            string    `json:"error"`
	Attempts         int       `json:"attempts"`
	FailedAt         time.Time `json:"failed_at"`
	Circuit          string    `json:"circuit,omitempty"`
	Category         string    `json:"category,omitempty"`
	BuildRevision    string    `json:"build_revision,omitempty"`
	RequeuedAt       time.Time `json:"requeued_at,omitzero"`
	Abandoned        bool      `json:"abandoned,omitzero"`
	LastReplayAt     time.Time `json:"last_replay_at,omitzero"`
	LastReplayError  string    `json:"last_replay_error,omitempty"`
	ReplayClaim      string    `json:"replay_claim,omitempty"`
	ReplayInFlight   bool      `json:"replay_in_flight,omitempty"`
	RecoveryRequired bool      `json:"recovery_required,omitempty"`
	RecoveryReason   string    `json:"recovery_reason,omitempty"`
}

func (e DeadLetterEntry) held() bool {
	return e.ReplayClaim != "" || e.ReplayInFlight || e.RecoveryRequired
}

// DeadLetterQueue uses lock-protected deltas against current disk membership,
// never a stale whole-snapshot rewrite. Failed commits preserve observed state.
type DeadLetterQueue struct {
	mu        sync.Mutex
	entries   []DeadLetterEntry
	path      string
	executor  ReplayExecutor
	replaying map[string]bool
	lastErr   error
}

type ReplayExecutor func(entry DeadLetterEntry) error

func NewDeadLetterQueue(path string) *DeadLetterQueue {
	q := &DeadLetterQueue{path: path}
	_ = q.ReloadWithError()
	return q
}

func (q *DeadLetterQueue) SetReplayExecutor(fn ReplayExecutor) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.executor = fn
}

func (q *DeadLetterQueue) PersistenceError() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.lastErr
}

// readDisk retains malformed/unreadable bytes in place. Missing state is an
// empty initial queue; malformed state must not be overwritten as empty.
func (q *DeadLetterQueue) readDisk() ([]DeadLetterEntry, error) {
	if q.path == "" {
		return slices.Clone(q.entries), nil
	}
	root, name, err := util.OpenPersistenceRoot(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer root.Close()
	data, err := root.ReadFile(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []DeadLetterEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.ID == "" || seen[e.ID] || e.Attempts < 0 {
			return nil, fmt.Errorf("invalid or duplicate dead letter identity")
		}
		seen[e.ID] = true
	}
	return entries, nil
}

// transact requires q.mu. Publish a mutation only after its atomic replacement
// succeeds. Lock failures never permit an unserialized write.
func (q *DeadLetterQueue) transact(mutate func([]DeadLetterEntry) ([]DeadLetterEntry, error)) error {
	if q.path != "" {
		if err := util.EnsurePersistenceParent(q.path); err != nil {
			q.lastErr = err
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), dlqTransactionBudget)
		defer cancel()
		release, err := AcquireFileLockWithContext(ctx, q.path)
		if err != nil {
			q.lastErr = err
			return err
		}
		defer release()
	}
	base, err := q.readDisk()
	if err != nil {
		q.lastErr = err
		return err
	}
	q.entries = slices.Clone(base)
	q.lastErr = nil
	next, err := mutate(slices.Clone(base))
	if err != nil {
		return err
	}
	if q.path != "" {
		if err := util.SaveJSONAtomic(q.path, next); err != nil {
			q.lastErr = err
			return err
		}
	}
	q.entries = next
	return nil
}

func (q *DeadLetterQueue) ReloadWithError() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	entries, err := q.readDisk()
	q.lastErr = err
	if err == nil {
		q.entries = entries
	}
	return err
}

func (q *DeadLetterQueue) Reload() { _ = q.ReloadWithError() }

func (q *DeadLetterQueue) PushWithError(entry DeadLetterEntry) error {
	return q.push(entry, false)
}

// PushExecutionFailureWithError retains the original execution disposition.
// A terminal diagnostic is not a new replayable failure: side effects may
// already have occurred before the scheduler or tree inserted this record.
func (q *DeadLetterQueue) PushExecutionFailureWithError(entry DeadLetterEntry, executionErr error) error {
	if entry.held() {
		return fmt.Errorf("new failure cannot supply a replay claim")
	}
	if executionErr != nil {
		entry.Error = executionErr.Error()
	}
	if IsExecutionTerminalError(executionErr) {
		entry.ReplayClaim = rand.Text()
		entry.RecoveryRequired = true
		entry.RecoveryReason = "original execution terminal outcome requires reconciliation"
	}
	return q.push(entry, true)
}

func (q *DeadLetterQueue) push(entry DeadLetterEntry, allowHeld bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	entry.FailedAt = time.Now()
	if entry.ID == "" {
		entry.ID = entry.Agent + "-" + rand.Text()
	}
	if entry.Category == "" && entry.Error != "" {
		entry.Category = ClassifyError(errors.New(entry.Error)).String()
	}
	if entry.Attempts < 0 || (entry.held() && !allowHeld) {
		return fmt.Errorf("invalid new dead letter")
	}
	return q.transact(func(entries []DeadLetterEntry) ([]DeadLetterEntry, error) {
		for _, e := range entries {
			if e.ID == entry.ID {
				return nil, fmt.Errorf("duplicate dead letter id")
			}
		}
		entries = append(entries, entry)
		if len(entries) > MaxDeadLetterEntries {
			index := slices.IndexFunc(entries[:len(entries)-1], func(e DeadLetterEntry) bool { return !e.held() })
			if index < 0 {
				return nil, fmt.Errorf("dead letter capacity reserved by recovery claims")
			}
			entries = slices.Delete(entries, index, index+1)
		}
		return entries, nil
	})
}

// Push is the legacy fire-and-report wrapper; acknowledgement callers must
// use PushWithError. Failure does not publish an uncommitted cache entry.
func (q *DeadLetterQueue) Push(entry DeadLetterEntry) {
	if err := q.PushWithError(entry); err != nil {
		slog.Error("dlq: push not persisted", "error", err)
	}
}

func (q *DeadLetterQueue) RequeueWithError(id string) (*DeadLetterEntry, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out DeadLetterEntry
	var exhausted bool
	err := q.transact(func(entries []DeadLetterEntry) ([]DeadLetterEntry, error) {
		for i := range entries {
			if entries[i].ID != id {
				continue
			}
			if entries[i].held() {
				return nil, ErrReplayRecovery
			}
			if entries[i].Abandoned {
				return nil, ErrReplayExhausted
			}
			if entries[i].Attempts >= MaxReplayAttempts {
				entries[i].Abandoned = true
				exhausted = true
				return entries, nil
			}
			entries[i].Attempts++
			entries[i].RequeuedAt = time.Now()
			out = entries[i]
			return entries, nil
		}
		return nil, ErrDeadLetterNotFound
	})
	if err != nil {
		return nil, err
	}
	if exhausted {
		return nil, ErrReplayExhausted
	}
	return &out, nil
}

func (q *DeadLetterQueue) Requeue(id string) (*DeadLetterEntry, bool) {
	entry, err := q.RequeueWithError(id)
	return entry, err == nil
}

// ReplayWithError durably claims before dispatch and records before release.
// The error preserves completed versus uncertain work; neither permits retry.
func (q *DeadLetterQueue) ReplayWithError(id string) (*DeadLetterEntry, error) {
	q.mu.Lock()
	if q.executor == nil || q.replaying[id] {
		q.mu.Unlock()
		return nil, ErrReplayUnavailable
	}
	executor := q.executor
	claim := rand.Text()
	var admitted DeadLetterEntry
	err := q.transact(func(entries []DeadLetterEntry) ([]DeadLetterEntry, error) {
		for i := range entries {
			if entries[i].ID != id {
				continue
			}
			if entries[i].held() {
				return nil, ErrReplayRecovery
			}
			if entries[i].Abandoned {
				return nil, ErrReplayExhausted
			}
			entries[i].ReplayClaim, entries[i].ReplayInFlight = claim, true
			entries[i].RecoveryRequired = true
			entries[i].RecoveryReason = "replay admission awaits a recorded terminal outcome"
			admitted = entries[i]
			return entries, nil
		}
		return nil, ErrDeadLetterNotFound
	})
	if err != nil {
		q.mu.Unlock()
		return nil, fmt.Errorf("admit replay: %w", err)
	}
	if q.replaying == nil {
		q.replaying = make(map[string]bool)
	}
	q.replaying[id] = true
	q.mu.Unlock()

	var executionErr error
	func() {
		defer func() {
			if p := recover(); p != nil {
				executionErr = &ExecutionUncertainError{Err: fmt.Errorf("replay executor panicked: %v", p)}
			}
		}()
		executionErr = executor(admitted)
	}()

	q.mu.Lock()
	defer q.mu.Unlock()
	defer delete(q.replaying, id)
	result := admitted
	err = q.transact(func(entries []DeadLetterEntry) ([]DeadLetterEntry, error) {
		for i := range entries {
			if entries[i].ID != id {
				continue
			}
			if entries[i].ReplayClaim != claim {
				return nil, ErrReplayRecovery
			}
			if executionErr == nil {
				result = entries[i]
				result.ReplayClaim, result.RecoveryReason = "", ""
				result.ReplayInFlight, result.RecoveryRequired = false, false
				return slices.Delete(entries, i, i+1), nil
			}
			entries[i].LastReplayAt = time.Now()
			entries[i].LastReplayError = executionErr.Error()
			entries[i].RequeuedAt = time.Time{}
			entries[i].ReplayInFlight = false
			if IsExecutionTerminalError(executionErr) {
				entries[i].RecoveryReason = "terminal replay outcome requires explicit reconciliation"
			} else {
				entries[i].ReplayClaim, entries[i].RecoveryReason = "", ""
				entries[i].RecoveryRequired = false
				if entries[i].Attempts >= MaxReplayAttempts {
					entries[i].Abandoned = true
				}
			}
			result = entries[i]
			return entries, nil
		}
		return nil, ErrReplayRecovery
	})
	if err != nil {
		if executionErr == nil || (IsExecutionPersistenceError(executionErr) && !IsExecutionUncertainError(executionErr) && !IsExecutionStoppedError(executionErr)) {
			return &admitted, &ExecutionPersistenceError{Err: errors.Join(executionErr, err)}
		}
		return &admitted, &ExecutionUncertainError{Err: errors.Join(executionErr, err)}
	}
	return &result, executionErr
}

func (q *DeadLetterQueue) Replay(id string) (*DeadLetterEntry, bool) {
	entry, err := q.ReplayWithError(id)
	if err != nil {
		return nil, false
	}
	return entry, true
}

// ResolveReplayRecovery is a trusted Go reconciliation seam, not an operator
// transport. The caller must establish owner quiescence and completed or
// provably unstarted work. The exact claim prevents releasing a newer attempt.
// Neither resolution dispatches work; unstarted work needs a new requeue.
func (q *DeadLetterQueue) ResolveReplayRecovery(id, claim string, completed bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if claim == "" || q.replaying[id] {
		return ErrReplayRecovery
	}
	return q.transact(func(entries []DeadLetterEntry) ([]DeadLetterEntry, error) {
		for i := range entries {
			if entries[i].ID != id {
				continue
			}
			if !entries[i].held() || entries[i].ReplayClaim != claim {
				return nil, ErrReplayRecovery
			}
			if completed {
				return slices.Delete(entries, i, i+1), nil
			}
			entries[i].ReplayClaim, entries[i].RecoveryReason = "", ""
			entries[i].ReplayInFlight, entries[i].RecoveryRequired = false, false
			entries[i].RequeuedAt = time.Time{}
			return entries, nil
		}
		return nil, ErrDeadLetterNotFound
	})
}

// PurgeWithError removes only unclaimed entries. Ordinary maintenance cannot
// erase the sole replay fence for completed or uncertain work.
func (q *DeadLetterQueue) PurgeWithError() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	removed := 0
	err := q.transact(func(entries []DeadLetterEntry) ([]DeadLetterEntry, error) {
		kept := make([]DeadLetterEntry, 0, len(entries))
		for _, e := range entries {
			if e.held() {
				kept = append(kept, e)
			} else {
				removed++
			}
		}
		return kept, nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (q *DeadLetterQueue) Purge() { _, _ = q.PurgeWithError() }

func (q *DeadLetterQueue) List() []DeadLetterEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.entries)
}

func (q *DeadLetterQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

func (q *DeadLetterQueue) CategoryCounts() map[string]int {
	counts := make(map[string]int)
	for _, e := range q.List() {
		category := e.Category
		if category == "" {
			category = "unknown"
		}
		counts[category]++
	}
	return counts
}

func (q *DeadLetterQueue) RequeuedReady() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lastErr != nil {
		return nil
	}
	var ids []string
	for _, e := range q.entries {
		if !e.RequeuedAt.IsZero() && !e.Abandoned && !e.held() {
			ids = append(ids, e.ID)
		}
	}
	return ids
}
