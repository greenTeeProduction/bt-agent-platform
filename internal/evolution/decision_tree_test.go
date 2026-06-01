package evolution

import (
	"testing"
)

func TestDTAnalyzer_Entropy(t *testing.T) {
	d := NewDTAnalyzer()
	// Record 10 tasks: 6 hit PathA, 4 hit PathB
	for i := 0; i < 6; i++ {
		d.RecordHit("StrategyRouter", "PathA", "IsCodeReview", true)
	}
	for i := 0; i < 4; i++ {
		d.RecordHit("StrategyRouter", "PathB", "IsBuildTask", true)
	}

	entropy := d.Entropy("StrategyRouter")
	// Expected: -(0.6*log2(0.6) + 0.4*log2(0.4)) ≈ 0.971
	if entropy < 0.9 || entropy > 1.0 {
		t.Errorf("expected entropy ~0.97, got %.3f", entropy)
	}
}

func TestDTAnalyzer_Gini(t *testing.T) {
	d := NewDTAnalyzer()
	for i := 0; i < 10; i++ {
		d.RecordHit("SR", "PathA", "Check", true)
	}
	for i := 0; i < 0; i++ {
		d.RecordHit("SR", "PathB", "Other", true)
	}

	// Pure split: Gini = 1 - (1.0^2 + 0.0^2) = 0
	gini := d.GiniImpurity("SR")
	if gini > 0.01 {
		t.Errorf("expected gini ~0 for pure split, got %.3f", gini)
	}

	// 50/50 split
	d2 := NewDTAnalyzer()
	for i := 0; i < 5; i++ {
		d2.RecordHit("SR2", "A", "x", true)
	}
	for i := 0; i < 5; i++ {
		d2.RecordHit("SR2", "B", "y", true)
	}
	gini2 := d2.GiniImpurity("SR2")
	// Gini = 1 - (0.5^2 + 0.5^2) = 0.5
	if gini2 < 0.45 || gini2 > 0.55 {
		t.Errorf("expected gini ~0.5 for balanced split, got %.3f", gini2)
	}
}

func TestDTAnalyzer_BestSplit(t *testing.T) {
	d := NewDTAnalyzer()
	// Condition "IsCodeReview" perfectly splits: always hits PathA
	for i := 0; i < 8; i++ {
		d.RecordHit("SR", "PathA", "IsCodeReview", true)
	}
	for i := 0; i < 2; i++ {
		d.RecordHit("SR", "PathB", "IsBuildTask", true)
	}

	best := d.BestSplitCondition("SR")
	if best != "IsCodeReview" {
		t.Errorf("expected IsCodeReview as best split, got %q", best)
	}
}

func TestBTOptimizer_ReorderSelectors(t *testing.T) {
	o := NewBTOptimizer()
	// Record usage: PathB hit more often than PathA
	for i := 0; i < 8; i++ {
		o.Analyzer.RecordHit("StrategyRouter", "BuildPath", "IsBuildTask", true)
	}
	for i := 0; i < 2; i++ {
		o.Analyzer.RecordHit("StrategyRouter", "ReviewPath", "IsCodeReview", true)
	}

	tree := &SerializableNode{
		Type: "Selector", Name: "StrategyRouter",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "ReviewPath", Children: []SerializableNode{
				{Type: "Condition", Name: "IsCodeReview"},
			}},
			{Type: "Sequence", Name: "BuildPath", Children: []SerializableNode{
				{Type: "Condition", Name: "IsBuildTask"},
			}},
			{Type: "Sequence", Name: "ExecutionPath", Children: []SerializableNode{
				{Type: "Condition", Name: "AlwaysSucceed"},
			}},
		},
	}

	changes := o.OptimizeSelectors(tree)
	if changes == 0 {
		t.Log("no reordering needed or no stats available")
	}
	// ExecutionPath should be last (isDefaultPath)
	if tree.Children[len(tree.Children)-1].Name != "ExecutionPath" {
		t.Error("ExecutionPath should be last (default path)")
	}

	report := o.AnalyzeTree(tree, "test")
	if report.OverallScore < 0 || report.OverallScore > 10 {
		t.Errorf("invalid overall score: %.2f", report.OverallScore)
	}
	t.Logf("DT Report: Entropy=%.3f, Gini=%.3f, BestSplit=%q, Score=%.1f",
		report.Entropy, report.Gini, report.BestSplit, report.OverallScore)
}

func TestConditionOverlap(t *testing.T) {
	// "IsCodeReview" and "IsCodeStyle" overlap (both contain "code")
	overlap := conditionOverlap("IsCodeReview", "IsCodeStyle")
	if overlap < 0.3 {
		t.Errorf("expected overlap for code-related conditions, got %.2f", overlap)
	}

	// "IsCodeReview" and "IsBuildTask" have no overlap
	overlap2 := conditionOverlap("IsCodeReview", "IsBuildTask")
	if overlap2 > 0.3 {
		t.Errorf("expected no overlap for different conditions, got %.2f", overlap2)
	}
}

func TestDTAnalyzer_InformationGain(t *testing.T) {
	d := NewDTAnalyzer()
	// Simulate: CodeReview paths succeed 90%, Build path succeeds 50%
	for i := 0; i < 9; i++ {
		d.RecordHit("SR", "Review", "IsCodeReview", true)
	}
	d.RecordHit("SR", "Review", "IsCodeReview", false)
	for i := 0; i < 5; i++ {
		d.RecordHit("SR", "Build", "IsBuildTask", true)
	}
	for i := 0; i < 5; i++ {
		d.RecordHit("SR", "Build", "IsBuildTask", false)
	}

	gain := d.InformationGain("SR", "IsCodeReview")
	t.Logf("Information gain for IsCodeReview: %.4f", gain)
	if gain < 0 {
		t.Error("information gain should be non-negative")
	}
}
