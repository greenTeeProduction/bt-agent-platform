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
	return activateAutomationVersion(reg, user, agentName, treeID, signature, schedule, representative, "")
}

func activateAutomationVersion(reg *agent.Registry, user, agentName, treeID, signature, schedule, representative, version string) error {
	if reg == nil {
		return fmt.Errorf("agent registry not configured")
	}
	if strings.TrimSpace(user) == "" || strings.TrimSpace(treeID) == "" || strings.TrimSpace(signature) == "" || strings.TrimSpace(schedule) == "" || strings.TrimSpace(representative) == "" {
		return fmt.Errorf("automation requires owner, tree, signature, schedule and exact task")
	}
	metadata := map[string]string{"auto_created": "true", "user": user, "pattern_signature": signature}
	if version != "" {
		metadata["tree_version"] = version
	}
	_, err := reg.EnsureDefinition(agent.Definition{
		Name:        agentName,
		Description: representative,
		Tree:        treeID,
		Schedule:    schedule,
		Metadata:    metadata,
	})
	return err
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
	out["activated"] = false
	fail := func(err error) map[string]any { out["activation_error"] = err.Error(); return out }
	if store == nil || strings.TrimSpace(user) == "" {
		return fail(fmt.Errorf("automation approval requires an owner store"))
	}
	ledger, err := NewAutomationStore(store.Workspace(user))
	if err != nil {
		return fail(err)
	}
	agentName := req.Context["agent_name"]
	err = ledger.transition(req.Context["pattern_signature"], func(rec *AutomationRecord) error {
		if req.ID == "" || rec.HITLID != req.ID || rec.TreeID != req.Context["tree_id"] {
			return fmt.Errorf("automation request does not match the reserved owner, request and tree")
		}
		// Legacy incomplete proposals may still be rejected. Activating one
		// requires a complete reservation; missing fields are not consent.
		if (approved || rec.Representative != "") && rec.Representative != req.Task ||
			(approved || rec.AgentName != "") && rec.AgentName != agentName ||
			(approved || rec.Schedule != "") && rec.Schedule != req.Context["schedule"] {
			return fmt.Errorf("automation approval does not match the reserved agent, schedule and task")
		}
		if !approved {
			rec.Status = AutomationRejected
			return nil
		}
		if rec.Status != AutomationPending && rec.Status != AutomationApproved {
			return fmt.Errorf("automation is %s; original approval cannot reactivate it", rec.Status)
		}
		if rec.TreeVersion != "" {
			tree, err := evolution.LoadNamedTree(store.Workspace(user).TreesDir(), rec.TreeID)
			if err != nil {
				return err
			}
			version, err := evolution.TreeVersion(tree)
			if err != nil || version != rec.TreeVersion || req.Context["tree_version"] != version || tree.Metadata["user"] != user || tree.Metadata["task"] != rec.Representative {
				return fmt.Errorf("automation definition changed since proposal")
			}
		}
		if err := activateAutomationVersion(reg, user, rec.AgentName, rec.TreeID, rec.Signature, rec.Schedule, rec.Representative, rec.TreeVersion); err != nil {
			return err
		}
		rec.Status = AutomationApproved
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if !approved {
		if err := evolution.QuarantineNamedTree(store.Workspace(user).TreesDir(), req.Context["tree_id"]); err != nil {
			out["quarantine_error"] = err.Error()
		}
		return out
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
	if !approved {
		return
	}
	if err := ledger.transition(signature, func(rec *AutomationRecord) error {
		if rec.Status != AutomationFlagged {
			return fmt.Errorf("automation is not flagged")
		}
		rec.Status = AutomationApproved
		return nil
	}); err != nil {
		engine.Warn("failed to resume automation after feedback-review approval", "user", user, "error", err)
	}
}
