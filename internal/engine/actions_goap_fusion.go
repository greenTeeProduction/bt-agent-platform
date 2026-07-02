package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
	"github.com/nico/go-bt-evolve/internal/util"

	btcore "github.com/rvitorper/go-bt/core"
)

const (
	goapFusionVaultDir     = "/mnt/ssd/clawd/wiki/bt-research"
	goapFusionSynthesesDir = "/mnt/ssd/clawd/wiki/bt-research/syntheses"
	goapFusionPlansDir     = "/mnt/ssd/clawd/wiki/bt-research/plans"
	goapFusionGraphReport  = "/home/nico/go-bt-evolve/graphify-out/GRAPH_REPORT.md"
	goapFusionRepo         = "/home/nico/go-bt-evolve"
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
	// Activity-Progress Confusion failure mode.
	goapFusionMaxLoopIterations = 50
)

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
		graphBytes, _ := os.ReadFile(goapFusionGraphReport)
		query := buildGoapFusionNotebookLMQuery(bb.Task, truncateGoap(string(graphBytes), 3500))
		out := nlmRun(180*time.Second, "notebook", "query", defaultNotebook, query)
		if isGoapNotebookLMFailure(out) {
			if isGoapNotebookLMQuotaError(out) {
				saveNlmQuotaExhausted(bb, time.Now())
			}
			setGoapState(bb, "notebooklm_skip_reason", truncateGoap(out, 2000))
			bb.Result = fmt.Sprintf("## GOAP NotebookLM Research Failed\n\nNotebookLM query failed or auth is unavailable; refusing to proceed from stale vault research.\n\n```\n%s\n```", truncateGoap(out, 2000))
			bb.Outcome = "goap_fusion_notebooklm_failed"
			return -1
		}

		answer := extractNotebookLMAnswer(out)
		goal, gap := extractGoapNotebookLMRecommendation(answer)
		if goal == "" {
			goal = firstNonEmptyGoapLine(answer)
		}
		if gap == "" {
			gap = "NotebookLM produced a cited recommendation for BT platform improvement; see raw answer."
		}
		if goal == "" {
			bb.Result = "## GOAP NotebookLM Research Failed\n\nNotebookLM returned no parseable recommendation."
			bb.Outcome = "goap_fusion_notebooklm_failed"
			return -1
		}

		ts := time.Now().Format("2006-01-02T150405")
		path := filepath.Join(goapFusionSynthesesDir, fmt.Sprintf("goap-fusion-notebooklm-%s.md", ts))
		report := fmt.Sprintf("# GOAP Fusion NotebookLM Research — %s\n\n## Notebook\n`%s`\n\n## Recommendation\nGOAL: %s\nGAP: %s\n\n## Raw NotebookLM Answer\n%s\n", ts, defaultNotebook, goal, gap, answer)
		if err := writeString(path, report); err != nil {
			bb.Result = fmt.Sprintf("## GOAP NotebookLM Research Failed\n\nCould not write `%s`: %v", path, err)
			bb.Outcome = "goap_fusion_notebooklm_failed"
			return -1
		}

		setGoapState(bb, "notebooklm_research", report)
		setGoapState(bb, "notebooklm_goal", goal)
		setGoapState(bb, "notebooklm_gap", gap)
		setGoapState(bb, "notebooklm_research_path", path)
		bb.Result = fmt.Sprintf("## GOAP NotebookLM Research Complete\n\nPath: `%s`\n\nGOAL: %s\n\nGAP: %s", path, goal, gap)
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
		report := string(b)
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

		// Add the GOAP-owned NotebookLM recommendation before static graph checks so
		// research-backed improvements are not starved by stale heuristic P0/P2 goals.
		if nlmGoal, _ := bb.ChainState["goap_fusion_notebooklm_goal"].(string); strings.TrimSpace(nlmGoal) != "" {
			nlmGap, _ := bb.ChainState["goap_fusion_notebooklm_gap"].(string)
			if strings.TrimSpace(nlmGap) == "" {
				nlmGap = "NotebookLM recommended this implementation target."
			}
			gaps = append(gaps,
				"NOTEBOOKLM_GOAL: "+strings.TrimSpace(nlmGoal),
				"NOTEBOOKLM_GAP: "+strings.TrimSpace(nlmGap))
		}

		// Check for domain coverage gaps
		if strings.Contains(graphReport, "AllDomainTrees") {
			gaps = append(gaps, "CHECK: AllDomainTrees coverage — verify all registered trees have smoke tests and descriptions")
		}

		// Check for testability gaps
		if strings.Contains(graphReport, "test") && !strings.Contains(graphReport, "engine test") {
			gaps = append(gaps, "CHECK: Engine tests executable — verify no import cycles block test compilation")
		}

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

		// P0: Verifiable correctness (test blockers, build failures)
		// P1: New capability (domain tree, condition node, action)
		// P2: Quality improvement (coverage, refactoring)

		if nlmGoal := goapFusionNotebookLMGoalFromGaps(gapsStr); nlmGoal != "" {
			goals = append(goals, "[P0] NotebookLM research: "+nlmGoal)
		}

		if strings.Contains(gapsStr, "import cycle") || strings.Contains(gapsStr, "test compilation") {
			goals = append(goals, "[P0] Unblock engine tests — fix import cycle or test blockers preventing test execution")
		}

		if strings.Contains(gapsStr, "LLM-supervised") || strings.Contains(gapsStr, "meta-controller") {
			goals = append(goals, "[P1] Add LLM-supervised population dynamics to gardener — dynamic mutation rate adjustment")
		}

		if strings.Contains(gapsStr, "Auction-based") {
			goals = append(goals, "[P1] Implement auction-based task allocation for A2A agent coordination")
		}

		if strings.Contains(gapsStr, "MetaClaw") || strings.Contains(gapsStr, "skill library") {
			goals = append(goals, "[P1] Build failure-to-skill pipeline: extract BT mutations from agent failures into skills")
		}

		if strings.Contains(gapsStr, "Code-BT") {
			goals = append(goals, "[P2] Prototype code-driven BT generation: LLM generates Go code → compiled to executable BT")
		}

		if strings.Contains(gapsStr, "typed-edge") {
			goals = append(goals, "[P2] Add typed-edge validation to tree generation: guard/effect/recovery/approval semantics")
		}

		if strings.Contains(gapsStr, "Checkpoint verification") {
			goals = append(goals, "[P2] Extend checkpoint verification to all domain trees: deterministic postcondition checks")
		}

		if strings.Contains(gapsStr, "AllDomainTrees") || strings.Contains(gapsStr, "domain coverage") {
			goals = append(goals, "[P2] Ensure all domain trees have smoke tests, descriptions, and condition coverage")
		}

		if len(goals) == 0 {
			goals = append(goals, "[P2] Review and improve condition node routing coverage across all domain trees")
		}

		currentGoals := strings.Join(goals, "\n")
		// Compare with previous run — skip if identical (no new gaps)
		latestPath := filepath.Join(goapFusionVaultDir, "goap-fusion-latest.md")
		if b, err := os.ReadFile(latestPath); err == nil {
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
		graphBytes, _ := os.ReadFile(goapFusionGraphReport)
		graphSnippet := truncateGoap(string(graphBytes), 3500)

		// Read grill round tracking from the agent-scope blackboard — it must
		// survive across scheduled runs (ChainState dies with each run).
		grillRound, conversationID := loadGrillState(bb)

		// Build round-specific query
		var query string
		switch grillRound {
		case 1:
			query = fmt.Sprintf(`You are a critical reviewer / coach grilling the go-bt-evolve behavior tree agent platform team.

		Current codebase structure (from graphify):
		%s

		Your job: Be brutally honest. What is this BT framework MISSING?

		For EACH gap you identify, push hard:
		- What specifically must be built?
		- How do we measure success?
		- What is the concrete implementation — exact tree types, nodes, metrics?
		- What existing platform components can we leverage?
		- What's the minimum viable fix vs the full solution?

		Prioritize ruthlessly — which 2 gaps must be addressed first?

		Rules:
		- No vague advice. Demand exact tree types, node names, metric thresholds.
		- Prefer implementation work over documentation.
		- Return in format: GAP n: <gap> | FIX: <concrete fix> | FILES: <likely files> | TESTS: <test commands>`, graphSnippet)
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

		// Extract implementation targets from grill answer
		goal, gap := extractGoapNotebookLMRecommendation(answer)
		if goal != "" {
			setGoapState(bb, "notebooklm_goal", goal)
			setGoapState(bb, "notebooklm_gap", gap)
		}

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
	if len(docs) > maxFiles {
		docs = docs[:maxFiles]
	}
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		b, rerr := os.ReadFile(filepath.Join(dir, d.name))
		if rerr != nil {
			continue
		}
		out = append(out, fmt.Sprintf("=== %s: %s ===\n%s", label, d.name, truncateGoap(string(b), perFileLimit)))
	}
	return out
}

func setGoapState(bb *Blackboard, key, value string) {
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["goap_fusion_"+key] = value
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

func buildGoapFusionNotebookLMQuery(task, graphReport string) string {
	return fmt.Sprintf(`You are grounding an autonomous GOAP fusion code-improvement cycle in the BT Platform Research notebook.

Task: %s

Current graphify/codebase context:
%s

Return EXACTLY this format, with one concrete implementation target and citations in the text where possible:
GOAL: <one specific code change the next automated Superpowers/Claude run should implement>
GAP: <why the current go-bt-evolve codebase needs it>
FILES: <likely files or packages to inspect/change>
TESTS: <specific Go tests/build commands to verify it>
CITATIONS: <NotebookLM citation numbers or source ids>

Rules:
- Prefer implementation work over documentation.
- Do not repeat these stale goals unless you have a new concrete variant: "Unblock engine tests" or "Ensure all domain trees have smoke tests".
- The goal must be small enough for one scheduled coding run.
- If no new research-backed implementation exists, still provide the best code-level next step from notebook evidence.`, task, graphReport)
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
