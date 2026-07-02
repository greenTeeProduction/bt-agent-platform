package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerSuperpowersProductionActions()
}

func titleSuperpowersVerb(verb string) string {
	if verb == "" {
		return ""
	}
	return strings.ToUpper(verb[:1]) + verb[1:]
}

// currentSuperpowersForEachTask resolves the Superpowers run and the task a
// ForEachTask loop is currently iterating on, using the
// ChainState["superpowers_task_index"] cursor that BuildForEachTask sets on
// every tick. The returned *SuperpowersTask aliases run.Tasks[idx], so phase
// funcs can mutate it directly and callers just need to persist run.
func currentSuperpowersForEachTask(bb *Blackboard) (*SuperpowersRun, *SuperpowersTask, bool) {
	run, ok := getSuperpowersRun(bb)
	if !ok {
		return nil, nil, false
	}
	idx, ok := chainStateInt(bb, "superpowers_task_index")
	if !ok || idx < 0 || idx >= len(run.Tasks) {
		return nil, nil, false
	}
	return run, &run.Tasks[idx], true
}

func registerSuperpowersProductionActions() {
	RegisterAction("LoadSuperpowersSkills", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		bb.ChainState["skills_loaded"] = []string{"using-superpowers", "writing-plans", "test-driven-development", "verified-patching", "finishing-a-development-branch"}
		bb.ChainState["skill_directives"] = "Use Superpowers SDLC: design, plan, HITL, TDD implementation, verification, finish. Preserve functionality; do not amputate paths."
		bb.Result = "## Superpowers Skills Loaded\n\nusing-superpowers, writing-plans, TDD, verified-patching, finishing"
		return 1
	})

	RegisterAction("UpdateBlackboard", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		bb.ChainState["phase"] = "complete"
		bb.ChainState["outcome"] = "success"
		bb.Outcome = "success"
		return 1
	})

	RegisterAction("ReportPipelineComplete", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if run, ok := getSuperpowersRun(bb); ok {
			bb.Result = fmt.Sprintf("## Superpowers Pipeline Complete\n\nRun: `%s`\nArtifacts: `%s`\nMode: `%s`", run.ID, run.ArtifactDir, run.Mode)
		} else {
			bb.Result = "## Superpowers Pipeline Complete"
		}
		return 1
	})

	RegisterCondition("IsSuperpowersDryRun", func(bb *Blackboard) bool {
		run, ok := getSuperpowersRun(bb)
		if ok {
			return run.Mode == SuperpowersModeDryRun
		}
		return superpowersModeFromTask(bb.Task) == SuperpowersModeDryRun
	})

	RegisterAction("InitSuperpowersRun", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, err := currentSuperpowersRun(bb)
		if err != nil {
			bb.Result = "## Superpowers Run Init Failed\n\n" + err.Error()
			return -1
		}
		if err := ensureSuperpowersRunDirs(run); err != nil {
			bb.Result = "## Superpowers Artifact Init Failed\n\n" + err.Error()
			return -1
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = "## Superpowers Run Persist Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Run Initialized\n\nRun: `%s`\nMode: `%s`\nArtifacts: `%s`", run.ID, run.Mode, run.ArtifactDir)
		return 1
	})

	RegisterAction("GenerateDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, err := currentSuperpowersRun(bb)
		if err != nil {
			bb.Result = err.Error()
			return -1
		}
		run.Phase = SuperpowersPhaseDesign
		run.DesignPath = filepath.Join(run.ArtifactDir, "design.md")
		design := buildDeterministicDesign(run.Task)
		written, err := writeArtifactOnce(run.DesignPath, []byte(design))
		if err != nil {
			bb.Result = "## Design Write Failed\n\n" + err.Error()
			return -1
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		verb := "reused"
		if written {
			verb = "written"
		}
		bb.Result = fmt.Sprintf("## Design Artifact %s\n\nPath: `%s`", titleSuperpowersVerb(verb), run.DesignPath)
		return 1
	})

	RegisterAction("ValidateDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok || run.DesignPath == "" {
			bb.Result = "## Design Validation Failed\n\nNo Superpowers design path in run state."
			return -1
		}
		data, err := os.ReadFile(run.DesignPath)
		if err != nil {
			bb.Result = "## Design Validation Failed\n\n" + err.Error()
			return -1
		}
		content := string(data)
		for _, heading := range []string{"## Goal", "## Architecture", "## Acceptance Criteria", "## Test Strategy", "## Risks"} {
			if !strings.Contains(content, heading) {
				bb.Result = fmt.Sprintf("## Design Validation Failed\n\nMissing heading: %s", heading)
				return -1
			}
		}
		bb.Result = fmt.Sprintf("## Design Validated\n\nPath: `%s`", run.DesignPath)
		return 1
	})

	RegisterAction("PrepareSuperpowersWorktree", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, err := currentSuperpowersRun(bb)
		if err != nil {
			bb.Result = err.Error()
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		if err := createSuperpowersWorktree(c, defaultSuperpowersCommandRunner, run); err != nil {
			bb.Result = "## Worktree Preparation Failed\n\n" + err.Error()
			return -1
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Worktree Ready\n\nPath: `%s`\nBranch: `%s`", run.WorktreePath, run.WorktreeBranch)
		return 1
	})

	RegisterAction("VerifySuperpowersBaseline", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Baseline Failed\n\nNo Superpowers run state."
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		cmd := "/usr/local/go/bin/go build ./cmd/bt-agent ./cmd/bt-agent-cli"
		res := runShellCommand(c, defaultSuperpowersCommandRunner, run.WorktreePathOrRepo(), cmd)
		_ = os.WriteFile(filepath.Join(run.ArtifactDir, "verification", "baseline-build.txt"), []byte(formatCommandResult(res)), 0o644)
		if res.Err != nil {
			bb.Result = "## Baseline Failed\n\n" + res.Output
			return -1
		}
		bb.Result = "## Baseline Verified\n\nBuild passed before implementation."
		return 1
	})

	RegisterAction("GenerateImplementationPlan", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, err := currentSuperpowersRun(bb)
		if err != nil {
			bb.Result = err.Error()
			return -1
		}
		run.Phase = SuperpowersPhasePlan
		if run.PlanPath == "" {
			run.PlanPath = filepath.Join(run.ArtifactDir, "plan.md")
		}
		plan := buildDeterministicImplementationPlan(run.Task)
		written, err := writeArtifactOnce(run.PlanPath, []byte(plan))
		if err != nil {
			bb.Result = "## Plan Write Failed\n\n" + err.Error()
			return -1
		}
		tasks, err := ParseSuperpowersPlan(plan)
		if err != nil {
			bb.Result = "## Generated Plan Invalid\n\n" + err.Error()
			return -1
		}
		run.Tasks = tasks
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		verb := "reused"
		if written {
			verb = "written"
		}
		bb.Result = fmt.Sprintf("## Implementation Plan %s\n\nPath: `%s`\nTasks: %d", titleSuperpowersVerb(verb), run.PlanPath, len(tasks))
		return 1
	})

	RegisterAction("ValidateImplementationPlanStrict", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok || run.PlanPath == "" {
			bb.Result = "## Plan Validation Failed\n\nNo plan path in Superpowers run state."
			return -1
		}
		data, err := os.ReadFile(run.PlanPath)
		if err != nil {
			bb.Result = "## Plan Validation Failed\n\n" + err.Error()
			return -1
		}
		tasks, err := ParseSuperpowersPlan(string(data))
		if err != nil {
			bb.Result = "## Plan Validation Failed\n\n" + err.Error()
			return -1
		}
		run.Tasks = tasks
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Plan Validated\n\nTasks: %d\nPath: `%s`", len(tasks), run.PlanPath)
		return 1
	})

	RegisterAction("ExecuteSuperpowersTaskBatch", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Superpowers Execution Failed\n\nNo run state."
			return -1
		}
		run.Phase = SuperpowersPhaseImplementation
		c, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		if err := ExecuteSuperpowersTaskBatchRuntime(c, run); err != nil {
			bb.Result = "## Superpowers Task Execution Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task Batch Complete\n\nTasks: %d\nMode: `%s`", len(run.Tasks), run.Mode)
		return 1
	})

	// The five actions below let a ForEachTask loop drive one Superpowers
	// task through the RED -> verify RED -> GREEN -> verify GREEN -> commit
	// phases one BT tick at a time, instead of running the whole task inside
	// a single ExecuteSuperpowersTaskBatch tick. Each action resolves its
	// target task from ChainState["superpowers_task_index"] (set by
	// BuildForEachTask), calls the matching phase func extracted from
	// SuperpowersTaskExecutor.ExecuteTask, persists the run, and reports
	// SUCCESS/FAILURE for the tree to act on.
	RegisterAction("SuperpowersTaskRed", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, ok := currentSuperpowersForEachTask(bb)
		if !ok {
			bb.Result = "## Superpowers Task RED Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		c, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err := superpowersTaskRed(c, defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner, run, task)
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			bb.Result = "## Superpowers Task RED Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task RED Complete\n\nTask: %s", task.Title)
		return 1
	})

	RegisterAction("SuperpowersTaskVerifyRed", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, ok := currentSuperpowersForEachTask(bb)
		if !ok {
			bb.Result = "## Superpowers Task Verify RED Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		c, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := superpowersTaskVerifyRed(c, defaultSuperpowersCommandRunner, run, task)
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			bb.Result = "## Superpowers Task Verify RED Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task Verify RED Passed\n\nTask: %s", task.Title)
		return 1
	})

	RegisterAction("SuperpowersTaskGreen", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, ok := currentSuperpowersForEachTask(bb)
		if !ok {
			bb.Result = "## Superpowers Task GREEN Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		c, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err := superpowersTaskGreen(c, defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner, run, task)
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			bb.Result = "## Superpowers Task GREEN Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task GREEN Complete\n\nTask: %s", task.Title)
		return 1
	})

	RegisterAction("SuperpowersTaskVerifyGreen", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, ok := currentSuperpowersForEachTask(bb)
		if !ok {
			bb.Result = "## Superpowers Task Verify GREEN Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		c, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := superpowersTaskVerifyGreen(c, defaultSuperpowersCommandRunner, run, task)
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			bb.Result = "## Superpowers Task Verify GREEN Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task Verify GREEN Passed\n\nTask: %s (status: %s)", task.Title, task.Status)
		return 1
	})

	RegisterAction("SuperpowersTaskCommit", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, ok := currentSuperpowersForEachTask(bb)
		if !ok {
			bb.Result = "## Superpowers Task Commit Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		dir := run.WorktreePathOrRepo()
		add := defaultSuperpowersCommandRunner.Run(c, dir, "git", "add", "-A")
		if add.Err != nil {
			bb.Result = "## Superpowers Task Commit Failed\n\n" + add.Output
			return -1
		}
		staged := runShellCommand(c, defaultSuperpowersCommandRunner, dir, "git diff --cached --quiet")
		if staged.Err == nil {
			bb.Result = fmt.Sprintf("## Superpowers Task Commit Skipped\n\nNo changes to commit for task %q", task.Title)
			return 1
		}
		commit := defaultSuperpowersCommandRunner.Run(c, dir, "git", "commit", "-m", fmt.Sprintf("superpowers: %s", task.Title))
		if commit.Err != nil {
			bb.Result = "## Superpowers Task Commit Failed\n\n" + commit.Output
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task Committed\n\nTask: %s", task.Title)
		return 1
	})

	RegisterCondition("PlanHasIndependentTasks", func(bb *Blackboard) bool {
		run, ok := getSuperpowersRun(bb)
		if !ok {
			return false
		}
		seen := map[string]bool{}
		pending := 0
		for _, t := range run.Tasks {
			if t.Status == "done" {
				continue
			}
			pending++
			for _, f := range t.Files {
				if seen[f] {
					return false
				}
				seen[f] = true
			}
		}
		return pending >= 2
	})

	RegisterAction("VerifySuperpowersRun", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Superpowers Verification Failed\n\nNo run state."
			return -1
		}
		run.Phase = SuperpowersPhaseVerification
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		if err := VerifySuperpowersRunRuntime(c, run); err != nil {
			bb.Result = "## Superpowers Verification Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Verification Passed\n\nChecks: %d", len(run.Verification))
		return 1
	})

	RegisterAction("ApplySuperpowersRunToMainRepo", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Superpowers Apply Failed\n\nNo run state."
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		if err := applySuperpowersRunToMainRepo(c, defaultSuperpowersCommandRunner, run); err != nil {
			bb.Result = "## Superpowers Pending Patch\n\n" + err.Error()
			bb.Outcome = "pending_patch"
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Applied To Main Repo\n\nStatus: `%s`\nCommit: `%s`", run.ApplyStatus, run.AppliedCommit)
		return 1
	})

	RegisterAction("WriteSuperpowersFinishReport", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Finish Failed\n\nNo run state."
			return -1
		}
		run.Phase = SuperpowersPhaseFinish
		finish := buildSuperpowersFinishReport(run)
		path := filepath.Join(run.ArtifactDir, "finish.md")
		if err := os.WriteFile(path, []byte(finish), 0o644); err != nil {
			bb.Result = "## Finish Failed\n\n" + err.Error()
			return -1
		}
		_ = writeSuperpowersRunJSON(run)
		bb.Result = fmt.Sprintf("## Superpowers Production Run Complete\n\nFinish: `%s`\nArtifacts: `%s`", path, run.ArtifactDir)
		return 1
	})

	RegisterAction("RunSuperpowersRuntimeFromExistingPlan", runSuperpowersRuntimeFromExistingPlanAction)
	RegisterAction("RunSuperpowersClaudeImplementation", runSuperpowersRuntimeFromExistingPlanAction)

	// RunScheduledGoapFusionCycle drives the research-to-implementation cycle
	// end-to-end: the GOAP fusion stage reads vault research and the graphify
	// report, identifies improvement gaps, prioritizes goals, and writes a
	// Superpowers implementation plan; this action then implements that plan via
	// the Superpowers runtime (Claude Code execution, TDD verification, apply, and
	// finish reporting). It reuses the existing-plan runtime so the scheduled
	// cycle shares the same durable, idempotent execution path.
	RegisterAction("RunScheduledGoapFusionCycle", runSuperpowersRuntimeFromExistingPlanAction)

}

