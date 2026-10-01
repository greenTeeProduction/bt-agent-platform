package evaluator

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func governedReportTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{Type: "Sequence", Name: "report", Children: []evolution.SerializableNode{
		{Type: "Condition", Name: "ValidateInput"},
		{Type: "Timeout", Name: "ReportBudget", TimeoutMs: 30000, Children: []evolution.SerializableNode{
			{Type: "QualityGate", Name: "VerifyReport", Metadata: map[string]any{"result_contract": map[string]any{"required_keys": []string{"summary", "sources"}}}, Children: []evolution.SerializableNode{
				{Type: "ChainAction", Name: "llm_call:Produce the requested report as JSON with summary and sources fields using the supplied evidence.", Description: "Produce the requested report and cite its evidence.", Metadata: map[string]any{"max_tokens": float64(1024)}},
			}},
		}},
	}}
}

func TestFitnessRewardsEnforcedQualityGateInsteadOfItsDeletion(t *testing.T) {
	tree := governedReportTree()
	pruned := governedReportTree()
	pruned.Children[1].Children[0] = pruned.Children[1].Children[0].Children[0]
	records := []evolution.Record{{Outcome: evolution.Success, DurationMs: 100}}
	before, after := EvaluateTree(tree, records), EvaluateTree(pruned, records)
	if before.Composite <= after.Composite {
		t.Fatalf("removing the result gate must lose fitness: governed=%+v pruned=%+v", before, after)
	}
}

func TestFitnessDoesNotRewardRecoverySkeletonOverTaskWorkflow(t *testing.T) {
	skeleton := &evolution.SerializableNode{Type: "Sequence", Name: "collapsed", Children: []evolution.SerializableNode{
		{Type: "Selector", Name: "OutcomeSelector", Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "WasSuccessful"},
			{Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect"}}},
			{Type: "Action", Name: "EscalateToDeepSeek"},
		}},
	}}
	records := []evolution.Record{{Outcome: evolution.Success, DurationMs: 100}}
	if good, bad := EvaluateTree(governedReportTree(), records), EvaluateTree(skeleton, records); good.Composite <= bad.Composite {
		t.Fatalf("taskless recovery skeleton outranked governed work: good=%+v bad=%+v", good, bad)
	}
}

func TestDecorativeDepthDoesNotChangeFitness(t *testing.T) {
	tree := governedReportTree()
	wrapped := &evolution.SerializableNode{Type: "Sequence", Name: "Decoration", Children: []evolution.SerializableNode{*tree}}
	records := []evolution.Record{{Outcome: evolution.Success, DurationMs: 100}}
	if a, b := EvaluateTree(tree, records).Composite, EvaluateTree(wrapped, records).Composite; a != b {
		t.Fatalf("decorative wrappers must neither earn credit nor induce pruning reward: %v != %v", a, b)
	}
}
