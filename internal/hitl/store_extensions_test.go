package hitl

import (
	"path/filepath"
	"testing"
)

func TestApproveByTaskID_and_Escalate(t *testing.T) {
	dir := t.TempDir()
	store, err := InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	req := NewRequest("G", "HumanApprovalGate", "body", "", "", "p", map[string]any{"task_id": "task-42"})
	if err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	got, ok := store.FindPendingByTaskID("task-42")
	if !ok || got.ID != req.ID {
		t.Fatalf("FindPendingByTaskID: %+v ok=%v", got, ok)
	}
	approved, err := store.ApproveByTaskID("task-42", "u", "ok")
	if err != nil || approved.Status != StatusApproved {
		t.Fatalf("ApproveByTaskID: %v %+v", err, approved)
	}

	req2 := NewRequest("G2", "HumanApprovalGate", "b2", "", "", "p2", nil)
	req2.SetTaskID("task-99")
	_ = store.Create(req2)
	esc, err := store.Escalate(req2.ID, "ops", "timeout")
	if err != nil || esc.Status != StatusEscalated {
		t.Fatalf("Escalate: %v %+v", err, esc)
	}
	if len(store.ListEscalated()) != 1 {
		t.Fatalf("expected 1 escalated")
	}
}

func TestApprove_FromEscalated(t *testing.T) {
	dir := t.TempDir()
	store, err := InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	req := NewRequest("G3", "HumanApprovalGate", "b3", "", "", "p3", nil)
	if err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Escalate(req.ID, "ops", "needs review"); err != nil {
		t.Fatal(err)
	}
	approved, err := store.Approve(req.ID, "u", "resolved")
	if err != nil {
		t.Fatalf("Approve from escalated should succeed: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Fatalf("expected approved, got %s", approved.Status)
	}
}

func TestReject_FromEscalated(t *testing.T) {
	dir := t.TempDir()
	store, err := InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	req := NewRequest("G4", "HumanApprovalGate", "b4", "", "", "p4", nil)
	if err := store.Create(req); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Escalate(req.ID, "ops", "needs review"); err != nil {
		t.Fatal(err)
	}
	rejected, err := store.Reject(req.ID, "u", "denied")
	if err != nil {
		t.Fatalf("Reject from escalated should succeed: %v", err)
	}
	if rejected.Status != StatusRejected {
		t.Fatalf("expected rejected, got %s", rejected.Status)
	}
}
