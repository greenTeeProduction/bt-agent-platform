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
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/util"
)

// Outcome is the result of a task execution.
type Outcome string

const (
	Success Outcome = "success"
	Partial Outcome = "partial"
	Failure Outcome = "failure"
)

// ResultCheck records an executed gate's verdict and the output it checked.
type ResultCheck struct {
	Gate         string `json:"gate"`
	Passed       bool   `json:"passed"`
	OutputDigest string `json:"output_digest"`
	Reason       string `json:"reason,omitempty"`
}

// Record captures evidence with its origin and execution identity.
type Record struct {
	Effects             []EffectReceipt `json:"effects,omitempty"`
	ResultChecks        []ResultCheck   `json:"result_checks,omitempty"`
	ResultChecksDropped int             `json:"result_checks_dropped,omitempty"`
	RunID               string          `json:"run_id,omitempty"`
	TreeVersion         string          `json:"tree_version,omitempty"`
	ExecutionVersions   []string        `json:"execution_versions,omitempty"`
	EvidenceKind        string          `json:"evidence_kind,omitempty"`
	Result              string          `json:"result,omitempty"`
	QualityScore        float64         `json:"quality_score,omitempty"`
	Path                string          `json:"path,omitempty"`
	Error               string          `json:"error,omitempty"`
	TaskID              string          `json:"task_id"`
	Timestamp           int64           `json:"timestamp"`
	Task                string          `json:"task"`
	Plan                string          `json:"plan"`
	TreeName            string          `json:"tree_name,omitempty"`
	WhatWentWell        []string        `json:"what_went_well"`
	WhatToImprove       []string        `json:"what_to_improve"`
	AdjustedBehavior    string          `json:"adjusted_behavior"`
	Outcome             Outcome         `json:"outcome"`
	DurationMs          int64           `json:"duration_ms"`
	// User attributes the record to a persona (ADR-133 Phase 5); empty for
	// anonymous/system runs.
	User string `json:"user,omitempty"`
	// UserFeedback carries an explicit satisfaction signal from the user
	// ("positive" or "negative", via bt_feedback). Records with feedback
	// feed the user_satisfaction fitness dimension; empty means the record
	// is a plain run reflection.
	UserFeedback string `json:"user_feedback,omitempty"`
}

// Explicit user-feedback signals stored in Record.UserFeedback.
const (
	FeedbackPositive = "positive"
	FeedbackNegative = "negative"
)

// FilterByTreeName returns only records attributed to the requested tree.
// Missing history is not permission to borrow unrelated or untagged runs.
func FilterByTreeName(records []Record, treeName string) []Record {
	return FilterByTreeNameStrict(records, treeName)
}

// FilterByTreeNameStrict returns only records whose TreeName matches exactly,
// with no backward-compat fallback. Personal trees (ADR-133 Phase 5) must be
// evaluated on their own evidence: inheriting the global record pool would
// hide a missing history from the gardener's evidence gate and score the tree
// on other trees' runs.
func FilterByTreeNameStrict(records []Record, treeName string) []Record {
	var filtered []Record
	for _, r := range records {
		if r.TreeName == treeName {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// FilterByTreeOwner selects evidence for one exact tree/owner pair. Empty
// owner denotes shared execution; it is never a wildcard for personal runs.
// Unattributed legacy records remain inspectable without being assigned to
// an arbitrary personal tree.
func FilterByTreeOwner(records []Record, treeName, user string) []Record {
	var filtered []Record
	if treeName == "" {
		return filtered
	}
	for _, r := range records {
		if r.TreeName == treeName && r.User == user {
			filtered = append(filtered, r)
		}
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

// reflectionFilePrefix is the filename prefix Save uses for reflection
// record files. The store directory is shared with sibling stores (the
// tree Store's tree.json / tree-<id>.json, the evaluator's
// transposition.json), so LoadAll must filter to this prefix rather than
// globbing every *.json file, or those siblings unmarshal as zero-value
// phantom records.
const reflectionFilePrefix = "reflection-"

// Save writes a record to a JSON file.
func (s *Store) Save(r *Record) error {
	r.Timestamp = time.Now().UnixMilli()
	if r.TaskID == "" {
		r.TaskID = fmt.Sprintf("task-%d", r.Timestamp)
	}
	path := filepath.Join(s.dir, fmt.Sprintf("%s%s.json", reflectionFilePrefix, r.TaskID))
	return util.SaveJSONAtomic(path, r)
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
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" || !strings.HasPrefix(e.Name(), reflectionFilePrefix) {
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
	for _, r := range ExecutionRecords(records) {
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
	records = ExecutionRecords(records)
	var failures []Record
	for i := len(records) - 1; i >= 0 && len(failures) < n; i-- {
		if records[i].Outcome == Failure {
			failures = append(failures, records[i])
		}
	}
	return failures
}
