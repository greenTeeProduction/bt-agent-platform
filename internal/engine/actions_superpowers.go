// Package engine — production Superpowers compatibility actions.
//
// The Superpowers production pipeline lives in actions_superpowers_prod.go and
// related typed runtime files. This file intentionally contains no placeholder
// phase actions, manual HITL sidecar writers, or fake TDD implementation nodes.
// It keeps only shared verification actions that are referenced by older trees.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerSuperpowersVerificationActions()
}

func registerSuperpowersVerificationActions() {
	RegisterAction("IdentifyFailingCheck", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if run, ok := getSuperpowersRun(bb); ok && len(run.Verification) > 0 {
			var failed []string
			for _, check := range run.Verification {
				if !check.Passed {
					failed = append(failed, fmt.Sprintf("%s: %s", check.Name, check.Command))
				}
			}
			if len(failed) == 0 {
				bb.Result = "## Verification Diagnosis\n\nNo failing Superpowers verification checks recorded."
				return 1
			}
			bb.Result = "## Verification Diagnosis\n\nFailing checks:\n- " + strings.Join(failed, "\n- ")
			return 1
		}
		results, _ := bb.ChainState["verification_results"].(map[string]bool)
		if len(results) == 0 {
			bb.Result = "## Verification Diagnosis Failed\n\nNo verification results are available."
			return -1
		}
		var failed []string
		for check, passed := range results {
			if !passed {
				failed = append(failed, check)
			}
		}
		if len(failed) == 0 {
			bb.Result = "## Verification Diagnosis\n\nAll recorded checks passed."
			return 1
		}
		bb.Result = "## Verification Diagnosis\n\nFailing checks:\n- " + strings.Join(failed, "\n- ")
		return 1
	})

	RegisterAction("RerunVerification", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		run, ok := getSuperpowersRun(bb)
		if !ok {
			bb.Result = "## Verification Retry Failed\n\nNo typed Superpowers run is available."
			return -1
		}
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		if err := VerifySuperpowersRunRuntime(c, run); err != nil {
			bb.Result = "## Verification Retry Failed\n\n" + err.Error()
			return -1
		}
		bb.Result = fmt.Sprintf("## Verification Retry Passed\n\nChecks: %d", len(run.Verification))
		return 1
	})

	RegisterAction("VerificationPassed", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if run, ok := getSuperpowersRun(bb); ok {
			if len(run.Verification) == 0 {
				bb.Result = "## Verification Status Failed\n\nNo verification checks recorded."
				return -1
			}
			for _, check := range run.Verification {
				if !check.Passed {
					bb.Result = fmt.Sprintf("## Verification Status Failed\n\nCheck %q failed.", check.Name)
					return -1
				}
			}
			bb.Result = "## Verification Status\n\nAll Superpowers verification checks passed."
			return 1
		}
		if passed, _ := bb.ChainState["verification_passed"].(bool); passed {
			bb.Result = "## Verification Status\n\nAll recorded verification checks passed."
			return 1
		}
		bb.Result = "## Verification Status Failed\n\nNo passing verification evidence found."
		return -1
	})

	RegisterAction("VerifyBinary", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		worktree := repoPathFromBlackboard(bb)
		candidates := []string{
			filepath.Join(worktree, "bt-agent"),
			filepath.Join(worktree, "bin", "bt-agent"),
			filepath.Join(worktree, "bin", "bt-agent-cli"),
			"/tmp/bt-agent-cli",
		}
		for _, p := range candidates {
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Size() > 0 {
				bb.Result = fmt.Sprintf("## Binary Verified\n\n`%s` (%d bytes)", p, info.Size())
				return 1
			}
		}
		bb.Result = "## Binary Verification Failed\n\nNo non-empty BT binary found in expected locations."
		return -1
	})

	RegisterAction("VerifyTestPass", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		worktree := repoPathFromBlackboard(bb)
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		cmd := "/usr/local/go/bin/go test ./internal/domains ./internal/engine -count=1 -run 'TestSuperpowersPipeline_ProductionContract|TestSuperpowersRuntime_ActionsRegistered|TestGoapFusion_Structure|TestValidateOutputQuality' -timeout 180s"
		res := runShellCommand(c, defaultSuperpowersCommandRunner, worktree, cmd)
		if res.Err != nil {
			bb.Result = "## Test Verification Failed\n\n" + formatCommandResult(res)
			return -1
		}
		bb.Result = "## Test Verification Passed\n\n" + formatCommandResult(res)
		return 1
	})

	RegisterAction("RunLint", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		worktree := repoPathFromBlackboard(bb)
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		res := runShellCommand(c, defaultSuperpowersCommandRunner, worktree, "/usr/local/go/bin/go vet ./internal/domains ./internal/engine")
		if res.Err != nil {
			bb.Result = "## Lint Failed\n\n" + formatCommandResult(res)
			bb.ChainState["lint_clean"] = false
			return -1
		}
		bb.ChainState["lint_clean"] = true
		bb.Result = "## Lint Passed\n\n" + formatCommandResult(res)
		return 1
	})

	RegisterAction("VerifyLintClean", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if clean, _ := bb.ChainState["lint_clean"].(bool); clean {
			bb.Result = "## Lint Verification Passed\n\nPrevious RunLint check recorded clean output."
			return 1
		}
		worktree := repoPathFromBlackboard(bb)
		c, cancel := superpowersCommandTimeout()
		defer cancel()
		res := runShellCommand(c, defaultSuperpowersCommandRunner, worktree, "/usr/local/go/bin/go vet ./internal/domains ./internal/engine")
		if res.Err != nil {
			bb.Result = "## Lint Verification Failed\n\n" + formatCommandResult(res)
			return -1
		}
		bb.Result = "## Lint Verification Passed\n\n" + formatCommandResult(res)
		return 1
	})
}

// VerifyScheduledGoapFusionInputs is the preflight guard for the unattended
// scheduled GOAP fusion cycle. Before the automatic research-to-implementation
// run proceeds, it confirms the cycle's required research inputs are readable —
// the vault research directory and the graphify report — so a scheduled run
// fails fast with a clear diagnosis instead of silently producing a plan from
// missing context.
func init() {
	RegisterAction("VerifyScheduledGoapFusionInputs", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var missing []string

		if info, err := os.Stat(goapFusionVaultDir); err != nil || !info.IsDir() {
			missing = append(missing, fmt.Sprintf("vault research directory `%s` is not readable: %v", goapFusionVaultDir, err))
		}
		if info, err := os.Stat(goapFusionGraphReport); err != nil || info.IsDir() {
			missing = append(missing, fmt.Sprintf("graphify report `%s` is not readable: %v", goapFusionGraphReport, err))
		}

		if len(missing) > 0 {
			bb.Result = "## Scheduled GOAP Fusion Preflight Failed\n\nRequired research inputs are unavailable:\n- " + strings.Join(missing, "\n- ")
			return -1
		}
		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Preflight Passed\n\nVault research: `%s`\nGraphify report: `%s`", goapFusionVaultDir, goapFusionGraphReport)
		return 1
	})
}

// VerifyScheduledGoapFusionResearchPresent is the preflight guard that protects
// the unattended scheduled GOAP fusion cycle against an empty research corpus.
// VerifyScheduledGoapFusionInputs only confirms the vault directory and graphify
// report exist; a vault directory that exists but contains zero research files
// would still pass it, letting a scheduled run silently produce a plan from no
// actual research. This guard closes that gap by requiring the vault research
// directory to contain at least one readable research file before the automatic
// research-to-implementation cycle proceeds.
func init() {
	RegisterAction("VerifyScheduledGoapFusionResearchPresent", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		entries, err := os.ReadDir(goapFusionVaultDir)
		if err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Research Preflight Failed\n\nVault research directory `%s` is not readable: %v", goapFusionVaultDir, err)
			return -1
		}

		var research []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(goapFusionVaultDir, e.Name())
			if info, statErr := os.Stat(path); statErr != nil || info.Size() == 0 {
				continue
			}
			research = append(research, e.Name())
		}

		if len(research) == 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Research Preflight Failed\n\nVault research directory `%s` exists but contains no readable research files; a scheduled run would produce a plan from no research.", goapFusionVaultDir)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Research Preflight Passed\n\n%d research file(s) present in `%s`", len(research), goapFusionVaultDir)
		return 1
	})
}

// VerifyScheduledGoapFusionRuntime is the preflight guard for the implementation
// runtime of the unattended scheduled GOAP fusion cycle. The input preflight
// (VerifyScheduledGoapFusionInputs) only confirms the research inputs are
// readable; before the automatic cycle commits to producing a Superpowers plan
// it must also confirm the implementation runtime is available — the
// go-bt-evolve repository working directory and the Claude Code binary used to
// implement findings — so a scheduled run fails fast with a clear diagnosis
// instead of producing a plan it can never implement.
func init() {
	RegisterAction("VerifyScheduledGoapFusionRuntime", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		var missing []string

		if info, err := os.Stat(goapFusionRepo); err != nil || !info.IsDir() {
			missing = append(missing, fmt.Sprintf("go-bt-evolve repository working directory `%s` is not readable: %v", goapFusionRepo, err))
		}
		if info, err := os.Stat(goapFusionClaudeBin); err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			missing = append(missing, fmt.Sprintf("Claude Code binary `%s` is not an executable file: %v", goapFusionClaudeBin, err))
		}

		if len(missing) > 0 {
			bb.Result = "## Scheduled GOAP Fusion Runtime Preflight Failed\n\nRequired implementation runtime is unavailable:\n- " + strings.Join(missing, "\n- ")
			return -1
		}
		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Runtime Preflight Passed\n\nRepository: `%s`\nClaude Code binary: `%s`", goapFusionRepo, goapFusionClaudeBin)
		return 1
	})
}

