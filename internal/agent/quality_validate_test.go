package agent

import "testing"

func TestValidateQualitySpec_MinLength(t *testing.T) {
	spec := &QualitySpec{MinLength: 50}
	score, ok, reasons := ValidateQualitySpec(spec, "short")
	if ok {
		t.Fatal("expected min_length failure")
	}
	if len(reasons) != 1 {
		t.Fatalf("reasons: %v", reasons)
	}
	if score <= 0 {
		t.Fatalf("expected non-zero score, got %.2f", score)
	}
}

func TestValidateQualitySpec_RequiredSections(t *testing.T) {
	spec := &QualitySpec{
		MinLength:        20,
		RequiredSections: []string{"Bugs", "Security"},
	}
	out := "## Review\n\n### Bugs\nnone found\n\n### Security\nno issues\n"
	_, ok, reasons := ValidateQualitySpec(spec, out)
	if !ok {
		t.Fatalf("expected pass, reasons: %v", reasons)
	}
}

func TestValidateQualitySpec_BlockedPattern(t *testing.T) {
	spec := &QualitySpec{
		MinLength:       10,
		BlockedPatterns: []string{"error: timeout"},
	}
	_, ok, _ := ValidateQualitySpec(spec, "status ok but error: timeout in logs")
	if ok {
		t.Fatal("expected blocked pattern failure")
	}
}

func TestValidateQualitySpec_NilSpecUsesHeuristic(t *testing.T) {
	out := "## Monitor\nstatus: OK\nseverity: INFO\ntimestamp: now\nthreshold: 5%\n"
	score, ok, reasons := ValidateQualitySpec(nil, out)
	if !ok || len(reasons) > 0 {
		t.Fatalf("expected pass with nil spec, ok=%v reasons=%v", ok, reasons)
	}
	if score < 0.5 {
		t.Fatalf("expected decent heuristic score, got %.2f", score)
	}
}
