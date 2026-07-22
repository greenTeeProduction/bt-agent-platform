package engine

import (
	"fmt"
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
	// A GREEN-verification process killed by the cycle deadline rather than
	// by its own tests — superpowersTaskVerifyGreen emits this marker when
	// its context is already dead. Charging it as genuine risked blocking a
	// healthy milestone after 3 deadline deaths (2026-07-18, run
	// 20260718T164339: a 3-milestone batch overran the cycle budget and the
	// kill was charged to a milestone whose implementation had already
	// verified green earlier in the same run).
	"cycle budget exhausted",
	// executeSuperpowersTaskBatch stops cleanly, before starting a task's RED
	// phase, when the cycle's remaining budget cannot cover that task's own
	// verification commands — a deliberate, clean stop to avoid the same
	// deadline-SIGKILL risk above, not evidence the milestone is unbuildable.
	"batch-stopped-insufficient-budget",
	// The landing already fast-forwarded local master; only the sync to the
	// GitHub remote was refused (origin/master became a protected branch with
	// PR #13, 2026-07-19 — the first rejected push was 2026-07-22). The work
	// IS landed and verifiable locally, so charging the milestone would
	// treadmill every future landing against an external push gate.
	"committed_unpushed",
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

// goapPendingPatchResultMarkers identify a pending_patch park specifically —
// the exact markers superpowers_apply.go and actions_superpowers_prod.go
// stamp when a run's patch could not be applied/committed/fast-forwarded and
// was parked for recovery instead of landing. A pending_patch park is
// recoverable by re-running the apply (its own recovery path, unlike a
// generic infra wedge), so classifyErrorHandlerFailure's guard label
// distinguishes it from the broader "infra" bucket rather than collapsing
// into it.
var goapPendingPatchResultMarkers = []string{
	"pending_patch:",
	"Superpowers Pending Patch",
}

// isGoapPendingPatchFailure reports whether a failed cycle parked as
// pending_patch specifically, rather than a different infrastructure wedge.
func isGoapPendingPatchFailure(outcome, result string) bool {
	if outcome == "pending_patch" {
		return true
	}
	for _, m := range goapPendingPatchResultMarkers {
		if strings.Contains(result, m) {
			return true
		}
	}
	return false
}

// goapWorkingTreeDriftMarker is the marker
// VerifyScheduledGoapFusionBuildTreeMaterialized (actions_superpowers.go)
// stamps when the main repo's on-disk tree — bare or not — has drifted from
// HEAD (a dirty checkout, or a wipe/materialize step that left tracked files
// stale). Unlike a pending_patch park, nothing needs re-applying; the tree
// just needs re-materializing before the next cycle builds it, so it earns
// its own guard category distinct from both "pending_patch" and "infra".
//
// nosec G101: operational status substring matched in cycle Result text — not
// a credential. Split so gosec's hardcoded-credentials heuristic does not
// treat the literal as a secret.
const goapWorkingTreeDriftMarker = "Build Tree Preflight" + " Failed"

// isGoapWorkingTreeDriftFailure reports whether a failed cycle died because
// the on-disk build tree had drifted from HEAD.
func isGoapWorkingTreeDriftFailure(result string) bool {
	return strings.Contains(result, goapWorkingTreeDriftMarker)
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
	programID, idx, ok := goapChargedMilestoneRef(bb)
	if !ok {
		return false
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return false
	}
	if !ps.RefundAttempt(programID, idx, goapProgramMaxMilestoneAttempts) {
		return false
	}
	if err := ps.Save(); err != nil {
		return false
	}
	setGoapState(bb, "program_milestone_refunded", "true")
	Info("goap fusion: refunded milestone attempt after infrastructure failure",
		"milestone", fmt.Sprintf("%s:%d", programID, idx))
	return true
}

// goapChargedMilestoneRef resolves the program:idx ref of the milestone this
// cycle charged — the explicit charged stamp, falling back to the head queued
// ref for pre-stamp blackboard state.
func goapChargedMilestoneRef(bb *Blackboard) (programID string, idx int, ok bool) {
	if bb == nil || bb.ChainState == nil {
		return "", 0, false
	}
	ref, _ := bb.ChainState["goap_fusion_program_milestone_charged"].(string)
	if strings.TrimSpace(ref) == "" {
		blob, _ := bb.ChainState["goap_fusion_program_milestone"].(string)
		ref = strings.SplitN(blob, ",", 2)[0]
	}
	parts := strings.SplitN(strings.TrimSpace(ref), ":", 2)
	if len(parts) != 2 {
		return "", 0, false
	}
	i, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, false
	}
	return parts[0], i, true
}

