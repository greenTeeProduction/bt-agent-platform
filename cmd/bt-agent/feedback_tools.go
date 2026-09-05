package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/hitl"
	"github.com/nico/go-bt-evolve/internal/persona"
)

// User feedback as a fitness signal (ADR-133 Phase 5): bt_feedback records an
// explicit 👍/👎 (plus optional correction text) as a reflection record with
// UserFeedback set. The evaluator folds these into the user_satisfaction
// fitness dimension, so the gardener's evolution of a personal tree is pulled
// toward what its owner actually liked — not just what succeeded. Two or more
// negatives flag the tree for supervised review (risk rail from the plan).

// feedbackReviewThreshold is the negative-signal count at which a tree is
// flagged for supervised re-run instead of continued autonomous evolution.
const feedbackReviewThreshold = 2

// recordUserFeedback persists one feedback signal and returns the tool
// result: current satisfaction ratio, signal counts, and the review flag.
func recordUserFeedback(deps *mcpDeps, user, treeID, signal, comment string) map[string]any {
	signal = strings.ToLower(strings.TrimSpace(signal))
	switch signal {
	case "👍", "up", "+1":
		signal = evolution.FeedbackPositive
	case "👎", "down", "-1":
		signal = evolution.FeedbackNegative
	}
	if signal != evolution.FeedbackPositive && signal != evolution.FeedbackNegative {
		return map[string]any{"error": fmt.Sprintf("signal must be %q or %q", evolution.FeedbackPositive, evolution.FeedbackNegative)}
	}
	// Canonicalize once: the record, the slug, and the cumulative
	// FilterByTreeNameStrict tally must all see the same identifier, or a
	// trailing space creates records no later lookup ever matches.
	user = strings.TrimSpace(user)
	treeID = strings.TrimSpace(treeID)
	if user == "" || treeID == "" {
		return map[string]any{"error": "user and tree are required"}
	}
	if deps.refStore == nil {
		return map[string]any{"error": "reflection store not configured"}
	}

	// Unique TaskID: the store's default (millisecond timestamp) can collide
	// when feedback arrives in quick succession, silently overwriting records.
	rec := &evolution.Record{
		TaskID:       fmt.Sprintf("feedback-%s-%d", goalTreeSlug(treeID), time.Now().UnixNano()),
		Task:         "User feedback on tree " + treeID,
		TreeName:     treeID,
		User:         user,
		UserFeedback: signal,
	}
	if signal == evolution.FeedbackPositive {
		rec.Outcome = evolution.Success
		rec.WhatWentWell = []string{"user approved the result"}
		if comment != "" {
			rec.WhatWentWell = append(rec.WhatWentWell, comment)
		}
	} else {
		rec.Outcome = evolution.Failure
		rec.WhatToImprove = []string{"user rejected the result"}
		if comment != "" {
			rec.WhatToImprove = append(rec.WhatToImprove, comment)
		}
	}
	if err := deps.refStore.Save(rec); err != nil {
		return map[string]any{"error": err.Error()}
	}

	// Report the tree's cumulative feedback standing.
	positives, negatives := 0, 0
	if all, err := deps.refStore.LoadAll(); err == nil {
		for _, r := range evolution.FilterByTreeNameStrict(all, treeID) {
			switch r.UserFeedback {
			case evolution.FeedbackPositive:
				positives++
			case evolution.FeedbackNegative:
				negatives++
			}
		}
	}
	result := map[string]any{
		"recorded":  true,
		"tree_id":   treeID,
		"signal":    signal,
		"positives": positives,
		"negatives": negatives,
	}
	if positives+negatives > 0 {
		result["satisfaction"] = float64(positives) / float64(positives+negatives)
	}
	if negatives >= feedbackReviewThreshold {
		result["flagged_for_review"] = true
		engine.Warn("tree flagged for supervised review after repeated negative feedback",
			"tree", treeID, "user", user, "negatives", negatives)
		if hitlID := escalateFlaggedTreeForReview(deps, user, treeID, negatives); hitlID != "" {
			result["hitl_id"] = hitlID
		}
	}
	return result
}

// automationFlaggedStatus aliases the shared persona.AutomationFlagged status
// (see its doc comment for the invariant); kept as a local name since
// escalateFlaggedTreeForReview below is still cmd/bt-agent-local.
const automationFlaggedStatus = persona.AutomationFlagged

// escalateFlaggedTreeForReview raises a HITL escalation for a tree that just
// crossed feedbackReviewThreshold negatives and pauses the automation
// tracked against it, pending human review (Q4 Personalization milestone 4).
// Returns the new request's ID, or "" when the tree has no automation to
// pause — e.g. a tree compiled manually and never proposed through the
// autopilot has nothing to escalate.
func escalateFlaggedTreeForReview(deps *mcpDeps, user, treeID string, negatives int) string {
	if deps.personaStore == nil || hitl.DefaultStore == nil {
		return ""
	}
	ledger, err := persona.NewAutomationStore(deps.personaStore.Workspace(user))
	if err != nil {
		return ""
	}
	records, err := ledger.All()
	if err != nil {
		return ""
	}
	var rec *persona.AutomationRecord
	for i := range records {
		if records[i].TreeID == treeID {
			rec = &records[i]
			break
		}
	}
	if rec == nil {
		return ""
	}
	if rec.Status == automationFlaggedStatus {
		// Already flagged and pending review — don't spam another HITL
		// request for every subsequent negative on the same tree.
		return ""
	}

	req := hitl.NewRequest("FeedbackReviewEscalation", "automation-review",
		fmt.Sprintf("Tree %s received %d negative feedback signals in a row", treeID, negatives),
		"", "", "Repeated negative user feedback — review before this automation resumes.",
		map[string]any{
			"tree_id":    treeID,
			"user":       user,
			"agent_name": rec.AgentName,
			"signature":  rec.Signature,
		})
	if err := hitl.DefaultStore.Create(req); err != nil {
		engine.Warn("failed to raise HITL escalation for flagged tree",
			"tree", treeID, "user", user, "error", err)
		return ""
	}

	rec.Status = automationFlaggedStatus
	if err := ledger.Upsert(*rec); err != nil {
		engine.Warn("failed to pause automation after flagging",
			"tree", treeID, "user", user, "error", err)
	}
	return req.ID
}

// registerFeedbackTools registers the user-feedback MCP surface.
func registerFeedbackTools(server *engine.Server, deps *mcpDeps) {
	server.RegisterTool("bt_feedback", "Record explicit user feedback (positive/negative + optional correction) on a tree's output; feeds the user_satisfaction fitness dimension used by tree evolution",
		map[string]engine.Property{
			"user":    {Type: "string", Description: "User ID (persona owner) giving the feedback"},
			"tree":    {Type: "string", Description: "Tree ID the feedback applies to (e.g. goal:automate_reports)"},
			"signal":  {Type: "string", Description: "\"positive\" (👍) or \"negative\" (👎)"},
			"comment": {Type: "string", Description: "Optional correction or praise text stored with the reflection"},
		},
		[]string{"user", "tree", "signal"},
		func(args json.RawMessage) *engine.ToolResult {
			var params struct {
				User    string `json:"user"`
				Tree    string `json:"tree"`
				Signal  string `json:"signal"`
				Comment string `json:"comment"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return goalError(err.Error())
			}
			result := recordUserFeedback(deps, params.User, params.Tree, params.Signal, params.Comment)
			data, _ := json.Marshal(result)
			return &engine.ToolResult{Content: []engine.ContentItem{{Type: "text", Text: string(data)}}}
		})
}
