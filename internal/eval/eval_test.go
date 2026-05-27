package eval

import (
	"testing"
)

func TestPlatformEval_AllSuites(t *testing.T) {
	result := RunPlatformEval()
	report := result.FormatReport()
	t.Log("\n" + report)

	if result.TotalTasks == 0 {
		t.Error("no tasks executed")
	}
	if result.SuccessRate < 50 {
		t.Errorf("overall success rate too low: %.1f%%", result.SuccessRate)
	}

	// Verify all 20 suites present
	expectedSuites := 20
	if result.TotalSuites != expectedSuites {
		t.Errorf("expected %d suites, got %d", expectedSuites, result.TotalSuites)
	}

	// Each suite should have at least 3 real tasks
	for _, s := range result.BySuite {
		if s.TotalTasks < 3 {
			t.Errorf("suite %s has only %d tasks (expected >= 3)", s.Name, s.TotalTasks)
		}
	}
}

func TestPlatformEval_JSON(t *testing.T) {
	result := RunPlatformEval()
	jsonStr := result.JSON()
	if len(jsonStr) < 100 {
		t.Error("JSON output too short")
	}
	// Verify key fields present in JSON
	if !contains(jsonStr, "total_suites") {
		t.Error("JSON missing total_suites")
	}
	if !contains(jsonStr, "scorecard") {
		t.Error("JSON missing scorecard")
	}
}

func TestPlatformEval_Scorecard(t *testing.T) {
	result := RunPlatformEval()
	if len(result.Scorecard.UseCases) != 20 {
		t.Errorf("expected 20 use cases in scorecard, got %d", len(result.Scorecard.UseCases))
	}

	// Verify status distribution
	statuses := map[string]int{}
	for _, uc := range result.Scorecard.UseCases {
		statuses[uc.Status]++
	}
	t.Logf("Status distribution: optimized=%d ready=%d partial=%d gap=%d",
		statuses["optimized"], statuses["ready"], statuses["partial"], statuses["gap"])

	// Top automation-fit use cases should be present
	topCases := []string{"Code Review", "Knowledge QA", "Health Monitoring"}
	for _, name := range topCases {
		found := false
		for _, uc := range result.Scorecard.UseCases {
			if uc.Name == name {
				found = true
				if uc.AutomationFit < 80 {
					t.Errorf("%s automation fit too low: %.0f", name, uc.AutomationFit)
				}
				break
			}
		}
		if !found {
			t.Errorf("top use case not found: %s", name)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
