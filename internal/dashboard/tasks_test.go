package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

// TestTaskStore_ApproveRejectAuditPersistsAcrossSaveLoad pins milestone 2/3 of
// the dashboard Workflow/Approval wiring program: dashboard.Task must carry an
// audit trail (mirroring workflow_engine.go's Approval struct) recording who
// approved/rejected it and when, not just the bare Status string. The audit
// fields must survive a Save/Load round trip since TaskStore is the durable
// source of truth consumed across dashboard restarts.
func TestTaskStore_ApproveRejectAuditPersistsAcrossSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")

	store := NewTaskStore(path)
	if err := store.Create(Task{ID: "t1", Title: "approve me"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(Task{ID: "t2", Title: "reject me"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Approve("t1", "alice"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := store.Reject("t2", "bob", "not ready"); err != nil {
		t.Fatalf("Reject: %v", err)
	}

	// Reload from disk: the audit trail must survive Save/Load, not just live
	// in the in-memory struct.
	reloaded := NewTaskStore(path)

	approved, ok := reloaded.Get("t1")
	if !ok {
		t.Fatal("t1 not found after reload")
	}
	if approved.Approval.ApprovedBy != "alice" {
		t.Errorf("ApprovedBy = %q, want alice", approved.Approval.ApprovedBy)
	}
	if approved.Approval.ApprovedAt == nil {
		t.Error("ApprovedAt not set after reload")
	}
	if approved.Approval.RejectedAt != nil {
		t.Errorf("RejectedAt should be nil for an approved task, got %v", approved.Approval.RejectedAt)
	}

	rejected, ok := reloaded.Get("t2")
	if !ok {
		t.Fatal("t2 not found after reload")
	}
	if rejected.Approval.ApprovedBy != "bob" {
		t.Errorf("ApprovedBy (rejector) = %q, want bob", rejected.Approval.ApprovedBy)
	}
	if rejected.Approval.RejectedAt == nil {
		t.Error("RejectedAt not set after reload")
	}
	if rejected.Approval.Reason != "not ready" {
		t.Errorf("Reason = %q, want %q", rejected.Approval.Reason, "not ready")
	}
}

// TestTaskStore_ApprovedOrdersByPriorityThenSprint pins milestone 3/3 of the
// dashboard Workflow/Approval wiring program: Approved() must return tasks
// ordered by priority (critical first) then sprint, mirroring workflow_engine.go's
// sortTasks/Prioritize, so handleSprintExecute dispatches high-urgency work
// first regardless of task creation order.
func TestTaskStore_ApprovedOrdersByPriorityThenSprint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")

	store := NewTaskStore(path)

	// The low-priority task is created (and approved) first; the critical
	// task is created and approved afterward. Approved() must still surface
	// the critical task before the low-priority one.
	if err := store.Create(Task{ID: "low-early", Title: "low priority, created first", Priority: "low", Sprint: 1}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(Task{ID: "critical-late", Title: "critical priority, created second", Priority: "critical", Sprint: 1}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Approve("low-early", "alice"); err != nil {
		t.Fatalf("Approve low-early: %v", err)
	}
	if err := store.Approve("critical-late", "alice"); err != nil {
		t.Fatalf("Approve critical-late: %v", err)
	}

	approved := store.Approved()
	if len(approved) != 2 {
		t.Fatalf("Approved() returned %d tasks, want 2", len(approved))
	}
	if approved[0].ID != "critical-late" {
		t.Errorf("Approved()[0].ID = %q, want %q (critical priority must dispatch first)", approved[0].ID, "critical-late")
	}
	if approved[1].ID != "low-early" {
		t.Errorf("Approved()[1].ID = %q, want %q", approved[1].ID, "low-early")
	}
}

// TestTaskStore_LoadReturnsErrorOnCorruptJSON pins the "fail loudly on
// corruption" half of the atomic load/save research goal: Load() must
// surface a JSON-parse error to the caller instead of silently discarding it
// (the prior behavior of `_ = json.Unmarshal(data, s)`), which made a
// corrupted tasks.json indistinguishable from an empty, freshly-created
// store — silently losing every persisted task. This mirrors the Load()
// error contract already used by goap.GoalStore and agent.FileJobStore.
func TestTaskStore_LoadReturnsErrorOnCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("seeding corrupt file: %v", err)
	}

	store := &TaskStore{path: path, Tasks: []Task{}}
	if err := store.Load(); err == nil {
		t.Fatal("Load() on a corrupt tasks.json returned a nil error; corruption must fail loudly instead of being silently discarded")
	}
}

// TestNewTaskStore_PanicsOnCorruptFile pins the same fail-loudly contract at
// the NewTaskStore constructor, which is the entry point cmd/bt-dashboard
// actually calls at startup (dashboard.NewTaskStore(...), no error return).
// Silently starting the dashboard with an empty task list because the
// on-disk file failed to parse would look identical to "no tasks yet" — an
// operator would never know their tasks were gone.
func TestNewTaskStore_PanicsOnCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("seeding corrupt file: %v", err)
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewTaskStore did not panic on a corrupt task store file; corruption must fail loudly instead of silently starting with an empty store")
		}
	}()
	NewTaskStore(path)
}

