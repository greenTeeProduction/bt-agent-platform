package engine

import (
	"context"
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
	result = goapFailureDiagnostic(result)
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
	return strings.Contains(goapFailureDiagnostic(result), goapWorkingTreeDriftMarker)
}

// goapFailureDiagnostic excludes subprocess output from verification failures.
// Engine tests log simulated RED passes, quota errors, and pending patches;
// those fixtures describe neither the outer command nor this cycle's outcome.
// Keep the executor's diagnostic (including its deadline marker), while the
// original Result and command artifacts retain all output for investigation.
func goapFailureDiagnostic(result string) string {
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "task GREEN verification ") ||
			strings.HasPrefix(line, "task RED verification ") ||
			strings.HasPrefix(line, "verification ") {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	return result
}

// isGoapInfraCycleFailure reports whether a failed cycle died for
// infrastructure reasons rather than an implementation failure.
func isGoapInfraCycleFailure(outcome, result string) bool {
	result = goapFailureDiagnostic(result)
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
	var refunded bool
	err := research.UpdatePrograms(currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
		refunded = ps.RefundAttempt(programID, idx, goapProgramMaxMilestoneAttempts)
		// This cycle is giving up on the milestone — release its program claim
		// (if it still holds one) so a sibling cycle need not wait out the full
		// lease window before it can plan the same program.
		ps.ReleaseClaim(programID, bb.RunID)
		return nil
	})
	if err != nil || !refunded {
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
	return strings.Contains(goapFailureDiagnostic(result), "RED command unexpectedly passed")
}

// extractRedPassCommand pulls the RED command out of the executor's refusal
// line ("RED command unexpectedly passed; refusing to run GREEN without
// failing regression evidence: <cmd>") — the command is the rest of that
// line. Empty when the marker is absent or malformed; RecordRedPass treats
// an empty command as "keep the previous record".
func extractRedPassCommand(result string) string {
	result = goapFailureDiagnostic(result)
	const marker = "failing regression evidence: "
	_, after, ok := strings.Cut(result, marker)
	if !ok {
		return ""
	}
	rest := after
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
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

// Two consecutive preimplementation passes require review of the proposed
// regression. They cannot establish that an implementation was delivered.
const goapRedPassReviewStreak = 2

// handleGoapRedPassCycleFailure retains the test evidence and stops repeated
// unsuitable plans through a review hold, without reporting completed work.
func handleGoapRedPassCycleFailure(bb *Blackboard) {
	programID, idx, ok := goapChargedMilestoneRef(bb)
	if !ok {
		// No milestone charged: the head plan task came from the charged
		// research goal (if any), so the red-pass evidence belongs to it.
		refundGoapMilestoneAttemptForInfraFailure(bb)
		recordGoapResearchGoalRedPass(bb)
		return
	}
	ref := fmt.Sprintf("%s:%d", programID, idx)

	// A missing named deliverable independently establishes unfinished work.
	// A whole-package RED command may pass because the requested _test.go
	// was never written. Completing on that is an inverted inference:
	// an audit found 41 of 47 checkable red-evidence completions named a
	// _test.go that does not exist and, per git log, never did.
	//
	// The probe shells out, so it runs OUTSIDE the program-store flock.
	verdict := goapRedPassDeliverableVerdict(goapMilestoneGoalText(programID, idx))
	if verdict == goapDeliverablesMissing {
		// Certain the deliverable is absent. Do NOT refund: a milestone whose
		// RED command cannot discriminate must consume its attempt budget and
		// block visibly, rather than refunding forever — the refund is exactly
		// what let one milestone treadmill 82 cycles / ~40h with zero output.
		// Charging the attempt is the loop-breaker, but this cycle is still
		// giving up on the milestone — release the program claim so a sibling
		// cycle need not wait out the full lease before planning the program's
		// OTHER milestones. Refund is the only other caller of ReleaseClaim on
		// this path, and we deliberately skip it, so release explicitly.
		_ = research.UpdatePrograms(currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
			ps.ReleaseClaim(programID, bb.RunID)
			return nil
		})
		bb.Result += fmt.Sprintf("\n\n## Red-Pass Rejected — Deliverable Missing\n\nMilestone %s: the RED command passed, but the deliverable the goal names is absent at HEAD, so the pass is evidence the work was NOT done — not that it already landed. Attempt charged; the milestone blocks after its budget instead of retrying forever. It needs a RED command that fails until its deliverable exists.", ref)
		Info("goap fusion: red-pass rejected, named deliverable missing at HEAD", "milestone", ref)
		return
	}

	refundGoapMilestoneAttemptForInfraFailure(bb)
	var streak int
	var review bool
	if err := research.UpdatePrograms(currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
		streak = ps.RecordRedPass(programID, idx, extractRedPassCommand(bb.Result))
		if streak >= goapRedPassReviewStreak {
			review = ps.MarkNeedsReview(programID, idx, bb.RunID, "Repeated RED commands passed before implementation; revise the goal/test or inspect an actual delivery before continuing dependent milestones.")
		}
		return nil
	}); err != nil {
		return
	}
	if !review && verdict == goapDeliverablesUnknown {
		bb.Result += fmt.Sprintf("\n\n## Red-Pass Undetermined\n\nMilestone %s: RED passed (streak %d/%d) but the deliverable probe could not reach HEAD, so completion is withheld this cycle. Attempt refunded; evidence recorded.", ref, streak, goapRedPassReviewStreak)
		Info("goap fusion: red-pass recorded, completion withheld — deliverable probe unavailable", "milestone", ref, "streak", streak)
		return
	}
	if review {
		bb.Result += fmt.Sprintf("\n\n## Milestone Needs Review\n\nMilestone %s: %d RED commands passed before implementation. No code delivery is established. Automatic retries and dependent milestones are held for goal/test review.", ref, streak)
		Info("goap fusion: milestone needs review after repeated red-pass", "milestone", ref, "streak", streak)
		return
	}
	bb.Result += fmt.Sprintf("\n\n## Red-Pass Recorded\n\nMilestone %s: RED passed before implementation (streak %d/%d). Attempt refunded; no code delivery established.", ref, streak, goapRedPassReviewStreak)
	Info("goap fusion: red-pass recorded, milestone attempt refunded", "milestone", ref, "streak", streak)
}