func runSuperpowersRuntimeFromExistingPlanAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	planPath, _ := bb.ChainState["goap_fusion_superpowers_plan_path"].(string)
	if planPath == "" {
		planPath, _ = bb.ChainState["plan_path"].(string)
	}
	if planPath == "" {
		bb.Result = "## GOAP Superpowers Runtime Failed\n\nNo existing plan path found."
		return -1
	}
	run, err := currentSuperpowersRun(bb)
	if err != nil {
		bb.Result = err.Error()
		return -1
	}
	run.PlanPath = planPath
	data, err := os.ReadFile(planPath)
	if err != nil {
		bb.Result = "## GOAP Superpowers Runtime Failed\n\n" + err.Error()
		return -1
	}
	tasks, err := ParseSuperpowersPlan(string(data))
	if err != nil {
		bb.Result = "## GOAP Superpowers Plan Invalid\n\n" + err.Error()
		return -1
	}
	run.Tasks = tasks
	if run.WorktreePath == "" {
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		if err := createSuperpowersWorktree(c, defaultSuperpowersCommandRunner, run); err != nil {
			bb.Result = "## GOAP Superpowers Worktree Failed\n\n" + err.Error()
			return -1
		}
	}
	c, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	if err := ExecuteSuperpowersTaskBatchRuntime(c, run); err != nil {
		errStr := err.Error()
		if isClaudeRateLimit(errStr) {
			// Claude rate-limited — save the plan for the next cycle and fall
			// back gracefully. Set goals_unchanged so the Selector falls through
			// to ScheduledAnalysisPath instead of dead-ending.
			bb.ChainState["goap_fusion_goals_unchanged"] = "true"
			bb.Result = fmt.Sprintf("## GOAP Superpowers Rate Limited\n\nClaude Code session limit reached. Plan saved for next cycle.\n\nPlan: `%s`\n\nError: %s", planPath, errStr)
			bb.Outcome = "goap_fusion_rate_limited"
			return 0 // non-fatal — let the tree fall through to analysis path
		}
		bb.Result = "## GOAP Superpowers Execution Failed\n\n" + errStr
		return -1
	}
	if err := VerifySuperpowersRunRuntime(c, run); err != nil {
		bb.Result = "## GOAP Superpowers Verification Failed\n\n" + err.Error()
		return -1
	}
	// Auto-clean tracked main-repo state before applying: unstage and reset tracked
	// files so stale state from interrupted previous runs cannot block apply.
	// Do NOT clean docs/superpowers/: the current run's durable evidence lives
	// there and hasBlockingMainRepoDirty already ignores those artifact paths.
	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cleanCancel()
	defaultSuperpowersCommandRunner.Run(cleanCtx, run.RepoDir, "bash", "-c",
		"git reset HEAD 2>/dev/null; git checkout -- . 2>/dev/null; true")
	if err := applySuperpowersRunToMainRepo(c, defaultSuperpowersCommandRunner, run); err != nil {
		finishPath := filepath.Join(run.ArtifactDir, "finish.md")
		_ = os.WriteFile(finishPath, []byte(buildSuperpowersFinishReport(run)), 0o644)
		bb.Result = "## GOAP Superpowers Pending Patch\n\n" + err.Error()
		bb.Outcome = "pending_patch"
		return -1
	}
	finishPath := filepath.Join(run.ArtifactDir, "finish.md")
	_ = os.WriteFile(finishPath, []byte(buildSuperpowersFinishReport(run)), 0o644)
	bb.Result = fmt.Sprintf("## GOAP Superpowers Runtime Complete\n\nRun: `%s`\nFinish: `%s`\nApply status: `%s`\nCommit: `%s`", run.ID, finishPath, run.ApplyStatus, run.AppliedCommit)
	return 1
}

