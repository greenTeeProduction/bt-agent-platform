package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/research"
	"github.com/nico/go-bt-evolve/internal/util"

	btcore "github.com/rvitorper/go-bt/core"
)

// goapFusionRepo is the main repo the scheduled loop builds and applies to.
// A var (not const) so preflight-guard tests can point it at an isolated
// throwaway repo.
var goapFusionRepo = "/home/nico/go-bt-evolve"

// goapFusionVaultDir is a var (not const) so note-writing tests can point it
// at an isolated temp dir instead of the live research vault.
var goapFusionVaultDir = "/mnt/ssd/clawd/wiki/bt-research"

// goapFusionGraphReport is a var (not const) so tests can point it at a
// synthetic GRAPH_REPORT.md instead of the live ~900KB report.
var goapFusionGraphReport = "/home/nico/go-bt-evolve/graphify-out/GRAPH_REPORT.md"

const (
	goapFusionSynthesesDir = "/mnt/ssd/clawd/wiki/bt-research/syntheses"
	goapFusionPlansDir     = "/mnt/ssd/clawd/wiki/bt-research/plans"
	goapFusionClaudeBin    = "/home/nico/.local/bin/claude"
	goapFusionGoBin        = "/usr/local/go/bin/go"
	goapFusionGraphifyTool = "graphify"

	// goapFusionRejectedLedger is the persistent corpus of known rejected unsafe
	// contexts (the rejected-context ledger) the continuous self-improving loop
	// runner replays against every new candidate to enforce the Monotonicity
	// Invariant of the Experience-Grounded Monotonicity Auditor — no mutation or
	// self-evolution edit may re-admit a previously rejected unsafe context.
	goapFusionRejectedLedger = "/mnt/ssd/clawd/wiki/bt-research/rejected-context-ledger.jsonl"

	// goapFusionCircuitHistoryWindow is the bounded state-hash history window the
	// CIRCUITPOLICY circuit breaker monitors to detect "Activity-Progress
	// Confusion" — the failure mode where the continuous self-improving loop
	// remains active by proposing syntactically valid but redundant patches that
	// never advance the task goal. Following the PatchBoard (2026) 3-hash window,
	// the breaker inspects the most recent goapFusionCircuitHistoryWindow state
	// hashes and HALTs the loop on a repeated hash (a state-transition cycle) or a
	// run of consecutive no-op patch proposals, instead of wasting tokens
	// indefinitely.
	goapFusionCircuitHistoryWindow = 3

	// goapFusionMaxLoopIterations is the finite runaway-loop backstop the
	// continuous self-improving loop runner enforces in addition to the
	// CIRCUITPOLICY window. The circuit breaker only halts on a *repeated* state
	// hash within the bounded window; a loop that keeps producing distinct,
	// never-repeating state hashes would slip past it and iterate forever. This
	// backstop bounds the total published state-hash history so the loop runner
	// always self-halts after a finite number of cycles even when every hash is
	// distinct — the "iterate forever without ever advancing the goal" tail of the
	// Activity-Progress Confusion failure mode. The backstop is HALF-OPEN: when it
	// trips, RunScheduledGoapFusionLoop clears the durable state-hash history so the
	// next cron tick starts from a fresh window rather than re-HALTing forever, and
	// goapFusionStateHashHistoryCap is held strictly above this threshold so the cap
	// alone never pins the history at the trip point.
	goapFusionMaxLoopIterations = 50

	// goapFusionMaxNoopPatchStreak is the bounded run of consecutive no-op patch
	// proposals the CIRCUITPOLICY loop runner tolerates before HALTing. The
	// repeated-state-hash circuit breaker only catches the "returned to a prior
	// state" cycle; a loop can instead publish an unbroken run of DISTINCT state
	// hashes while every proposed patch is a no-op that never advances the goal —
	// the "Activity-Progress Confusion" tail neither the repeated-hash breaker nor
	// the runaway-loop backstop fires on. Following the same PatchBoard (2026)
	// 3-window as the state-hash breaker, a streak of goapFusionMaxNoopPatchStreak
	// or more consecutive no-op patches HALTs the loop rather than iterating on
	// syntactically valid but empty patches indefinitely.
	goapFusionMaxNoopPatchStreak = 3
)

// goapProgramClaimLease bounds how long a program-store claim recorded by
// research.ProgramStore.ClaimActiveForCycle blocks a sibling cycle's
// PrioritizeGoapGoals from also charging or planning the SAME program —
// default: the cycle budget, so a claim outlives one cycle's typical
// wall-clock but a crashed/abandoned cycle's stale claim is reclaimable well
// before the next tick (the loop-runner burned 3 cycles 2026-07-23
// 12:38-14:55 on milestones a sibling cycle was already landing).
const goapProgramClaimLease = time.Hour

// goapProgramClaimedBySibling reports whether p is currently claimed by a
// DIFFERENT agent within the lease window — evidence a sibling cycle is
// still landing it, so this cycle's queueing pass must not advertise its
// milestones as available plan work either (it already declined to charge
// it in PrioritizeGoapGoals' claim step above).
func goapProgramClaimedBySibling(p *research.Program, agentID string) bool {
	return p.ClaimedBy != "" && p.ClaimedBy != agentID && time.Since(p.ClaimedAt) < goapProgramClaimLease
}

func init() {
	registerGoapFusionActions()
}