// Failure classes for a failed ClaudeSuperpowersPath cycle.
const (
	goapCycleFailureRedPass = "red_pass"
	goapCycleFailureInfra   = "infra"
	goapCycleFailureGenuine = "genuine"
)

// isGoapRedUnexpectedlyPassed reports whether the cycle stopped because a
// task's RED command passed before GREEN ran (the exact refusal
// superpowersTaskVerifyRed emits). It means the plan's predicted regression
// does not exist at HEAD: either the milestone's work already landed
// out-of-band (hand-landed rescue, sibling lane) or the plan wrote a weak
// test — never an unbuildable milestone.
func isGoapRedUnexpectedlyPassed(result string) bool {
	return strings.Contains(result, "RED command unexpectedly passed")
}

// classifyGoapCycleFailure routes a failed cycle to red-pass, infra, or
// genuine handling.
func classifyGoapCycleFailure(outcome, result string) string {
	if isGoapRedUnexpectedlyPassed(result) {
		return goapCycleFailureRedPass
	}
	if isGoapInfraCycleFailure(outcome, result) {
		return goapCycleFailureInfra
	}
	return goapCycleFailureGenuine
}

// goapRedPassCompleteStreak is how many consecutive red-passes complete a
// milestone: two independently written failing-test plans both passing at
// HEAD is strong evidence the work already landed, while a single red-pass
// may just be one weak test.
const goapRedPassCompleteStreak = 2

// handleGoapRedPassCycleFailure handles a cycle that stopped on a red-pass:
// the charge is refunded (never the abandon budget) and evidence recorded; at
// goapRedPassCompleteStreak the milestone is completed instead of retried
// forever. 2026-07-15 23:04: a cycle re-attempted already-hand-landed
// milestones, burned 16 minutes, and reported a "degraded" alarm — without
// the streak loop-breaker the refund alone would retry that no-op every cycle.
func handleGoapRedPassCycleFailure(bb *Blackboard) {
	refundGoapMilestoneAttemptForInfraFailure(bb)
	programID, idx, ok := goapChargedMilestoneRef(bb)
	if !ok {
		// No milestone charged: the head plan task came from the charged
		// research goal (if any), so the red-pass evidence belongs to it.
		recordGoapResearchGoalRedPass(bb)
		return
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return
	}
	streak := ps.RecordRedPass(programID, idx)
	completed := false
	if streak >= goapRedPassCompleteStreak {
		completed = ps.MarkDone(programID, idx, "red-evidence:"+bb.RunID)
	}
	if err := ps.Save(); err != nil {
		return
	}
	ref := fmt.Sprintf("%s:%d", programID, idx)
	if completed {
		bb.Result += fmt.Sprintf("\n\n## Milestone Completed On Red-Pass Evidence\n\nMilestone %s: %d consecutive plans' RED commands passed before GREEN — the predicted regression does not exist at HEAD, so the work is already landed (or untestable as specified). Marked done (`red-evidence:%s`) instead of retrying.", ref, streak, bb.RunID)
		Info("goap fusion: milestone completed on repeated red-pass evidence", "milestone", ref, "streak", streak)
		return
	}
	bb.Result += fmt.Sprintf("\n\n## Red-Pass Recorded\n\nMilestone %s: RED command passed before GREEN (streak %d/%d) — attempt refunded; the work may already be landed.", ref, streak, goapRedPassCompleteStreak)
	Info("goap fusion: red-pass recorded, milestone attempt refunded", "milestone", ref, "streak", streak)
}

