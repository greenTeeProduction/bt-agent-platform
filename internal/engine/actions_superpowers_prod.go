package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
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

// ensureSuperpowersForEachTaskSetup resolves the ForEachTask run/task exactly
// like currentSuperpowersForEachTask, and — critically — also performs the
// same one-time, idempotent per-task setup ExecuteTask runs via
// ensureSuperpowersTaskSetup before any phase func touches the task. That
// setup populates task.ArtifactDir (so each task's red.txt/green.txt/claude
// output land in their own directory instead of colliding on an empty path)
// and applies the same dry-run / no-test-command short-circuits the batch
// path enforces. Every one of the five SuperpowersTask* actions calls this
// instead of currentSuperpowersForEachTask directly, so each is self
// contained and does not depend on ExecuteSuperpowersTaskBatch — or another
// phase action — having already run setup for this task.
//
// ok is false when there is no run/task on the blackboard at all (same
// meaning as currentSuperpowersForEachTask returning ok=false). When ok is
// true, err carries any ensureSuperpowersTaskSetup failure (e.g. a task with
// no test commands, which previously reached phase funcs like
// superpowersTaskVerifyRed and panicked on an empty task.Tests slice) and
// dryRun reports whether the task is already fully handled (dry-run mode),
// mirroring ExecuteTask's own dryRun short-circuit.
func ensureSuperpowersForEachTaskSetup(bb *Blackboard) (run *SuperpowersRun, task *SuperpowersTask, dryRun bool, ok bool, err error) {
	run, task, ok = currentSuperpowersForEachTask(bb)
	if !ok {
		return nil, nil, false, false, nil
	}
	dryRun, err = ensureSuperpowersTaskSetup(run, task)
	return run, task, dryRun, true, err
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
		if missing := validateDesignHeadings(string(data)); len(missing) > 0 {
			bb.Result = "## Design Validation Failed\n\nMissing: " + strings.Join(missing, ", ")
			return -1
		}
		bb.Result = fmt.Sprintf("## Design Validated\n\nPath: `%s`", run.DesignPath)
		return 1
	})

	// GrillDesignArtifact is the ReviewCycle reviewer for the GrillLoop. It
	// interrogates the validated design: Claude generates up to 12
	// "Q [critical|normal] <branch>: <question>" lines, answered via NotebookLM
	// (batched ≤5/call); Web fallback is nil so unanswered degrade to OPEN. It
	// appends a round-tagged "## Grill Q&A — round N" section to design.md and
	// persists round bookkeeping to the run JSON (GrillRound is the authoritative
	// 10-round bound, refused before any Claude call).
	//
	// Returns SUCCESS with ChainState["review_verdict"]="approved" (zero open
	// criticals) or "needs_work" + ChainState["review_feedback"] (open criticals
	// remain; the reviser consumes the digest). Returns FAILURE only on protocol
	// errors (Claude call failed, no parseable questions), the round bound
	// ("grill_round_bound"), or the no-progress breaker ("grill_no_progress"
	// after 2 stale rounds) — failure ends ReviewCycle and routes to SplitPath.
	RegisterAction("GrillDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		run, ok := getSuperpowersRun(bb)
		if !ok || run.DesignPath == "" {
			bb.Result = "## Grill Design Failed\n\nNo Superpowers design path in run state."
			return -1
		}
		data, err := os.ReadFile(run.DesignPath)
		if err != nil {
			bb.Result = "## Grill Design Failed\n\n" + err.Error()
			return -1
		}
		designContent := string(data)

		if run.Mode == SuperpowersModeDryRun {
			dryMarkdown := "\n## Grill Q&A\n\n_DRY RUN: question generation and answerers skipped._\n\n" +
				"**Q (dry-run, N/A):** N/A\n\n**A:** OPEN-dry-run\n\n"
			if err := os.WriteFile(run.DesignPath, []byte(designContent+dryMarkdown), 0o644); err != nil {
				bb.Result = "## Grill Design Failed\n\n" + err.Error()
				return -1
			}
			bb.ChainState["grill_open_critical"] = 0
			bb.ChainState["review_verdict"] = "approved"
			bb.Result = "## Grill Design (dry run)\n\nExternal calls skipped; marked OPEN-dry-run."
			return 1
		}

		// Round bound: run.GrillRound is authoritative across restarts.
		const grillMaxRounds = 10
		if run.GrillRound >= grillMaxRounds {
			bb.Outcome = "grill_round_bound"
			bb.Result = fmt.Sprintf("## Grill Design Halted\n\nRound bound reached (%d).", grillMaxRounds)
			return -1
		}
		round := run.GrillRound + 1

		grillPrompt := fmt.Sprintf(`Interview this design relentlessly (grill-me). Walk every design-tree branch. Output ONLY lines "Q [critical|normal] <branch>: <question>". Max 12 questions. Mark [critical] only where a wrong answer breaks correctness, data, or security.

## Design

%s`, designContent)

		claudeRes := defaultSuperpowersClaudeRunner.RunClaude(context.Background(), run.WorktreePathOrRepo(), grillPrompt)
		if claudeRes.Err != nil {
			bb.Result = "## Grill Design Failed\n\nClaude question-generation call failed: " + claudeRes.Err.Error() + "\n\n" + claudeRes.Output
			bb.Outcome = "grill_claude_failed"
			return -1
		}
		qs := parseGrillQuestions(claudeRes.Output)
		if len(qs) == 0 {
			// A non-error Claude run that yields zero parseable "Q [critical|
			// normal] <branch>: ..." lines is a protocol failure, not a clean
			// design: the prompt demands up to 12 Q-lines, so zero means the
			// output was garbage (wrong format, refusal, empty response) —
			// never treat it as "nothing to ask".
			bb.Result = "## Grill Design Failed\n\nClaude produced no parseable grill questions.\n\n" + claudeRes.Output
			bb.Outcome = "grill_no_questions_parsed"
			return -1
		}

		res := resolveGrillQuestions(context.Background(), qs, grillAnswerers{
			NotebookLM: grillNotebookLMAnswerer,
			Web:        nil, // no batched-question web-research action exists to wire
		})

		// Round-tagged, append-only Q&A appendix.
		section := grillRoundHeading(round) + strings.TrimPrefix(res.Markdown, "\n## Grill Q&A\n\n")
		if err := os.WriteFile(run.DesignPath, []byte(designContent+section), 0o644); err != nil {
			bb.Result = "## Grill Design Failed\n\n" + err.Error()
			return -1
		}

		// No-progress breaker: same open-critical set AND same body hash as
		// the previous round, twice in a row => reviewer failure => SplitPath.
		body, _ := splitDesignDocument(designContent)
		hash := designBodyHash(body)
		stale := hash == run.DesignBodyHash && slices.Equal(res.OpenCriticalBranches, run.OpenCriticalBranches)
		if stale {
			run.NoProgressRounds++
		} else {
			run.NoProgressRounds = 0
		}
		run.GrillRound = round
		run.DesignBodyHash = hash
		run.OpenCriticalBranches = res.OpenCriticalBranches
		if run.NoProgressRounds >= 2 {
			run.NoProgressTripped = true
			_ = writeSuperpowersRunJSON(run)
			setSuperpowersRun(bb, run)
			bb.Outcome = "grill_no_progress"
			bb.Result = fmt.Sprintf("## Grill Design Halted\n\nNo progress for 2 consecutive rounds (round %d, %d open criticals).", round, res.OpenCritical)
			return -1
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		setSuperpowersRun(bb, run)

		bb.ChainState["grill_open_critical"] = res.OpenCritical
		if res.OpenCritical == 0 {
			bb.ChainState["review_verdict"] = "approved"
			bb.Result = fmt.Sprintf("## Grill Design Approved\n\nRound %d: %d questions, 0 open criticals.", round, len(qs))
			return 1
		}
		bb.ChainState["review_verdict"] = "needs_work"
		bb.ChainState["review_feedback"] = fmt.Sprintf("Grill round %d results:\n%s", round, openCriticalDigest(qs, res.Answers))
		bb.Result = fmt.Sprintf("## Grill Design Needs Work\n\nRound %d: %d questions, %d open criticals.", round, len(qs), res.OpenCritical)
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
		plan := buildGoalDrivenImplementationPlan(run.Task)
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
		// Goal-driven plans run up to maxGoalDrivenTasks full RED→GREEN
		// Claude executions per cycle; 45 minutes fit only the legacy
		// single-task template.
		c, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
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
	// BuildForEachTask) via ensureSuperpowersForEachTaskSetup — which also
	// runs the same one-time per-task setup ExecuteTask relies on, so
	// ArtifactDir is populated and dry-run/no-test-command tasks are handled
	// consistently no matter which of these five actions runs first — calls
	// the matching phase func extracted from SuperpowersTaskExecutor.ExecuteTask,
	// persists the run, and reports SUCCESS/FAILURE for the tree to act on.
	RegisterAction("SuperpowersTaskRed", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, dryRun, ok, err := ensureSuperpowersForEachTaskSetup(bb)
		if !ok {
			bb.Result = "## Superpowers Task RED Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		if err != nil {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = "## Superpowers Task RED Failed\n\n" + err.Error()
			return -1
		}
		if dryRun {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = fmt.Sprintf("## Superpowers Task RED Skipped (dry run)\n\nTask: %s", task.Title)
			return 1
		}
		c, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		err = superpowersTaskRed(c, defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner, run, task)
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
		run, task, dryRun, ok, err := ensureSuperpowersForEachTaskSetup(bb)
		if !ok {
			bb.Result = "## Superpowers Task Verify RED Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		if err != nil {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = "## Superpowers Task Verify RED Failed\n\n" + err.Error()
			return -1
		}
		if dryRun {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = fmt.Sprintf("## Superpowers Task Verify RED Skipped (dry run)\n\nTask: %s", task.Title)
			return 1
		}
		c, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err = superpowersTaskVerifyRed(c, defaultSuperpowersCommandRunner, run, task)
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
		run, task, dryRun, ok, err := ensureSuperpowersForEachTaskSetup(bb)
		if !ok {
			bb.Result = "## Superpowers Task GREEN Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		if err != nil {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = "## Superpowers Task GREEN Failed\n\n" + err.Error()
			return -1
		}
		if dryRun {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = fmt.Sprintf("## Superpowers Task GREEN Skipped (dry run)\n\nTask: %s", task.Title)
			return 1
		}
		c, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		// review_feedback is set by a prior SuperpowersTaskReview "needs_work"
		// verdict (see ReviewCycle decorator, review_cycle.go); when present it
		// is injected into the GREEN prompt so Claude addresses the reviewer's
		// concerns on this pass, and left in place until the decorator clears
		// it on approval.
		feedback, _ := bb.ChainState["review_feedback"].(string)
		err = superpowersTaskGreen(c, defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner, run, task, feedback)
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
		run, task, dryRun, ok, err := ensureSuperpowersForEachTaskSetup(bb)
		if !ok {
			bb.Result = "## Superpowers Task Verify GREEN Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		if err != nil {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = "## Superpowers Task Verify GREEN Failed\n\n" + err.Error()
			return -1
		}
		if dryRun {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = fmt.Sprintf("## Superpowers Task Verify GREEN Skipped (dry run)\n\nTask: %s", task.Title)
			return 1
		}
		c, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err = superpowersTaskVerifyGreen(c, defaultSuperpowersCommandRunner, run, task)
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			bb.Result = "## Superpowers Task Verify GREEN Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Task Verify GREEN Passed\n\nTask: %s (status: %s)", task.Title, task.Status)
		return 1
	})

	// SuperpowersTaskReview backs the ReviewCycle decorator's reviewer_action
	// slot (review_cycle.go): it makes a separate Claude call reviewing the
	// worktree's current `git diff` against the task spec, and writes the
	// parsed verdict/feedback onto ChainState["review_verdict"]/
	// ["review_feedback"] for the decorator to act on. An unparseable or
	// missing verdict defaults to "needs_work" (see
	// parseSuperpowersReviewVerdict) — the same safe default the decorator
	// itself falls back to.
	RegisterAction("SuperpowersTaskReview", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, dryRun, ok, err := ensureSuperpowersForEachTaskSetup(bb)
		if !ok {
			bb.Result = "## Superpowers Task Review Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		if err != nil {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = "## Superpowers Task Review Failed\n\n" + err.Error()
			return -1
		}
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		if dryRun {
			_ = writeSuperpowersRunJSON(run)
			// Nothing was actually implemented in dry-run mode; approve
			// trivially so a ReviewCycle wrapping this action does not spin
			// through max_iterations reviewing a no-op change.
			bb.ChainState["review_verdict"] = "approved"
			delete(bb.ChainState, "review_feedback")
			bb.Result = fmt.Sprintf("## Superpowers Task Review Skipped (dry run)\n\nTask: %s", task.Title)
			return 1
		}
		c, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		verdict, feedback, err := superpowersTaskReview(c, defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner, run, task)
		_ = writeSuperpowersRunJSON(run)
		if err != nil {
			bb.Result = "## Superpowers Task Review Failed\n\n" + err.Error()
			return -1
		}
		bb.ChainState["review_verdict"] = verdict
		if feedback != "" {
			bb.ChainState["review_feedback"] = feedback
		} else {
			delete(bb.ChainState, "review_feedback")
		}
		bb.Result = fmt.Sprintf("## Superpowers Task Review Complete\n\nTask: %s\nVerdict: %s", task.Title, verdict)
		return 1
	})

	RegisterAction("SuperpowersTaskCommit", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, task, dryRun, ok, err := ensureSuperpowersForEachTaskSetup(bb)
		if !ok {
			bb.Result = "## Superpowers Task Commit Failed\n\nNo Superpowers run/task index on blackboard."
			return -1
		}
		if err != nil {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = "## Superpowers Task Commit Failed\n\n" + err.Error()
			return -1
		}
		if dryRun {
			_ = writeSuperpowersRunJSON(run)
			bb.Result = fmt.Sprintf("## Superpowers Task Commit Skipped (dry run)\n\nTask: %s", task.Title)
			return 1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		dir := run.WorktreePathOrRepo()
		// Scope `git add -A` away from generated Superpowers/graphify
		// artifacts (task evidence dirs, graphify-out/, docs/superpowers/**),
		// mirroring the exclusion pathspecs commitAppliedSuperpowersRun uses
		// for the whole-run apply commit (superpowers_apply.go) — otherwise a
		// per-task commit in the run worktree would also stage those
		// generated paths.
		addArgs := append([]string{"add", "-A", "--", "."}, superpowersGeneratedCommitExclusions()...)
		add := defaultSuperpowersCommandRunner.Run(c, dir, "git", addArgs...)
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

	// PushBranchAndCreatePR and DiscardSuperpowersWorktree back the "pr" and
	// "discard" (default) branches of the ChooseFinishOption/FinishRouter
	// DecisionTree (superpowers_workflow.go) — the finishing-a-development-branch
	// options beyond the existing merge path (ApplySuperpowersRunToMainRepo).
	RegisterAction("PushBranchAndCreatePR", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Push Branch And Create PR Failed\n\nNo run state."
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		prURL, err := pushBranchAndCreatePR(c, defaultSuperpowersCommandRunner, run)
		if err != nil {
			bb.Result = "## Push Branch And Create PR Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Pull Request Created\n\nBranch: `%s`\nPR: %s", run.WorktreeBranch, prURL)
		return 1
	})

	RegisterAction("DiscardSuperpowersWorktree", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Discard Superpowers Worktree Failed\n\nNo run state."
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		if err := discardSuperpowersWorktree(c, defaultSuperpowersCommandRunner, run); err != nil {
			bb.Result = "## Discard Superpowers Worktree Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Superpowers Worktree Discarded\n\nWorktree: `%s`\nBranch: `%s`", run.WorktreePath, run.WorktreeBranch)
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

	// RunScheduledGoapFusionCycle sits at the END of the Phase-0 preflight —
	// BEFORE the main sequence's research/gap-analysis/plan steps have run.
	// Its job is only to RESUME a plan saved by an earlier (e.g. rate-limited)
	// cycle. Without a saved plan it must no-op succeed and let the main
	// sequence drive research→plan→implement: registering it as a bare alias
	// of the existing-plan runtime made every wired scheduled cycle die in
	// ~120ms with "No existing plan path found" (2026-07-03 23:00/23:30).
	RegisterAction("RunScheduledGoapFusionCycle", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		// Bounded, best-effort: sweep every parked (pending_patch) run under
		// superpowersRunsDir for a rebase/ff-land recovery attempt BEFORE
		// deciding whether to resume a saved plan — a run parked by an earlier
		// cycle's refused fast-forward is otherwise stuck forever once its own
		// cycle already cleared its plan carryover. A failure here must never
		// abort the preflight; it only ever records evidence in each run's own
		// run.json.
		recoverCtx, recoverCancel := context.WithTimeout(context.Background(), 15*time.Minute)
		recoverGoapFusionPendingPatchesInDir(recoverCtx, defaultSuperpowersCommandRunner, superpowersRunsDir)
		recoverCancel()
		// Read the saved plan DURABLY: a fresh cron tick's ChainState is empty, so
		// the only place a rate-limited carryover survives is the agent-scope store.
		planPath, activePlan := loadSuperpowersPlanState(bb)
		if planPath == "" {
			planPath, _ = bb.ChainState["plan_path"].(string)
		}
		if planPath == "" {
			bb.Result = "## Scheduled GOAP Fusion Cycle\n\nPreflight passed; no saved plan to resume — the main cycle will research, plan, and implement."
			return 1
		}
		// A saved plan whose every task objective is already recorded as
		// implemented is a stale carryover (e.g. saved by a binary that
		// predated clear-on-success). Resuming it re-implements landed work:
		// the REDs pass unexpectedly, the run fails, the cycle burns.
		if superpowersPlanAlreadyImplemented(activePlan) {
			clearSuperpowersPlanState(bb)
			bb.Result = "## Scheduled GOAP Fusion Cycle\n\nSaved plan's tasks are already implemented (knowledge store); cleared the stale carryover — the main cycle will research fresh goals."
			return 1
		}
		return runSuperpowersRuntimeFromExistingPlanAction(ctx)
	})

	RegisterAction("ClassifyTaskKind", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if k, _ := bb.ChainState["task_kind"].(string); k != "" {
			return 1 // resumed run: never reclassify
		}
		task := strings.ToLower(bb.Task)
		bugWords := []string{"fix", "bug", "error", "fail", "crash", "regression", "broken", "flake"}
		creativeWords := []string{"build", "add", "implement", "create", "feature", "extend", "design", "refactor"}
		kind := ""
		for _, w := range bugWords {
			if strings.Contains(task, w) {
				kind = "bug"
				break
			}
		}
		if kind == "" {
			for _, w := range creativeWords {
				if strings.Contains(task, w) {
					kind = "creative"
					break
				}
			}
		}
		if kind == "" {
			if len(bb.Task) <= 200 {
				kind = "direct"
			} else {
				kind = "creative" // long ambiguous request ⇒ brainstorm first (using-superpowers bias)
			}
		}
		bb.ChainState["task_kind"] = kind
		return 1
	})
	RegisterCondition("TaskKindIsBug", func(bb *Blackboard) bool {
		k, _ := bb.ChainState["task_kind"].(string)
		return k == "bug"
	})
	RegisterCondition("TaskKindIsCreative", func(bb *Blackboard) bool {
		k, _ := bb.ChainState["task_kind"].(string)
		return k == "creative"
	})

}