func registerGoapFusionActions() {
	// Conditions for GOAP fusion tree routing
	RegisterCondition("IsFusionTask", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"fusion", "improve", "expand", "capability", "research", "evolve", "update",
			"enhance", "upgrade", "optimize", "refactor", "extend",
			"apply", "implement", "fix", "create", "build", "add", "install")
	})
	RegisterCondition("HasNewGaps", func(bb *Blackboard) bool {
		v, _ := bb.ChainState["goap_fusion_goals_unchanged"].(string)
		return v != "true"
	})
	RegisterCondition("NoNewGaps", func(bb *Blackboard) bool {
		v, _ := bb.ChainState["goap_fusion_goals_unchanged"].(string)
		return v == "true"
	})
	// NoNewGapsOrImplDegraded is the fallback-eligible head guard for
	// ScheduledAnalysisPath. It opens the deterministic analysis fallback in
	// BOTH the pre-existing "goals unchanged" (NoNewGaps) case AND the new
	// "implementation degraded" case, so that ANY failure of
	// ClaudeSuperpowersPath — not just a Claude rate limit — degrades the cycle
	// to deterministic analysis + build/graphify evidence instead of aborting
	// the whole loop.
	RegisterCondition("NoNewGapsOrImplDegraded", func(bb *Blackboard) bool {
		if v, _ := bb.ChainState["goap_fusion_goals_unchanged"].(string); v == "true" {
			return true
		}
		v, _ := bb.ChainState["goap_fusion_impl_degraded"].(string)
		return v == "true"
	})

	// RunGoapFusionNotebookLMResearch performs a GOAP-owned NotebookLM query so the
	// scheduled fusion runner is not dependent on the separate notebooklm-researcher
	// agent or its vault handoff. It writes a dedicated synthesis file that the
	// normal ReadVaultResearch step ingests immediately afterwards.
	RegisterAction("RunGoapFusionNotebookLMResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if nlmQuotaExhausted(bb) {
			until, _ := nlmQuotaExhaustedUntil(bb)
			reason := fmt.Sprintf("NotebookLM daily quota window exhausted until %s (cached from an earlier cycle)", until.Format(time.RFC3339))
			setGoapState(bb, "notebooklm_skip_reason", reason)
			bb.Result = "## GOAP NotebookLM Research Skipped\n\n" + reason
			bb.Outcome = "goap_fusion_notebooklm_quota_cached"
			return -1 // fail fast so ResearchRouter runs the Claude review fallback
		}
		query := buildGoapFusionNotebookLMQuery(bb.Task, readSectionAwareGraphContext())
		out := nlmRun(180*time.Second, "notebook", "query", defaultNotebook, query)
		if isGoapNotebookLMFailure(out) {
			if isGoapNotebookLMQuotaError(out) {
				saveNlmQuotaExhausted(bb, time.Now())
			}
			setGoapState(bb, "notebooklm_skip_reason", truncateGoap(out, 2000))
			bb.Result = fmt.Sprintf("## GOAP NotebookLM Research Failed\n\nNotebookLM query failed; refusing to proceed from stale vault research. The raw nlm output below is the actual cause — do not assume auth.\n\n```\n%s\n```", truncateGoap(out, 2000))
			bb.Outcome = "goap_fusion_notebooklm_failed"
			return -1
		}

		answer := extractNotebookLMAnswer(out)
		program := extractGoapProgram(answer)
		goals := extractGoapResearchGoals(answer)
		if len(goals) == 0 && program == nil {
			if first := fallbackGoapGoal(answer); first != "" {
				goals = []goapResearchGoal{{Goal: first, Gap: "NotebookLM produced a cited recommendation for BT platform improvement; see raw answer."}}
			}
		}
		if len(goals) == 0 && program == nil {
			bb.Result = "## GOAP NotebookLM Research Failed\n\nNotebookLM returned no parseable recommendation."
			bb.Outcome = "goap_fusion_notebooklm_failed"
			return -1
		}
		if program != nil {
			persistGoapProgram(bb, program, "notebooklm")
		}
		appendGoapResearchGoals(bb, goals)
		goalSummary := strings.Join(goapResearchGoalLines(bb), "\n- ")

		ts := time.Now().Format("2006-01-02T150405")
		path := filepath.Join(goapFusionSynthesesDir, fmt.Sprintf("goap-fusion-notebooklm-%s.md", ts))
		report := fmt.Sprintf("# GOAP Fusion NotebookLM Research — %s\n\n## Notebook\n`%s`\n\n## Recommendations\n- %s\n\n## Raw NotebookLM Answer\n%s\n", ts, defaultNotebook, goalSummary, answer)
		if err := writeString(path, report); err != nil {
			bb.Result = fmt.Sprintf("## GOAP NotebookLM Research Failed\n\nCould not write `%s`: %v", path, err)
			bb.Outcome = "goap_fusion_notebooklm_failed"
			return -1
		}

		setGoapState(bb, "notebooklm_research", report)
		setGoapState(bb, "notebooklm_research_path", path)
		bb.Result = fmt.Sprintf("## GOAP NotebookLM Research Complete\n\nPath: `%s`\n\nGoals:\n- %s", path, goalSummary)
		return 1
	})

	// ReadVaultResearch reads the newest NotebookLM research syntheses, evolution
	// reports, and improvement plans from the Obsidian vault into the blackboard.
	// Reads are capped per category: the loop appends new synthesis files every
	// scheduled cycle and nothing prunes the vault (769 syntheses and counting),
	// so an uncapped read grows without bound while downstream consumers
	// truncate the result anyway.
	RegisterAction("ReadVaultResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var sources []string

		sources = append(sources, readNewestVaultDocs(goapFusionSynthesesDir, "Synthesis",
			func(name string) bool { return strings.HasSuffix(name, ".md") }, 8, 2000)...)
		sources = append(sources, readNewestVaultDocs(goapFusionVaultDir, "Evolution Report",
			func(name string) bool { return strings.HasPrefix(name, "bt-evolution-") }, 4, 3000)...)
		sources = append(sources, readNewestVaultDocs(goapFusionPlansDir, "Plan",
			func(name string) bool { return strings.HasSuffix(name, ".md") }, 4, 2000)...)

		if len(sources) == 0 {
			sources = append(sources, "No vault research found. Proceed with codebase-only analysis.")
		}
		setGoapState(bb, "vault_research", strings.Join(sources, "\n\n---\n\n"))
		bb.Result = fmt.Sprintf("## Vault Research Loaded\n\n%d research documents read from %s",
			len(sources), goapFusionVaultDir)
		return 1
	})

	// ReadGraphifyReport reads the graphify codebase analysis report.
	RegisterAction("ReadGraphifyReport", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		b, err := os.ReadFile(goapFusionGraphReport)
		if err != nil {
			bb.Result = fmt.Sprintf("## Graphify Report Error\n\nCould not read %s: %v", goapFusionGraphReport, err)
			bb.Outcome = "goap_fusion_graphify_missing"
			return -1
		}
		// Extract the most useful sections: summary, god nodes, community hubs
		extracted := sectionAwareGraphContext(string(b))

		setGoapState(bb, "graphify_report", extracted)
		bb.Result = fmt.Sprintf("## Graphify Report Loaded\n\n%d bytes extracted from %s", len(extracted), goapFusionGraphReport)
		return 1
	})

	// AnalyzeImprovementGaps cross-references vault research with codebase structure
	// to identify actionable improvement gaps.
	RegisterAction("AnalyzeImprovementGaps", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		vaultResearch, _ := bb.ChainState["goap_fusion_vault_research"].(string)
		graphReport, _ := bb.ChainState["goap_fusion_graphify_report"].(string)

		if vaultResearch == "" && graphReport == "" {
			bb.Result = "## Gap Analysis\n\nNo research or graph data available. Cannot perform gap analysis."
			bb.Outcome = "goap_fusion_no_data"
			return -1
		}

		// Deterministic heuristics for gap detection:
		gaps := []string{}

		// Check: does the codebase reference the key research papers?
		researchTopics := []struct {
			topic string
			check string
		}{
			{"LLM-supervised GP for BT evolution", "dynamic mutation rates, population state vector, meta-controller"},
			{"Auction-based task allocation", "auction, task allocation, multi-agent coordination"},
			{"Continual meta-learning (MetaClaw)", "skill library, failure-to-skill, fast adaptation"},
			{"Code-driven BT generation (Code-BT)", "code generation, API selection, rule-constrained generation"},
			{"Typed-edge validation", "guard/effect/recovery/approval typed edges"},
			{"Checkpoint verification", "deterministic postcondition checks, evidence gate"},
		}
		for _, rt := range researchTopics {
			if strings.Contains(vaultResearch, rt.topic) {
				// Cross-reference: is this reflected in the codebase?
				found := strings.Contains(graphReport, rt.check) ||
					strings.Contains(graphReport, rt.topic)
				if !found {
					gaps = append(gaps, fmt.Sprintf("GAP: Research finding '%s' not reflected in codebase structure", rt.topic))
				}
			}
		}

		// Add every research-backed goal (grill + NotebookLM/Claude review all
		// append to the shared list) before static graph checks so research
		// goals are not starved by stale heuristic P0/P2 goals.
		goalLines := goapResearchGoalLines(bb)
		gapLines := goapResearchGapLines(bb)
		for i, g := range goalLines {
			gap := "research recommended this implementation target."
			if i < len(gapLines) {
				gap = gapLines[i]
			}
			gaps = append(gaps,
				"NOTEBOOKLM_GOAL: "+g,
				"NOTEBOOKLM_GAP: "+gap)
		}

		// Check for domain coverage gaps
		if strings.Contains(graphReport, "AllDomainTrees") {
			gaps = append(gaps, "CHECK: AllDomainTrees coverage — verify all registered trees have smoke tests and descriptions")
		}

		// NOTE: A prior "testability" heuristic here fabricated a bogus
		// "Engine tests executable — verify no import cycles" CHECK gap whenever
		// a graph report merely mentioned "test" (as nearly every report does).
		// It had no real blocker behind it and flowed into PrioritizeGoapGoals as
		// an un-implementable P0 "Unblock engine tests" goal that dead-lettered
		// the loop. Removed: a false import-cycle blocker is never a real gap.

		if len(gaps) == 0 {
			gaps = append(gaps, "No clear structural gaps detected. Next: review individual tree quality and condition coverage.")
		}

		setGoapState(bb, "improvement_gaps", strings.Join(gaps, "\n"))
		bb.Result = fmt.Sprintf("## Gap Analysis\n\n%d gaps identified:\n\n%s", len(gaps), strings.Join(gaps, "\n"))
		return 1
	})

	// PrioritizeGoapGoals builds a GOAP goal queue from identified gaps, ordered by
	// impact/risk ratio.
	RegisterAction("PrioritizeGoapGoals", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		gapsStr, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
		goals := []string{}

		// Red pre-check (2026-07-23 review gap 5): head milestones carrying
		// prior red-pass evidence get their recorded RED re-run BEFORE any
		// charge or Claude plan phase — a stale milestone (work already
		// landed) completes here for the cost of one test run instead of
		// burning two full plan cycles on the RedPassStreak treadmill.
		precheckGoapStaleMilestones(bb)

		// P0: Verifiable correctness (test blockers, build failures)
		// P1: New capability (domain tree, condition node, action)
		// P2: Quality improvement (coverage, refactoring)

		// An active multi-cycle program's pending milestones go to the head
		// of the queue — up to the plan builder's task capacity per cycle, so
		// a program no longer costs one full cycle per milestone (the 5-
		// milestone auction program took 5 cycles ≈ 5 hours at 1/cycle).
		// Charging the head milestone's attempt (RecordAttemptAndMaybeBlock +
		// save) is a read-modify-write against the shared program store, so it
		// must go through UpdatePrograms' flock like every other program-store
		// writer (persistGoapProgram, RefundAttempt, RecordRedPass, MarkDone) —
		// a bare OpenPrograms+Save here could clobber a concurrent writer's
		// already-persisted change with this call's stale in-memory copy.
		var chargedProgramID string
		var chargedIdx int
		var charged bool
		if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
			// ClaimActiveForCycle (rather than plain Active()) refuses the
			// program when it is still claimed by a DIFFERENT, in-lease
			// sibling cycle — a sibling agent must not plan/charge a program
			// another cycle is actively landing.
			p := ps.ClaimActiveForCycle(bb.RunID, goapProgramClaimLease)
			if p == nil {
				return nil
			}
			// Record an attempt on the head pending milestone; if it has now
			// failed too many times it is marked blocked (and skipped below),
			// so an unbuildable/fabricated milestone the agent keeps
			// declining stops freezing the program forever.
			idx, m := p.NextMilestone()
			if m == nil {
				return nil
			}
			ps.RecordAttemptAndMaybeBlock(p.ID, idx, goapProgramMaxMilestoneAttempts)
			chargedProgramID, chargedIdx, charged = p.ID, idx, true
			return nil
		}); err == nil && charged {
			// Stamp WHICH milestone was charged so an infrastructure failure
			// later in this cycle can refund exactly this charge — the
			// queued refs below are re-read after the store re-open and may
			// start past a just-blocked one.
			setGoapStateDurable(bb, "program_milestone_charged", fmt.Sprintf("%s:%d", chargedProgramID, chargedIdx))
		}

		// Re-open (read-only) so a just-blocked milestone is reflected in the
		// queueing pass below.
		if ps, err := research.OpenPrograms(goapProgramsPath); err == nil {
			if p := ps.Active(); p != nil && !goapProgramClaimedBySibling(p, bb.RunID) {
				var refs []string
				for idx := range p.Milestones {
					m := &p.Milestones[idx]
					if m.Status != "pending" {
						continue
					}
					goals = append(goals, fmt.Sprintf("[P0] Program %q milestone %d/%d: %s", p.Title, idx+1, len(p.Milestones), m.Goal))
					refs = append(refs, fmt.Sprintf("%s:%d", p.ID, idx))
					if len(refs) == maxGoalDrivenTasks {
						break
					}
				}
				if len(refs) > 0 {
					setGoapStateDurable(bb, "program_milestone", strings.Join(refs, ","))
				}
			}
		}

		// Research goals carry a durable failure budget: after
		// goapGoalMaxAttempts genuine implementation failures a goal is
		// abandoned here instead of treadmilling (11 blind retries on
		// 2026-07-10). The head SURVIVING goal is stamped so a failed cycle
		// charges exactly the goal it attempted.
		goalBudget, _ := research.OpenGoalAttempts(goapGoalAttemptsPath)
		var abandonedGoals []string
		researchGoalStamped := false
		for _, nlmGoal := range goapFusionNotebookLMGoalsFromGaps(gapsStr) {
			if goapAbandonedResearchGoal(goalBudget, nlmGoal) {
				abandonedGoals = append(abandonedGoals, truncateGoap(nlmGoal, 90))
				continue
			}
			goals = append(goals, "[P0] NotebookLM research: "+nlmGoal)
			if !researchGoalStamped {
				setGoapStateDurable(bb, "research_goal_charged", goapResearchGoalKey(nlmGoal))
				// The raw goal text rides along so a red-pass closure can
				// record it goap:implemented (research prompts dedup by
				// title, not by budget key).
				setGoapStateDurable(bb, "research_goal_charged_text", nlmGoal)
				researchGoalStamped = true
			}
		}
		if len(abandonedGoals) > 0 {
			setGoapState(bb, "research_goals_abandoned", strings.Join(abandonedGoals, " | "))
			Info("goap fusion: research goals abandoned after repeated implementation failures",
				"count", len(abandonedGoals))
		}

		// Only an *affirmative* build blocker becomes the P0 "Unblock engine
		// tests" goal. A prior bare `Contains(gapsStr,"import cycle")` matcher
		// fabricated it from ANY incidental mention — and this codebase discusses
		// import-cycle avoidance everywhere ("engine cannot import domains —
		// import cycle", "avoid an import cycle when guard builders move"), so a
		// research gap noting a boundary design constraint spawned an
		// un-implementable P0 goal for a blocker that does not exist (the engine
		// package builds cleanly) and dead-lettered the loop. The discriminator
		// requires blocker phrasing and rejects avoidance/negation notes.
		if goapFusionHasEngineTestBlocker(gapsStr) {
			goals = append(goals, "[P0] Unblock engine tests — fix import cycle or test blockers preventing test execution")
		}

		// Catalog goals carry real file anchors so the goal-driven plan
		// builder can task them — pathless goals never become tasks.
		if strings.Contains(gapsStr, "LLM-supervised") || strings.Contains(gapsStr, "meta-controller") {
			goals = append(goals, "[P1] Add LLM-supervised population dynamics to gardener — dynamic mutation rate adjustment (files: internal/gardener/gardener.go, internal/evolution/evolve_v2.go)")
		}

		if strings.Contains(gapsStr, "Auction-based") {
			goals = append(goals, "[P1] Implement auction-based task allocation for A2A agent coordination (files: internal/a2a/server.go, internal/a2a/client.go)")
		}

		if strings.Contains(gapsStr, "MetaClaw") || strings.Contains(gapsStr, "skill library") {
			goals = append(goals, "[P1] Build failure-to-skill pipeline: extract BT mutations from agent failures into skills (files: internal/evolution/reflection_store.go, internal/gardener/gardener.go)")
		}

		if strings.Contains(gapsStr, "Code-BT") {
			goals = append(goals, "[P2] Prototype code-driven BT generation: LLM generates Go code → compiled to executable BT (files: internal/evolution/tree_builders.go, internal/evolution/node_types.go)")
		}

		if strings.Contains(gapsStr, "typed-edge") {
			goals = append(goals, "[P2] Add typed-edge validation to tree generation: guard/effect/recovery/approval semantics (files: internal/evolution/node_types.go, internal/evolution/tree_builders.go)")
		}

		if strings.Contains(gapsStr, "Checkpoint verification") {
			goals = append(goals, "[P2] Extend checkpoint verification to all domain trees: deterministic postcondition checks (files: internal/engine/checkpoint_verifier.go, internal/domains/trees.go)")
		}

		if strings.Contains(gapsStr, "AllDomainTrees") || strings.Contains(gapsStr, "domain coverage") {
			goals = append(goals, "[P2] Ensure all domain trees have smoke tests, descriptions, and condition coverage (files: internal/domains/trees.go, internal/domains/domains_test.go)")
		}

		if len(goals) == 0 {
			goals = append(goals, "[P2] Review and improve condition node routing coverage across all domain trees")
		}

		currentGoals := strings.Join(goals, "\n")
		// Compare with previous run — skip to analysis if identical (no new
		// gaps), UNLESS an active program milestone is driving the cycle. A
		// multi-cycle program milestone produces the identical goal queue
		// every cycle until it lands, so the "unchanged goals → just analyze"
		// heuristic (meant for the catalog case where nothing new to build
		// exists) would route it to analysis forever and never implement it —
		// the same false-positive that HALTed the loop, one layer down
		// (2026-07-05: milestone 2 analyzed, never built).
		activeMilestone, _ := bb.ChainState["goap_fusion_program_milestone"].(string)
		latestPath := filepath.Join(goapFusionVaultDir, "goap-fusion-latest.md")
		if b, err := os.ReadFile(latestPath); err == nil && strings.TrimSpace(activeMilestone) == "" {
			prevGoals := extractGoapGoals(string(b))
			if prevGoals == currentGoals && currentGoals != "" {
				setGoapState(bb, "goal_queue", currentGoals)
				setGoapState(bb, "goals_unchanged", "true")
				bb.Result = fmt.Sprintf("## Goals Unchanged\n\nNo new gaps detected. Same %d goal(s) as previous run:\n\n%s", len(goals), currentGoals)
				return 1
			}
		}

		setGoapState(bb, "goal_queue", currentGoals)
		bb.Result = fmt.Sprintf("## GOAP Goal Queue\n\n%d goals prioritized:\n\n%s", len(goals), strings.Join(goals, "\n"))
		return 1
	})

	// VerifyGoapBuild runs go test on changed packages and go build ./...
	RegisterAction("VerifyGoapBuild", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var results []string

		// A bare main repo has no working tree to build: any files on disk are
		// frozen leftovers from the pre-bare conversion, and go's VCS stamping
		// dies on `git status` (exit 128). The run's changes were already
		// build- and test-verified inside its worktree by the apply step
		// (applySuperpowersRunFromBareRepo) — re-verifying here would check
		// stale source, not what landed. Report and pass through.
		if out, err := runGoapShell("git rev-parse --is-bare-repository"); err == nil && strings.TrimSpace(out) == "true" {
			bb.Result = "## Verification Delegated\n\nMain repo is bare (no working tree to build); the run's changes were build- and test-verified in its worktree during apply. Skipping stale-tree re-verification."
			setGoapState(bb, "verify_result", "delegated to apply-stage worktree verification (bare main repo)")
			return 1
		}

		// runGoapShell's Dir is already goapFusionRepo; no cd needed. Outer
		// shell deadlines must exceed the inner go tool budgets (see
		// runGoapShellTimeout) so slow-but-passing runs aren't killed and
		// misreported as verification failures on this ARM host.
		buildCmd := "/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli"
		if out, err := runGoapShellTimeout(buildCmd, 180*time.Second); err != nil {
			results = append(results, fmt.Sprintf("BUILD FAILED (%v):\n%s", err, out))
			setGoapState(bb, "verify_result", strings.Join(results, "\n"))
			bb.Result = fmt.Sprintf("## Verification Failed\n\n%s", strings.Join(results, "\n"))
			bb.Outcome = "goap_fusion_verify_failed"
			return -1
		}
		results = append(results, "go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED")

		testCmd := "/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestGoapFusion_Structure|TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestValidateOutputQuality' -timeout 180s"
		if out, err := runGoapShellTimeout(testCmd, 240*time.Second); err != nil {
			results = append(results, fmt.Sprintf("TEST FAILED (%v):\n%s", err, truncateGoap(out, 3000)))
			setGoapState(bb, "verify_result", strings.Join(results, "\n"))
			bb.Result = fmt.Sprintf("## Verification Failed\n\n%s", strings.Join(results, "\n"))
			bb.Outcome = "goap_fusion_verify_failed"
			return -1
		}
		results = append(results, "focused go tests: PASSED")

		setGoapState(bb, "verify_result", strings.Join(results, "\n"))
		bb.Result = fmt.Sprintf("## Verification Passed\n\n%s", strings.Join(results, "\n"))
		return 1
	})

	// ── GrillMeNotebookLM — Multi-turn critical review ("grill me" pattern) ──
	// Uses conversation_id to chain 3 rounds of questioning:
	//   Round 1: "What is the BT framework missing? Be critical."
	//   Round 2: "Push harder. What exact code should change? File paths, tree types."
	//   Round 3: "Final demand: concrete implementation plan with test commands."
	// The final answer is saved to ChainState and the vault.
	RegisterAction("GrillMeNotebookLM", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if nlmQuotaExhausted(bb) {
			until, _ := nlmQuotaExhaustedUntil(bb)
			bb.Result = fmt.Sprintf("## GrillMe Skipped\n\nNotebookLM daily quota window exhausted until %s; skipping to preserve calls.", until.Format(time.RFC3339))
			bb.Outcome = "goap_fusion_grill_skipped_quota"
			// 1, not 0: in this engine 0 means Running — a memoryless Sequence
			// re-ticks from the top forever and the run dies "partial" at the
			// maxTicks cap. Success lets the Sequence advance to ResearchRouter,
			// whose Claude review fallback exists exactly for this quota case.
			return 1
		}
		graphSnippet := readSectionAwareGraphContext()

		// Read grill round tracking from the agent-scope blackboard — it must
		// survive across scheduled runs (ChainState dies with each run).
		grillRound, conversationID := loadGrillState(bb)

		// Build round-specific query
		var query string
		switch grillRound {
		case 1:
			query = buildGrillRound1Query(graphSnippet, deriveGraphifyReuseTopic(bb.Task))
		case 2:
			query = `Push harder. Your previous answer identified gaps — now get CONCRETE.

		For each gap you identified:
		- What EXACT Go files need to change? (give file paths in internal/engine/, internal/domains/, etc.)
		- What tree node types are needed? (Action, Condition, Selector, Sequence, HumanApprovalGate, ChainAction, etc.)
		- What new action/condition names should be registered?
		- What tests should verify the change?

		Be specific enough that an automated Claude Code run could implement this without further clarification.`
		default: // round 3+
			query = `FINAL DEMAND: Convert your analysis into an implementation plan.

		Give me a task breakdown ordered by impact:
		1. P0 item: exact file path, what to add/change, test command
		2. P1 item: exact file path, what to add/change, test command
		3. P2 item: exact file path, what to add/change, test command

		Format each as:
		GOAL: <one-sentence implementation target>
		GAP: <why current codebase needs it>
		FILES: <specific file paths>
		CHANGE: <exact code changes — what to add/modify>
		TESTS: <specific go test commands>

		This will be executed by an automated Claude Code pipeline. Make it executable.`
		}

		// Execute NotebookLM query
		var args []string
		args = append(args, "notebook", "query", "--json", "--timeout", "180", defaultNotebook, query)
		if conversationID != "" {
			args = append(args, "--conversation-id", conversationID)
		}
		out := nlmRun(200*time.Second, args...)

		if isGoapNotebookLMFailure(out) {
			if isGoapNotebookLMQuotaError(out) {
				saveNlmQuotaExhausted(bb, time.Now())
			}
			// If grill fails, fall back to single-shot research path
			bb.Result = fmt.Sprintf("## GrillMe Round %d Failed\n\nNotebookLM query failed; falling back to single-shot research.\n\n```\n%s\n```", grillRound, truncateGoap(out, 2000))
			bb.Outcome = "goap_fusion_grill_failed"
			// 1, not 0 (Running): see the quota-skip note above — the Sequence
			// only continues to the single-shot research path on Success.
			return 1
		}

		answer := extractNotebookLMAnswer(out)
		newConvID := extractConversationID(out)
		if newConvID != "" {
			conversationID = newConvID
		}

		// Save grill state for the next scheduled run. After round 3 the
		// escalation wraps to round 1 with a FRESH conversation — reusing the
		// exhausted one would grow it without bound and skew new round-1 answers.
		if grillRound >= 3 {
			saveGrillState(bb, 1, "")
		} else {
			saveGrillState(bb, grillRound+1, conversationID)
		}

		// Extract implementation targets from grill answer: goals APPEND to
		// the shared multi-goal list (the research router adds its own later)
		// and a PROGRAM block registers a multi-cycle program.
		if program := extractGoapProgram(answer); program != nil {
			persistGoapProgram(bb, program, "grill")
		}
		appendGoapResearchGoals(bb, extractGoapResearchGoals(answer))
		goal, gap := extractGoapNotebookLMRecommendation(answer)

		// Save grill transcript to vault
		ts := time.Now().Format("2006-01-02T150405")
		path := filepath.Join(goapFusionSynthesesDir, fmt.Sprintf("goap-fusion-grill-r%d-%s.md", grillRound, ts))
		report := fmt.Sprintf("# GOAP Fusion Grill Me — Round %d — %s\n\n## Notebook\n`%s`\n\n## Conversation\n`%s`\n\n## Answer\n%s\n\n## Extracted\nGOAL: %s\nGAP: %s\n",
			grillRound, ts, defaultNotebook, conversationID, answer, goal, gap)
		_ = writeString(path, report)
		setGoapState(bb, "notebooklm_research_path", path)

		bb.Result = fmt.Sprintf("## Grill Me — Round %d Complete\n\nConversation: `%s`\nPath: `%s`\n\nGOAL: %s\n\nGAP: %s",
			grillRound, conversationID, path, goal, gap)
		return 1
	})
}

