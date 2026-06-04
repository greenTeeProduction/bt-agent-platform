package hitl

import (
	"path/filepath"
	"testing"
)

func TestStoreApproveReject(t *testing.T) {
	dir := t.TempDir()
	s, err := InitStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Persist a non-pending record and verify reload.
	SetPolicy(Policy{Enabled: true, AutoApprove: true, Timeout: DefaultPolicy().Timeout})
	req := NewRequest("TestGate", "HumanApprovalGate", "do something risky", "", "proposed output", "approve?", nil)
	req = ApplyAutoApproveIfPolicy(req)
	if err := s.Create(req); err != nil {
		t.Fatal(err)
	}
	SetPolicy(DefaultPolicy())

	s2, err := InitStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	DefaultStore = s2
	if got, ok := DefaultStore.Get(req.ID); !ok || got.Status != StatusSkipped {
		t.Fatalf("reload: status=%v ok=%v", got.Status, ok)
	}

	req2 := NewRequest("Gate2", "HumanApprovalGate", "task2", "", "", "ok?", nil)
	req2.Status = StatusPending
	if err := DefaultStore.Create(req2); err != nil {
		t.Fatal(err)
	}

	approved, err := DefaultStore.Approve(req2.ID, "tester", "looks good")
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != StatusApproved {
		t.Fatalf("status=%s", approved.Status)
	}

	req3 := NewRequest("Gate3", "HumanApprovalGate", "task3", "", "", "no", nil)
	req3.Status = StatusPending
	if err := DefaultStore.Create(req3); err != nil {
		t.Fatal(err)
	}
	if _, err = DefaultStore.Reject(req3.ID, "tester", "too risky"); err != nil {
		t.Fatal(err)
	}

	pending := DefaultStore.ListPending()
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(pending))
	}
	_ = filepath.Base(dir)
}

func TestPolicyAutoApprove(t *testing.T) {
	SetPolicy(Policy{Enabled: true, AutoApprove: true, Timeout: DefaultPolicy().Timeout})
	req := NewRequest("A", "HumanApprovalGate", "t", "", "", "", nil)
	req = ApplyAutoApproveIfPolicy(req)
	if req.Status != StatusSkipped {
		t.Fatalf("expected skipped, got %s", req.Status)
	}
	SetPolicy(DefaultPolicy())
}
