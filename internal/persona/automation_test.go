package persona

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAutomationStore_UpsertGetRoundTrip pins the ledger basics: a record
// survives a store reopen (ADR-003 file persistence), Get finds it by
// signature, and Upsert with the same signature replaces instead of
// duplicating.
func TestAutomationStore_UpsertGetRoundTrip(t *testing.T) {
	ws := Workspace{Root: t.TempDir(), User: "nico"}
	store, err := NewAutomationStore(ws)
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}

	rec := AutomationRecord{
		Signature:      "report_sales_weekly",
		Status:         AutomationPending,
		HITLID:         "hitl-abc12345",
		TreeID:         "goal:automate_weekly_sales_report",
		Representative: "summarize the weekly sales report",
	}
	if err := store.Upsert(rec); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Reopen: state must come from disk, not memory.
	reopened, err := NewAutomationStore(ws)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, ok, err := reopened.Get("report_sales_weekly")
	if err != nil || !ok {
		t.Fatalf("Get after reopen: ok=%v err=%v", ok, err)
	}
	if got.Status != AutomationPending || got.TreeID != rec.TreeID || got.CreatedAt == 0 {
		t.Errorf("round-tripped record mismatch: %+v", got)
	}

	// Same-signature upsert replaces (no duplicates) and keeps CreatedAt.
	rec.Status = AutomationApproved
	rec.AgentName = "auto-nico-report_sales_weekly"
	if err := reopened.Upsert(rec); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	all, err := reopened.All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("Upsert with same signature must replace, got %d records", len(all))
	}
	if all[0].Status != AutomationApproved || all[0].AgentName == "" {
		t.Errorf("replaced record mismatch: %+v", all[0])
	}
	if all[0].CreatedAt != got.CreatedAt {
		t.Errorf("Upsert must preserve CreatedAt: got %d want %d", all[0].CreatedAt, got.CreatedAt)
	}

	// The ledger file lives at the documented workspace path.
	if _, err := os.Stat(filepath.Join(ws.Root, "automations.json")); err != nil {
		t.Errorf("automations.json not written at workspace root: %v", err)
	}
}

// TestAutomationStore_SetStatusByHITLID pins the approval-hook path: the
// record is looked up by its HITL request ID, and unknown IDs report ok=false
// without erroring (a non-automation HITL approval is not our business).
func TestAutomationStore_SetStatusByHITLID(t *testing.T) {
	store, err := NewAutomationStore(Workspace{Root: t.TempDir(), User: "u"})
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	if err := store.Upsert(AutomationRecord{
		Signature: "sig-a", Status: AutomationPending, HITLID: "hitl-1",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	rec, ok, err := store.SetStatus("hitl-1", AutomationApproved, "auto-u-sig-a")
	if err != nil || !ok {
		t.Fatalf("SetStatus: ok=%v err=%v", ok, err)
	}
	if rec.Status != AutomationApproved || rec.AgentName != "auto-u-sig-a" {
		t.Errorf("SetStatus result mismatch: %+v", rec)
	}

	if _, ok, err := store.SetStatus("hitl-unknown", AutomationRejected, ""); err != nil || ok {
		t.Errorf("unknown HITL ID must be ok=false, err=nil; got ok=%v err=%v", ok, err)
	}
}

// TestAutomationStore_CountApproved pins the MaxAutoCreatedAgents cap input:
// only approved records count as active automations.
func TestAutomationStore_CountApproved(t *testing.T) {
	store, err := NewAutomationStore(Workspace{Root: t.TempDir(), User: "u"})
	if err != nil {
		t.Fatalf("NewAutomationStore: %v", err)
	}
	for _, rec := range []AutomationRecord{
		{Signature: "a", Status: AutomationApproved},
		{Signature: "b", Status: AutomationPending},
		{Signature: "c", Status: AutomationRejected},
		{Signature: "d", Status: AutomationApproved},
	} {
		if err := store.Upsert(rec); err != nil {
			t.Fatalf("Upsert %q: %v", rec.Signature, err)
		}
	}
	n, err := store.CountApproved()
	if err != nil {
		t.Fatalf("CountApproved: %v", err)
	}
	if n != 2 {
		t.Errorf("CountApproved = %d, want 2 (pending/rejected are not active)", n)
	}
}

// TestPatternSignature pins the dedup fingerprint: word order and
// punctuation must not change the signature (a re-mined cluster with a
// different most-recent representative still maps to the same proposal),
// while different tasks produce different signatures.
func TestPatternSignature(t *testing.T) {
	a := PatternSignature("Summarize the weekly sales report!")
	b := PatternSignature("weekly report: summarize sales")
	if a != b {
		t.Errorf("signatures must be order/punctuation independent: %q vs %q", a, b)
	}
	c := PatternSignature("review the deployment pipeline")
	if a == c {
		t.Errorf("different tasks must not collide: %q", a)
	}
	if PatternSignature("") != "pattern" {
		t.Errorf("empty representative must yield the fallback signature")
	}
	if PatternSignature("a an it") != "pattern" {
		t.Errorf("all-stopword/short representative must yield the fallback signature")
	}
}