// readNewestVaultDocs returns the maxFiles most recently modified files in dir
// whose names satisfy match, each truncated to perFileLimit and labeled. File
// infos are fetched once before sorting (not inside the comparator).
func readNewestVaultDocs(dir, label string, match func(string) bool, maxFiles, perFileLimit int) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type vaultDoc struct {
		name string
		mod  time.Time
	}
	docs := make([]vaultDoc, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		docs = append(docs, vaultDoc{name: e.Name(), mod: info.ModTime()})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].mod.After(docs[j].mod) })
	out := make([]string, 0, maxFiles)
	for _, d := range docs {
		if len(out) == maxFiles {
			break
		}
		b, rerr := os.ReadFile(filepath.Join(dir, d.name))
		if rerr != nil {
			continue
		}
		content := string(b)
		// Quota-error garbage (pre-bd8c5b6 syntheses captured NotebookLM
		// error output as "research") must not feed gap analysis — skip and
		// take the next-newest doc instead.
		if isFusionQuotaGarbage(content) {
			continue
		}
		out = append(out, fmt.Sprintf("=== %s: %s ===\n%s", label, d.name, truncateGoap(content, perFileLimit)))
	}
	return out
}

func setGoapState(bb *Blackboard, key, value string) {
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["goap_fusion_"+key] = value
}