func buildDeterministicDesign(task string) string {
	return fmt.Sprintf(`# Superpowers Design

## Goal
%s

## Architecture
Use the production Superpowers runtime: typed run state, durable artifacts, native HITL, Claude Code task execution, TDD verification, and finish reporting.

## Acceptance Criteria
- Design and plan artifacts are durable and idempotent.
- Claude Code never modifies files before HITL approval.
- Each task records prompt, Claude output, RED/GREEN evidence, and verification results.
- Existing functionality is preserved.

## Test Strategy
Run focused Superpowers runtime tests, tree contract tests, clean Go build, and live CLI smoke tests.

## Risks
- Claude Code can make unrelated edits; guard with changed-file checks and finish evidence.
- BT re-ticks can duplicate pre-HITL artifacts; all pre-HITL writes must be idempotent.
`, task)
}

func buildDeterministicImplementationPlan(task string) string {
	return fmt.Sprintf(`# Superpowers Implementation Plan

> Use RED/GREEN/REFACTOR. Preserve explicit feature paths; do not amputate functionality.

### Task 1: Execute requested Superpowers change

**Objective:** Implement the smallest safe code change for: %s

**Files:**
- Modify: internal/engine/actions_superpowers.go
- Test: internal/engine/superpowers_runtime_contract_test.go

**Step 1: Write failing test (RED)**
Add or update the narrowest regression test for the behavior being changed.

**Step 2: Run RED**
Run: /usr/local/go/bin/go test ./internal/engine -count=1 -run TestSuperpowersRuntime_ActionsRegistered -timeout 120s
Expected: FAIL for the intended missing behavior before implementation.

**Step 3: Implement minimal code**
Use Claude Code to implement only the behavior required by the failing test.

**Step 4: Run GREEN**
Run: /usr/local/go/bin/go test ./internal/engine -count=1 -run TestSuperpowersRuntime_ActionsRegistered -timeout 120s
Expected: PASS.

**Risk:** medium
`, task)
}

