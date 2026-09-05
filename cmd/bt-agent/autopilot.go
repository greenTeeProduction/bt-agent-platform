package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/goap"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/knowledge"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// This file is the interaction-time GOAP autopilot (ADR-133 Phase 4): after
// good user-attributed runs the agent mines the user's habits, and when a
// recurring pattern has no automation yet it compiles a tree (Phase 2 goal →
// GOAP plan → Phase 3 compiler), persists it, and proposes scheduling it as
// an agent through the existing HITL queue. Guard rails: per-pattern dedup +
// rejection memory (persona.AutomationStore), the profile's
// MaxAutoCreatedAgents cap, and HITL default-on.

// considerAutomation runs the observe→propose pipeline for a user and
// returns a result map describing what happened (for MCP surfacing). It is
// deliberately LLM-free — pattern mining uses keyword clustering and the
// goal/plan/compile path is deterministic — so it can run synchronously
// after bt_run_task without noticeable latency.
func considerAutomation(deps *mcpDeps, user string) map[string]any {
	if deps.personaStore == nil {
		return map[string]any{"proposed": false, "skipped": "persona store not configured"}
	}
	if strings.TrimSpace(user) == "" {
		return map[string]any{"proposed": false, "skipped": "no user"}
	}

	profile, err := deps.personaStore.Load(user)
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}
	ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(user))
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}

	// Automation-spam guard: cap active auto-created agents per user.
	approved, err := ledger.CountApproved()
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}
	maxActive := profile.Approval.MaxAutoCreatedAgents
	if maxActive <= 0 {
		maxActive = 3
	}
	if approved >= maxActive {
		return map[string]any{
			"proposed": false,
			"skipped":  fmt.Sprintf("automation cap reached (%d/%d active)", approved, maxActive),
		}
	}

	// Keyword-only mining keeps the in-run hook fast and Ollama-independent.
	patterns, _, err := mineUserPatterns(deps, user, 0, 0, false)
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}
	if len(patterns) == 0 {
		return map[string]any{"proposed": false, "skipped": "no recurring patterns"}
	}

	// First pattern without a ledger entry (pending, approved, or rejected —
	// each habit is proposed at most once) is the proposal candidate.
	for _, pattern := range patterns {
		signature := persona.PatternSignature(pattern.Representative)
		if _, exists, lerr := ledger.Get(signature); lerr != nil || exists {
			continue
		}
		return proposeAutomation(deps, user, profile, ledger, pattern, signature)
	}
	return map[string]any{"proposed": false, "skipped": "all recurring patterns already proposed"}
}

// proposeAutomation compiles the pattern into a tree and raises the HITL
// proposal (or activates directly when policy allows).
func proposeAutomation(deps *mcpDeps, user string, profile *persona.Profile, ledger *persona.AutomationStore, pattern persona.RecurringPattern, signature string) map[string]any {
	// Goal → plan → compiled tree (Phases 2+3, deterministic path).
	factory := goalFactory(deps)
	goal := factory.FromPattern(pattern.Representative, pattern.Count)
	planner := goap.NewPlanner(factory.Actions, 50, 10000)
	plan := planner.Plan(factory.InitialState.Clone(), goal)
	if plan == nil || len(plan.Steps) == 0 {
		return map[string]any{"proposed": false, "error": "no plan for automation goal " + goal.Name}
	}

	treeID := "goal:" + goalTreeSlug(goal.Name)
	goapNode, err := goap.CompilePlanToTree(plan, goap.CompileOptions{
		TreeName:     treeID,
		InitialState: factory.InitialState,
		KnownAction:  func(name string) bool { return engine.GetAction(name) != nil },
		StyleHints:   personaStyleHints(deps, user),
		Provenance: map[string]any{
			"user":              user,
			"source":            "autopilot",
			"pattern_signature": signature,
			"pattern_count":     pattern.Count,
		},
	})
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}
	tree := evolution.FromGoapNode(goapNode)

	result := map[string]any{
		"tree_id":   treeID,
		"pattern":   pattern.Representative,
		"count":     pattern.Count,
		"signature": signature,
	}
	persistGeneratedTreeForUser(deps, user, treeID, tree, result)
	if result["persisted"] != true {
		result["proposed"] = false
		return result
	}
	planSummary := make([]string, 0, len(plan.Steps))
	for _, s := range plan.Steps {
		planSummary = append(planSummary, s.Name)
	}
	// Seed compile-time evidence so the gardener can evolve the tree (Phase 5).
	seedCompileReflection(deps, user, treeID, goal.Name, planSummary)
	if deps.kg != nil {
		deps.kg.Register(&knowledge.TreeMeta{
			ID:          treeID,
			Name:        treeID,
			Category:    "goal",
			Description: "Autopilot automation for recurring task: " + pattern.Representative,
			NodeCount:   evolution.CountNodes(tree),
			Keywords:    strings.Fields(strings.ToLower(pattern.Representative)),
			Capabilities: []knowledge.Capability{
				{Action: "goal_automation", Domain: "goal", Strength: 0.7},
			},
		})
	}

	schedule := suggestSchedule(pattern)
	agentName := automationAgentName(user, signature)
	proposed := fmt.Sprintf(
		"I noticed you asked for similar tasks %d times (last: %q). I compiled tree %s and prepared agent %q (schedule: %s). Approve to schedule it.",
		pattern.Count, pattern.Representative, treeID, agentName, schedule)

	// Profile-level auto-approval bypasses HITL entirely.
	if profile.Approval.AutoApproveAutomations {
		if err := activateAutomation(deps, user, agentName, treeID, signature, schedule, pattern.Representative); err != nil {
			result["proposed"] = false
			result["error"] = err.Error()
			return result
		}
		_ = ledger.Upsert(persona.AutomationRecord{
			Signature:      signature,
			Status:         persona.AutomationApproved,
			TreeID:         treeID,
			AgentName:      agentName,
			Representative: pattern.Representative,
		})
		result["proposed"] = true
		result["auto_approved"] = true
		result["agent"] = agentName
		result["schedule"] = schedule
		return result
	}

	// HITL proposal (default path).
	if hitl.DefaultStore == nil {
		result["proposed"] = false
		result["error"] = "HITL store not initialized"
		return result
	}
	req := hitl.NewRequest("AutomationProposal", "automation",
		pattern.Representative, strings.Join(planSummary, " → "), proposed,
		"Approve to schedule this automation as an agent.",
		map[string]any{
			"automation":        "true",
			"tree_id":           treeID,
			"agent_name":        agentName,
			"user":              user,
			"pattern_signature": signature,
			"schedule":          schedule,
		})
	req = hitl.ApplyAutoApproveIfPolicy(req)
	if err := hitl.DefaultStore.Create(req); err != nil {
		result["proposed"] = false
		result["error"] = err.Error()
		return result
	}

	status := persona.AutomationPending
	// Global HITL policy may have skipped the gate (dev/test auto-approve).
	if req.Status == hitl.StatusSkipped {
		if err := activateAutomation(deps, user, agentName, treeID, signature, schedule, pattern.Representative); err != nil {
			result["proposed"] = false
			result["error"] = err.Error()
			return result
		}
		status = persona.AutomationApproved
		result["auto_approved"] = true
		result["agent"] = agentName
	}
	_ = ledger.Upsert(persona.AutomationRecord{
		Signature:      signature,
		Status:         status,
		HITLID:         req.ID,
		TreeID:         treeID,
		AgentName:      agentName,
		Representative: pattern.Representative,
	})
	result["proposed"] = true
	result["hitl_id"] = req.ID
	result["status"] = status
	result["schedule"] = schedule
	return result
}