// VerifyScheduledGoapFusionGraphReportPresent is the preflight guard that
// protects the unattended scheduled GOAP fusion cycle against an empty graphify
// report. VerifyScheduledGoapFusionInputs only confirms the graphify report file
// exists (os.Stat, not a directory); a zero-byte or contentless graphify report
// would still pass it, letting a scheduled run silently derive its improvement
// gaps from an empty report. This guard closes that gap by requiring the
// graphify report to contain readable content before the automatic
// research-to-implementation cycle proceeds — the report-content analogue of the
// VerifyScheduledGoapFusionResearchPresent vault-content guard.
func init() {
	RegisterAction("VerifyScheduledGoapFusionGraphReportPresent", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		b, err := os.ReadFile(goapFusionGraphReport)
		if err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graph Report Preflight Failed\n\nGraphify report `%s` is not readable: %v", goapFusionGraphReport, err)
			return -1
		}

		if len(strings.TrimSpace(string(b))) == 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graph Report Preflight Failed\n\nGraphify report `%s` exists but contains no readable content; a scheduled run would derive its improvement gaps from an empty report.", goapFusionGraphReport)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graph Report Preflight Passed\n\n%d bytes of content present in `%s`", len(b), goapFusionGraphReport)
		return 1
	})
}

// VerifyScheduledGoapFusionToolchain is the preflight guard for the Go toolchain
// the unattended scheduled GOAP fusion cycle depends on. The runtime guard
// (VerifyScheduledGoapFusionRuntime) only confirms the repository working
// directory and the Claude Code binary are available; the cycle's build and TDD
// verification step additionally shells out to the hardcoded Go toolchain
// (goapFusionGoBin), so a scheduled run could pass every other preflight yet
// still fail at verification when that toolchain is missing or not executable.
// This guard closes that gap by requiring the Go toolchain binary to be an
// executable file before the automatic research-to-implementation cycle
// proceeds — the verification-toolchain analogue of the runtime guard.
func init() {
	RegisterAction("VerifyScheduledGoapFusionToolchain", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		if info, err := os.Stat(goapFusionGoBin); err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Toolchain Preflight Failed\n\nGo toolchain binary `%s` is not an executable file: %v", goapFusionGoBin, err)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Toolchain Preflight Passed\n\nGo toolchain: `%s`", goapFusionGoBin)
		return 1
	})
}

// VerifyScheduledGoapFusionPlansWritable is the preflight guard for the plan
// output location the unattended scheduled GOAP fusion cycle writes to. The
// existing guards confirm the cycle's inputs (vault research, graphify report)
// and its implementation runtime (repo, Claude Code, Go toolchain) are
// available, but nothing confirms the cycle can persist its output: the cycle
// writes a Superpowers implementation plan and, on an incomplete Claude run,
// saves the failed patch into the plans directory (goapFusionPlansDir). A
// scheduled run could pass every other preflight yet still fail when that plans
// directory is missing or not writable, losing its plan and patch with no clear
// diagnosis. This guard closes that gap by requiring the plans directory to be a
// writable directory before the automatic research-to-implementation cycle
// proceeds — the output-location analogue of the input and runtime guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionPlansWritable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		info, err := os.Stat(goapFusionPlansDir)
		if err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Plans Preflight Failed\n\nPlans output directory `%s` is not an accessible directory: %v", goapFusionPlansDir, err)
			return -1
		}

		probe := filepath.Join(goapFusionPlansDir, ".goap-fusion-write-probe")
		if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Plans Preflight Failed\n\nPlans output directory `%s` is not writable: %v", goapFusionPlansDir, err)
			return -1
		}
		_ = os.Remove(probe)

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Plans Preflight Passed\n\nPlans output directory `%s` is a writable directory", goapFusionPlansDir)
		return 1
	})
}

// VerifyScheduledGoapFusionRunsWritable is the preflight guard for the
// Superpowers run-artifact output location the unattended scheduled GOAP fusion
// cycle writes its entire implement-verify-report output into. The existing
// writable-location guards each cover a distinct directory the cycle uses —
// VerifyScheduledGoapFusionPlansWritable (goapFusionPlansDir),
// VerifyScheduledGoapFusionVaultWritable (goapFusionVaultDir),
// VerifyScheduledGoapFusionSynthesesWritable (goapFusionSynthesesDir), and
// VerifyScheduledGoapFusionGraphOutputWritable (the graphify report's output
// directory) — but none covers the Superpowers runs directory
// (superpowersRunsDir). Every scheduled cycle's run is rooted at
// filepath.Join(superpowersRunsDir, id): the cycle's "write Superpowers
// implementation plan" step writes plan.md there, its verification step writes
// baseline-build.txt / worktree.patch / per-check outputs under the run's
// verification/ subdirectory, and its report step writes finish.md and run.json.
// A scheduled run could pass every other preflight yet still fail the moment it
// initializes its run when superpowersRunsDir is missing or not writable, losing
// its plan, verification evidence, and finish report with no early diagnosis.
// This guard closes that gap by requiring the Superpowers runs directory to be a
// writable directory before the automatic research-to-implementation cycle
// proceeds — the run-artifact-output analogue of the plans-, vault-, syntheses-,
// and graph-output-writable guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionRunsWritable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		info, err := os.Stat(superpowersRunsDir)
		if err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Runs Preflight Failed\n\nSuperpowers runs output directory `%s` is not an accessible directory: %v", superpowersRunsDir, err)
			return -1
		}

		probe := filepath.Join(superpowersRunsDir, ".goap-fusion-write-probe")
		if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Runs Preflight Failed\n\nSuperpowers runs output directory `%s` is not writable: %v", superpowersRunsDir, err)
			return -1
		}
		_ = os.Remove(probe)

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Runs Preflight Passed\n\nSuperpowers runs output directory `%s` is a writable directory", superpowersRunsDir)
		return 1
	})
}

// VerifyScheduledGoapFusionSynthesesPresent is the preflight guard that protects
// the unattended scheduled GOAP fusion cycle against a missing or empty
// research-syntheses corpus. The cycle's ReadVaultResearch step reads the
// syntheses directory (goapFusionSynthesesDir) first and newest-first, treating
// it as the highest priority research input, but it swallows a read error
// (os.ReadDir ... err == nil) — so a syntheses directory that is missing,
// unreadable, or contains zero synthesis files would silently degrade the
// research corpus and let a scheduled run produce a plan from the most recent
// research being absent, with no diagnosis. The existing
// VerifyScheduledGoapFusionResearchPresent guard only covers the vault directory
// itself, not this distinct syntheses subdirectory. This guard closes that gap
// by requiring the syntheses directory to contain at least one readable
// synthesis file before the automatic research-to-implementation cycle proceeds
// — the syntheses-content analogue of the vault-content and graph-report-content
// guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionSynthesesPresent", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		entries, err := os.ReadDir(goapFusionSynthesesDir)
		if err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Syntheses Preflight Failed\n\nResearch syntheses directory `%s` is not readable: %v", goapFusionSynthesesDir, err)
			return -1
		}

		var syntheses []string
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			path := filepath.Join(goapFusionSynthesesDir, e.Name())
			if info, statErr := os.Stat(path); statErr != nil || info.Size() == 0 {
				continue
			}
			syntheses = append(syntheses, e.Name())
		}

		if len(syntheses) == 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Syntheses Preflight Failed\n\nResearch syntheses directory `%s` exists but contains no readable synthesis files; a scheduled run would produce a plan with the most recent research absent.", goapFusionSynthesesDir)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Syntheses Preflight Passed\n\n%d synthesis file(s) present in `%s`", len(syntheses), goapFusionSynthesesDir)
		return 1
	})
}

// VerifyScheduledGoapFusionGraphifyTool is the preflight guard for the external
// graphify tool the unattended scheduled GOAP fusion cycle depends on. The
// runtime and toolchain guards confirm the Claude Code binary
// (VerifyScheduledGoapFusionRuntime) and the Go toolchain
// (VerifyScheduledGoapFusionToolchain) are available, but the cycle's
// RunGraphifyUpdate step shells out to the external `graphify` command to
// regenerate the graphify report from which the cycle derives every improvement
// gap. A scheduled run could pass every other preflight yet still fail when the
// graphify tool is not installed or not on PATH, leaving the cycle's gap
// analysis grounded in a stale report with no clear diagnosis. This guard closes
// that gap by requiring the graphify tool to be resolvable before the automatic
// research-to-implementation cycle proceeds — the graphify-tool analogue of the
// runtime and toolchain guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionGraphifyTool", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		path, err := exec.LookPath(goapFusionGraphifyTool)
		if err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graphify Tool Preflight Failed\n\nGraphify tool %q is not resolvable on PATH: %v; a scheduled run would derive its improvement gaps from a stale report.", goapFusionGraphifyTool, err)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graphify Tool Preflight Passed\n\nGraphify tool resolved to `%s`", path)
		return 1
	})
}

