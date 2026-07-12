package engine

import (
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/research"
)

// The milestone-abandon budget (goapProgramMaxMilestoneAttempts) exists for
// fabricated/unbuildable milestones the implementation agent keeps declining.
// Cycles that die on *infrastructure* — Claude rate limit, commit gate wedged
// by an external landing, apply/sync refusal, worktree failure — must not
// consume it: on 2026-07-09 a 16h doc-drift wedge wrongly blocked 2 programs'
// milestones, and on 2026-07-08 a rate-limit window blocked all 5 of
// a69ef9d1's. PrioritizeGoapGoals charges at queue time and stamps
// program_milestone_charged; the runtime action's deferred failure handler
// refunds that charge when the failure classifies as infrastructure.

// goapInfraResultMarkers identify infrastructure failures by the Result the
// failing step wrote. Verification failures and agent declines are genuine
// implementation outcomes and are deliberately NOT listed.
var goapInfraResultMarkers = []string{
	"applied_uncommitted",
	"pending_patch:",
	"Superpowers Pending Patch",
	"Superpowers Worktree Failed",
	// A Claude usage/credit-limit exhaustion is an external outage, not an
	// unbuildable milestone. The CLI exits non-zero with "reached your <model>
	// limit ... Run /usage-credits", so classify it as infrastructure: the
	// cycle then refunds the milestone attempt (and skips the research-goal
	// charge) instead of burning the abandon budget. A fleet-wide model quota
	// cap once wrongly blocked every seeded milestone ×3 for ~33h
	// (2026-07-10 14:02 → 2026-07-12): the seeder kept walking the repo while
	// the loop treadmilled, landing nothing.
	"reached your ",
	"/usage-credits",
}

// goapImplGateFailureMarkers identify a commit-gate rejection caused by the
// cycle's OWN staged code failing a deterministic quality check — the exact ✗
// lines the pre-commit hook (scripts/git-hooks/pre-commit) prints for gofmt, go
// vet, golangci-lint, go mod tidy, and the short test suite. Unlike an external
// wedge (stale materialized docs) or a resource kill (OOM "signal: killed",
// which prints no ✗ marker), these reproduce every cycle until the code is
// fixed, so they MUST charge the milestone-abandon budget even though the step
// reports the generic applied_uncommitted / pending_patch result. Without this
// precedence, program 94b0b31's milestone treadmilled ~15 cycles over 12h on
// 2026-07-12 (each cycle's generated code tripped a different linter —
// redefines-builtin `min`, unchecked errcheck, gocritic appendCombine) and
// refunded every time, landing nothing while the fleet's whole goap path stalled.
var goapImplGateFailureMarkers = []string{
	"golangci-lint found issues", // "✗ golangci-lint found issues …" (covers revive/errcheck/gocritic/etc.)
	"go vet found issues",        // "✗ go vet found issues. …"
	"need formatting",            // "✗ The following staged files need formatting:"
	"Tests failed. Fix before",   // "✗ Tests failed. Fix before committing."
	"go.mod/go.sum out of sync",  // "✗ go.mod/go.sum out of sync …"
}

// isGoapInfraCycleFailure reports whether a failed cycle died for
// infrastructure reasons rather than an implementation failure.
func isGoapInfraCycleFailure(outcome, result string) bool {
	// An own-code gate failure (lint/vet/fmt/test/mod-tidy) is a genuine
	// implementation failure that reproduces every cycle — checked FIRST so it
	// wins over the generic applied_uncommitted / pending_patch markers (both of
	// which also appear in the same commit-gate output) and charges the budget.
	for _, marker := range goapImplGateFailureMarkers {
		if strings.Contains(result, marker) {
			return false
		}
	}
	switch outcome {
	case "goap_fusion_rate_limited", "pending_patch":
		return true
	}
	for _, marker := range goapInfraResultMarkers {
		if strings.Contains(result, marker) {
			return true
		}
	}
	return false
}

// refundGoapMilestoneAttemptForInfraFailure refunds the milestone attempt
// PrioritizeGoapGoals charged this cycle. It prefers the explicit
// program_milestone_charged stamp (the queued refs re-read after the store
// re-open may start past a just-blocked milestone) and falls back to the head
// queued ref for pre-stamp blackboard state. Idempotent per cycle via the
// program_milestone_refunded flag so a doubled failure path can never refund
// twice. Reports whether a charge was refunded.
func refundGoapMilestoneAttemptForInfraFailure(bb *Blackboard) bool {
	if bb == nil || bb.ChainState == nil {
		return false
	}
	if done, _ := bb.ChainState["goap_fusion_program_milestone_refunded"].(string); done == "true" {
		return false
	}
	ref, _ := bb.ChainState["goap_fusion_program_milestone_charged"].(string)
	if strings.TrimSpace(ref) == "" {
		blob, _ := bb.ChainState["goap_fusion_program_milestone"].(string)
		ref = strings.SplitN(blob, ",", 2)[0]
	}
	parts := strings.SplitN(strings.TrimSpace(ref), ":", 2)
	if len(parts) != 2 {
		return false
	}
	idx, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return false
	}
	if !ps.RefundAttempt(parts[0], idx, goapProgramMaxMilestoneAttempts) {
		return false
	}
	if err := ps.Save(); err != nil {
		return false
	}
	setGoapState(bb, "program_milestone_refunded", "true")
	Info("goap fusion: refunded milestone attempt after infrastructure failure",
		"milestone", parts[0]+":"+parts[1])
	return true
}
