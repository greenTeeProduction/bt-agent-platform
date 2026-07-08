package persona

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Interaction is one observed unit of collaboration with the user — the raw
// signal habit mining runs on (ADR-010 Phase 1).
type Interaction struct {
	Task       string `json:"task"`
	TreeID     string `json:"tree_id,omitempty"`
	Outcome    string `json:"outcome,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	// Correction holds explicit user feedback text when the user amended or
	// re-asked; empty for plain runs.
	Correction string `json:"correction,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// Log is a per-user append-only JSONL interaction log, mirroring the agent
// History persistence style (one JSON object per line, append on write).
type Log struct {
	path string
	mu   sync.Mutex
}

// NewLog opens (or prepares) the interaction log inside a user workspace.
func NewLog(ws Workspace) (*Log, error) {
	if err := os.MkdirAll(ws.Root, 0755); err != nil {
		return nil, fmt.Errorf("interaction log: %w", err)
	}
	return &Log{path: ws.InteractionsPath()}, nil
}

// Append records one interaction. A zero Timestamp is filled with now.
func (l *Log) Append(rec Interaction) error {
	if rec.Timestamp == 0 {
		rec.Timestamp = time.Now().Unix()
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal interaction: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return fmt.Errorf("interaction log dir: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open interaction log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append interaction: %w", err)
	}
	return nil
}

// All reads every interaction in chronological (append) order. A missing
// file is an empty log, not an error; unparseable lines are skipped.
func (l *Log) All() ([]Interaction, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open interaction log: %w", err)
	}
	defer f.Close()

	var out []Interaction
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var rec Interaction
		if json.Unmarshal(scanner.Bytes(), &rec) == nil && rec.Task != "" {
			out = append(out, rec)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("scan interaction log: %w", err)
	}
	return out, nil
}

// Since returns the interactions with Timestamp >= cutoff.
func (l *Log) Since(cutoff time.Time) ([]Interaction, error) {
	all, err := l.All()
	if err != nil {
		return nil, err
	}
	cut := cutoff.Unix()
	var out []Interaction
	for _, rec := range all {
		if rec.Timestamp >= cut {
			out = append(out, rec)
		}
	}
	return out, nil
}