// VerifyScheduledGoapFusionNotebookLMTool is the preflight guard for the
// external NotebookLM (`nlm`) binary the unattended scheduled GOAP fusion cycle
// depends on. The runtime, toolchain, and graphify-tool guards confirm the
// Claude Code binary (VerifyScheduledGoapFusionRuntime), the Go toolchain
// (VerifyScheduledGoapFusionToolchain), and the graphify tool
// (VerifyScheduledGoapFusionGraphifyTool) are available, but the cycle's
// RunGoapFusionNotebookLMResearch step — which runs independent NotebookLM
// research before implementation — shells out to the `nlm` binary (nlmBin) and
// hard-fails ("refusing to proceed from stale vault research") when it is
// unavailable. A scheduled run could pass every other preflight yet still abort
// at the research step when that binary is missing or not executable, wasting
// the cycle with no early diagnosis. This guard closes that gap by requiring the
// NotebookLM binary to be an executable file before the automatic
// research-to-implementation cycle proceeds — the NotebookLM-tool analogue of
// the runtime, toolchain, and graphify-tool guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionNotebookLMTool", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		if info, err := os.Stat(nlmBin); err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion NotebookLM Tool Preflight Failed\n\nNotebookLM binary `%s` is not an executable file: %v; a scheduled run would abort at the research step and proceed from stale vault research.", nlmBin, err)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion NotebookLM Tool Preflight Passed\n\nNotebookLM binary: `%s`", nlmBin)
		return 1
	})
}

// VerifyScheduledGoapFusionSynthesesWritable is the preflight guard for the
// syntheses output location the unattended scheduled GOAP fusion cycle writes
// to. The cycle's RunGoapFusionNotebookLMResearch step writes a dedicated
// synthesis file (goap-fusion-notebooklm-<ts>.md) into the syntheses directory
// (goapFusionSynthesesDir) via writeString, and the immediately following
// ReadVaultResearch step ingests that newest synthesis as its highest-priority
// research input. The existing VerifyScheduledGoapFusionSynthesesPresent guard
// only confirms the directory is readable and already contains synthesis files;
// it does not confirm the cycle can persist a new one. The existing
// VerifyScheduledGoapFusionPlansWritable guard confirms writability but for a
// distinct directory (goapFusionPlansDir), not this syntheses directory. A
// scheduled run could pass every other preflight yet still fail when the
// syntheses directory is not writable, losing the freshly generated NotebookLM
// research with no clear diagnosis. This guard closes that gap by requiring the
// syntheses directory to be a writable directory before the automatic
// research-to-implementation cycle proceeds — the syntheses-output-location
// analogue of the VerifyScheduledGoapFusionPlansWritable guard.
func init() {
	RegisterAction("VerifyScheduledGoapFusionSynthesesWritable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		info, err := os.Stat(goapFusionSynthesesDir)
		if err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Syntheses Writable Preflight Failed\n\nSyntheses output directory `%s` is not an accessible directory: %v", goapFusionSynthesesDir, err)
			return -1
		}

		probe := filepath.Join(goapFusionSynthesesDir, ".goap-fusion-syntheses-write-probe")
		if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Syntheses Writable Preflight Failed\n\nSyntheses output directory `%s` is not writable: %v; a scheduled run would lose the freshly generated NotebookLM research.", goapFusionSynthesesDir, err)
			return -1
		}
		_ = os.Remove(probe)

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Syntheses Writable Preflight Passed\n\nSyntheses output directory `%s` is a writable directory", goapFusionSynthesesDir)
		return 1
	})
}

// VerifyScheduledGoapFusionGraphOutputWritable is the preflight guard for the
// graphify report OUTPUT directory the unattended scheduled GOAP fusion cycle
// regenerates its report into. The cycle's RunGraphifyUpdate step shells out to
// `graphify update .`, which regenerates the graphify report
// (goapFusionGraphReport) inside its output directory — the very report every
// improvement gap is derived from. The existing VerifyScheduledGoapFusionGraphifyTool
// guard only proves the `graphify` binary is resolvable on PATH, and
// VerifyScheduledGoapFusionGraphReportPresent only proves the report already
// holds content; neither confirms graphify can WRITE a fresh report. A scheduled
// run could pass every current preflight yet still fail when that output
// directory is missing or not writable, leaving RunGraphifyUpdate unable to
// refresh the report so the cycle silently derives its gaps from a stale report
// with no clear diagnosis. This guard closes that gap by requiring the graphify
// report's output directory to be a writable directory before the automatic
// research-to-implementation cycle proceeds — the graphify-output analogue of
// the plans-, vault-, and syntheses-writable guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionGraphOutputWritable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		graphOutputDir := filepath.Dir(goapFusionGraphReport)

		info, err := os.Stat(graphOutputDir)
		if err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graph Output Writable Preflight Failed\n\nGraphify report output directory `%s` is not an accessible directory: %v", graphOutputDir, err)
			return -1
		}

		probe := filepath.Join(graphOutputDir, ".goap-fusion-graph-output-write-probe")
		if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graph Output Writable Preflight Failed\n\nGraphify report output directory `%s` is not writable: %v; a scheduled run's RunGraphifyUpdate step could not refresh the report and would derive its gaps from a stale report.", graphOutputDir, err)
			return -1
		}
		_ = os.Remove(probe)

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Graph Output Writable Preflight Passed\n\nGraphify report output directory `%s` is a writable directory", graphOutputDir)
		return 1
	})
}

// VerifyScheduledGoapFusionNotebook is the preflight guard for the configured
// NotebookLM notebook id the unattended scheduled GOAP fusion cycle queries
// against. The VerifyScheduledGoapFusionNotebookLMTool guard only confirms the
// `nlm` binary is an executable file; it does not confirm a notebook is actually
// configured. The cycle's RunGoapFusionNotebookLMResearch step shells out to
// `nlm notebook query <defaultNotebook> ...` — so an empty or unset notebook id
// would let a scheduled run pass the binary check yet still query against no
// notebook, silently degrading the research corpus and producing a plan from
// stale vault research with no clear diagnosis. This guard closes that gap by
// requiring the configured notebook id to be non-empty before the automatic
// research-to-implementation cycle proceeds — the notebook-id analogue of the
// NotebookLM-tool guard.
func init() {
	RegisterAction("VerifyScheduledGoapFusionNotebook", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		if strings.TrimSpace(defaultNotebook) == "" {
			bb.Result = "## Scheduled GOAP Fusion Notebook Preflight Failed\n\nNotebookLM notebook id is empty; a scheduled run would query against no notebook and proceed from stale vault research."
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Notebook Preflight Passed\n\nNotebookLM notebook id: `%s`", defaultNotebook)
		return 1
	})
}

// VerifyScheduledGoapFusionGitTool is the preflight guard for the external `git`
// binary the unattended scheduled GOAP fusion cycle depends on. The cycle's
// Superpowers implementation step (RunSuperpowersClaudeImplementation) shells
// out to `git` for worktree isolation, diffing, and committing every
// improvement. The VerifyScheduledGoapFusionGitRemote guard runs `git
// remote get-url origin`, but if the `git` binary is missing entirely that guard
// fails with a misleading "origin remote is not configured" diagnosis rather than
// naming the real cause. A scheduled run could otherwise pass every tool guard
// (Claude Code, Go toolchain, graphify, NotebookLM) yet still fail at the very
// first git sync when `git` is not installed or not on PATH, wasting the cycle
// with no clear diagnosis. This guard closes that gap by requiring the `git`
// binary to be resolvable on PATH before the automatic research-to-implementation
// cycle proceeds — the git-binary analogue of the graphify-tool and NotebookLM-tool
// guards, and the prerequisite of the git-remote guard.
func init() {
	RegisterAction("VerifyScheduledGoapFusionGitTool", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		path, err := exec.LookPath("git")
		if err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Git Tool Preflight Failed\n\nGit binary %q is not resolvable on PATH: %v; a scheduled run would fail at the first git operation of the Superpowers implementation step.", "git", err)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Git Tool Preflight Passed\n\nGit binary resolved to `%s`", path)
		return 1
	})
}

// VerifyScheduledGoapFusionGitRemote is the preflight guard for the git `origin`
// remote the unattended scheduled GOAP fusion cycle depends on. The runtime guard
// (VerifyScheduledGoapFusionRuntime) only confirms the repository working
// directory and the Claude Code binary are available, but the cycle's
// Superpowers implementation step synchronizes its worktree against the
// repository state and needs a reachable origin remote. A scheduled run
// could pass every current preflight yet still fail at the fetch/pull sync
// (goap_fusion_preflight_failed) — or silently degrade at push — when the
// `origin` remote is unconfigured or unreachable, wasting the cycle with no early
// diagnosis. This guard closes that gap by requiring the repository's `origin`
// remote to be configured before the automatic research-to-implementation cycle
// proceeds — the git-remote analogue of the runtime and toolchain guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionGitRemote", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		if info, err := os.Stat(goapFusionRepo); err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Git Remote Preflight Failed\n\ngo-bt-evolve repository working directory `%s` is not readable: %v", goapFusionRepo, err)
			return -1
		}

		c, cancel := superpowersCommandTimeout()
		defer cancel()
		res := runShellCommand(c, defaultSuperpowersCommandRunner, goapFusionRepo, "git remote get-url origin")
		if res.Err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Git Remote Preflight Failed\n\nGit `origin` remote is not configured in `%s`; a scheduled run would fail at the fetch/pull sync or push step:\n\n%s", goapFusionRepo, formatCommandResult(res))
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Git Remote Preflight Passed\n\nGit `origin` remote configured in `%s`:\n\n%s", goapFusionRepo, strings.TrimSpace(res.Output))
		return 1
	})
}

