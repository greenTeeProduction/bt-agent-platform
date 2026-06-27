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
