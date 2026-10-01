package dashboard

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestTaskExecutionContextBoundsMutexAndFileContention(t *testing.T) {
	for _, operation := range []string{"claim", "result"} {
		for _, lock := range []string{"mutex", "file"} {
			t.Run(operation+"-"+lock, func(t *testing.T) {
				store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
				if err := store.Create(Task{ID: "owned", Status: "approved"}); err != nil {
					t.Fatal(err)
				}
				if operation == "result" {
					if _, err := store.ClaimApproved(); err != nil {
						t.Fatal(err)
					}
				}
				before := store.List()
				if lock == "mutex" {
					store.mu.Lock()
					defer store.mu.Unlock()
				} else {
					release, err := reliability.AcquireFileLockWithContext(context.Background(), store.path)
					if err != nil {
						t.Fatal(err)
					}
					defer release()
				}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				start := time.Now()
				var err error
				if operation == "claim" {
					var claims []Task
					claims, err = store.ClaimApprovedWithContext(ctx)
					if len(claims) != 0 {
						t.Fatal("uncommitted claim returned")
					}
				} else {
					err = store.CommitExecutionWithContext(ctx, "owned", TaskExecutionResult{Status: "completed", Outcome: "success", Output: "uncommitted"})
				}
				if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
					t.Fatalf("caller budget lost: %v", err)
				}
				// The mutex fixture intentionally retains ownership for inspection.
				if !reflect.DeepEqual(before, store.Tasks) {
					t.Fatal("canceled transaction changed cache")
				}
				fresh := NewTaskStore(store.path)
				if !reflect.DeepEqual(before, fresh.List()) {
					t.Fatal("canceled transaction changed disk")
				}
			})
		}
	}
}

func TestTaskExecutionBatchRejectsMissingOrConflictingMemberAtomically(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	for _, id := range []string{"one", "two"} {
		if err := store.Create(Task{ID: id, Status: "approved"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.ClaimApproved(); err != nil {
		t.Fatal(err)
	}
	before := store.List()
	results := map[string]TaskExecutionResult{"one": {Status: "approved", Outcome: "not_started"}, "missing": {Status: "approved", Outcome: "not_started"}}
	if err := store.CommitExecutionBatchWithContext(context.Background(), results); !errors.Is(err, ErrTaskNotFound) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, store.List()) {
		t.Fatal("invalid group partially published")
	}
	delete(results, "missing")
	results["two"] = TaskExecutionResult{Status: "approved", Outcome: "not_started"}
	if err := store.CommitExecutionBatchWithContext(context.Background(), results); err != nil {
		t.Fatal(err)
	}
	fresh := NewTaskStore(store.path)
	if len(fresh.Approved()) != 2 {
		t.Fatal("unstarted related claims not returned together")
	}
}