// VerifyScheduledGoapFusionRejectedContextLedger is the preflight guard that
// protects the continuous self-improving loop runner against safety-drift
// regression. The prior guards protect a single cycle's inputs, runtime, and
// external tools, but the loop runner introduces a distinct hazard: because it
// re-runs the research-to-implementation cycle indefinitely, a later iteration
// can generate a high-fitness improvement that re-admits a previously rejected
// unsafe context — the "Safety Drift" / "Activity-Progress Confusion" failure
// mode the Experience-Grounded Monotonicity Auditor exists to prevent [Source
// 207, 214, 215, 250]. Enforcing the Monotonicity Invariant requires the loop
// runner to replay a persistent corpus of known rejected unsafe contexts (the
// rejected-context ledger) against every new candidate; if that ledger is
// missing or unreadable, the loop has no historical safety-regression kernel and
// silently relaxes previously patched security gates with no diagnosis. This
// guard closes that gap by requiring the rejected-context ledger to be a
// readable file with content before the continuous loop runner proceeds — the
// monotonicity-ledger analogue of the input, runtime, and tool guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionRejectedContextLedger", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		b, err := os.ReadFile(goapFusionRejectedLedger)
		if err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Rejected-Context Ledger Preflight Failed\n\nRejected-context ledger `%s` is not readable: %v; the continuous loop runner would have no historical safety-regression kernel and could silently re-admit a previously rejected unsafe context.", goapFusionRejectedLedger, err)
			return -1
		}

		if len(strings.TrimSpace(string(b))) == 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Rejected-Context Ledger Preflight Failed\n\nRejected-context ledger `%s` exists but contains no readable entries; the continuous loop runner would replay an empty unsafe-context corpus and could re-admit a previously rejected unsafe context.", goapFusionRejectedLedger)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Rejected-Context Ledger Preflight Passed\n\n%d bytes of rejected-context entries present in `%s`", len(b), goapFusionRejectedLedger)
		return 1
	})
}

// VerifyScheduledGoapFusionVaultWritable is the preflight guard for the vault
// research directory the unattended scheduled GOAP fusion cycle writes its own
// analysis back into. The cycle's WriteFusionAnalysis step persists its per-run
// gap analysis (goap-fusion-analysis-<ts>.md) and a rolling pointer
// (goap-fusion-latest.md) directly into the vault directory (goapFusionVaultDir)
// via os.WriteFile — and the next scheduled run's ReadVaultResearch step ingests
// those files as part of its research corpus. The existing
// VerifyScheduledGoapFusionResearchPresent guard only confirms the vault
// directory is readable and already contains research files; it does not confirm
// the cycle can persist a new analysis. The existing
// VerifyScheduledGoapFusionPlansWritable and
// VerifyScheduledGoapFusionSynthesesWritable guards confirm writability but for
// distinct directories (goapFusionPlansDir, goapFusionSynthesesDir), not this
// vault directory. A scheduled run could pass every current preflight yet still
// fail when the vault directory is not writable, silently dropping its own
// analysis and starving the next run's research corpus with no clear diagnosis.
// This guard closes that gap by requiring the vault directory to be a writable
// directory before the automatic research-to-implementation cycle proceeds — the
// vault-output-location analogue of the plans- and syntheses-writable guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionVaultWritable", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		info, err := os.Stat(goapFusionVaultDir)
		if err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Vault Writable Preflight Failed\n\nVault research directory `%s` is not an accessible directory: %v", goapFusionVaultDir, err)
			return -1
		}

		probe := filepath.Join(goapFusionVaultDir, ".goap-fusion-vault-write-probe")
		if err := os.WriteFile(probe, []byte("probe"), 0o644); err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Vault Writable Preflight Failed\n\nVault research directory `%s` is not writable: %v; a scheduled run would silently drop its own analysis and starve the next run's research corpus.", goapFusionVaultDir, err)
			return -1
		}
		_ = os.Remove(probe)

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Vault Writable Preflight Passed\n\nVault research directory `%s` is a writable directory", goapFusionVaultDir)
		return 1
	})
}

// VerifyScheduledGoapFusionCircuitPolicy is the CIRCUITPOLICY (circuit-breaker)
// preflight guard that protects the continuous self-improving loop runner
// against "Activity-Progress Confusion" — the failure mode surfaced by the P0
// NotebookLM research goal, where the loop remains active by proposing
// syntactically valid but redundant patches that never advance the task goal.
// The prior VerifyScheduledGoapFusionRejectedContextLedger guard protects
// against safety-drift by replaying a corpus of rejected unsafe contexts, but
// nothing protects against the distinct hazard of the loop spinning on repeated
// state hashes or consecutive no-op patch proposals. Production-grade
// reliability requires a deterministic kernel-level circuit policy that monitors
// a bounded state-hash history window (goapFusionCircuitHistoryWindow) and, on
// detecting a repeated state hash or a run of consecutive no-op patches, halts
// the loop instead of wasting tokens indefinitely. This guard closes that gap by
// requiring the circuit-policy history window to be a positive, bounded value
// before the continuous loop runner proceeds — the circuit-breaker analogue of
// the rejected-context-ledger and input/runtime/tool guards.
func init() {
	RegisterAction("VerifyScheduledGoapFusionCircuitPolicy", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		if goapFusionCircuitHistoryWindow <= 0 {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Circuit Policy Preflight Failed\n\nCircuit-policy history window (%d) must be a positive, bounded value; the continuous loop runner would have no CIRCUITPOLICY window to detect repeated state hashes or consecutive no-op patches and could spin indefinitely.", goapFusionCircuitHistoryWindow)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Circuit Policy Preflight Passed\n\nCircuit-policy state-hash history window: %d", goapFusionCircuitHistoryWindow)
		return 1
	})
}

// goapFusionCircuitBreakerWindow is the single shared CIRCUITPOLICY bounded-window
// dedup that both EvaluateScheduledGoapFusionCircuitBreaker and
// RunScheduledGoapFusionLoop delegate to, so the two actions can never drift into
// enforcing different halt policies. It slices the recent state-hash history down
// to the most recent goapFusionCircuitHistoryWindow entries (the PatchBoard 3-hash
// window) and scans for the first state hash that repeats within that bounded
// window — the "Activity-Progress Confusion" cycle where the loop returned to a
// prior state without advancing the goal. It returns the bounded window it
// inspected, the repeated hash (empty when none repeats), and whether the breaker
// tripped. A future change to the window/dedup semantics made here reaches both
// callers at once.
func goapFusionCircuitBreakerWindow(hashes []string) (window []string, repeated string, tripped bool) {
	window = hashes
	if len(window) > goapFusionCircuitHistoryWindow {
		window = window[len(window)-goapFusionCircuitHistoryWindow:]
	}

	seen := make(map[string]struct{}, len(window))
	for _, h := range window {
		if _, dup := seen[h]; dup {
			return window, h, true
		}
		seen[h] = struct{}{}
	}
	return window, "", false
}

// goapFusionCircuitBreakerVerdict is the single source of truth for the
// CIRCUITPOLICY halt/continue *decision* — not merely the bounded-window dedup
// scan goapFusionCircuitBreakerWindow performs. It layers the "trip → HALT"
// decision on top of that scan so both EvaluateScheduledGoapFusionCircuitBreaker
// and RunScheduledGoapFusionLoop delegate their entire circuit-breaker verdict
// here rather than each re-deriving `halt` from `(window, repeated, tripped)`
// inline. It returns whether the breaker decides to HALT, the bounded window it
// inspected, and the repeated hash (empty when none repeats). A future change to
// what counts as a "trip" — the window/dedup semantics or the halt decision
// itself — is made once here and reaches both callers at once, closing the exact
// class of drift the extraction set out to eliminate. The loop runner then layers
// only its own runaway-loop backstop on top of this shared verdict.
func goapFusionCircuitBreakerVerdict(hashes []string) (halt bool, window []string, repeated string) {
	window, repeated, tripped := goapFusionCircuitBreakerWindow(hashes)
	return tripped, window, repeated
}

// goapFusionCircuitPolicyVerdict is the single source of truth for the *entire*
// CIRCUITPOLICY halt decision — folding BOTH halt conditions the P0 NotebookLM
// research goal requires into one verdict so EvaluateScheduledGoapFusionCircuitBreaker
// and RunScheduledGoapFusionLoop delegate their whole halt/continue decision here
// instead of each re-implementing `streak >= goapFusionMaxNoopPatchStreak` inline.
// It layers the consecutive-no-op-patch streak halt on top of the shared state-hash
// circuit-breaker verdict:
//
//   - the repeated-state-hash cycle within the bounded window (via
//     goapFusionCircuitBreakerVerdict) — the "Activity-Progress Confusion" cycle
//     where the loop returned to a prior state without advancing the goal; and
//   - a consecutive-no-op-patch streak at or over goapFusionMaxNoopPatchStreak —
//     the no-op tail the state-hash scan alone cannot catch, where every published
//     hash is distinct yet every proposed patch is a syntactically valid but empty
//     no-op that never advances the goal.
//
// The state-hash cycle takes precedence: when it decides HALT the repeated hash is
// reported with noopTripped=false; when the hashes are all distinct but the no-op
// streak trips, HALT is decided with noopTripped=true and no repeated hash. A
// future change to what counts as a trip — the window/dedup semantics, the `>=`
// bound, or the halt decision itself — is made once here and reaches both callers
// at once, closing the exact class of drift the extraction set out to eliminate.
func goapFusionCircuitPolicyVerdict(hashes []string, noopStreak int) (halt bool, window []string, repeated string, noopTripped bool) {
	halt, window, repeated = goapFusionCircuitBreakerVerdict(hashes)
	if halt {
		return true, window, repeated, false
	}
	if noopStreak >= goapFusionMaxNoopPatchStreak {
		return true, window, "", true
	}
	return false, window, "", false
}

