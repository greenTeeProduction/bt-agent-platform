package dashboard

import (
	"path/filepath"
	"testing"
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
