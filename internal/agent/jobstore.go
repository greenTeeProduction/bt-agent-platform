// Package agent provides job persistence for the scheduler.
// FileJobStore implements the JobStore interface using JSON file persistence,
// enabling scheduled jobs to survive bt-agent restarts.
package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// JobStore persists scheduled jobs across process restarts.
// Implementations can be file-based, Redis-backed, or database-backed.
type JobStore interface {
	// Save persists the given jobs, replacing any existing state.
	Save(jobs []ScheduledJob) error
	// Load returns previously persisted jobs, or an empty slice if none exist.
	Load() ([]ScheduledJob, error)
}

// FileJobStore persists scheduled jobs as JSON to a file on disk.
// Thread-safe — all reads/writes hold a mutex.
type FileJobStore struct {
	mu   sync.RWMutex
	path string
	// sawNonEmpty records whether THIS process has ever loaded or saved a
	// non-empty job table. A process that never saw the jobs (e.g. a sibling
	// whose Load raced the daemon's atomic rename and found nothing) must not
	// erase them; a process that owned them may legitimately empty the table
	// (RemoveJob of the last job, agent deletion).
	sawNonEmpty bool
}

// NewFileJobStore creates a file-backed job store.
// If path is empty, operations are no-ops (useful for in-memory-only mode).
func NewFileJobStore(path string) *FileJobStore {
	return &FileJobStore{path: path}
}

// Save serializes jobs to the JSON file. Creates parent directories as needed.
//
// Clobber guard (2026-07-15): an empty job table is never written over a
// non-empty one. A sibling/CLI process whose scheduler holds zero jobs (e.g.
// after racing the daemon's atomic rename, or an empty-registry reconcile)
// was observed overwriting the live scheduler-jobs.json with a literal [] —
// the long-unattributed job-table wipe. Losing that save is harmless (the
// daemon rewrites the file on its next cycle); losing the table is not.
func (fs *FileJobStore) Save(jobs []ScheduledJob) error {
	if fs.path == "" {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if len(jobs) == 0 && !fs.sawNonEmpty {
		if fi, err := os.Stat(fs.path); err == nil && fi.Size() > 4 {
			slog.Warn("jobstore: refusing to overwrite a non-empty job table with an empty one this process never loaded",
				"path", fs.path, "existing_size", fi.Size())
			return nil
		}
	}
	if len(jobs) > 0 {
		fs.sawNonEmpty = true
	}

	_ = os.MkdirAll(filepath.Dir(fs.path), 0755)
	tmp := fs.path + ".tmp"
	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, fs.path)
}

// Load reads jobs from the JSON file. Returns empty slice if file doesn't exist.
// Takes the write lock: a successful non-empty load marks sawNonEmpty.
func (fs *FileJobStore) Load() ([]ScheduledJob, error) {
	if fs.path == "" {
		return nil, nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var jobs []ScheduledJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		fs.sawNonEmpty = true
	}
	return jobs, nil
}

// ReadOnlyJobStore wraps a JobStore so sibling bt-agent processes (MCP/CLI
// instances spawned next to the daemon) can see the daemon's job table without
// ever writing it. Sibling saves were the attributed 2026-07-15 job-table
// wiper: each sibling runs its own scheduler against the shared file and its
// saveState clobbered the daemon's state (fresh job IDs, reset run counters,
// or a plain []).
type ReadOnlyJobStore struct {
	base JobStore
}

// NewReadOnlyJobStore wraps base with a write-dropping JobStore.
func NewReadOnlyJobStore(base JobStore) *ReadOnlyJobStore {
	return &ReadOnlyJobStore{base: base}
}

// Load delegates to the wrapped store.
func (r *ReadOnlyJobStore) Load() ([]ScheduledJob, error) {
	if r.base == nil {
		return nil, nil
	}
	return r.base.Load()
}

// Save is a silent no-op: sibling processes observe, the daemon owns writes.
func (r *ReadOnlyJobStore) Save([]ScheduledJob) error { return nil }