// EvaluateScheduledGoapFusionCircuitBreaker is the deterministic kernel-level
// CIRCUITPOLICY evaluation that enforces the P0 NotebookLM research goal:
// detecting and halting state-transition cycles and repeated no-op patch
// proposals. The VerifyScheduledGoapFusionCircuitPolicy guard is only a config
// preflight — it checks that goapFusionCircuitHistoryWindow is positive, but it
// never inspects a running state-hash history and never actually halts the loop.
// This action closes that gap: given the loop's recent state-hash history
// (published on the blackboard under "goap_fusion_state_hashes"), it returns
// HALT (-1) when a state hash repeats within goapFusionCircuitHistoryWindow —
// the repeated-state cycle the PatchBoard 3-hash window is designed to catch —
// and CONTINUE (1) when the window shows only distinct, progress-making hashes.
func init() {
	RegisterAction("EvaluateScheduledGoapFusionCircuitBreaker", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		hashes := goapFusionStateHashes(bb)
		streak := goapFusionNoopPatchStreak(bb)

		// Delegate the *entire* CIRCUITPOLICY halt decision — the bounded-window dedup
		// scan AND the consecutive-no-op-patch streak — to the single shared verdict
		// both this breaker and RunScheduledGoapFusionLoop use, so the two can never
		// drift on what counts as a trip. The no-op streak halt (a DISTINCT run of
		// hashes where every proposed patch is a syntactically valid but empty no-op
		// that never advances the goal — the "Activity-Progress Confusion" tail the
		// bounded-window dedup never trips on) is now decided in the shared helper, not
		// re-implemented inline here.
		halt, window, repeated, noopTripped := goapFusionCircuitPolicyVerdict(hashes, streak)
		if halt {
			if noopTripped {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Circuit Breaker: HALT\n\nConsecutive no-op-patch streak reached: %d consecutive no-op patch proposals meet or exceed the CIRCUITPOLICY bound (%d). Even with every state hash distinct — so the bounded-window dedup never trips — the loop is proposing syntactically valid but empty patches that never advance the goal, the \"Activity-Progress Confusion\" tail. Halting instead of iterating on no-op patches indefinitely.", streak, goapFusionMaxNoopPatchStreak)
			} else {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Circuit Breaker: HALT\n\nRepeated state hash %q detected within the bounded history window (size %d); the continuous loop returned to a prior state without advancing the goal — the \"Activity-Progress Confusion\" cycle. Halting to avoid wasting tokens on redundant no-op patches.", repeated, goapFusionCircuitHistoryWindow)
			}
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Circuit Breaker: CONTINUE\n\nThe most recent %d state hash(es) are distinct and progress-making; no state-transition cycle or repeated no-op patch detected.", len(window))
		return 1
	})
}

// RunScheduledGoapFusionLoop is the continuous self-improving *loop runner* — the
// bounded, self-halting driver the whole preflight-guard and circuit-breaker
// apparatus in this package exists to protect. Every sibling action guards one
// input, tool, output location, or a single circuit-breaker evaluation; this
// action is the kernel that decides, before driving another research-to-
// implementation cycle, whether the loop may CONTINUE (1) or must HALT (-1). It
// composes two independent bounds over the loop's recent state-hash history
// (published on the blackboard under "goap_fusion_state_hashes"):
//
//   - the CIRCUITPOLICY circuit breaker — HALT when a state hash repeats within
//     the bounded goapFusionCircuitHistoryWindow, the "Activity-Progress
//     Confusion" cycle where the loop returned to a prior state without advancing
//     the goal [Source 207, 214, 215, 250]; and
//   - a finite runaway-loop backstop — HALT once the total published history
//     reaches goapFusionMaxLoopIterations, so the loop can never iterate unbounded
//     even when every state hash is distinct and the circuit breaker's window
//     never sees a repeat.
//
// Only a short window of distinct, progress-making hashes — under the backstop
// and with no repeat in the bounded window — lets the runner CONTINUE to drive
// the next cycle.
func init() {
	RegisterAction("RunScheduledGoapFusionLoop", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		hashes := goapFusionStateHashes(bb)
		streak := goapFusionNoopPatchStreak(bb)

		// CIRCUITPOLICY: delegate the *entire* halt decision — the bounded-window dedup
		// scan AND the consecutive-no-op-patch streak — to the same shared verdict
		// EvaluateScheduledGoapFusionCircuitBreaker uses, so the loop runner and the
		// dedicated breaker enforce one identical halt DECISION from a single source of
		// truth. The no-op streak halt (a DISTINCT run of hashes where neither the
		// repeated-hash breaker nor the runaway-loop backstop fires, yet every proposed
		// patch is a syntactically valid but empty no-op — the "Activity-Progress
		// Confusion" tail) is now decided in the shared helper, not re-implemented
		// inline here. The loop runner then layers only its own runaway-loop backstop
		// below on top of this shared verdict.
		halt, window, repeated, noopTripped := goapFusionCircuitPolicyVerdict(hashes, streak)
		if halt {
			if noopTripped {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Loop Runner: HALT\n\nConsecutive no-op-patch streak reached: %d consecutive no-op patch proposals meet or exceed the CIRCUITPOLICY bound (%d). Even with every state hash distinct — so neither the repeated-hash circuit breaker nor the runaway-loop backstop fires — the loop is proposing syntactically valid but empty patches that never advance the goal, the \"Activity-Progress Confusion\" tail. Halting instead of iterating on no-op patches indefinitely.", streak, goapFusionMaxNoopPatchStreak)
			} else {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Loop Runner: HALT\n\nCircuit breaker tripped: repeated state hash %q within the bounded history window (size %d); the continuous loop returned to a prior state without advancing the goal — the \"Activity-Progress Confusion\" cycle. Halting instead of driving another redundant no-op cycle.", repeated, goapFusionCircuitHistoryWindow)
			}
			return -1
		}

		// Runaway-loop backstop: even when every state hash is distinct — so the
		// circuit breaker's bounded window never sees a repeat — a bounded loop
		// runner must refuse to iterate forever.
		if len(hashes) >= goapFusionMaxLoopIterations {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Loop Runner: HALT\n\nRunaway-loop backstop reached: %d state hash(es) in the published history meet or exceed the finite backstop (%d). Even with every hash distinct the loop must self-halt rather than iterate unbounded — the \"iterate forever without advancing the goal\" tail of Activity-Progress Confusion.", len(hashes), goapFusionMaxLoopIterations)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Loop Runner: CONTINUE\n\nThe most recent %d state hash(es) are distinct and under the runaway-loop backstop (%d/%d); no state-transition cycle detected. Driving the next research-to-implementation cycle.", len(window), len(hashes), goapFusionMaxLoopIterations)
		return 1
	})
}

// PublishGoapFusionStateHash is the missing PRODUCER the whole CIRCUITPOLICY
// apparatus depends on. EvaluateScheduledGoapFusionCircuitBreaker and
// RunScheduledGoapFusionLoop both derive their entire halt/continue decision from
// bb.ChainState["goap_fusion_state_hashes"] (via goapFusionStateHashes), yet no
// registered action ever WROTE that key in production — so in a real scheduled
// cycle the history stayed permanently empty, the bounded window never saw a
// repeat, the loop runner always returned CONTINUE, and the "Activity-Progress
// Confusion" cycle the loop-runner apparatus exists to break could never be
// detected [Source 207, 214, 215, 250].
//
// This action closes that producer gap. The cycle's progress-relevant state is its
// prioritized goal queue — PrioritizeGoapGoals stores it under
// bb.ChainState["goap_fusion_goal_queue"] and HasNewGaps already treats an
// unchanged goal queue as "no progress." So this producer hashes that goal queue
// deterministically (identical goal queues → identical SHA-256 hash) and appends
// the hash to goap_fusion_state_hashes, preserving any prior history. Two
// consecutive cycles that re-derive the same goals append the same hash, and the
// circuit breaker / loop runner it feeds finally HALTs on the repeat; genuine goal
// progress produces a distinct hash and reads as advancement, not a cycle.
func init() {
	RegisterAction("PublishGoapFusionStateHash", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}

		// The prioritized goal queue is the progress-relevant state; hashing it
		// deterministically means identical goal queues collapse to the identical
		// state hash the repeated-state breaker relies on.
		queue, _ := bb.ChainState["goap_fusion_goal_queue"].(string)
		sum := sha256.Sum256([]byte(queue))
		hash := hex.EncodeToString(sum[:])

		// Append onto any prior state-hash history published on earlier ticks so the
		// bounded window accumulates across cycles rather than resetting each tick.
		history := append(goapFusionStateHashes(bb), hash)
		bb.ChainState["goap_fusion_state_hashes"] = history

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion State Hash Published\n\nDeterministic goal-queue state hash `%s` appended to the CIRCUITPOLICY history (window depth %d/%d). The circuit breaker and loop runner now have a real producer to detect the \"Activity-Progress Confusion\" cycle against.", hash, len(history), goapFusionCircuitHistoryWindow)
		return 1
	})
}

