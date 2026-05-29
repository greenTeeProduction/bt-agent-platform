// Package agent provides job persistence for the scheduler.
// FileJobStore implements the JobStore interface using JSON file persistence,
// enabling scheduled jobs to survive bt-agent restarts.
package agent

import (
	"encoding/json"
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
}

// NewFileJobStore creates a file-backed job store.
// If path is empty, operations are no-ops (useful for in-memory-only mode).
func NewFileJobStore(path string) *FileJobStore {
	return &FileJobStore{path: path}
}

// Save serializes jobs to the JSON file. Creates parent directories as needed.
func (fs *FileJobStore) Save(jobs []ScheduledJob) error {
	if fs.path == "" {
		return nil
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()

	os.MkdirAll(filepath.Dir(fs.path), 0755)
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
func (fs *FileJobStore) Load() ([]ScheduledJob, error) {
	if fs.path == "" {
		return nil, nil
	}
	fs.mu.RLock()
	defer fs.mu.RUnlock()

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
	return jobs, nil
}
