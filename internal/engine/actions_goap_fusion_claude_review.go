package engine

// Claude Code review fallback for the GOAP fusion runner: when NotebookLM is
// unavailable (daily quota exhausted or any other failure), the ResearchRouter
// selector in GoapFusionLoopTree falls through to RunClaudeCodeReviewResearch,
// which reviews the daemon's recent auto-approved commits and emits findings
// in the same GOAL/GAP/FILES/TESTS contract the downstream pipeline consumes.
// Spec: docs/superpowers/specs/2026-07-02-goap-fusion-claude-review-fallback-design.md

import (
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

// isGoapNotebookLMQuotaError reports whether a NotebookLM CLI failure is the
// daily-quota kind (RESOURCE_EXHAUSTED / error code 8). It is strictly a
// subset of isGoapNotebookLMFailure: quota-looking text inside a successful
// answer is not a quota error.
func isGoapNotebookLMQuotaError(out string) bool {
	if !isGoapNotebookLMFailure(out) {
		return false
	}
	lower := strings.ToLower(out)
	return strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "error code 8") ||
		strings.Contains(lower, "google rejected the query")
}

// nextNlmQuotaReset returns when the NotebookLM daily quota next resets:
// midnight America/Los_Angeles (Google API daily quotas reset at midnight
// Pacific). If the tz database is unavailable, fall back to now+12h.
func nextNlmQuotaReset(now time.Time) time.Time {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return now.Add(12 * time.Hour)
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
}

// Quota state must survive across scheduled runs (same rationale and
// mechanism as the grill state in actions_goap_fusion.go): agent-scope
// blackboard first, ChainState fallback.

func saveNlmQuotaExhausted(bb *Blackboard, now time.Time) {
	until := nextNlmQuotaReset(now).Format(time.RFC3339)
	setGoapState(bb, "nlm_quota_until", until)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_nlm_quota_until", until,
			"NotebookLM daily quota exhausted until this RFC3339 timestamp", "text")
	}
}

func nlmQuotaExhaustedUntil(bb *Blackboard) (time.Time, bool) {
	var raw string
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_nlm_quota_until"); err == nil {
			raw = strings.TrimSpace(e.Value)
		}
	}
	if raw == "" {
		raw, _ = bb.ChainState["goap_fusion_nlm_quota_until"].(string)
	}
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, false
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return until, true
}

func nlmQuotaExhausted(bb *Blackboard) bool {
	until, ok := nlmQuotaExhaustedUntil(bb)
	return ok && until.After(time.Now())
}

// Last-reviewed SHA tracks how far the Claude review fallback has covered the
// daemon's commit history, so consecutive fallback cycles review disjoint
// ranges instead of the same commits.

func saveLastReviewedSHA(bb *Blackboard, sha string) {
	setGoapState(bb, "last_reviewed_sha", sha)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_last_reviewed_sha", sha,
			"HEAD SHA covered by the last Claude Code review fallback", "text")
	}
}

func loadLastReviewedSHA(bb *Blackboard) string {
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_last_reviewed_sha"); err == nil {
			return strings.TrimSpace(e.Value)
		}
	}
	s, _ := bb.ChainState["goap_fusion_last_reviewed_sha"].(string)
	return strings.TrimSpace(s)
}