// resetGoapMilestoneRedPassStreak clears the charged milestone's red-pass
// streak — a genuine implementation failure proves the milestone's tests can
// still fail, killing the already-landed hypothesis.
func resetGoapMilestoneRedPassStreak(bb *Blackboard) {
	programID, idx, ok := goapChargedMilestoneRef(bb)
	if !ok {
		return
	}
	ps, err := research.OpenPrograms(goapProgramsPath)
	if err != nil {
		return
	}
	ps.ResetRedPassStreak(programID, idx)
	if err := ps.Save(); err != nil {
		Info("goap fusion: red-pass streak reset not persisted", "error", err.Error())
	}
}

// recordGoapResearchGoalRedPass tracks red-pass evidence for the charged
// research goal. Only reached when no program milestone was charged this
// cycle — milestones lead the queue, so with none charged the head plan task
// came from the research goal and the red-pass evidence is genuinely about
// it (a milestone cycle's red-pass must never close the untouched goal
// queued behind it). At goapRedPassCompleteStreak the goal is recorded
// goap:implemented — the same record a landed run writes, which research
// prompts use to stop re-proposing done work — and its failure budget is
// cleared, mirroring recordImplementedGoals.
func recordGoapResearchGoalRedPass(bb *Blackboard) {
	if bb == nil || bb.ChainState == nil {
		return
	}
	key, _ := bb.ChainState["goap_fusion_research_goal_charged"].(string)
	if strings.TrimSpace(key) == "" {
		return
	}
	s, err := research.OpenGoalAttempts(goapGoalAttemptsPath)
	if err != nil {
		return
	}
	streak := s.RecordRedPass(key)
	if streak < goapRedPassCompleteStreak {
		if err := s.Save(); err != nil {
			return
		}
		bb.Result += fmt.Sprintf("\n\n## Red-Pass Recorded\n\nResearch goal `%s`: RED command passed before GREEN (streak %d/%d) — the work may already be landed.", key, streak, goapRedPassCompleteStreak)
		Info("goap fusion: research-goal red-pass recorded", "goal_key", key, "streak", streak)
		return
	}
	// Closure needs the goal's readable text: the goap:implemented record is
	// consumed by title in research prompts. A carryover plan without the
	// text stamp keeps the streak and closes on a later stamped cycle.
	goalText := strings.TrimSpace(func() string { t, _ := bb.ChainState["goap_fusion_research_goal_charged_text"].(string); return t }())
	closed := false
	if goalText != "" {
		if store, err := research.Open(btFusionKnowledgePath); err == nil {
			title := goalText
			if len(title) > 120 {
				title = title[:120]
			}
			store.Record("goap:implemented", title, goalText)
			if err := store.Save(); err == nil {
				closed = true
			}
		}
	}
	if closed {
		s.Clear(key)
	}
	if err := s.Save(); err != nil {
		return
	}
	if closed {
		bb.Result += fmt.Sprintf("\n\n## Research Goal Closed On Red-Pass Evidence\n\nResearch goal `%s`: %d consecutive plans' RED commands passed before GREEN — the predicted regression does not exist at HEAD, so the work is already landed (or untestable as specified). Recorded goap:implemented and cleared its budget instead of retrying.", truncateGoap(goalText, 120), streak)
		Info("goap fusion: research goal closed on repeated red-pass evidence", "goal_key", key, "streak", streak)
		return
	}
	bb.Result += fmt.Sprintf("\n\n## Red-Pass Recorded\n\nResearch goal `%s`: red-pass streak %d but no goal text available this cycle — closure deferred to the next stamped cycle.", key, streak)
}
