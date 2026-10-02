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

// newestTaskRequest selects an unexpired pending/escalated request.
func newestTaskRequest(records map[string]*Request, taskID string, now time.Time) *Request {
	return latestTaskRequest(records, taskID, now, true)
}
func latestTaskRequest(records map[string]*Request, taskID string, now time.Time, unresolvedOnly bool) *Request {
	var best *Request
	for _, r := range records {
		if unresolvedOnly {
			if r.Status != StatusPending && r.Status != StatusEscalated {
				continue
			}
			if !r.ExpiresAt.IsZero() && now.After(r.ExpiresAt) {
				continue
			}
		}
		tid := r.TaskID
		if tid == "" {
			tid = r.Context["task_id"]
		}
		if tid != taskID {
			continue
		}
		if best == nil || r.CreatedAt.After(best.CreatedAt) || (r.CreatedAt.Equal(best.CreatedAt) && r.ID < best.ID) {
			best = r
		}
	}
	return best
}

func (s *Store) FindPendingByTaskID(taskID string) (*Request, bool) {
	req, ok, _ := s.FindPendingByTaskIDWithContext(context.Background(), taskID)
	return req, ok
}
func (s *Store) FindPendingByTaskIDWithContext(ctx context.Context, taskID string) (*Request, bool, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, false, nil
	}
	var req *Request
	err := s.transaction(ctx, func(records map[string]*Request) (bool, error) {
		changed := expireRequests(records)
		if r := newestTaskRequest(records, taskID, time.Now()); r != nil {
			req = cloneRequest(r)
		}
		return changed, nil
	})
	if err != nil {
		return nil, false, err
	}
	return req, req != nil, nil
}

// WaitForRequest polls committed status; every lock wait shares ctx's budget.
func (s *Store) WaitForRequest(ctx context.Context, id string, pollEvery time.Duration) (*Request, error) {
	if pollEvery <= 0 {
		pollEvery = 500 * time.Millisecond
	}
	ticker := time.NewTicker(pollEvery)
	defer ticker.Stop()
	for {
		req, err := s.RefreshRequestWithContext(ctx, id)
		if err != nil {
			return nil, err
		}
		switch req.Status {
		case StatusApproved, StatusSkipped, StatusEscalated:
			return req, nil
		case StatusRejected, StatusExpired:
			return req, fmt.Errorf("hitl: request %s", req.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Store) WaitForTaskID(ctx context.Context, taskID string, pollEvery time.Duration) (*Request, error) {
	req, ok, err := s.FindPendingByTaskIDWithContext(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w for task %q", ErrRequestNotFound, taskID)
	}
	return s.WaitForRequest(ctx, req.ID, pollEvery)
}
func (s *Store) ApproveByTaskID(taskID, reviewer, comment string) (*Request, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, ErrRequestNotFound
	}
	return s.decideWithContext(context.Background(), "", taskID, reviewer, comment, StatusApproved)
}
func (s *Store) RejectByTaskID(taskID, reviewer, reason string) (*Request, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, ErrRequestNotFound
	}
	return s.decideWithContext(context.Background(), "", taskID, reviewer, reason, StatusRejected)
}

// Escalate requires an unresolved request, so it cannot revive terminal state.
func (s *Store) Escalate(id, reviewer, reason string) (*Request, error) {
	return s.EscalateWithContext(context.Background(), id, reviewer, reason)
}
func (s *Store) EscalateWithContext(ctx context.Context, id, reviewer, reason string) (*Request, error) {
	return s.decideWithContext(ctx, id, "", reviewer, reason, StatusEscalated)
}
func (s *Store) ListEscalated() []*Request {
	list, _ := s.listWithContext(context.Background(), StatusEscalated, false)
	return list
}
