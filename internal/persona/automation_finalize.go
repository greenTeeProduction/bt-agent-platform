package persona

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
)

// ActivateAutomation writes an approved automation into the agent registry
// as a scheduled agent definition. Binary-agnostic: callers that need to
// refresh derived state (e.g. cmd/bt-agent's A2A card registry) do so after
// this returns successfully.
func ActivateAutomation(reg *agent.Registry, user, agentName, treeID, signature, schedule, representative string) error {
	if reg == nil {
		return fmt.Errorf("agent registry not configured")
	}
	_, err := reg.Create(agent.Definition{
		Name:        agentName,
		Description: "Auto-created automation for recurring task: " + representative,
		Tree:        treeID,
		Schedule:    schedule,
		Metadata: map[string]string{
			"auto_created":      "true",
			"user":              user,
			"pattern_signature": signature,
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return nil // idempotent: re-approval of an existing agent is fine
		}
		return err
	}
	return nil
}

// FinalizeAutomationApproval activates an approved automation proposal (or
// quarantines its compiled tree on rejection) and updates the user's
// automation ledger. Binary-agnostic so both the MCP bt_hitl_approve/
// bt_hitl_reject path and the dashboard's HITL resolution path finalize
// automations identically: dashboard-approved/rejected automations
// activate, resume, and quarantine exactly like the MCP path.
func FinalizeAutomationApproval(reg *agent.Registry, store *Store, req *hitl.Request, approved bool) map[string]any {
	if req == nil || req.Context["automation"] != "true" {
		return nil
	}
	user := req.Context["user"]
	out := map[string]any{"automation": true, "user": user}
	var ledger *AutomationStore
	if store != nil && user != "" {
		ledger, _ = NewAutomationStore(store.Workspace(user))
	}

	if !approved {
		if ledger != nil {
			_, _, _ = ledger.SetStatus(req.ID, AutomationRejected, "")
		}
		if store != nil && user != "" {
			ws := store.Workspace(user)
			if qerr := evolution.QuarantineNamedTree(ws.TreesDir(), req.Context["tree_id"]); qerr != nil {
				engine.Warn("FinalizeAutomationApproval: tree quarantine failed", "tree", req.Context["tree_id"], "error", qerr)
			}
		}
		out["activated"] = false
		return out
	}

	agentName := req.Context["agent_name"]
	if err := ActivateAutomation(reg, user, agentName, req.Context["tree_id"],
		req.Context["pattern_signature"], req.Context["schedule"], req.Task); err != nil {
		out["activated"] = false
		out["activation_error"] = err.Error()
		return out
	}
	if ledger != nil {
		_, _, _ = ledger.SetStatus(req.ID, AutomationApproved, agentName)
	}
	out["activated"] = true
	out["agent"] = agentName
	out["schedule"] = req.Context["schedule"]
	return out
}

// FinalizeFeedbackEscalation is the resume half of the feedback-escalation
// loop (Q4 Personalization & Self-Growth milestone 2/3): a human reviewing a
// FeedbackReviewEscalation HITL request must be able to actually reactivate
// the paused automation, not just leave it flagged forever. Approving flips
// the AutomationRecord back to AutomationApproved so the engine's execution
// gate resolves the tree again; rejecting leaves the record in
// AutomationFlagged so the automation stays paused. Binary-agnostic like
// FinalizeAutomationApproval, so both the MCP bt_hitl_approve/bt_hitl_reject
// path and the dashboard's HITL resolution path finalize identically. No-ops
// for HITL requests that aren't a feedback-review escalation (e.g.
// automation-proposal approvals, handled by FinalizeAutomationApproval).
func FinalizeFeedbackEscalation(store *Store, req *hitl.Request, approved bool) {
	if req == nil || req.NodeName != "FeedbackReviewEscalation" {
		return
	}
	user := req.Context["user"]
	signature := req.Context["signature"]
	if store == nil || user == "" || signature == "" {
		return
	}
	ledger, err := NewAutomationStore(store.Workspace(user))
	if err != nil {
		return
	}
	rec, exists, err := ledger.Get(signature)
	if err != nil || !exists || rec.Status != AutomationFlagged {
		return
	}
	if !approved {
		// Rejected — the automation stays paused; nothing further to do.
		return
	}
	rec.Status = AutomationApproved
	if err := ledger.Upsert(*rec); err != nil {
		engine.Warn("failed to resume automation after feedback-review approval",
			"tree", rec.TreeID, "user", user, "error", err)
	}
}
