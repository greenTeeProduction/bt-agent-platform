package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"

	btcore "github.com/rvitorper/go-bt/core"
)

const btFusionRepo = "/home/nico/go-bt-evolve"
const btFusionReport = "/mnt/ssd/clawd/wiki/bt-research/bt-fusion-latest.md"

func init() {
	registerBTFusionActions()
}

func registerBTFusionActions() {
	RegisterAction("SearchForBTPatterns", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		findings := []string{
			"LLM-supervised BT evolution: use an LLM as meta-controller over GP-style mutations, but gate runtime/core edits behind review.",
			"Skill-library expansion: extract successful subtrees/actions into reusable domain trees and agent YAMLs.",
			"Telemetry-driven self-improvement: prioritize candidates from run history, trace latency, success rate, and failure modes.",
			"Typed-edge validation: preserve guard/effect/recovery/approval semantics when generating new trees.",
			"Checkpoint verification: generated trees should include deterministic postcondition checks before reporting success.",
		}
		setFusionState(bb, "research_findings", strings.Join(findings, "\n- "))
		bb.Result = "## BT Fusion Research Findings\n\n- " + strings.Join(findings, "\n- ")
		bb.Outcome = string(evolution.Success)
		return 1
	})

	RegisterAction("QueryNotebookLMResearch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		paths := []string{
			"/mnt/ssd/clawd/wiki/bt-research/bt-evolution-2026-06-16.md",
			"/mnt/ssd/clawd/wiki/bt-research/bt-fusion-latest.md",
		}
		var snippets []string
		for _, p := range paths {
			if b, err := os.ReadFile(p); err == nil {
				snippets = append(snippets, fmt.Sprintf("%s: %s", p, truncateFusion(string(b), 900)))
			}
		}
		if len(snippets) == 0 {
			snippets = append(snippets, "No local NotebookLM/vault BT research notes found; continue from built-in research findings.")
		}
		setFusionState(bb, "notebooklm_context", strings.Join(snippets, "\n\n"))
		bb.Result += "\n\n## NotebookLM/Vault Context\n\n" + strings.Join(snippets, "\n\n")
		return 1
	})

	RegisterAction("SynthesizeFindings", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		synthesis := `Top concrete fusion targets for this Go BT platform:
1. Make BT Fusion deterministic where possible; avoid generic LLM-only no-op actions.
2. Keep A2A/scheduler daemon stable under --no-mcp so scheduled fusion can actually run.
3. Add repository-specific fusion reports under the vault so future runs can compound research.
4. Expand gardener pool to include new domain trees (bt_fusion, hermes_update, notebooklm monitors) after current 32-tree pool mismatch is fixed.
5. Add checkpoint/evidence gates to generated domain trees so success requires build/test/log evidence.`
		setFusionState(bb, "synthesis", synthesis)
		bb.Result += "\n\n## Synthesis\n\n" + synthesis
		return 1
	})

	RegisterAction("CheckCodebaseFit", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		cmd := `printf 'trees='; grep -R '"bt_fusion"\|"hermes_update"\|"notebooklm_pipeline_monitor"' -n internal/domains/trees.go; printf '\nagents='; ls ~/.go-bt-evolve/agents/*fusion* ~/.go-bt-evolve/agents/*manager* 2>/dev/null; printf '\nservice='; systemctl --user show bt-agent.service -p ActiveState,SubState,Restart --no-pager 2>/dev/null`
		out, code := runFusionShell(cmd)
		setFusionState(bb, "codebase_fit", out)
		bb.Result += fmt.Sprintf("\n\n## Codebase Fit Evidence (exit=%d)\n\n```\n%s\n```", code, truncateFusion(out, 2500))
		if code != 0 {
			bb.Outcome = "fusion_codebase_fit_failed"
			return -1
		}
		return 1
	})

	RegisterAction("AssessFusionComplexity", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		assessment := `Complexity assessment:
- Deterministic BT Fusion actions: DONE / low risk / additive engine action file.
- --no-mcp daemon stability: DONE / medium risk / main.go signal wait path.
- Gardener pool expansion to 36 trees: medium risk / requires gardener config/state migration.
- Typed-edge generator for new trees: medium risk / should be added as a new domain-tree template first.
- Runtime interface changes: high risk / HITL required.`
		setFusionState(bb, "complexity", assessment)
		bb.Result += "\n\n## Complexity\n\n" + assessment
		return 1
	})

	RegisterAction("PrioritizeFusionTargets", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		priorities := `Priority order:
1. Stabilize bt-agent daemon in --no-mcp mode (required for scheduled agents).
2. Replace generic/no-op BT Fusion actions with deterministic repo-grounded actions.
3. Persist BT Fusion report to vault for compounding research memory.
4. Run domain tests + build every fusion cycle.
5. Next cycle: expand gardener pool from 32 to all 36 registered domain trees.`
		setFusionState(bb, "priorities", priorities)
		bb.Result += "\n\n## Priorities\n\n" + priorities
		return 1
	})

	RegisterAction("ApplyFusion", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		_ = os.MkdirAll(filepath.Dir(btFusionReport), 0755)
		report := fusionMarkdown(bb)
		if err := os.WriteFile(btFusionReport, []byte(report), 0644); err != nil {
			bb.Outcome = "fusion_write_failed"
			bb.Result += "\n\n## ApplyFusion\n\nFailed writing report: " + err.Error()
			return -1
		}
		bb.Result += "\n\n## ApplyFusion\n\nWrote fusion report: `" + btFusionReport + "`"
		return 1
	})

	RegisterAction("VerifyFusionBuild", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		testOut, testCode := runFusionShell("/usr/local/go/bin/go test ./internal/domains/ -run TestAllDomainTrees -count=1 -timeout 180s")
		buildOut, buildCode := runFusionShell("/usr/local/go/bin/go build -o bt-agent ./cmd/bt-agent")
		verification := fmt.Sprintf("go test exit=%d\n%s\n\ngo build exit=%d\n%s", testCode, testOut, buildCode, buildOut)
		setFusionState(bb, "verification", verification)
		bb.Result += fmt.Sprintf("\n\n## Verification\n\n```\n%s\n```", truncateFusion(verification, 3000))
		if testCode != 0 || buildCode != 0 {
			bb.Outcome = "fusion_verify_failed"
			return -1
		}
		return 1
	})

	RegisterAction("ReportFusionStatus", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Outcome = string(evolution.Success)
		bb.Result += "\n\n## Fusion Status\n\nApplied deterministic BT Fusion reporting and verification path. Next target: include all 36 registered domain trees in the gardener/evolution pool and add checkpoint gates to generated trees."
		return 1
	})
}

func setFusionState(bb *Blackboard, key, value string) {
	if bb.ChainState == nil {
		bb.ChainState = map[string]any{}
	}
	bb.ChainState["bt_fusion_"+key] = value
}

func runFusionShell(command string) (string, int) {
	cmd := exec.Command("bash", "-lc", command)
	cmd.Dir = btFusionRepo
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return string(out), exit.ExitCode()
		}
		return string(out) + "\n" + err.Error(), 1
	}
	return string(out), 0
}

func fusionMarkdown(bb *Blackboard) string {
	// Strip injected blackboard context from both the task and the result.
	// The blackboard seeder appends a standardized hint to bb.Task;
	// the report should show the clean task and result only.
	result := bb.Result
	const fusionHeader = "## BT Fusion Research Findings"
	if idx := strings.Index(result, fusionHeader); idx >= 0 {
		result = result[idx:]
	}
	task := bb.Task
	if idx := strings.Index(task, "\n\nBLACKBOARD CONTEXT"); idx >= 0 {
		task = task[:idx]
	}
	return fmt.Sprintf(`# BT Fusion Report

Generated: %s
Task: %s

%s
`, time.Now().Format(time.RFC3339), task, result)
}

func truncateFusion(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...<truncated>"
}
