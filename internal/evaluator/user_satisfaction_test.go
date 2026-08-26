package evaluator

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// User satisfaction fitness dimension (ADR-133 Phase 5): explicit 👍/👎
// signals recorded via bt_feedback must shift the composite, and their
// absence must leave the historical formula untouched.

func satisfactionRecords(positive, negative, plain int) []evolution.Record {
	var records []evolution.Record
	for range positive {
		records = append(records, evolution.Record{
			Outcome: evolution.Success, DurationMs: 1000,
			UserFeedback: evolution.FeedbackPositive,
		})
	}
	for range negative {
		records = append(records, evolution.Record{
			Outcome: evolution.Failure, DurationMs: 1000,
			UserFeedback: evolution.FeedbackNegative,
		})
	}
	for range plain {
		records = append(records, evolution.Record{
			Outcome: evolution.Success, DurationMs: 1000,
		})
	}
	return records
}

func TestEvaluateTree_NoFeedbackLeavesCompositeUnchanged(t *testing.T) {
	tree := evolution.DefaultTree()
	records := satisfactionRecords(0, 0, 5)

	fitness := EvaluateTree(tree, records)

	if fitness.UserSatisfaction != -1 {
		t.Errorf("UserSatisfaction = %v, want -1 (no feedback)", fitness.UserSatisfaction)
	}

	// Recompute the historical formula and confirm no satisfaction rescale.
	base := fitness.SuccessRate*50 +
		fitness.Stability*15 +
		fitness.PathCoverage*15 +
		(1.0-minFloat64(float64(fitness.AvgDurationMs)/120000.0, 1.0))*10 +
		fitness.StructuralQuality*8 +
		(1.0-minFloat64(float64(fitness.NodeCount)/100.0, 1.0))*2
	if diff := fitness.Composite - base; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("Composite = %v, want unrescaled base %v", fitness.Composite, base)
	}
}

func TestEvaluateTree_FeedbackShiftsComposite(t *testing.T) {
	tree := evolution.DefaultTree()

	liked := EvaluateTree(tree, satisfactionRecords(4, 0, 0))
	disliked := EvaluateTree(tree, satisfactionRecords(0, 4, 0))

	if liked.UserSatisfaction != 1.0 {
		t.Errorf("all-positive UserSatisfaction = %v, want 1.0", liked.UserSatisfaction)
	}
	if disliked.UserSatisfaction != 0.0 {
		t.Errorf("all-negative UserSatisfaction = %v, want 0.0", disliked.UserSatisfaction)
	}

	// Same tree, and the negative records also fail, so the success-rate gap
	// already separates them; the satisfaction term must widen that gap by
	// its full 10-point swing on top of the 90% base rescale.
	mixed := EvaluateTree(tree, satisfactionRecords(2, 2, 0))
	if mixed.UserSatisfaction != 0.5 {
		t.Errorf("mixed UserSatisfaction = %v, want 0.5", mixed.UserSatisfaction)
	}
	if !(liked.Composite > mixed.Composite && mixed.Composite > disliked.Composite) {
		t.Errorf("composite ordering broken: liked=%v mixed=%v disliked=%v",
			liked.Composite, mixed.Composite, disliked.Composite)
	}
}

func TestEvaluateTree_EmptyRecordsSatisfactionUnknown(t *testing.T) {
	fitness := EvaluateTree(evolution.DefaultTree(), nil)
	if fitness.UserSatisfaction != -1 {
		t.Errorf("UserSatisfaction = %v, want -1 for empty history", fitness.UserSatisfaction)
	}
}
