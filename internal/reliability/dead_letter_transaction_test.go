package reliability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDLQClaimSurvivesSiblingDeltas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlq.json")
	owner, sibling := NewDeadLetterQueue(path), NewDeadLetterQueue(path)
	if err := owner.PushWithError(DeadLetterEntry{ID: "owned"}); err != nil {
		t.Fatal(err)
	}
	started, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	owner.SetReplayExecutor(func(DeadLetterEntry) error { close(started); <-finish; return nil })
	go func() { _, err := owner.ReplayWithError("owned"); done <- err }()
	<-started
	t.Cleanup(func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	})
	sibling.SetReplayExecutor(func(DeadLetterEntry) error { t.Error("sibling executed claimed work"); return nil })
	if _, err := sibling.ReplayWithError("owned"); !errors.Is(err, ErrReplayRecovery) {
		t.Fatalf("sibling admission: %v", err)
	}
	if _, err := sibling.RequeueWithError("owned"); !errors.Is(err, ErrReplayRecovery) {
		t.Fatalf("requeue: %v", err)
	}
	if err := sibling.PushWithError(DeadLetterEntry{ID: "sibling"}); err != nil {
		t.Fatal(err)
	}
	if n, err := sibling.PurgeWithError(); err != nil || n != 1 {
		t.Fatalf("purge removed=%d err=%v", n, err)
	}
	held := sibling.List()
	if len(held) != 1 || held[0].ReplayClaim == "" || !held[0].RecoveryRequired {
		t.Fatalf("claim erased: %+v", held)
	}
	if err := owner.ResolveReplayRecovery("owned", held[0].ReplayClaim, true); !errors.Is(err, ErrReplayRecovery) {
		t.Fatalf("active reconciliation: %v", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Sibling's cache still contains the claim. Its next delta must not resurrect
	// the entry removed by the completed owner.
	if err := sibling.PushWithError(DeadLetterEntry{ID: "after"}); err != nil {
		t.Fatal(err)
	}
	if entries := NewDeadLetterQueue(path).List(); len(entries) != 1 || entries[0].ID != "after" {
		t.Fatalf("stale resurrection: %+v", entries)
	}
}

func TestDLQRecoveryRequiresExactClaimAndCommittedDecision(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unstarted", true: "completed"}[completed], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dlq.json")
			q := NewDeadLetterQueue(path)
			if err := q.PushWithError(DeadLetterEntry{ID: "owned"}); err != nil {
				t.Fatal(err)
			}
			if _, err := q.RequeueWithError("owned"); err != nil {
				t.Fatal(err)
			}
			count := 0
			q.SetReplayExecutor(func(DeadLetterEntry) error {
				count++
				return &ExecutionUncertainError{Err: errors.New("fixture uncertain")}
			})
			entry, err := q.ReplayWithError("owned")
			if !IsExecutionUncertainError(err) || entry == nil || !entry.RecoveryRequired {
				t.Fatalf("outcome=%+v %v", entry, err)
			}
			restarted := NewDeadLetterQueue(path)
			if err := restarted.ResolveReplayRecovery("owned", "stale-claim", completed); !errors.Is(err, ErrReplayRecovery) {
				t.Fatalf("stale decision: %v", err)
			}
			if err := restarted.ResolveReplayRecovery("owned", entry.ReplayClaim, completed); err != nil {
				t.Fatal(err)
			}
			if count != 1 || len(restarted.RequeuedReady()) != 0 {
				t.Fatal("reconciliation dispatched or scheduled work")
			}
			if completed && restarted.Len() != 0 {
				t.Fatal("completed work retained")
			}
			if !completed {
				e := restarted.List()[0]
				if e.held() || !e.RequeuedAt.IsZero() {
					t.Fatalf("unstarted resolution: %+v", e)
				}
			}
		})
	}
}

func TestDLQFailedMutationsDoNotAcknowledgeOrPublish(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fault requires an unprivileged process")
	}
	root := t.TempDir()
	path := filepath.Join(root, "dlq.json")
	q := NewDeadLetterQueue(path)
	if err := q.PushWithError(DeadLetterEntry{ID: "owned"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0700) })
	if err := q.PushWithError(DeadLetterEntry{ID: "failed"}); err == nil {
		t.Fatal("push acknowledged")
	}
	if entry, err := q.RequeueWithError("owned"); err == nil || entry != nil {
		t.Fatal("requeue acknowledged")
	}
	if n, err := q.PurgeWithError(); err == nil || n != 0 {
		t.Fatal("purge acknowledged")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || q.Len() != 1 || q.List()[0].Attempts != 0 {
		t.Fatal("failed mutation changed durable/cache state")
	}
}

func TestDLQLockFailureDoesNotFallBackToUnlockedWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlq.json")
	q := NewDeadLetterQueue(path)
	if err := q.PushWithError(DeadLetterEntry{ID: "owned"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	start := time.Now()
	if err := q.PushWithError(DeadLetterEntry{ID: "not-admitted"}); err == nil {
		t.Fatal("lock failure acknowledged")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("lock wait was not bounded")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unserialized fallback write")
	}
}

func TestDLQPanicRetainsRecoveryFence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dlq.json")
	q := NewDeadLetterQueue(path)
	if err := q.PushWithError(DeadLetterEntry{ID: "owned"}); err != nil {
		t.Fatal(err)
	}
	q.SetReplayExecutor(func(DeadLetterEntry) error { panic("fixture") })
	if _, err := q.ReplayWithError("owned"); !IsExecutionUncertainError(err) {
		t.Fatal(err)
	}
	restarted := NewDeadLetterQueue(path)
	if _, err := restarted.RequeueWithError("owned"); !errors.Is(err, ErrReplayRecovery) {
		t.Fatalf("panic admitted retry: %v", err)
	}
	if n, err := restarted.PurgeWithError(); err != nil || n != 0 {
		t.Fatalf("purged panic fence: %d %v", n, err)
	}
}

func TestDLQCapacityCannotEvictRecoveryClaims(t *testing.T) {
	q := NewDeadLetterQueue("")
	for i := range MaxDeadLetterEntries {
		q.entries = append(q.entries, DeadLetterEntry{ID: fmt.Sprint(i), ReplayClaim: "fixture-claim", RecoveryRequired: true})
	}
	if err := q.PushWithError(DeadLetterEntry{ID: "overflow"}); err == nil {
		t.Fatal("capacity admitted by erasing a claim")
	}
	if q.Len() != MaxDeadLetterEntries {
		t.Fatal("capacity failure mutated cache")
	}
	q.entries[MaxDeadLetterEntries-1] = DeadLetterEntry{ID: "ordinary"}
	if err := q.PushWithError(DeadLetterEntry{ID: "replacement"}); err != nil {
		t.Fatal(err)
	}
	for _, e := range q.List() {
		if e.ID == "ordinary" {
			t.Fatal("unheld entry was not evicted")
		}
		if e.ID != "replacement" && !e.held() {
			t.Fatal("claim lost")
		}
	}
}

func TestDLQInvalidIdentitiesPreserveStateAndCloseAdmission(t *testing.T) {
	for _, raw := range []string{`[{"id":""}]`, `[{"id":"same"},{"id":"same"}]`, `[{"id":"negative","attempts":-1}]`} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dlq.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			q := NewDeadLetterQueue(path)
			if q.PersistenceError() == nil {
				t.Fatal("invalid identities accepted")
			}
			if err := q.PushWithError(DeadLetterEntry{ID: "new"}); err == nil {
				t.Fatal("invalid membership overwritten")
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != raw {
				t.Fatal("invalid evidence erased")
			}
		})
	}
}