// setGoapStateDurable stamps a GOAP fusion state key in ChainState AND the
// agent-scope blackboard, mirroring saveSuperpowersPlanState/saveGrillState:
// a resumed cron tick builds a fresh Blackboard (RunOnce) whose ChainState
// dies with the run, so queue-time charge stamps (program milestone /
// research goal) must also survive there — otherwise a genuine failure on a
// resumed tick silently fails to charge or refund the budget it should hit.
func setGoapStateDurable(bb *Blackboard, key, value string) {
	setGoapState(bb, key, value)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_"+key, value,
			"durable GOAP fusion charge stamp for preflight resume", "text")
	}
}

// loadGoapChargeStampsDurable is the read-back counterpart of
// setGoapStateDurable: a resumed cron tick builds a fresh Blackboard
// (RunOnce) whose ChainState dies with the run, so the charge stamps written
// by setGoapStateDurable (program_milestone_charged, program_milestone,
// research_goal_charged, research_goal_charged_text) must be restored from
// the agent-scope store into ChainState before the resumed tick's failure
// handlers (chargeGoapResearchGoalFailure /
// refundGoapMilestoneAttemptForInfraFailure) look for them there. It only
// fills keys ChainState doesn't already hold, so an in-flight originating
// tick's fresher value is never clobbered by a stale durable stamp.
func loadGoapChargeStampsDurable(bb *Blackboard) {
	if bb.BB == nil || bb.BB.AgentName == "" {
		return
	}
	scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
	for _, key := range []string{
		"program_milestone_charged",
		"program_milestone",
		"research_goal_charged",
		"research_goal_charged_text",
	} {
		if _, ok := bb.ChainState["goap_fusion_"+key]; ok {
			continue
		}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_"+key); err == nil {
			setGoapState(bb, key, e.Value)
		}
	}
}

