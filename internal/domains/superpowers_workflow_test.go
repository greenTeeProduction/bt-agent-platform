package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestSuperpowersWorkflowTree_GrillLoopShape uses the findNode(node
// evolution.SerializableNode, name string) *evolution.SerializableNode
// helper already defined in domains_test.go (dereferencing pointers at each
// call site to match its by-value signature).
func TestSuperpowersWorkflowTree_GrillLoopShape(t *testing.T) {
	tree := SuperpowersWorkflowTree()
	brainstorm := findNode(*tree, "BrainstormBranch")
	if brainstorm == nil {
		t.Fatal("BrainstormBranch missing")
	}
	router := findNode(*brainstorm, "GrillConvergenceRouter")
	if router == nil || router.Type != "Selector" {
		t.Fatalf("GrillConvergenceRouter missing or wrong type: %+v", router)
	}
	loop := findNode(*router, "GrillLoop")
	if loop == nil || loop.Type != "ReviewCycle" {
		t.Fatal("GrillLoop ReviewCycle missing")
	}
	if ra, _ := loop.Metadata["reviewer_action"].(string); ra != "GrillDesignArtifact" {
		t.Fatalf("reviewer_action = %q", ra)
	}
	if mi, _ := loop.Metadata["max_iterations"].(int); mi != 10 {
		t.Fatalf("max_iterations = %v", loop.Metadata["max_iterations"])
	}
	round := findNode(*loop, "GrillRound")
	if round == nil || round.Type != "MemSequence" || len(round.Children) != 2 ||
		round.Children[0].Name != "ReviseDesignArtifact" || round.Children[1].Name != "ValidateRevisedDesign" {
		t.Fatalf("GrillRound shape wrong: %+v", round)
	}
	split := findNode(*router, "SplitPath")
	if split == nil || split.Children[0].Name != "SplitDesignArtifact" || split.Children[1].Name != "ValidateSplitDesign" {
		t.Fatalf("SplitPath shape wrong: %+v", split)
	}
	if findNode(*brainstorm, "GrillDesignArtifact") != nil {
		t.Fatal("standalone GrillDesignArtifact leaf must be gone (it is the reviewer now)")
	}
}

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
