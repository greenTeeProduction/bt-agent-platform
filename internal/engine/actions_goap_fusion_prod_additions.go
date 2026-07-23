package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	btcore "github.com/rvitorper/go-bt/core"
)

// goapFusionBareDelegationMarker is VerifyGoapBuild's internal note when it
// hands build/test verification to the apply-stage worktree on the bare main
// repo. It is an implementation detail of VerifyGoapBuild's wording.
//
// goapFusionNormalizedDelegationLine is the explicit, self-describing token
// ReportFusionCycle appends to the cycle report whenever the verify_result
// carries the bare marker. GOAL1's evidence gate keys on THIS normalized token,
// not on VerifyGoapBuild's raw wording — so a future reword of one does not
// silently re-break the other.
const (
	goapFusionBareDelegationMarker     = "delegated to apply-stage worktree verification"
	goapFusionNormalizedDelegationLine = "Build/tests: DELEGATED (bare main repo, verified in apply worktree)"
)

func init() {
	registerGoapFusionProductionAdditions()
}

// GoapEvidenceShapeRejection is the evidence-gate reason emitted when a
// Superpowers completion report lacks the run/artifact/apply-status/commit
// evidence the validator's recognition list accepts. The reporter and
// validator ship in the same binary, so the rejection is deterministic per
// binary — retrying the cycle reproduces it exactly (2026-07-22/23: two
// healthy landings were blind-retried into false DLQ entry #239 this way).
// cmd/bt-agent's scheduler classifies attempts carrying this marker as
// non-retryable via this shared const.
const GoapEvidenceShapeRejection = "Superpowers completion missing run/artifact/committed/commit evidence"

