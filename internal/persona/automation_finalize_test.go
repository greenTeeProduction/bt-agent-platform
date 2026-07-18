package persona_test

import (
	"os"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// newFinalizeTestDeps builds an isolated agent registry and persona store for
// exercising persona.FinalizeAutomationApproval directly, without needing the
// full cmd/bt-agent MCP dependency bundle.
func newFinalizeTestDeps(t *testing.T) (*agent.Registry, *persona.Store) {
	t.Helper()
	store, err := persona.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("persona store: %v", err)
	}
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("agent registry: %v", err)
	}
	return reg, store
}

// TestFinalizeAutomationApproval_ApprovedActivatesAgentAndLedger pins the
// binary-agnostic finalization contract (milestone 1 of the dashboard HITL
// approval-finalization program): approval must schedule the agent in the
// registry and mark the ledger record approved.
func TestFinalizeAutomationApproval_ApprovedActivatesAgentAndLedger(t *testing.T) {
	reg, store := newFinalizeTestDeps(t)
	user := "nico"
	ledger, err := persona.NewAutomationStore(store.Workspace(user))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	signature := "sig-1"
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature:      signature,
		Status:         persona.AutomationPending,
		HITLID:         "req-1",
		TreeID:         "goal:demo",
		Representative: "demo task",
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	req := &hitl.Request{
		ID:   "req-1",
		Task: "demo task",
		Context: map[string]string{
			"automation":        "true",
			"tree_id":           "goal:demo",
			"agent_name":        "auto-nico-sig-1",
			"user":              user,
			"pattern_signature": signature,
			"schedule":          "0 9 * * *",
		},
	}

	out := persona.FinalizeAutomationApproval(reg, store, req, true)
	if out == nil || out["activated"] != true {
		t.Fatalf("approval must activate the automation: %v", out)
	}
	if out["agent"] != "auto-nico-sig-1" {
		t.Errorf("expected agent name in result, got %v", out)
	}

	inst, err := reg.Get("auto-nico-sig-1")
	if err != nil {
		t.Fatalf("approved automation must exist in the agent registry: %v", err)
	}
	if inst.Definition.Tree != "goal:demo" || inst.Definition.Schedule != "0 9 * * *" ||
		inst.Definition.Metadata["auto_created"] != "true" || inst.Definition.Metadata["user"] != user {
		t.Errorf("agent definition mismatch: %+v", inst.Definition)
	}

	rec, ok, err := ledger.Get(signature)
	if err != nil || !ok {
		t.Fatalf("ledger record missing: ok=%v err=%v", ok, err)
	}
	if rec.Status != persona.AutomationApproved || rec.AgentName != "auto-nico-sig-1" {
		t.Errorf("ledger must reflect approval: %+v", rec)
	}
}

// TestFinalizeAutomationApproval_RejectedQuarantinesTreeAndLedger pins the
// reject path: the compiled tree must be quarantined (evolution.QuarantineNamedTree)
// and the ledger record marked rejected, with no agent created.
func TestFinalizeAutomationApproval_RejectedQuarantinesTreeAndLedger(t *testing.T) {
	reg, store := newFinalizeTestDeps(t)
	user := "nico"
	ws := store.Workspace(user)
	ledger, err := persona.NewAutomationStore(ws)
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	signature := "sig-2"
	treeID := "goal:reject-me"
	if err := ledger.Upsert(persona.AutomationRecord{
		Signature:      signature,
		Status:         persona.AutomationPending,
		HITLID:         "req-2",
		TreeID:         treeID,
		Representative: "reject task",
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	treePath, err := evolution.SaveNamedTree(ws.TreesDir(), treeID, &evolution.SerializableNode{Type: "action", Name: "noop"})
	if err != nil {
		t.Fatalf("save tree: %v", err)
	}

	req := &hitl.Request{
		ID:   "req-2",
		Task: "reject task",
		Context: map[string]string{
			"automation":        "true",
			"tree_id":           treeID,
			"agent_name":        "auto-nico-sig-2",
			"user":              user,
			"pattern_signature": signature,
			"schedule":          "0 9 * * 1",
		},
	}

	out := persona.FinalizeAutomationApproval(reg, store, req, false)
	if out == nil || out["activated"] != false {
		t.Fatalf("rejection must not activate: %v", out)
	}
	if _, err := reg.Get("auto-nico-sig-2"); err == nil {
		t.Error("rejected automation must not create an agent")
	}
	if _, err := os.Stat(treePath); err == nil {
		t.Errorf("rejected automation's tree file must be quarantined, still present at %s", treePath)
	}
	if _, err := os.Stat(treePath + ".rejected"); err != nil {
		t.Errorf("expected quarantined tree file at %s.rejected: %v", treePath, err)
	}

	rec, ok, err := ledger.Get(signature)
	if err != nil || !ok {
		t.Fatalf("ledger record missing: ok=%v err=%v", ok, err)
	}
	if rec.Status != persona.AutomationRejected {
		t.Errorf("ledger must reflect rejection: %+v", rec)
	}
}

// TestFinalizeAutomationApproval_NilOrNonAutomationRequestIgnored pins the
// guard: only HITL requests carrying the automation context are handled.
func TestFinalizeAutomationApproval_NilOrNonAutomationRequestIgnored(t *testing.T) {
	reg, store := newFinalizeTestDeps(t)
	if out := persona.FinalizeAutomationApproval(reg, store, nil, true); out != nil {
		t.Errorf("nil request must be ignored, got %v", out)
	}
	req := &hitl.Request{ID: "req-3", Context: map[string]string{}}
	if out := persona.FinalizeAutomationApproval(reg, store, req, true); out != nil {
		t.Errorf("non-automation request must be ignored, got %v", out)
	}
}
