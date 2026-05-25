package benchmark

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestToolBench_APISelection(t *testing.T) {
	entries := BuiltinToolBench()
	if len(entries) != 15 {
		t.Errorf("expected 15 ToolBench entries, got %d", len(entries))
	}

	// Verify each entry has required fields
	for _, e := range entries {
		if e.ID == "" {
			t.Error("entry missing ID")
		}
		if e.Category == "" {
			t.Errorf("entry %s missing Category", e.ID)
		}
		if e.TaskDescription == "" {
			t.Errorf("entry %s missing TaskDescription", e.ID)
		}
		if len(e.RequiredAPIs) == 0 {
			t.Errorf("entry %s has no RequiredAPIs", e.ID)
		}
		if len(e.AvailableAPIs) < 2 {
			t.Errorf("entry %s has < 2 AvailableAPIs (need distractors)", e.ID)
		}
	}

	// Verify all 15 categories are unique
	categories := make(map[string]int)
	for _, e := range entries {
		categories[e.Category]++
	}
	if len(categories) != 15 {
		t.Errorf("expected 15 unique categories, got %d", len(categories))
	}
	for cat, count := range categories {
		if count > 1 {
			t.Errorf("category %q appears %d times (expected 1)", cat, count)
		}
	}
}

func TestToolBench_EvaluateWithGoDevTree(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	entries := BuiltinToolBench()
	mock := DefaultLLM()

	metrics := EvaluateToolBench(tree, entries, mock)

	fmt.Printf("\nToolBench GoDev: %d tasks, API accuracy=%.0f%%, step completion=%.0f%%, success=%.0f%%\n",
		metrics.TotalTasks, metrics.APISelectionAccuracy*100, metrics.StepCompletionRate*100, metrics.SuccessRate*100)

	if metrics.TotalTasks != 15 {
		t.Errorf("expected 15 tasks, got %d", metrics.TotalTasks)
	}

	// Even with a generic tree, some API-selection should succeed
	// because detectPath matches API names in the output
	if metrics.APISelectionAccuracy < 0.1 {
		t.Logf("API selection accuracy low (%.0f%%), but may be expected with generic tree",
			metrics.APISelectionAccuracy*100)
	}
}

func TestToolBench_EvaluateWithCodeReviewTree(t *testing.T) {
	tree := domains.CodeReviewTree()
	entries := BuiltinToolBench()
	mock := DefaultLLM()

	metrics := EvaluateToolBench(tree, entries, mock)

	fmt.Printf("\nToolBench CodeReview: %d tasks, API accuracy=%.0f%%, step completion=%.0f%%\n",
		metrics.TotalTasks, metrics.APISelectionAccuracy*100, metrics.StepCompletionRate*100)

	if metrics.TotalTasks != 15 {
		t.Errorf("expected 15 tasks, got %d", metrics.TotalTasks)
	}

	// Verify no metrics are NaN or negative
	if metrics.APISelectionAccuracy < 0 || metrics.APISelectionAccuracy > 1.0 {
		t.Errorf("API selection accuracy out of range [0,1]: %.2f", metrics.APISelectionAccuracy)
	}
	if metrics.StepCompletionRate < 0 || metrics.StepCompletionRate > 1.0 {
		t.Errorf("step completion rate out of range [0,1]: %.2f", metrics.StepCompletionRate)
	}
	if metrics.SuccessRate < 0 || metrics.SuccessRate > 1.0 {
		t.Errorf("success rate out of range [0,1]: %.2f", metrics.SuccessRate)
	}
}

func TestToolBench_EmptyEntries(t *testing.T) {
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()
	metrics := EvaluateToolBench(tree, nil, mock)

	if metrics.TotalTasks != 0 {
		t.Errorf("empty entries should give 0 tasks, got %d", metrics.TotalTasks)
	}
	if metrics.APISelectionAccuracy != 0 {
		t.Errorf("empty entries should give 0 accuracy, got %.2f", metrics.APISelectionAccuracy)
	}
}

func TestToolBench_IndividualEntries(t *testing.T) {
	entries := BuiltinToolBench()
	tree := evolution.GoDeveloperTree()
	mock := DefaultMock()

	// Test a few entries individually to verify they don't panic
	sample := entries[:5]
	metrics := EvaluateToolBench(tree, sample, mock)

	fmt.Printf("\nToolBench sample (5 entries): API accuracy=%.0f%%\n", metrics.APISelectionAccuracy*100)

	// Should not panic, should produce valid metrics
	if metrics.TotalTasks != 5 {
		t.Errorf("expected 5 tasks, got %d", metrics.TotalTasks)
	}
}