func registerGoapFusionProductionAdditions() {
	RegisterAction("RunGraphifyUpdate", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bin, err := resolveGraphifyBin()
		if err != nil {
			setGoapState(bb, "graphify_update_result", "FAILED:\n"+err.Error())
			bb.Result = fmt.Sprintf("## Graphify Update Failed\n\n%v", err)
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		res := runShellCommand(c, defaultSuperpowersCommandRunner, goapFusionRepo, bin+" update .")
		if res.Err != nil {
			setGoapState(bb, "graphify_update_result", "FAILED:\n"+truncateGoap(res.Output, 2000))
			bb.Result = fmt.Sprintf("## Graphify Update Failed\n\n%s", truncateGoap(res.Output, 2000))
			return -1
		}
		setGoapState(bb, "graphify_update_result", "graphify update .: PASSED")
		bb.Result = "## Graphify Updated\n\n" + truncateGoap(res.Output, 2000)
		return 1
	})

	RegisterAction("WriteSuperpowersImplementationPlan", func(ctx *btcore.BTContext[Blackboard]) (result int) {
		bb := ctx.Blackboard
		// This action is the head of ClaudeSuperpowersPath. Any failure here
		// (plan saturation, dir/file IO) must degrade the cycle to the
		// deterministic ScheduledAnalysisPath rather than abort the loop, so
		// stamp the durable impl-degraded signal on every failure exit.
		defer func() {
			if result == -1 {
				markGoapFusionImplDegraded(bb, bb.Result)
			}
		}()
		// Reuse a plan carried over from an earlier (e.g. rate-limited) cycle.
		// Read durably so a fresh cron tick whose ChainState is empty still finds
		// the saved plan in the agent-scope store instead of re-planning.
		if existing, _ := loadSuperpowersPlanState(bb); existing != "" {
			bb.Result = fmt.Sprintf("## GOAP Superpowers Plan Reused\n\nPath: `%s`", existing)
			return 1
		}
		goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
		gaps, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
		if goals == "" {
			goals = "Implement the highest-priority GOAP fusion improvement safely."
		}
		// Deterministically scope goal lines that name no Go files (git grep
		// on the goal's keywords): a pathless goal can never become a plan
		// task, and the biggest catalog/research goals are exactly the ones
		// that tend to arrive pathless.
		var scopedLines []string
		for _, line := range strings.Split(goals, "\n") {
			scopedLines = append(scopedLines, scopeGoapGoalLine(line))
		}
		// The saturation check hashes the grep-scoped but NOT graphify-enriched
		// task text: the reuse suffix carries volatile graph loc=L<n>
		// coordinates, so hashing it would hand the same stuck goal a fresh
		// task hash after every graphify rebuild — defeating the two-strikes
		// refusal below. Checking BEFORE enrichment also keeps refused cycles
		// from paying the per-goal-line graphify exec cost.
		task := fmt.Sprintf("%s\n\nGOAP goals:\n%s\n\nGaps:\n%s", bb.Task, strings.Join(scopedLines, "\n"), gaps)
		// Two failed attempts at the same task hash are signal enough — the
		// old default of 12 burned half a day of cycles on a stuck goal.
		maxAttempts := 3
		if raw := os.Getenv("BT_SUPERPOWERS_MAX_PLAN_REPEATS"); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				maxAttempts = parsed
			}
		}
		if saturated, matches := superpowersPlanAttemptSaturated(task, maxAttempts); saturated {
			setGoapState(bb, "superpowers_plan_saturated", strings.Join(matches, "\n"))
			bb.Result = fmt.Sprintf("## GOAP Superpowers Plan Saturated\n\nTask hash `%s` already has %d attempt(s). Refusing to create another isolated worktree loop. Existing attempts:\n```\n%s\n```", superpowersTaskHashSuffix(task), len(matches), strings.Join(matches, "\n"))
			bb.Outcome = "goap_fusion_plan_saturated"
			return -1
		}
		// After the lexical grep scoping, append knowledge-graph reuse hits
		// (graphifyScopeGoalLine, arc42 Q5) pointing each goal at existing
		// canonical components. Both suffixes are TRANSIENT and the reuse hits
		// ADVISORY: they exist only in this composed plan/task text — the
		// ChainState goal queue, everything goapResearchGoalKey hashes, the
		// saturation hash above, and the durable goap:implemented store (which
		// records stripped objectives) stay untouched. A wedged graphify costs
		// at most one query timeout here: the first failure latches a cooldown
		// and the remaining lines fail fast (see graphify_components.go).
		for i, line := range scopedLines {
			scopedLines[i] = graphifyScopeGoalLine(line)
		}
		goals = strings.Join(scopedLines, "\n")
		task = fmt.Sprintf("%s\n\nGOAP goals:\n%s\n\nGaps:\n%s", bb.Task, goals, gaps)
		plan := buildGoalDrivenImplementationPlan(task)
		dir := filepath.Join(goapFusionRepo, "docs", "superpowers", "plans")
		path := filepath.Join(dir, fmt.Sprintf("goap-fusion-%s-%s.md", time.Now().Format("20060102T150405"), safeSlug(goals)))
		if len(path) > 220 {
			path = filepath.Join(dir, fmt.Sprintf("goap-fusion-%s.md", time.Now().Format("20060102T150405")))
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			bb.Result = err.Error()
			return -1
		}
		if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
			bb.Result = err.Error()
			return -1
		}
		// Persist durably (agent-scope + ChainState) so a later rate-limited
		// cycle with a fresh ChainState can resume this exact plan.
		saveSuperpowersPlanState(bb, path, plan)
		bb.Plan = plan
		bb.Result = fmt.Sprintf("## GOAP Superpowers Plan Written\n\nPath: `%s`\n\n### Approval summary\n- Task: %s\n- Top GOAP goals:\n%s\n- Gap analysis:\n%s\n\n### Plan excerpt\n```markdown\n%s\n```", path, bb.Task, truncateGoap(goals, 1200), truncateGoap(gaps, 1200), truncateGoap(plan, 3500))
		return 1
	})

	RegisterAction("WriteFusionAnalysis", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
		gaps, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
		ts := time.Now().Format("2006-01-02T150405")
		path := filepath.Join(goapFusionVaultDir, fmt.Sprintf("goap-fusion-analysis-%s.md", ts))
		latest := filepath.Join(goapFusionVaultDir, "goap-fusion-latest.md")

		// Fast path: goals unchanged — write minimal note with goals for future comparison
		if v, _ := bb.ChainState["goap_fusion_goals_unchanged"].(string); v == "true" {
			report := fmt.Sprintf("# GOAP Fusion — %s\n\n**No new gaps.** Goals unchanged from previous run.\n\n## Goals\n%s\n", ts, goals)
			// goals_unchanged is also the impl-degraded fallback flag, so this
			// path regularly ends cycles that seeded a program or degraded —
			// both outcomes must survive into the note (the 11:34 verification
			// cycle showed neither).
			report += goapFusionImplDegradedSection(bb)
			report += goapFusionSeedSection(bb)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				bb.Result = err.Error()
				return -1
			}
			_ = os.WriteFile(path, []byte(report), 0o644)
			_ = os.WriteFile(latest, []byte(report), 0o644)
			setGoapState(bb, "fusion_analysis_path", path)
			bb.Result = fmt.Sprintf("## No New Gaps\n\nGoals unchanged. Skipping analysis boilerplate.\nAnalysis: `%s`", path)
			return 1
		}

		report := fmt.Sprintf("# GOAP Fusion Analysis — %s\n\n## Task\n%s\n\n## Goals\n%s\n\n## Gaps\n%s\n", ts, bb.Task, goals, gaps)
		report += goapFusionImplDegradedSection(bb)
		report += goapFusionSeedSection(bb)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			bb.Result = err.Error()
			return -1
		}
		if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
			bb.Result = err.Error()
			return -1
		}
		_ = os.WriteFile(latest, []byte(report), 0o644)
		setGoapState(bb, "fusion_analysis_path", path)
		bb.Result = fmt.Sprintf("## Analysis Written\n\nSaved to: `%s`", path)
		return 1
	})

	RegisterAction("VerifyGoapFusionEvidence", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		out := strings.TrimSpace(bb.Result)
		lower := strings.ToLower(out)
		fail := func(reason string) int {
			bb.Outcome = "failure"
			bb.Result = fmt.Sprintf("## GOAP Fusion Evidence Failed\n\n%s\n\nPrevious output:\n```\n%s\n```", reason, truncateGoap(out, 2000))
			return -1
		}
		if out == "" {
			return fail("empty output; no GOAP fusion artifact evidence")
		}
		fabricationMarkers := []string{
			"self-corrected output",
			"simulated",
			"fabricated",
			"pretend",
			"claude code commands executed",
			"src/goap_runner.py",
			"fusion.py",
			"verify.sh",
		}
		for _, marker := range fabricationMarkers {
			if strings.Contains(lower, marker) {
				return fail("fabrication marker found: " + marker)
			}
		}

		if strings.Contains(out, "## Superpowers Implementation Complete") {
			applyStatusEvidence := strings.Contains(out, "Apply status: `committed`") || strings.Contains(out, "Apply status: `committed_pr_opened`")
			if !strings.Contains(out, "Run: `") || !strings.Contains(out, "Artifacts: `") || !applyStatusEvidence || !strings.Contains(out, "Commit: `") {
				return fail(GoapEvidenceShapeRejection)
			}
			artifactPath := goapBacktickValueAfter(out, "Artifacts: `")
			if artifactPath == "" {
				return fail("Superpowers completion missing parseable artifact path")
			}
			if info, err := os.Stat(artifactPath); err != nil || !info.IsDir() {
				return fail(fmt.Sprintf("Superpowers artifact directory `%s` is not present: %v", artifactPath, err))
			}
			if _, err := os.Stat(filepath.Join(artifactPath, "run.json")); err != nil {
				return fail(fmt.Sprintf("Superpowers artifact `%s` missing run.json: %v", artifactPath, err))
			}
			if _, err := os.Stat(filepath.Join(artifactPath, "finish.md")); err != nil {
				return fail(fmt.Sprintf("Superpowers artifact `%s` missing finish.md: %v", artifactPath, err))
			}
			// A committed implementation is the only run that actually landed
			// code — score it authoritatively high and leave the outcome as the
			// tree's "success". (Honest-signal change, 2026-07-13.)
			bb.QualityScore = 0.9
			bb.QualityAuthoritative = true
			return 1
		}

		if strings.Contains(out, "## GOAP Fusion Cycle Complete") {
			analysisPath := goapBacktickValueAfter(out, "Analysis: `")
			if analysisPath == "" {
				return fail("deterministic analysis output missing parseable Analysis path")
			}
			if _, err := os.Stat(analysisPath); err != nil {
				return fail(fmt.Sprintf("analysis artifact `%s` is not present: %v", analysisPath, err))
			}
			requiredEvidence := []string{
				"Verification:",
				"graphify update .: PASSED",
			}
			for _, evidence := range requiredEvidence {
				if !strings.Contains(out, evidence) {
					return fail("deterministic analysis output missing evidence: " + evidence)
				}
			}
			// Build/test evidence is either/or: the two focused PASSED strings, or
			// the bare-repo delegation marker VerifyGoapBuild emits when it hands
			// build/test verification to the apply-stage worktree. Requiring the
			// PASSED strings unconditionally dead-letters every ScheduledAnalysisPath
			// cycle on the bare main repo.
			buildTestPassed := strings.Contains(out, "go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED") &&
				strings.Contains(out, "focused go tests: PASSED")
			// Accept EITHER VerifyGoapBuild's raw bare-repo note OR the normalized
			// delegation token ReportFusionCycle appends. Keying on the normalized
			// token decouples the gate from VerifyGoapBuild's exact wording: a reword
			// of the internal note still passes as long as the normalized line is
			// present.
			delegated := strings.Contains(out, goapFusionBareDelegationMarker) ||
				strings.Contains(out, goapFusionNormalizedDelegationLine)
			if !buildTestPassed && !delegated {
				return fail("deterministic analysis output missing build/test evidence: expected both PASSED strings or the apply-stage delegation marker")
			}
			// An analysis-only cycle landed no code. Distinguish a healthy
			// no-code run ("no_change") from a Claude-path failure that fell back
			// to deterministic analysis ("degraded") so the recorded outcome and
			// quality stop overstating a full success. (Honest-signal, 2026-07-13.)
			// A red-pass stop wears the degraded wrapper but means the predicted
			// regression does not exist at HEAD — the work already landed
			// out-of-band. That is a healthy no-op, not a degradation
			// (2026-07-15 23:04: the re-attempt of hand-landed milestones
			// alarmed as "degraded").
			if strings.Contains(lower, "red command unexpectedly passed") {
				bb.OutcomeRefinement = "no_change"
				bb.QualityScore = 0.5
			} else if strings.Contains(lower, "implementation degraded (fallback)") ||
				strings.Contains(lower, "degraded to deterministic") {
				bb.OutcomeRefinement = "degraded"
				bb.QualityScore = 0.3
			} else {
				bb.OutcomeRefinement = "no_change"
				bb.QualityScore = 0.5
			}
			bb.QualityAuthoritative = true
			return 1
		}

		return fail("unrecognized GOAP fusion output shape; expected deterministic analysis or committed Superpowers artifact report")
	})

	RegisterAction("ReportFusionCycle", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		path, _ := bb.ChainState["goap_fusion_fusion_analysis_path"].(string)
		verify, _ := bb.ChainState["goap_fusion_verify_result"].(string)
		graphify, _ := bb.ChainState["goap_fusion_graphify_update_result"].(string)
		bb.Result = fmt.Sprintf("## GOAP Fusion Cycle Complete\n\nAnalysis: `%s`\n\nVerification:\n```\n%s\n%s\n```", path, verify, graphify)
		// The final report is the only artifact that reaches runs/latest/output
		// and the dashboard — carry the cycle's seeding and impl-degraded
		// outcomes instead of leaving them stranded in ChainState.
		bb.Result += goapFusionImplDegradedSection(bb)
		bb.Result += goapFusionSeedSection(bb)
		// When VerifyGoapBuild delegated build/test verification to the apply-stage
		// worktree (bare main repo), its raw note is a pass-through that only the
		// gate's exact-string match understands. Append an explicit, self-describing
		// delegation line so the report carries a stable, gate-parseable token that
		// survives a reword of VerifyGoapBuild's internal note.
		if strings.Contains(verify, goapFusionBareDelegationMarker) {
			bb.Result += "\n\n" + goapFusionNormalizedDelegationLine
		}
		// Surface silent-wipe evidence and the parked-run triage backlog in
		// every cycle report — the only artifact that reaches runs/latest/
		// output and the dashboard. Both are best-effort: neither can fail the
		// cycle, since a snapshot dir or git listing being briefly unreadable
		// must not dead-letter an otherwise genuinely verified cycle.
		bb.Result += goapFusionMaterializerSnapshotsSection(bb)
		bb.Result += goapFusionParkedBranchesSection()
		return 1
	})

	RegisterAction("ReportSuperpowersImplementation", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Superpowers Implementation Report\n\nNo Superpowers run state found."
			return 1
		}
		changed := strings.Join(run.ChangedFiles, "\n")
		if changed == "" {
			changed = "none recorded"
		}
		status := run.ApplyStatus
		if status == "" {
			status = "not_applied"
		}
		heading := "## Superpowers Implementation Pending Patch"
		if status == "committed" || status == "applied" || status == "applied_no_commit" || status == "main_repo" || status == "dry_run" || status == "committed_pr_opened" {
			heading = "## Superpowers Implementation Complete"
		}
		bb.Result = fmt.Sprintf("%s\n\nRun: `%s`\nArtifacts: `%s`\nApply status: `%s`\nPatch: `%s`\nCommit: `%s`\nChanged files:\n```\n%s\n```", heading, run.ID, run.ArtifactDir, status, run.PatchPath, run.AppliedCommit, changed)
		// This is the last full rewrite of bb.Result in the cycle: re-append
		// the PROGRAM-CONTINUE marker or the scheduler never sees it and the
		// fast requeue stays dormant (observed on run 20260704T110402 — the
		// marker was appended upstream and erased here).
		bb.Result += programContinueNote()
		return 1
	})

	RegisterAction("CleanupGraphifyOut", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if out, err := runGoapShell("git checkout -- graphify-out/"); err != nil {
			setGoapState(bb, "graphify_cleanup_warning", fmt.Sprintf("git checkout -- graphify-out/ failed: %s", truncateGoap(out, 200)))
		}
		// graph.json, manifest.json, and cache/ are untracked+gitignored (Q5
		// milestone 1/5: git rm --cached'd out of the heavy graphify artifacts),
		// so `git checkout --` above — which only restores TRACKED paths — is a
		// silent no-op against the regenerated copies a cycle's `graphify update .`
		// leaves behind. `git clean -x` also removes gitignored files; still
		// scoped to graphify-out/ so GRAPH_REPORT.md and the other still-tracked
		// files there are untouched (clean never removes tracked paths).
		if out, err := runGoapShell("git clean -fdx -- graphify-out/"); err != nil {
			setGoapState(bb, "graphify_cleanup_warning", fmt.Sprintf("git clean -fdx -- graphify-out/ failed: %s", truncateGoap(out, 200)))
		}
		return 1
	})
}

