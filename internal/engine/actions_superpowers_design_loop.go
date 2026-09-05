// Package engine — grill-driven design-improvement loop actions
// (spec: docs/superpowers/specs/2026-07-15-brainstorm-grill-loop-design.md).
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	btcore "github.com/rvitorper/go-bt/core"
)

func init() { registerSuperpowersDesignLoopActions() }

// validateDesignHeadings returns the required headings missing from content.
// (Task: also re-point ValidateDesignArtifact's inline loop at this helper.)
func validateDesignHeadings(content string) []string {
	var missing []string
	for _, h := range []string{"## Goal", "## Architecture", "## Acceptance Criteria", "## Test Strategy", "## Risks"} {
		if !strings.Contains(content, h) {
			missing = append(missing, h)
		}
	}
	return missing
}

func validateDesignAction(ctx *btcore.BTContext[Blackboard]) int {
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
	bb.Result = "## Design Validated"
	return 1
}

func registerSuperpowersDesignLoopActions() {
	RegisterAction("ValidateRevisedDesign", validateDesignAction)
	RegisterAction("ValidateSplitDesign", validateDesignAction)

	// ReviseDesignArtifact rewrites the design BODY from the previous grill
	// round's feedback; the Q&A appendix is append-only and preserved. A
	// failed Claude call no-ops (unchanged design) so the reviewer's
	// no-progress breaker — not a child failure — ends a stuck loop.
	RegisterAction("ReviseDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok || run.DesignPath == "" {
			bb.Result = "## Revise Design Failed\n\nNo Superpowers design path in run state."
			return -1
		}
		feedback, _ := bb.ChainState["review_feedback"].(string)
		if strings.TrimSpace(feedback) == "" {
			bb.Result = "## Revise Design Skipped\n\nNo grill feedback yet (round 1)."
			return 1
		}
		if run.Mode == SuperpowersModeDryRun {
			bb.Result = "## Revise Design (dry run)\n\nClaude call skipped."
			return 1
		}
		data, err := os.ReadFile(run.DesignPath)
		if err != nil {
			bb.Result = "## Revise Design Failed\n\n" + err.Error()
			return -1
		}
		body, appendix := splitDesignDocument(string(data))

		prompt := fmt.Sprintf(`Revise this design document based on the grill round results below. Incorporate every ANSWERED insight into the design body. For each OPEN CRITICAL question, either answer it from your knowledge of this repository or change the design so the risk it probes no longer exists — record which you did in the relevant section.

Rules:
- Output ONLY the revised design body markdown, starting with "# ".
- Keep the exact section headings: ## Goal, ## Architecture, ## Acceptance Criteria, ## Test Strategy, ## Risks.
- Do NOT output any "## Grill Q&A" section.

## Current design body

%s

## Grill round results

%s`, body, feedback)

		res := defaultSuperpowersClaudeRunner.RunClaude(context.Background(), run.WorktreePathOrRepo(), prompt)
		if res.Err != nil {
			bb.Result = "## Revise Design Degraded\n\nClaude revision failed; keeping design unchanged: " + res.Err.Error()
			bb.Outcome = "revise_claude_failed_noop"
			return 1
		}
		revised := strings.TrimSpace(res.Output)
		if !strings.HasPrefix(revised, "# ") || len(validateDesignHeadings(revised)) > 0 {
			bb.Result = "## Revise Design Degraded\n\nClaude output not a valid design body; keeping design unchanged."
			bb.Outcome = "revise_output_invalid_noop"
			return 1
		}
		// Defensive re-assembly: appendix always re-attached from the
		// pre-revision file, so a drifting rewrite can never eat the audit
		// trail.
		if err := os.WriteFile(run.DesignPath, []byte(revised+"\n"+appendix), 0o644); err != nil {
			bb.Result = "## Revise Design Failed\n\n" + err.Error()
			return -1
		}
		run.DesignRevision++
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		setSuperpowersRun(bb, run)
		bb.Result = fmt.Sprintf("## Design Revised\n\nRevision %d applied from grill feedback.", run.DesignRevision)
		return 1
	})

	// SplitDesignArtifact partitions an exhausted grill loop's design into a
	// clear scope (kept as design.md) and a deferred scope persisted as a
	// standalone follow-up spec + goap program for a later cycle to pick up.
	RegisterAction("SplitDesignArtifact", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok || run.DesignPath == "" {
			bb.Result = "## Split Design Failed\n\nNo Superpowers design path in run state."
			return -1
		}
		if run.Mode == SuperpowersModeDryRun {
			bb.Result = "## Split Design (dry run)\n\nClaude call skipped."
			return 1
		}
		data, err := os.ReadFile(run.DesignPath)
		if err != nil {
			bb.Result = "## Split Design Failed\n\n" + err.Error()
			return -1
		}
		body, appendix := splitDesignDocument(string(data))

		prompt := fmt.Sprintf(`This design has unresolved critical questions on these branches: %s.
Partition it. The CLEAR DESIGN keeps only scope NOT blocked by those branches (same headings: ## Goal, ## Architecture, ## Acceptance Criteria, ## Test Strategy, ## Risks). The FOLLOWUP is a standalone spec for the deferred scope including the open questions. The PROGRAM turns the followup into 2-5 milestones, each naming the repo-relative Go files it touches.
Output EXACTLY:
=== CLEAR DESIGN ===
<markdown>
=== FOLLOWUP ===
<markdown>
=== PROGRAM ===
PROGRAM: <title>
MILESTONE1: <milestone (files: path1,path2)>

## Design body

%s

## Grill Q&A history

%s`, strings.Join(run.OpenCriticalBranches, ", "), body, appendix)

		res := defaultSuperpowersClaudeRunner.RunClaude(context.Background(), run.WorktreePathOrRepo(), prompt)
		if res.Err != nil {
			bb.Result = "## Split Design Failed\n\n" + res.Err.Error()
			bb.Outcome = "split_claude_failed"
			return -1
		}
		clearScope, followup, programText, perr := parseSplitOutput(res.Output)
		if perr != nil {
			bb.Result = "## Split Design Failed\n\n" + perr.Error() + "\n\n" + truncateGoap(res.Output, 1500)
			bb.Outcome = "split_output_invalid"
			return -1
		}
		if strings.TrimSpace(clearScope) == "" || len(validateDesignHeadings(clearScope)) > 0 {
			bb.Result = "## Split Design Failed\n\nNo implementable clear scope remained."
			bb.Outcome = "split_nothing_clear"
			return -1
		}

		summary := fmt.Sprintf("\n## Grill Loop Summary\n\n- Rounds used: %d\n- Design revisions: %d\n- Deferred branches: %s\n- Follow-up spec: design-followup.md\n",
			run.GrillRound, run.DesignRevision, strings.Join(run.OpenCriticalBranches, ", "))
		if err := os.WriteFile(run.DesignPath, []byte(strings.TrimSpace(clearScope)+"\n"+summary+appendix), 0o644); err != nil {
			bb.Result = "## Split Design Failed\n\n" + err.Error()
			return -1
		}
		run.FollowupPath = filepath.Join(run.ArtifactDir, "design-followup.md")
		if err := os.WriteFile(run.FollowupPath, []byte(followup), 0o644); err != nil {
			bb.Result = "## Split Design Failed\n\n" + err.Error()
			return -1
		}

		pickup := "manual (no valid program milestones)"
		if spec := extractGoapProgram(programText); spec != nil {
			v := validateGoapProgramMilestones(spec.Milestones)
			if len(v.Valid) >= 1 {
				spec.Milestones = v.Valid
				persistGoapProgram(bb, spec, "design-followup")
				run.FollowupProgramID = spec.Title
				pickup = fmt.Sprintf("goap program %q (%d milestones)", spec.Title, len(v.Valid))
			}
		}
		if err := writeSuperpowersRunJSON(run); err != nil {
			bb.Result = err.Error()
			return -1
		}
		setSuperpowersRun(bb, run)
		bb.Result = fmt.Sprintf("## Design Split\n\nClear scope kept; deferred scope → %s; pickup: %s", run.FollowupPath, pickup)
		return 1
	})
}

// parseSplitOutput splits a SplitDesignArtifact Claude response into its
// three ordered sections using the fenced markers from the split protocol.
func parseSplitOutput(out string) (clearScope, followup, program string, err error) {
	const mClear, mFollow, mProg = "=== CLEAR DESIGN ===", "=== FOLLOWUP ===", "=== PROGRAM ==="
	ci, fi, pi := strings.Index(out, mClear), strings.Index(out, mFollow), strings.Index(out, mProg)
	if ci < 0 || fi < 0 || pi < 0 || ci >= fi || fi >= pi {
		return "", "", "", fmt.Errorf("split output missing ordered markers")
	}
	return out[ci+len(mClear) : fi], out[fi+len(mFollow) : pi], out[pi+len(mProg):], nil
}
