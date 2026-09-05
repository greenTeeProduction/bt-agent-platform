package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestSuperpowersPipelineSimulation(t *testing.T) {
	tree := SuperpowersPipelineTree()
	if tree == nil {
		t.Fatal("Tree is nil")
	}
	if tree.Name != "SuperpowersPipeline_Main" {
		t.Fatalf("unexpected tree name %q", tree.Name)
	}

	nodeCount := countTreeNodes(tree, 0)
	if nodeCount < 20 {
		t.Fatalf("too few production nodes: %d", nodeCount)
	}

	bb := &engine.Blackboard{
		Task:       "dry_run: add /health endpoint to dashboard",
		ChainState: make(map[string]any),
		Results:    []string{},
	}
	cmd, err := engine.BuildAndValidate(tree, bb)
	if err != nil {
		t.Fatalf("Tree validation failed: %v", err)
	}
	if cmd == nil {
		t.Fatal("BuildAndValidate returned nil command")
	}

	requiredActions := []string{
		"InitSuperpowersRun",
		"LoadSuperpowersSkills",
		"GenerateDesignArtifact",
		"ValidateDesignArtifact",
		"PrepareSuperpowersWorktree",
		"VerifySuperpowersBaseline",
		"GenerateImplementationPlan",
		"ValidateImplementationPlanStrict",
		"ExecuteSuperpowersTaskBatch",
		"VerifySuperpowersRun",
		"WriteSuperpowersFinishReport",
		"ReportPipelineComplete",
	}
	for _, name := range requiredActions {
		if engine.GetAction(name) == nil {
			t.Errorf("Missing action: %s", name)
		}
	}

	if engine.GetCondition("IsSuperpowersDryRun") == nil {
		t.Errorf("Missing condition: IsSuperpowersDryRun")
	}
}

func countTreeNodes(node *evolution.SerializableNode, depth int) int {
	if node == nil {
		return 0
	}
	count := 1
	for i := range node.Children {
		count += countTreeNodes(&node.Children[i], depth+1)
	}
	return count
}
