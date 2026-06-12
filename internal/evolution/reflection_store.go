// Package reflection provides persistent storage for behavior tree
// execution records. Each Record captures the task, outcome, plan,
// timestamp, and structured reflection (what went well, what to improve).
// Records are stored as JSON files for post-hoc analysis and evolution.
package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Outcome is the result of a task execution.
type Outcome string

const (
	Success Outcome = "success"
	Partial Outcome = "partial"
	Failure Outcome = "failure"
)

// Record captures a completed task execution and its reflection.
type Record struct {
	TaskID           string   `json:"task_id"`
	Timestamp        int64    `json:"timestamp"`
	Task             string   `json:"task"`
	Plan             string   `json:"plan"`
	TreeName         string   `json:"tree_name,omitempty"`
	WhatWentWell     []string `json:"what_went_well"`
	WhatToImprove    []string `json:"what_to_improve"`
	AdjustedBehavior string   `json:"adjusted_behavior"`
	Outcome          Outcome  `json:"outcome"`
	DurationMs       int64    `json:"duration_ms"`
}

// FilterByTreeName returns records matching the given tree name.
// An empty treeName matches records that have no TreeName set (backward compat).
func FilterByTreeName(records []Record, treeName string) []Record {
	if treeName == "" {
		return records
	}
	var filtered []Record
	for _, r := range records {
		if r.TreeName == treeName {
			filtered = append(filtered, r)
		}
	}
	// If no records match, return all records (backward compat — before TreeName was populated)
	if len(filtered) == 0 {
		return records
	}
	return filtered
}

// Store persists reflection records as JSON files.
type Store struct {
	dir string
}

// NewStore creates a Store at the given directory (created if needed).
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the store's directory path.
func (s *Store) Dir() string { return s.dir }

// Save writes a record to a JSON file.
func (s *Store) Save(r *Record) error {
	r.Timestamp = time.Now().UnixMilli()
	if r.TaskID == "" {
		r.TaskID = fmt.Sprintf("task-%d", r.Timestamp)
	}
	path := filepath.Join(s.dir, fmt.Sprintf("reflection-%s.json", r.TaskID))
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	// Atomic write
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// LoadAll reads all reflection records from the store directory.
func (s *Store) LoadAll() ([]Record, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read dir: %w", err)
	}
	records := make([]Record, 0, 64)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r Record
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		records = append(records, r)
	}
	return records, nil
}

// CountFailures returns the number of failure records.
func (s *Store) CountFailures() int {
	records, err := s.LoadAll()
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range records {
		if r.Outcome == Failure {
			n++
		}
	}
	return n
}

// RecentFailures returns the last N failure records.
func (s *Store) RecentFailures(n int) []Record {
	records, err := s.LoadAll()
	if err != nil {
		return nil
	}
	var failures []Record
	for i := len(records) - 1; i >= 0 && len(failures) < n; i-- {
		if records[i].Outcome == Failure {
			failures = append(failures, records[i])
		}
	}
	return failures
}
