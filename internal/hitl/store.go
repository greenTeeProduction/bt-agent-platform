// Package hitl provides human-in-the-loop approval for behavior tree execution.
package hitl

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"

	"github.com/nico/go-bt-evolve/internal/util"
)

// Status of an approval request.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
	StatusExpired  Status = "expired"
	StatusSkipped  Status = "skipped" // auto-approved by policy
)

// Request is a single human approval checkpoint.
type Request struct {
	ID         string            `json:"id"`
	Status     Status            `json:"status"`
	NodeName   string            `json:"node_name"`
	NodeType   string            `json:"node_type"`
	Prompt     string            `json:"prompt"`
	Task       string            `json:"task"`
	Plan       string            `json:"plan"`
	Proposed   string            `json:"proposed"` // what the agent wants to do / current result preview
	Context    map[string]string `json:"context,omitempty"`
	Reviewer   string            `json:"reviewer,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	ExpiresAt  time.Time         `json:"expires_at"`
	ApprovedAt *time.Time        `json:"approved_at,omitempty"`
	RejectedAt *time.Time        `json:"rejected_at,omitempty"`
	AgentName  string            `json:"agent_name,omitempty"`
	TreeID     string            `json:"tree_id,omitempty"`
	TaskID     string            `json:"task_id,omitempty"`
	Phase      string            `json:"phase,omitempty"`
}

// Store persists approval requests.
type Store struct {
	mu      sync.RWMutex
	path    string
	records map[string]*Request
}

// DefaultStore is the process-wide HITL store (initialized from main).
var DefaultStore *Store

// InitStore creates or loads the default store under dir/hitl/.
func InitStore(baseDir string) (*Store, error) {
	dir := filepath.Join(baseDir, "hitl")
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	s := &Store{
		path:    filepath.Join(dir, "requests.json"),
		records: make(map[string]*Request),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	DefaultStore = s
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var list []*Request
	if err := json.Unmarshal(data, &list); err != nil {
		return err
	}
	s.records = make(map[string]*Request, len(list))
	for _, r := range list {
		if r != nil && r.ID != "" {
			s.records[r.ID] = r
		}
	}
	return nil
}

// hitlMaxStoredTerminal caps how many TERMINAL requests (skipped, expired,
// approved, rejected) save() retains — newest by UpdatedAt win, and pending
// requests are never dropped. The store previously kept every request forever
// (1,514 requests / 9.4 MB on the production box by 2026-07-09, 99% of them
// auto-skipped).
const hitlMaxStoredTerminal = 1000

func (s *Store) save() error {
	list := make([]*Request, 0, len(s.records))
	var terminal []*Request
	for _, r := range s.records {
		if r.Status == StatusPending || r.Status == StatusEscalated {
			list = append(list, r)
			continue
		}
		terminal = append(terminal, r)
	}
	if len(terminal) > hitlMaxStoredTerminal {
		slices.SortFunc(terminal, func(a, b *Request) int {
			return b.UpdatedAt.Compare(a.UpdatedAt)
		})
		for _, dropped := range terminal[hitlMaxStoredTerminal:] {
			delete(s.records, dropped.ID)
		}
		terminal = terminal[:hitlMaxStoredTerminal]
	}
	list = append(list, terminal...)
	if err := util.SaveJSONAtomic(s.path, list); err != nil {
		return err
	}
	// Requests can carry sensitive review context; tighten from
	// SaveJSONAtomic's 0644 default to the store's original 0600.
	return os.Chmod(s.path, 0600)
}

// Create adds a new pending request.
func (s *Store) Create(req *Request) error {
	if req == nil || req.ID == "" {
		return fmt.Errorf("hitl: invalid request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return err
	}
	defer release()
	req.UpdatedAt = time.Now()
	if req.CreatedAt.IsZero() {
		req.CreatedAt = req.UpdatedAt
	}
	s.records[req.ID] = cloneRequest(req)
	return s.save()
}

// Get returns a request by ID.
func (s *Store) Get(id string) (*Request, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return nil, false
	}
	defer release()
	r, ok := s.records[id]
	if !ok {
		return nil, false
	}
	cp := *cloneRequest(r)
	return &cp, true
}

// ListPending returns all pending (non-expired) requests.
func (s *Store) ListPending() []*Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return nil
	}
	defer release()
	now := time.Now()
	out := make([]*Request, 0, len(s.records))
	for _, r := range s.records {
		if r.Status != StatusPending {
			continue
		}
		if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
			r.Status = StatusExpired
			r.UpdatedAt = now
			continue
		}
		cp := *cloneRequest(r)
		out = append(out, &cp)
	}
	_ = s.save()
	return out
}

// ListAll returns all requests newest first.
func (s *Store) ListAll() []*Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return nil
	}
	defer release()
	out := make([]*Request, 0, len(s.records))
	for _, r := range s.records {
		cp := *cloneRequest(r)
		out = append(out, &cp)
	}
	return out
}

// Approve marks a request approved.
func (s *Store) Approve(id, reviewer, comment string) (*Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return nil, err
	}
	defer release()
	r, ok := s.records[id]
	if !ok {
		return nil, fmt.Errorf("hitl: request %q not found", id)
	}
	if r.Status != StatusPending && r.Status != StatusEscalated {
		return nil, fmt.Errorf("hitl: request %q is %s, not pending or escalated", id, r.Status)
	}
	now := time.Now()
	r.Status = StatusApproved
	r.Reviewer = reviewer
	r.Reason = comment
	r.ApprovedAt = &now
	r.UpdatedAt = now
	if err := s.save(); err != nil {
		return nil, err
	}
	cp := *cloneRequest(r)
	return &cp, nil
}

// Reject marks a request rejected.
func (s *Store) Reject(id, reviewer, reason string) (*Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return nil, err
	}
	defer release()
	r, ok := s.records[id]
	if !ok {
		return nil, fmt.Errorf("hitl: request %q not found", id)
	}
	if r.Status != StatusPending && r.Status != StatusEscalated {
		return nil, fmt.Errorf("hitl: request %q is %s, not pending or escalated", id, r.Status)
	}
	now := time.Now()
	r.Status = StatusRejected
	r.Reviewer = reviewer
	r.Reason = reason
	r.RejectedAt = &now
	r.UpdatedAt = now
	if err := s.save(); err != nil {
		return nil, err
	}
	cp := *cloneRequest(r)
	return &cp, nil
}

// RefreshStatus applies expiry to a single request and returns current status.
func (s *Store) RefreshStatus(id string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := s.reloadLocked()
	if err != nil {
		return "", err
	}
	defer release()
	r, ok := s.records[id]
	if !ok {
		return "", fmt.Errorf("hitl: request %q not found", id)
	}
	if r.Status == StatusPending && !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt) {
		r.Status = StatusExpired
		r.UpdatedAt = time.Now()
		if err := s.save(); err != nil {
			return "", err
		}
	}
	return r.Status, nil
}

// Caller holds mu. The file lock covers the authoritative reload and every
// subsequent read/modify/save, so independently opened daemon stores agree.
func (s *Store) reloadLocked() (func(), error) {
	release, err := reliability.AcquireFileLock(s.path)
	if err != nil {
		return nil, err
	}
	if err = s.load(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}
func cloneRequest(r *Request) *Request {
	cp := *r
	cp.Context = maps.Clone(r.Context)
	if r.ApprovedAt != nil {
		v := *r.ApprovedAt
		cp.ApprovedAt = &v
	}
	if r.RejectedAt != nil {
		v := *r.RejectedAt
		cp.RejectedAt = &v
	}
	return &cp
}
