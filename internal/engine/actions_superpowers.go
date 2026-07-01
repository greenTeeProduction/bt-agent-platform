// Package engine — production Superpowers compatibility actions.
//
// The Superpowers production pipeline lives in actions_superpowers_prod.go and
// related typed runtime files. This file intentionally contains no placeholder
// phase actions, manual HITL sidecar writers, or fake TDD implementation nodes.
// It keeps only shared verification actions that are referenced by older trees.
package engine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
// ApplyImprovementWithClaude step shells out to `git` dozens of times via
// runGoapShell — `git checkout`, `git fetch origin`, `git pull origin master
// --ff-only`, `git status`, `git stash`, `git diff`, `git reset --hard`, `git
// clean`, and `git push origin master` — to synchronize, isolate, and publish
// every improvement. The VerifyScheduledGoapFusionGitRemote guard runs `git
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
			bb.Result = fmt.Sprintf("## Scheduled GOAP Fusion Git Tool Preflight Failed\n\nGit binary %q is not resolvable on PATH: %v; a scheduled run would fail at the very first git sync in ApplyImprovementWithClaude.", "git", err)
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
// ApplyImprovementWithClaude step synchronizes against origin before letting
// Claude implement (`git fetch origin`, `git pull origin master --ff-only`) and
// publishes the result afterwards (`git push origin master`). A scheduled run
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
