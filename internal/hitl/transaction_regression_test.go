package hitl

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func newTransactionTestStore(t *testing.T) *Store {
	t.Helper()
	previous := DefaultStore
	t.Cleanup(func() { DefaultStore = previous })
	s, err := InitStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Create(&Request{ID: "pending", Status: StatusPending, TaskID: "task"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestApprovalWaitCancelsDuringLockContention(t *testing.T) {
	for _, lockKind := range []string{"file", "memory"} {
		for _, waitKind := range []string{"request", "task"} {
			t.Run(lockKind+"/"+waitKind, func(t *testing.T) {
				s := newTransactionTestStore(t)
				if lockKind == "file" {
					release, err := reliability.AcquireFileLockWithContext(t.Context(), s.path)
					if err != nil {
						t.Fatal(err)
					}
					defer release()
				} else {
					s.mu.Lock()
					defer s.mu.Unlock()
				}
				ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
				defer cancel()
				start := time.Now()
				var err error
				if waitKind == "request" {
					_, err = s.WaitForRequest(ctx, "pending", time.Hour)
				} else {
					_, err = s.WaitForTaskID(ctx, "task", time.Hour)
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("wait error=%v", err)
				}
				if elapsed := time.Since(start); elapsed > time.Second {
					t.Fatalf("deadline wait took %v", elapsed)
				}
			})
		}
	}
}

func TestTransactionFailurePreservesCacheAndCanRetry(t *testing.T) {
	s := newTransactionTestStore(t)
	original, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	err = s.transaction(t.Context(), func(records map[string]*Request) (bool, error) {
		records["pending"].Status = StatusApproved
		if err := os.Remove(s.path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(s.path, 0700); err != nil {
			t.Fatal(err)
		}
		return true, nil
	})
	if err == nil {
		t.Fatal("expected failed commit")
	}
	if s.records["pending"].Status != StatusPending {
		t.Fatal("failed transaction published approval")
	}
	if err := os.Remove(s.path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve("pending", "reviewer", "retry"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := InitStore(filepath.Dir(filepath.Dir(s.path)))
	if err != nil {
		t.Fatal(err)
	}
	req, ok := reloaded.Get("pending")
	if !ok || req.Status != StatusApproved {
		t.Fatalf("retry state=%+v", req)
	}
}

func TestFailedRetentionWriteDoesNotPruneCache(t *testing.T) {
	s := newTransactionTestStore(t)
	for i := range hitlMaxStoredTerminal + 1 {
		id := time.Unix(int64(i), 0).String()
		s.records[id] = &Request{ID: id, Status: StatusApproved, UpdatedAt: time.Unix(int64(i), 0)}
	}
	before := len(s.records)
	s.path = t.TempDir()
	if err := s.save(); err == nil {
		t.Fatal("expected failed snapshot")
	}
	if len(s.records) != before {
		t.Fatal("failed snapshot pruned cache")
	}
}

func TestCorruptLookupIsAnErrorAndDoesNotPublishPartialData(t *testing.T) {
	s := newTransactionTestStore(t)
	if err := os.WriteFile(s.path, []byte(`[{"id":"partial","status":"approved"},{"id":"bad","created_at":true}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if req, ok, err := s.GetWithContext(t.Context(), "partial"); err == nil || ok || req != nil {
		t.Fatalf("lookup=%+v %v %v", req, ok, err)
	}
	if s.records["pending"].Status != StatusPending || len(s.records) != 1 {
		t.Fatal("parse failure published partial records")
	}
}

func TestTerminalRequestsCannotBeRevivedOrOverwritten(t *testing.T) {
	s := newTransactionTestStore(t)
	if _, err := s.Approve("pending", "reviewer", "done"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Escalate("pending", "reviewer", "revive"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("escalation error=%v", err)
	}
	if err := s.Create(&Request{ID: "pending", Status: StatusPending}); err == nil {
		t.Fatal("duplicate ID overwrote approval")
	}
	after, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid decision changed persisted approval")
	}
}

func TestExpiredRequestCannotBeApprovedBeforePolling(t *testing.T) {
	s := newTransactionTestStore(t)
	if err := s.Create(&Request{ID: "expired", Status: StatusPending, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve("expired", "reviewer", "late"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("expired approval=%v", err)
	}
}

func TestExpiredEscalationWaitReturnsExpired(t *testing.T) {
	s := newTransactionTestStore(t)
	if err := s.Create(&Request{ID: "escalated", Status: StatusEscalated, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	req, err := s.WaitForRequest(t.Context(), "escalated", time.Hour)
	if err == nil || req == nil || req.Status != StatusExpired {
		t.Fatalf("expired wait=%+v %v", req, err)
	}
}

func TestTaskDecisionRetryIsIdempotentAndCannotHideTerminalConflict(t *testing.T) {
	s := newTransactionTestStore(t)
	first, err := s.ApproveByTaskID("task", "first-reviewer", "first")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.ApproveByTaskID("task", "retry-reviewer", "retry")
	if err != nil || retry == nil || retry.Reviewer != first.Reviewer || !retry.ApprovedAt.Equal(*first.ApprovedAt) {
		t.Fatalf("retry=%+v %v", retry, err)
	}
	if _, err := s.RejectByTaskID("task", "reviewer", "opposite"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("opposite decision=%v", err)
	}
	if _, err := s.ApproveByTaskID("no-audit", "reviewer", ""); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("missing audit=%v", err)
	}
}

func TestTaskDecisionCannotBypassNewerTerminalAudit(t *testing.T) {
	for _, terminal := range []Status{StatusRejected, StatusExpired} {
		t.Run(string(terminal), func(t *testing.T) {
			s := newTransactionTestStore(t)
			if err := s.Create(&Request{ID: "newer", TaskID: "task", Status: terminal, CreatedAt: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ApproveByTaskID("task", "reviewer", ""); !errors.Is(err, ErrInvalidStatus) {
				t.Fatalf("newer %s bypassed: %v", terminal, err)
			}
			older, _ := s.Get("pending")
			if older.Status != StatusPending {
				t.Fatal("older request was approved instead")
			}
		})
	}
}
