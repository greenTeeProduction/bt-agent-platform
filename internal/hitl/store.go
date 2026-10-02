// Package hitl provides human-in-the-loop approval for behavior tree execution.
package hitl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	if err := util.EnsurePersistenceParent(filepath.Join(dir, "requests.json")); err != nil {
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

// ErrRequestNotFound and ErrInvalidStatus distinguish rejected decisions from
// storage failures at HTTP/MCP boundaries.
var (
	ErrRequestNotFound = errors.New("hitl: request not found")
	ErrInvalidStatus   = errors.New("hitl: request is not pending or escalated")
)

func (s *Store) readRecords() (map[string]*Request, error) {
	data, err := util.ReadPersistenceFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*Request), nil
		}
		return nil, err
	}
	var list []*Request
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	records := make(map[string]*Request, len(list))
	for _, r := range list {
		if r != nil && r.ID != "" {
			records[r.ID] = r
		}
	}
	return records, nil
}

func (s *Store) load() error {
	records, err := s.readRecords()
	if err == nil {
		s.records = records
	}
	return err
}

// hitlMaxStoredTerminal caps terminal requests, newest by UpdatedAt first.
// Pending and escalated requests are never dropped.
const hitlMaxStoredTerminal = 1000

// persistRecords builds retention state separately so a failed write cannot
// prune the live cache. Caller holds the in-process and sidecar locks.
func (s *Store) persistRecords(records map[string]*Request) (map[string]*Request, error) {
	list := make([]*Request, 0, len(records))
	var terminal []*Request
	for _, r := range records {
		if r.Status == StatusPending || r.Status == StatusEscalated {
			list = append(list, r)
		} else {
			terminal = append(terminal, r)
		}
	}
	slices.SortFunc(terminal, func(a, b *Request) int {
		if order := b.UpdatedAt.Compare(a.UpdatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	if len(terminal) > hitlMaxStoredTerminal {
		terminal = terminal[:hitlMaxStoredTerminal]
	}
	list = append(list, terminal...)
	if err := util.SaveJSONAtomic(s.path, list); err != nil {
		return nil, err
	}
	retained := make(map[string]*Request, len(list))
	for _, r := range list {
		retained[r.ID] = r
	}
	return retained, nil
}

// save is an internal snapshot writer; runtime callers use transaction.
func (s *Store) save() error {
	records, err := s.persistRecords(s.records)
	if err == nil {
		s.records = records
	}
	return err
}

// transaction reloads authoritative state under a bounded sidecar lock. Both
// mutex contention and file-lock contention obey the caller's shorter budget.
// The callback receives detached records; changes publish only after commit.
func (s *Store) transaction(ctx context.Context, update func(map[string]*Request) (bool, error)) error {
	if s == nil {
		return fmt.Errorf("hitl: store not initialized")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.mu.TryLock() {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	defer s.mu.Unlock()
	if err := util.EnsurePersistenceParent(s.path); err != nil {
		return err
	}
	release, err := reliability.AcquireFileLockWithContext(ctx, s.path)
	if err != nil {
		return err
	}
	defer release()
	records, err := s.readRecords()
	if err != nil {
		return err
	}
	changed, err := update(records)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if changed {
		records, err = s.persistRecords(records)
		if err != nil {
			return err
		}
	}
	s.records = records
	return nil
}

// Create adds a request, preserving a pre-existing approval on ID collision.
func (s *Store) Create(req *Request) error { return s.CreateWithContext(context.Background(), req) }
func (s *Store) CreateWithContext(ctx context.Context, req *Request) error {
	if req == nil || req.ID == "" {
		return fmt.Errorf("hitl: invalid request")
	}
	candidate := cloneRequest(req)
	candidate.UpdatedAt = time.Now()
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = candidate.UpdatedAt
	}
	err := s.transaction(ctx, func(records map[string]*Request) (bool, error) {
		if _, exists := records[candidate.ID]; exists {
			return false, fmt.Errorf("hitl: request %q already exists", candidate.ID)
		}
		records[candidate.ID] = candidate
		return true, nil
	})
	if err == nil {
		req.CreatedAt, req.UpdatedAt = candidate.CreatedAt, candidate.UpdatedAt
	}
	return err
}

// Get returns a detached request. The compatibility wrapper omits read errors;
// external boundaries and approval waits use GetWithContext.
func (s *Store) Get(id string) (*Request, bool) {
	req, ok, _ := s.GetWithContext(context.Background(), id)
	return req, ok
}
func (s *Store) GetWithContext(ctx context.Context, id string) (*Request, bool, error) {
	var req *Request
	err := s.transaction(ctx, func(records map[string]*Request) (bool, error) {
		if r := records[id]; r != nil {
			req = cloneRequest(r)
		}
		return false, nil
	})
	if err != nil {
		return nil, false, err
	}
	return req, req != nil, nil
}

func expireRequests(records map[string]*Request) bool {
	now := time.Now()
	changed := false
	for _, r := range records {
		if (r.Status == StatusPending || r.Status == StatusEscalated) && !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
			r.Status, r.UpdatedAt = StatusExpired, now
			changed = true
		}
	}
	return changed
}

func (s *Store) ListPending() []*Request {
	list, _ := s.ListPendingWithContext(context.Background())
	return list
}
func (s *Store) ListPendingWithContext(ctx context.Context) ([]*Request, error) {
	return s.listWithContext(ctx, StatusPending, true)
}
func (s *Store) ListAll() []*Request {
	list, _ := s.ListAllWithContext(context.Background())
	return list
}
func (s *Store) ListAllWithContext(ctx context.Context) ([]*Request, error) {
	return s.listWithContext(ctx, "", false)
}
func (s *Store) listWithContext(ctx context.Context, status Status, expire bool) ([]*Request, error) {
	out := make([]*Request, 0)
	err := s.transaction(ctx, func(records map[string]*Request) (bool, error) {
		changed := expire && expireRequests(records)
		for _, r := range records {
			if status == "" || r.Status == status {
				out = append(out, cloneRequest(r))
			}
		}
		return changed, nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b *Request) int {
		if order := b.CreatedAt.Compare(a.CreatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (s *Store) Approve(id, reviewer, comment string) (*Request, error) {
	return s.ApproveWithContext(context.Background(), id, reviewer, comment)
}
func (s *Store) ApproveWithContext(ctx context.Context, id, reviewer, comment string) (*Request, error) {
	return s.decideWithContext(ctx, id, "", reviewer, comment, StatusApproved)
}
func (s *Store) Reject(id, reviewer, reason string) (*Request, error) {
	return s.RejectWithContext(context.Background(), id, reviewer, reason)
}
func (s *Store) RejectWithContext(ctx context.Context, id, reviewer, reason string) (*Request, error) {
	return s.decideWithContext(ctx, id, "", reviewer, reason, StatusRejected)
}

// decideWithContext selects task requests and commits under the same lock,
// avoiding a lookup/decision gap across independent stores.
func (s *Store) decideWithContext(ctx context.Context, id, taskID, reviewer, reason string, status Status) (*Request, error) {
	var result *Request
	err := s.transaction(ctx, func(records map[string]*Request) (bool, error) {
		r := records[id]
		if taskID != "" {
			r = latestTaskRequest(records, taskID, time.Now(), false)
			if r != nil && r.Status == status {
				result = cloneRequest(r)
				return false, nil
			}

		}
		if r == nil {
			return false, fmt.Errorf("%w: %q", ErrRequestNotFound, id+taskID)
		}
		if r.Status != StatusPending && r.Status != StatusEscalated {
			return false, fmt.Errorf("%w: %q is %s", ErrInvalidStatus, r.ID, r.Status)
		}
		now := time.Now()
		if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
			return false, fmt.Errorf("%w: %q has expired", ErrInvalidStatus, r.ID)
		}
		r.Status, r.Reviewer, r.Reason, r.UpdatedAt = status, reviewer, reason, now
		if status == StatusApproved {
			r.ApprovedAt = &now
		}
		if status == StatusRejected {
			r.RejectedAt = &now
		}
		result = cloneRequest(r)
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) RefreshStatus(id string) (Status, error) {
	req, err := s.RefreshRequestWithContext(context.Background(), id)
	if err != nil {
		return "", err
	}
	return req.Status, nil
}

// RefreshRequestWithContext returns status and payload from one transaction,
// and never returns an expiry whose write failed.
func (s *Store) RefreshRequestWithContext(ctx context.Context, id string) (*Request, error) {
	var req *Request
	err := s.transaction(ctx, func(records map[string]*Request) (bool, error) {
		r := records[id]
		if r == nil {
			return false, fmt.Errorf("%w: %q", ErrRequestNotFound, id)
		}
		changed := false
		if (r.Status == StatusPending || r.Status == StatusEscalated) && !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt) {
			r.Status, r.UpdatedAt = StatusExpired, time.Now()
			changed = true
		}
		req = cloneRequest(r)
		return changed, nil
	})
	if err != nil {
		return nil, err
	}
	return req, nil
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