// pendingPatchRecoveryCheckName is the VerificationCheck.Name a recorded
// pending_patch recovery attempt uses — reusing run.Verification (rather than
// a new SuperpowersRun field) as the durable per-run attempt ledger, the same
// artifact shape every other apply-time check already uses
// (superpowers_apply.go's verifySuperpowersRuntimeInDir).
const pendingPatchRecoveryCheckName = "pending-patch-recovery"

// pendingPatchRecoveryMaxAttempts bounds the TOTAL rebase/ff-land attempts a
// parked run may accumulate across scheduled cycles (not per-cycle): once a
// run has this many recorded pendingPatchRecoveryCheckName attempts in
// run.json it is abandoned — left parked, but never retried again — so a run
// that keeps failing to land cannot spin the scheduler forever.
const pendingPatchRecoveryMaxAttempts = 2

// recoverGoapFusionPendingPatchesInDir is the bounded pending_patch recovery
// pass for the scheduled runtime cycle (Q3 Reliability & Q5 Consistency —
// non-destructive goap-fusion materializer, milestone 4/5). It scans every
// run parked under runsDir and, for each one still recorded as
// ApplyStatus=="pending_patch" whose superpowers/<id> branch still exists,
// attempts at most ONE rebase-onto-master + full re-verify + ff-land
// (reapplyRunBranchOntoMaster) per cycle — UNLESS the run's plan is already
// recorded as landed in the knowledge store (superpowersPlanAlreadyImplemented),
// in which case it is stale carryover and is skipped entirely, untouched. Every
// attempt (success or failure) is recorded durably via
// pendingPatchRecoveryCheckName so the count survives across cron ticks; once
// a run has accumulated pendingPatchRecoveryMaxAttempts attempts it is
// abandoned outright — skipped with zero commands issued, never a 3rd time.
// Failures here are non-fatal to the caller: this is a best-effort background
// sweep, not the main scheduled-cycle path.
func recoverGoapFusionPendingPatchesInDir(ctx context.Context, runner CommandRunner, runsDir string) {
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		run, err := readSuperpowersRunJSON(filepath.Join(runsDir, entry.Name(), "run.json"))
		if err != nil || run == nil || run.ApplyStatus != "pending_patch" {
			continue
		}
		if pendingPatchRecoveryAttempts(run) >= pendingPatchRecoveryMaxAttempts {
			continue // abandoned: attempt budget exhausted, never retried again
		}
		branch := strings.TrimSpace(run.WorktreeBranch)
		if branch == "" {
			continue
		}
		listed := runner.Run(ctx, run.RepoDir, "git", "branch", "--list", branch)
		if strings.TrimSpace(listed.Output) == "" {
			continue // branch gone; nothing left to recover
		}
		planText, _ := os.ReadFile(run.PlanPath)
		if superpowersPlanAlreadyImplemented(string(planText)) {
			continue // superseded: this work already landed out-of-band
		}
		recoverErr := reapplyRunBranchOntoMaster(ctx, runner, run)
		vc := VerificationCheck{Name: pendingPatchRecoveryCheckName, Command: "reapplyRunBranchOntoMaster", Passed: recoverErr == nil}
		if recoverErr != nil {
			vc.Output = recoverErr.Error()
			run.Verification = append(run.Verification, vc)
			run.ApplyStatus = "pending_patch"
			_ = writeSuperpowersRunJSON(run)
			continue
		}
		run.Verification = append(run.Verification, vc)
		// ffLandRunBranchAndPush sets ApplyStatus to "committed" or
		// "committed_unpushed"; either way the run is no longer pending_patch,
		// so a future cycle will not re-attempt it regardless of its error.
		_ = ffLandRunBranchAndPush(ctx, runner, run)
		_ = writeSuperpowersRunJSON(run)
	}
}

