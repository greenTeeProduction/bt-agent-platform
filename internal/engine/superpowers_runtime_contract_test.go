package engine

import (
	"context"
	"fmt"
	"reflect"
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

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopHaltsOnConsecutiveNoopPatches
// pins the SECOND CIRCUITPOLICY halt condition the P0 NotebookLM research goal
// names but the loop runner never implements: halting on a run of consecutive
// no-op patch proposals. The VerifyScheduledGoapFusionCircuitPolicy guard's own
// contract promises the loop halts "on detecting a repeated state hash or a run of
// consecutive no-op patches" (actions_superpowers.go), and the circuit breaker's
// doc names "detecting and halting state-transition cycles and repeated no-op
// patch proposals" — yet goapFusionCircuitBreakerVerdict only scans the bounded
// state-hash window for a repeat. That leaves the distinct "Activity-Progress
// Confusion" tail uncaught: a loop can publish a run of DISTINCT state hashes — so
// neither the repeated-hash circuit breaker nor, under the runaway-loop backstop,
// the finite-iteration guard fires — while every proposed patch is a no-op that
// never advances the goal. The loop stays "active" producing syntactically valid
// but empty patches indefinitely, exactly the failure mode the CIRCUITPOLICY
// promises to catch but does not.
//
// The loop runner publishes its consecutive-no-op-patch streak on the blackboard
// under "goap_fusion_noop_patch_streak"; a bounded run of no-op patches must HALT
// (-1) the loop even when the state hashes are all distinct. This test asserts
// RunScheduledGoapFusionLoop HALTs on a run of consecutive no-op patches with
// distinct state hashes, and CONTINUEs when no no-op streak is present. It fails
// today because the loop runner returns CONTINUE for distinct hashes regardless of
// the no-op streak (RED) and passes once the loop runner halts on a bounded
// consecutive-no-op-patch run (GREEN) — the no-op-streak analogue of the
// repeated-state-hash circuit breaker.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopHaltsOnConsecutiveNoopPatches(t *testing.T) {
	action := GetAction("RunScheduledGoapFusionLoop")
	if action == nil {
		t.Fatalf("missing production Superpowers action %q", "RunScheduledGoapFusionLoop")
	}

	// Distinct state hashes so neither the repeated-hash circuit breaker nor the
	// runaway-loop backstop fires; a run of consecutive no-op patch proposals is the
	// only halt signal present. The loop runner must still HALT — the loop is
	// "active" proposing empty patches that never advance the goal, the
	// Activity-Progress Confusion the CIRCUITPOLICY exists to break.
	noop := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes":      []string{"aaa", "bbb", "ccc"},
			"goap_fusion_noop_patch_streak": 10,
		},
	}
	noopCtx := &btcore.BTContext[Blackboard]{Blackboard: noop}
	if status := action(noopCtx); status != -1 {
		t.Fatalf("expected HALT (-1) on a run of consecutive no-op patch proposals even with distinct state hashes, got %d", status)
	}
	if !strings.Contains(noop.Result, "HALT") {
		t.Fatalf("expected a HALT diagnosis in Result on a consecutive no-op patch run, got %q", noop.Result)
	}

	// No consecutive no-op patch run (and distinct hashes under the backstop) lets
	// the loop runner CONTINUE (1) to drive the next cycle.
	progress := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes":      []string{"aaa", "bbb", "ccc"},
			"goap_fusion_noop_patch_streak": 0,
		},
	}
	progressCtx := &btcore.BTContext[Blackboard]{Blackboard: progress}
	if status := action(progressCtx); status != 1 {
		t.Fatalf("expected CONTINUE (1) when no consecutive no-op patch run is present and state hashes are distinct, got %d", status)
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

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitPolicyVerdictFoldsNoopStreak
// pins the LAST piece of single-source-of-truth the P0 NotebookLM research goal
// requires: the *entire* CIRCUITPOLICY halt decision — the repeated-state-hash
// cycle AND the consecutive-no-op-patch streak — must be decided in ONE shared
// helper that both EvaluateScheduledGoapFusionCircuitBreaker and
// RunScheduledGoapFusionLoop delegate to, not each re-implementing
// `streak >= goapFusionMaxNoopPatchStreak` inline (actions_superpowers.go:810 in
// the breaker, :866 in the loop runner). goapFusionCircuitBreakerVerdict already
// centralizes the state-hash verdict precisely so the two callers "can never drift
// on what counts as a trip," yet the no-op-streak halt is copy-pasted into both —
// and its HALT messages have already diverged in wording. A future change to the
// no-op semantics (`>=` vs `>`, the bound, the message) must currently be made in
// two places, reintroducing the exact drift the surrounding comments claim to
// eliminate.
//
// This test asserts a single shared verdict helper —
// goapFusionCircuitPolicyVerdict(hashes []string, noopStreak int) (halt bool,
// window []string, repeated string, noopTripped bool) — exists and folds BOTH halt
// conditions into one decision: it decides HALT on a repeated state hash within the
// bounded window (reporting the repeated hash, noopTripped=false) and HALT on a
// consecutive-no-op-patch streak at or over goapFusionMaxNoopPatchStreak even when
// every state hash is distinct (noopTripped=true, no repeated hash), and CONTINUE
// only when neither condition trips. It fails to compile until the folded verdict
// helper is introduced (RED), and passes once both actions delegate their entire
// CIRCUITPOLICY halt decision to it (GREEN). The public-action behavior of both
// actions stays pinned by
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHalts,
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopRunner, and
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopHaltsOnConsecutiveNoopPatches,
// so after folding both continue to enforce identical semantics — now the entire
// halt DECISION from a single source of truth.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitPolicyVerdictFoldsNoopStreak(t *testing.T) {
	// A repeated state hash within the bounded window with no no-op streak: the
	// state-hash cycle alone decides HALT, reports the repeated hash, and marks the
	// no-op condition as NOT the tripping cause.
	halt, window, repeated, noopTripped := goapFusionCircuitPolicyVerdict([]string{"aaa", "bbb", "aaa"}, 0)
	if !halt {
		t.Fatalf("expected halt=true on a repeated state hash within the window, got false (window=%v)", window)
	}
	if repeated != "aaa" {
		t.Fatalf("expected the repeated hash reported as %q, got %q", "aaa", repeated)
	}
	if noopTripped {
		t.Fatalf("expected noopTripped=false when the halt is a state-hash cycle, got true")
	}
	if len(window) != 3 {
		t.Fatalf("expected the returned window to hold all 3 hashes, got %d: %v", len(window), window)
	}

	// Distinct state hashes so the bounded-window dedup never trips, but a
	// consecutive-no-op-patch streak at the bound: the folded no-op halt must decide
	// HALT with noopTripped=true and no repeated hash — the "Activity-Progress
	// Confusion" tail the state-hash scan alone cannot catch.
	halt, _, repeated, noopTripped = goapFusionCircuitPolicyVerdict([]string{"aaa", "bbb", "ccc"}, goapFusionMaxNoopPatchStreak)
	if !halt {
		t.Fatalf("expected halt=true on a consecutive no-op-patch streak at the bound even with distinct hashes, got false")
	}
	if !noopTripped {
		t.Fatalf("expected noopTripped=true when the halt is the consecutive no-op-patch streak, got false")
	}
	if repeated != "" {
		t.Fatalf("expected no repeated hash reported for a no-op-streak halt, got %q", repeated)
	}

	// Distinct hashes and a streak BELOW the bound: neither condition trips, so the
	// folded verdict must decide CONTINUE.
	halt, _, _, noopTripped = goapFusionCircuitPolicyVerdict([]string{"aaa", "bbb", "ccc"}, goapFusionMaxNoopPatchStreak-1)
	if halt {
		t.Fatalf("expected halt=false on distinct hashes with a sub-bound no-op streak, got true")
	}
	if noopTripped {
		t.Fatalf("expected noopTripped=false on a sub-bound no-op streak, got true")
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

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGatesClaudeImplementation
// pins the integration seam the P0 NotebookLM research goal names in its own words:
// the production loop must "gate the Superpowers implementation path on
// EvaluateScheduledGoapFusionCircuitBreaker / RunScheduledGoapFusionLoop" so those
// CIRCUITPOLICY nodes run BEFORE RunSuperpowersClaudeImplementation. Every prior
// increment wired the guards and the loop runner into the Phase-0
// GoapFusionPreflightNode() and taught GoapFusionLoop_Main to prepend that preflight
// via PrependGoapFusionPreflight, but nothing gates the implementation subtree
// itself: the production ClaudeSuperpowersPath runs
// WriteSuperpowersImplementationPlan and then RunSuperpowersClaudeImplementation
// inside a HumanApprovalGate with no circuit-breaker / loop-runner check in front of
// it. A non-progressing loop that reached the implementation path could therefore
// still shell out to Claude Code to implement — the very "Activity-Progress
// Confusion" the circuit breaker exists to break — because the gate lives only in
// the top-level preflight, not immediately ahead of the implementation step it must
// protect.
//
// The engine package cannot import internal/domains (import cycle), but
// domains -> engine is the safe direction, so this seam belongs here at the guards'
// own package: PrependGoapFusionImplementationGate takes the Superpowers
// implementation path's child list (the WriteSuperpowersImplementationPlan +
// RunSuperpowersClaudeImplementation subtree) and returns a new list with the
// CIRCUITPOLICY gate — EvaluateScheduledGoapFusionCircuitBreaker then
// RunScheduledGoapFusionLoop — prepended as the first children, so the
// implementation only proceeds after the gate returns CONTINUE. This test asserts
// the seam (1) composes both gate nodes as runnable Action nodes ordered BEFORE
// RunSuperpowersClaudeImplementation, (2) keeps the circuit-breaker evaluation ahead
// of the loop runner, (3) preserves the original implementation children in order
// after the gate, and (4) does not mutate the caller's slice.
//
// It fails to compile until PrependGoapFusionImplementationGate is introduced (RED)
// and passes once the seam returns the gate-prepended child list (GREEN), ready for
// GoapFusionLoopTree()'s ClaudeSuperpowersPath to embed without duplicating the
// composition.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGatesClaudeImplementation(t *testing.T) {
	const (
		circuitBreaker = "EvaluateScheduledGoapFusionCircuitBreaker"
		loopRunner     = "RunScheduledGoapFusionLoop"
		impl           = "RunSuperpowersClaudeImplementation"
	)

	// Mirror the production ClaudeSuperpowersPath implementation subtree: the plan
	// writer followed by the HumanApprovalGate wrapping the Claude implementation
	// and its build verification.
	implChildren := []evolution.SerializableNode{
		{Type: "Action", Name: "WriteSuperpowersImplementationPlan"},
		{
			Type: "HumanApprovalGate",
			Name: "ApproveGoapFusionApply",
			Children: []evolution.SerializableNode{
				{Type: "Action", Name: impl},
				{Type: "Action", Name: "VerifyGoapBuild"},
			},
		},
	}

	got := PrependGoapFusionImplementationGate(implChildren)

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the gate must precede the implementation it protects).
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
	for _, n := range got {
		collect(n)
	}

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	cbIdx := indexOf(circuitBreaker)
	loopIdx := indexOf(loopRunner)
	implIdx := indexOf(impl)

	if cbIdx < 0 {
		t.Fatalf("PrependGoapFusionImplementationGate does not compose the %q circuit-breaker evaluation as a runnable Action node; the implementation path would run RunSuperpowersClaudeImplementation without a CIRCUITPOLICY gate in front of it", circuitBreaker)
	}
	if loopIdx < 0 {
		t.Fatalf("PrependGoapFusionImplementationGate does not compose the %q loop runner as a runnable Action node; the implementation path would run RunSuperpowersClaudeImplementation without the bounded loop-runner gate in front of it", loopRunner)
	}
	if implIdx < 0 {
		t.Fatalf("PrependGoapFusionImplementationGate dropped the %q implementation node; the gate must preserve the implementation path it protects", impl)
	}
	if cbIdx >= implIdx {
		t.Fatalf("expected the %q circuit-breaker evaluation (index %d) to be composed BEFORE the %q implementation node (index %d), so a detected Activity-Progress Confusion cycle HALTs the path before Claude Code implements", circuitBreaker, cbIdx, impl, implIdx)
	}
	if loopIdx >= implIdx {
		t.Fatalf("expected the %q loop runner (index %d) to be composed BEFORE the %q implementation node (index %d), so a non-progressing loop HALTs the path before Claude Code implements", loopRunner, loopIdx, impl, implIdx)
	}
	if cbIdx >= loopIdx {
		t.Fatalf("expected the %q circuit-breaker evaluation (index %d) to run BEFORE the %q loop runner (index %d), matching the preflight's gate ordering", circuitBreaker, cbIdx, loopRunner, loopIdx)
	}

	if GetAction(circuitBreaker) == nil {
		t.Fatalf("gate composes Action %q but it is not a registered, runnable action", circuitBreaker)
	}
	if GetAction(loopRunner) == nil {
		t.Fatalf("gate composes Action %q but it is not a registered, runnable action", loopRunner)
	}

	// The original implementation children must be preserved, in order, after the
	// gate — the gate only prepends, it never reorders or drops the path it wraps.
	if len(got) != len(implChildren)+2 {
		t.Fatalf("expected the CIRCUITPOLICY gate (2 nodes) prepended (len %d), got %d: %+v", len(implChildren)+2, len(got), got)
	}
	if got[len(got)-2].Name != "WriteSuperpowersImplementationPlan" || got[len(got)-1].Name != "ApproveGoapFusionApply" {
		t.Fatalf("original implementation children not preserved in order after the gate: %+v", got)
	}

	// The seam must not mutate the caller's slice.
	if len(implChildren) != 2 || implChildren[0].Name != "WriteSuperpowersImplementationPlan" || implChildren[1].Name != "ApproveGoapFusionApply" {
		t.Fatalf("PrependGoapFusionImplementationGate mutated the caller's slice: %+v", implChildren)
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

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightSoftensRejectedContextLedger
// pins the increment the "goap-fusion-loop-runner" goal actually requires to make
// the preflight go live without regressing to a worse-than-no-op HALT: the fatal
// VerifyScheduledGoapFusionRejectedContextLedger guard must be Selector-wrapped with
// an AlwaysSucceed fallback, exactly mirroring the two NotebookLM guards at
// actions_superpowers.go:1206-1233.
//
// The guard returns HALT (-1) when the rejected-context ledger is missing, unreadable,
// or empty (actions_superpowers.go:642-654), and that ledger
// (/mnt/ssd/clawd/wiki/bt-research/rejected-context-ledger.jsonl) is confirmed absent
// on disk. The sibling test above (…ComposesRejectedContextLedger) only proves the
// guard is composed ahead of the loop runner — but it is composed as a BARE Action
// child of the hard preflight Sequence (actions_superpowers.go:1104-1107), so once
// GoapFusionLoopTree() adopts WireGoapFusionLoopTree and the preflight goes live, the
// guard's FAILURE propagates straight to the enclosing Sequence and HALTs the loop on
// EVERY scheduled tick — a regression strictly worse than the current no-op.
//
// The NotebookLM guards already solved this exact problem: wrap the guard in a
// Selector whose second child is an AlwaysSucceed node, so the guard still runs and
// warns but its FAILURE is swallowed rather than propagated to the preflight Sequence.
// This test asserts the rejected-context ledger guard is (1) wrapped in a Selector
// rather than sitting bare in the Sequence and (2) that Selector carries an
// AlwaysSucceed fallback ordered AFTER the guard. It fails today because the guard is a
// bare Action child of the Sequence (RED) and passes once it is Selector-wrapped like
// the NotebookLM guards (GREEN).
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightSoftensRejectedContextLedger(t *testing.T) {
	const guard = "VerifyScheduledGoapFusionRejectedContextLedger"

	node := GoapFusionPreflightNode()

	// Locate the guard's DIRECT parent so we can prove it is Selector-wrapped
	// (non-fatal) rather than a bare Action child of the hard preflight Sequence.
	var parentOf func(n, parent evolution.SerializableNode) (evolution.SerializableNode, bool)
	parentOf = func(n, parent evolution.SerializableNode) (evolution.SerializableNode, bool) {
		if n.Type == "Action" && n.Name == guard {
			return parent, true
		}
		for _, c := range n.Children {
			if p, ok := parentOf(c, n); ok {
				return p, true
			}
		}
		return evolution.SerializableNode{}, false
	}

	parent, found := parentOf(node, evolution.SerializableNode{})
	if !found {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q guard at all; cannot assert it is softened", guard)
	}

	if parent.Type != "Selector" {
		t.Fatalf("expected the fatal %q guard to be wrapped in a Selector (mirroring the NotebookLM guards at actions_superpowers.go:1206-1233) so a missing/empty rejected-context ledger does not HALT the newly-live preflight on every scheduled tick; its direct parent is a %q node instead, so the guard's FAILURE (-1) propagates to the hard preflight Sequence and halts the loop every tick", guard, parent.Type)
	}

	// The Selector must carry an AlwaysSucceed fallback ordered AFTER the guard, so
	// the guard runs and warns but its FAILURE is swallowed rather than propagated.
	guardIdx, fallbackIdx := -1, -1
	for i, c := range parent.Children {
		if c.Type == "Action" && c.Name == guard {
			guardIdx = i
		}
		if c.Type == "AlwaysSucceed" {
			fallbackIdx = i
		}
	}
	if guardIdx < 0 {
		t.Fatalf("the Selector wrapping %q no longer contains the guard as an Action child: %+v", guard, parent.Children)
	}
	if fallbackIdx < 0 {
		t.Fatalf("expected an AlwaysSucceed fallback sibling in the Selector wrapping %q (mirroring the NotebookLM guards at actions_superpowers.go:1206-1233) so a missing ledger degrades to a warning instead of a HALT; got children %+v", guard, parent.Children)
	}
	if fallbackIdx <= guardIdx {
		t.Fatalf("expected the AlwaysSucceed fallback (index %d) to be ordered AFTER the %q guard (index %d) in the Selector, so the guard is attempted first and only its FAILURE falls through to the always-succeeding fallback", fallbackIdx, guard, guardIdx)
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

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebookGuardsNonFatal
// pins the P0 NotebookLM research goal's non-fatal requirement: the two external
// NotebookLM guards the scheduled GOAP fusion preflight composes —
// VerifyScheduledGoapFusionNotebookLMTool and VerifyScheduledGoapFusionNotebook —
// must be NON-FATAL within GoapFusionPreflightNode().
//
// Every other preflight guard protects an input, tool, or output location the cycle
// genuinely cannot run without, so a FAILURE there rightly aborts the top-level
// GoapFusionPreflight Sequence. But the `nlm` binary and the configured NotebookLM
// notebook id are optional enrichment: when they are absent the cycle should degrade
// to the existing vault research and STILL drive its research-to-implementation loop,
// not abort the whole scheduled cycle. Composed as direct Action children of the
// top-level Sequence (actions_superpowers.go), a single NotebookLM FAILURE
// short-circuits the entire preflight and the bounded loop runner never executes — the
// scheduled cycle silently stops improving whenever `nlm` is missing or unconfigured,
// exactly the abort the P0 goal calls out ("make the two NotebookLM guards non-fatal
// ... so they don't abort the cycle").
//
// This test asserts each NotebookLM guard's nearest enclosing composite parent is a
// Selector — the fallback node whose child FAILURE is caught rather than propagated —
// so the guard runs and warns but its failure cannot abort the enclosing Sequence. It
// fails while the guards are direct Sequence children (RED) and passes once each is
// Selector-wrapped as a non-fatal preflight child (GREEN). The engine package cannot
// import internal/domains (import cycle), so this non-fatal-composition contract is
// pinned here at the guards' own package, ready for GoapFusionLoopTree() to embed via
// PrependGoapFusionPreflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionNotebookGuardsNonFatal(t *testing.T) {
	node := GoapFusionPreflightNode()

	notebookGuards := []string{
		"VerifyScheduledGoapFusionNotebookLMTool",
		"VerifyScheduledGoapFusionNotebook",
	}

	// Walk the tree tracking the nearest enclosing composite (Selector/Sequence)
	// parent of each Action, so we can prove each NotebookLM guard's FAILURE is
	// swallowed by a Selector rather than propagated by a Sequence.
	parentType := map[string]string{}
	found := map[string]bool{}
	var walk func(n evolution.SerializableNode, parent string)
	walk = func(n evolution.SerializableNode, parent string) {
		if n.Type == "Action" {
			for _, g := range notebookGuards {
				if n.Name == g {
					parentType[g] = parent
					found[g] = true
				}
			}
		}
		nextParent := parent
		if n.Type == "Selector" || n.Type == "Sequence" {
			nextParent = n.Type
		}
		for _, c := range n.Children {
			walk(c, nextParent)
		}
	}
	walk(node, "")

	for _, g := range notebookGuards {
		if !found[g] {
			t.Fatalf("GoapFusionPreflightNode() no longer composes the %q NotebookLM guard as a runnable Action node; the optional NotebookLM enrichment must still run in the scheduled cycle", g)
		}
		if GetAction(g) == nil {
			t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", g)
		}
		if parentType[g] != "Selector" {
			t.Fatalf("expected the %q NotebookLM guard to be wrapped in a Selector (non-fatal) so its FAILURE cannot abort the GoapFusionPreflight Sequence, but its nearest composite parent is %q; an absent `nlm` binary or unset notebook id would short-circuit the whole preflight and the bounded loop runner would never run", g, parentType[g])
		}
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGraphifyTool
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the graphify-tool guard —
// VerifyScheduledGoapFusionGraphifyTool — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// The scheduled cycle's whole purpose is to read the vault research and the
// graphify report and derive its improvement gaps from them; the cycle's
// RunGraphifyUpdate step shells out to the external `graphify` command to
// regenerate that report before the gaps are derived.
// VerifyScheduledGoapFusionGraphifyTool is the guard whose own doc comment
// states a scheduled run "could pass every other preflight yet still fail when
// the graphify tool is not installed or not on PATH, leaving the cycle's gap
// analysis grounded in a stale report with no clear diagnosis" — so it must
// prove the `graphify` tool is resolvable on PATH before the loop runner drives
// a cycle whose report it can never refresh. Yet GoapFusionPreflightNode()
// composes the build-tree materializer, the circuit-policy config guard, the
// rejected-context ledger, the runtime, toolchain, plans-writable, git-tool,
// git-remote, vault-writable, and the two NotebookLM guards before the loop
// runner — never the graphify-tool guard — so a scheduled cycle could gate on
// the loop runner and only then discover at the RunGraphifyUpdate step that the
// `graphify` tool is absent, wasting the cycle and deriving its gaps from a
// stale report with no early diagnosis. VerifyScheduledGoapFusionGraphifyTool is
// registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphifyTool) yet
// wired into no composed tree, so it can never run in a scheduled cycle — the
// exact "registered but unwired" gap the preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionGraphifyTool as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the graphify-tool guard (RED) and passes once the graphify-tool guard is
// composed ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed
// as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGraphifyTool(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionGraphifyTool"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q graphify-tool guard as a runnable Action node; the preflight drives the loop runner without first proving the `graphify` tool is resolvable on PATH, so a scheduled cycle would only discover its graphify tool is missing at the RunGraphifyUpdate step and derive its gaps from a stale report", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the graphify-tool guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q graphify-tool guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the `graphify` tool the RunGraphifyUpdate step needs to refresh the report is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightSchemaValid
// pins the validation contract the "goap-fusion-loop-runner" goal actually drives
// toward: the Phase-0 preflight node GoapFusionPreflightNode() composes must be
// schema-valid — every node type it composes must be a known node type — so the
// production GoapFusionLoopTree() that embeds it (via PrependGoapFusionPreflight)
// survives BuildAndValidate/ValidateTreeFull instead of breaking the moment it is
// wired in.
//
// Every sibling test in this file only walks the composed tree and calls
// GetAction — none ever validates the node's schema. That blind spot hides a live
// defect: to make the two external NotebookLM guards non-fatal, the preflight now
// wraps each in a Selector whose fallback child is an "AlwaysSucceed" node
// (actions_superpowers.go). "AlwaysSucceed" is fully supported at runtime
// (engine.buildNode returns a success leaf) and is already emitted by production
// builders, yet it is absent from evolution.KnownNodeTypes — and both
// evolution.SerializableNode.Validate and engine.ValidateTreeFull reject any node
// whose Type is not in that map with `unknown node type "AlwaysSucceed"`. So the
// structural contract tests stay green (they never validate) while a scheduled
// cycle that passes the preflight through BuildAndValidate — the endpoint the
// goap-fusion-loop-runner wiring reaches — would fail validation.
//
// This test asserts GoapFusionPreflightNode() reports zero schema-validation
// errors. It fails today with `unknown node type "AlwaysSucceed"` (RED) and passes
// once "AlwaysSucceed" is admitted as a known node type so the real schema is the
// single source of truth (GREEN). The engine package cannot import
// internal/domains (import cycle), so this validation contract is pinned here at
// the preflight builder's own package, ready for the domains tree to embed and
// validate.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightSchemaValid(t *testing.T) {
	node := GoapFusionPreflightNode()

	if errs := node.Validate(); len(errs) > 0 {
		t.Fatalf("GoapFusionPreflightNode() is not schema-valid; every composed node type must be a known node type so the production tree that embeds it survives BuildAndValidate, but validation reported %d error(s):\n- %s", len(errs), strings.Join(errs, "\n- "))
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesResearchPresent
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the vault-research-corpus guard —
// VerifyScheduledGoapFusionResearchPresent — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// The scheduled cycle's whole purpose is to read the vault research and derive
// its improvement gaps from it; the cycle's ReadVaultResearch step ingests the
// vault research directory as its corpus. VerifyScheduledGoapFusionResearchPresent
// is the guard whose own doc comment states a vault directory that "exists but
// contains zero research files would still pass [VerifyScheduledGoapFusionInputs],
// letting a scheduled run silently produce a plan from no actual research" — so it
// must prove the vault directory holds at least one readable research file before
// the loop runner drives a cycle that would otherwise plan from an empty corpus.
// The already-composed VerifyScheduledGoapFusionVaultWritable guard only proves the
// vault directory is a writable directory; it does not confirm any research is
// present to read. Yet GoapFusionPreflightNode() composes the build-tree
// materializer, the circuit-policy config guard, the rejected-context ledger, the
// runtime, toolchain, plans-writable, git-tool, git-remote, vault-writable,
// graphify-tool, and the two NotebookLM guards before the loop runner — never the
// research-present guard — so a scheduled cycle could gate on the loop runner and
// only then produce a plan from an empty research corpus with no early diagnosis.
// VerifyScheduledGoapFusionResearchPresent is registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionResearchPresent) yet
// wired into no composed tree, so it can never run in a scheduled cycle — the exact
// "registered but unwired" gap the preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionResearchPresent as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the research-present guard (RED) and passes once the research-present guard is
// composed ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is pinned
// here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesResearchPresent(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionResearchPresent"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q vault-research-corpus guard as a runnable Action node; the preflight drives the loop runner without first proving the vault directory holds at least one readable research file, so a scheduled cycle would produce a plan from an empty research corpus", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the research-present guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q vault-research-corpus guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the vault research the cycle derives its gaps from is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesSynthesesWritable
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the syntheses-output-location guard —
// VerifyScheduledGoapFusionSynthesesWritable — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's
// RunGoapFusionNotebookLMResearch step writes a dedicated synthesis file
// (goap-fusion-notebooklm-<ts>.md) into the syntheses directory
// (goapFusionSynthesesDir) via writeString, and the immediately following
// ReadVaultResearch step ingests that newest synthesis as its highest-priority
// research input. VerifyScheduledGoapFusionSynthesesWritable is the guard whose
// own doc comment states a scheduled run "could pass every other preflight yet
// still fail when the syntheses directory is not writable, losing the freshly
// generated NotebookLM research with no clear diagnosis" — so it must prove the
// syntheses directory is a writable directory before the loop runner drives a
// cycle whose freshest research it can never persist. The already-composed
// VerifyScheduledGoapFusionVaultWritable and VerifyScheduledGoapFusionPlansWritable
// guards prove distinct directories (goapFusionVaultDir, goapFusionPlansDir) are
// writable; neither covers this syntheses directory. Yet GoapFusionPreflightNode()
// composes the build-tree materializer, the circuit-policy config guard, the
// rejected-context ledger, the runtime, toolchain, plans-writable, git-tool,
// git-remote, vault-writable, research-present, graphify-tool,
// graph-report-present, and the two NotebookLM guards before the loop runner —
// never the syntheses-writable guard — so a scheduled cycle could gate on the loop
// runner and only then discover at the RunGoapFusionNotebookLMResearch step that
// its syntheses output location is unwritable, losing the freshly generated
// research with no early diagnosis. VerifyScheduledGoapFusionSynthesesWritable is
// registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSynthesesWritable)
// yet wired into no composed tree, so it can never run in a scheduled cycle — the
// exact "registered but unwired" gap the preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionSynthesesWritable as a registered Action node AND that
// it is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the syntheses-writable guard (RED) and passes once the syntheses-writable guard
// is composed ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is pinned
// here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesSynthesesWritable(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionSynthesesWritable"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q syntheses-output-location guard as a runnable Action node; the preflight drives the loop runner without first proving the syntheses directory is a writable directory, so a scheduled cycle would only discover its syntheses output location is unwritable at the RunGoapFusionNotebookLMResearch step and lose the freshly generated NotebookLM research", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the syntheses-writable guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q syntheses-output-location guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the syntheses directory the cycle persists its freshest research into is proven writable before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesInputs
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the coarse research-inputs guard —
// VerifyScheduledGoapFusionInputs — as a runnable Action node ordered BEFORE the
// bounded loop runner it protects.
//
// VerifyScheduledGoapFusionInputs is the original, coarsest preflight guard: it
// confirms the two research inputs the whole scheduled cycle reads from — the
// vault research directory and the graphify report — actually exist before the
// cycle's ReadVaultResearch step and gap analysis derive an implementation plan
// from them, so a scheduled run fails fast with a clear "inputs missing"
// diagnosis instead of silently producing a plan from missing context. It is the
// foundational input guard the finer-grained VerifyScheduledGoapFusionResearchPresent
// (vault holds ≥1 file) and VerifyScheduledGoapFusionGraphReportPresent (report
// holds content) guards refine — their own doc comments each open by noting that
// "VerifyScheduledGoapFusionInputs only confirms" existence, not content. Yet
// GoapFusionPreflightNode() composes those refinements while never composing the
// foundational existence guard itself: VerifyScheduledGoapFusionInputs is
// registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionInputs) yet wired
// into no composed tree, so it can never run in a scheduled cycle — the exact
// "registered but unwired" gap the preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionInputs as a registered Action node AND that it is
// ordered before RunScheduledGoapFusionLoop. It fails while the builder omits the
// inputs guard (RED) and passes once the inputs guard is composed ahead of the
// loop runner (GREEN). The engine package cannot import internal/domains (import
// cycle), so this runnable-composition contract is pinned here at the action's
// own package, ready for the domains tree to embed as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesInputs(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionInputs"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q research-inputs guard as a runnable Action node; the preflight drives the loop runner without first proving the vault research directory and graphify report exist, so a scheduled cycle would silently produce a plan from missing research context", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the inputs guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q research-inputs guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the vault research directory and graphify report the cycle reads from are proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGraphReportPresent
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the graphify-report-content guard —
// VerifyScheduledGoapFusionGraphReportPresent — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// The scheduled cycle derives every improvement gap from the graphify report;
// after RunGraphifyUpdate regenerates it, the cycle's gap analysis reads that
// report's content. VerifyScheduledGoapFusionGraphReportPresent is the guard whose
// own doc comment states a "zero-byte or contentless graphify report would still
// pass [VerifyScheduledGoapFusionInputs], letting a scheduled run silently derive
// its improvement gaps from an empty report" — it is the report-content analogue of
// the already-composed VerifyScheduledGoapFusionResearchPresent vault-content guard.
// The preflight already composes the vault-content guard (ResearchPresent) and the
// graphify-tool guard (GraphifyTool), but GraphifyTool only proves the `graphify`
// binary is resolvable — not that the report it produced holds any content. Yet
// GoapFusionPreflightNode() composes the build-tree materializer, the circuit-policy
// config guard, the rejected-context ledger, the runtime, toolchain, plans-writable,
// git-tool, git-remote, vault-writable, research-present, graphify-tool, and the two
// NotebookLM guards before the loop runner — never the graph-report-present guard —
// so a scheduled cycle could gate on the loop runner and only then derive its gaps
// from a contentless graphify report with no early diagnosis.
// VerifyScheduledGoapFusionGraphReportPresent is registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphReportPresent)
// yet wired into no composed tree, so it can never run in a scheduled cycle — the
// exact "registered but unwired" gap the preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionGraphReportPresent as a registered Action node AND that
// it is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the graph-report-present guard (RED) and passes once the guard is composed ahead
// of the loop runner (GREEN). The engine package cannot import internal/domains
// (import cycle), so this runnable-composition contract is pinned here at the
// action's own package, ready for the domains tree to embed as its Phase-0
// preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGraphReportPresent(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionGraphReportPresent"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q graphify-report-content guard as a runnable Action node; the preflight drives the loop runner without first proving the graphify report holds readable content, so a scheduled cycle would derive its improvement gaps from a contentless report", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the graph-report-present guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q graphify-report-content guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the graphify report the cycle derives its gaps from is proven to hold content before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesSynthesesPresent
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the syntheses-corpus guard —
// VerifyScheduledGoapFusionSynthesesPresent — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// The scheduled cycle treats the syntheses directory (goapFusionSynthesesDir) as
// its highest-priority research input: the cycle's ReadVaultResearch step reads it
// first and newest-first but swallows a read error, so a syntheses directory that
// is missing, unreadable, or holds zero synthesis files would silently degrade the
// research corpus and let the cycle plan from the most recent research being
// absent. VerifyScheduledGoapFusionSynthesesPresent is the guard whose own doc
// comment states this gap — that the existing VerifyScheduledGoapFusionResearchPresent
// guard only covers the vault directory itself, "not this distinct syntheses
// subdirectory" — so it must prove the syntheses directory holds at least one
// readable synthesis file before the loop runner drives a cycle that would
// otherwise plan from an absent freshest research. It is the syntheses-content
// analogue of the already-composed VerifyScheduledGoapFusionResearchPresent
// vault-content and VerifyScheduledGoapFusionGraphReportPresent report-content
// guards. The already-composed VerifyScheduledGoapFusionSynthesesWritable guard
// only proves the syntheses directory is a writable directory; it does not confirm
// any synthesis is present to read. Yet GoapFusionPreflightNode() composes the
// build-tree materializer, the circuit-policy config guard, the rejected-context
// ledger, the runtime, toolchain, plans-writable, git-tool, git-remote,
// vault-writable, syntheses-writable, research-present, graphify-tool,
// graph-report-present, and the two NotebookLM guards before the loop runner —
// never the syntheses-present guard — so a scheduled cycle could gate on the loop
// runner and only then plan from an empty syntheses corpus with no early
// diagnosis. VerifyScheduledGoapFusionSynthesesPresent is registered and
// unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSynthesesPresent)
// yet wired into no composed tree, so it can never run in a scheduled cycle — the
// exact "registered but unwired" gap the preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionSynthesesPresent as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the syntheses-present guard (RED) and passes once the syntheses-present guard is
// composed ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is pinned
// here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesSynthesesPresent(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionSynthesesPresent"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q syntheses-corpus guard as a runnable Action node; the preflight drives the loop runner without first proving the syntheses directory holds at least one readable synthesis file, so a scheduled cycle would plan from an absent freshest research corpus", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the syntheses-present guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q syntheses-corpus guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the syntheses research the cycle treats as its highest-priority input is proven present before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphOutputWritable
// asserts the presence of the preflight action that guards the graphify-report
// OUTPUT directory the unattended scheduled GOAP fusion cycle regenerates its
// report into. The existing writable guards cover the three directories the cycle
// persists its own artifacts to — the plans directory
// (VerifyScheduledGoapFusionPlansWritable), the vault directory
// (VerifyScheduledGoapFusionVaultWritable), and the syntheses directory
// (VerifyScheduledGoapFusionSynthesesWritable) — but nothing confirms the cycle
// can persist the graphify report itself. The cycle's RunGraphifyUpdate step
// shells out to `graphify update .`, which regenerates
// `graphify-out/GRAPH_REPORT.md` (the directory of goapFusionGraphReport) — the
// very report every improvement gap is derived from. The existing
// VerifyScheduledGoapFusionGraphifyTool guard only proves the `graphify` binary is
// resolvable on PATH, and VerifyScheduledGoapFusionGraphReportPresent only proves
// the report already holds content; neither confirms graphify can WRITE a fresh
// report. A scheduled run could pass every current preflight yet still fail when
// that graphify-out directory is missing or not writable, leaving RunGraphifyUpdate
// unable to refresh the report so the cycle silently derives its gaps from a stale
// report with no clear diagnosis. This action closes that gap by requiring the
// graphify report's output directory to be a writable directory before the
// automatic research-to-implementation cycle proceeds — the graphify-output
// analogue of the plans-, vault-, and syntheses-writable guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphOutputWritable(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionGraphOutputWritable") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionGraphOutputWritable")
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGraphOutputWritable
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the graphify-report-OUTPUT-location guard —
// VerifyScheduledGoapFusionGraphOutputWritable — as a runnable Action node ordered
// BEFORE the bounded loop runner it protects.
//
// Once RunScheduledGoapFusionLoop decides CONTINUE, the scheduled cycle's
// RunGraphifyUpdate step shells out to `graphify update .`, which regenerates the
// graphify report inside its output directory (the directory of
// goapFusionGraphReport) — the very report every improvement gap is derived from.
// VerifyScheduledGoapFusionGraphOutputWritable is the guard whose own doc comment
// states a scheduled run "could pass every current preflight yet still fail when
// that output directory is missing or not writable, leaving RunGraphifyUpdate
// unable to refresh the report so the cycle silently derives its gaps from a stale
// report" — so it must prove the graphify output directory is a writable directory
// before the loop runner drives a cycle whose refreshed report it can never
// persist. Yet the guard is registered and unit-tested (the sibling
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionGraphOutputWritable
// only asserts GetAction != nil) but never composed into GoapFusionPreflightNode()
// — every OTHER writable guard (plans, vault, syntheses) and content/tool guard is
// already wired ahead of the loop runner, so this one guard alone can never run in
// a scheduled cycle, exactly the "registered but never embedded" defect the whole
// preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// VerifyScheduledGoapFusionGraphOutputWritable as a registered Action node AND that
// it is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits
// the graphify-output-writable guard (RED) and passes once that guard is inserted
// ahead of the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is pinned
// here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesGraphOutputWritable(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionGraphOutputWritable"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q graphify-output-location guard as a runnable Action node; the preflight drives the loop runner without first proving the graphify report's output directory is writable, so a scheduled cycle would only discover at RunGraphifyUpdate that it cannot refresh the report and derive its gaps from a stale report", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the graphify-output-writable guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q graphify-output-location guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the output location RunGraphifyUpdate refreshes the report into is proven writable before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesCircuitBreaker
// pins the next increment the "goap-fusion-loop-runner" goal requires: the
// Phase-0 preflight node must compose the dedicated CIRCUITPOLICY *evaluation*
// action — EvaluateScheduledGoapFusionCircuitBreaker — as a runnable Action node
// ordered BEFORE the bounded loop runner it protects.
//
// EvaluateScheduledGoapFusionCircuitBreaker is the deterministic kernel-level
// circuit-breaker evaluation the P0 NotebookLM research goal names: given the
// loop's recent state-hash history (published on the blackboard under
// "goap_fusion_state_hashes"), it HALTs (-1) when a state hash repeats within the
// bounded goapFusionCircuitHistoryWindow — the "Activity-Progress Confusion"
// cycle — and CONTINUEs (1) otherwise. As a preflight Sequence child ahead of the
// loop runner it makes the circuit-breaker verdict an explicit, observable BT gate
// that fails the preflight fast on a detected cycle, so the scheduled cycle only
// gates on the loop runner after the dedicated breaker has passed. Every OTHER
// action in this CIRCUITPOLICY apparatus — the config guard
// (VerifyScheduledGoapFusionCircuitPolicy) and the loop runner
// (RunScheduledGoapFusionLoop) — is already composed into GoapFusionPreflightNode(),
// but EvaluateScheduledGoapFusionCircuitBreaker is registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHalts)
// yet wired into no composed tree, so it can never run in a scheduled cycle — the
// exact "registered but unwired" gap the whole preflight apparatus exists to close.
//
// This test asserts the preflight sequence references
// EvaluateScheduledGoapFusionCircuitBreaker as a registered Action node AND that it
// is ordered before RunScheduledGoapFusionLoop. It fails while the builder omits the
// circuit-breaker evaluation (RED) and passes once it is composed ahead of the loop
// runner (GREEN). The engine package cannot import internal/domains (import cycle),
// so this runnable-composition contract is pinned here at the action's own package,
// ready for the domains tree to embed as its Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesCircuitBreaker(t *testing.T) {
	const (
		guard      = "EvaluateScheduledGoapFusionCircuitBreaker"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q circuit-breaker evaluation as a runnable Action node; the preflight drives the loop runner without first making the deterministic CIRCUITPOLICY verdict an explicit, observable BT gate, so the dedicated breaker can never run in a scheduled cycle", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the circuit-breaker evaluation runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q circuit-breaker evaluation (index %d) to be composed BEFORE the %q loop runner (index %d), so the deterministic CIRCUITPOLICY verdict gates the scheduled cycle before the loop drives another iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHaltsOnConsecutiveNoopPatches
// pins the SECOND CIRCUITPOLICY halt condition the dedicated circuit-breaker
// evaluation *promises but never enforces*: halting on a run of consecutive no-op
// patch proposals. EvaluateScheduledGoapFusionCircuitBreaker's own doc comment
// states it enforces "detecting and halting state-transition cycles and repeated
// no-op patch proposals" (actions_superpowers.go), yet its implementation only
// delegates to goapFusionCircuitBreakerVerdict(hashes) — the bounded state-hash
// window dedup — and never consults goapFusionNoopPatchStreak. That leaves the
// "Activity-Progress Confusion" tail uncaught AT THE DEDICATED GATE: because the
// breaker is composed into GoapFusionPreflightNode() as an explicit, observable
// BT gate immediately BEFORE RunScheduledGoapFusionLoop, a scheduled cycle that
// publishes a run of DISTINCT state hashes while every proposed patch is a no-op
// would sail past the dedicated circuit-breaker gate (it returns CONTINUE) and
// only be caught one node later by the loop runner. The dedicated breaker — whose
// whole purpose is to make the CIRCUITPOLICY verdict an explicit gate — must
// enforce the SAME halt policy the loop runner does, from the single source of
// truth, or the two drift on what counts as a trip.
//
// The loop's consecutive-no-op-patch streak is published on the blackboard under
// "goap_fusion_noop_patch_streak"; a bounded run of no-op patches must HALT (-1)
// the dedicated breaker even when the state hashes are all distinct. This test
// asserts EvaluateScheduledGoapFusionCircuitBreaker HALTs on a run of consecutive
// no-op patches with distinct state hashes, and CONTINUEs when no no-op streak is
// present. It fails today because the breaker returns CONTINUE for distinct hashes
// regardless of the no-op streak (RED) and passes once the breaker halts on a
// bounded consecutive-no-op-patch run (GREEN) — the no-op-streak analogue of the
// repeated-state-hash halt the breaker already enforces, and the dedicated-gate
// counterpart of
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionLoopHaltsOnConsecutiveNoopPatches.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCircuitBreakerHaltsOnConsecutiveNoopPatches(t *testing.T) {
	action := GetAction("EvaluateScheduledGoapFusionCircuitBreaker")
	if action == nil {
		t.Fatalf("missing production Superpowers action %q", "EvaluateScheduledGoapFusionCircuitBreaker")
	}

	// Distinct state hashes so the bounded-window dedup never trips; a run of
	// consecutive no-op patch proposals is the only halt signal present. The
	// dedicated circuit breaker must still HALT — the loop is "active" proposing
	// empty patches that never advance the goal, the Activity-Progress Confusion
	// its own doc comment promises to catch.
	noop := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes":      []string{"aaa", "bbb", "ccc"},
			"goap_fusion_noop_patch_streak": 10,
		},
	}
	noopCtx := &btcore.BTContext[Blackboard]{Blackboard: noop}
	if status := action(noopCtx); status != -1 {
		t.Fatalf("expected HALT (-1) on a run of consecutive no-op patch proposals even with distinct state hashes, got %d", status)
	}
	if !strings.Contains(noop.Result, "HALT") {
		t.Fatalf("expected a HALT diagnosis in Result on a consecutive no-op patch run, got %q", noop.Result)
	}

	// No consecutive no-op patch run (and distinct hashes) lets the dedicated
	// circuit breaker CONTINUE (1) — the same verdict the loop runner reaches.
	progress := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_state_hashes":      []string{"aaa", "bbb", "ccc"},
			"goap_fusion_noop_patch_streak": 0,
		},
	}
	progressCtx := &btcore.BTContext[Blackboard]{Blackboard: progress}
	if status := action(progressCtx); status != 1 {
		t.Fatalf("expected CONTINUE (1) when no consecutive no-op patch run is present and state hashes are distinct, got %d", status)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightDrivesCycleAfterLoopRunner
// pins the increment the "goap-fusion-loop-runner" goal actually names: the
// Phase-0 preflight node must compose the research-to-implementation cycle driver
// — RunScheduledGoapFusionCycle — as a runnable Action node ordered AFTER the
// bounded loop runner that gates it.
//
// Every prior increment wired the guards, the circuit-breaker evaluation, and the
// bounded loop runner (RunScheduledGoapFusionLoop) into GoapFusionPreflightNode(),
// but the loop runner only DECIDES: it returns CONTINUE (1) or HALT (-1) over the
// loop's recent state-hash history and never drives the actual cycle. Nothing in
// the composed node runs RunScheduledGoapFusionCycle — the action that reads vault
// research and the graphify report, identifies improvement gaps, writes a
// Superpowers implementation plan, and implements it via the Superpowers runtime
// (actions_superpowers_prod.go). So RunScheduledGoapFusionCycle is registered and
// unit-tested (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionCycle)
// yet wired into no composed tree — the exact "registered but unwired" gap the
// preflight apparatus exists to close, now for the loop's own driver: a scheduled
// cycle that embeds the preflight would run every guard and consult the loop
// runner but never actually drive a research-to-implementation iteration.
//
// The cycle driver must run AFTER the loop runner so the CIRCUITPOLICY gate can
// short-circuit it: in the preflight Sequence, a HALT from
// RunScheduledGoapFusionLoop must stop the sequence before the cycle runs, so the
// driver only executes once the loop runner has decided CONTINUE and every guard
// ahead of it has passed.
//
// This test asserts the preflight sequence references RunScheduledGoapFusionCycle
// as a registered Action node AND that it is ordered after
// RunScheduledGoapFusionLoop. It fails while the builder ends at the loop runner
// and never composes the cycle driver (RED) and passes once the cycle driver is
// appended after the loop runner (GREEN). The engine package cannot import
// internal/domains (import cycle), so this runnable-composition contract is pinned
// here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightDrivesCycleAfterLoopRunner(t *testing.T) {
	const (
		loopRunner = "RunScheduledGoapFusionLoop"
		cycle      = "RunScheduledGoapFusionCycle"
	)

	node := GoapFusionPreflightNode()

	// Flatten the composed Action nodes in traversal order so we can assert both
	// presence and ordering (the cycle driver must run after the loop runner gate).
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

	loopIdx := indexOf(loopRunner)
	cycleIdx := indexOf(cycle)

	if cycleIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q research-to-implementation cycle driver as a runnable Action node; the preflight gates the scheduled cycle on the loop runner but never actually drives a research-to-implementation iteration, so a scheduled run would decide CONTINUE and then do nothing", cycle)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the cycle driver runs after it", loopRunner)
	}
	if cycleIdx <= loopIdx {
		t.Fatalf("expected the %q cycle driver (index %d) to be composed AFTER the %q loop runner (index %d), so a HALT from the loop runner short-circuits the sequence before the cycle drives another research-to-implementation iteration", cycle, cycleIdx, loopRunner, loopIdx)
	}

	if GetAction(cycle) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", cycle)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresLoopTree pins
// the one integration seam the "goap-fusion-loop-runner" goal still lacks: a
// single tree-level entry point that returns a fully-wired copy of the production
// GOAP fusion loop tree, so the domains GoapFusionLoopTree() can adopt BOTH the
// Phase-0 preflight AND the Claude-implementation circuit gate in one call.
//
// The apparatus is otherwise complete: GoapFusionPreflightNode() composes every
// guard, the bounded loop runner, and the cycle driver; PrependGoapFusionPreflight
// prepends that preflight as a loop's first child; and PrependGoapFusionImplementationGate
// gates a Claude implementation child list with the CIRCUITPOLICY breaker + loop
// runner. But those are TWO list-level primitives the caller must apply separately,
// each to a different, hand-isolated child list — exactly the manual, error-prone
// wiring the recorded "registered but unwired" gap ([[goap-fusion-preflight-unwired]])
// keeps re-opening: every prior run grew the primitives without ever taking the one
// step that makes a whole loop tree adopt them atomically. Nothing composes the two
// into a single, schema-valid wired tree the domains package can embed without
// duplicating the composition or navigating the nested implementation subtree by
// hand.
//
// WireGoapFusionLoopTree closes that gap: given the production loop tree it (1)
// prepends GoapFusionPreflightNode() as the tree's first child (via
// PrependGoapFusionPreflight) and (2) rewrites the "ClaudeSuperpowersPath"
// implementation subtree's children via PrependGoapFusionImplementationGate so a
// detected Activity-Progress Confusion cycle HALTs the path before
// RunSuperpowersClaudeImplementation shells out to Claude Code. It returns a fresh
// tree and never mutates the caller's input.
//
// The engine package cannot import internal/domains (import cycle), but
// domains -> engine is the safe direction, so this whole-tree wiring seam belongs
// here at the guards' own package, ready for GoapFusionLoopTree() to adopt in one
// call. It fails to compile until WireGoapFusionLoopTree is introduced (RED) and
// passes once the seam returns the fully-wired tree (GREEN).
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresLoopTree(t *testing.T) {
	const (
		preflight      = "GoapFusionPreflight"
		claudePath     = "ClaudeSuperpowersPath"
		circuitBreaker = "EvaluateScheduledGoapFusionCircuitBreaker"
		loopRunner     = "RunScheduledGoapFusionLoop"
		planWriter     = "WriteSuperpowersImplementationPlan"
		impl           = "RunSuperpowersClaudeImplementation"
		setup          = "SetupFusionTools"
	)

	// A minimal mirror of the production GoapFusionLoop_Main tree: a setup action,
	// then an ExecutionRouter Selector whose ClaudeSuperpowersPath sequence writes
	// the plan and runs the Claude implementation behind a HumanApprovalGate.
	tree := evolution.SerializableNode{
		Type: "Sequence",
		Name: "GoapFusionLoop_Main",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: setup},
			{
				Type: "Selector",
				Name: "ExecutionRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence",
						Name: claudePath,
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasNewGaps"},
							{Type: "Action", Name: planWriter},
							{
								Type: "HumanApprovalGate",
								Name: "ApproveGoapFusionApply",
								Children: []evolution.SerializableNode{
									{Type: "Action", Name: impl},
									{Type: "Action", Name: "VerifyGoapBuild"},
								},
							},
						},
					},
				},
			},
		},
	}

	wired := WireGoapFusionLoopTree(tree)

	// (1) The Phase-0 preflight must be the tree's FIRST child, ahead of setup, so a
	// scheduled cycle materializes a fresh tree and consults the bounded loop runner
	// before it runs anything else.
	if len(wired.Children) != len(tree.Children)+1 {
		t.Fatalf("expected the Phase-0 preflight prepended (len %d), got %d: %+v", len(tree.Children)+1, len(wired.Children), wired.Children)
	}
	if wired.Children[0].Name != preflight {
		t.Fatalf("expected the first wired child to be the %q Phase-0 preflight, got %q", preflight, wired.Children[0].Name)
	}
	if wired.Children[1].Name != setup {
		t.Fatalf("expected the original %q setup child preserved immediately after the preflight, got %q", setup, wired.Children[1].Name)
	}

	// Locate the ClaudeSuperpowersPath subtree in the wired tree and flatten its
	// Action nodes in traversal order.
	var findPath func(n evolution.SerializableNode) (evolution.SerializableNode, bool)
	findPath = func(n evolution.SerializableNode) (evolution.SerializableNode, bool) {
		if n.Name == claudePath {
			return n, true
		}
		for _, c := range n.Children {
			if found, ok := findPath(c); ok {
				return found, true
			}
		}
		return evolution.SerializableNode{}, false
	}
	path, ok := findPath(wired)
	if !ok {
		t.Fatalf("WireGoapFusionLoopTree dropped the %q implementation subtree; the wiring must preserve the path it gates", claudePath)
	}

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
	collect(path)

	indexOf := func(name string) int {
		for i, n := range order {
			if n == name {
				return i
			}
		}
		return -1
	}

	cbIdx := indexOf(circuitBreaker)
	loopIdx := indexOf(loopRunner)
	planIdx := indexOf(planWriter)
	implIdx := indexOf(impl)

	// (2) The CIRCUITPOLICY gate must be prepended to the ClaudeSuperpowersPath
	// implementation children, so the breaker + loop runner run before the plan
	// writer and before Claude Code implements.
	if cbIdx < 0 || loopIdx < 0 {
		t.Fatalf("WireGoapFusionLoopTree did not gate the %q implementation path with the %q breaker and %q loop runner; a non-progressing loop could still shell out to Claude Code (order=%v)", claudePath, circuitBreaker, loopRunner, order)
	}
	if planIdx < 0 || implIdx < 0 {
		t.Fatalf("WireGoapFusionLoopTree dropped the implementation nodes it must preserve (plan=%d impl=%d order=%v)", planIdx, implIdx, order)
	}
	if cbIdx >= loopIdx {
		t.Fatalf("expected the %q breaker (index %d) to run BEFORE the %q loop runner (index %d), matching the gate ordering", circuitBreaker, cbIdx, loopRunner, loopIdx)
	}
	if loopIdx >= planIdx || loopIdx >= implIdx {
		t.Fatalf("expected the CIRCUITPOLICY gate (breaker@%d, loop@%d) to run BEFORE the plan writer (index %d) and the Claude implementation (index %d), so a detected Activity-Progress Confusion cycle HALTs the path before Claude Code implements", cbIdx, loopIdx, planIdx, implIdx)
	}

	// (3) The wired tree must be schema-valid end-to-end so the production tree that
	// embeds it survives BuildAndValidate.
	if errs := wired.Validate(); len(errs) > 0 {
		t.Fatalf("WireGoapFusionLoopTree produced a tree that is not schema-valid; every composed node type must be known so the production tree survives BuildAndValidate, but validation reported %d error(s):\n- %s", len(errs), strings.Join(errs, "\n- "))
	}

	// (4) The seam must not mutate the caller's input tree: the original first child
	// is still the setup action and the original ClaudeSuperpowersPath still starts
	// with its HasNewGaps condition (no gate prepended in place).
	if tree.Children[0].Name != setup {
		t.Fatalf("WireGoapFusionLoopTree mutated the caller's tree: first child is now %q, want %q", tree.Children[0].Name, setup)
	}
	origPath, ok := findPath(tree)
	if !ok || len(origPath.Children) == 0 || origPath.Children[0].Name != "HasNewGaps" {
		t.Fatalf("WireGoapFusionLoopTree mutated the caller's %q subtree: %+v", claudePath, origPath)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresLoopTreeIsIdempotent
// pins the safety property the "goap-fusion-loop-runner" goal needs before the
// domains GoapFusionLoopTree() adopts the whole-tree seam: WireGoapFusionLoopTree
// must be IDEMPOTENT — wiring an already-wired tree must return a tree identical to
// wiring it once.
//
// The seam is the single call the domains package is meant to make, but nothing
// stops it from being applied to a tree that already carries the Phase-0 preflight
// and the ClaudeSuperpowersPath CIRCUITPOLICY gate. Today WireGoapFusionLoopTree
// unconditionally PrependGoapFusionPreflight's a fresh GoapFusionPreflight as the
// tree's first child and unconditionally PrependGoapFusionImplementationGate's a
// fresh breaker+loop-runner pair onto ClaudeSuperpowersPath's children, so a second
// application prepends a SECOND preflight ahead of the first and a SECOND gate pair
// ahead of the first — the loop runner and circuit breaker would then run twice per
// cycle and the preflight sequence would execute its whole guard chain twice. A
// re-invocation, a retry, or a tree that was hand-wired once and then routed through
// the seam would silently double-gate, exactly the kind of drift the recorded
// "registered but unwired" pitfall ([[goap-fusion-preflight-unwired]]) keeps
// re-opening as the apparatus grows.
//
// This test asserts that wiring twice deep-equals wiring once, and — for a clear
// diagnosis — that the twice-wired tree still has exactly one GoapFusionPreflight
// top-level child. It fails today because the seam double-prepends both the
// preflight and the implementation gate (RED) and passes once WireGoapFusionLoopTree
// skips wiring a tree that is already wired (GREEN). The engine package cannot import
// internal/domains (import cycle), so this idempotency contract is pinned here at the
// seam's own package, ready for GoapFusionLoopTree() to adopt in one safe call.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresLoopTreeIsIdempotent(t *testing.T) {
	const (
		preflight  = "GoapFusionPreflight"
		claudePath = "ClaudeSuperpowersPath"
		setup      = "SetupFusionTools"
		planWriter = "WriteSuperpowersImplementationPlan"
		impl       = "RunSuperpowersClaudeImplementation"
	)

	// The same minimal mirror of the production GoapFusionLoop_Main tree used by
	// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresLoopTree.
	tree := evolution.SerializableNode{
		Type: "Sequence",
		Name: "GoapFusionLoop_Main",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: setup},
			{
				Type: "Selector",
				Name: "ExecutionRouter",
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence",
						Name: claudePath,
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasNewGaps"},
							{Type: "Action", Name: planWriter},
							{
								Type: "HumanApprovalGate",
								Name: "ApproveGoapFusionApply",
								Children: []evolution.SerializableNode{
									{Type: "Action", Name: impl},
									{Type: "Action", Name: "VerifyGoapBuild"},
								},
							},
						},
					},
				},
			},
		},
	}

	once := WireGoapFusionLoopTree(tree)
	twice := WireGoapFusionLoopTree(once)

	// A clear, specific diagnosis first: wiring an already-wired tree must not
	// prepend a second Phase-0 preflight.
	preflightCount := 0
	for _, c := range twice.Children {
		if c.Name == preflight {
			preflightCount++
		}
	}
	if preflightCount != 1 {
		t.Fatalf("WireGoapFusionLoopTree is not idempotent: wiring an already-wired tree left %d %q top-level children, want exactly 1; a re-invocation double-prepends the Phase-0 preflight so its whole guard chain and the bounded loop runner would run twice per cycle", preflightCount, preflight)
	}

	// The full contract: wiring twice must be identical to wiring once, so the seam
	// never double-gates the ClaudeSuperpowersPath implementation subtree either.
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("WireGoapFusionLoopTree is not idempotent: wiring an already-wired tree differs from wiring once, so the seam double-prepends the preflight and/or the ClaudeSuperpowersPath CIRCUITPOLICY gate.\nonce:  %+v\ntwice: %+v", once, twice)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresStateHashProducer
// pins the last unwired link of the "goap-fusion-loop-runner" goal: the whole-tree
// seam must embed the PublishGoapFusionStateHash PRODUCER into the loop tree so the
// CIRCUITPOLICY apparatus it feeds actually has a producer in a real scheduled cycle.
//
// PublishGoapFusionStateHash is registered and unit-tested
// (TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPublishesStateHash),
// and RunScheduledGoapFusionLoop + EvaluateScheduledGoapFusionCircuitBreaker derive
// their entire halt/continue decision from bb.ChainState["goap_fusion_state_hashes"].
// But WireGoapFusionLoopTree today only (1) prepends the Phase-0 preflight and (2)
// gates ClaudeSuperpowersPath — it never embeds the producer that WRITES the history
// those consumers read. So in the production wired tree the state-hash history stays
// permanently empty every cycle, the bounded window never sees a repeat, the loop
// runner always returns CONTINUE, and the "Activity-Progress Confusion" cycle the
// whole loop-runner apparatus exists to detect can never fire ([[goap-fusion-state-hash-no-producer]]).
//
// The cycle's progress-relevant state is the prioritized goal queue that
// PrioritizeGoapGoals publishes (Phase 4). So the seam must insert a
// PublishGoapFusionStateHash Action immediately AFTER PrioritizeGoapGoals in its
// parent's child list — after the goal queue exists, before the ExecutionRouter that
// consumes it — so every cycle hashes the freshly prioritized goals and appends them
// to the history the circuit breaker/loop runner then HALT on when a cycle repeats.
//
// This test asserts WireGoapFusionLoopTree embeds PublishGoapFusionStateHash as the
// immediate next sibling of PrioritizeGoapGoals, produces a schema-valid tree, and
// does not mutate the caller's input. It fails today because the seam never embeds
// the producer (RED) and passes once WireGoapFusionLoopTree inserts it right after
// PrioritizeGoapGoals (GREEN). The engine package cannot import internal/domains
// (import cycle), so this producer-wiring contract is pinned here at the seam's own
// package, ready for the domains GoapFusionLoopTree() to adopt in one call.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionWiresStateHashProducer(t *testing.T) {
	const (
		prioritize = "PrioritizeGoapGoals"
		producer   = "PublishGoapFusionStateHash"
		analyze    = "AnalyzeImprovementGaps"
		router     = "ExecutionRouter"
		claudePath = "ClaudeSuperpowersPath"
		setup      = "SetupFusionTools"
		planWriter = "WriteSuperpowersImplementationPlan"
		impl       = "RunSuperpowersClaudeImplementation"
	)

	// A minimal mirror of the production GoapFusionLoop_Main tree: Phase-0 setup, the
	// Phase-4 AnalyzeImprovementGaps -> PrioritizeGoapGoals pair that builds the goal
	// queue, then the ExecutionRouter Selector whose ClaudeSuperpowersPath consumes it.
	tree := evolution.SerializableNode{
		Type: "Sequence",
		Name: "GoapFusionLoop_Main",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: setup},
			{Type: "Action", Name: analyze},
			{Type: "Action", Name: prioritize},
			{
				Type: "Selector",
				Name: router,
				Children: []evolution.SerializableNode{
					{
						Type: "Sequence",
						Name: claudePath,
						Children: []evolution.SerializableNode{
							{Type: "Condition", Name: "HasNewGaps"},
							{Type: "Action", Name: planWriter},
							{Type: "Action", Name: impl},
						},
					},
				},
			},
		},
	}

	wired := WireGoapFusionLoopTree(tree)

	// Locate the parent whose child list contains PrioritizeGoapGoals, and the index
	// of the goal-queue producer within it.
	var parent *evolution.SerializableNode
	prioritizeIdx := -1
	var find func(n *evolution.SerializableNode)
	find = func(n *evolution.SerializableNode) {
		for i := range n.Children {
			if n.Children[i].Name == prioritize {
				parent = n
				prioritizeIdx = i
			}
			find(&n.Children[i])
		}
	}
	find(&wired)

	if parent == nil || prioritizeIdx < 0 {
		t.Fatalf("WireGoapFusionLoopTree dropped the %q node; the seam must preserve the Phase-4 goal-queue producer it publishes state hashes from", prioritize)
	}

	// The producer must be the IMMEDIATE next sibling of PrioritizeGoapGoals: after the
	// goal queue exists, before the ExecutionRouter that consumes it, so every cycle
	// hashes the freshly prioritized goals into the CIRCUITPOLICY history.
	if prioritizeIdx+1 >= len(parent.Children) {
		t.Fatalf("WireGoapFusionLoopTree did not embed the %q producer after %q; the CIRCUITPOLICY history the loop runner reads stays permanently empty in a real scheduled cycle, so the Activity-Progress Confusion cycle can never be detected (siblings=%d)", producer, prioritize, len(parent.Children))
	}
	if next := parent.Children[prioritizeIdx+1]; next.Name != producer {
		t.Fatalf("expected %q embedded as the immediate next sibling of %q (index %d), got %q; the producer must run right after the goal queue is prioritized and before the ExecutionRouter consumes it", producer, prioritize, prioritizeIdx+1, next.Name)
	}

	// The wired tree must be schema-valid end-to-end so the production tree that
	// embeds it survives BuildAndValidate.
	if errs := wired.Validate(); len(errs) > 0 {
		t.Fatalf("WireGoapFusionLoopTree produced a tree that is not schema-valid after embedding %q; validation reported %d error(s):\n- %s", producer, len(errs), strings.Join(errs, "\n- "))
	}

	// The seam must not mutate the caller's input: the original Phase-4 pair is still
	// AnalyzeImprovementGaps -> PrioritizeGoapGoals with no producer spliced in place.
	if got := tree.Children[2].Name; got != prioritize {
		t.Fatalf("WireGoapFusionLoopTree mutated the caller's tree: child index 2 is now %q, want %q (no producer may be spliced into the caller's input)", got, prioritize)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSandbox pins the
// engine-side completion of the P0 NotebookLM research goal: structural tree
// evaluation must never spawn subprocesses / hit the network / burn external
// quotas. The scheduled GOAP fusion cycle validates candidate mutations by
// ticking trees through the benchmark harnesses, and commit 2bea250 set
// `Sandbox: true` on the RunSuite Blackboard so those structural ticks are
// short-circuited before any real `nlm`/`git`/`claude` action can dispatch
// (tree.go actionForName: when bb.Sandbox is true it returns a `[sandbox] name`
// stub instead of the real registered implementation). That sandbox short-circuit
// is the single engine-side mechanism the whole "don't burn quotas during
// structural evaluation" defense rests on — yet nothing in the scheduled cycle's
// Phase-0 preflight proves the mechanism is intact before the cycle runs its
// benchmark-based structural validation. If a refactor ever dropped the
// `bb.Sandbox` guard in actionForName, every benchmark harness would silently
// dispatch real side-effecting actions again — the exact 11-hour/quota-burning
// defect commit 2bea250 set out to eliminate, now undetected.
//
// This action closes that gap the same way its sibling guards do: it is a
// deterministic kernel-level preflight that proves, before the automatic
// research-to-implementation cycle proceeds, that a sandboxed Blackboard blocks a
// real registered action from executing — so structural evaluation during the
// scheduled cycle can never spawn `nlm`/`git`/`claude` subprocesses. It returns
// PASS (1) when the sandbox invariant holds and FAIL (-1) with a clear diagnosis
// when a real action would dispatch under sandbox — the sandbox-invariant analogue
// of the input, runtime, and tool guards.
//
// It fails today because VerifyScheduledGoapFusionSandbox is not registered (RED)
// and passes once the guard is implemented and registered (GREEN).
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSandbox(t *testing.T) {
	action := GetAction("VerifyScheduledGoapFusionSandbox")
	if action == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionSandbox")
	}

	// In this build the sandbox short-circuit in actionForName is intact, so a
	// sandboxed Blackboard blocks real action dispatch and the guard must PASS (1)
	// with a diagnosis naming the sandbox invariant it verified.
	bb := &Blackboard{Task: "verify scheduled goap fusion sandbox"}
	if status := action(btcore.NewBTContext(context.Background(), bb)); status != 1 {
		t.Fatalf("expected PASS (1) while the sandbox invariant holds (a sandboxed Blackboard blocks real action dispatch), got %d: %s", status, bb.Result)
	}
	if !strings.Contains(bb.Result, "Sandbox") {
		t.Fatalf("expected a sandbox-invariant diagnosis in Result, got %q", bb.Result)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesSandbox
// pins the next increment the "goap-fusion-loop-runner" goal requires: the Phase-0
// preflight node must compose the sandbox-invariant guard —
// VerifyScheduledGoapFusionSandbox — as a runnable Action node ordered BEFORE the
// bounded loop runner it protects.
//
// VerifyScheduledGoapFusionSandbox is the deterministic kernel-level preflight that
// proves the single engine-side mechanism the whole "don't burn quotas during
// structural evaluation" defense rests on (the bb.Sandbox short-circuit in
// actionForName) is intact before the unattended scheduled cycle runs its
// benchmark-based structural validation. Its own doc comment states it closes that
// gap "the same way its sibling input/runtime/tool guards do" — but those siblings
// ARE children of the preflight Sequence while this one is not, so it provides zero
// runtime protection: the guard is registered and unit-tested (the sibling
// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionSandbox only asserts
// GetAction != nil and the PASS behavior) yet composed into no tree, exactly the
// recurring "registered but never embedded" defect the whole preflight apparatus
// exists to close.
//
// This test asserts the preflight sequence references VerifyScheduledGoapFusionSandbox
// as a registered Action node AND that it is ordered before RunScheduledGoapFusionLoop.
// It fails while the builder omits the sandbox-invariant guard (RED) and passes once
// that guard is inserted ahead of the loop runner (GREEN). The engine package cannot
// import internal/domains (import cycle), so this runnable-composition contract is
// pinned here at the action's own package, ready for the domains tree to embed as its
// Phase-0 preflight.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPreflightComposesSandbox(t *testing.T) {
	const (
		guard      = "VerifyScheduledGoapFusionSandbox"
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
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q sandbox-invariant guard as a runnable Action node; the preflight drives the loop runner without first proving the bb.Sandbox short-circuit still blocks real action dispatch, so a scheduled cycle's benchmark-based structural validation could silently spawn real nlm/git/claude subprocesses again", guard)
	}
	if loopIdx < 0 {
		t.Fatalf("GoapFusionPreflightNode() does not compose the %q loop runner; cannot assert the sandbox-invariant guard runs before it", loopRunner)
	}
	if guardIdx >= loopIdx {
		t.Fatalf("expected the %q sandbox-invariant guard (index %d) to be composed BEFORE the %q loop runner (index %d), so the sandbox short-circuit is proven intact before the loop drives another benchmark-validating iteration", guard, guardIdx, loopRunner, loopIdx)
	}

	if GetAction(guard) == nil {
		t.Fatalf("preflight composes Action %q but it is not a registered, runnable action", guard)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPublishesStateHash
// pins the missing PRODUCER the "goap-fusion-loop-runner" goal actually depends on:
// nothing on the blackboard ever publishes the state-hash history the whole
// CIRCUITPOLICY apparatus consumes. RunScheduledGoapFusionLoop and
// EvaluateScheduledGoapFusionCircuitBreaker both derive their entire halt/continue
// decision from bb.ChainState["goap_fusion_state_hashes"] (goapFusionStateHashes),
// and every prior increment built, wired, and unit-tested those consumers — yet
// grep proves that key is only ever READ in production and WRITTEN only by test
// fixtures. No registered action computes a state hash of the cycle's
// progress-relevant state and appends it to the history, so in a real scheduled
// cycle the history stays permanently empty: the circuit breaker's bounded window
// never sees a repeat, the loop runner always returns CONTINUE, and the
// "Activity-Progress Confusion" cycle the entire loop-runner apparatus exists to
// break can never actually be detected [Source 207, 214, 215, 250].
//
// The progress-relevant state is the cycle's prioritized goal queue —
// PrioritizeGoapGoals stores it under bb.ChainState["goap_fusion_goal_queue"], and
// HasNewGaps already treats an unchanged goal queue as "no progress." So the
// producer must hash that goal queue deterministically (identical goal queues →
// identical hash) and append it to goap_fusion_state_hashes, preserving prior
// history, so two consecutive cycles that produce the same goals append the same
// hash and the circuit breaker/loop runner it feeds finally HALT on the repeat.
//
// This test asserts PublishGoapFusionStateHash (1) is a registered, runnable action,
// (2) appends a non-empty, deterministic hash of the goal queue while preserving any
// prior history, (3) hashes distinct goal queues to distinct values, and (4) feeds
// the consumer: a run that publishes the same goal queue on two consecutive ticks
// produces a repeated hash within the bounded window that RunScheduledGoapFusionLoop
// then HALTs on. It fails today because the producer is not registered (RED) and
// passes once PublishGoapFusionStateHash is implemented and appends the deterministic
// goal-queue hash (GREEN). The engine package cannot import internal/domains (import
// cycle), so this producer contract is pinned here at the action's own package,
// ready for the domains tree to embed after the cycle prioritizes its goals.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionPublishesStateHash(t *testing.T) {
	const producer = "PublishGoapFusionStateHash"

	action := GetAction(producer)
	if action == nil {
		t.Fatalf("missing production Superpowers action %q; the loop runner and circuit breaker consume goap_fusion_state_hashes but no registered action ever publishes it, so the Activity-Progress Confusion cycle can never be detected in a real scheduled cycle", producer)
	}

	// (2) Appends a non-empty, deterministic hash of the goal queue while preserving
	// any prior state-hash history published on earlier ticks.
	first := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_goal_queue":   "[P0] fix the loop runner\n[P2] add smoke tests",
			"goap_fusion_state_hashes": []string{"prior-tick-hash"},
		},
	}
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: first}); status != 1 {
		t.Fatalf("expected PublishGoapFusionStateHash to return SUCCESS (1), got %d: %s", status, first.Result)
	}
	hist := goapFusionStateHashes(first)
	if len(hist) != 2 {
		t.Fatalf("expected the producer to append exactly one hash to the existing history (len 2), got %d: %v", len(hist), hist)
	}
	if hist[0] != "prior-tick-hash" {
		t.Fatalf("expected the prior state-hash history preserved as the first entry, got %q (full: %v)", hist[0], hist)
	}
	hashA := hist[1]
	if strings.TrimSpace(hashA) == "" {
		t.Fatalf("expected a non-empty state hash appended for a non-empty goal queue, got %q", hashA)
	}

	// (3a) Determinism: an identical goal queue on a fresh blackboard hashes to the
	// SAME value — the property the repeated-state circuit breaker relies on to
	// recognize the loop returned to a prior goal state without advancing.
	same := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_goal_queue": "[P0] fix the loop runner\n[P2] add smoke tests",
		},
	}
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: same}); status != 1 {
		t.Fatalf("expected SUCCESS (1) on the second publish, got %d: %s", status, same.Result)
	}
	sameHist := goapFusionStateHashes(same)
	if len(sameHist) != 1 || sameHist[0] != hashA {
		t.Fatalf("expected an identical goal queue to hash deterministically to %q, got %v", hashA, sameHist)
	}

	// (3b) A DIFFERENT goal queue must hash to a DIFFERENT value, so genuine goal
	// progress advances the state and does not read as a repeated-state cycle.
	other := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_goal_queue": "[P0] a completely different goal",
		},
	}
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: other}); status != 1 {
		t.Fatalf("expected SUCCESS (1) on the third publish, got %d: %s", status, other.Result)
	}
	otherHist := goapFusionStateHashes(other)
	if len(otherHist) != 1 {
		t.Fatalf("expected exactly one hash appended for the different goal queue, got %v", otherHist)
	}
	if otherHist[0] == hashA {
		t.Fatalf("expected a distinct goal queue to hash to a distinct value, but it collided with %q", hashA)
	}

	// (4) The producer feeds the consumer: publishing the same goal queue on two
	// consecutive ticks appends the same hash twice, and the loop runner it feeds must
	// HALT on that repeated state hash within the bounded window — the end-to-end
	// closure the whole loop-runner apparatus was built for.
	loop := &Blackboard{
		ChainState: map[string]any{
			"goap_fusion_goal_queue": "[P0] the same unchanged goal queue",
		},
	}
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: loop}); status != 1 {
		t.Fatalf("expected SUCCESS (1) on the first loop-feed publish, got %d: %s", status, loop.Result)
	}
	// The next tick re-derives the same goal queue and publishes again onto the
	// accumulated history.
	loop.ChainState["goap_fusion_goal_queue"] = "[P0] the same unchanged goal queue"
	if status := action(&btcore.BTContext[Blackboard]{Blackboard: loop}); status != 1 {
		t.Fatalf("expected SUCCESS (1) on the second loop-feed publish, got %d: %s", status, loop.Result)
	}
	if fed := goapFusionStateHashes(loop); len(fed) != 2 || fed[0] != fed[1] {
		t.Fatalf("expected two identical hashes accumulated across ticks for an unchanged goal queue, got %v", fed)
	}

	loopRunner := GetAction("RunScheduledGoapFusionLoop")
	if loopRunner == nil {
		t.Fatalf("missing production Superpowers action %q", "RunScheduledGoapFusionLoop")
	}
	if status := loopRunner(&btcore.BTContext[Blackboard]{Blackboard: loop}); status != -1 {
		t.Fatalf("expected the loop runner to HALT (-1) on the repeated state hash the producer published, got %d: %s", status, loop.Result)
	}
	if !strings.Contains(loop.Result, "HALT") {
		t.Fatalf("expected a HALT diagnosis once the producer feeds a repeated state hash, got %q", loop.Result)
	}
}

// TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionRunsWritable asserts
// the presence of the preflight action that guards the Superpowers run artifact
// directory the unattended scheduled GOAP fusion cycle writes its entire
// implement-verify-report output into. The existing writable-location guards each
// cover a distinct directory the cycle uses: VerifyScheduledGoapFusionPlansWritable
// (goapFusionPlansDir), VerifyScheduledGoapFusionVaultWritable (goapFusionVaultDir),
// VerifyScheduledGoapFusionSynthesesWritable (goapFusionSynthesesDir), and
// VerifyScheduledGoapFusionGraphOutputWritable (the graphify report's output
// directory) — but none covers the Superpowers runs directory
// (superpowersRunsDir). Every scheduled cycle's run is rooted at
// filepath.Join(superpowersRunsDir, id) (superpowers_artifacts.go): the cycle's
// "write Superpowers implementation plan" step writes plan.md there, its
// verification step writes baseline-build.txt / worktree.patch / per-check outputs
// under the run's verification/ subdirectory, and its report step writes finish.md
// and run.json — the exact "write plan ... verify ... report" outputs this
// objective names. A scheduled run could pass every current preflight (inputs,
// research corpus, runtime, toolchain, git, plans/vault/syntheses/graph-output
// writability) yet still fail the moment it initializes its run when
// superpowersRunsDir is missing or not writable, losing its plan, verification
// evidence, and finish report with no early diagnosis. This action closes that gap
// by requiring the Superpowers runs directory to be a writable directory before the
// automatic research-to-implementation cycle proceeds — the run-artifact-output
// analogue of the plans-, vault-, syntheses-, and graph-output-writable guards.
func TestSuperpowersRuntime_ActionsRegistered_ScheduledGoapFusionRunsWritable(t *testing.T) {
	if GetAction("VerifyScheduledGoapFusionRunsWritable") == nil {
		t.Fatalf("missing production Superpowers action %q", "VerifyScheduledGoapFusionRunsWritable")
	}
}
