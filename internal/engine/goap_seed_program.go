package engine

// Self-seeding backlog: when every multi-cycle program is complete, the
// cycle's tail proposes the NEXT program itself — research is asked for a
// PROGRAM/MILESTONEn proposal (the existing contract), every milestone is
// validated as file-scoped and actionable, and the program store persists it
// for the following cycle to pick up at [P0] head. One rule prevents
// pile-up: nothing is seeded while any program still has pending milestones.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/research"

	btcore "github.com/rvitorper/go-bt/core"
)

// seedProgramFetchFn obtains a program proposal for the given prompt. The
// default asks NotebookLM (quota-cached and budgeted at the nlmRun choke
// point) but ACCEPTS its answer only when it actually contains a parseable
// PROGRAM block; otherwise it falls through to Claude, which follows the
// exact PROGRAM/MILESTONEn format far more reliably. Var for test override.
//
// The earlier version returned the nlm answer whenever nlm merely SUCCEEDED,
// so an up-but-unhelpful nlm (prose without a PROGRAM: line) yielded nil at
// extractGoapProgram and never reached the Claude fallback — the self-seeder
// produced nothing for hours while the loop starved on stale catalog goals.
var seedProgramFetchFn = func(prompt string) string {
	out := nlmRun(180*time.Second, "notebook", "query", defaultNotebook, prompt)
	return chooseProgramProposal(out, func() string {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		res := defaultSuperpowersClaudeRunner.RunClaude(cctx, goapFusionRepo, prompt)
		if res.Err != nil {
			return ""
		}
		return res.Output
	})
}

// chooseProgramProposal accepts the nlm answer only when it actually contains
// a parseable PROGRAM block; otherwise it invokes claudeFn. This is the gate
// whose absence starved the self-seeder: an up-but-unhelpful nlm answer used
// to be returned as-is and never reached the Claude fallback.
func chooseProgramProposal(nlmOut string, claudeFn func() string) string {
	if !isGoapNotebookLMFailure(nlmOut) {
		answer := extractNotebookLMAnswer(nlmOut)
		if extractGoapProgram(answer) != nil {
			return answer
		}
	}
	return claudeFn()
}

func init() {
	RegisterCondition("NeedsFreshProgram", func(bb *Blackboard) bool {
		ps, err := research.OpenPrograms(goapProgramsPath)
		if err != nil {
			return false
		}
		return ps.Active() == nil
	})

	RegisterAction("SeedNextProgram", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		ps, err := research.OpenPrograms(goapProgramsPath)
		if err != nil {
			bb.Result += "\n\n## Backlog Seeding Skipped\n\nProgram store unreadable: " + err.Error()
			return 1
		}
		if ps.Active() != nil {
			bb.Result += "\n\n## Backlog Seeding Skipped\n\nA program is still active."
			return 1
		}

		answer := seedProgramFetchFn(buildSeedProgramPrompt(ps))
		spec := extractGoapProgram(answer)
		if spec == nil {
			bb.Result += "\n\n## Backlog Seeding Produced No Program\n\nResearch returned no PROGRAM/MILESTONE proposal this cycle; will retry next cycle."
			return 1
		}
		var rejected []string
		for _, m := range spec.Milestones {
			if !isValidProgramMilestone(m) {
				rejected = append(rejected, m)
			}
		}
		if len(rejected) > 0 || len(spec.Milestones) < 2 {
			bb.Result += fmt.Sprintf("\n\n## Backlog Seeding Rejected Proposal\n\nProgram %q has %d milestone(s); %d not actionable/file-scoped. Will retry next cycle.", spec.Title, len(spec.Milestones), len(rejected))
			return 1
		}
		persistGoapProgram(bb, spec, "auto-seed")
		bb.Result += fmt.Sprintf("\n\n## Backlog Seeded\n\nNew program %q with %d file-scoped milestones queued for the next cycle.", spec.Title, len(spec.Milestones))
		bb.Result += programContinueNote()
		return 1
	})
}

// isValidProgramMilestone is the acceptance gate for a seeded program's
// milestones — deliberately MORE tolerant than isActionableGoapGoal (which
// is tuned for terse goal-queue LINES with a 400-char cap): a program
// milestone is a richer proposal that legitimately runs longer and may open
// with a noun phrase. It requires only that the milestone names at least one
// Go file, does not open with a review/summary prose phrase, and is a
// sensible length. Over-strict validation here rejected otherwise-valid
// programs and left the self-seeder producing nothing (2026-07-04 16:xx).
func isValidProgramMilestone(m string) bool {
	m = strings.TrimSpace(m)
	if len(m) < 12 || len(m) > 600 {
		return false
	}
	if goapProseGoalRe.MatchString(m) {
		return false
	}
	return len(extractGoFilePaths(m)) > 0
}

// buildSeedProgramPrompt frames the proposal request with what has already
// been built so the next program expands the platform instead of repeating.
func buildSeedProgramPrompt(ps *research.ProgramStore) string {
	past := make([]string, 0, len(ps.Programs))
	for _, p := range ps.Programs {
		past = append(past, "- "+p.Title)
	}
	done := recentImplementedGoals(10)
	graph, _ := os.ReadFile(goapFusionGraphReport)
	return fmt.Sprintf(`You plan the next multi-cycle improvement program for the go-bt-evolve
behavior-tree agent platform (Go, packages under internal/).

Completed programs (do NOT repeat these):
%s

Recently implemented goals (do NOT re-propose):
- %s

Codebase context:
%s

Propose ONE new program that expands PLATFORM capabilities (prefer
internal/gardener, internal/evolution, internal/a2a, internal/domains,
internal/knowledge, internal/dashboard over the self-improvement pipeline's
own files). Return EXACTLY:
PROGRAM: <title>
MILESTONE1: <self-contained, independently verifiable step naming the repo-relative Go files it touches>
MILESTONE2..MILESTONE5: <further steps, 3-5 total, each naming Go files>

Rules: every milestone MUST name at least one repo-relative Go file path and
open with an imperative verb; milestones must be landable one per task with
tests; no documentation-only milestones.`,
		strings.Join(past, "\n"), strings.Join(done, "\n- "), truncateGoap(string(graph), 2500))
}
