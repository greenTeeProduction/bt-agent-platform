package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// NotebookLMState tracks idempotency across agent runs to prevent duplicate
// research queries and source imports.
type NotebookLMState struct {
	mu         sync.RWMutex
	Queries    map[string]time.Time `json:"queries"` // query_hash → last_queried_at
	Sources    map[string]time.Time `json:"sources"` // source_url → imported_at
	LastRun    time.Time            `json:"last_run"`
	TotalRuns  int                  `json:"total_runs"`
	TotalDupes int                  `json:"total_dupes"`
	path       string
}

const nlmStateDir = "/mnt/ssd/clawd/wiki/bt-research/state"
const nlmStateFile = "notebooklm-state.json"

// LoadNotebookLMState loads the state from disk, or creates a fresh one.
func LoadNotebookLMState() (*NotebookLMState, error) {
	state := &NotebookLMState{
		Queries: make(map[string]time.Time),
		Sources: make(map[string]time.Time),
	}

	p := filepath.Join(nlmStateDir, nlmStateFile)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil // fresh state
		}
		return state, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(data, state); err != nil {
		return state, fmt.Errorf("parse state: %w", err)
	}
	if state.Queries == nil {
		state.Queries = make(map[string]time.Time)
	}
	if state.Sources == nil {
		state.Sources = make(map[string]time.Time)
	}
	state.path = p
	return state, nil
}

// Save persists the state to disk.
func (s *NotebookLMState) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p := s.path
	if p == "" {
		p = filepath.Join(nlmStateDir, nlmStateFile)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(p, data, 0644)
}

// QueryHash returns a stable hash for a research query string.
func QueryHash(query string) string {
	h := sha256.Sum256([]byte(query))
	return fmt.Sprintf("%x", h[:8])
}

// IsDuplicateQuery returns true if the query was already processed within the
// cooldown window (default: 7 days).
func (s *NotebookLMState) IsDuplicateQuery(query string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	last, ok := s.Queries[QueryHash(query)]
	if !ok {
		return false
	}
	return time.Since(last) < 7*24*time.Hour
}

// IsDuplicateSource returns true if the source URL was already imported within
// the cooldown window (default: 30 days).
func (s *NotebookLMState) IsDuplicateSource(url string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	last, ok := s.Sources[url]
	if !ok {
		return false
	}
	return time.Since(last) < 30*24*time.Hour
}

// MarkQueried records that a query was executed.
func (s *NotebookLMState) MarkQueried(query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Queries[QueryHash(query)] = time.Now()
}

// MarkImported records that a source URL was imported.
func (s *NotebookLMState) MarkImported(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Sources[url] = time.Now()
}

// MarkRun records a completed run.
func (s *NotebookLMState) MarkRun() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastRun = time.Now()
	s.TotalRuns++
}

// MarkDupe records a skipped duplicate.
func (s *NotebookLMState) MarkDupe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalDupes++
}

// Summary returns a human-readable state summary.
func (s *NotebookLMState) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("NotebookLM State: %d runs, %d dupes skipped, %d unique queries, %d unique sources, last run: %s",
		s.TotalRuns, s.TotalDupes, len(s.Queries), len(s.Sources), s.LastRun.Format(time.RFC3339))
}

// ─── BT Action handlers ─────────────────────────────────────────────────────

// loadNotebookLMStateAction loads the NotebookLM state from disk into the blackboard.
func loadNotebookLMStateAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	state, err := LoadNotebookLMState()
	if err != nil {
		bb.ChainState["nlm_state_error"] = err.Error()
		return -1
	}
	if bb.ChainState == nil {
		bb.ChainState = make(map[string]any)
	}
	bb.ChainState["nlm_state"] = state
	bb.ChainState["nlm_state_summary"] = state.Summary()
	return 1
}

// saveNotebookLMStateAction persists the state and records the run.
func saveNotebookLMStateAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	if bb.ChainState == nil {
		return 1
	}
	state, ok := bb.ChainState["nlm_state"].(*NotebookLMState)
	if !ok || state == nil {
		return 1
	}
	state.MarkRun()
	if err := state.Save(); err != nil {
		bb.ChainState["nlm_state_error"] = err.Error()
	}
	return 1
}