// resetGoapMilestoneRedPassStreak clears the charged milestone's red-pass
// streak — a genuine implementation failure proves the milestone's tests can
// still fail, killing the already-landed hypothesis. It also releases this
// cycle's program claim (if any): a milestone circling the abandon budget
// must not ALSO wedge a sibling agent out of the whole program for up to the
// full lease window.
func resetGoapMilestoneRedPassStreak(bb *Blackboard) {
	programID, idx, ok := goapChargedMilestoneRef(bb)
	if !ok {
		return
	}
	if err := research.UpdatePrograms(currentGoapProgramsPath(), func(ps *research.ProgramStore) error {
		ps.ResetRedPassStreak(programID, idx)
		ps.ReleaseClaim(programID, bb.RunID)
		return nil
	}); err != nil {
		Info("goap fusion: red-pass streak reset not persisted", "error", err.Error())
	}
}

// recordGoapResearchGoalRedPass stops repeated unsuitable RED plans without
// claiming they implemented a change. The goal remains visible for review.
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
		bb.Result += "\n\nCould not read research red-pass evidence: " + err.Error()
		return
	}
	streak := s.RecordRedPass(key)
	if err := s.Save(); err != nil {
		bb.Result += "\n\nCould not persist research red-pass evidence: " + err.Error()
		return
	}
	goal, _ := bb.ChainState["goap_fusion_research_goal_charged_text"].(string)
	if streak < goapRedPassReviewStreak || strings.TrimSpace(goal) == "" {
		bb.Result += fmt.Sprintf("\n\nRed-pass evidence for goal `%s`: streak %d/%d. No code delivery established.", key, streak, goapRedPassReviewStreak)
		return
	}
	err = research.UpdateTraces(context.Background(), researchTracePath(bb.User), bb.User, func(traces *research.TraceStore) error {
		traces.Goal(researchGoalTraceID(goal), goal).ReviewReason = "Repeated RED commands passed before implementation; the proposed test does not establish the claimed gap. Revise the goal/test before retrying."
		return nil
	})
	if err != nil {
		bb.Result += "\n\nCould not persist research review requirement: " + err.Error()
		return
	}
	bb.Result += fmt.Sprintf("\n\nResearch goal `%s` needs review after %d consecutive red-pass results. Automatic retries are withheld; no delivery or impact credit was awarded.", truncateGoap(goal, 120), streak)
}
