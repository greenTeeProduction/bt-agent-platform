package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	goapFusionClaudeTimeout = 3600 // seconds (1 hour)
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
		return util.ContainsAnyStr(strings.ToLower(bb.Task),
			"apply", "implement", "fix", "create", "build", "add", "register",
			"deploy", "install", "patch", "write", "generate")
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

		setGoapState(bb, "goal_queue", strings.Join(goals, "\n"))
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
	// research, graphify report, gap analysis, and goal queue. It implements ONE
	// focused change, runs go build + go test, and commits.
	RegisterAction("ApplyImprovementWithClaude", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		gapsStr, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
		goalsStr, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
		planStr, _ := bb.ChainState["goap_fusion_active_plan"].(string)
		vaultStr, _ := bb.ChainState["goap_fusion_vault_research"].(string)
		graphStr, _ := bb.ChainState["goap_fusion_graphify_report"].(string)

		prompt := buildClaudeFusionPrompt(bb.Task, gapsStr, goalsStr, planStr,
			truncateGoap(vaultStr, 5000), truncateGoap(graphStr, 5000))

		startTime := time.Now()
		claudeOut, err := runClaudeCode(prompt)
		elapsed := time.Since(startTime)

		setGoapState(bb, "claude_elapsed", elapsed.String())
		setGoapState(bb, "claude_output", lastChars(strings.TrimSpace(claudeOut), 8000))

		if err != nil {
			bb.Result = fmt.Sprintf("## Claude Code Failed\n\nError: %v\n\nPartial output:\n%s\n\nElapsed: %s",
				err, lastChars(strings.TrimSpace(claudeOut), 3000), elapsed)
			bb.Outcome = "goap_fusion_claude_failed"
			return -1
		}

		// Check if Claude made changes
		diffOut, _ := runGoapShell("cd " + goapFusionRepo + " && git diff --stat")
		logOut, _ := runGoapShell("cd " + goapFusionRepo + " && git log --oneline -1")

		bb.Result = fmt.Sprintf("## Claude Code Improvement Applied\n\nElapsed: %s\nLast commit: %s\nChanges:\n```\n%s\n```\n\nClaude output:\n%s",
			elapsed, strings.TrimSpace(logOut), strings.TrimSpace(diffOut),
			truncateGoap(strings.TrimSpace(claudeOut), 3000))
		setGoapState(bb, "claude_result", bb.Result)
		return 1
	})

	// VerifyGoapBuild runs go test on changed packages and go build ./...
	RegisterAction("VerifyGoapBuild", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var results []string

		// Run go test on the domains package (most likely changed by fusion)
		buildCmd := fmt.Sprintf("cd %s && /usr/local/go/bin/go build ./...", goapFusionRepo)
		if out, err := runGoapShell(buildCmd); err != nil {
			results = append(results, fmt.Sprintf("BUILD FAILED:\n%s", out))
			setGoapState(bb, "verify_result", strings.Join(results, "\n"))
			bb.Result = fmt.Sprintf("## Verification Failed\n\n%s", strings.Join(results, "\n"))
			bb.Outcome = "goap_fusion_verify_failed"
			return -1
		}
		results = append(results, "go build ./...: PASSED")

		testCmd := fmt.Sprintf("cd %s && /usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -timeout 180s", goapFusionRepo)
		if out, err := runGoapShell(testCmd); err != nil {
			results = append(results, fmt.Sprintf("TEST FAILED:\n%s", truncateGoap(out, 3000)))
			setGoapState(bb, "verify_result", strings.Join(results, "\n"))
			bb.Result = fmt.Sprintf("## Verification Failed\n\n%s", strings.Join(results, "\n"))
			bb.Outcome = "goap_fusion_verify_failed"
			return -1
		}
		results = append(results, "go test ./internal/domains ./internal/engine: PASSED")

		setGoapState(bb, "verify_result", strings.Join(results, "\n"))
		bb.Result = fmt.Sprintf("## Verification Passed\n\n%s", strings.Join(results, "\n"))
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
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = goapFusionRepo
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func truncateGoap(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...<truncated>"
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

	cmd := exec.CommandContext(ctx,
		goapFusionClaudeBin, "--print",
		"--model", "opus",
		"--allowedTools", "Bash(git diff:git log:git status:go test:go build:go vet:*),Read,Write,Edit,Glob,Grep",
		"--add-dir", goapFusionRepo,
		"-p", prompt,
	)
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