// activateAutomation writes the approved automation into the agent registry
// as a scheduled agent definition, delegating the binary-agnostic part to
// persona.ActivateAutomation and refreshing A2A cards on success.
func activateAutomation(deps *mcpDeps, user, agentName, treeID, signature, schedule, representative string) error {
	if err := persona.ActivateAutomation(deps.agentReg, user, agentName, treeID, signature, schedule, representative); err != nil {
		return err
	}
	if deps.refreshA2ACards != nil {
		if rerr := deps.refreshA2ACards(); rerr != nil {
			engine.Warn("a2a: card refresh after activateAutomation failed", "agent", agentName, "error", rerr)
		}
	}
	return nil
}

// finalizeAutomationApproval activates an approved automation proposal and
// updates the user's ledger, or quarantines its tree on rejection. Called
// from bt_hitl_approve/bt_hitl_reject for requests carrying the automation
// context. Delegates the binary-agnostic finalization to
// persona.FinalizeAutomationApproval and refreshes A2A cards on activation.
func finalizeAutomationApproval(deps *mcpDeps, req *hitl.Request, approved bool) map[string]any {
	out := persona.FinalizeAutomationApproval(deps.agentReg, deps.personaStore, req, approved)
	if out != nil && out["activated"] == true && deps.refreshA2ACards != nil {
		agentName, _ := out["agent"].(string)
		if rerr := deps.refreshA2ACards(); rerr != nil {
			engine.Warn("a2a: card refresh after activateAutomation failed", "agent", agentName, "error", rerr)
		}
	}
	return out
}

// suggestSchedule derives a cron suggestion from the pattern's observed
// frequency: roughly daily habits run every morning, everything else weekly.
func suggestSchedule(pattern persona.RecurringPattern) string {
	spanDays := float64(pattern.LastSeen-pattern.FirstSeen) / 86400.0
	if spanDays < 1 {
		spanDays = 1
	}
	perDay := float64(pattern.Count) / spanDays
	if perDay >= 0.75 {
		return "0 9 * * *" // daily, 09:00
	}
	return "0 9 * * 1" // weekly, Monday 09:00
}

// automationAgentName builds a filesystem-safe agent name for an
// auto-created automation.
func automationAgentName(user, signature string) string {
	sig := signature
	if len(sig) > 40 {
		sig = sig[:40]
	}
	return "auto-" + persona.SanitizeUserID(user) + "-" + sig
}

// registerAutomationTools registers the autopilot's on-demand MCP surface.
func registerAutomationTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_automation_propose", "Run the automation autopilot for a user: mine recurring habits and propose (or auto-approve) a compiled automation via HITL",
		map[string]engine.Property{
			"user": {Type: "string", Description: "User ID (persona owner)"},
		},
		[]string{"user"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User string `json:"user"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			result := considerAutomation(deps, params.User)

			// Include the ledger so callers see the full proposal history.
			if deps.personaStore != nil && strings.TrimSpace(params.User) != "" {
				if ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(params.User)); err == nil {
					if records, err := ledger.All(); err == nil {
						result["automations"] = records
					}
				}
			}
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}
