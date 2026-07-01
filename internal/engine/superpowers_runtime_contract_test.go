package engine

import "testing"

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
// ApplyImprovementWithClaude step synchronizes against origin before letting
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
// ApplyImprovementWithClaude step shells out to `git` dozens of times via
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
