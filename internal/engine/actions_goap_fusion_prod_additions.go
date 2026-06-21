package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerGoapFusionProductionAdditions()
}

func registerGoapFusionProductionAdditions() {
	RegisterAction("RunGraphifyUpdate", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		res := runShellCommand(c, defaultSuperpowersCommandRunner, goapFusionRepo, "graphify update .")
		if res.Err != nil {
			bb.Result = fmt.Sprintf("## Graphify Update Failed\n\n%s", truncateGoap(res.Output, 2000))
			return -1
		}
		bb.Result = "## Graphify Updated\n\n" + truncateGoap(res.Output, 2000)
		return 1
	})

	RegisterAction("WriteSuperpowersImplementationPlan", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState != nil {
			if existing, _ := bb.ChainState["goap_fusion_superpowers_plan_path"].(string); existing != "" {
				bb.Result = fmt.Sprintf("## GOAP Superpowers Plan Reused\n\nPath: `%s`", existing)
				return 1
			}
		}
		goals, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
		gaps, _ := bb.ChainState["goap_fusion_improvement_gaps"].(string)
		if goals == "" {
			goals = "Implement the highest-priority GOAP fusion improvement safely."
		}
		task := fmt.Sprintf("%s\n\nGOAP goals:\n%s\n\nGaps:\n%s", bb.Task, goals, gaps)
		plan := buildDeterministicImplementationPlan(task)
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
		setGoapState(bb, "superpowers_plan_path", path)
		setGoapState(bb, "superpowers_active_plan", plan)
		bb.ChainState["goap_fusion_superpowers_plan_path"] = path
		bb.ChainState["goap_fusion_active_plan"] = plan
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
		report := fmt.Sprintf("# GOAP Fusion Analysis — %s\n\n## Task\n%s\n\n## Goals\n%s\n\n## Gaps\n%s\n", ts, bb.Task, goals, gaps)
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

	RegisterAction("ReportFusionCycle", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		path, _ := bb.ChainState["goap_fusion_fusion_analysis_path"].(string)
		verify, _ := bb.ChainState["goap_fusion_verify_result"].(string)
		bb.Result = fmt.Sprintf("## GOAP Fusion Cycle Complete\n\nAnalysis: `%s`\n\nVerification:\n```\n%s\n```", path, verify)
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
		bb.Result = fmt.Sprintf("## Superpowers Implementation Complete\n\nRun: `%s`\nArtifacts: `%s`\nChanged files:\n```\n%s\n```", run.ID, run.ArtifactDir, changed)
		return 1
	})
}