// goapFusionImplDegradedSection builds the vault-note section describing an
// impl-degraded cycle. Every -1 exit from ClaudeSuperpowersPath stamps
// goap_fusion_impl_degraded="true" via the shared defer, INCLUDING an expected
// Claude rate-limit carryover (bb.Outcome == "goap_fusion_rate_limited"). That
// signal must stay set so the ExecutionRouter still lets ScheduledAnalysisPath
// catch the cycle, but a healthy carryover must NOT be narrated as a real
// ClaudeSuperpowersPath failure — that phantom-failure line pollutes the next
// run's research corpus. So: report genuine degradations verbatim, but tag a
// rate-limit carryover distinctly as the expected pause it is.
func goapFusionImplDegradedSection(bb *Blackboard) string {
	if v, _ := bb.ChainState["goap_fusion_impl_degraded"].(string); v != "true" {
		return ""
	}
	reason, _ := bb.ChainState["goap_fusion_impl_degraded_reason"].(string)
	if strings.TrimSpace(reason) == "" {
		reason = "implementation path degraded; cause not recorded"
	}
	if bb.Outcome == "goap_fusion_rate_limited" {
		return fmt.Sprintf("\n## Implementation Deferred (Rate Limit Carryover)\nClaudeSuperpowersPath paused on a Claude rate limit; the saved plan carries over to the next cycle. This is an expected, healthy pause — not a degradation.\n\n```\n%s\n```\n", reason)
	}
	return fmt.Sprintf("\n## Implementation Degraded (Fallback)\nClaudeSuperpowersPath failed; degraded to deterministic analysis.\n\n```\n%s\n```\n", reason)
}

