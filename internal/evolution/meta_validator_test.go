package evolution

import "testing"

func TestMetaValidator_AcceptsDefaultTree(t *testing.T) {
	validator := NewMetaValidator(MetaValidatorConfig{
		ArchetypeCategory: "domain",
		MinComposite:      0.30,
	})

	report := validator.Validate(DefaultTree(), 0.82)
	if report.Decision == MetaReject {
		t.Fatalf("expected default tree to pass meta-validation, got reject: %#v", report)
	}
	if report.NodeCount != CountNodes(DefaultTree()) {
		t.Fatalf("node count mismatch: got %d", report.NodeCount)
	}
	if report.Score <= 0 || report.Score > 1 {
		t.Fatalf("score out of range: %f", report.Score)
	}
	if len(report.Checks) == 0 {
		t.Fatal("expected validation checks to be recorded")
	}
}

func TestMetaValidator_RejectsNilTree(t *testing.T) {
	validator := NewMetaValidator(MetaValidatorConfig{})

	report := validator.Validate(nil, 0.90)
	if report.Decision != MetaReject {
		t.Fatalf("expected nil tree to reject, got %s", report.Decision)
	}
	if len(report.Issues) == 0 || report.Issues[0].Check != "nil_tree" {
		t.Fatalf("expected nil_tree issue, got %#v", report.Issues)
	}
}

func TestMetaValidator_RejectsFitnessRegression(t *testing.T) {
	validator := NewMetaValidator(MetaValidatorConfig{MaxRegression: 0.10})

	report := validator.ValidateMutation(DefaultTree(), DefaultTree(), 0.90, 0.70)
	if report.Decision != MetaReject {
		t.Fatalf("expected regression reject, got %s: %#v", report.Decision, report)
	}
	if report.Regression <= 0 {
		t.Fatalf("expected regression to be populated, got %f", report.Regression)
	}
}

func TestMetaValidator_RejectsBrokenStructure(t *testing.T) {
	bad := &SerializableNode{
		Type: "Action",
		Name: "RootAction",
		Children: []SerializableNode{
			{Type: "Retry", Name: "LoopForever", MaxRetries: 99},
		},
	}
	validator := NewMetaValidator(MetaValidatorConfig{})

	report := validator.Validate(bad, 0.95)
	if report.Decision != MetaReject {
		t.Fatalf("expected broken structure reject, got %s: %#v", report.Decision, report)
	}
	if !hasMetaIssue(report, "root_type") || !hasMetaIssue(report, "retry_bounds") {
		t.Fatalf("expected root_type and retry_bounds issues, got %#v", report.Issues)
	}
}

func TestMetaValidator_WarnsOnArchetypeMismatch(t *testing.T) {
	minimal := &SerializableNode{
		Type: "Sequence",
		Name: "TinyRoot",
		Children: []SerializableNode{
			{Type: "Action", Name: "DoWork"},
		},
	}
	validator := NewMetaValidator(MetaValidatorConfig{
		ArchetypeCategory: "domain",
		MinComposite:      0.30,
	})

	report := validator.Validate(minimal, 0.90)
	if report.Decision == MetaReject {
		t.Fatalf("expected small valid tree to warn but not reject, got %#v", report)
	}
	if len(report.Warnings) == 0 {
		t.Fatalf("expected archetype warnings, got %#v", report)
	}
}

// TestMetaValidator_DefaultMinCompositeMatchesRealCallerScale guards against
// the default MinComposite being expressed on a 0-1 scale while every real
// caller (evaluator.FitnessScore.Composite, see internal/evaluator/stockfish.go)
// feeds 0-100 "centipawns-like" composite scores. Production wires the
// MetaValidator with a zero-value MetaValidatorConfig (cmd/bt-gardener/config.go),
// so a badly-scaled default silently disables the fitness-floor safety check.
func TestMetaValidator_DefaultMinCompositeMatchesRealCallerScale(t *testing.T) {
	validator := NewMetaValidator(MetaValidatorConfig{})

	// A composite of 5.0 on the real 0-100 scale is far below any reasonable
	// safety floor, but is nowhere near the buggy 0-1 scale default of 0.30.
	report := validator.Validate(DefaultTree(), 5.0)
	if !hasMetaIssue(report, "fitness_floor") {
		t.Fatalf("expected fitness_floor issue for a low 0-100 scale composite under the default config, got %#v", report)
	}
}

func hasMetaIssue(report MetaValidationReport, check string) bool {
	for _, issue := range report.Issues {
		if issue.Check == check {
			return true
		}
	}
	return false
}
