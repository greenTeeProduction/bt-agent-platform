package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// This file is the interaction-time GOAP autopilot (ADR-133 Phase 4): after
// good user-attributed runs the agent mines the user's habits, and when a
// recurring pattern has no automation yet it clones the governed task definition, persists it, and proposes scheduling it as
// an agent through the existing HITL queue. Guard rails: per-pattern dedup +
// rejection memory (persona.AutomationStore), the profile's
// MaxAutoCreatedAgents cap, and HITL default-on.

// considerAutomation runs the observe→propose pipeline for a user and
// returns a result map describing what happened (for MCP surfacing). It is
// deliberately LLM-free — pattern mining uses keyword clustering and the
// task-template selection is deterministic — so it can run synchronously
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

// proposeAutomation finds the exact recurring task template and raises its
// version-bound scheduling proposal (or activates when policy allows).
func proposeAutomation(deps *mcpDeps, user string, profile *persona.Profile, ledger *persona.AutomationStore, pattern persona.RecurringPattern, signature string) map[string]any {
	source, sourceID, err := recurringTaskTemplate(deps, user, pattern.Representative)
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error(), "requires_task_contract": true}
	}
	return proposeTaskAutomation(deps, user, profile, ledger, pattern, signature, sourceID, source, suggestSchedule(pattern))
}

// recurringTaskTemplate reuses the actual governed task from exact successful
// interactions. Keyword similarity never selects another task's instructions.
func recurringTaskTemplate(deps *mcpDeps, user, task string) (*evolution.SerializableNode, string, error) {
	log, err := persona.NewLog(deps.personaStore.Workspace(user))
	if err != nil {
		return nil, "", err
	}
	records, err := log.All()
	if err != nil {
		return nil, "", err
	}
	for _, rec := range slices.Backward(records) {
		if rec.Task != task || rec.Outcome != "success" || rec.TreeID == "" || rec.TreeVersion == "" {
			continue
		}
		tree, err := loadAutomationTemplate(deps, user, rec.TreeID)
		if err != nil {
			continue
		}
		version, _ := evolution.TreeVersion(tree)
		if version == rec.TreeVersion && tree.Metadata["task"] == task {
			return tree, rec.TreeID, nil
		}
	}
	return nil, "", fmt.Errorf("recurring task needs an executable owned task contract; create its task tree before scheduling")
}

func loadAutomationTemplate(deps *mcpDeps, user, id string) (*evolution.SerializableNode, error) {
	if deps.personaStore == nil || user == "" {
		return nil, fmt.Errorf("personal task store and owner required")
	}
	// Reuse the same exact active-version authority as evolution publication.
	// Reading only the legacy file would miss a successfully adopted improvement.
	tree, err := publicationBaseline(deps, id, user)
	if err != nil {
		return nil, err
	}
	kind, _ := tree.Metadata["factory_kind"].(string)
	task, _ := tree.Metadata["task"].(string)
	if tree.Metadata["user"] != user || task == "" || kind != "response" && kind != "file_task" {
		return nil, fmt.Errorf("automation requires an owned response or file task contract")
	}
	if info := engine.ValidateTreeFull(tree); !info.Valid() {
		return nil, fmt.Errorf("invalid automation task: %v", info.Errors)
	}
	return tree, nil
}

