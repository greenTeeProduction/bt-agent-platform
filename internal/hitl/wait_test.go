package hitl

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitForRequest_Approve(t *testing.T) {
	dir := t.TempDir()
	store, err := InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	req := NewRequest("step", "WorkflowApproval", "approve?", "", "payload", "approve?", map[string]any{"task_id": "wf:test:approve:1"})
	if err := store.Create(req); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		time.Sleep(100 * time.Millisecond)
		_, _ = store.Approve(req.ID, "tester", "ok")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := store.WaitForRequest(ctx, req.ID, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusApproved {
		t.Fatalf("expected approved, got %s", got.Status)
	}
	<-done
}
