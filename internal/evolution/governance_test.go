package evolution

import "testing"

func TestGovernanceOnlyCreditsControlsOnTheWorkersExecutionPath(t *testing.T) {
	work := SerializableNode{Type: "Action", Name: "ProduceReport", Description: "Produce the task report"}
	tree := &SerializableNode{Type: "Selector", Children: []SerializableNode{
		{Type: "Sequence", Children: []SerializableNode{{Type: "Condition", Name: "ValidateInput"}, {Type: "AlwaysFail"}}},
		work,
	}}
	if got := AssessGovernance(tree); got.WorkNodes != 1 || got.InputGuarded != 0 {
		t.Fatalf("alternative branch inherited an input gate: %+v", got)
	}
	tree.Type = "Sequence"
	tree.Children = []SerializableNode{{Type: "Condition", Name: "ValidateInput"}, work}
	guarded := AssessGovernance(tree)
	tree.Children = append([]SerializableNode{{Type: "Condition", Name: "ValidateInput"}}, tree.Children...)
	if duplicated := AssessGovernance(tree); guarded.InputGuarded != 1 || duplicated.Score != guarded.Score {
		t.Fatalf("duplicate guards changed fitness: before=%+v after=%+v", guarded, duplicated)
	}
}

func TestFinalCheckDoesNotPretendToVerifyEveryIntermediateResult(t *testing.T) {
	tree := &SerializableNode{Type: "Sequence", Children: []SerializableNode{
		{Type: "Action", Name: "FirstResult"},
		{Type: "Action", Name: "SecondResult"},
		{Type: "Condition", Name: "ValidateOutput"},
	}}
	if got := AssessGovernance(tree); got.WorkNodes != 2 || got.ResultChecked != 1 {
		t.Fatalf("a final-output check must not claim both intermediate results: %+v", got)
	}
	tree.Children[0] = SerializableNode{Type: "QualityGate", Children: []SerializableNode{tree.Children[0]}}
	if got := AssessGovernance(tree); got.ResultChecked != 2 {
		t.Fatalf("an intermediate quality gateway must receive credit: %+v", got)
	}
}

func TestCheckpointNeedsDeclaredPostconditionsForGovernanceCredit(t *testing.T) {
	tree := &SerializableNode{Type: "CheckpointVerifier", Children: []SerializableNode{{Type: "Action", Name: "ProduceReport"}}}
	if got := AssessGovernance(tree); got.ResultChecked != 0 {
		t.Fatalf("empty checkpoint is not a verification contract: %+v", got)
	}
	tree.Metadata = map[string]any{"postconditions": map[string]any{"report_verified": true}}
	if got := AssessGovernance(tree); got.ResultChecked != 1 {
		t.Fatalf("declared checkpoint was not credited: %+v", got)
	}
}

func TestGovernanceDoesNotCreditDecorativeOrBypassedControls(t *testing.T) {
	work := SerializableNode{Type: "ChainAction", Name: "agent:Report", Description: "Long instructions that are not actually sent to the agent", Metadata: map[string]any{"max_iterations": float64(3)}, TimeoutMs: 100}
	if g := AssessGovernance(&work); g.Guided != 0 || g.Bounded != 0 {
		t.Fatalf("ignored runtime configuration earned credit: %+v", g)
	}
	gate := SerializableNode{Type: "QualityGate", Children: []SerializableNode{work}}
	masked := SerializableNode{Type: "Succeeder", Children: []SerializableNode{gate}}
	if g := AssessGovernance(&masked); g.ResultChecked != 0 {
		t.Fatalf("swallowed quality failure earned credit: %+v", g)
	}
	gate.Children = []SerializableNode{{Type: "Sequence", Children: []SerializableNode{work, {Type: "Action", Name: "OtherWork"}}}}
	if g := AssessGovernance(&gate); g.ResultChecked != 1 {
		t.Fatalf("outer gate credited unverified intermediate work: %+v", g)
	}
}

func TestGovernancePromotionPreservesTaskCapabilitiesAndChecks(t *testing.T) {
	work := SerializableNode{Type: "ChainAction", Name: "agent:Report"}
	base := SerializableNode{Type: "Sequence", Children: []SerializableNode{
		{Type: "QualityGate", Children: []SerializableNode{work}},
		{Type: "Action", Name: "DeliverReport"},
	}}
	pruned := base
	pruned.Children = base.Children[:1]
	if PreservesGovernance(&base, &pruned) {
		t.Fatal("deleting delivery must not pass as an improvement")
	}
	stripped := SerializableNode{Type: "Sequence", Children: []SerializableNode{work, base.Children[1]}}
	if PreservesGovernance(&base, &stripped) {
		t.Fatal("deleting a result check must not pass")
	}
	if !PreservesGovernance(&base, &base) {
		t.Fatal("unchanged capabilities should pass preservation")
	}
}