// Grill state must survive across scheduled runs: each cron tick executes
// GrillMeNotebookLM once, and ChainState dies with the run (RunOnce builds a
// fresh Blackboard). The agent-scope blackboard persists to disk, so the
// 1→2→3 round escalation and conversation chaining are tracked there;
// ChainState is the fallback when the scoped blackboard is disabled.
func loadGrillState(bb *Blackboard) (round int, conversationID string) {
	round = 1
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_grill_round"); err == nil {
			if n, aerr := strconv.Atoi(strings.TrimSpace(e.Value)); aerr == nil && n >= 1 && n <= 3 {
				round = n
			}
		}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_grill_conversation_id"); err == nil {
			conversationID = strings.TrimSpace(e.Value)
		}
		return round, conversationID
	}
	if s, ok := bb.ChainState["goap_fusion_grill_round"].(string); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 1 && n <= 3 {
			round = n
		}
	}
	conversationID, _ = bb.ChainState["goap_fusion_grill_conversation_id"].(string)
	return round, conversationID
}

func saveGrillState(bb *Blackboard, round int, conversationID string) {
	setGoapState(bb, "grill_round", strconv.Itoa(round))
	setGoapState(bb, "grill_conversation_id", conversationID)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_grill_round", strconv.Itoa(round), "grill-me round counter (1-3)", "text")
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_grill_conversation_id", conversationID, "grill-me NotebookLM conversation id", "text")
	}
}