// goapFusionMaterializerSnapshotsSection lists every materializer snapshot
// patch (written by writeGoapFusionMaterializerSnapshot to
// ~/.go-bt-evolve/materializer-snapshots before a bare-repo wipe) that has not
// already been surfaced in a prior cycle report — filename, byte size, and
// changed-file count — so a silent wipe on the bare main repo leaves a
// visible trace in cycle history and on the dashboard instead of none.
// Already-reported filenames are tracked durably (agent-scope blackboard, the
// same pattern as saveSuperpowersPlanState) so a snapshot is listed exactly
// once, in the first cycle report after it was written, and does not
// accumulate forever in every later report.
func goapFusionMaterializerSnapshotsSection(bb *Blackboard) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".go-bt-evolve", "materializer-snapshots")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	reported := loadGoapFusionReportedSnapshots(bb)
	var lines []string
	var newNames []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".patch") || reported[e.Name()] {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		changed := 0
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "diff --git ") {
				changed++
			}
		}
		lines = append(lines, fmt.Sprintf("- `%s` — %d bytes, %d file(s) changed", e.Name(), len(data), changed))
		newNames = append(newNames, e.Name())
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Strings(lines)
	markGoapFusionReportedSnapshots(bb, newNames)
	return "\n\n## Materializer Snapshots\n\nMaterializer snapshot patch(es) written since the prior cycle (a bare-repo wipe was snapshotted before materializing, never destructive):\n\n" + strings.Join(lines, "\n")
}

