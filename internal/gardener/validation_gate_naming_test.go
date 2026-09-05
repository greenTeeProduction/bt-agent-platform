package gardener

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// The gardener registry names domain trees "domain_<x>" while the agent
// process records SLO evidence under the engine name "domain:<x>". The gate
// must bridge the two schemes or no domain tree can ever match its evidence.
func TestValidationGate_RegistryNameMatchesEngineEvidence(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "goap-fusion-runner", TreeName: "domain:goap_fusion", TotalCalls: 20, SuccessfulCalls: 18, FailedCalls: 2, RecoveredCalls: 1},
	})

	if err := ValidationGate("domain_goap_fusion", "domain_goap_fusion", cfg); err != nil {
		t.Errorf("expected registry name domain_goap_fusion to match evidence tree domain:goap_fusion, got: %v", err)
	}
}

// Unhealthy engine-named evidence must still reject the mapped registry name —
// the mapping must not weaken threshold enforcement.
func TestValidationGate_RegistryNameMapping_StillEnforcesThresholds(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "goap-fusion-runner", TreeName: "domain:goap_fusion", TotalCalls: 20, SuccessfulCalls: 5, FailedCalls: 15},
	})

	if err := ValidationGate("domain_goap_fusion", "domain_goap_fusion", cfg); err == nil {
		t.Error("expected mapped evidence with 25% success rate to reject")
	}
}

// AllowUnverified: trees with no evidence anywhere may persist (the gardener's
// output is not consumed by live agents at runtime), while trees WITH evidence
// keep full threshold enforcement. Default remains fail-closed.
func TestValidationGate_AllowUnverified_PassesWithoutEvidence(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.AllowUnverified = true
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{})

	if err := ValidationGate("never_executed_tree", "never_executed_tree", cfg); err != nil {
		t.Errorf("expected AllowUnverified to pass a tree with no evidence, got: %v", err)
	}
}

func TestValidationGate_AllowUnverified_StillRejectsBadEvidence(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.AllowUnverified = true
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "agent-x", TreeName: "unhealthy_tree", TotalCalls: 20, SuccessfulCalls: 5, FailedCalls: 15},
	})

	if err := ValidationGate("unhealthy_tree", "unhealthy_tree", cfg); err == nil {
		t.Error("AllowUnverified must not bypass thresholds when evidence exists")
	}
}
