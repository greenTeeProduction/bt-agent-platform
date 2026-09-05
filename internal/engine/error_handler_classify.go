package engine

import (
	"errors"
	"strings"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// classifyErrorHandlerFailure derives a non-empty error category for a failure
// that reached a ClaudeErrorHandler with no classification (no CircuitBreaker or
// Timeout on its path populated last_error_category). Without this, the guard
// mechanism has nothing to key on — the proposed recovery must guard on
// LastErrorCategoryIs:<cat>/LastErrorNodeIs:<node>, so an empty category forces
// Claude to judge every such failure "unresolvable" (observed in production:
// code_review/devops_ci failures at 2026-07-16 08:11 both returned unresolvable
// purely because the failure carried no category).
//
// GOAP fusion cycles carry a rich failure taxonomy (rate-limit carryover, own-code
// gate rejection, infrastructure wedge, quality/evidence gate) that is far more
// useful to guard on than a generic reliability category, so it is preferred when
// the cycle is a goap fusion one.
func classifyErrorHandlerFailure(bb *Blackboard) string {
	if isGoapFusionCycle(bb) {
		return goapFailureCategory(bb)
	}
	if bb != nil {
		if cat := reliability.ClassifyError(errors.New(bb.Result)); cat != reliability.ErrCatUnknown {
			return cat.String()
		}
	}
	return "unclassified"
}

// isGoapFusionCycle reports whether the blackboard belongs to a goap fusion cycle,
// detected by the goap_fusion_* ChainState keys those actions stamp. This keeps the
// generic handler free of goap-name string matching while still routing goap
// failures to the goap-specific classifier.
func isGoapFusionCycle(bb *Blackboard) bool {
	if bb == nil || bb.ChainState == nil {
		return false
	}
	for k := range bb.ChainState {
		if strings.HasPrefix(k, "goap_fusion_") {
			return true
		}
	}
	return false
}

// goapFailureCategory maps a failed goap fusion cycle onto the platform's existing
// failure taxonomy (see isGoapInfraCycleFailure / goapImplGateFailureMarkers). The
// ordering is specificity-first: an own-code gate rejection reproduces every cycle
// and must not be masked as generic "infra", a rate-limit carryover is an expected
// pause distinct from a real infrastructure wedge, and pending_patch / a drifted
// build tree are each recoverable in their own specific way (re-apply; re-materialize)
// so they get distinct guard labels rather than collapsing into generic "infra".
// This categorization is display/guard-label only — it does NOT change the
// red_pass/infra/genuine refund routing, which classifyGoapCycleFailure
// (actions_goap_fusion_refund.go) alone owns.
func goapFailureCategory(bb *Blackboard) string {
	if bb == nil {
		return "goap_fusion_failure"
	}
	outcome := bb.Outcome
	result := bb.Result
	if strings.Contains(outcome, "rate_limited") || strings.Contains(strings.ToLower(result), "rate limit") {
		return "rate_limit"
	}
	for _, m := range goapImplGateFailureMarkers {
		if strings.Contains(result, m) {
			return "impl_gate"
		}
	}
	if isGoapPendingPatchFailure(outcome, result) {
		return "pending_patch"
	}
	if isGoapWorkingTreeDriftFailure(result) {
		return "working_tree_drift"
	}
	if isGoapInfraCycleFailure(outcome, result) {
		return "infra"
	}
	lower := strings.ToLower(result)
	if strings.Contains(lower, "quality") || strings.Contains(lower, "evidence") {
		return "quality_gate"
	}
	if cat := reliability.ClassifyError(errors.New(result)); cat != reliability.ErrCatUnknown {
		return cat.String()
	}
	return "goap_fusion_failure"
}