// TestTaskStore_SaveAtomicWriteLeavesOriginalIntactOnFailure pins the
// "atomic" half of the research goal: Save must write through a sibling temp
// file and rename into place (mirroring goap.GoalStore.saveLocked and
// agent.FileJobStore.Save), not truncate-and-overwrite the live file
// directly. A read-only directory blocks creating that sibling temp file, so
// an atomic save must fail here *without* touching the existing file. The
// prior os.WriteFile(s.path, ...) implementation truncates the existing file
// in place — which needs only write permission on the file itself, not the
// directory — so it silently succeeds and destroys the original content:
// exactly the corruption-on-crash risk this goal exists to close.
func TestTaskStore_SaveAtomicWriteLeavesOriginalIntactOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses directory permission checks")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")

	store := NewTaskStore(path)
	if err := store.Create(Task{ID: "t1", Title: "first"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading seeded file: %v", err)
	}

	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("chmod dir read-only: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()

	saveErr := store.Create(Task{ID: "t2", Title: "second"})

	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatalf("chmod dir writable: %v", err)
	}

	if saveErr == nil {
		t.Fatal("Create() succeeded despite a read-only directory; an atomic save needs to create a sibling temp file and must fail here")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file after failed save: %v", err)
	}
	if string(after) != string(original) {
		t.Errorf("tasks.json content changed after a failed save; atomic save must leave the original file untouched on failure.\noriginal: %s\nafter:    %s", original, after)
	}
}

// newHITLTestStore initializes a scratch HITL store with a permissive,
// non-auto-approving policy so the requests it creates stay pending until a
// test explicitly resolves them, and registers it as hitl.DefaultStore.
func newHITLTestStore(t *testing.T) *hitl.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := hitl.InitStore(dir)
	if err != nil {
		t.Fatalf("hitl.InitStore: %v", err)
	}
	origPolicy := hitl.GetPolicy()
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour, DefaultPrompt: "test"})
	t.Cleanup(func() { hitl.SetPolicy(origPolicy) })
	return store
}