// goapFusionStateHashes extracts the continuous loop's recent state-hash history
// from the blackboard chain state under "goap_fusion_state_hashes", tolerating
// either a []string (the canonical publish form) or a []any of strings.
func goapFusionStateHashes(bb *Blackboard) []string {
	if bb == nil || bb.ChainState == nil {
		return nil
	}
	switch v := bb.ChainState["goap_fusion_state_hashes"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// goapFusionNoopPatchStreak extracts the continuous loop's current run of
// consecutive no-op patch proposals from the blackboard chain state under
// "goap_fusion_noop_patch_streak", tolerating the numeric forms a blackboard may
// carry (int, int64, or a JSON-decoded float64). It returns 0 when the key is
// absent or of an unexpected type, so a loop that has never published the streak
// is treated as making progress rather than halting.
func goapFusionNoopPatchStreak(bb *Blackboard) int {
	if bb == nil || bb.ChainState == nil {
		return 0
	}
	switch v := bb.ChainState["goap_fusion_noop_patch_streak"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// VerifyScheduledGoapFusionBuildTreeMaterialized is the preflight guard that
// closes the P0 NotebookLM research gap: the on-disk build directory the
// unattended scheduled GOAP fusion cycle compiles and TDD-verifies against must
// be materialized to match HEAD. When the main repository is bare
// (core.bare=true), applying a run fast-forwards the bare `master` ref
// (`git fetch . <branch>:master`) but never checks that tree out to the on-disk
// working files — updating a ref in a bare repo touches no file, so
// goapFusionRepo's tracked source stays frozen at whatever the working tree last
// held, arbitrarily many commits behind HEAD. The cycle's build and TDD
// verification step then compiles that stale tree and the deployed binary is
// built from source that does not match HEAD, so the loop's own committed fixes
// never reach the running code. The runtime and toolchain guards confirm the
// repository directory and Go toolchain exist but never confirm the on-disk tree
// matches HEAD. This guard closes that gap by materializing the build
// directory's tracked working tree to HEAD before the automatic
// research-to-implementation cycle proceeds. On a bare main repo it runs an
// explicit `git --git-dir=<dir> --work-tree=. checkout -f HEAD -- .` (a plain
// diff/checkout dies with "must be run in a work tree"), then verifies no
// tracked file still differs from HEAD; on a non-bare checkout it compares the
// on-disk tree against HEAD directly. Either way the cycle only proceeds once
// the on-disk tree matches HEAD, so it never silently builds a stale tree.
func init() {
	RegisterAction("VerifyScheduledGoapFusionBuildTreeMaterialized", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		if info, err := os.Stat(goapFusionRepo); err != nil || !info.IsDir() {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\ngo-bt-evolve build directory `%s` is not readable: %v", goapFusionRepo, err)
			return -1
		}

		// A bare main repo (core.bare=true) keeps an on-disk working tree, but
		// applying a run only fast-forwards the bare `master` ref
		// (`git fetch . <branch>:master`) — updating a ref in a bare repo touches no
		// file, so the tracked source stays frozen arbitrarily many commits behind
		// HEAD. A plain `git diff --name-only HEAD --` there dies with "this
		// operation must be run in a work tree" (exit 128), so the guard cannot even
		// observe the drift. Rather than delegating (a no-op that materializes
		// nothing and passes in exactly the stale-tree case this guard exists to
		// catch), materialize the on-disk tree to HEAD with an explicit
		// --git-dir/--work-tree checkout before the build+TDD step compiles it, then
		// confirm no tracked file differs from HEAD.
		if out, err := runGoapShell("git rev-parse --is-bare-repository"); err == nil && strings.TrimSpace(out) == "true" {
			gitDir, gderr := runGoapShell("git rev-parse --git-dir")
			if gderr != nil {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nMain repo `%s` is bare but its git directory could not be resolved to materialize the on-disk tree: %v\n\n%s", goapFusionRepo, gderr, strings.TrimSpace(gitDir))
				return -1
			}
			gd := strings.TrimSpace(gitDir)
			checkout := fmt.Sprintf("git --git-dir=%s --work-tree=. checkout -f HEAD -- .", gd)
			if co, coErr := runGoapShell(checkout); coErr != nil {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nMain repo `%s` is bare; could not materialize its on-disk tree to HEAD via `%s`: %v\n\n%s", goapFusionRepo, checkout, coErr, strings.TrimSpace(co))
				return -1
			}
			verify := fmt.Sprintf("git --git-dir=%s --work-tree=. diff --name-only HEAD --", gd)
			diff, derr := runGoapShell(verify)
			if derr != nil {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nMain repo `%s` is bare; materialized the on-disk tree to HEAD but could not verify it via `%s`: %v\n\n%s", goapFusionRepo, verify, derr, strings.TrimSpace(diff))
				return -1
			}
			if stale := strings.TrimSpace(diff); stale != "" {
				bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nMain repo `%s` is bare; materializing the on-disk tree to HEAD left tracked file(s) still differing from HEAD, so the build would compile stale source:\n\n%s", goapFusionRepo, stale)
				return -1
			}
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Materialized\n\nMain repo `%s` is bare (no ref-update materializes the working tree during apply); materialized its on-disk tree to HEAD via `%s` so the build+TDD step compiles HEAD, not a stale tree. No tracked file now differs from HEAD.", goapFusionRepo, checkout)
			return 1
		}

		c, cancel := superpowersCommandTimeout()
		defer cancel()
		res := runShellCommand(c, defaultSuperpowersCommandRunner, goapFusionRepo, "git diff --name-only HEAD --")
		if res.Err != nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nCould not compare the on-disk build tree in `%s` against HEAD; a bare (`core.bare=true`) repository has no materialized working tree, so the cycle would build stale source:\n\n%s", goapFusionRepo, formatCommandResult(res))
			return -1
		}

		if stale := strings.TrimSpace(res.Output); stale != "" {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Failed\n\nThe on-disk build tree in `%s` is not materialized to HEAD; the following tracked file(s) differ from the committed HEAD and would be compiled stale, so the deployed binary would not match HEAD:\n\n%s", goapFusionRepo, stale)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Build Tree Preflight Passed\n\nThe on-disk build tree in `%s` is materialized to HEAD; no tracked file differs from the committed HEAD.", goapFusionRepo)
		return 1
	})
}

// GoapFusionPreflightNode composes the scheduled GOAP fusion loop's Phase-0
// preflight as a runnable behavior-tree node. The Scheduled* guard actions are
// registered and unit-tested, yet none were wired into any composed tree, so a
// scheduled cycle could never execute them — most critically
// VerifyScheduledGoapFusionBuildTreeMaterialized, whose absence lets a bare main
// repo silently build a stale on-disk tree. This builder closes that gap: it
// returns a Sequence that runs the materializer guard before the cycle proceeds,
// so the guard actually executes in a scheduled run.
//
// The engine package cannot import internal/domains (where GoapFusionLoopTree
// lives) without an import cycle, so this builder lives here at the guards' own
// package and is meant to be embedded by the domains tree as its Phase-0
// preflight. It sequences the materializer guard — the concrete, observed
// defect — then the VerifyScheduledGoapFusionCircuitPolicy config guard, then the
// VerifyScheduledGoapFusionRejectedContextLedger safety-drift monotonicity guard,
// then the VerifyScheduledGoapFusionRuntime guard, then the
// VerifyScheduledGoapFusionToolchain Go-toolchain guard, then the
// VerifyScheduledGoapFusionPlansWritable plan-output-location guard, then the
// VerifyScheduledGoapFusionGitTool git-binary guard, then the
// VerifyScheduledGoapFusionGitRemote git-`origin`-remote guard, then the
// VerifyScheduledGoapFusionVaultWritable vault-output-location guard, all ahead of
// the bounded loop runner, so the scheduled cycle only drives another iteration
// after the on-disk tree is proven fresh, the loop runner's CIRCUITPOLICY history
// window is proven a positive, bounded value, the loop runner has consulted the
// circuit-breaker window / runaway-loop backstop, the persistent rejected-context
// ledger is proven present and readable so a high-fitness candidate cannot silently
// re-admit a previously rejected unsafe context (the "Safety Drift" failure mode),
// the Go toolchain the build+TDD step shells out to is proven an executable file,
// the plans directory the cycle writes its plan and failed patch into is proven
// a writable directory, the `git` binary the implementation step commits and
// publishes fixes with is proven resolvable on PATH, the `origin` remote the
// implementation step fetches/pulls and pushes against is proven configured, and
// the vault directory the cycle persists its per-run analysis back into is proven
// a writable directory so a cycle never discovers only at WriteFusionAnalysis that
// its vault output location is unwritable and starves the next run's corpus, the
// `graphify` tool the cycle's RunGraphifyUpdate step shells out to is proven
// resolvable on PATH so the cycle never gates on the loop runner only to discover
// at RunGraphifyUpdate that graphify is missing and derive its gaps from a stale
// report, and — immediately after that tool guard — the
// VerifyScheduledGoapFusionGraphReportPresent guard proves the graphify report holds
// readable content (not just that the binary is resolvable) so a scheduled cycle
// never derives its improvement gaps from a zero-byte or contentless report, and the
// `nlm` NotebookLM binary the cycle's RunGoapFusionNotebookLMResearch step
// shells out to is proven an executable file so a cycle never gates on the loop
// runner only to abort at the research step with a missing binary and degrade to
// stale vault research, and — immediately after that binary guard — the
// VerifyScheduledGoapFusionNotebook notebook-id guard proves a NotebookLM notebook
// id is actually configured so the research step never queries against no notebook
// and silently degrades to stale vault research.
func GoapFusionPreflightNode() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "GoapFusionPreflight",
		Description: "Phase-0 preflight for the scheduled GOAP fusion loop; materializes the on-disk build tree to HEAD, proves the circuit policy and rejected-context ledger, verifies the implementation runtime (repository working directory and Claude Code binary), proves the Go toolchain the build+TDD step uses is executable and the plans directory the cycle writes into is writable, then gates the cycle on the bounded loop runner before it builds and TDD-verifies another iteration.",
		Children: []evolution.SerializableNode{
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionBuildTreeMaterialized",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionCircuitPolicy",
			},
			// VerifyScheduledGoapFusionRejectedContextLedger returns HALT (-1) when the
			// rejected-context ledger is missing, unreadable, or empty — and that ledger
			// is confirmed absent on disk. Once GoapFusionLoopTree() adopts
			// WireGoapFusionLoopTree and this preflight goes live, a bare Action child
			// here would propagate that FAILURE straight to the enclosing hard Sequence
			// and HALT the loop on EVERY scheduled tick — a regression strictly worse
			// than a no-op. The safety-drift replay is best-effort enrichment: wrap it in
			// a Selector with an AlwaysSucceed fallback (mirroring the two NotebookLM
			// guards below) so the guard still runs and warns but its FAILURE is swallowed
			// rather than propagated to the preflight Sequence.
			{
				Type: "Selector",
				Name: "GoapFusionRejectedContextLedgerOptional",
				Children: []evolution.SerializableNode{
					{
						Type: "Action",
						Name: "VerifyScheduledGoapFusionRejectedContextLedger",
					},
					{
						Type: "AlwaysSucceed",
						Name: "GoapFusionRejectedContextLedgerNonFatal",
					},
				},
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionRuntime",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionToolchain",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionPlansWritable",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionGitTool",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionGitRemote",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionVaultWritable",
			},
			// PlansWritable and VaultWritable prove distinct output directories are
			// writable; neither covers the syntheses directory the cycle's
			// RunGoapFusionNotebookLMResearch step writes its freshly generated
			// synthesis into (and the following ReadVaultResearch step ingests as its
			// highest-priority research). Prove that syntheses output location is a
			// writable directory too, before the loop runner drives another iteration,
			// so a cycle never discovers only at RunGoapFusionNotebookLMResearch that
			// its syntheses output location is unwritable and loses that research.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionSynthesesWritable",
			},
			// The finer-grained ResearchPresent (vault holds ≥1 file) and
			// GraphReportPresent (report holds content) guards each refine this
			// foundational existence guard, which only confirms the vault research
			// directory and graphify report exist at all. Compose that foundational
			// guard here — ahead of the content refinements — so a scheduled cycle
			// proves its research inputs exist before it derives a plan from them.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionInputs",
			},
			// VaultWritable only proves the vault is a writable directory; a vault that
			// exists but holds zero research files would still pass it, letting a
			// scheduled cycle plan from an empty corpus. Prove the vault holds at least
			// one readable research file before the loop runner drives another iteration.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionResearchPresent",
			},
			// SynthesesWritable only proves the syntheses directory is a writable
			// directory; a syntheses directory that exists but holds zero synthesis
			// files would still pass it, letting a scheduled cycle plan from an absent
			// freshest research corpus (the cycle's ReadVaultResearch step reads the
			// syntheses directory first and newest-first but swallows a read error).
			// Prove the syntheses directory holds at least one readable synthesis file
			// before the loop runner drives another iteration — the syntheses-content
			// analogue of the ResearchPresent vault-content guard.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionSynthesesPresent",
			},
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionGraphifyTool",
			},
			// GraphifyTool only proves the `graphify` binary is resolvable on PATH; a
			// zero-byte or contentless report it produced would still pass it, letting a
			// scheduled cycle derive its improvement gaps from an empty report. Prove the
			// graphify report holds readable content before the loop runner drives another
			// iteration — the report-content analogue of the ResearchPresent vault guard.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionGraphReportPresent",
			},
			// GraphReportPresent only proves the report currently holds content; it
			// says nothing about whether the cycle's RunGraphifyUpdate step can refresh
			// that report. Once RunScheduledGoapFusionLoop decides CONTINUE, the cycle
			// regenerates the report inside its output directory — the very report every
			// improvement gap is derived from — so a run could pass every content guard
			// yet still fail at RunGraphifyUpdate when that output directory is missing
			// or unwritable, leaving the cycle to derive its gaps from a stale report.
			// Prove the graphify report's output directory is a writable directory
			// before the loop runner drives another iteration — the graphify-output
			// analogue of the PlansWritable/VaultWritable/SynthesesWritable guards.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionGraphOutputWritable",
			},
			// PlansWritable/VaultWritable/SynthesesWritable/GraphOutputWritable each
			// prove a distinct cycle output directory is writable, but none covers the
			// Superpowers runs directory (superpowersRunsDir) every scheduled run is
			// rooted under: its plan.md, verification evidence, and finish.md/run.json
			// all land there. Prove that runs directory is a writable directory too,
			// before the loop runner drives another iteration, so a scheduled run fails
			// fast with a clear diagnosis instead of losing its plan, verification
			// evidence, and finish report the moment it initializes its run — the
			// run-artifact-output analogue of the sibling writable-location guards.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionRunsWritable",
			},
			// The two NotebookLM guards are optional enrichment: an absent `nlm`
			// binary or an unset notebook id must NOT abort the scheduled cycle,
			// which still degrades cleanly to vault research. Each is Selector-wrapped
			// with an AlwaysSucceed fallback so the guard runs and warns, but its
			// FAILURE is swallowed rather than propagated to the preflight Sequence.
			{
				Type: "Selector",
				Name: "GoapFusionNotebookLMToolOptional",
				Children: []evolution.SerializableNode{
					{
						Type: "Action",
						Name: "VerifyScheduledGoapFusionNotebookLMTool",
					},
					{
						Type: "AlwaysSucceed",
						Name: "GoapFusionNotebookLMToolNonFatal",
					},
				},
			},
			{
				Type: "Selector",
				Name: "GoapFusionNotebookOptional",
				Children: []evolution.SerializableNode{
					{
						Type: "Action",
						Name: "VerifyScheduledGoapFusionNotebook",
					},
					{
						Type: "AlwaysSucceed",
						Name: "GoapFusionNotebookNonFatal",
					},
				},
			},
			// VerifyScheduledGoapFusionCircuitPolicy above is only a config guard —
			// it proves goapFusionCircuitHistoryWindow is positive but never inspects
			// the running state-hash history and never halts the loop. Compose the
			// deterministic kernel-level circuit-breaker evaluation here, as the last
			// gate before the loop runner, so the CIRCUITPOLICY verdict is an explicit,
			// observable BT gate: it HALTs the preflight fast on a detected
			// "Activity-Progress Confusion" cycle before RunScheduledGoapFusionLoop
			// drives another research-to-implementation iteration.
			{
				Type: "Action",
				Name: "EvaluateScheduledGoapFusionCircuitBreaker",
			},
			// The scheduled cycle validates candidate mutations by ticking trees
			// through the benchmark harnesses, which rely on the bb.Sandbox
			// short-circuit in actionForName to keep structural ticks from
			// dispatching real `nlm`/`git`/`claude` actions. That short-circuit is
			// the single engine-side mechanism the whole "don't burn quotas during
			// structural evaluation" defense rests on, yet no composed tree proved it
			// intact. Compose the deterministic kernel-level sandbox-invariant guard
			// here — the last gate before the loop runner — so a scheduled cycle
			// proves the Sandbox short-circuit still blocks real action dispatch
			// before RunScheduledGoapFusionLoop drives another benchmark-validating
			// iteration; on a broken invariant it HALTs the preflight fast the same
			// way its sibling input/runtime/tool guards do.
			{
				Type: "Action",
				Name: "VerifyScheduledGoapFusionSandbox",
			},
			{
				Type: "Action",
				Name: "RunScheduledGoapFusionLoop",
			},
			// The loop runner only DECIDES CONTINUE (SUCCESS) or HALT (FAILURE) over
			// the loop's recent state-hash history; on HALT the enclosing Sequence
			// short-circuits here and the cycle driver below never runs. Once the loop
			// runner decides CONTINUE and every guard ahead of it has passed, drive an
			// actual research-to-implementation iteration: RunScheduledGoapFusionCycle
			// reads the vault research and graphify report, identifies improvement
			// gaps, writes a Superpowers implementation plan, and implements it via the
			// Superpowers runtime. Composing it last — after RunScheduledGoapFusionLoop —
			// closes the "registered but unwired" gap for the loop's own driver: without
			// it a scheduled run would consult every gate, decide CONTINUE, and then do
			// nothing.
			{
				Type: "Action",
				Name: "RunScheduledGoapFusionCycle",
			},
		},
	}
}

