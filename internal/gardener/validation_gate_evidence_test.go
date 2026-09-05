package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

func writeEvidenceFile(t *testing.T, snapshots []engine.SLOSnapshot) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "slo-metrics.json")
	data, err := json.Marshal(snapshots)
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	return path
}

// B1: the gardener process never executes trees, so in-memory SLO metrics are
// always empty there. File-based evidence written by the agent process must
// satisfy the gate.
func TestValidationGate_FileEvidence_Passes(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "agent-x", TreeName: "evidence_pass_tree", TotalCalls: 20, SuccessfulCalls: 18, FailedCalls: 2, RecoveredCalls: 1},
	})

	if err := ValidationGate("agent-x", "evidence_pass_tree", cfg); err != nil {
		t.Errorf("expected gate to pass with healthy file evidence, got: %v", err)
	}
}

func TestValidationGate_FileEvidence_LowSuccessRate_Rejects(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "agent-x", TreeName: "evidence_low_success", TotalCalls: 20, SuccessfulCalls: 10, FailedCalls: 10, RecoveredCalls: 9},
	})

	if err := ValidationGate("agent-x", "evidence_low_success", cfg); err == nil {
		t.Error("expected gate to reject evidence with 50% success rate")
	}
}

// Evidence for the same tree from multiple agents is aggregated: the gate
// matches on tree name, not on which agent produced the evidence.
func TestValidationGate_FileEvidence_AggregatesAcrossAgents(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "agent-a", TreeName: "evidence_shared_tree", TotalCalls: 5, SuccessfulCalls: 5},
		{AgentName: "agent-b", TreeName: "evidence_shared_tree", TotalCalls: 5, SuccessfulCalls: 4, FailedCalls: 1, RecoveredCalls: 1},
		{AgentName: "agent-c", TreeName: "unrelated_tree", TotalCalls: 100, SuccessfulCalls: 0, FailedCalls: 100},
	})

	// Aggregate for evidence_shared_tree: 10 calls, 9 success (0.9), recovery 1/1.
	// The unrelated tree's catastrophic stats must not bleed in.
	if err := ValidationGate("any-agent", "evidence_shared_tree", cfg); err != nil {
		t.Errorf("expected aggregated evidence to pass, got: %v", err)
	}
}

func TestValidationGate_NoMemoryNoFile_FailsClosed(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = filepath.Join(t.TempDir(), "missing.json")

	if err := ValidationGate("agent-x", "evidence_absent_tree", cfg); err == nil {
		t.Error("expected fail-closed rejection when neither memory nor file evidence exists")
	}
}

func TestValidationGate_EvidenceForOtherTreeOnly_FailsClosed(t *testing.T) {
	cfg := DefaultValidationGateConfig()
	cfg.EvidencePath = writeEvidenceFile(t, []engine.SLOSnapshot{
		{AgentName: "agent-x", TreeName: "some_other_tree", TotalCalls: 50, SuccessfulCalls: 50},
	})

	if err := ValidationGate("agent-x", "evidence_wrong_tree", cfg); err == nil {
		t.Error("expected fail-closed rejection when file has no evidence for this tree")
	}
}
