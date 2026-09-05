// Package engine — systematic-debugging phase actions (Task 11).
//
// These three actions expose Phases 1-3 of the Superpowers "systematic-debugging"
// skill as tree-visible leaves under the "SystematicDebugging" MemSequence
// (internal/domains/superpowers_workflow.go). Each is a single Claude Code
// call (mirroring the RED/GREEN/review phase-split actions in
// superpowers_task_executor.go / superpowers_task_review.go): resolve the run,
// build a phase-specific prompt quoting that phase's rules verbatim from
// systematic-debugging/SKILL.md, invoke Claude, persist the raw response as
// durable evidence, and fold it into the running ChainState["debug_findings"]
// trail so later phases (and the eventual TDD fix) see the accumulated
// investigation. Phase 4 (Implementation) is intentionally NOT one of these
// three actions — it is the existing "TDDTask" MemSequence the plan wires in
// after these three phases (see systematicDebugging in superpowers_workflow.go).
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerSuperpowersDebugActions()
}

// debugPhaseSpec describes one systematic-debugging phase driven by a single
// Claude call: its 1-based phase number (used for the debug-phase-N.md
// evidence filename), a short human label for bb.Result messages, and the
// prompt template embedding that phase's rules. The template takes exactly
// four %s substitutions in order: repo/worktree path, run task, current
// worktree `git diff` (evidence for "check recent changes"), and the prior
// accumulated ChainState["debug_findings"] text.
type debugPhaseSpec struct {
	Number int
	Label  string
	Prompt string
}

// debugPhase1RootCausePrompt quotes systematic-debugging/SKILL.md's Phase 1
// (Root Cause Investigation) verbatim: read errors, reproduce, check recent
// changes, gather evidence at component boundaries, trace data flow. The
// Iron Law — no fixes without root cause investigation first — is stated
// explicitly so this phase never degrades into a fix attempt.
const debugPhase1RootCausePrompt = `You are Claude Code performing Phase 1 (Root Cause Investigation) of systematic debugging (Superpowers "systematic-debugging" skill) on this Superpowers run's worktree.

The Iron Law: NO FIXES WITHOUT ROOT CAUSE INVESTIGATION FIRST. Do NOT propose or apply any fix in this phase.

Before attempting ANY fix:
1. Read Error Messages Carefully — don't skip past errors or warnings; read stack traces completely; note line numbers, file paths, error codes.
2. Reproduce Consistently — can you trigger it reliably? What are the exact steps? Does it happen every time? If not reproducible, gather more data, don't guess.
3. Check Recent Changes — what changed that could cause this? Check git diff, recent commits, new dependencies, config changes, environmental differences.
4. Gather Evidence in Multi-Component Systems — for each component boundary, log what data enters/exits, verify environment/config propagation, check state at each layer. Run once to gather evidence showing WHERE it breaks, then analyze the evidence to identify the failing component, then investigate that specific component.
5. Trace Data Flow — where does the bad value originate? What called this with the bad value? Keep tracing up until you find the source. Do not fix yet — fixing at the source happens in the later TDD phase.

Repo: %s
Task: %s

Current worktree diff (uncommitted state, evidence for "check recent changes"):
---
%s
---

Prior debug findings so far (may be empty for the first phase):
---
%s
---

Return findings only — no fixes. Report WHAT is broken and WHY, with evidence (error text, reproduction steps, data-flow trace).
`

// debugPhase2PatternAnalysisPrompt quotes systematic-debugging/SKILL.md's
// Phase 2 (Pattern Analysis) verbatim: find working examples, compare
// against references, identify every difference, understand dependencies.
const debugPhase2PatternAnalysisPrompt = `You are Claude Code performing Phase 2 (Pattern Analysis) of systematic debugging (Superpowers "systematic-debugging" skill) on this Superpowers run's worktree.

Find the pattern before fixing. Do NOT propose or apply any fix in this phase.

1. Find Working Examples — locate similar working code in the same codebase; what works that's similar to what's broken?
2. Compare Against References — if implementing a pattern, read the reference implementation COMPLETELY; don't skim; understand the pattern fully before applying it.
3. Identify Differences — what's different between the working and the broken code? List every difference, however small; don't assume "that can't matter".
4. Understand Dependencies — what other components does this need? What settings, config, environment does it assume?

Repo: %s
Task: %s

Current worktree diff (uncommitted state):
---
%s
---

Prior debug findings so far (Phase 1 root-cause investigation, and earlier):
---
%s
---

Return the working examples found, the differences identified, and the dependencies understood — no fixes yet.
`

// debugPhase3HypothesisTestPrompt quotes systematic-debugging/SKILL.md's
// Phase 3 (Hypothesis and Testing) verbatim: form a single hypothesis, test
// it minimally, verify before continuing, and admit uncertainty rather than
// guess.
const debugPhase3HypothesisTestPrompt = `You are Claude Code performing Phase 3 (Hypothesis and Testing) of systematic debugging (Superpowers "systematic-debugging" skill) on this Superpowers run's worktree.

Scientific method — form ONE hypothesis and test it minimally:
1. Form Single Hypothesis — state clearly: "I think X is the root cause because Y." Be specific, not vague.
2. Test Minimally — make the SMALLEST possible change to test the hypothesis; one variable at a time; don't fix multiple things at once.
3. Verify Before Continuing — did it work? Didn't work? Form a NEW hypothesis; don't add more fixes on top.
4. When You Don't Know — say "I don't understand X"; don't pretend to know; research more instead of guessing.

Repo: %s
Task: %s

Current worktree diff (uncommitted state):
---
%s
---

Prior debug findings so far (Phase 1 root-cause investigation + Phase 2 pattern analysis):
---
%s
---

Return the hypothesis, the minimal test performed to check it, and whether it was verified (or the new hypothesis, if it was not).
`

