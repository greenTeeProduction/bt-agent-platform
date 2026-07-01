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

	"github.com/nico/go-bt-evolve/internal/util"

	btcore "github.com/rvitorper/go-bt/core"
)

const (
	goapFusionVaultDir      = "/mnt/ssd/clawd/wiki/bt-research"
	goapFusionSynthesesDir  = "/mnt/ssd/clawd/wiki/bt-research/syntheses"
	goapFusionPlansDir      = "/mnt/ssd/clawd/wiki/bt-research/plans"
	goapFusionGraphReport   = "/home/nico/go-bt-evolve/graphify-out/GRAPH_REPORT.md"
	goapFusionRepo          = "/home/nico/go-bt-evolve"
	goapFusionClaudeBin     = "/home/nico/.local/bin/claude"
	goapFusionGoBin         = "/usr/local/go/bin/go"
	goapFusionGraphifyTool  = "graphify"
	goapFusionClaudeTimeout = 3600 // seconds (1 hour)

	// goapFusionRejectedLedger is the persistent corpus of known rejected unsafe
	// contexts (the rejected-context ledger) the continuous self-improving loop
	// runner replays against every new candidate to enforce the Monotonicity
	// Invariant of the Experience-Grounded Monotonicity Auditor — no mutation or
	// self-evolution edit may re-admit a previously rejected unsafe context.
	goapFusionRejectedLedger = "/mnt/ssd/clawd/wiki/bt-research/rejected-context-ledger.jsonl"
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
	RegisterCondition("IsResearchOrGapRequest", func(bb *Blackboard) bool {
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"research", "gap", "analyze", "plan", "assess", "review", "scan",
			"audit", "evaluate", "survey", "study", "compare")
	})
	RegisterCondition("IsApplyRequest", func(bb *Blackboard) bool {
		return isGoapFusionApplyRequest(bb.Task)
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
		graphBytes, _ := os.ReadFile(goapFusionGraphReport)
		query := buildGoapFusionNotebookLMQuery(bb.Task, truncateGoap(string(graphBytes), 3500))
		out := nlmRun(180*time.Second, "notebook", "query", defaultNotebook, query)
		if isGoapNotebookLMFailure(out) {
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

	// ReadVaultResearch reads all NotebookLM research syntheses, evolution reports,
	// and improvement plans from the Obsidian vault into the blackboard.
	RegisterAction("ReadVaultResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var sources []string

		// Read syntheses (newest first)
		if entries, err := os.ReadDir(goapFusionSynthesesDir); err == nil {
			sort.Slice(entries, func(i, j int) bool {
				ii, _ := entries[i].Info()
				jj, _ := entries[j].Info()
				if ii == nil || jj == nil {
					return false
				}
				return ii.ModTime().After(jj.ModTime())
			})
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				path := filepath.Join(goapFusionSynthesesDir, e.Name())
				if b, err := os.ReadFile(path); err == nil {
					sources = append(sources, fmt.Sprintf("=== Synthesis: %s ===\n%s",
						e.Name(), truncateGoap(string(b), 2000)))
				}
			}
		}

		// Read evolution reports
		if entries, err := os.ReadDir(goapFusionVaultDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasPrefix(e.Name(), "bt-evolution-") {
					continue
				}
				path := filepath.Join(goapFusionVaultDir, e.Name())
				if b, err := os.ReadFile(path); err == nil {
					sources = append(sources, fmt.Sprintf("=== Evolution Report: %s ===\n%s",
						e.Name(), truncateGoap(string(b), 3000)))
				}
			}
		}

		// Read plans
		if entries, err := os.ReadDir(goapFusionPlansDir); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				path := filepath.Join(goapFusionPlansDir, e.Name())
				if b, err := os.ReadFile(path); err == nil {
					sources = append(sources, fmt.Sprintf("=== Plan: %s ===\n%s",
						e.Name(), truncateGoap(string(b), 2000)))
				}
			}
		}

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
			gaps = append(gaps, "NOTEBOOKLM_GOAL: "+strings.TrimSpace(nlmGoal))
			gaps = append(gaps, "NOTEBOOKLM_GAP: "+strings.TrimSpace(nlmGap))
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

	// ReadImprovementPlan reads the highest-priority improvement plan from the vault.
	RegisterAction("ReadImprovementPlan", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		goalsStr, _ := bb.ChainState["goap_fusion_goal_queue"].(string)

		planLines := []string{}
		planLines = append(planLines, fmt.Sprintf("## Improvement Plan\n\nBased on GOAP goal analysis:\n\n%s\n\n---\n\n", goalsStr))

		// Read specific plans from vault
		if entries, err := os.ReadDir(goapFusionPlansDir); err == nil {
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() > entries[j].Name() // newest first by filename date
			})
			for i, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
					continue
				}
				if i >= 3 {
					break // only most recent 3 plans
				}
				path := filepath.Join(goapFusionPlansDir, e.Name())
				if b, err := os.ReadFile(path); err == nil {
					planLines = append(planLines, fmt.Sprintf("### Plan: %s\n\n%s", e.Name(), truncateGoap(string(b), 1500)))
				}
			}
		}

		planLines = append(planLines, "\n---\n## Implementation Directive\n\nPick the HIGHEST priority goal above and implement exactly ONE improvement. Rules:\n1. Read source files before editing\n2. One focused change per cycle\n3. Build + test must pass\n4. Commit with descriptive message")
		plan := strings.Join(planLines, "\n\n")
		setGoapState(bb, "active_plan", plan)
		bb.Result = plan
		return 1
	})

	// ApplyImprovementWithClaude launches Claude Code to implement the highest-priority
	// improvement from the GOAP goal queue. Claude gets full context from the vault
	// research, graphify report, gap analysis, and goal queue.
	//
	// Production hardening (June 2026):
	//   - Preflight: auto-stash dirty files, reset graphify-out, checkout master
	//   - After Claude: detect uncommitted edits, verify build/tests, graphify-out filter
	//   - On failure: git reset --hard to before_head, save failed patch
	//   - Requires HITL approval (HumanApprovalGate in tree) before reaching this point
	RegisterAction("ApplyImprovementWithClaude", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		gapsStr, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
		goalsStr, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
		planStr, _ := bb.ChainState["goap_fusion_active_plan"].(string)
		vaultStr, _ := bb.ChainState["goap_fusion_vault_research"].(string)
		graphStr, _ := bb.ChainState["goap_fusion_graphify_report"].(string)

		// ── Preflight: clean worktree ──
		// Reset graphify-out (generated noise)
		if out, err := runGoapShell("git checkout -- graphify-out/"); err != nil {
			bb.Result = fmt.Sprintf("## Preflight Failed\n\nCould not reset graphify-out:\n%s", out)
			bb.Outcome = "goap_fusion_preflight_failed"
			return -1
		}
		// Auto-stash any dirty non-graphify files
		dirtyStatus, _ := runGoapShell("git status --short --untracked-files=all")
		if hasNonGraphifyDirty(dirtyStatus) {
			stashCmd := fmt.Sprintf("git stash push --include-untracked -m 'goap-fusion auto-stash %d'", time.Now().Unix())
			if _, err := runGoapShell(stashCmd); err != nil {
				bb.Result = fmt.Sprintf("## Preflight Failed\n\nCould not stash dirty worktree:\n%s", dirtyStatus)
				bb.Outcome = "goap_fusion_preflight_failed"
				return -1
			}
		}
		// Ensure on master
		if out, err := runGoapShell("git checkout master"); err != nil {
			bb.Result = fmt.Sprintf("## Preflight Failed\n\nCould not checkout master:\n%s", out)
			bb.Outcome = "goap_fusion_preflight_failed"
			return -1
		}
		// Sync with origin before making local changes. This is strict for unattended
		// runs: if master cannot be fast-forwarded from origin/master, do not let
		// Claude implement on stale or divergent local code.
		if fetchOut, fetchErr := runGoapShell("git fetch origin"); fetchErr != nil {
			bb.Result = fmt.Sprintf("## Preflight Failed\n\nCould not fetch origin before GOAP implementation:\n%s", truncateGoap(fetchOut, 1000))
			bb.Outcome = "goap_fusion_preflight_failed"
			return -1
		}
		if pullOut, pullErr := runGoapShell("git pull origin master --ff-only"); pullErr != nil {
			bb.Result = fmt.Sprintf("## Preflight Failed\n\nLocal master is not safely up to date with origin/master. Refusing to run implementation on stale or divergent code.\n\n```\n%s\n```", truncateGoap(pullOut, 1000))
			bb.Outcome = "goap_fusion_preflight_failed"
			return -1
		}

		beforeHead, _ := runGoapShell("git rev-parse HEAD")
		beforeHead = strings.TrimSpace(beforeHead)

		prompt := buildClaudeFusionPrompt(bb.Task, gapsStr, goalsStr, planStr,
			truncateGoap(vaultStr, 5000), truncateGoap(graphStr, 5000))

		startTime := time.Now()
		claudeOut, err := runClaudeCode(prompt)
		elapsed := time.Since(startTime)

		setGoapState(bb, "claude_elapsed", elapsed.String())
		setGoapState(bb, "claude_output", lastChars(strings.TrimSpace(claudeOut), 8000))

		if err != nil {
			// Claude failed — reset to clean state
			resetToHead(beforeHead)
			restoreGoapStash()
			bb.Result = fmt.Sprintf("## Claude Code Failed\n\nError: %v\n\nPartial output:\n%s\n\nElapsed: %s\n\nWorktree reset to %s",
				err, lastChars(strings.TrimSpace(claudeOut), 3000), elapsed, beforeHead[:12])
			bb.Outcome = "goap_fusion_claude_failed"
			return -1
		}

		// ── Post-Claude verification ──
		// Detect uncommitted edits (Claude ran but didn't commit)
		dirtyAfter, _ := runGoapShell("git status --short --untracked-files=all")
		changedFiles := realChangedFilesGoap("master")
		if len(changedFiles) == 0 && hasNonGraphifyDirty(dirtyAfter) {
			// Claude edited files but didn't commit — save patch, reset
			patchOut, _ := runGoapShell("git diff --binary")
			savePath := filepath.Join(goapFusionPlansDir, fmt.Sprintf("failed-claude-%s.patch",
				time.Now().Format("20060102T150405")))
			_ = os.WriteFile(savePath, []byte(patchOut), 0644)
			resetToHead(beforeHead)
			restoreGoapStash()
			bb.Result = fmt.Sprintf("## Claude Code Incomplete\n\nClaude edited files but did not commit. Saved patch to %s\nDirty files: %s",
				savePath, dirtyAfter)
			bb.Outcome = "goap_fusion_uncommitted"
			return -1
		}

		// Apply changes: cherry-pick Claude's commits to master
		var logOut string
		if len(changedFiles) > 0 {
			// Already on master; Claude committed on its branch? Actually Claude
			// runs in the main worktree with --add-dir. Check for commits.
			logOut, _ = runGoapShell("git diff --stat master..HEAD")
			gitDiff, _ := runGoapShell("git diff --stat -- . ':!graphify-out/'")

			// Run verification
			buildOut, buildErr := runGoapShell("/usr/local/go/bin/go build ./...")
			if buildErr != nil {
				resetToHead(beforeHead)
				restoreGoapStash()
				bb.Result = fmt.Sprintf("## Verification Failed\n\ngo build ./... failed after Claude changes:\n%s\n\nReset to %s",
					truncateGoap(buildOut, 3000), beforeHead[:12])
				bb.Outcome = "goap_fusion_verify_failed"
				return -1
			}

			// Run targeted tests on changed packages
			pkgs := changedGoapPackages(changedFiles)
			for _, pkg := range pkgs {
				testOut, testErr := runGoapShell(fmt.Sprintf("/usr/local/go/bin/go test %s -count=1 -timeout 120s", pkg))
				if testErr != nil {
					resetToHead(beforeHead)
					restoreGoapStash()
					bb.Result = fmt.Sprintf("## Verification Failed\n\ngo test %s failed:\n%s\n\nReset to %s",
						pkg, truncateGoap(testOut, 3000), beforeHead[:12])
					bb.Outcome = "goap_fusion_verify_failed"
					return -1
				}
			}

			bb.Result = fmt.Sprintf("## Claude Code Improvement Applied\n\nElapsed: %s\nChanges:\n```\n%s\n```\n\nBuild: PASSED\nTests: PASSED\n\nClaude output:\n%s",
				elapsed, strings.TrimSpace(gitDiff),
				truncateGoap(strings.TrimSpace(claudeOut), 3000))
			// Push to origin — non-fatal if network/auth fails
			if pushOut, pushErr := runGoapShell("git push origin master"); pushErr != nil {
				setGoapState(bb, "git_push_warning", fmt.Sprintf("git push origin master failed: %s", truncateGoap(pushOut, 500)))
			}
		} else {
			// Claude ran but produced no code changes
			bb.Result = fmt.Sprintf("## Claude Code Research-Only\n\nElapsed: %s\nNo code changes produced. Research output:\n%s",
				elapsed, truncateGoap(strings.TrimSpace(claudeOut), 3000))
		}

		restoreGoapStash()
		setGoapState(bb, "claude_result", bb.Result)
		bb.Result += fmt.Sprintf("\n\nLog commit:\n```\n%s\n```", strings.TrimSpace(logOut))
		return 1
	})

	// VerifyGoapBuild runs go test on changed packages and go build ./...
	RegisterAction("VerifyGoapBuild", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var results []string

		buildCmd := fmt.Sprintf("cd %s && /usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli", goapFusionRepo)
		if out, err := runGoapShell(buildCmd); err != nil {
			results = append(results, fmt.Sprintf("BUILD FAILED:\n%s", out))
			setGoapState(bb, "verify_result", strings.Join(results, "\n"))
			bb.Result = fmt.Sprintf("## Verification Failed\n\n%s", strings.Join(results, "\n"))
			bb.Outcome = "goap_fusion_verify_failed"
			return -1
		}
		results = append(results, "go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED")

		testCmd := fmt.Sprintf("cd %s && /usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestGoapFusion_Structure|TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestValidateOutputQuality' -timeout 180s", goapFusionRepo)
		if out, err := runGoapShell(testCmd); err != nil {
			results = append(results, fmt.Sprintf("TEST FAILED:\n%s", truncateGoap(out, 3000)))
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
		graphBytes, _ := os.ReadFile(goapFusionGraphReport)
		graphSnippet := truncateGoap(string(graphBytes), 3500)

		// Read grill round tracking from ChainState (survives across loop iterations)
		grillRound := 1
		if r, ok := bb.ChainState["goap_fusion_grill_round"].(float64); ok {
			grillRound = int(r)
		} else if r, ok := bb.ChainState["goap_fusion_grill_round"].(int); ok {
			grillRound = r
		}
		conversationID, _ := bb.ChainState["goap_fusion_grill_conversation_id"].(string)

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
			// If grill fails, fall back to single-shot research path
			bb.Result = fmt.Sprintf("## GrillMe Round %d Failed\n\nNotebookLM query failed; falling back to single-shot research.\n\n```\n%s\n```", grillRound, truncateGoap(out, 2000))
			bb.Outcome = "goap_fusion_grill_failed"
			return 0 // non-fatal — let the pipeline continue with single-shot
		}

		answer := extractNotebookLMAnswer(out)
		newConvID := extractConversationID(out)
		if newConvID != "" {
			conversationID = newConvID
		}

		// Save grill state for next iteration
		setGoapState(bb, "grill_conversation_id", conversationID)
		if grillRound >= 3 {
			// Reset to round 1 for next loop iteration
			setGoapState(bb, "grill_round", "1")
		} else {
			setGoapState(bb, "grill_round", strconv.Itoa(grillRound+1))
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

func setGoapState(bb *Blackboard, key, value string) {
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["goap_fusion_"+key] = value
}

func runGoapShell(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = goapFusionRepo
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func truncateGoap(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...<truncated>"
}

func buildClaudeFusionPrompt(task, gaps, goals, plan, vault, graph string) string {
	return fmt.Sprintf(`You are improving the go-bt-evolve behavior tree agent platform.

WORKING DIRECTORY: %s
Go is at: /usr/local/go/bin/go

Before reading source files, read graphify-out/GRAPH_REPORT.md for the codebase map.

CONTEXT FROM GOAP FUSION ANALYSIS:

TASK: %s

GAP ANALYSIS:
%s

GOAL QUEUE:
%s

IMPLEMENTATION PLAN:
%s

VAULT RESEARCH HIGHLIGHTS:
%s

GRAPHIFY CODEBASE STRUCTURE:
%s

YOUR TASK: Implement the HIGHEST-PRIORITY improvement from the goal queue above.
Rules:
- One focused change per run (1-3 files max).
- Read source files before editing them.
- Do NOT modify evaluator/, gardener/, secrets, configs, graphify-out/.
- Do NOT remove existing functionality.
- Build must pass: go build ./...
- Run tests: go test <changed package> -count=1 -timeout 120s
- Commit: git add <files> && git commit -m "improve: <area> — <what changed>"
- If pre-commit blocks on go-test/gofmt hook only: SKIP=go-test,gofmt git commit -m ...

OUTPUT FORMAT:
FILES_CHANGED: ...
WHAT_YOU_DID: ...
TESTS_RUN: ...
CONFIDENCE: high|medium|low

Stop after ONE improvement.`,
		goapFusionRepo, task, gaps, goals, plan, vault, graph)
}

func runClaudeCode(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(goapFusionClaudeTimeout)*time.Second)
	defer cancel()

	model := os.Getenv("BT_SUPERPOWERS_CLAUDE_MODEL")
	args := []string{goapFusionClaudeBin, "--print",
		"--allowedTools", "Bash(git diff:git log:git status:go test:go build:go vet:*),Read,Write,Edit,Glob,Grep",
		"--add-dir", goapFusionRepo,
		"-p", prompt,
	}
	if model != "" {
		args = append([]string{args[0], "--model", model}, args[1:]...)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = goapFusionRepo
	cmd.Env = append(os.Environ(),
		"PATH=/usr/local/go/bin:"+os.Getenv("HOME")+"/go/bin:"+os.Getenv("PATH"),
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("claude code failed: %w\noutput: %s", err, lastChars(string(out), 2000))
	}
	return string(out), nil
}

func lastChars(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ── Production hardening helpers ──

func isGraphifyOutPath(p string) bool {
	return strings.HasPrefix(p, "graphify-out/")
}

func hasNonGraphifyDirty(status string) bool {
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 3 {
			continue
		}
		path := line[3:]
		if !isGraphifyOutPath(path) {
			return true
		}
	}
	return false
}

func realChangedFilesGoap(baseBranch string) []string {
	out, err := runGoapShell(fmt.Sprintf("git diff %s --name-only -- . ':!graphify-out/'", baseBranch))
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
		f = strings.TrimSpace(f)
		if f != "" && !isGraphifyOutPath(f) {
			files = append(files, f)
		}
	}
	return files
}

func changedGoapPackages(files []string) []string {
	seen := map[string]bool{}
	var pkgs []string
	for _, p := range files {
		if !strings.HasSuffix(p, ".go") {
			continue
		}
		dir := filepath.Dir(p)
		if dir == "." {
			if !seen["."] {
				pkgs = append(pkgs, ".")
				seen["."] = true
			}
		} else {
			pkg := "./" + dir
			if !seen[pkg] {
				pkgs = append(pkgs, pkg)
				seen[pkg] = true
			}
		}
	}
	return pkgs
}

func resetToHead(ref string) {
	if ref == "" {
		return
	}
	_, _ = runGoapShell(fmt.Sprintf("git reset --hard %s", ref))
	_, _ = runGoapShell("git clean -fd")
}

func restoreGoapStash() {
	out, _ := runGoapShell("git stash list")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "goap-fusion auto-stash") {
			ref := strings.SplitN(line, ":", 2)[0]
			_, _ = runGoapShell(fmt.Sprintf("git stash pop %s", ref))
			return
		}
	}
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
	failureMarkers := []string{
		"authentication expired",
		"authentication failed",
		"notebooklm circuit breaker open",
		"query failed",
		"auth_status\":\"stale",
		"not_configured",
		"nlm error:",
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

func isGoapFusionApplyRequest(task string) bool {
	lower := strings.ToLower(task)
	if util.ContainsAnyStr(lower,
		"analysis only", "report only", "research only", "scheduled analysis",
		"scheduled goap fusion cycle", "deterministic analysis", "do not apply", "no code", "without code") {
		return false
	}
	return util.ContainsAnyStr(lower,
		"apply", "implement", "fix", "patch",
		"modify code", "edit code", "change code", "write code",
		"add code", "add test", "add feature",
		"register action", "register condition",
		"create tree", "create domain", "build feature",
		"deploy code", "install dependency")
}

// extractConversationID extracts the conversation_id from a NotebookLM JSON response.
// The response looks like: {"answer": "...", "conversation_id": "abc123", ...}
func extractConversationID(out string) string {
	var payload struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err == nil && payload.ConversationID != "" {
		return payload.ConversationID
	}
	return extractJSONStringField(out, "conversation_id")
}