// Superpowers plan state must survive across scheduled runs the same way grill
// state and the last-reviewed SHA do: each cron tick builds a fresh Blackboard
// (RunOnce) whose ChainState dies with the run, so the 4a60278 preflight resume
// branch can only re-open a rate-limited carryover if BOTH the plan path and the
// active plan body live in the agent-scope blackboard, which persists to disk.
// ChainState is the fallback when the scoped blackboard is disabled (unit paths,
// scope-off deployments) so a single run still round-trips.
func saveSuperpowersPlanState(bb *Blackboard, planPath, activePlan string) {
	setGoapState(bb, "superpowers_plan_path", planPath)
	setGoapState(bb, "superpowers_active_plan", activePlan)
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_superpowers_plan_path", planPath,
			"durable Superpowers plan path for preflight resume", "text")
		_ = bb.BB.Mgr.Set(scope, "goap_fusion_superpowers_active_plan", activePlan,
			"durable Superpowers active plan body for preflight resume", "text")
	}
}

func loadSuperpowersPlanState(bb *Blackboard) (planPath, activePlan string) {
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_superpowers_plan_path"); err == nil {
			planPath = strings.TrimSpace(e.Value)
		}
		if e, err := bb.BB.Mgr.Get(scope, "goap_fusion_superpowers_active_plan"); err == nil {
			activePlan = e.Value
		}
		if planPath != "" || activePlan != "" {
			return planPath, activePlan
		}
	}
	planPath, _ = bb.ChainState["goap_fusion_superpowers_plan_path"].(string)
	activePlan, _ = bb.ChainState["goap_fusion_superpowers_active_plan"].(string)
	return strings.TrimSpace(planPath), activePlan
}

// clearSuperpowersPlanState wipes the durable plan state after a successful
// apply so the next scheduled cycle does not re-resume an already completed plan
// and loop forever re-applying finished work. It also retires the durable
// charge-stamp keys set by setGoapStateDurable (program_milestone_charged,
// program_milestone, research_goal_charged, research_goal_charged_text): every
// call site here already marks a cycle as completed/abandoned, so leaving those
// stamps behind would let a later, unrelated cycle's failure handler
// (chargeGoapResearchGoalFailure / refundGoapMilestoneAttemptForInfraFailure)
// read a stale stamp and charge or refund the wrong goal/milestone.
func clearSuperpowersPlanState(bb *Blackboard) {
	keys := []string{
		"goap_fusion_superpowers_plan_path",
		"goap_fusion_superpowers_active_plan",
		"goap_fusion_program_milestone_charged",
		"goap_fusion_program_milestone",
		"goap_fusion_research_goal_charged",
		"goap_fusion_research_goal_charged_text",
	}
	if bb.ChainState != nil {
		for _, key := range keys {
			delete(bb.ChainState, key)
		}
	}
	if bb.BB != nil && bb.BB.AgentName != "" {
		scope := blackboard.Scope{Kind: blackboard.ScopeAgent, ID: bb.BB.AgentName}
		for _, key := range keys {
			_ = bb.BB.Mgr.Delete(scope, key)
		}
	}
}

// Claude rate-limit backoff state (save/load/active/clear, reset-hint
// parsing, and the fleet-wide store) lives in goap_claude_backoff.go.

// goapRedPrecheckTimeout bounds one charge-time RED re-run. RED commands are
// scoped `go test -run X ./pkg` invocations; on the tegra a cold engine
// compile takes a few minutes, so the bound guards a wedged run, not a slow
// one.
const goapRedPrecheckTimeout = 8 * time.Minute

// goapRedPrecheckMaxPerCycle caps how many stale milestones one cycle may
// pre-check-complete, bounding the charge-time cost when a whole program
// landed out-of-band.
const goapRedPrecheckMaxPerCycle = 3

// errGoapRedPrecheckUnavailable marks a pre-check that could not run at all —
// distinct from a failing RED, which is evidence. The unstubbed default
// returns it under `go test` so a test that forgets to stub the runner can
// neither exec real shells nor clear live red-pass evidence (the gap-1
// test-pollution class).
var errGoapRedPrecheckUnavailable = errors.New("goap red pre-check unavailable")

// goapRedPrecheckRunFn runs a recorded RED command against the HEAD-
// materialized main checkout; a package var so tests stub the shell.
var goapRedPrecheckRunFn = func(cmd string) (string, error) {
	if testing.Testing() {
		return "", errGoapRedPrecheckUnavailable
	}
	return runGoapShellTimeout(cmd, goapRedPrecheckTimeout)
}