var (
	debugPhaseRootCause  = debugPhaseSpec{Number: 1, Label: "Root Cause Investigation", Prompt: debugPhase1RootCausePrompt}
	debugPhasePattern    = debugPhaseSpec{Number: 2, Label: "Pattern Analysis", Prompt: debugPhase2PatternAnalysisPrompt}
	debugPhaseHypothesis = debugPhaseSpec{Number: 3, Label: "Hypothesis and Testing", Prompt: debugPhase3HypothesisTestPrompt}
)

func registerSuperpowersDebugActions() {
	RegisterAction("DebugRootCauseInvestigation", superpowersDebugPhaseAction(debugPhaseRootCause))
	RegisterAction("DebugPatternAnalysis", superpowersDebugPhaseAction(debugPhasePattern))
	RegisterAction("DebugHypothesisTest", superpowersDebugPhaseAction(debugPhaseHypothesis))
}

// superpowersDebugPhase drives one systematic-debugging phase for run: it
// captures the worktree's current `git diff` as evidence context (mirroring
// superpowersTaskReview's diff capture in superpowers_task_review.go), makes
// one Claude call with the phase's prompt plus the prior accumulated
// findings, and returns the raw Claude output as this phase's evidence. The
// caller persists it to <run.ArtifactDir>/debug-phase-N.md and appends it to
// ChainState["debug_findings"]. Mirrors the superpowersTaskRed/
// superpowersTaskReview runner-injection pattern so it is directly testable
// with fake CommandRunner/ClaudeRunner implementations.
func superpowersDebugPhase(ctx context.Context, runner CommandRunner, claude ClaudeRunner, run *SuperpowersRun, phase debugPhaseSpec, priorFindings string) (evidence string, err error) {
	dir := run.WorktreePathOrRepo()
	diffRes := runner.Run(ctx, dir, "git", "diff")
	prompt := fmt.Sprintf(phase.Prompt, dir, run.Task, diffRes.Output, priorFindings)
	res := claude.RunClaude(ctx, dir, prompt)
	if res.Err != nil {
		return res.Output, fmt.Errorf("debug phase %d (%s) claude failed: %v\n%s", phase.Number, phase.Label, res.Err, res.Output)
	}
	return res.Output, nil
}

// superpowersDebugPhaseAction builds the RegisterAction closure for one debug
// phase: resolve the Superpowers run, short-circuit to a dry-run marker file
// (skipping Claude entirely — the sibling convention every other
// dry-run-aware Superpowers action follows, e.g. ensureSuperpowersTaskSetup),
// otherwise run the phase via superpowersDebugPhase against the
// default(-swappable-in-tests) runner/claude globals, write the evidence file,
// and append to ChainState["debug_findings"] on success.
func superpowersDebugPhaseAction(phase debugPhaseSpec) ActionFunc {
	return func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = fmt.Sprintf("## Debug %s Failed\n\nNo Superpowers run state.", phase.Label)
			return -1
		}
		if err := os.MkdirAll(run.ArtifactDir, 0o755); err != nil {
			bb.Result = fmt.Sprintf("## Debug %s Failed\n\n%v", phase.Label, err)
			return -1
		}
		evidencePath := filepath.Join(run.ArtifactDir, fmt.Sprintf("debug-phase-%d.md", phase.Number))
		priorFindings, _ := bb.ChainState["debug_findings"].(string)

		if run.Mode == SuperpowersModeDryRun {
			marker := fmt.Sprintf("DRY RUN: %s (phase %d) skipped; Claude Code not invoked.\n", phase.Label, phase.Number)
			if err := os.WriteFile(evidencePath, []byte(marker), 0o644); err != nil {
				bb.Result = fmt.Sprintf("## Debug %s Failed\n\n%v", phase.Label, err)
				return -1
			}
			bb.Result = fmt.Sprintf("## Debug %s Skipped (dry run)\n\nEvidence: `%s`", phase.Label, evidencePath)
			return 1
		}

		c, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		evidence, err := superpowersDebugPhase(c, defaultSuperpowersCommandRunner, defaultSuperpowersClaudeRunner, run, phase, priorFindings)
		_ = os.WriteFile(evidencePath, []byte(evidence), 0o644)
		if err != nil {
			bb.Result = fmt.Sprintf("## Debug %s Failed\n\n%v", phase.Label, err)
			return -1
		}
		bb.ChainState["debug_findings"] = priorFindings + fmt.Sprintf("\n\n## Phase %d: %s\n\n%s", phase.Number, phase.Label, evidence)
		bb.Result = fmt.Sprintf("## Debug %s Complete\n\nEvidence: `%s`", phase.Label, evidencePath)
		return 1
	}
}
