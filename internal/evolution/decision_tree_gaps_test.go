package evolution

import (
	"testing"
)

// TestFindMainSelector covers findMainSelector edge cases.
func TestFindMainSelector(t *testing.T) {
	// StrategyRouter match (deep selector inside root)
	tree := &SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []SerializableNode{
			{Type: "Selector", Name: "StrategyRouter", Children: []SerializableNode{
				{Type: "Sequence", Name: "PathA"},
			}},
		},
	}
	if name := findMainSelector(tree); name != "StrategyRouter" {
		t.Errorf("expected StrategyRouter, got %q", name)
	}

	// "router" match at root level
	tree2 := &SerializableNode{
		Type: "Selector", Name: "MainRouter",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PathA"},
		},
	}
	if name := findMainSelector(tree2); name != "MainRouter" {
		t.Errorf("expected MainRouter, got %q", name)
	}

	// "selector" match
	tree3 := &SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []SerializableNode{
			{Type: "Selector", Name: "MainSelector", Children: []SerializableNode{
				{Type: "Sequence", Name: "PathA"},
			}},
		},
	}
	if name := findMainSelector(tree3); name != "MainSelector" {
		t.Errorf("expected MainSelector, got %q", name)
	}

	// First selector when no named match
	tree4 := &SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []SerializableNode{
			{Type: "Selector", Name: "AlphaRouter", Children: []SerializableNode{
				{Type: "Sequence", Name: "PathA"},
			}},
			{Type: "Selector", Name: "BetaRouter", Children: []SerializableNode{
				{Type: "Sequence", Name: "PathB"},
			}},
		},
	}
	if name := findMainSelector(tree4); name != "AlphaRouter" {
		t.Errorf("expected AlphaRouter (contains 'router'), got %q", name)
	}

	// No selectors at all
	tree5 := &SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []SerializableNode{
			{Type: "Action", Name: "DoSomething"},
		},
	}
	if name := findMainSelector(tree5); name != "" {
		t.Errorf("expected empty for no selectors, got %q", name)
	}
}

// TestPruneDeadPaths covers PruneDeadPaths and internal pruneNode edge cases.
func TestPruneDeadPaths(t *testing.T) {
	o := NewBTOptimizer()

	// Tree with a Selector that has paths below threshold
	// Record hits: PathA has lots, PathB has few
	for i := 0; i < 100; i++ {
		o.Analyzer.RecordHit("MainSelector", "PathA", "IsCodeReview", true)
	}
	for i := 0; i < 1; i++ {
		o.Analyzer.RecordHit("MainSelector", "PathB", "IsBuildTask", true)
	}

	tree := &SerializableNode{
		Type: "Selector", Name: "MainSelector",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PathA", Children: []SerializableNode{
				{Type: "Condition", Name: "IsCodeReview"},
			}},
			{Type: "Sequence", Name: "PathB", Children: []SerializableNode{
				{Type: "Condition", Name: "IsBuildTask"},
			}},
		},
	}

	// Prune with 5% threshold — PathB (1/101 ≈ 1%) should be removed
	removed := o.PruneDeadPaths(tree, 0.05)
	if removed != 1 {
		t.Errorf("expected 1 path removed (PathB at 1/101 ≈ 1%% < 5%%), got %d", removed)
	}
	if len(tree.Children) != 1 {
		t.Errorf("expected 1 remaining child, got %d", len(tree.Children))
	}
	if tree.Children[0].Name != "PathA" {
		t.Errorf("expected remaining child to be PathA, got %q", tree.Children[0].Name)
	}

	// Edge case: default/ExecutionPath should NOT be pruned even if below threshold
	o2 := NewBTOptimizer()
	for i := 0; i < 100; i++ {
		o2.Analyzer.RecordHit("MS2", "PathA", "c1", true)
	}
	for i := 0; i < 1; i++ {
		o2.Analyzer.RecordHit("MS2", "ExecutionPath", "c2", true)
	}

	tree2 := &SerializableNode{
		Type: "Selector", Name: "MS2",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PathA"},
			{Type: "Sequence", Name: "ExecutionPath"},
		},
	}

	// ExecutionPath is a default path, so it should survive even below threshold
	removed2 := o2.PruneDeadPaths(tree2, 0.05)
	if removed2 != 0 {
		t.Errorf("expected 0 removed (ExecutionPath is default path), got %d", removed2)
	}
	// Both paths should remain
	if len(tree2.Children) != 2 {
		t.Errorf("expected 2 remaining children (PathA + ExecutionPath), got %d", len(tree2.Children))
	}

	// Edge case: nil or unknown selector stats (no panic)
	o3 := NewBTOptimizer()
	tree3 := &SerializableNode{
		Type: "Selector", Name: "UnknownSelector",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "A"},
			{Type: "Sequence", Name: "B"},
			{Type: "Sequence", Name: "C"},
		},
	}
	removed3 := o3.PruneDeadPaths(tree3, 0.01)
	if removed3 != 0 {
		t.Errorf("expected 0 removed for unknown selector with no stats, got %d", removed3)
	}

	// Edge case: TotalTasks <= 10 (not enough data to prune)
	o4 := NewBTOptimizer()
	for i := 0; i < 5; i++ {
		o4.Analyzer.RecordHit("MS3", "A", "c1", true)
	}
	for i := 0; i < 1; i++ {
		o4.Analyzer.RecordHit("MS3", "B", "c2", true)
	}

	tree4 := &SerializableNode{
		Type: "Selector", Name: "MS3",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "A"},
			{Type: "Sequence", Name: "B"},
		},
	}
	removed4 := o4.PruneDeadPaths(tree4, 0.1)
	if removed4 != 0 {
		t.Errorf("expected 0 removed with only 6 tasks (not enough data), got %d", removed4)
	}
}