func proposeTaskAutomation(deps *mcpDeps, user string, profile *persona.Profile, ledger *persona.AutomationStore, pattern persona.RecurringPattern, signature, sourceID string, tree *evolution.SerializableNode, schedule string) map[string]any {
	sourceVersion, err := evolution.TreeVersion(tree)
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}
	identity := sha256.Sum256([]byte(user + "\x00" + signature + "\x00" + sourceVersion))
	treeID := fmt.Sprintf("factory:automation_%x", identity[:16])
	tree.Name = treeID
	tree.Metadata["source"], tree.Metadata["source_tree_id"], tree.Metadata["source_tree_version"] = "autopilot", sourceID, sourceVersion
	tree.Metadata["pattern_signature"] = signature
	version, err := evolution.TreeVersion(tree)
	if err != nil {
		return map[string]any{"proposed": false, "error": err.Error()}
	}

	result := map[string]any{
		"tree_id":      treeID,
		"tree_version": version,
		"pattern":      pattern.Representative,
		"count":        pattern.Count,
		"signature":    signature,
	}

	planSummary := []string{"Execute the original task", "Validate the declared result"}
	if tree.Type == "FileTask" {
		planSummary = append(planSummary, "Commit output file and verify independent readback")
	}

	agentName := automationAgentName(user, signature)
	proposed := fmt.Sprintf(
		"Prepared the task %q using tree %s and agent %q (schedule: %s). Approve to schedule this exact task definition.",
		pattern.Representative, treeID, agentName, schedule)

	if tree.Type == "FileTask" {
		spec, _ := evolution.ParseFileTask(tree)
		checks, _ := json.Marshal(tree.Metadata["result_contract"])
		proposed += fmt.Sprintf("\nInput artifact: %q; output artifact: %q (inside your artifact directory). Result checks: %s", spec.Input, spec.Output, checks)
	}

	if !profile.Approval.AutoApproveAutomations && hitl.DefaultStore == nil {
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
			"tree_version":      version,
			"agent_name":        agentName,
			"user":              user,
			"pattern_signature": signature,
			"schedule":          schedule,
		})
	req = hitl.ApplyAutoApproveIfPolicy(req)
	limit := profile.Approval.MaxAutoCreatedAgents
	if limit <= 0 {
		limit = 3
	}
	if err := ledger.Reserve(persona.AutomationRecord{
		Signature: signature, Status: persona.AutomationPending, HITLID: req.ID,
		TreeID: treeID, TreeVersion: version, AgentName: agentName, Schedule: schedule, Representative: pattern.Representative,
	}, limit); err != nil {
		result["proposed"], result["error"] = false, err.Error()
		return result
	}
	// A pending record is durable before the tree becomes resolvable. Any
	// subsequent failure leaves admission closed and is reported for repair.
	persistGeneratedTreeForUser(deps, user, treeID, tree, result)
	if result["persisted"] != true {
		result["proposed"] = false
		return result
	}
	if !profile.Approval.AutoApproveAutomations {
		if err := hitl.DefaultStore.Create(req); err != nil {
			result["proposed"], result["error"] = false, err.Error()
			return result
		}
	}
	// Personal task text stays in the owner's workspace, outside the shared KG.
	result["status"] = persona.AutomationPending
	if profile.Approval.AutoApproveAutomations || req.Status == hitl.StatusSkipped {
		activation := finalizeAutomationApproval(deps, req, true)
		if activation["activated"] != true {
			result["proposed"], result["error"] = false, activation["activation_error"]
			return result
		}
		result["status"], result["auto_approved"], result["agent"] = persona.AutomationApproved, true, agentName
	}
	result["proposed"], result["hitl_id"], result["schedule"] = true, req.ID, schedule
	return result
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
	sum := sha256.Sum256([]byte(user + "\x00" + signature))
	return fmt.Sprintf("auto-%x", sum[:16])
}

// registerAutomationTools registers the autopilot's on-demand MCP surface.
func registerAutomationTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_automation_schedule", "Propose scheduling an existing owned task tree with its exact task, file paths and result contract", map[string]engine.Property{
		"user": {Type: "string", Description: "Task owner"}, "tree": {Type: "string", Description: "Owned factory task tree ID"}, "schedule": {Type: "string", Description: "Five-field cron schedule"},
	}, []string{"user", "tree", "schedule"}, func(args json.RawMessage) *engine.ToolResult {
		var params struct {
			User     string `json:"user"`
			Tree     string `json:"tree"`
			Schedule string `json:"schedule"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			return goalError(err.Error())
		}
		tree, err := loadAutomationTemplate(deps, params.User, params.Tree)
		if err != nil {
			return goalError(err.Error())
		}
		if err := agent.ValidateCronSchedule(params.Schedule); err != nil {
			return goalError(err.Error())
		}
		profile, err := deps.personaStore.Load(params.User)
		if err != nil {
			return goalError(err.Error())
		}
		ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(params.User))
		if err != nil {
			return goalError(err.Error())
		}
		task, _ := tree.Metadata["task"].(string)
		signature := fmt.Sprintf("task-%x", sha256.Sum256([]byte(params.Tree)))
		result := proposeTaskAutomation(deps, params.User, profile, ledger, persona.RecurringPattern{Representative: task}, signature, params.Tree, tree, params.Schedule)
		data, _ := json.Marshal(result)
		return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
	})

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
