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
