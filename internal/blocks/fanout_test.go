package blocks

import (
	"testing"
)

// Characterization tests for fanout.go. These pin the *current* exported
// behavior of ParallelFanoutBlock and MergeResultsBlock before any future
// refactor; they make no production changes.

func TestParallelFanoutBlock_RootShape(t *testing.T) {
	n := ParallelFanoutBlock()
	if n.Type != "Sequence" {
		t.Errorf("root type = %q, want %q", n.Type, "Sequence")
	}
	if n.Name != "ParallelFanout" {
		t.Errorf("root name = %q, want %q", n.Name, "ParallelFanout")
	}
	if n.Description != "Decompose plan into subtasks and merge outputs" {
		t.Errorf("root description = %q", n.Description)
	}
	if len(n.Children) != 3 {
		t.Fatalf("root children = %d, want 3", len(n.Children))
	}
}

func TestParallelFanoutBlock_Children(t *testing.T) {
	n := ParallelFanoutBlock()
	if len(n.Children) != 3 {
		t.Fatalf("root children = %d, want 3", len(n.Children))
	}

	cases := []struct {
		name        string
		wantType    string
		wantName    string
		wantDesc    string
		wantNumKids int
	}{
		{"HasPlan condition", "Condition", "HasPlan", "Plan available for fan-out", 0},
		{"FanoutParallel", "Parallel", "FanoutParallel", "Execute plan steps in parallel (all must succeed)", 1},
		{"MergeResults action", "Action", "MergeResults", "Combine bb.Results into bb.Result", 0},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			child := n.Children[i]
			if child.Type != tc.wantType {
				t.Errorf("children[%d].Type = %q, want %q", i, child.Type, tc.wantType)
			}
			if child.Name != tc.wantName {
				t.Errorf("children[%d].Name = %q, want %q", i, child.Name, tc.wantName)
			}
			if child.Description != tc.wantDesc {
				t.Errorf("children[%d].Description = %q, want %q", i, child.Description, tc.wantDesc)
			}
			if len(child.Children) != tc.wantNumKids {
				t.Errorf("children[%d] has %d children, want %d", i, len(child.Children), tc.wantNumKids)
			}
		})
	}
}

// TestParallelFanoutBlock_ParallelMetadata pins the "all must succeed"
// success policy on the FanoutParallel node.
func TestParallelFanoutBlock_ParallelMetadata(t *testing.T) {
	n := ParallelFanoutBlock()
	parallel := n.Children[1]
	policy, ok := parallel.Metadata["success_policy"]
	if !ok {
		t.Fatal("FanoutParallel missing success_policy metadata")
	}
	if policy != "all" {
		t.Errorf("success_policy = %v, want %q", policy, "all")
	}
}

// TestParallelFanoutBlock_ChainAction pins the map_reduce ChainAction leaf:
// its prompt-carrying Name, Description, and max_tokens budget.
func TestParallelFanoutBlock_ChainAction(t *testing.T) {
	n := ParallelFanoutBlock()
	parallel := n.Children[1]
	if len(parallel.Children) != 1 {
		t.Fatalf("FanoutParallel has %d children, want 1", len(parallel.Children))
	}
	chain := parallel.Children[0]

	if chain.Type != "ChainAction" {
		t.Errorf("chain action type = %q, want %q", chain.Type, "ChainAction")
	}
	wantName := "map_reduce:Execute subtasks from the plan.\n\nTask: {{.Task}}\nPlan: {{.Plan}}"
	if chain.Name != wantName {
		t.Errorf("chain action name = %q, want %q", chain.Name, wantName)
	}
	if chain.Description != "Map-reduce over plan steps" {
		t.Errorf("chain action description = %q", chain.Description)
	}
	maxTokens, ok := chain.Metadata["max_tokens"]
	if !ok {
		t.Fatal("chain action missing max_tokens metadata")
	}
	if maxTokens != float64(2048) {
		t.Errorf("max_tokens = %v, want %v", maxTokens, float64(2048))
	}
}

// TestParallelFanoutBlock_Validates pins that the tree returned by
// ParallelFanoutBlock currently passes evolution.SerializableNode.Validate()
// with no errors.
func TestParallelFanoutBlock_Validates(t *testing.T) {
	n := ParallelFanoutBlock()
	if errs := n.Validate(); len(errs) != 0 {
		t.Errorf("Validate() = %v, want no errors", errs)
	}
}

func TestMergeResultsBlock_Shape(t *testing.T) {
	n := MergeResultsBlock()
	if n.Type != "Sequence" {
		t.Errorf("root type = %q, want %q", n.Type, "Sequence")
	}
	if n.Name != "MergeResults" {
		t.Errorf("root name = %q, want %q", n.Name, "MergeResults")
	}
	if len(n.Children) != 1 {
		t.Fatalf("root children = %d, want 1", len(n.Children))
	}

	child := n.Children[0]
	if child.Type != "Action" {
		t.Errorf("child type = %q, want %q", child.Type, "Action")
	}
	if child.Name != "MergeResults" {
		t.Errorf("child name = %q, want %q", child.Name, "MergeResults")
	}
	if child.Description != "Merge bb.Results" {
		t.Errorf("child description = %q", child.Description)
	}
}

// TestMergeResultsBlock_Validates pins that the tree returned by
// MergeResultsBlock currently passes evolution.SerializableNode.Validate()
// with no errors.
func TestMergeResultsBlock_Validates(t *testing.T) {
	n := MergeResultsBlock()
	if errs := n.Validate(); len(errs) != 0 {
		t.Errorf("Validate() = %v, want no errors", errs)
	}
}

// TestFanoutBlocks_ReturnFreshValues pins that both constructors return
// independent values on each call — mutating one call's result must not
// leak into a subsequent call.
func TestFanoutBlocks_ReturnFreshValues(t *testing.T) {
	a := ParallelFanoutBlock()
	a.Children[0].Name = "mutated"
	b := ParallelFanoutBlock()
	if b.Children[0].Name != "HasPlan" {
		t.Errorf("mutation of one ParallelFanoutBlock() result leaked into another: got %q", b.Children[0].Name)
	}

	c := MergeResultsBlock()
	c.Children[0].Name = "mutated"
	d := MergeResultsBlock()
	if d.Children[0].Name != "MergeResults" {
		t.Errorf("mutation of one MergeResultsBlock() result leaked into another: got %q", d.Children[0].Name)
	}
}