// TestTaskStore_ApproveRejectRecordHITLAuditTrail pins the "single
// audit-trail code path" half of the Task/Approval-model consolidation goal:
// approving or rejecting a Task through TaskStore must resolve the matching
// pending hitl.Request (found via task_id), not just flip the in-memory
// Task.Approval field. Before consolidation, only the dashboard HTTP handler
// (cmd/bt-dashboard's handleTaskApprove/handleTaskReject) called into hitl
// separately — TaskStore.Approve/Reject themselves never touched the HITL
// store, so any other caller of TaskStore.Approve/Reject (e.g.
// Workflow-mirrored decisions) silently left the HITL audit trail pending
// forever.
func TestTaskStore_ApproveRejectRecordHITLAuditTrail(t *testing.T) {
	t.Run("approve", func(t *testing.T) {
		hstore := newHITLTestStore(t)
		req := hitl.NewRequest("t1", "DashboardTask", "approve me", "", "", "", map[string]any{"task_id": "t1"})
		if err := hstore.Create(req); err != nil {
			t.Fatalf("hstore.Create: %v", err)
		}

		store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
		if err := store.Create(Task{ID: "t1", Title: "approve me"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Approve("t1", "alice"); err != nil {
			t.Fatalf("Approve: %v", err)
		}

		if _, pending := hstore.FindPendingByTaskID("t1"); pending {
			t.Fatal("HITL request for t1 is still pending after TaskStore.Approve; approving a Task must resolve its HITL audit-trail entry too")
		}
		resolved, ok := hstore.Get(req.ID)
		if !ok {
			t.Fatal("HITL request not found after TaskStore.Approve")
		}
		if resolved.Status != hitl.StatusApproved {
			t.Errorf("HITL request status = %q, want %q", resolved.Status, hitl.StatusApproved)
		}
		if resolved.Reviewer != "alice" {
			t.Errorf("HITL request reviewer = %q, want %q", resolved.Reviewer, "alice")
		}
	})

	t.Run("reject", func(t *testing.T) {
		hstore := newHITLTestStore(t)
		req := hitl.NewRequest("t2", "DashboardTask", "reject me", "", "", "", map[string]any{"task_id": "t2"})
		if err := hstore.Create(req); err != nil {
			t.Fatalf("hstore.Create: %v", err)
		}

		store := NewTaskStore(filepath.Join(t.TempDir(), "tasks.json"))
		if err := store.Create(Task{ID: "t2", Title: "reject me"}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := store.Reject("t2", "bob", "not ready"); err != nil {
			t.Fatalf("Reject: %v", err)
		}

		if _, pending := hstore.FindPendingByTaskID("t2"); pending {
			t.Fatal("HITL request for t2 is still pending after TaskStore.Reject; rejecting a Task must resolve its HITL audit-trail entry too")
		}
		resolved, ok := hstore.Get(req.ID)
		if !ok {
			t.Fatal("HITL request not found after TaskStore.Reject")
		}
		if resolved.Status != hitl.StatusRejected {
			t.Errorf("HITL request status = %q, want %q", resolved.Status, hitl.StatusRejected)
		}
		if resolved.Reviewer != "bob" {
			t.Errorf("HITL request reviewer = %q, want %q", resolved.Reviewer, "bob")
		}
	})
}

// TestWorkflow_ApproveRejectTaskRecordHITLAuditTrail pins the other half of
// the consolidation goal: Workflow.ApproveTask/RejectTask — the WorkflowTask
// model's own approve/reject path — must resolve the matching HITL request
// exactly like TaskStore.Approve/Reject does. Before consolidation this path
// never touched hitl.DefaultStore at all (cmd/bt-dashboard's
// handleWorkflowApprove/handleWorkflowReject only mirrored the decision into
// taskStore, not into the HITL store), so a workflow-level decision left no
// HITL audit-trail record even though a task-level decision on the same
// underlying task_id did. Consolidating onto one audit-trail code path closes
// that gap.
func TestWorkflow_ApproveRejectTaskRecordHITLAuditTrail(t *testing.T) {
	t.Run("approve", func(t *testing.T) {
		hstore := newHITLTestStore(t)
		req := hitl.NewRequest("wf1-wt1", "WorkflowApproval", "approve me", "", "", "", map[string]any{"task_id": "wf1-wt1"})
		if err := hstore.Create(req); err != nil {
			t.Fatalf("hstore.Create: %v", err)
		}

		wf := &Workflow{ID: "wf1", Tasks: []WorkflowTask{{ID: "wf1-wt1", Status: StatusPending}}}
		if task := wf.ApproveTask("wf1-wt1", "carol"); task == nil {
			t.Fatal("ApproveTask returned nil for an existing task")
		}

		if _, pending := hstore.FindPendingByTaskID("wf1-wt1"); pending {
			t.Fatal("HITL request for wf1-wt1 is still pending after Workflow.ApproveTask; approving a WorkflowTask must resolve its HITL audit-trail entry too")
		}
		resolved, ok := hstore.Get(req.ID)
		if !ok {
			t.Fatal("HITL request not found after Workflow.ApproveTask")
		}
		if resolved.Status != hitl.StatusApproved {
			t.Errorf("HITL request status = %q, want %q", resolved.Status, hitl.StatusApproved)
		}
		if resolved.Reviewer != "carol" {
			t.Errorf("HITL request reviewer = %q, want %q", resolved.Reviewer, "carol")
		}
	})

	t.Run("reject", func(t *testing.T) {
		hstore := newHITLTestStore(t)
		req := hitl.NewRequest("wf1-wt2", "WorkflowApproval", "reject me", "", "", "", map[string]any{"task_id": "wf1-wt2"})
		if err := hstore.Create(req); err != nil {
			t.Fatalf("hstore.Create: %v", err)
		}

		wf := &Workflow{ID: "wf1", Tasks: []WorkflowTask{{ID: "wf1-wt2", Status: StatusPending}}}
		if task := wf.RejectTask("wf1-wt2", "dave", "not ready"); task == nil {
			t.Fatal("RejectTask returned nil for an existing task")
		}

		if _, pending := hstore.FindPendingByTaskID("wf1-wt2"); pending {
			t.Fatal("HITL request for wf1-wt2 is still pending after Workflow.RejectTask; rejecting a WorkflowTask must resolve its HITL audit-trail entry too")
		}
		resolved, ok := hstore.Get(req.ID)
		if !ok {
			t.Fatal("HITL request not found after Workflow.RejectTask")
		}
		if resolved.Status != hitl.StatusRejected {
			t.Errorf("HITL request status = %q, want %q", resolved.Status, hitl.StatusRejected)
		}
		if resolved.Reviewer != "dave" {
			t.Errorf("HITL request reviewer = %q, want %q", resolved.Reviewer, "dave")
		}
	})
}