// pendingPatchRecoveryAttempts counts the recorded pending_patch recovery
// attempts already accumulated in run.Verification.
func pendingPatchRecoveryAttempts(run *SuperpowersRun) int {
	n := 0
	for _, v := range run.Verification {
		if v.Name == pendingPatchRecoveryCheckName {
			n++
		}
	}
	return n
}

// markGoapFusionImplDegraded records a durable signal that the ClaudeSuperpowersPath
// implementation attempt degraded, so the fallback-eligible NoNewGapsOrImplDegraded
// guard lets ScheduledAnalysisPath catch the cycle and produce deterministic
// analysis + build/graphify evidence instead of aborting the whole loop. The
// reason is preserved so real (non-rate-limit) failures stay observable in the
// fusion analysis note WriteFusionAnalysis writes.
func markGoapFusionImplDegraded(bb *Blackboard, reason string) {
	trimmed := truncateGoap(strings.TrimSpace(reason), 2000)
	setGoapState(bb, "impl_degraded", "true")
	setGoapState(bb, "impl_degraded_reason", trimmed)
	// Observability: a genuine degradation — anything but the healthy Claude
	// rate-limit carryover (goap_fusion_rate_limited), which is an expected
	// pause resumed next cycle — means ClaudeSuperpowersPath failed and the
	// cycle produced NO landed code before falling back to deterministic
	// analysis. Surface it at WARN so a sustained LLM/model outage is visible
	// in bt.log instead of hiding behind the fallback's outcome:success: the
	// 2026-07-10→12 Fable-limit drought ran ~33h logging only
	// "scheduler: cycle complete outcome:success quality:0.9".
	if bb.Outcome != "goap_fusion_rate_limited" {
		Warn("goap fusion: implementation degraded — Claude path failed, no code landed; fell back to deterministic analysis",
			"outcome", bb.Outcome,
			"reason", truncateGoap(trimmed, 300))
	}
}