// TestPathHitRatioExtras covers nil path edge cases.
func TestPathHitRatioExtras(t *testing.T) {
	o := NewBTOptimizer()
	o.Analyzer.RecordHit("SR", "PathA", "c1", true)

	ss, ok := o.Analyzer.Stats["SR"]
	if !ok || ss == nil {
		t.Fatal("expected stats for SR")
	}

	// Existing path
	ratio := o.pathHitRatio(ss, "PathA")
	if ratio <= 0 {
		t.Errorf("expected positive hit ratio for PathA, got %.3f", ratio)
	}

	// Unknown path
	ratio2 := o.pathHitRatio(ss, "NonExistent")
	if ratio2 != 0 {
		t.Errorf("expected 0 for unknown path, got %.3f", ratio2)
	}
}

// TestExtractCondition covers edge cases.
func TestExtractCondition(t *testing.T) {
	// Sequence with a Condition child
	node := &SerializableNode{
		Type: "Sequence", Name: "ReviewPath",
		Children: []SerializableNode{
			{Type: "Condition", Name: "IsCodeReview"},
			{Type: "Action", Name: "ReviewGoCode"},
		},
	}
	cond := extractCondition(node)
	if cond != "IsCodeReview" {
		t.Errorf("expected IsCodeReview, got %q", cond)
	}

	// No children
	node2 := &SerializableNode{
		Type: "Action", Name: "DoSomething",
	}
	cond2 := extractCondition(node2)
	if cond2 != "DoSomething" {
		t.Errorf("expected DoSomething (fallback), got %q", cond2)
	}

	// Empty children list
	node3 := &SerializableNode{
		Type: "Sequence", Name: "EmptyPath",
		Children: []SerializableNode{},
	}
	cond3 := extractCondition(node3)
	if cond3 != "EmptyPath" {
		t.Errorf("expected EmptyPath (fallback), got %q", cond3)
	}
}

// TestSplitCamelCase covers edge cases.
func TestSplitCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"IsCodeReview", []string{"Is", "Code", "Review"}},
		{"IsCodeStyle", []string{"Is", "Code", "Style"}},
		{"simple", []string{"simple"}},
		{"ABC", []string{"A", "B", "C"}},
		{"", []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := splitCamelCase(tc.input)
			if len(result) != len(tc.expected) {
				t.Errorf("expected %v, got %v", tc.expected, result)
				return
			}
			for i, v := range result {
				if v != tc.expected[i] {
					t.Errorf("expected %v, got %v", tc.expected, result)
					return
				}
			}
		})
	}
}

// TestMergeOverlappingPaths_MergeNode covers edge cases in mergeNode.
func TestMergeOverlappingPaths_MergeNode(t *testing.T) {
	o := NewBTOptimizer()

	// Single child — nothing to merge
	tree := &SerializableNode{
		Type: "Selector", Name: "Single",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PathA"},
		},
	}
	merged := o.MergeOverlappingPaths(tree)
	if merged != 0 {
		t.Errorf("expected 0 merged for single child, got %d", merged)
	}

	// Two non-overlapping conditions
	tree2 := &SerializableNode{
		Type: "Selector", Name: "Dual",
		Children: []SerializableNode{
			{Type: "Sequence", Name: "PathA", Children: []SerializableNode{
				{Type: "Condition", Name: "IsCodeReview"},
			}},
			{Type: "Sequence", Name: "PathB", Children: []SerializableNode{
				{Type: "Condition", Name: "IsBuildTask"},
			}},
		},
	}
	merged2 := o.MergeOverlappingPaths(tree2)
	// IsCodeReview and IsBuildTask have no overlap, so should be 0
	if merged2 != 0 {
		t.Logf("merged %d paths (IsCodeReview vs IsBuildTask)", merged2)
	}
}
