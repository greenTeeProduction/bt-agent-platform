package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestSuperpowersRuntime_ActionsRegistered(t *testing.T) {
	actions := []string{
		"InitSuperpowersRun",
		"GenerateDesignArtifact",
		"ValidateDesignArtifact",
		"PrepareSuperpowersWorktree",
		"VerifySuperpowersBaseline",
		"GenerateImplementationPlan",
		"ValidateImplementationPlanStrict",
		"ExecuteSuperpowersTaskBatch",
		"VerifySuperpowersRun",
		"ApplySuperpowersRunToMainRepo",
		"WriteSuperpowersFinishReport",
		"RunSuperpowersRuntimeFromExistingPlan",
		"RunSuperpowersClaudeImplementation",
	}
	for _, name := range actions {
		if GetAction(name) == nil {
			t.Fatalf("missing production Superpowers action %q", name)
		}
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCycle asserts the
// presence of the end-to-end scheduled GOAP fusion action that reads vault
// research and the graphify report, identifies improvement gaps, prioritizes
// goals, writes a Superpowers implementation plan, implements findings via the
// Superpowers runtime, verifies, and reports — research-to-implementation in one
// automatically scheduled cycle.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCycle(t *testing.T) {
	if GetAction("RunScheduledGoapFusionCycle") == nil {
		t.Fatalf("missing production Superpowers action %q", "RunScheduledGoapFusionCycle")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionInputs asserts the
// presence of the preflight action that guards the unattended scheduled GOAP
// fusion cycle: before the automatic research-to-implementation run proceeds, it
// verifies the cycle's required research inputs are readable — the vault research
// directory and the graphify report — so a scheduled run fails fast with a clear
// diagnosis instead of silently producing a plan from missing context.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionInputs(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionInputs") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionInputs")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionResearchPresent
// asserts the presence of the preflight action that guards the unattended
// scheduled GOAP fusion cycle against an empty research corpus. The existing
// VerifyScheduledGoapFusionInputs guard only confirms the vault directory and
// graphify report exist; a vault directory that exists but contains zero
// research files would still pass it, letting a scheduled run silently produce
// a plan from no actual research. This action closes that gap by requiring the
// vault research directory to contain at least one readable research file
// before the automatic research-to-implementation cycle proceeds.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionResearchPresent(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionResearchPresent") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionResearchPresent")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionRuntime asserts the
// presence of the preflight action that guards the implementation runtime of the
// unattended scheduled GOAP fusion cycle. The input preflight only confirms the
// research inputs are readable; before the automatic cycle commits to producing a
// Superpowers plan it must also confirm the implementation runtime is available —
// the go-bt-evolve repository working directory and the Claude Code binary used to
// implement findings — so a scheduled run fails fast with a clear diagnosis
// instead of producing a plan it can never implement.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionRuntime(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionRuntime") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionRuntime")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphReportPresent
// asserts the presence of the preflight action that guards the unattended
// scheduled GOAP fusion cycle against an empty graphify report. The existing
// VerifyScheduledGoapFusionInputs guard only confirms the graphify report file
// exists (os.Stat, not a directory); a zero-byte or contentless graphify report
// would still pass it, letting a scheduled run silently derive its improvement
// gaps from an empty report. This action closes that gap by requiring the
// graphify report to contain readable content before the automatic
// research-to-implementation cycle proceeds — the report-content analogue of the
// VerifyScheduledGoapFusionResearchPresent vault-content guard.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphReportPresent(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionGraphReportPresent") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionGraphReportPresent")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionToolchain asserts
// the presence of the preflight action that guards the Go toolchain the
// unattended scheduled GOAP fusion cycle depends on. The existing runtime guard
// (VerifyScheduledGoapFusionRuntime) only confirms the repository working
// directory and the Claude Code binary are available; the cycle's build and TDD
// verification step additionally shells out to the hardcoded Go toolchain
// (/usr/local/go/bin/go), so a scheduled run could pass every current preflight
// yet still fail at verification when that toolchain is missing or not
// executable. This action closes that gap by requiring the Go toolchain binary
// to be an executable file before the automatic research-to-implementation cycle
// proceeds — the verification-toolchain analogue of the runtime guard.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionToolchain(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionToolchain") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionToolchain")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPlansWritable
// asserts the presence of the preflight action that guards the plan output
// location the unattended scheduled GOAP fusion cycle writes to. The existing
// guards confirm the cycle's inputs (vault research, graphify report) and its
// implementation runtime (repo, Claude Code, Go toolchain) are available, but
// nothing confirms the cycle can persist its output: the cycle writes a
// Superpowers implementation plan and, on an incomplete Claude run, saves the
// failed patch into the plans directory (goapFusionPlansDir). A scheduled run
// could pass every current preflight yet still fail when that plans directory
// is missing or not writable, losing its plan and patch with no clear
// diagnosis. This action closes that gap by requiring the plans directory to be
// a writable directory before the automatic research-to-implementation cycle
// proceeds — the output-location analogue of the input and runtime guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPlansWritable(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionPlansWritable") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionPlansWritable")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSynthesesPresent
// asserts the presence of the preflight action that guards the unattended
// scheduled GOAP fusion cycle against a missing or empty research-syntheses
// corpus. The cycle's ReadVaultResearch step reads the syntheses directory
// (goapFusionSynthesesDir) first and newest-first, treating it as the highest
// priority research input, but it swallows a read error (os.ReadDir ... err ==
// nil) — so a syntheses directory that is missing, unreadable, or contains zero
// synthesis files would silently degrade the research corpus and let a
// scheduled run produce a plan from the most recent research being absent, with
// no diagnosis. The existing VerifyScheduledGoapFusionResearchPresent guard only
// covers the vault directory itself, not this distinct syntheses subdirectory.
// This action closes that gap by requiring the syntheses directory to contain at
// least one readable synthesis file before the automatic
// research-to-implementation cycle proceeds — the syntheses-content analogue of
// the vault-content and graph-report-content guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSynthesesPresent(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionSynthesesPresent") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionSynthesesPresent")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphifyTool
// asserts the presence of the preflight action that guards the external
// graphify tool the unattended scheduled GOAP fusion cycle depends on. The
// existing runtime and toolchain guards confirm the Claude Code binary
// (VerifyScheduledGoapFusionRuntime) and the Go toolchain
// (VerifyScheduledGoapFusionToolchain) are available, but the cycle's
// RunGraphifyUpdate step shells out to the external `graphify` command to
// regenerate the graphify report from which the cycle derives every improvement
// gap. A scheduled run could pass every current preflight yet still fail when
// the graphify tool is not installed or not on PATH, leaving the cycle's gap
// analysis grounded in a stale report with no clear diagnosis. This action
// closes that gap by requiring the graphify tool to be resolvable before the
// automatic research-to-implementation cycle proceeds — the graphify-tool
// analogue of the runtime and toolchain guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphifyTool(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionGraphifyTool") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionGraphifyTool")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebookLMTool
// asserts the presence of the preflight action that guards the external NotebookLM
// (`nlm`) binary the unattended scheduled GOAP fusion cycle depends on. The
// existing runtime, toolchain, and graphify-tool guards confirm the Claude Code
// binary (VerifyScheduledGoapFusionRuntime), the Go toolchain
// (VerifyScheduledGoapFusionToolchain), and the graphify tool
// (VerifyScheduledGoapFusionGraphifyTool) are available, but the cycle's
// RunGoapFusionNotebookLMResearch step — which now runs independent NotebookLM
// research before implementation — shells out to the `nlm` binary (nlmBin) via
// nlmRun and hard-fails ("refusing to proceed from stale vault research") when it
// is unavailable. A scheduled run could pass every current preflight yet still
// abort at the research step when that binary is missing or not executable,
// wasting the cycle with no early diagnosis. This action closes that gap by
// requiring the NotebookLM binary to be an executable file before the automatic
// research-to-implementation cycle proceeds — the NotebookLM-tool analogue of the
// runtime, toolchain, and graphify-tool guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebookLMTool(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionNotebookLMTool") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionNotebookLMTool")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSynthesesWritable
// asserts the presence of the preflight action that guards the syntheses output
// location the unattended scheduled GOAP fusion cycle writes to. The cycle's
// RunGoapFusionNotebookLMResearch step writes a dedicated synthesis file
// (goap-fusion-notebooklm-<ts>.md) into the syntheses directory
// (goapFusionSynthesesDir) via writeString, and the immediately following
// ReadVaultResearch step ingests that newest synthesis as its highest-priority
// research input. The existing VerifyScheduledGoapFusionSynthesesPresent guard
// only confirms the directory is readable and already contains synthesis files;
// it does not confirm the cycle can persist a new one. The existing
// VerifyScheduledGoapFusionPlansWritable guard confirms writability but for a
// distinct directory (goapFusionPlansDir), not this syntheses directory. A
// scheduled run could pass every current preflight yet still fail when the
// syntheses directory is not writable, losing the freshly generated NotebookLM
// research with no clear diagnosis. This action closes that gap by requiring the
// syntheses directory to be a writable directory before the automatic
// research-to-implementation cycle proceeds — the syntheses-output-location
// analogue of the VerifyScheduledGoapFusionPlansWritable guard.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSynthesesWritable(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionSynthesesWritable") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionSynthesesWritable")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebook asserts
// the presence of the preflight action that guards the configured NotebookLM
// notebook id the unattended scheduled GOAP fusion cycle queries against. The
// existing VerifyScheduledGoapFusionNotebookLMTool guard only confirms the `nlm`
// binary is an executable file; it does not confirm a notebook is actually
// configured. The cycle's RunGoapFusionNotebookLMResearch step shells out to
// `nlm notebook query <defaultNotebook> ...` — so an empty or unset notebook id
// would let a scheduled run pass the binary check yet still query against no
// notebook, silently degrading the research corpus and producing a plan from
// stale vault research with no clear diagnosis. This action closes that gap by
// requiring the configured notebook id to be non-empty before the automatic
// research-to-implementation cycle proceeds — the notebook-id analogue of the
// NotebookLM-tool guard.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebook(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionNotebook") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionNotebook")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGitRemote asserts
// the presence of the preflight action that guards the git `origin` remote the
// unattended scheduled GOAP fusion cycle depends on. The existing runtime guard
// (VerifyScheduledGoapFusionRuntime) only confirms the repository working
// directory and the Claude Code binary are available; but the cycle's
// Superpowers implementation step synchronizes against origin before letting
// Claude implement (`git fetch origin`, `git pull origin master --ff-only`) and
// publishes the result afterwards (`git push origin master`). A scheduled run
// could pass every current preflight yet still fail at the fetch/pull sync
// (goap_fusion_preflight_failed) — or silently degrade at push — when the
// `origin` remote is unconfigured or unreachable, wasting the cycle with no
// early diagnosis. This action closes that gap by requiring the repository's
// `origin` remote to be configured before the automatic
// research-to-implementation cycle proceeds — the git-remote analogue of the
// runtime and toolchain guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGitRemote(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionGitRemote") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionGitRemote")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGitTool asserts the
// presence of the preflight action that guards the external `git` binary the
// unattended scheduled GOAP fusion cycle depends on. The cycle's
// Superpowers implementation step shells out to `git` via
// runGoapShell — `git checkout`, `git fetch origin`, `git pull origin master
// --ff-only`, `git status`, `git stash`, `git diff`, `git reset --hard`, `git
// clean`, and `git push origin master` — to synchronize, isolate, and publish
// every improvement. The existing VerifyScheduledGoapFusionGitRemote guard runs
// `git remote get-url origin`, but if the `git` binary is missing entirely that
// guard fails with a misleading "origin remote is not configured" diagnosis
// rather than naming the real cause. A scheduled run could otherwise pass every
// tool guard (Claude Code, Go toolchain, graphify, NotebookLM) yet still fail at
// the very first git sync when `git` is not installed or not on PATH, wasting the
// cycle with no clear diagnosis. This action closes that gap by requiring the
// `git` binary to be resolvable on PATH before the automatic
// research-to-implementation cycle proceeds — the git-binary analogue of the
// graphify-tool and NotebookLM-tool guards, and the prerequisite of the
// git-remote guard.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGitTool(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionGitTool") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionGitTool")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionRejectedContextLedger
// asserts the presence of the preflight action that guards the continuous
// self-improving loop runner against safety-drift regression. The prior guards
// protect a single cycle's inputs, runtime, and external tools, but the loop
// runner introduces a distinct hazard: because it re-runs the
// research-to-implementation cycle indefinitely, a later iteration can generate
// a high-fitness improvement that re-admits a previously rejected unsafe context
// — the "Safety Drift" / "Activity-Progress Confusion" failure mode the
// Experience-Grounded Monotonicity Auditor exists to prevent [Source 207, 214,
// 215, 250]. Enforcing the Monotonicity Invariant requires the loop runner to
// replay a persistent corpus of known rejected unsafe contexts (the
// rejected-context ledger) against every new candidate; if that ledger is
// missing or unreadable, the loop has no historical safety-regression kernel and
// silently relaxes previously patched security gates with no diagnosis. This
// action closes that gap by requiring the rejected-context ledger to be readable
// before the continuous loop runner proceeds — the monotonicity-ledger analogue
// of the input, runtime, and tool guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionRejectedContextLedger(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionRejectedContextLedger") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionRejectedContextLedger")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionVaultWritable
// asserts the presence of the preflight action that guards the vault research
// directory the unattended scheduled GOAP fusion cycle writes its own analysis
// back into. The cycle's WriteFusionAnalysis step persists its per-run gap
// analysis (goap-fusion-analysis-<ts>.md) and a rolling pointer
// (goap-fusion-latest.md) directly into the vault directory
// (goapFusionVaultDir) via os.WriteFile — and the next scheduled run's
// ReadVaultResearch step ingests those files as part of its research corpus.
// The existing VerifyScheduledGoapFusionResearchPresent guard only confirms the
// vault directory is readable and already contains research files; it does not
// confirm the cycle can persist a new analysis. The existing
// VerifyScheduledGoapFusionPlansWritable and
// VerifyScheduledGoapFusionSynthesesWritable guards confirm writability but for
// distinct directories (goapFusionPlansDir, goapFusionSynthesesDir), not this
// vault directory. A scheduled run could pass every current preflight yet still
// fail when the vault directory is not writable, silently dropping its own
// analysis and starving the next run's research corpus with no clear diagnosis.
// This action closes that gap by requiring the vault directory to be a writable
// directory before the automatic research-to-implementation cycle proceeds —
// the vault-output-location analogue of the plans- and syntheses-writable
// guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionVaultWritable(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionVaultWritable") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionVaultWritable")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitPolicy
// asserts the presence of the CIRCUITPOLICY (circuit-breaker) preflight action
// that guards the continuous self-improving loop runner against
// "Activity-Progress Confusion" — the failure mode surfaced by the P0 NotebookLM
// research goal, where the loop remains active by proposing syntactically valid
// but redundant patches that never advance the task goal. The prior
// VerifyScheduledGoapFusionRejectedContextLedger guard protects against
// safety-drift by replaying a corpus of rejected unsafe contexts, but nothing
// protects against the distinct hazard of the loop spinning on repeated state
// hashes or consecutive no-op patch proposals. Production-grade reliability
// requires a deterministic kernel-level circuit policy that monitors a bounded
// state-hash history window (goapFusionCircuitHistoryWindow) and, on detecting a
// repeated state hash or a run of consecutive no-op patches, halts the loop
// instead of wasting tokens indefinitely. This action closes that gap by
// requiring the circuit-policy history window to be a positive, bounded value
// before the continuous loop runner proceeds — the circuit-breaker analogue of
// the rejected-context-ledger and input/runtime/tool guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitPolicy(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionCircuitPolicy") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionCircuitPolicy")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHalts
// asserts the actual CIRCUITPOLICY circuit-breaker *behavior* the P0 NotebookLM
// research goal requires: detecting and halting state-transition cycles and
// repeated no-op patch proposals. The existing
// VerifyScheduledGoapFusionCircuitPolicy guard is only a config preflight — it
// checks that goapFusionCircuitHistoryWindow is a positive, bounded value, but
// it never evaluates a running state-hash history and never actually halts the
// loop. That leaves "Activity-Progress Confusion" unmitigated: nothing inspects
// the bounded state-hash window at runtime to detect that the continuous loop is
// proposing syntactically valid but redundant patches that never advance the
// task goal. This action closes that gap with a deterministic kernel-level
// evaluation: given the loop's recent state-hash history (published on the
// blackboard under "goap_fusion_state_hashes"), it returns HALT (-1) when a
// state hash repeats within goapFusionCircuitHistoryWindow — the repeated-state
// cycle the PatchBoard 3-hash window is designed to catch — and CONTINUE (1)
// when the window shows only distinct, progress-making hashes.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHalts(t *testing.T) {
	action := GetAction("EvaluateScheduledGoapFusionCircuitBreaker")
	if action == nil {
		t.Fatalf("missing production Superpowers action %q", "EvaluateScheduledGoapFusionCircuitBreaker")
	}

	// A repeated state hash within the bounded history window is the
	// "Activity-Progress Confusion" cycle: the loop returned to a prior state
	// without advancing the goal. The circuit breaker must HALT (-1).
	halt := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes": []string{"aaa", "bbb", "aaa"},
		},
	}
	haltCtx := &btcore.BTContext[Blackboard]{Blackboard: halt}
	if status := action(haltCtx); status != -1 {
		t.Fatalf("expected HALT (-1) on a repeated state hash within the window, got %d", status)
	}
	if !strings.Contains(halt.Result, "HALT") {
		t.Fatalf("expected HALT diagnosis in Result, got %q", halt.Result)
	}

	// A window of only distinct, progress-making state hashes must let the loop
	// CONTINUE (1).
	cont := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes": []string{"aaa", "bbb", "ccc"},
		},
	}
	contCtx := &btcore.BTContext[Blackboard]{Blackboard: cont}
	if status := action(contCtx); status != 1 {
		t.Fatalf("expected CONTINUE (1) on a window of distinct state hashes, got %d", status)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionBuildTreeMaterialized
// asserts the presence of the preflight guard that closes the P0 NotebookLM
// research gap: the on-disk build directory the unattended scheduled GOAP fusion
// cycle compiles and TDD-verifies against must be materialized to match HEAD.
// When the main repository is bare (core.bare=true), applying a run fast-forwards
// the bare `master` ref (`git fetch . <branch>:master`) but never checks that
// tree out to the on-disk working files (superpowers_apply.go:118) — updating a
// ref in a bare repo touches no file, so goapFusionRepo's tracked source stays
// frozen at whatever the working tree last held, arbitrarily many commits behind
// HEAD. The cycle's build and TDD verification step then compiles that stale tree
// and the deployed binary is built from source that does not match HEAD, so the
// loop's own committed fixes never reach the running code. The existing runtime
// and toolchain guards confirm the repository directory and Go toolchain exist
// but never confirm the on-disk tree matches HEAD. This guard closes that gap by
// requiring the build directory's tracked working tree to be materialized to HEAD
// — no tracked file whose on-disk content differs from the committed HEAD — before
// the automatic research-to-implementation cycle proceeds, so a bare-repo run
// fails fast with a clear diagnosis instead of silently building a stale tree.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionBuildTreeMaterialized(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionBuildTreeMaterialized") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionBuildTreeMaterialized")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionBuildTreeMaterializedMaterializesOnBareRepo
// pins the reconciled bare-main-repo *behavior* the P0 NotebookLM research goal
// requires. Commit 54011e3 turned this guard into a bare-repo no-op: on a bare
// (core.bare=true) main repo it returned 1 with a "Delegated" note and
// materialized nothing — passing in exactly the stale-tree case the guard was
// invented to catch. goapFusionRepo is bare yet keeps an on-disk working tree
// that can sit arbitrarily many commits behind HEAD (updating the bare `master`
// ref during apply touches no file). The cycle's build+TDD step then compiles
// that stale tree, so the loop's own committed fixes never reach the deployed
// binary — the live "silently building a stale tree" failure the guard exists to
// block. The reconciled guard must therefore *materialize* the on-disk tree to
// HEAD (e.g. `git checkout -f HEAD -- .`) before returning pass, not skip.
//
// This test asserts that on a bare repo the guard (1) returns 1, (2) reports that
// it materialized the on-disk tree to HEAD rather than merely delegating, and
// (3) leaves no tracked file on disk differing from the committed HEAD.
//
// Skips when goapFusionRepo is not a bare repo (CI checkouts): the non-bare path
// already runs a real HEAD comparison against the on-disk tree.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionBuildTreeMaterializedMaterializesOnBareRepo(t *testing.T) {
	out, err := runGoapShell("git rev-parse --is-bare-repository")
	if err != nil || strings.TrimSpace(out) != "true" {
		t.Skipf("goapFusionRepo is not a bare repo here (out=%q err=%v)", strings.TrimSpace(out), err)
	}
	fn := GetAction("VerifyScheduledGoapFusionBuildTreeMaterialized")
	if fn == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionBuildTreeMaterialized")
	}
	bb := &Blackboard{Task: "verify scheduled goap fusion build tree materialized"}
	code := fn(btcore.NewBTContext(context.Background(), bb))
	if code != 1 {
		t.Fatalf("VerifyScheduledGoapFusionBuildTreeMaterialized on bare repo = %d, want 1 (materialize on-disk tree to HEAD)", code)
	}
	if !strings.Contains(bb.Result, "Materialized") {
		t.Fatalf("expected a bare-repo materialization note in Result (guard must check out HEAD, not delegate), got: %s", bb.Result[:min(len(bb.Result), 300)])
	}

	// After the guard runs, the on-disk tracked tree must match HEAD: a bare repo
	// with a work tree lets us compare via an explicit --git-dir/--work-tree diff.
	// A delegation no-op leaves the stale files in place, so this stays non-empty
	// until the guard actually materializes the tree.
	diff, derr := runGoapShell("git --git-dir=.git --work-tree=. diff --name-only HEAD --")
	if derr != nil {
		t.Fatalf("post-materialization diff against HEAD failed: %v\n%s", derr, diff)
	}
	if stale := strings.TrimSpace(diff); stale != "" {
		t.Fatalf("expected the on-disk build tree materialized to HEAD (no tracked file differs), but these still differ:\n%s", stale)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopRunner asserts
// the continuous self-improving *loop runner* itself — the driver the whole
// preflight-guard and circuit-breaker apparatus in this file exists to protect.
// Every sibling test so far guards one input, tool, output location, or a single
// cycle; the P0 NotebookLM research goal's "loop runner" is the action that
// re-runs the research-to-implementation cycle indefinitely. Left ungoverned that
// driver is the source of the two failure modes the research names — "Safety
// Drift" and "Activity-Progress Confusion" [Source 207, 214, 215, 250] — because
// an unbounded loop can spin on syntactically valid but redundant no-op patches
// or iterate forever without ever advancing the goal. A production-grade loop
// runner must therefore be a bounded, self-halting kernel: before it drives
// another iteration it consults the CIRCUITPOLICY circuit breaker over the loop's
// recent state-hash history (published on the blackboard under
// "goap_fusion_state_hashes") and HALTs (-1) when a state hash repeats within the
// bounded window — the Activity-Progress Confusion cycle — and it additionally
// enforces a finite runaway-loop backstop so the continuous loop can never
// iterate unbounded even when every state hash is distinct. Only a short window
// of distinct, progress-making hashes lets the runner CONTINUE (1) to drive the
// next cycle. This closes the loop-runner gap: the sibling guards and the
// circuit-breaker *evaluation* exist, but nothing wires them into the bounded
// driver that the "loop runner" actually is.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopRunner(t *testing.T) {
	action := GetAction("RunScheduledGoapFusionLoop")
	if action == nil {
		t.Fatalf("missing production Superpowers action %q", "RunScheduledGoapFusionLoop")
	}

	// A repeated state hash within the bounded history window is the
	// "Activity-Progress Confusion" cycle: the loop returned to a prior state
	// without advancing the goal. The loop runner must consult the circuit
	// breaker and HALT (-1) rather than drive another redundant iteration.
	cycle := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes": []string{"aaa", "bbb", "aaa"},
		},
	}
	cycleCtx := &btcore.BTContext[Blackboard]{Blackboard: cycle}
	if status := action(cycleCtx); status != -1 {
		t.Fatalf("expected HALT (-1) when the circuit breaker detects a repeated state hash, got %d", status)
	}
	if !strings.Contains(cycle.Result, "HALT") {
		t.Fatalf("expected a HALT diagnosis in Result on a repeated state hash, got %q", cycle.Result)
	}

	// A runaway-loop backstop: even when every state hash is distinct — so the
	// circuit breaker's bounded window sees no cycle — a bounded loop runner must
	// refuse to iterate forever. A history far longer than any sane finite bound
	// must HALT (-1) on the backstop alone.
	runaway := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		runaway = append(runaway, fmt.Sprintf("hash-%03d", i))
	}
	over := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes": runaway,
		},
	}
	overCtx := &btcore.BTContext[Blackboard]{Blackboard: over}
	if status := action(overCtx); status != -1 {
		t.Fatalf("expected HALT (-1) once the loop reaches its runaway-loop backstop, got %d", status)
	}

	// A short window of distinct, progress-making state hashes — under the
	// backstop and with no repeat in the bounded window — lets the loop runner
	// CONTINUE (1) to drive the next cycle.
	progress := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes": []string{"aaa", "bbb", "ccc"},
		},
	}
	progressCtx := &btcore.BTContext[Blackboard]{Blackboard: progress}
	if status := action(progressCtx); status != 1 {
		t.Fatalf("expected CONTINUE (1) on a short window of distinct state hashes, got %d", status)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopSharesCircuitBreakerWindow
// pins the drift-elimination the P0 NotebookLM research goal requires: the
// bounded-window dedup that decides "did a state hash repeat within the most
// recent goapFusionCircuitHistoryWindow hashes?" must live in ONE shared helper
// that both EvaluateScheduledGoapFusionCircuitBreaker and RunScheduledGoapFusionLoop
// call — not two independent verbatim copies (actions_superpowers.go:709-722 and
// :759-770). Two copies of the exact same CIRCUITPOLICY semantics silently
// drift: a future fix to the window logic applied to one action (e.g. widening
// goapFusionCircuitHistoryWindow handling or scanning full history) would not
// reach the other, and the loop runner — "the kernel the whole apparatus exists
// to protect" — could then enforce a different halt policy than the dedicated
// breaker.
//
// This test asserts the extracted helper exists and correctly implements the
// bounded window + first-repeat dedup, so the two actions provably share it. It
// fails to compile until the shared helper is introduced (RED), and passes once
// the window/dedup is extracted into one function both actions delegate to
// (GREEN). The public-action behavior of both actions is already pinned by
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHalts
// and TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopRunner, so
// after extraction both continue to enforce identical semantics — now from a
// single source of truth.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopSharesCircuitBreakerWindow(t *testing.T) {
	// A state hash that repeats within the bounded most-recent window is the
	// "Activity-Progress Confusion" cycle: the shared helper must report the
	// repeated hash and that the breaker is tripped.
	window, repeated, tripped := goapFusionCircuitBreakerWindow([]string{"aaa", "bbb", "aaa"})
	if !tripped {
		t.Fatalf("expected tripped=true on a repeated state hash within the window, got false (window=%v)", window)
	}
	if repeated != "aaa" {
		t.Fatalf("expected the repeated hash to be reported as %q, got %q", "aaa", repeated)
	}
	if len(window) != 3 {
		t.Fatalf("expected the returned window to hold all 3 hashes (window size %d), got %d: %v", goapFusionCircuitHistoryWindow, len(window), window)
	}

	// A window of only distinct, progress-making hashes must not trip the shared
	// dedup.
	if _, _, tripped := goapFusionCircuitBreakerWindow([]string{"aaa", "bbb", "ccc"}); tripped {
		t.Fatalf("expected tripped=false on a window of distinct state hashes, got true")
	}

	// The dedup is BOUNDED: only the most recent goapFusionCircuitHistoryWindow
	// (3) hashes are inspected. Here the earlier "aaa" falls outside the last-3
	// window ["bbb","ccc","aaa"], so the two "aaa" occurrences are not both in the
	// window and the breaker must NOT trip. This is the exact window-slicing
	// semantics the extraction must preserve for both actions.
	boundedWindow, _, boundedTripped := goapFusionCircuitBreakerWindow([]string{"aaa", "bbb", "ccc", "aaa"})
	if boundedTripped {
		t.Fatalf("expected tripped=false when the repeat lies outside the bounded window, got true (window=%v)", boundedWindow)
	}
	if len(boundedWindow) != goapFusionCircuitHistoryWindow {
		t.Fatalf("expected the bounded window to hold the most recent %d hashes, got %d: %v", goapFusionCircuitHistoryWindow, len(boundedWindow), boundedWindow)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopSharesCircuitBreakerVerdict
// pins the *remaining* single-source-of-truth the P0 NotebookLM research goal
// requires. Commit f5d3eeb extracted only the bounded-window *dedup scan* into
// goapFusionCircuitBreakerWindow, but both EvaluateScheduledGoapFusionCircuitBreaker
// and RunScheduledGoapFusionLoop still independently re-derive the CIRCUITPOLICY
// *verdict* from that scan and re-implement the `tripped → HALT` decision inline.
// Sharing the scan but not the halt/continue *decision* leaves the exact class of
// drift the commit set out to eliminate partially open: a future change to what
// counts as a "trip" must still be applied in two places.
//
// This test asserts a single shared verdict helper —
// goapFusionCircuitBreakerVerdict(hashes) (halt bool, window []string, repeated
// string) — exists and encodes the breaker's halt DECISION (not merely the dedup)
// in one place, so both actions can delegate their entire circuit-breaker verdict
// to it and the loop runner layers only its runaway backstop on top. It fails to
// compile until the shared verdict helper is introduced (RED), and passes once the
// halt decision is extracted into one function both actions delegate to (GREEN).
// The public-action behavior of both actions stays pinned by
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHalts
// and TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopRunner, so
// after extraction both continue to enforce identical semantics — now the DECISION,
// not just the scan, from a single source of truth.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopSharesCircuitBreakerVerdict(t *testing.T) {
	// A state hash that repeats within the bounded most-recent window is the
	// "Activity-Progress Confusion" cycle: the shared verdict must decide HALT and
	// report the repeated hash and the bounded window it inspected.
	halt, window, repeated := goapFusionCircuitBreakerVerdict([]string{"aaa", "bbb", "aaa"})
	if !halt {
		t.Fatalf("expected halt=true on a repeated state hash within the window, got false (window=%v)", window)
	}
	if repeated != "aaa" {
		t.Fatalf("expected the repeated hash to be reported as %q, got %q", "aaa", repeated)
	}
	if len(window) != 3 {
		t.Fatalf("expected the returned window to hold all 3 hashes, got %d: %v", len(window), window)
	}

	// A window of only distinct, progress-making hashes must decide CONTINUE.
	if halt, _, _ := goapFusionCircuitBreakerVerdict([]string{"aaa", "bbb", "ccc"}); halt {
		t.Fatalf("expected halt=false on a window of distinct state hashes, got true")
	}

	// The verdict is BOUNDED: only the most recent goapFusionCircuitHistoryWindow
	// (3) hashes are inspected. Here the earlier "aaa" falls outside the last-3
	// window ["bbb","ccc","aaa"], so the two "aaa" occurrences are not both in the
	// window and the verdict must decide CONTINUE — the exact window-slicing
	// semantics the extracted decision must preserve for both actions.
	boundedHalt, boundedWindow, _ := goapFusionCircuitBreakerVerdict([]string{"aaa", "bbb", "ccc", "aaa"})
	if boundedHalt {
		t.Fatalf("expected halt=false when the repeat lies outside the bounded window, got true (window=%v)", boundedWindow)
	}
	if len(boundedWindow) != goapFusionCircuitHistoryWindow {
		t.Fatalf("expected the bounded window to hold the most recent %d hashes, got %d: %v", goapFusionCircuitHistoryWindow, len(boundedWindow), boundedWindow)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesRuntime
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the implementation-runtime guard —
// VerifyScheduledGoapFusionRuntime — as a runnable Action node ordered BEFORE the
// bounded loop runner it protects.
//
// RunScheduledGoapFusionLoop is the driver of the research-to-implementation
// cycle: once it decides CONTINUE, the scheduled cycle shells out to the Claude
// Code binary inside the go-bt-evolve repository working directory to actually
// implement findings. VerifyScheduledGoapFusionRuntime is the guard that proves
// both of those — the repository working directory and the executable Claude Code
// binary — are present, so a scheduled cycle fails fast with a clear diagnosis
// instead of letting the loop runner drive a cycle it can never implement. Yet
// GoapFusionPreflightNode() composes only the build-tree materializer, the
// circuit-policy config guard, and the rejected-context ledger before the loop
// runner — never the runtime guard — so a scheduled cycle could materialize a
// fresh tree, prove its circuit policy and ledger, gate on the loop runner, and
// only then discover at the implementation step that the runtime it needs is
// absent, wasting the cycle with no early diagnosis.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionRuntime as a registered Action node AND that it is
// ordered before RunScheduledGoapFusionLoop. It fails while the builder composes
// only the materializer guard, the circuit-policy guard, the rejected-context
// ledger, and the loop runner (RED) and passes once the runtime guard is inserted
// ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is pinned
// here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesRuntime(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionRuntime"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q implementation-runtime guard as a runnable Action node; the preflight drives the loop runner without first proving the repository working directory and Claude Code binary are present, so a scheduled cycle would only discover its runtime is missing at the implementation step", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the runtime guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q implementation-runtime guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the runtime the loop runner needs to implement findings is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_GoapFusionPreflightNodeComposesBuildTreeMaterializer
// pins the concrete, observed defect the P0 NotebookLM research goal names: the
// VerifyScheduledGoapFusionBuildTreeMaterialized guard must not merely be
// *registered and unit-tested* — it must be *composed into a runnable Phase-0
// preflight node* the scheduled GOAP fusion loop can actually execute. Every
// sibling test in this file only asserts GetAction(name) != nil, which proves
// the guard is registered but never proves it is wired into any behavior tree.
// The observed defect is exactly that: the Scheduled* guards appear only in
// their own RegisterAction calls and in _test.go files, never in any composed
// BT, so they can never run in a scheduled cycle — a bare-repo run silently
// builds a stale tree because the materializer guard is never invoked.
//
// This test closes that gap at its source: the engine must expose a Phase-0
// preflight node builder (GoapFusionPreflightNode) that composes the
// materializer guard as an Action node, and that composed action name must
// resolve to a registered, runnable action. It is the in-package analogue of
// "compose the tree and assert the node runs" — the engine package cannot
// import internal/domains (where GoapFusionLoopTree lives) without an import
// cycle, so the runnable-composition contract is pinned here, at the guard's
// own package, ready for the domains tree to embed as its Phase-0 preflight.
//
// It fails to compile until GoapFusionPreflightNode is introduced (RED) and
// passes once the builder composes the materializer guard into a runnable node
// (GREEN).
func TestSuperpowersRuntime_GoapFusionPreflightNodeComposesBuildTreeMaterializer(t *testing.T) {
	const want = "VerifyScheduledGoapFusionBuildTreeMaterialized"

	node := GoapFusionPreflightNode()

	var references func(n evolution.SerializableNode) bool
	references = func(n evolution.SerializableNode) bool {
		if n.Type == "Action" && n.Name == want {
			return true
		}
		for _, c := range n.Children {
			if references(c) {
				return true
			}
		}
		return false
	}

	if !references(node) {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q guard as a runnable Action node; the materializer guard is registered but never wired into any preflight, so it can never run in a scheduled cycle", want)
	}

	if GetAction(want) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", want)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightPrependsToLoopSequence
// pins the integration seam the "goap-fusion-loop-runner" goal actually requires:
// every prior run grew the orphan preflight builder (materializer guard, then
// loop runner, plus unit tests) without ever taking the one step that makes any
// of it run — inserting the Phase-0 preflight as the FIRST child of the
// production GoapFusionLoop_Main sequence, before SetupFusionTools. The
// GoapFusionPreflightNode() builder composes the guards, but nothing lets the
// loop sequence adopt it, so the materializer guard and bounded loop runner
// execute nowhere in a scheduled cycle.
//
// The engine package cannot import internal/domains (import cycle), but
// domains -> engine is the safe direction, so the seam belongs here at the
// guards' own package: PrependGoapFusionPreflight takes the loop sequence's
// child list and returns a new list with the Phase-0 preflight node prepended as
// the first child, ready for GoapFusionLoopTree() to embed without duplicating
// the composition. This test asserts that seam (1) prepends the preflight as the
// first child, (2) that first child composes both the build-tree materializer
// guard and the bounded loop runner as runnable Action nodes, (3) preserves the
// original loop children in order after the preflight, and (4) does not mutate
// the caller's slice.
//
// It fails to compile until PrependGoapFusionPreflight is introduced (RED) and
// passes once the seam returns the preflight-prepended child list (GREEN).
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightPrependsToLoopSequence(t *testing.T) {
	original := []evolution.SerializableNode{
		{Type: "Action", Name: "SetupFusionTools"},
		{Type: "Action", Name: "GrillMeNotebookLM"},
	}

	got := PrependGoapFusionPreflight(original)

	if len(got) != len(original)+1 {
		t.Fatalf("expected the Phase-0 preflight prepended (len %d), got %d: %+v", len(original)+1, len(got), got)
	}

	// The preflight must be the FIRST child — it runs before SetupFusionTools so a
	// scheduled cycle materializes a fresh on-disk tree and consults the bounded
	// loop runner before it does anything else.
	first := got[0]

	var references func(n evolution.SerializableNode, name string) bool
	references = func(n evolution.SerializableNode, name string) bool {
		if n.Type == "Action" && n.Name == name {
			return true
		}
		for _, c := range n.Children {
			if references(c, name) {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"VerifyScheduledGoapFusionBuildTreeMaterialized",
		"RunScheduledGoapFusionLoop",
	} {
		if !references(first, want) {
			t.Fatalf("the prepended Phase-0 preflight does not compose %q as a runnable Action node; the loop sequence would run without the materializer guard / bounded loop runner", want)
		}
		if GetAction(want) == nil {
			t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", want)
		}
	}

	// The original loop children must be preserved, in order, after the preflight.
	if got[1].Name != original[0].Name || got[2].Name != original[1].Name {
		t.Fatalf("original loop children not preserved in order after the preflight: %+v", got)
	}

	// The seam must not mutate the caller's slice.
	if len(original) != 2 || original[0].Name != "SetupFusionTools" || original[1].Name != "GrillMeNotebookLM" {
		t.Fatalf("PrependGoapFusionPreflight mutated the caller's slice: %+v", original)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesLoopRunner
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the bounded loop runner —
// RunScheduledGoapFusionLoop — as a runnable Action node, not only the
// build-tree materializer guard.
//
// The materializer guard proves the on-disk tree is fresh, but the loop runner
// is "the kernel the whole apparatus exists to protect": it consults the shared
// circuit-breaker window and the runaway-loop backstop to decide whether the
// scheduled cycle may drive another iteration or must HALT on
// Activity-Progress Confusion. GoapFusionPreflightNode()'s own contract
// (actions_superpowers.go) explicitly reserves room to "append the
// circuit-breaker/loop-runner pair as additional preflight children"; until it
// does, a scheduled cycle that embeds the preflight would materialize a fresh
// tree yet never gate the run on the loop runner, so a non-progressing loop
// could still iterate unchecked.
//
// This test asserts the preflight sequence references RunScheduledGoapFusionLoop
// as a registered Action node. It fails while the builder composes only the
// materializer guard (RED) and passes once the loop runner is appended as a
// preflight child (GREEN). The engine package cannot import internal/domains
// (import cycle), so this runnable-composition contract is pinned here at the
// action's own package, ready for the domains tree to embed as its Phase-0
// preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesLoopRunner(t *testing.T) {
	const want = "RunScheduledGoapFusionLoop"

	node := GoapFusionPreflightNode()

	var references func(n evolution.SerializableNode) bool
	references = func(n evolution.SerializableNode) bool {
		if n.Type == "Action" && n.Name == want {
			return true
		}
		for _, c := range n.Children {
			if references(c) {
				return true
			}
		}
		return false
	}

	if !references(node) {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner as a runnable Action node; the preflight materializes the build tree but never gates the scheduled cycle on the bounded loop runner, so a non-progressing loop could iterate unchecked", want)
	}

	if GetAction(want) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", want)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesCircuitPolicy
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the CIRCUITPOLICY config guard —
// VerifyScheduledGoapFusionCircuitPolicy — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// RunScheduledGoapFusionLoop's entire halt/continue decision is derived from the
// bounded state-hash window whose size is goapFusionCircuitHistoryWindow; if that
// window is not a positive, bounded value the loop runner has no CIRCUITPOLICY
// window to detect repeated state hashes and could spin indefinitely — the
// "Activity-Progress Confusion" tail the P0 NotebookLM research goal names.
// VerifyScheduledGoapFusionCircuitPolicy is the config preflight that proves the
// window is sound, yet GoapFusionPreflightNode() composes only the build-tree
// materializer and the loop runner — never the circuit-policy guard — so a
// scheduled cycle would drive the loop runner without first proving its
// CIRCUITPOLICY window is valid.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionCircuitPolicy as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder
// composes only the materializer guard and the loop runner (RED) and passes once
// the circuit-policy guard is inserted ahead of the loop runner (GREEN). The
// engine package cannot import internal/domains (import cycle), so this
// runnable-composition contract is pinned here at the action's own package, ready
// for the domains tree to embed as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesCircuitPolicy(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionCircuitPolicy"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q config guard as a runnable Action node; the preflight drives the loop runner without first proving its CIRCUITPOLICY window is a positive, bounded value", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the circuit-policy guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q config guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the loop runner's CIRCUITPOLICY window is proven sound before it drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGitTool
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the git-binary guard —
// VerifyScheduledGoapFusionGitTool — as a runnable Action node ordered BEFORE
// the bounded loop runner it protects.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's
// Superpowers implementation step shells out to `git` via runGoapShell — `git
// checkout`, `git fetch origin`, `git pull origin master --ff-only`, `git
// status`, `git stash`, `git diff`, `git reset --hard`, `git clean`, and `git
// push origin master` — to synchronize, isolate, and publish every improvement.
// VerifyScheduledGoapFusionGitTool is the guard whose own doc comment states a
// scheduled run "could otherwise pass every tool guard (Claude Code, Go
// toolchain, graphify, NotebookLM) yet still fail at the very first git sync
// when `git` is not installed or not on PATH" — so it must prove the `git`
// binary is resolvable on PATH before the loop runner drives a cycle whose
// fixes it can never commit or publish. Yet GoapFusionPreflightNode() composes
// only the build-tree materializer, the circuit-policy config guard, the
// rejected-context ledger, the runtime guard, the toolchain guard, and the
// plans-writable guard before the loop runner — never the git-tool guard — so a
// scheduled cycle could gate on the loop runner and only then discover at the
// first git operation that the `git` binary is absent, wasting the cycle with
// no early diagnosis.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionGitTool as a registered Action node AND that it is
// ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the git-tool guard (RED) and passes once the git-tool guard is inserted ahead
// of the loop runner (GREEN). The engine package cannot import internal/domains
// (import cycle), so this runnable-composition contract is pinned here at the
// action's own package, ready for the domains tree to embed as its Phase-0
// preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGitTool(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionGitTool"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q git-binary guard as a runnable Action node; the preflight drives the loop runner without first proving the `git` binary is resolvable on PATH, so a scheduled cycle would only discover its git binary is missing at the first git sync of the implementation step", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the git-tool guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q git-binary guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the `git` binary the implementation step needs to commit and publish fixes is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesRejectedContextLedger
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the safety-drift monotonicity guard —
// VerifyScheduledGoapFusionRejectedContextLedger — as a runnable Action node
// ordered BEFORE the bounded loop runner it protects.
//
// The rejected-context ledger guard is the "Experience-Grounded Monotonicity
// Auditor" kernel: because RunScheduledGoapFusionLoop re-runs the
// research-to-implementation cycle indefinitely, a later iteration can generate
// a high-fitness improvement that re-admits a previously rejected unsafe context
// — the "Safety Drift" failure mode [Source 207, 214, 215, 250]. Enforcing the
// Monotonicity Invariant requires replaying the persistent rejected-context
// ledger against every new candidate before the loop drives another iteration.
// Yet GoapFusionPreflightNode() composes only the build-tree materializer, the
// circuit-policy config guard, and the loop runner — never the rejected-context
// ledger guard — so a scheduled cycle would drive the loop runner without first
// proving its historical safety-regression kernel is present and readable,
// leaving safety drift unmitigated exactly where the loop runner needs it.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionRejectedContextLedger as a registered Action node AND
// that it is ordered before RunScheduledGoapFusionLoop. It fails while the
// builder composes only the materializer guard, the circuit-policy guard, and
// the loop runner (RED) and passes once the rejected-context ledger guard is
// inserted ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed
// as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesRejectedContextLedger(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionRejectedContextLedger"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q safety-drift monotonicity guard as a runnable Action node; the preflight drives the loop runner without first replaying the rejected-context ledger, leaving Safety Drift unmitigated where the loop runner needs it", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the rejected-context ledger guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q safety-drift guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the historical safety-regression kernel is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesToolchain
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the Go-toolchain guard —
// VerifyScheduledGoapFusionToolchain — as a runnable Action node ordered BEFORE
// the bounded loop runner it protects.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's build
// and TDD verification step shells out to the hardcoded Go toolchain
// (goapFusionGoBin) to compile and test every improvement.
// VerifyScheduledGoapFusionToolchain is the guard whose own doc comment states a
// scheduled run "could pass every other preflight yet still fail at verification
// when that toolchain is missing or not executable" — so it must prove the Go
// toolchain binary is an executable file before the loop runner drives a cycle
// it can never build. Yet GoapFusionPreflightNode() composes only the build-tree
// materializer, the circuit-policy config guard, the rejected-context ledger,
// and the runtime guard before the loop runner — never the toolchain guard — so
// a scheduled cycle could gate on the loop runner and only then discover at the
// build+TDD step that the Go toolchain it needs is absent, wasting the cycle
// with no early diagnosis.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionToolchain as a registered Action node AND that it is
// ordered before RunScheduledGoapFusionLoop. It fails while the builder omits the
// toolchain guard (RED) and passes once the toolchain guard is inserted ahead of
// the loop runner (GREEN). The engine package cannot import internal/domains
// (import cycle), so this runnable-composition contract is pinned here at the
// action's own package, ready for the domains tree to embed as its Phase-0
// preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesToolchain(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionToolchain"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q Go-toolchain guard as a runnable Action node; the preflight drives the loop runner without first proving the Go toolchain binary is an executable file, so a scheduled cycle would only discover its toolchain is missing at the build+TDD step", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the toolchain guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q Go-toolchain guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the toolchain the build+TDD step needs is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesPlansWritable
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the plan-output-location guard —
// VerifyScheduledGoapFusionPlansWritable — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle writes a
// Superpowers implementation plan and, on an incomplete Claude run, saves the
// failed patch into the plans directory (goapFusionPlansDir).
// VerifyScheduledGoapFusionPlansWritable is the guard whose own doc comment
// states a scheduled run "could pass every other preflight yet still fail when
// that plans directory is missing or not writable, losing its plan and patch
// with no clear diagnosis" — so it must prove the plans directory is a writable
// directory before the loop runner drives a cycle whose output it can never
// persist. Yet GoapFusionPreflightNode() composes only the build-tree
// materializer, the circuit-policy config guard, the rejected-context ledger,
// and the runtime guard before the loop runner — never the plans-writable guard
// — so a scheduled cycle could gate on the loop runner and only then discover at
// the plan/patch output step that its output location is unwritable, wasting the
// cycle with no early diagnosis.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionPlansWritable as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the plans-writable guard (RED) and passes once the plans-writable guard is
// inserted ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed
// as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesPlansWritable(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionPlansWritable"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q plan-output-location guard as a runnable Action node; the preflight drives the loop runner without first proving the plans directory is a writable directory, so a scheduled cycle would only discover its output location is unwritable at the plan/patch output step", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the plans-writable guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q plan-output-location guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the output location the cycle persists its plan and patch to is proven writable before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGitRemote
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the git-`origin`-remote guard —
// VerifyScheduledGoapFusionGitRemote — as a runnable Action node ordered BEFORE
// the bounded loop runner it protects, and immediately AFTER the git-binary
// guard that is its own stated prerequisite.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's
// Superpowers implementation step synchronizes its worktree against origin
// before letting Claude implement (`git fetch origin`, `git pull origin master
// --ff-only`) and publishes the result afterwards (`git push origin master`).
// The already-composed VerifyScheduledGoapFusionGitTool guard only proves the
// `git` binary is resolvable on PATH; its own doc comment names
// VerifyScheduledGoapFusionGitRemote as the successor guard that proves the
// `origin` remote is actually configured. Without it a scheduled run could pass
// the git-binary guard yet still fail at the fetch/pull sync
// (goap_fusion_preflight_failed) — or silently degrade at push — when `origin`
// is unconfigured or unreachable, wasting the cycle with no early diagnosis. Yet
// GoapFusionPreflightNode() composes the git-binary guard and the loop runner but
// never the git-remote guard, so a scheduled cycle could gate on the loop runner
// and only then discover at the first origin sync that no remote is configured.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionGitRemote as a registered Action node, that it is
// ordered before RunScheduledGoapFusionLoop, and that it follows the git-binary
// guard it depends on. It fails while the builder omits the git-remote guard
// (RED) and passes once the git-remote guard is inserted after the git-tool guard
// and ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed
// as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGitRemote(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionGitRemote"
		gitTool    = "VerifyScheduledGoapFusionGitTool"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects
	// and follow the git-binary guard it depends on).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	gitToolIdx := indexOf(gitTool)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q git-`origin`-remote guard as a runnable Action node; the preflight drives the loop runner without first proving the `origin` remote is configured, so a scheduled cycle would only discover its remote is missing at the first origin fetch/pull sync of the implementation step", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the git-remote guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q git-`origin`-remote guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the `origin` remote the implementation step syncs and publishes against is proven configured before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}
	if gitToolIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q git-binary guard; the git-remote guard's prerequisite must run before it", gitTool)
	}
	if gitToolIdx >= guardIdx {
		t.Fatalf("expected the %q git-binary guard (index %d) to be composed BEFORE the %q git-remote guard (index %d), so a missing `git` binary is diagnosed as its real cause rather than surfacing as a misleading \"origin remote is not configured\" failure", gitTool, gitToolIdx, guard, guardIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesNotebookLMTool
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the NotebookLM-binary guard —
// VerifyScheduledGoapFusionNotebookLMTool — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// The P0 NotebookLM research goal makes independent NotebookLM research the first
// step of every scheduled cycle: once RunScheduledGoapFusionLoop decides
// CONTINUE, the cycle's RunGoapFusionNotebookLMResearch step shells out to the
// `nlm` binary (nlmBin) and hard-fails ("refusing to proceed from stale vault
// research") when it is unavailable. VerifyScheduledGoapFusionNotebookLMTool is
// the guard whose own doc comment states a scheduled run "could pass every
// current preflight yet still abort at the research step when that binary is
// missing or not executable, wasting the cycle with no early diagnosis" — so it
// must prove the `nlm` binary is an executable file before the loop runner drives
// a cycle whose research it can never gather. Yet GoapFusionPreflightNode()
// composes the build-tree materializer, the circuit-policy config guard, the
// rejected-context ledger, the runtime, toolchain, plans-writable, git-tool,
// git-remote, and vault-writable guards before the loop runner — never the
// NotebookLM-tool guard — so a scheduled cycle could gate on the loop runner and
// only then discover at the research step that the `nlm` binary is absent,
// wasting the cycle and degrading to stale vault research with no early
// diagnosis. VerifyScheduledGoapFusionNotebookLMTool is registered and
// unit-tested (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebookLMTool)
// yet wired into no composed tree, so it can never run in a scheduled cycle.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionNotebookLMTool as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the NotebookLM-tool guard (RED) and passes once the NotebookLM-tool guard is
// inserted ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed
// as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesNotebookLMTool(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionNotebookLMTool"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q NotebookLM-binary guard as a runnable Action node; the preflight drives the loop runner without first proving the `nlm` binary is an executable file, so a scheduled cycle would only discover its NotebookLM binary is missing at the research step and degrade to stale vault research", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the NotebookLM-tool guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q NotebookLM-binary guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the `nlm` binary the research step needs is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesVaultWritable
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the vault-output-location guard —
// VerifyScheduledGoapFusionVaultWritable — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's
// WriteFusionAnalysis step persists its per-run gap analysis
// (goap-fusion-analysis-<ts>.md) and a rolling pointer (goap-fusion-latest.md)
// directly into the vault research directory (goapFusionVaultDir) via
// os.WriteFile, and the next scheduled run's ReadVaultResearch step ingests
// those files as part of its research corpus.
// VerifyScheduledGoapFusionVaultWritable is the guard whose own doc comment
// states a scheduled run "could pass every current preflight yet still fail when
// the vault directory is not writable, silently dropping its own analysis and
// starving the next run's research corpus with no clear diagnosis" — so it must
// prove the vault directory is a writable directory before the loop runner drives
// a cycle whose analysis it can never persist. The already-composed
// VerifyScheduledGoapFusionPlansWritable guard proves a distinct directory
// (goapFusionPlansDir) is writable; it does not cover this vault directory. Yet
// GoapFusionPreflightNode() composes only the build-tree materializer, the
// circuit-policy config guard, the rejected-context ledger, the runtime guard,
// the toolchain guard, the plans-writable guard, the git-tool guard, and the
// git-remote guard before the loop runner — never the vault-writable guard — so a
// scheduled cycle could gate on the loop runner and only then discover at the
// WriteFusionAnalysis step that its vault output location is unwritable, wasting
// the cycle and starving the next run's research corpus with no early diagnosis.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionVaultWritable as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the vault-writable guard (RED) and passes once the vault-writable guard is
// inserted ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed
// as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesVaultWritable(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionVaultWritable"
		loopRunner = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q vault-output-location guard as a runnable Action node; the preflight drives the loop runner without first proving the vault directory is a writable directory, so a scheduled cycle would only discover its vault output location is unwritable at the WriteFusionAnalysis step and starve the next run's research corpus", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the vault-writable guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q vault-output-location guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the vault directory the cycle persists its analysis into is proven writable before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesNotebook
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the NotebookLM-notebook-id guard —
// VerifyScheduledGoapFusionNotebook — as a runnable Action node ordered BEFORE
// the bounded loop runner it protects, and immediately AFTER the
// NotebookLM-binary guard that is its own stated prerequisite.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's
// RunGoapFusionNotebookLMResearch step shells out to `nlm notebook query
// <defaultNotebook> ...` to gather the independent NotebookLM research the P0
// research goal makes the first step of every cycle. The already-composed
// VerifyScheduledGoapFusionNotebookLMTool guard only proves the `nlm` binary is
// an executable file; its own doc comment names VerifyScheduledGoapFusionNotebook
// as the successor guard that proves a notebook id is actually configured.
// Without it a scheduled run could pass the binary guard yet still query against
// no notebook when defaultNotebook is empty or unset, silently degrading the
// research corpus and producing a plan from stale vault research with no early
// diagnosis — the notebook-id analogue of how VerifyScheduledGoapFusionGitRemote
// follows VerifyScheduledGoapFusionGitTool. Yet GoapFusionPreflightNode()
// composes the NotebookLM-binary guard and the loop runner but never the
// notebook-id guard, so a scheduled cycle could gate on the loop runner and only
// then discover at the research step that no notebook is configured.
// VerifyScheduledGoapFusionNotebook is registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebook) yet
// wired into no composed tree, so it can never run in a scheduled cycle.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionNotebook as a registered Action node, that it is
// ordered before RunScheduledGoapFusionLoop, and that it follows the
// NotebookLM-binary guard it depends on. It fails while the builder omits the
// notebook-id guard (RED) and passes once the notebook-id guard is inserted after
// the NotebookLM-tool guard and ahead of the loop runner (GREEN). The engine
// package cannot import internal/domains (import cycle), so this
// runnable-composition contract is pinned here at the action's own package, ready
// for the domains tree to embed as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesNotebook(t *testing.T) {
	const (
		guard        = "VerifyScheduledGoapFusionNotebook"
		notebookTool = "VerifyScheduledGoapFusionNotebookLMTool"
		loopRunner   = "RunScheduledGoapFusionLoop"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the guard must precede the loop runner it protects
	// and follow the NotebookLM-binary guard it depends on).
	var order []string
	var collect func(n evolution.SerializableNode)
	collect = func(n evolution.SerializableNode) {
		if n.Type == "Action" {
			order = append(order, n.Name)
		}
		for _, c := range n.Children {
			collect(c)
		}
	}
	collect(node)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	guardIdx := indexOf(guard)
	notebookToolIdx := indexOf(notebookTool)
	loopIdx := indexOf(loopRunner)

	if guardIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q NotebookLM-notebook-id guard as a runnable Action node; the preflight drives the loop runner without first proving a NotebookLM notebook id is configured, so a scheduled cycle would only discover it queries against no notebook at the research step and degrade to stale vault research", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the notebook-id guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q NotebookLM-notebook-id guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the notebook id the research step queries against is proven configured before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}
	if notebookToolIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q NotebookLM-binary guard; the notebook-id guard's prerequisite must run before it", notebookTool)
	}
	if notebookToolIdx >= guardIdx {
		t.Fatalf("expected the %q NotebookLM-binary guard (index %d) to be composed BEFORE the %q notebook-id guard (index %d), so a missing `nlm` binary is diagnosed as its real cause rather than surfacing as a misleading empty-notebook failure", notebookTool, notebookToolIdx, guard, guardIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}
