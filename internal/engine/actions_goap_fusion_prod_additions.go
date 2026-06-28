package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
			setGoapState(bb, "graphify_update_result", "FAILED:\n"+truncateGoap(res.Output, 2000))
			bb.Result = fmt.Sprintf("## Graphify Update Failed\n\n%s", truncateGoap(res.Output, 2000))
			return -1
		}
		setGoapState(bb, "graphify_update_result", "graphify update .: PASSED")
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
		maxAttempts := 12
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
			if !strings.Contains(out, "Run: `") || !strings.Contains(out, "Artifacts: `") || !strings.Contains(out, "Apply status: `committed`") || !strings.Contains(out, "Commit: `") {
				return fail("Superpowers completion missing run/artifact/committed/commit evidence")
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
				"go build ./cmd/bt-agent ./cmd/bt-agent-cli: PASSED",
				"focused go tests: PASSED",
				"graphify update .: PASSED",
			}
			for _, evidence := range requiredEvidence {
				if !strings.Contains(out, evidence) {
					return fail("deterministic analysis output missing evidence: " + evidence)
				}
			}
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
		if status == "committed" || status == "applied" || status == "applied_no_commit" || status == "main_repo" || status == "dry_run" {
			heading = "## Superpowers Implementation Complete"
		}
		bb.Result = fmt.Sprintf("%s\n\nRun: `%s`\nArtifacts: `%s`\nApply status: `%s`\nPatch: `%s`\nCommit: `%s`\nChanged files:\n```\n%s\n```", heading, run.ID, run.ArtifactDir, status, run.PatchPath, run.AppliedCommit, changed)
		return 1
	})

	RegisterAction("CleanupGraphifyOut", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if out, err := runGoapShell("git checkout -- graphify-out/"); err != nil {
			setGoapState(bb, "graphify_cleanup_warning", fmt.Sprintf("git checkout -- graphify-out/ failed: %s", truncateGoap(out, 200)))
		}
		return 1
	})
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
