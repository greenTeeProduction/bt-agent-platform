package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// newAutopilotDeps builds an isolated deps bundle for autopilot tests:
// temp persona store, tree store, KG, and agent registry, plus a fresh
// HITL store with a deterministic policy (enabled, no auto-approve).
// Globals are restored on cleanup.
func newAutopilotDeps(t *testing.T) *mcpDeps {
	t.Helper()

	personaStore, err := persona.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("persona store: %v", err)
	}
	treeStore, err := evolution.NewTreeStore(t.TempDir())
	if err != nil {
		t.Fatalf("tree store: %v", err)
	}
	agentReg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("agent registry: %v", err)
	}

	prevStore := hitl.DefaultStore
	prevPolicy := hitl.GetPolicy()
	if _, err := hitl.InitStore(t.TempDir()); err != nil {
		t.Fatalf("hitl store: %v", err)
	}
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: time.Hour, DefaultPrompt: "review"})
	t.Cleanup(func() {
		hitl.DefaultStore = prevStore
		hitl.SetPolicy(prevPolicy)
	})

	return &mcpDeps{
		personaStore: personaStore,
		treeStore:    treeStore,
		kg:           knowledge.NewKnowledgeGraph(),
		agentReg:     agentReg,
	}
}

// seedRecurringTask appends enough similar interactions to make the habit
// miner emit a recurring pattern for the task.
func seedRecurringTask(t *testing.T, deps *mcpDeps, user, task string) {
	t.Helper()
	log, err := persona.NewLog(deps.personaStore.Workspace(user))
	if err != nil {
		t.Fatalf("interaction log: %v", err)
	}
	now := time.Now().Unix()
	for i := 0; i < 3; i++ {
		if err := log.Append(persona.Interaction{
			Task:      task,
			Outcome:   "success",
			Timestamp: now - int64(3-i)*3600,
		}); err != nil {
			t.Fatalf("append interaction: %v", err)
		}
	}
}

// TestConsiderAutomation_ProposesViaHITLOnce pins the observe→propose loop
// (ADR-133 Phase 4): a task repeated 3× yields exactly one HITL automation
// proposal backed by a compiled, persisted, KG-registered tree — and the
// dedup ledger prevents the same habit from being proposed twice.
func TestConsiderAutomation_ProposesViaHITLOnce(t *testing.T) {
	deps := newAutopilotDeps(t)
	seedRecurringTask(t, deps, "nico", "summarize the weekly sales report")

	result := considerAutomation(deps, "nico")
	if result["proposed"] != true {
		t.Fatalf("expected a proposal, got %v", result)
	}
	hitlID, _ := result["hitl_id"].(string)
	if hitlID == "" {
		t.Fatalf("proposal must reference a HITL request: %v", result)
	}
	if result["status"] != persona.AutomationPending {
		t.Errorf("HITL default-on: proposal must be pending, got %v", result["status"])
	}

	// The compiled tree is persisted and resolvable-by-file.
	treeID, _ := result["tree_id"].(string)
	if treeID == "" {
		t.Fatalf("proposal must carry a tree_id: %v", result)
	}
	if file, _ := result["file"].(string); file == "" {
		t.Errorf("compiled tree must be persisted, got %v", result)
	} else if _, err := os.Stat(file); err != nil {
		t.Errorf("persisted tree file missing: %v", err)
	}
	if _, registered := deps.kg.Trees[treeID]; !registered {
		t.Errorf("compiled tree %q must be KG-registered", treeID)
	}

	// The HITL request carries the activation context for the approve hook.
	req, ok := hitl.DefaultStore.Get(hitlID)
	if !ok {
		t.Fatalf("HITL request %q not found", hitlID)
	}
	if req.Status != hitl.StatusPending || req.Context["automation"] != "true" ||
		req.Context["tree_id"] != treeID || req.Context["user"] != "nico" ||
		req.Context["agent_name"] == "" || req.Context["schedule"] == "" {
		t.Errorf("HITL request context incomplete: status=%s context=%v", req.Status, req.Context)
	}

	// Dedup: the same habit is never proposed twice.
	again := considerAutomation(deps, "nico")
	if again["proposed"] != false {
		t.Fatalf("second run must not re-propose the same pattern: %v", again)
	}
	if len(hitl.DefaultStore.ListPending()) != 1 {
		t.Errorf("expected exactly one pending HITL request, got %d", len(hitl.DefaultStore.ListPending()))
	}
}

