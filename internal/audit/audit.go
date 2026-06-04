// Package audit provides append-only JSONL audit logging for agent tasks.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one audit log record.
type Entry struct {
	Timestamp time.Time         `json:"timestamp"`
	Agent     string            `json:"agent,omitempty"`
	Task      string            `json:"task,omitempty"`
	Action    string            `json:"action"`
	Detail    string            `json:"detail,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

var (
	mu       sync.Mutex
	baseDir  string
	taskPath string
)

// Init configures the audit log directory (e.g. ~/.go-bt-evolve).
func Init(base string) {
	mu.Lock()
	defer mu.Unlock()
	baseDir = base
	if base != "" {
		_ = os.MkdirAll(filepath.Join(base, "audit"), 0750)
		taskPath = filepath.Join(base, "audit", "task.jsonl")
	}
}

// Append writes one JSONL entry to the task audit log.
func Append(e Entry) error {
	mu.Lock()
	path := taskPath
	mu.Unlock()
	if path == "" {
		return fmt.Errorf("audit: not initialized")
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// TaskLogPath returns the configured task audit path.
func TaskLogPath() string {
	mu.Lock()
	defer mu.Unlock()
	return taskPath
}
