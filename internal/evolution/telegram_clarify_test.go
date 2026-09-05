package evolution

import (
	"testing"
)

func TestTelegramClarifyTree(t *testing.T) {
	tree := TelegramClarifyTree()
	if tree == nil {
		t.Fatal("TelegramClarifyTree returned nil")
	}

	// Verify root structure
	if tree.Type != "Sequence" {
		t.Errorf("root type = %s, want Sequence", tree.Type)
	}
	if tree.Name != "TelegramClarify" {
		t.Errorf("root name = %s, want TelegramClarify", tree.Name)
	}
	if len(tree.Children) != 4 {
		t.Errorf("root children = %d, want 4 (PreGate, StrategyRouter, ReflectOnOutcome, OutcomeSelector)", len(tree.Children))
	}

	// Verify PreGate
	preGate := &tree.Children[0]
	if preGate.Name != "PreGate" || preGate.Type != "Sequence" {
		t.Errorf("PreGate = %s/%s, want Sequence/PreGate", preGate.Type, preGate.Name)
	}
	if len(preGate.Children) != 2 {
		t.Errorf("PreGate children = %d, want 2 (IsTelegram, HasQuestion)", len(preGate.Children))
	}
	if preGate.Children[0].Name != "IsTelegram" {
		t.Errorf("PreGate[0] = %s, want IsTelegram", preGate.Children[0].Name)
	}
	if preGate.Children[1].Name != "HasQuestion" {
		t.Errorf("PreGate[1] = %s, want HasQuestion", preGate.Children[1].Name)
	}

	// Verify StrategyRouter
	router := &tree.Children[1]
	if router.Name != "StrategyRouter" || router.Type != "Selector" {
		t.Errorf("StrategyRouter = %s/%s, want Selector/StrategyRouter", router.Type, router.Name)
	}
	if len(router.Children) != 2 {
		t.Errorf("StrategyRouter children = %d, want 2", len(router.Children))
	}

	// Verify ClarifyUsedPath
	happyPath := &router.Children[0]
	if happyPath.Name != "ClarifyUsedPath" {
		t.Errorf("happy path name = %s, want ClarifyUsedPath", happyPath.Name)
	}
	if len(happyPath.Children) != 2 {
		t.Errorf("happy path children = %d, want 2", len(happyPath.Children))
	}
	if happyPath.Children[0].Name != "IsClarifyUsed" {
		t.Errorf("happy[0] = %s, want IsClarifyUsed", happyPath.Children[0].Name)
	}
	if happyPath.Children[1].Name != "MarkClarifyOK" {
		t.Errorf("happy[1] = %s, want MarkClarifyOK", happyPath.Children[1].Name)
	}

	// Verify ViolationPath
	violationPath := &router.Children[1]
	if violationPath.Name != "ClarifyViolationPath" {
		t.Errorf("violation path name = %s, want ClarifyViolationPath", violationPath.Name)
	}
	if len(violationPath.Children) != 2 {
		t.Errorf("violation path children = %d, want 2", len(violationPath.Children))
	}
	if violationPath.Children[0].Name != "ReportClarifyViolation" {
		t.Errorf("violation[0] = %s, want ReportClarifyViolation", violationPath.Children[0].Name)
	}
	if violationPath.Children[1].Name != "SuggestFix" {
		t.Errorf("violation[1] = %s, want SuggestFix", violationPath.Children[1].Name)
	}

	// Verify ReflectOnOutcome + OutcomeSelector
	reflectNode := &tree.Children[2]
	if reflectNode.Name != "ReflectOnOutcome" {
		t.Errorf("reflect = %s, want ReflectOnOutcome", reflectNode.Name)
	}
	outcomeNode := &tree.Children[3]
	if outcomeNode.Name != "OutcomeSelector" || outcomeNode.Type != "Selector" {
		t.Errorf("outcome = %s/%s, want Selector/OutcomeSelector", outcomeNode.Type, outcomeNode.Name)
	}

	t.Logf("TelegramClarifyTree: %d nodes, structure validated OK", CountNodes(tree))
}