// TestConsiderAutomation_ApprovalActivatesRejectionRemembers pins the HITL
// resolution paths: approval writes a scheduled agent definition into the
// registry and marks the ledger approved; rejection only marks the ledger so
// the habit is never re-proposed (anti-spam rail).
func TestConsiderAutomation_ApprovalActivatesRejectionRemembers(t *testing.T) {
	deps := newAutopilotDeps(t)
	seedRecurringTask(t, deps, "nico", "summarize the weekly sales report")
	seedRecurringTask(t, deps, "nico", "review the deployment pipeline logs")

	// Proposal 1 → approve → agent scheduled.
	first := considerAutomation(deps, "nico")
	if first["proposed"] != true {
		t.Fatalf("first proposal failed: %v", first)
	}
	req, err := hitl.DefaultStore.Approve(first["hitl_id"].(string), "tester", "ship it")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	activation := finalizeAutomationApproval(deps, req, true)
	if activation == nil || activation["activated"] != true {
		t.Fatalf("approval must activate the automation: %v", activation)
	}
	agentName := req.Context["agent_name"]
	inst, err := deps.agentReg.Get(agentName)
	if err != nil {
		t.Fatalf("approved automation must exist in the agent registry: %v", err)
	}
	if inst.Definition.Tree != req.Context["tree_id"] || inst.Definition.Schedule == "" ||
		inst.Definition.Metadata["auto_created"] != "true" || inst.Definition.Metadata["user"] != "nico" {
		t.Errorf("agent definition mismatch: %+v", inst.Definition)
	}

	// Proposal 2 → reject → remembered, never re-proposed, no agent.
	second := considerAutomation(deps, "nico")
	if second["proposed"] != true {
		t.Fatalf("second pattern must yield a new proposal: %v", second)
	}
	treePath, _ := second["file"].(string)
	if treePath == "" {
		t.Fatalf("second proposal must persist a tree file: %v", second)
	}
	if _, err := os.Stat(treePath); err != nil {
		t.Fatalf("tree file must exist before rejection: %v", err)
	}
	req2, err := hitl.DefaultStore.Reject(second["hitl_id"].(string), "tester", "not useful")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if res := finalizeAutomationApproval(deps, req2, false); res == nil || res["activated"] != false {
		t.Fatalf("rejection must not activate: %v", res)
	}
	if _, err := deps.agentReg.Get(req2.Context["agent_name"]); err == nil {
		t.Error("rejected automation must not create an agent")
	}
	// A rejected automation's compiled tree must be quarantined so it can't
	// be resolved by direct tree-ID even before per-request tree isolation
	// (Milestone 1) lands.
	if _, err := os.Stat(treePath); err == nil {
		t.Errorf("rejected automation's tree file must be quarantined (renamed or deleted), still present at %s", treePath)
	}

	third := considerAutomation(deps, "nico")
	if third["proposed"] != false {
		t.Fatalf("rejected habit must never be re-proposed: %v", third)
	}

	// Ledger reflects both resolutions.
	ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace("nico"))
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	records, err := ledger.All()
	if err != nil || len(records) != 2 {
		t.Fatalf("expected 2 ledger records, got %d (err=%v)", len(records), err)
	}
	statuses := map[string]bool{}
	for _, rec := range records {
		statuses[rec.Status] = true
	}
	if !statuses[persona.AutomationApproved] || !statuses[persona.AutomationRejected] {
		t.Errorf("ledger must hold one approved and one rejected record: %+v", records)
	}
}

// TestConsiderAutomation_AutoApproveAndCap pins the profile policy rails:
// AutoApproveAutomations skips HITL and activates directly, and the
// MaxAutoCreatedAgents cap stops further proposals.
func TestConsiderAutomation_AutoApproveAndCap(t *testing.T) {
	deps := newAutopilotDeps(t)
	if _, err := deps.personaStore.Update("nico", func(p *persona.Profile) {
		p.Approval.AutoApproveAutomations = true
		p.Approval.MaxAutoCreatedAgents = 1
	}); err != nil {
		t.Fatalf("profile update: %v", err)
	}
	seedRecurringTask(t, deps, "nico", "summarize the weekly sales report")
	seedRecurringTask(t, deps, "nico", "review the deployment pipeline logs")

	result := considerAutomation(deps, "nico")
	if result["proposed"] != true || result["auto_approved"] != true {
		t.Fatalf("auto-approve policy must activate directly: %v", result)
	}
	if len(hitl.DefaultStore.ListPending()) != 0 {
		t.Errorf("auto-approved automation must not queue a HITL request")
	}
	if _, err := deps.agentReg.Get(result["agent"].(string)); err != nil {
		t.Fatalf("auto-approved automation must exist in the registry: %v", err)
	}

	// Cap of 1 is now reached: the second habit must be skipped.
	capped := considerAutomation(deps, "nico")
	if capped["proposed"] != false {
		t.Fatalf("cap must stop further proposals: %v", capped)
	}
	if skipped, _ := capped["skipped"].(string); skipped == "" {
		t.Errorf("cap skip must be explained: %v", capped)
	}
}

// TestBTAutomationProposeRegistered pins the MCP surface: the tool is
// registered by registerMCPTools and returns the proposal result plus the
// user's automation ledger as JSON.
func TestBTAutomationProposeRegistered(t *testing.T) {
	deps := newAutopilotDeps(t)
	seedRecurringTask(t, deps, "nico", "summarize the weekly sales report")

	server := engine.NewServer("test")
	registerMCPTools(server, deps)
	if !server.HasTool("bt_automation_propose") {
		t.Fatal("bt_automation_propose must be registered by registerMCPTools")
	}

	res, ok := server.Invoke("bt_automation_propose", json.RawMessage(`{"user":"nico"}`))
	if !ok || res == nil || len(res.Content) == 0 {
		t.Fatal("bt_automation_propose returned no content")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &out); err != nil {
		t.Fatalf("result is not valid JSON: %v (text=%q)", err, res.Content[0].Text)
	}
	if out["proposed"] != true {
		t.Fatalf("expected a proposal, got %v", out)
	}
	if _, hasLedger := out["automations"]; !hasLedger {
		t.Errorf("result must include the automation ledger, got keys %v", out)
	}
}