func buildSuperpowersFinishReport(run *SuperpowersRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Superpowers Finish Report\n\n")
	fmt.Fprintf(&b, "- Run: `%s`\n", run.ID)
	fmt.Fprintf(&b, "- Mode: `%s`\n", run.Mode)
	fmt.Fprintf(&b, "- Task: %s\n", run.Task)
	fmt.Fprintf(&b, "- Artifact dir: `%s`\n", run.ArtifactDir)
	fmt.Fprintf(&b, "- Worktree: `%s`\n", run.WorktreePath)
	if run.ApplyStatus != "" {
		fmt.Fprintf(&b, "- Apply status: `%s`\n", run.ApplyStatus)
	}
	if run.PatchPath != "" {
		fmt.Fprintf(&b, "- Patch: `%s`\n", run.PatchPath)
	}
	if run.AppliedCommit != "" {
		fmt.Fprintf(&b, "- Applied commit: `%s`\n", run.AppliedCommit)
	}
	fmt.Fprintf(&b, "- Generated: %s\n\n", nowRFC3339())
	fmt.Fprintf(&b, "## Tasks\n")
	for _, task := range run.Tasks {
		fmt.Fprintf(&b, "- [%s] %02d %s\n", task.Status, task.Index, task.Title)
	}
	fmt.Fprintf(&b, "\n## Verification\n")
	for _, check := range run.Verification {
		status := "FAIL"
		if check.Passed {
			status = "PASS"
		}
		fmt.Fprintf(&b, "- %s `%s` (%s)\n", status, check.Name, check.Duration)
	}
	fmt.Fprintf(&b, "\n## Changed Files\n")
	if len(run.ChangedFiles) == 0 {
		fmt.Fprintf(&b, "- none recorded\n")
	} else {
		for _, f := range run.ChangedFiles {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
	}
	return b.String()
}

// isClaudeRateLimit returns true if the error string indicates a Claude Code
// rate limit (session limit, usage limit, or quota exhaustion). The Superpowers
// pipeline treats these as non-fatal: the plan is saved for the next cycle
// instead of failing the entire GOAP fusion run.
func isClaudeRateLimit(errStr string) bool {
	lower := strings.ToLower(errStr)
	return strings.Contains(lower, "session limit") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "usage limit") ||
		strings.Contains(lower, "quota exceeded") ||
		strings.Contains(lower, "resets")
}