// PrependGoapFusionPreflight is the integration seam the "goap-fusion-loop-runner"
// goal requires: it takes the production GoapFusionLoop_Main sequence's child list
// and returns a new list with GoapFusionPreflightNode() prepended as the first
// child, so a scheduled cycle materializes a fresh on-disk build tree and consults
// the bounded loop runner before it runs SetupFusionTools or anything else.
//
// The engine package cannot import internal/domains (import cycle), but
// domains -> engine is the safe direction, so this seam lives here at the guards'
// own package and GoapFusionLoopTree() embeds it without duplicating the Phase-0
// composition. It returns a freshly allocated slice and never mutates the caller's
// input.
func PrependGoapFusionPreflight(loopChildren []evolution.SerializableNode) []evolution.SerializableNode {
	prepended := make([]evolution.SerializableNode, 0, len(loopChildren)+1)
	prepended = append(prepended, GoapFusionPreflightNode())
	prepended = append(prepended, loopChildren...)
	return prepended
}

// PrependGoapFusionImplementationGate is the integration seam that gates the
// Superpowers implementation path on the CIRCUITPOLICY verdict: it takes the
// production ClaudeSuperpowersPath's child list (WriteSuperpowersImplementationPlan
// followed by the HumanApprovalGate wrapping RunSuperpowersClaudeImplementation) and
// returns a new list with the circuit-breaker evaluation and the bounded loop runner
// prepended as the first two children, in that order.
//
// The top-level GoapFusionPreflightNode() already gates the whole cycle, but nothing
// gates the implementation subtree itself: a non-progressing loop that reached the
// implementation path could still shell out to Claude Code. Prepending
// EvaluateScheduledGoapFusionCircuitBreaker then RunScheduledGoapFusionLoop
// immediately ahead of the implementation ensures a detected "Activity-Progress
// Confusion" cycle HALTs the path before RunSuperpowersClaudeImplementation runs.
//
// Like PrependGoapFusionPreflight, it lives here at the guards' own package (the
// domains -> engine direction is safe) so GoapFusionLoopTree()'s ClaudeSuperpowersPath
// embeds it without duplicating the composition. It returns a freshly allocated slice
// and never mutates the caller's input.
func PrependGoapFusionImplementationGate(implChildren []evolution.SerializableNode) []evolution.SerializableNode {
	gated := make([]evolution.SerializableNode, 0, len(implChildren)+2)
	gated = append(gated,
		evolution.SerializableNode{
			Type: "Action",
			Name: "EvaluateScheduledGoapFusionCircuitBreaker",
		},
		evolution.SerializableNode{
			Type: "Action",
			Name: "RunScheduledGoapFusionLoop",
		},
	)
	gated = append(gated, implChildren...)
	return gated
}