// precheckGoapStaleMilestones re-runs the recorded RED command of head
// milestones that already red-passed once (RedPassStreak ≥ 1): a second pass
// completes the milestone on the spot (evidence `red-evidence-precheck:`),
// letting the cycle charge and plan the NEXT genuinely-pending milestone in
// the same slot; a failing RED kills the already-landed hypothesis (streak +
// command cleared) and the milestone proceeds to a real implementation
// attempt. The shell runs OUTSIDE the program-store flock — only the
// bookkeeping takes the lock, with the milestone re-validated under it.
func precheckGoapStaleMilestones(bb *Blackboard) {
	for i := 0; i < goapRedPrecheckMaxPerCycle; i++ {
		var programID, cmd string
		var idx int
		found := false
		if ps, err := research.OpenPrograms(goapProgramsPath); err == nil {
			if p := ps.Active(); p != nil {
				if mIdx, m := p.NextMilestone(); m != nil && m.RedPassStreak >= 1 && strings.TrimSpace(m.LastRedCmd) != "" {
					programID, idx, cmd, found = p.ID, mIdx, m.LastRedCmd, true
				}
			}
		}
		if !found {
			return
		}

		// Cheapest evidence first (2026-08-01). When the goal names a _test.go
		// deliverable that is provably absent at HEAD, the work definitively has
		// not been done — so no test run can support the already-landed
		// hypothesis, and shelling one costs a whole package suite (bounded at
		// goapRedPrecheckTimeout) every cycle to re-derive what the file system
		// answers in milliseconds. Discard the hypothesis and let the milestone
		// take a real implementation attempt.
		//
		// The ORDER is load-bearing: with this probe placed after the shell the
		// branch is unreachable in production, because recorded RED commands are
		// whole-package runs and the bare main checkout fails two environment
		// tests unconditionally, so the error branch below always returns first.
		if goapRedPassDeliverableVerdict(goapMilestoneGoalText(programID, idx)) == goapDeliverablesMissing {
			_ = research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
				ps.ResetRedPassStreak(programID, idx)
				return nil
			})
			Info("goap fusion: red pre-check hypothesis discarded — named deliverable missing at HEAD",
				"milestone", fmt.Sprintf("%s:%d", programID, idx), "red_cmd", cmd)
			return
		}

		if _, err := goapRedPrecheckRunFn(cmd); err != nil {
			if !goapRedRunProducedVerdict(err) {
				// The command produced no verdict — the shell timed out, could
				// not start, or is the inert under-test runner. No evidence
				// either way; leave the recorded red-pass state untouched.
				Info("goap fusion: red pre-check produced no verdict — evidence left untouched",
					"milestone", fmt.Sprintf("%s:%d", programID, idx), "red_cmd", cmd, "err", err)
				return
			}
			// RED still fails: the predicted regression exists, the work is
			// genuinely missing — hypothesis dead, plan it for real.
			_ = research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
				ps.ResetRedPassStreak(programID, idx)
				return nil
			})
			Info("goap fusion: red pre-check still fails — milestone needs real work",
				"milestone", fmt.Sprintf("%s:%d", programID, idx), "red_cmd", cmd)
			return
		}

		var streak int
		var completed bool
		if err := research.UpdatePrograms(goapProgramsPath, func(ps *research.ProgramStore) error {
			// Re-validate under the lock: another writer may have completed,
			// blocked, or re-evidenced the milestone while the shell ran.
			for _, p := range ps.Programs {
				if p.ID != programID {
					continue
				}
				if idx >= len(p.Milestones) || p.Milestones[idx].Status != "pending" || p.Milestones[idx].LastRedCmd != cmd {
					return nil
				}
			}
			streak = ps.RecordRedPass(programID, idx, cmd)
			if streak >= goapRedPassCompleteStreak {
				completed = ps.MarkDone(programID, idx, "red-evidence-precheck:"+bb.RunID)
				if completed {
					// This precheck runs BEFORE the cycle's own
					// ClaimActiveForCycle call, so any claim on the program
					// belongs to an EARLIER cycle's RunID, not bb.RunID —
					// ReleaseClaim's agentID match can never succeed here.
					// Clear whatever claim is present instead.
					ps.ClearClaim(programID)
				}
			}
			return nil
		}); err != nil || !completed {
			return
		}
		Info("goap fusion: milestone completed on red pre-check — recorded RED passed again at HEAD, no plan phase needed",
			"milestone", fmt.Sprintf("%s:%d", programID, idx), "streak", streak)
	}
}

func runGoapShell(command string) (string, error) {
	return runGoapShellTimeout(command, 120*time.Second)
}

// runGoapShellTimeout runs command with an explicit deadline. Callers that
// embed their own timeout (e.g. `go test -timeout 180s`) must pass an outer
// deadline LARGER than the inner one, or the shell kill preempts the tool
// and a slow-but-passing run is misreported as failure.
func runGoapShellTimeout(command string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = goapFusionRepo
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("command killed by %s shell timeout: %w", timeout, ctx.Err())
	}
	return string(out), err
}

func truncateGoap(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...<truncated>"
}

// truncateGoapTail bounds s to its last limit characters, prefixing an
// ellipsis marker when truncated. It complements truncateGoap (which keeps
// the head) for callers whose actionable content sits at the end of s — e.g.
// re-truncating a failure tail that was already tail-kept upstream.
func truncateGoapTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return "...<truncated>\n" + s[len(s)-limit:]
}

func extractSection(text, startMarker, endMarker string) string {
	start := strings.Index(text, startMarker)
	if start < 0 {
		return ""
	}
	start += len(startMarker)
	end := strings.Index(text[start:], endMarker)
	if end < 0 {
		return strings.TrimSpace(text[start:])
	}
	return strings.TrimSpace(text[start : start+end])
}

// sectionAwareGraphContext extracts a GRAPH_REPORT.md's analytical sections
// (Summary, God Nodes, Community Hubs, Surprising Connections, Low-Cohesion
// Files) instead of a raw head truncation. On a report-scale (hundreds of KB)
// document, a head truncation only ever keeps the header — every research
// prompt that grounds itself in the graph shares this extraction so the
// sections that actually matter reach the LLM.
func sectionAwareGraphContext(report string) string {
	summary := extractSection(report, "## Summary", "## Graph Freshness")
	godNodes := extractSection(report, "## God Nodes", "## Community Hubs")
	communityHubs := extractSection(report, "## Community Hubs", "## Surprising Connections")
	surprising := extractSection(report, "## Surprising Connections", "## Low-Cohesion Files")
	lowCohesion := extractSection(report, "## Low-Cohesion Files", "## Isolated Nodes")

	extracted := fmt.Sprintf("### Summary\n%s\n\n### God Nodes\n%s\n\n### Community Hubs (first 20)\n%s",
		summary, godNodes, truncateGoap(communityHubs, 3000))
	if surprising != "" {
		extracted += fmt.Sprintf("\n\n### Surprising Connections\n%s", truncateGoap(surprising, 2000))
	}
	if lowCohesion != "" {
		extracted += fmt.Sprintf("\n\n### Low-Cohesion Files\n%s", truncateGoap(lowCohesion, 2000))
	}
	return extracted
}

