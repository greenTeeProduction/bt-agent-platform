package dashboard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestTaskExecutionCommitPreservesClaimAndRepairsMetadata(t *testing.T) {
	for _, status := range []string{"completed", "failed", "approved"} {
		t.Run(status, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tasks.json")
			store := NewTaskStore(path)
			if err := store.Create(Task{ID: "owned", Status: "approved"}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ClaimApproved(); err != nil {
				t.Fatal(err)
			}
			before := store.List()
			committed, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// An owned temporary target directory prevents replacement while
			// preserving the last committed bytes for the repair fixture.
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
			result := TaskExecutionResult{Status: status, Output: "observed output", Outcome: "success", RunID: "observed-run"}
			switch status {
			case "failed":
				result.Outcome, result.Error, result.ErrorKind = "timeout", "known timeout", "stopped"
			case "approved":
				result.Outcome = "deferred"
			}
			if err := store.CommitExecution("owned", result); err == nil {
				t.Fatal("blocked execution commit acknowledged")
			}
			if !reflect.DeepEqual(before, store.List()) || len(store.Approved()) != 0 {
				t.Fatal("uncommitted result changed cache or became replayable")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, committed, 0600); err != nil {
				t.Fatal(err)
			}
			if err := store.CommitExecution("owned", result); err != nil {
				t.Fatal(err)
			}
			fresh := NewTaskStore(path)
			got, _ := fresh.Get("owned")
			if got.Status != status || got.Output != result.Output || got.Outcome != result.Outcome || got.Error != result.Error || got.ErrorKind != result.ErrorKind || got.RunID != result.RunID {
				t.Fatalf("related execution metadata not committed together: %+v", got)
			}
			if status != "approved" {
				if _, err := time.Parse(time.RFC3339Nano, got.CompletedAt); err != nil {
					t.Fatal(err)
				}
			} else if got.CompletedAt != "" {
				t.Fatal("deferred task falsely timestamped as completed")
			}
			if err := store.CommitExecution("owned", result); !errors.Is(err, ErrTaskInvalidStatus) {
				t.Fatalf("terminal/operator status overwritten: %v", err)
			}
		})
	}
}

func TestTaskExecutionCommitRejectsUnclaimedOrUnhealthyCompletion(t *testing.T) {
	store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err := store.Create(Task{ID: "pending"}); err != nil {
		t.Fatal(err)
	}
	for _, result := range []TaskExecutionResult{
		{Status: "completed", Outcome: "success"},
		{Status: "completed", Outcome: "pending_approval"},
		{Status: "bogus", Outcome: "success"},
	} {
		if err := store.CommitExecution("pending", result); !errors.Is(err, ErrTaskInvalidStatus) {
			t.Fatalf("invalid admission/result accepted: %+v err=%v", result, err)
		}
	}
	got, _ := store.Get("pending")
	if got.Status != "pending" || got.CompletedAt != "" || got.Output != "" {
		t.Fatal("rejected execution mutated pending task")
	}
	if err := store.CommitExecution("missing", TaskExecutionResult{Status: "failed"}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatal(err)
	}
}
