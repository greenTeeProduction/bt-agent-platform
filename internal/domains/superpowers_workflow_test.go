package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestSuperpowersWorkflowTree_ValidatesAndCoversPhases(t *testing.T) {
	tree := SuperpowersWorkflowTree()
	if tree.Type != "PersistentMemSequence" {
		t.Fatalf("root must be PersistentMemSequence, got %s", tree.Type)
	}
	if tree.TimeoutMs != 3600000 {
		t.Fatalf("root TimeoutMs must be 3600000, got %d", tree.TimeoutMs)
	}
	names := map[string]bool{}
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		names[n.Name] = true
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)
	for _, required := range []string{
		"SkillRouter", "BrainstormBranch", "WorkspacePhase", "PlanPhase",
		"ApproveSuperpowersPlan", "TaskLoop", "TDDTask", "VerifyOrDebug",
		"SystematicDebugging", "ChooseFinishOption", "FinishRouter",
	} {
		if !names[required] {
			t.Errorf("missing required node %q", required)
		}
	}
}
