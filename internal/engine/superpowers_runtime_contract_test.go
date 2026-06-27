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
