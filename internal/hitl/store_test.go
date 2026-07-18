package hitl

import (
	"bytes"
	"encoding/json"
	"os"
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

// TestStoreSave_UsesCanonicalAtomicJSONFormatAndPermission pins the two
// invariants the util.SaveJSONAtomic migration (Q5 Consistency & Reuse,
// milestone 3/3) must satisfy for Store.save: the on-disk bytes must match
// util.SaveJSONAtomic's canonical json.MarshalIndent output (today save()
// deliberately writes compact json.Marshal instead, per its "Compact JSON"
// comment), while the 0600 file mode — tighter than SaveJSONAtomic's
// hardcoded 0644 default because requests can carry sensitive review
// context — must be preserved rather than widened.
func TestStoreSave_UsesCanonicalAtomicJSONFormatAndPermission(t *testing.T) {
	dir := t.TempDir()
	s, err := InitStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	req := NewRequest("Gate", "HumanApprovalGate", "task", "", "", "approve?", nil)
	req.Status = StatusPending
	if err := s.Create(req); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(s.path)
	if err != nil {
		t.Fatalf("stat store file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file mode = %v, want 0600 — util.SaveJSONAtomic's migration must not widen hitl's existing permission to its own 0644 default", perm)
	}

	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatalf("read store file: %v", err)
	}
	var got []*Request
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal store file: %v", err)
	}
	want, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("re-marshal for comparison: %v", err)
	}
	if !bytes.Equal(raw, want) {
		t.Errorf("save() did not write util.SaveJSONAtomic's canonical indented JSON format\ngot:  %s\nwant: %s", raw, want)
	}
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