// loadGoapFusionReportedSnapshots reads the durable set of materializer
// snapshot filenames already surfaced in a prior cycle report. Nil when the
// agent-scope blackboard is unavailable (unit paths, scope-off deployments),
// which makes every snapshot look new on every call — acceptable there since
// nothing persists across calls anyway.
func loadGoapFusionReportedSnapshots(bb *Blackboard) map[string]bool {
	reported := map[string]bool{}
	if bb.BB == nil || bb.BB.AgentName == "" {
		return reported
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
	e, err := bb.BB.Mgr.Get(scope, "goap_fusion_reported_materializer_snapshots")
	if err != nil {
		return reported
	}
	for _, name := range strings.Split(e.Value, "\n") {
		if name = strings.TrimSpace(name); name != "" {
			reported[name] = true
		}
	}
	return reported
}

// markGoapFusionReportedSnapshots records names as surfaced, merging with the
// existing durable set rather than overwriting it, so snapshots reported
// across multiple cycles all stay marked.
func markGoapFusionReportedSnapshots(bb *Blackboard, names []string) {
	if bb.BB == nil || bb.BB.AgentName == "" || len(names) == 0 {
		return
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
	reported := loadGoapFusionReportedSnapshots(bb)
	for _, n := range names {
		reported[n] = true
	}
	all := make([]string, 0, len(reported))
	for n := range reported {
		all = append(all, n)
	}
	sort.Strings(all)
	_ = bb.BB.Mgr.Set(scope, "goap_fusion_reported_materializer_snapshots", strings.Join(all, "\n"),
		"durable set of materializer snapshot filenames already surfaced in a cycle report", "text")
}

// goapFusionParkedBranchesSection lists every superpowers/* branch that is
// unmerged into master and whose tip commit is older than 24h, tagged
// pending_patch. reapOrphanedSuperpowersBranches only reaps an orphan once
// `git branch -d` finds it merged into HEAD; a genuinely unmerged branch
// survives that sweep forever, so it is exactly the parked-run triage
// backlog this section makes visible in cycle history and on the dashboard.
// Best-effort: an unreadable/non-git goapFusionRepo yields an empty section
// rather than failing the cycle.
func goapFusionParkedBranchesSection() string {
	out, err := runGoapShell(`git branch --list 'superpowers/*' --no-merged master --format='%(refname:short)^%(committerdate:iso-strict)'`)
	if err != nil {
		return ""
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	var lines []string
	for _, raw := range strings.Split(out, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		branch, tsRaw, ok := strings.Cut(raw, "^")
		if !ok {
			continue
		}
		branch = strings.TrimSpace(branch)
		ts, perr := time.Parse(time.RFC3339, strings.TrimSpace(tsRaw))
		if perr != nil || ts.After(cutoff) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- `%s` — pending_patch (last commit %s)", branch, ts.UTC().Format(time.RFC3339)))
	}
	if len(lines) == 0 {
		return ""
	}
	sort.Strings(lines)
	return "\n\n## Parked Work (pending_patch)\n\nUnmerged superpowers/* branch(es) older than 24h — parked-run triage backlog:\n\n" + strings.Join(lines, "\n")
}

func goapBacktickValueAfter(s, prefix string) string {
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	rest := s[start:]
	end := strings.Index(rest, "`")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}