// WireGoapFusionLoopTree is the whole-tree integration seam the
// "goap-fusion-loop-runner" goal still lacks: a single entry point that returns a
// fully-wired copy of the production GOAP fusion loop tree so the domains
// GoapFusionLoopTree() can adopt BOTH the Phase-0 preflight AND the Claude
// implementation circuit gate in one call, instead of applying the two list-level
// primitives (PrependGoapFusionPreflight and PrependGoapFusionImplementationGate)
// separately to two hand-isolated child lists — the manual, error-prone wiring the
// recorded "registered but unwired" gap keeps re-opening.
//
// Given the production loop tree it (1) prepends GoapFusionPreflightNode() as the
// tree's first child (via PrependGoapFusionPreflight) and (2) rewrites the
// "ClaudeSuperpowersPath" implementation subtree's children via
// PrependGoapFusionImplementationGate, so a detected Activity-Progress Confusion
// cycle HALTs the path before RunSuperpowersClaudeImplementation shells out to
// Claude Code.
//
// The engine package cannot import internal/domains (import cycle), but
// domains -> engine is the safe direction, so this whole-tree wiring seam lives
// here at the guards' own package. It rebuilds every node's child slice as it
// descends, so it returns a freshly allocated tree and never mutates the caller's
// input.
func WireGoapFusionLoopTree(tree evolution.SerializableNode) evolution.SerializableNode {
	var rewrite func(n evolution.SerializableNode) evolution.SerializableNode
	rewrite = func(n evolution.SerializableNode) evolution.SerializableNode {
		if len(n.Children) > 0 {
			rewritten := make([]evolution.SerializableNode, len(n.Children))
			for i, c := range n.Children {
				rewritten[i] = rewrite(c)
			}
			n.Children = rewritten
		}
		if n.Name == "ClaudeSuperpowersPath" && !goapFusionImplementationGateWired(n.Children) {
			n.Children = PrependGoapFusionImplementationGate(n.Children)
		}
		// Splice the goal-queue state-hash producer in wherever the Phase-4
		// PrioritizeGoapGoals node lives, so every scheduled cycle hashes the freshly
		// prioritized goals into the CIRCUITPOLICY history the circuit breaker and
		// loop runner read. Doing it during the descent (rather than only at the top
		// level) keeps the seam robust to the exact nesting depth of PrioritizeGoapGoals.
		n.Children = spliceGoapFusionStateHashProducer(n.Children)
		return n
	}

	wired := rewrite(tree)
	if !goapFusionPreflightWired(wired.Children) {
		wired.Children = PrependGoapFusionPreflight(wired.Children)
	}
	return wired
}

// goapFusionPreflightWired reports whether loopChildren already begins with the
// Phase-0 GoapFusionPreflight node, so WireGoapFusionLoopTree stays idempotent and
// a re-invocation never double-prepends the preflight ahead of an already-wired tree.
func goapFusionPreflightWired(loopChildren []evolution.SerializableNode) bool {
	return len(loopChildren) > 0 && loopChildren[0].Name == "GoapFusionPreflight"
}

// spliceGoapFusionStateHashProducer returns a copy of children with a
// PublishGoapFusionStateHash Action inserted immediately after PrioritizeGoapGoals,
// so the goal queue that PrioritizeGoapGoals builds is hashed into the CIRCUITPOLICY
// state-hash history before the ExecutionRouter consumes it. Without this producer
// bb.ChainState["goap_fusion_state_hashes"] stays permanently empty in a real
// scheduled cycle and the loop runner always returns CONTINUE — the very
// "Activity-Progress Confusion" backstop the loop-runner apparatus exists to break.
//
// It is idempotent (a producer already sitting right after PrioritizeGoapGoals is
// left untouched) and never mutates the caller's slice: it returns children unchanged
// when there is nothing to splice, otherwise a freshly allocated slice.
func spliceGoapFusionStateHashProducer(children []evolution.SerializableNode) []evolution.SerializableNode {
	idx := -1
	for i := range children {
		if children[i].Name == "PrioritizeGoapGoals" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return children
	}
	if idx+1 < len(children) && children[idx+1].Name == "PublishGoapFusionStateHash" {
		return children
	}
	spliced := make([]evolution.SerializableNode, 0, len(children)+1)
	spliced = append(spliced, children[:idx+1]...)
	spliced = append(spliced, evolution.SerializableNode{
		Type: "Action",
		Name: "PublishGoapFusionStateHash",
	})
	spliced = append(spliced, children[idx+1:]...)
	return spliced
}

// goapFusionImplementationGateWired reports whether implChildren already begins with
// the circuit-breaker + bounded-loop-runner pair PrependGoapFusionImplementationGate
// prepends, so wiring an already-gated ClaudeSuperpowersPath is a no-op.
func goapFusionImplementationGateWired(implChildren []evolution.SerializableNode) bool {
	return len(implChildren) >= 2 &&
		implChildren[0].Name == "EvaluateScheduledGoapFusionCircuitBreaker" &&
		implChildren[1].Name == "RunScheduledGoapFusionLoop"
}

func repoPathFromBlackboard(bb *Blackboard) string {
	if run, ok := getSuperpowersRun(bb); ok {
		return run.WorktreePathOrRepo()
	}
	if bb != nil && bb.ChainState != nil {
		if p, _ := bb.ChainState["worktree_path"].(string); p != "" {
			return p
		}
	}
	return superpowersRepoDir
}

// VerifyScheduledGoapFusionSandbox is the deterministic kernel-level preflight
// that proves the single engine-side mechanism the whole "don't burn quotas
// during structural evaluation" defense rests on is intact before the unattended
// scheduled GOAP fusion cycle runs its benchmark-based structural validation.
//
// The scheduled cycle validates candidate mutations by ticking trees through the
// benchmark harnesses, and commit 2bea250 set `Sandbox: true` on those harness
// Blackboards so structural ticks are short-circuited before any real
// `nlm`/`git`/`claude` action can dispatch (tree.go actionForName: when
// bb.Sandbox is true it returns a `[sandbox] name` stub instead of the real
// registered implementation). If a refactor ever dropped that `bb.Sandbox` guard,
// every benchmark harness would silently dispatch real side-effecting actions
// again — the exact 11-hour/quota-burning defect commit 2bea250 set out to
// eliminate, now undetected.
//
// This guard closes that gap the same way its sibling input/runtime/tool guards
// do: it dispatches a genuinely-registered real action through a sandboxed probe
// Blackboard and proves the sandbox short-circuit returned the `[sandbox]` stub
// rather than the real implementation. It returns PASS (1) when the sandbox
// invariant holds and FAIL (-1) with a clear diagnosis if a real action would
// dispatch under sandbox.
func init() {
	RegisterAction("VerifyScheduledGoapFusionSandbox", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard

		// The probe target must itself be a genuinely-registered real action, so
		// the sandbox short-circuit is proven to be blocking something real rather
		// than falling through to the permissive unknown-action fallback.
		const probeAction = "VerifyScheduledGoapFusionInputs"
		if GetAction(probeAction) == nil {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Sandbox Preflight Failed\n\nSandbox invariant unverifiable: probe action `%s` is not registered, so the guard cannot prove the Sandbox short-circuit blocks a real registered action.", probeAction)
			return -1
		}

		// Dispatch the real action through a sandboxed Blackboard. When the
		// bb.Sandbox short-circuit is intact, actionForName returns the
		// `[sandbox] name` stub — appending exactly that marker to Results and
		// never executing the real implementation.
		probe := &Blackboard{Sandbox: true}
		status := probe.actionForName(probeAction)(btcore.NewBTContext(ctx, probe))

		wantStub := "[sandbox] " + probeAction
		gotStub := len(probe.Results) > 0 && probe.Results[len(probe.Results)-1] == wantStub
		if status != 1 || !gotStub {
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Sandbox Preflight Failed\n\nSandbox invariant BROKEN: dispatching real action `%s` through a Sandbox Blackboard did not short-circuit to the %q stub (status=%d, results=%v). The bb.Sandbox guard in actionForName no longer blocks real dispatch, so every benchmark harness would spawn real `nlm`/`git`/`claude` actions during structural evaluation.", probeAction, wantStub, status, probe.Results)
			return -1
		}

		bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Sandbox Preflight Passed\n\nSandbox invariant holds: a Sandbox Blackboard short-circuits real action dispatch (`%s` returned the %q stub without executing its real implementation), so structural evaluation during the scheduled cycle can never spawn `nlm`/`git`/`claude` subprocesses.", probeAction, wantStub)
		return 1
	})
}
