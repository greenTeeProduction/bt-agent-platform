package engine

import "testing"

func TestValidateOutputQuality_IncompleteStepLimitFails(t *testing.T) {
	bb := &Blackboard{Result: "## Final Answer\n\nThe investigation is INCOMPLETE — the agent reached its step limit before finishing. Some items could not be determined or verified."}
	if validateOutputQuality(bb) {
		t.Fatalf("expected incomplete/step-limit output to fail quality validation, score=%.2f", bb.QualityScore)
	}
	if bb.QualityScore > 0.1 {
		t.Fatalf("expected low quality score for incomplete output, got %.2f", bb.QualityScore)
	}
}
