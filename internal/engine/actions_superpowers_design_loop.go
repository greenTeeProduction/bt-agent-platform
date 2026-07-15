// Package engine — grill-driven design-improvement loop actions
// (spec: docs/superpowers/specs/2026-07-15-brainstorm-grill-loop-design.md).
package engine

import (
	"context"
	"fmt"
	"os"
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
}
