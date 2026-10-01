package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

func TestTaskMutationsFailedCommitPreservesStateAndPendingApproval(t *testing.T) {
	for _, mutation := range []string{"create", "status", "approve", "reject", "output", "claim"} {
		t.Run(mutation, func(t *testing.T) {
			priorHITL := hitl.DefaultStore
			t.Cleanup(func() { hitl.DefaultStore = priorHITL })
			audit := newHITLTestStore(t)
			req := hitl.NewRequest("t1", "DashboardTask", "test pending task", "", "", "", map[string]any{"task_id": "t1"})
			if err := audit.Create(req); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "state", "tasks.json")
			store := NewTaskStore(path)
			status := "pending"
			if mutation == "claim" {
				status = "approved"
			}
			if err := store.Create(Task{ID: "t1", Title: "test", Status: status}); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(store.List())
			diskBefore, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// A directory as the target cannot be replaced by an atomic file commit.
			blocked := filepath.Join(t.TempDir(), "blocked")
			if err := os.Mkdir(blocked, 0700); err != nil {
				t.Fatal(err)
			}
			store.path = blocked
			switch mutation {
			case "create":
				err = store.Create(Task{ID: "t2", Title: "second"})
			case "status":
				err = store.UpdateStatus("t1", "completed")
			case "approve":
				err = store.Approve("t1", "reviewer")
			case "reject":
				err = store.Reject("t1", "reviewer", "reason")
			case "output":
				err = store.SetOutput("t1", "uncommitted", "success")
			case "claim":
				var claimed []Task
				claimed, err = store.ClaimApproved()
				if len(claimed) != 0 {
					t.Fatal("uncommitted task admitted")
				}
			}
			if err == nil {
				t.Fatal("failed commit was acknowledged")
			}
			after, _ := json.Marshal(store.List())
			if !bytes.Equal(before, after) {
				t.Fatalf("failed mutation changed memory: %s", after)
			}
			diskAfter, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(diskBefore, diskAfter) {
				t.Fatal("failed mutation changed committed file")
			}
			if found, ok := audit.Get(req.ID); !ok || found.Status != hitl.StatusPending {
				t.Fatal("failed task write resolved approval")
			}
		})
	}
}

func TestTaskSavePreservesLegacyTemporarySymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	store := NewTaskStore(path)
	victim := filepath.Join(t.TempDir(), "victim")
	const content = "unrelated temporary-file target"
	if err := os.WriteFile(victim, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(Task{ID: "t1", Title: "new"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != content {
		t.Fatal("legacy temp symlink target changed")
	}
	if link, err := os.Readlink(path + ".tmp"); err != nil || link != victim {
		t.Fatal("unrelated legacy temp was removed")
	}
	loaded := NewTaskStore(path)
	if len(loaded.List()) != 1 {
		t.Fatal("committed task not recoverable")
	}
}

func TestTaskLoadDecodeFailurePreservesLiveState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	store := NewTaskStore(path)
	if err := store.Create(Task{ID: "t1", Title: "original"}); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(store.List())
	// A late type error can partially populate json.Unmarshal's destination.
	if err := os.WriteFile(path, []byte(`{"tasks":[{"id":"replacement","title":"partial"},{"id":"broken","title":42}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(); err == nil {
		t.Fatal("invalid task JSON accepted")
	}
	after, _ := json.Marshal(store.List())
	if !bytes.Equal(before, after) {
		t.Fatal("failed load partially replaced live tasks")
	}
}

func TestTaskSnapshotsOwnApprovalTimestamps(t *testing.T) {
	s := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
	original := time.Now()
	if err := s.Create(Task{ID: "task", Status: "approved", Approval: Approval{ApprovedAt: &original}}); err != nil {
		t.Fatal(err)
	}
	committed := original
	original = original.Add(time.Hour)
	for _, snapshot := range []Task{s.List()[0], s.Approved()[0]} {
		*snapshot.Approval.ApprovedAt = original
	}
	snapshot, _ := s.Get("task")
	*snapshot.Approval.ApprovedAt = original
	tasks, err := s.ClaimApproved()
	if err != nil {
		t.Fatal(err)
	}
	*tasks[0].Approval.ApprovedAt = original
	actual, _ := s.Get("task")
	if !actual.Approval.ApprovedAt.Equal(committed) {
		t.Fatal("caller mutated live approval timestamp")
	}
}

func TestTaskDecisionAuditFailureBlocksAdmissionAndRetryReconciles(t *testing.T) {
	for _, decision := range []string{"approve", "reject"} {
		t.Run(decision, func(t *testing.T) {
			previous := hitl.DefaultStore
			t.Cleanup(func() { hitl.DefaultStore = previous })
			root := t.TempDir()
			audit, err := hitl.InitStore(root)
			if err != nil {
				t.Fatal(err)
			}
			req := hitl.NewRequest("task", "WorkflowApproval", "task", "", "", "", map[string]any{"task_id": "task"})
			if err := audit.Create(req); err != nil {
				t.Fatal(err)
			}
			auditPath := filepath.Join(root, "hitl", "requests.json")
			original, err := os.ReadFile(auditPath)
			if err != nil {
				t.Fatal(err)
			}
			taskPath := filepath.Join(t.TempDir(), "tasks.json")
			tasks := NewTaskStore(taskPath)
			if err := tasks.Create(Task{ID: "task"}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(auditPath, []byte("corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
			decide := func() error {
				if decision == "approve" {
					return tasks.Approve("task", "reviewer")
				}
				return tasks.Reject("task", "reviewer", "reason")
			}
			var diagnostic *TaskDecisionPersistenceError
			if err := decide(); !errors.As(err, &diagnostic) {
				t.Fatalf("partial decision diagnostic=%v", err)
			}
			stored, _ := tasks.Get("task")
			if !stored.AuditPending {
				t.Fatal("missing reconciliation marker")
			}
			if len(tasks.Approved()) != 0 {
				t.Fatal("unreconciled decision became dispatchable")
			}
			if claimed, err := tasks.ClaimApproved(); err != nil || len(claimed) != 0 {
				t.Fatalf("claim=%+v %v", claimed, err)
			}
			// Restart recovers the marker; retrying the same decision finishes audit.
			tasks = NewTaskStore(taskPath)
			if got, _ := tasks.Get("task"); !got.AuditPending {
				t.Fatal("reconciliation marker lost on restart")
			}
			if err := os.WriteFile(auditPath, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := decide(); err != nil {
				t.Fatal(err)
			}
			got, _ := tasks.Get("task")
			if got.AuditPending {
				t.Fatal("successful audit did not clear marker")
			}
			resolved, _ := audit.Get(req.ID)
			expected := hitl.StatusApproved
			if decision == "reject" {
				expected = hitl.StatusRejected
			}
			if resolved.Status != expected {
				t.Fatalf("audit=%+v", resolved)
			}
			if decision == "approve" {
				if claimed, err := tasks.ClaimApproved(); err != nil || len(claimed) != 1 {
					t.Fatalf("resolved claim=%+v %v", claimed, err)
				}
				if claimed, err := tasks.ClaimApproved(); err != nil || len(claimed) != 0 {
					t.Fatal("retry dispatched twice")
				}
			}
		})
	}
}
