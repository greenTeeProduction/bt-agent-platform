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