// superpowersRuntimeRunBudget bounds one ClaudeSuperpowersPath run end-to-end
// (task batch, verification, review, apply). The legacy 45 minutes fit only
// the single-task template: on 2026-07-18 nine consecutive cycles finished a
// goal-driven batch's tasks 1-2 green in ~40 minutes, were SIGKILLed
// mid-task-3 at exactly 45:00, and landed nothing while wrongly charging the
// milestone-abandon budget. Matches ExecuteSuperpowersTaskBatch's 90-minute
// batch budget; the goap runners' cron ticks skip while a cycle is live, so a
// longer run stretches cadence instead of overlapping.
const superpowersRuntimeRunBudget = 90 * time.Minute

func runSuperpowersRuntimeFromExistingPlanAction(ctx *btcore.BTContext[Blackboard]) (result int) {
	bb := ctx.Blackboard
	// Restore durable charge stamps BEFORE the deferred failure handler below
	// can need them: a resumed cron tick builds a fresh Blackboard with an
	// empty ChainState, so chargeGoapResearchGoalFailure/
	// refundGoapMilestoneAttemptForInfraFailure would otherwise see no stamp
	// and silently no-op on a genuine failure.
	loadGoapChargeStampsDurable(bb)
	// ANY failure of ClaudeSuperpowersPath — not just a Claude rate limit — must
	// degrade the cycle to the deterministic ScheduledAnalysisPath rather than
	// abort the loop. Every failure exit returns -1; stamp the durable
	// impl-degraded signal here so we cannot forget it on any error path.
	defer func() {
		if result == -1 {
			markGoapFusionImplDegraded(bb, bb.Result)
			// Infrastructure failures (rate limit, wedged commit gate, apply/
			// sync refusal, worktree failure) refund the milestone attempt
			// charged at queue time — external outages must never consume the
			// milestone-abandon budget (2026-07-09 doc-drift wedge lesson).
			switch classifyGoapCycleFailure(bb.Outcome, bb.Result) {
			case goapCycleFailureRedPass:
				// RED passed before GREEN: the predicted regression does not
				// exist at HEAD — most likely the milestone's work already
				// landed out-of-band. Refund the charge, record red-pass
				// evidence, and complete the milestone on repeat evidence so
				// it never treadmills against done work (2026-07-15 23:04).
				handleGoapRedPassCycleFailure(bb)
			case goapCycleFailureInfra:
				refundGoapMilestoneAttemptForInfraFailure(bb)
			default:
				// A GENUINE implementation failure consumes one attempt of
				// the head research goal's budget, so a goal the agent cannot
				// land is abandoned instead of treadmilling — and it kills
				// any already-landed hypothesis the milestone had accrued.
				chargeGoapResearchGoalFailure(bb)
				resetGoapMilestoneRedPassStreak(bb)
			}
			// Clear the durable plan on every non-rate-limit failure so the
			// next scheduled cycle re-plans from scratch instead of re-resuming
			// a doomed plan forever (and dropping any freshly-analyzed goals).
			// The rate-limit branch sets bb.Outcome == goap_fusion_rate_limited
			// and is the ONLY case whose carryover must survive to be resumed.
			if bb.Outcome != "goap_fusion_rate_limited" {
				clearSuperpowersPlanState(bb)
			}
		}
	}()
	// Read durably: a rate-limited carryover only survives in the agent-scope
	// store because the next cron tick builds a fresh, empty ChainState.
	planPath, _ := loadSuperpowersPlanState(bb)
	if planPath == "" {
		planPath, _ = bb.ChainState["plan_path"].(string)
	}
	if planPath == "" {
		bb.Result = "## GOAP Superpowers Runtime Failed\n\nNo existing plan path found."
		return -1
	}
	// Honor a durable rate-limit backoff BEFORE creating a worktree or spending
	// the superpowersRuntimeRunBudget attempt: a quota known to be closed makes the whole
	// run doomed, so degrade to ScheduledAnalysisPath instantly with the exact
	// rate-limited Result/Outcome shape — the deferred clearSuperpowersPlanState
	// guard then preserves the plan carryover for the tick after the window
	// expires. claudeBackoffActive self-clears an elapsed window (half-open),
	// so a stale deadline can never wedge the loop into skipping Claude forever.
	if claudeBackoffActive(bb, time.Now()) {
		until, _ := loadClaudeBackoffState(bb)
		bb.ChainState["goap_fusion_goals_unchanged"] = "true"
		bb.Result = fmt.Sprintf("## GOAP Superpowers Rate Limited\n\nClaude rate-limit backoff active until %s; plan carried over to the next cycle.\n\nPlan: `%s`", until.UTC().Format(time.RFC3339), planPath)
		bb.Outcome = "goap_fusion_rate_limited"
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
	c, cancel := context.WithTimeout(context.Background(), superpowersRuntimeRunBudget)
	defer cancel()
	// Reap worktrees leaked by earlier crashed/abandoned cycles before doing
	// new work; failure paths below intentionally keep their worktree for
	// diagnosis, and this sweep is what eventually reclaims them.
	if swept := sweepStaleSuperpowersWorktrees(c, defaultSuperpowersCommandRunner, run.RepoDir, run.WorktreePath, staleSuperpowersWorktreeMaxAge); len(swept) > 0 {
		Info("swept stale superpowers worktrees", "count", len(swept), "paths", strings.Join(swept, ", "))
	}
	// The per-worktree sweep above only deletes branches whose worktree dir is
	// still present; branches orphaned after their dir is gone accumulate
	// unbounded (89 leaked as of 2026-07-13). Reap the merged ones here.
	if reaped := reapOrphanedSuperpowersBranches(c, defaultSuperpowersCommandRunner, run.RepoDir); len(reaped) > 0 {
		Info("reaped orphaned superpowers branches", "count", len(reaped), "branches", strings.Join(reaped, ", "))
	}
	if err := ExecuteSuperpowersTaskBatchRuntime(c, run); err != nil {
		errStr := err.Error()
		if isClaudeRateLimit(errStr) {
			// Claude rate-limited — save the plan for the next cycle and fall
			// back gracefully. Set goals_unchanged so the Selector falls through
			// to ScheduledAnalysisPath instead of dead-ending. Record the durable
			// backoff deadline — the CLI-reported reset when the output names
			// one, the fixed window otherwise — so the NEXT ticks short-circuit
			// at the entry guard instead of re-resuming the plan against the
			// closed quota.
			saveClaudeBackoffState(bb, claudeBackoffDeadline(errStr, time.Now(), claudeBackoffWindow()))
			bb.ChainState["goap_fusion_goals_unchanged"] = "true"
			bb.Result = fmt.Sprintf("## GOAP Superpowers Rate Limited\n\nClaude Code session limit reached. Plan saved for next cycle.\n\nPlan: `%s`\n\nError: %s", planPath, errStr)
			bb.Outcome = "goap_fusion_rate_limited"
			// -1, not 0: a Selector only advances to its next child on Failure;
			// 0 means Running in this engine and re-ticks the tree until the
			// runner's maxTicks cap stamps the run "partial".
			return -1
		}
		bb.Result = "## GOAP Superpowers Execution Failed\n\n" + errStr
		return -1
	}
	// Keep the architecture documentation in the same commit as the change:
	// best-effort per-section arc42 + README sync in the run worktree before
	// verification (classifier-prefiltered; degrades to all sections).
	if _, note := syncArc42SectionsAndReadme(c, defaultSuperpowersClaudeRunner, defaultSuperpowersCommandRunner, run); note != "" {
		run.Arc42Sync = note
		_ = writeSuperpowersRunJSON(run)
	}
	// Doc-drift repair before the hard doc-drift verification check: the
	// trees write the documentation their changes require (and self-heal
	// drift inherited from external landings) in the same commit.
	if _, note := syncDriftDocs(c, defaultSuperpowersClaudeRunner, defaultSuperpowersCommandRunner, run); note != "" {
		run.DocDriftSync = note
		_ = writeSuperpowersRunJSON(run)
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
	run.Phase = SuperpowersPhaseFinish
	_ = writeSuperpowersRunJSON(run)
	finishPath := filepath.Join(run.ArtifactDir, "finish.md")
	_ = os.WriteFile(finishPath, []byte(buildSuperpowersFinishReport(run)), 0o644)
	// Produce the durable consecutive no-op-patch streak the CIRCUITPOLICY loop
	// runner reads via goapFusionNoopPatchStreak: a run that applied but changed
	// no tracked files (empty ChangedFiles AND no commit) increments the streak;
	// a genuine change resets it to 0. Without this the streak was never written
	// and the no-op tail of Activity-Progress Confusion could not be detected.
	recordGoapFusionPatchApply(bb, run)
	// Research memory: record what this run landed so future research cycles
	// do not re-propose it, and advance the active multi-cycle program only
	// when this run's changed files or done tasks executed the milestone's
	// file anchors — a drifted cycle must not check off work it never did.
	recordImplementedGoals(run)
	completeGoapProgramMilestone(bb, run)
	// Real progress landed — reset the CIRCUITPOLICY state-hash window so the
	// next milestone starts fresh instead of inheriting this milestone's
	// repeated hashes and tripping the preflight breaker before it can run.
	ClearGoapFusionStateHashes(bb)
	// The run's work is applied to master; the worktree and its merged branch
	// are done. Cleanup is best-effort — a failure here must not turn a
	// successfully applied run into a reported failure.
	if err := cleanupAppliedSuperpowersWorktree(c, defaultSuperpowersCommandRunner, run); err != nil {
		Info("superpowers worktree cleanup after apply failed (non-fatal)", "run", run.ID, "err", err.Error())
	}
	// The plan is applied to master; clear the durable plan state so the next
	// scheduled cycle does not re-resume already completed work.
	clearSuperpowersPlanState(bb)
	bb.Result = fmt.Sprintf("## GOAP Superpowers Runtime Complete\n\nRun: `%s`\nFinish: `%s`\nApply status: `%s`\nCommit: `%s`", run.ID, finishPath, run.ApplyStatus, run.AppliedCommit)
	bb.Result += programContinueNote()
	if run.PartialFailure != "" {
		bb.Result += "\n\nPARTIAL LANDING: completed tasks landed; " + run.PartialFailure
	}
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
	if run.PartialFailure != "" {
		fmt.Fprintf(&b, "- PARTIAL LANDING: %s\n", run.PartialFailure)
	}
	if run.Arc42Sync != "" {
		fmt.Fprintf(&b, "- arc42 sync: %s\n", run.Arc42Sync)
	}
	if run.DocDriftSync != "" {
		fmt.Fprintf(&b, "- doc-drift sync: %s\n", run.DocDriftSync)
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

// nlmGrillAnswerer answers a batch of grill questions (≤5, enforced by
// resolveGrillQuestions) with a single `nlm notebook query` call. The nlm CLI
// has no multi-question "ask" endpoint (confirmed via `nlm notebook query
// --help`: it takes exactly one NOTEBOOK_ID and one QUESTION), so batching is
// realized by folding all questions for this call into one numbered prompt
// and asking NotebookLM to answer them with matching "A<n>:" markers — this
// is what keeps the call count within the free-plan 50/day budget instead of
// spending one call per question.
//
// Before spending the call (nlmGrillAuthGuard) and again on the response
// (nlmGrillUnavailable), the answerer is checked for auth/quota/error
// conditions. A past bug wrote quota error text straight into artifacts as if
// it were an answer; these guards return errAnswererUnavailable instead so
// resolveGrillQuestions leaves those questions OPEN for the next answerer (or
// OPEN in the final markdown) rather than fabricating content from error
// output. The same treatment applies to a batch response that comes back
// empty/whitespace or with zero parseable "A<n>:" lines: that is NotebookLM
// producing garbage, not "zero answers found", so it must stop batching
// immediately rather than let resolveGrillQuestions keep spending calls on
// later batches against a broken answerer.
// grillNotebookLMAnswerer is the swappable indirection GrillDesignArtifact
// calls through, mirroring defaultSuperpowersClaudeRunner /
// defaultSuperpowersCommandRunner: production wiring is nlmGrillAnswerer,
// but nlmRun (unlike ClaudeRunner/CommandRunner) execs the real nlm binary
// unconditionally with no interface seam of its own, so tests substitute
// this var instead to stay off the network entirely.
var grillNotebookLMAnswerer = nlmGrillAnswerer

// nlmGrillRunFn is the swappable indirection for nlmGrillAnswerer's actual
// `nlm notebook query` invocation. nlmRun execs the real nlm binary with no
// interface seam of its own, so tests substitute this var to exercise
// nlmGrillAnswerer's response handling (empty output, quota errors, valid
// A1/A2 output) without ever touching the network.
var nlmGrillRunFn = nlmRun

// nlmGrillAuthGuard is the swappable indirection for nlmGrillAnswerer's
// pre-flight auth check. Production wiring is defaultNlmGrillAuthGuard, which
// invokes the registered CheckNotebookLMAuthAndRefresh action (the same
// check-then-refresh guard the scheduled auth-guardian agent uses,
// actions_notebooklm.go:161-176) instead of duplicating an inline
// `nlm login --check` call. Tests substitute this var to avoid invoking that
// action's own unconditional real nlm exec.
var nlmGrillAuthGuard = defaultNlmGrillAuthGuard

// defaultNlmGrillAuthGuard extracts the Blackboard GrillDesignArtifact's own
// ctx carries (it is a *btcore.BTContext[Blackboard], which satisfies
// context.Context via embedding) and drives CheckNotebookLMAuthAndRefresh
// through it. The action's own bb.Result/bb.Outcome writes are saved and
// restored around the call so they don't clobber GrillDesignArtifact's — this
// is purely an auth side-effect (and possible `nlm login` refresh), not a
// change GrillDesignArtifact's caller should see reflected in bb.Result. When
// ctx carries no Blackboard (e.g. a unit test calling nlmGrillAnswerer with a
// bare context.Context) the guard is skipped rather than panicking; output-
// based checks (nlmGrillUnavailable) still apply to whatever the query call
// returns.
func defaultNlmGrillAuthGuard(ctx context.Context) error {
	btctx, ok := ctx.(*btcore.BTContext[Blackboard])
	if !ok || btctx == nil || btctx.Blackboard == nil {
		return nil
	}
	act := GetAction("CheckNotebookLMAuthAndRefresh")
	if act == nil {
		return nil
	}
	return runGrillAuthGuardAction(btctx, act)
}

// runGrillAuthGuardAction invokes act (production: the registered
// CheckNotebookLMAuthAndRefresh action) against btctx, saving and restoring
// the Blackboard's Result/Outcome around the call so a check-then-refresh
// side effect never clobbers GrillDesignArtifact's own bb.Result/bb.Outcome.
// Extracted from defaultNlmGrillAuthGuard so this preserve/restore behavior
// is directly unit-testable with a fake ActionFunc — the real
// CheckNotebookLMAuthAndRefresh execs the nlm binary unconditionally with no
// seam of its own, so it cannot be driven in tests.
func runGrillAuthGuardAction(btctx *btcore.BTContext[Blackboard], act ActionFunc) error {
	bb := btctx.Blackboard
	prevResult, prevOutcome := bb.Result, bb.Outcome
	code := act(btctx)
	outcome := bb.Outcome
	bb.Result, bb.Outcome = prevResult, prevOutcome
	if code < 0 || outcome == "failure" {
		return errAnswererUnavailable
	}
	return nil
}

func nlmGrillAnswerer(ctx context.Context, batch []grillQuestion) (map[int]string, error) {
	if len(batch) == 0 {
		return map[int]string{}, nil
	}
	if err := nlmGrillAuthGuard(ctx); err != nil {
		return nil, errAnswererUnavailable
	}

	var prompt strings.Builder
	prompt.WriteString("Answer each question below using only the notebook sources. Respond in EXACTLY this format, one line per question:\nA1: <answer>\nA2: <answer>\n...\nIf a question cannot be answered from the sources, respond \"A<n>: UNKNOWN\".\n\n")
	for i, q := range batch {
		fmt.Fprintf(&prompt, "Q%d [%s]: %s\n", i+1, q.Branch, q.Text)
	}

	out := nlmGrillRunFn(180*time.Second, "notebook", "query", "--json", "--timeout", "150", defaultNotebook, prompt.String())
	if nlmGrillUnavailable(out) {
		return nil, errAnswererUnavailable
	}
	if strings.TrimSpace(out) == "" {
		// Empty/whitespace output is NotebookLM producing nothing at all —
		// treat it the same as an unavailable answerer instead of "0
		// answers found", so resolveGrillQuestions stops sending it later
		// batches rather than burning them against a broken answerer.
		return nil, errAnswererUnavailable
	}

	answer := extractNotebookLMAnswer(out)
	parsed := parseNumberedAnswers(answer)
	if len(parsed) == 0 {
		// Non-empty output but zero "A<n>:" lines means the response did not
		// conform to the requested format at all (prose, refusal, garbage) —
		// again an answerer-unavailable condition, not "0 answers found".
		return nil, errAnswererUnavailable
	}
	result := map[int]string{}
	for i := range batch {
		text, ok := parsed[i+1]
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" || strings.EqualFold(text, "UNKNOWN") {
			continue
		}
		result[i] = text
	}
	return result, nil
}

// nlmGrillUnavailable reports whether nlm CLI output indicates the
// NotebookLM answerer cannot be trusted to have produced a real answer: auth
// failures (via isGoapNotebookLMFailure) or a quota/rate-limit/error
// signature. The marker list is deliberately narrow and case-insensitive —
// mirroring internal/reliability/errors.go's isRateLimitError precision — so
// a legitimate answer that merely discusses "quota" as a concept (e.g. "set a
// quota of 5 calls per batch") is never misclassified as unavailable. A bare
// "quota" substring match previously caused exactly that false positive.
func nlmGrillUnavailable(out string) bool {
	if isGoapNotebookLMFailure(out) {
		return true
	}
	lower := strings.ToLower(out)
	markers := []string{
		"resource_exhausted",
		"resource exhausted",
		"resourceexhausted",
		"quota exceeded",
		"rate limit",
		"error code 8",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// parseNumberedAnswers splits a "A1: ...\nA2: ...\n" formatted NotebookLM
// answer back into per-question text, keyed by the 1-based question number
// nlmGrillAnswerer assigned in its prompt.
func parseNumberedAnswers(text string) map[int]string {
	result := map[int]string{}
	re := regexp.MustCompile(`(?m)^A(\d+):\s*`)
	matches := re.FindAllStringSubmatchIndex(text, -1)
	for i, m := range matches {
		idx, err := strconv.Atoi(text[m[2]:m[3]])
		if err != nil {
			continue
		}
		start := m[1]
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		result[idx] = strings.TrimSpace(text[start:end])
	}
	return result
}
