package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

const (
	goapFusionVaultDir     = "/mnt/ssd/clawd/wiki/bt-research"
	goapFusionSynthesesDir = "/mnt/ssd/clawd/wiki/bt-research/syntheses"
	goapFusionPlansDir     = "/mnt/ssd/clawd/wiki/bt-research/plans"
	goapFusionGraphReport  = "/home/nico/go-bt-evolve/graphify-out/GRAPH_REPORT.md"
	goapFusionRepo         = "/home/nico/go-bt-evolve"
)

func init() {
	registerGoapFusionActions()
}

func registerGoapFusionActions() {
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