// graphReportBuiltCommit extracts the short SHA from a GRAPH_REPORT.md's
// "## Graph Freshness" section line ("- Built from commit: `<sha>`"). Returns
// "" if the report has no such line — the real freshness signal GraphIsFresh
// compares against goapFusionRepo's current HEAD, instead of merely checking
// that the report file exists on disk.
func graphReportBuiltCommit(report string) string {
	const marker = "Built from commit: `"
	idx := strings.Index(report, marker)
	if idx < 0 {
		return ""
	}
	rest := report[idx+len(marker):]
	end := strings.Index(rest, "`")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// readSectionAwareGraphContext reads goapFusionGraphReport and extracts its
// analytical sections via sectionAwareGraphContext. Callers get an empty
// string on a read error, same as the prior raw-read-then-truncate callers.
func readSectionAwareGraphContext() string {
	b, err := os.ReadFile(goapFusionGraphReport)
	if err != nil {
		return ""
	}
	return sectionAwareGraphContext(string(b))
}

func buildGoapFusionNotebookLMQuery(task, graphReport string) string {
	return fmt.Sprintf(`You are grounding an autonomous GOAP fusion code-improvement cycle in the BT Platform Research notebook.

Task: %s

Current graphify/codebase context:
%s

%s%s
Return EXACTLY this format, with up to THREE ranked implementation targets and citations in the text where possible:
GOAL1: <the highest-impact concrete code change the next automated Superpowers/Claude run should implement>
GAP1: <why the current go-bt-evolve codebase needs it>
FILES1: <repo-relative Go files/packages to change>
GOAL2/GAP2/FILES2 and GOAL3/GAP3/FILES3: <optional further independent targets>
TESTS: <specific Go tests/build commands to verify them>
CITATIONS: <NotebookLM citation numbers or source ids>

Prefer SUBSTANTIAL goals: a goal that adds a real capability (a new node
type, a new coordination primitive, an evaluation metric, a persistence
layer) is worth more than a one-line tweak. A downstream planner will
decompose each substantial goal into several TDD tasks, so a goal spanning
2-4 related files is ideal, not too large.

If the single highest-impact change is too large even for one multi-task run, return INSTEAD a program:
PROGRAM: <title of the multi-cycle change>
MILESTONE1: <first self-contained milestone, naming the repo-relative Go files it touches>
MILESTONE2..MILESTONE5: <further milestones, each independently verifiable>

Rules:
- Prefer implementation work over documentation.
- Each goal must be scoped to the named files/packages; multi-file and multi-package changes are welcome and preferred over trivial single-line edits.
- Prefer one coherent larger change over several trivial ones.
- Do not repeat these stale goals unless you have a new concrete variant: "Unblock engine tests" or "Ensure all domain trees have smoke tests".
- If no new research-backed implementation exists, still provide the best code-level next step from notebook evidence.`,
		task, graphReport, implementedGoalsPromptBlock(), graphifyComponentsPromptBlock(task))
}

// implementedGoalsPromptBlock renders the "already done" list injected into
// research prompts so cycles do not re-propose landed work.
func implementedGoalsPromptBlock() string {
	done := recentImplementedGoals(10)
	if len(done) == 0 {
		return ""
	}
	return "\nAlready implemented recently — do NOT re-propose these:\n- " + strings.Join(done, "\n- ") + "\n"
}

func isGoapNotebookLMFailure(out string) bool {
	lower := strings.ToLower(out)
	// nlm prints CLI-level errors as plain "Error: ..." text instead of the
	// JSON answer envelope; successful answers never start with that prefix.
	if strings.HasPrefix(strings.TrimSpace(lower), "error:") {
		return true
	}
	failureMarkers := []string{
		"authentication expired",
		"authentication failed",
		"notebooklm circuit breaker open",
		"query failed",
		"auth_status\":\"stale",
		"not_configured",
		"nlm error:",
		"resource_exhausted",
		"google rejected the query",
		"api error (code",
		// Local daily-budget denial (nlm_quota.go): deliberately NOT
		// error-prefixed (an expected skip, and "Error:" tripped the generic
		// output-quality gate when embedded in reports), but a budget-denied
		// query still produced no answer — the goap research path must route
		// to its Claude fallback exactly as before.
		"budget exhausted",
	}
	for _, marker := range failureMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func extractNotebookLMAnswer(out string) string {
	var payload struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err == nil && strings.TrimSpace(payload.Answer) != "" {
		return strings.TrimSpace(payload.Answer)
	}
	if answer := extractJSONStringField(out, "answer"); strings.TrimSpace(answer) != "" {
		return strings.TrimSpace(answer)
	}
	return strings.TrimSpace(out)
}

func extractJSONStringField(out, field string) string {
	marker := `"` + field + `"`
	idx := strings.Index(out, marker)
	if idx < 0 {
		return ""
	}
	rest := out[idx+len(marker):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	end := 1
	for end < len(rest) {
		if rest[end] == '\\' {
			end += 2
			continue
		}
		if rest[end] == '"' {
			quoted := rest[:end+1]
			if unquoted, err := strconv.Unquote(quoted); err == nil {
				return unquoted
			}
			return strings.Trim(quoted, `"`)
		}
		end++
	}
	return strings.Trim(rest, `"`)
}

func extractGoapNotebookLMRecommendation(answer string) (goal, gap string) {
	for _, line := range strings.Split(answer, "\n") {
		trimmed := strings.TrimSpace(strings.Trim(line, "-*• 	"))
		trimmed = strings.TrimSpace(strings.ReplaceAll(trimmed, "**", ""))
		upper := strings.ToUpper(trimmed)
		switch {
		case strings.HasPrefix(upper, "GOAL:"):
			goal = strings.Trim(strings.TrimSpace(trimmed[len("GOAL:"):]), `"`)
		case strings.HasPrefix(upper, "GAP:"):
			gap = strings.Trim(strings.TrimSpace(trimmed[len("GAP:"):]), `"`)
		}
	}
	return goal, gap
}

// goapFusionHasEngineTestBlocker reports whether the gap text describes a
// genuine build/test blocker (an import cycle that actually breaks compilation,
// tests that fail to compile) rather than an incidental mention of the phrase
// "import cycle" in a boundary design note. The bare-substring predecessor
// fabricated an un-implementable P0 "Unblock engine tests" goal from avoidance
// notes ("avoid an import cycle", "engine cannot import domains — import
// cycle"), dead-lettering the loop. Matching is case-insensitive.
func goapFusionHasEngineTestBlocker(gaps string) bool {
	lower := strings.ToLower(gaps)
	// Affirmative blocker phrasings: the cycle/blocker is stated as active.
	blockerPhrases := []string{
		"import cycle blocks",
		"import cycle breaks",
		"import cycles block test compilation",
		"blocks test compilation",
		"breaks test compilation",
		"tests fail to compile",
		"test compilation fails",
		"fails to compile",
		"cannot run tests",
		"tests do not compile",
	}
	for _, p := range blockerPhrases {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func goapFusionNotebookLMGoalFromGaps(gaps string) string {
	for _, line := range strings.Split(gaps, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "NOTEBOOKLM_GOAL:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "NOTEBOOKLM_GOAL:"))
		}
	}
	return ""
}

func firstNonEmptyGoapLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "-*• "))
		if line != "" && !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "}") {
			return line
		}
	}
	return ""
}

// extractGoapGoals extracts goal lines (starting with [P0], [P1], or [P2]) from
// a goap-fusion analysis file for comparison with the current run.
func extractGoapGoals(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[P0]") || strings.HasPrefix(trimmed, "[P1]") || strings.HasPrefix(trimmed, "[P2]") {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}

func extractConversationID(out string) string {
	var payload struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err == nil && payload.ConversationID != "" {
		return payload.ConversationID
	}
	return extractJSONStringField(out, "conversation_id")
}

// buildGrillRound1Query opens the grill conversation: a brutal gap review of
// the platform, judged against the arc42 quality goals so "missing" means
// "missing for what the platform is documented to be good at", not missing
// relative to whatever the research corpus happens to discuss. reuseTopic
// seeds the graphify components block that answers the grill's own "What
// existing platform components can we leverage?" question; empty is fine —
// the block degrades to nothing.
func buildGrillRound1Query(graphSnippet, reuseTopic string) string {
	return fmt.Sprintf(`You are a critical reviewer / coach grilling the go-bt-evolve behavior tree agent platform team.

		Current codebase structure (from graphify):
		%s

		%s%s
		Your job: Be brutally honest. What is this BT framework MISSING to achieve the quality goals above?

		For EACH gap you identify, push hard:
		- Which quality goal does closing it advance?
		- What specifically must be built?
		- How do we measure success?
		- What is the concrete implementation — exact tree types, nodes, metrics?
		- What existing platform components can we leverage?
		- What's the minimum viable fix vs the full solution?

		Prioritize ruthlessly — which 2 gaps must be addressed first?

		Rules:
		- No vague advice. Demand exact tree types, node names, metric thresholds.
		- Prefer implementation work over documentation.
		- Return in format: GAP n: <gap> | GOAL: <arc42 quality goal advanced> | FIX: <concrete fix> | FILES: <likely files> | TESTS: <test commands>`,
		graphSnippet, arc42GoalsPromptBlock(), graphifyComponentsPromptBlock(reuseTopic))
}
