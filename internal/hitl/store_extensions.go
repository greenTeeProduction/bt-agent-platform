package hitl

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// StatusEscalated indicates operator escalation.
const StatusEscalated Status = "escalated"

// SetTaskID attaches a workflow task id for lookup via ApproveByTaskID.
func (r *Request) SetTaskID(taskID string) {
	if r == nil {
		return
	}
	if r.Context == nil {
		r.Context = map[string]string{}
	}
	r.Context["task_id"] = taskID
	r.TaskID = taskID
}

// FindPendingByTaskID returns the newest pending or escalated request for a
// task id, so ApproveByTaskID/RejectByTaskID can resolve an escalation.
func (s *Store) FindPendingByTaskID(taskID string) (*Request, bool) {
	if s == nil || strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	var best *Request
	for _, r := range s.records {
		if r.Status != StatusPending && r.Status != StatusEscalated {
			continue
		}
		if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
			r.Status = StatusExpired
			r.UpdatedAt = now
			continue
		}
		tid := r.TaskID
		if tid == "" && r.Context != nil {
			tid = r.Context["task_id"]
		}
		if tid != taskID {
			continue
		}
		if best == nil || r.CreatedAt.After(best.CreatedAt) {
			best = r
		}
	}
	_ = s.save()
	if best == nil {
		return nil, false
	}
	cp := *best
	return &cp, true
}

// WaitForRequest polls until the request leaves pending state or ctx is cancelled.
func (s *Store) WaitForRequest(ctx context.Context, id string, pollEvery time.Duration) (*Request, error) {
	if s == nil {
		return nil, fmt.Errorf("hitl: store not initialized")
	}
	if pollEvery <= 0 {
		pollEvery = 500 * time.Millisecond
	}
	for {
		st, err := s.RefreshStatus(id)
		if err != nil {
			return nil, err
		}
		switch st {
		case StatusApproved, StatusSkipped, StatusEscalated:
			req, ok := s.Get(id)
			if !ok {
				return nil, fmt.Errorf("hitl: request %q not found", id)
			}
			return req, nil
		case StatusRejected, StatusExpired:
			req, _ := s.Get(id)
			return req, fmt.Errorf("hitl: request %s", st)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// WaitForTaskID polls the latest request for taskID until resolved.
func (s *Store) WaitForTaskID(ctx context.Context, taskID string, pollEvery time.Duration) (*Request, error) {
	req, ok := s.FindPendingByTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("hitl: no pending request for task %q", taskID)
	}
	return s.WaitForRequest(ctx, req.ID, pollEvery)
}

// ApproveByTaskID approves the latest pending request for taskID.
func (s *Store) ApproveByTaskID(taskID, reviewer, comment string) (*Request, error) {
	req, ok := s.FindPendingByTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("hitl: no pending request for task %q", taskID)
	}
	return s.Approve(req.ID, reviewer, comment)
}

// RejectByTaskID rejects the latest pending request for taskID.
func (s *Store) RejectByTaskID(taskID, reviewer, reason string) (*Request, error) {
	req, ok := s.FindPendingByTaskID(taskID)
	if !ok {
		return nil, fmt.Errorf("hitl: no pending request for task %q", taskID)
	}
	return s.Reject(req.ID, reviewer, reason)
}

// Escalate marks a request escalated and returns a copy.
func (s *Store) Escalate(id, reviewer, reason string) (*Request, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[id]
	if !ok {
		return nil, fmt.Errorf("hitl: request %q not found", id)
	}
	now := time.Now()
	r.Status = StatusEscalated
	r.Reviewer = reviewer
	r.Reason = reason
	r.UpdatedAt = now
	if err := s.save(); err != nil {
		return nil, err
	}
	cp := *r
	return &cp, nil
}

// ListEscalated returns escalated requests.
func (s *Store) ListEscalated() []*Request {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Request
	for _, r := range s.records {
		if r.Status == StatusEscalated {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out
}
