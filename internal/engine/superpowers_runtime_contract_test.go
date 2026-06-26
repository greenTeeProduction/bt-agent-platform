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
