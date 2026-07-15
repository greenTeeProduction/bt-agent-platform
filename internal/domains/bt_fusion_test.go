package domains

import "testing"

// TestBTFusionApproveGateAutoApproves pins the fix for the bt-fusion HITL gate
// misclassification that deadlocked every unattended hourly cycle:
// ApproveFusionReportWrite must be classified local_reversible with
// auto_approve set, matching goap_fusion.go's ApproveGoapFusionApply gate.
func TestBTFusionApproveGateAutoApproves(t *testing.T) {
	tree := BTFusionTree()

	gate := findNode(*tree, "ApproveFusionReportWrite")
	if gate == nil {
		t.Fatal("ApproveFusionReportWrite node not found in BTFusionTree")
	}

	sideEffectClass, _ := gate.Metadata["side_effect_class"].(string)
	if sideEffectClass != "local_reversible" {
		t.Errorf("ApproveFusionReportWrite side_effect_class = %q, want %q", sideEffectClass, "local_reversible")
	}

	autoApprove, _ := gate.Metadata["auto_approve"].(bool)
	if !autoApprove {
		t.Errorf("ApproveFusionReportWrite auto_approve = %v, want true", gate.Metadata["auto_approve"])
	}
}
