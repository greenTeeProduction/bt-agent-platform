package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestSuperpowersPipeline_ProductionContract_NoPlaceholderPath(t *testing.T) {
	tree := SuperpowersPipelineTree()
	if tree == nil {
		t.Fatal("SuperpowersPipelineTree returned nil")
	}

	for _, typ := range []string{"ChainAgent"} {
		if containsNodeType(*tree, typ) {
			t.Fatalf("production Superpowers tree must not contain placeholder node type %q", typ)
		}
	}

	forbiddenNames := []string{"SkipBrainstorm", "SkipWorktree", "ManualIntervention", "BuildApprovalRequest", "SendHITLRequest"}
	for _, name := range forbiddenNames {
		if findNode(*tree, name) != nil {
			t.Fatalf("production Superpowers tree must not contain placeholder/sidecar node %q", name)
		}
	}

	required := []string{
		"InitSuperpowersRun",
		"GenerateDesignArtifact",
		"ValidateDesignArtifact",
		"PrepareSuperpowersWorktree",
		"VerifySuperpowersBaseline",
		"GenerateImplementationPlan",
		"ValidateImplementationPlanStrict",
		"ApproveSuperpowersPlan",
		"ExecuteSuperpowersTaskBatch",
		"VerifySuperpowersRun",
		"ApplySuperpowersRunToMainRepo",
		"WriteSuperpowersFinishReport",
		"ReportPipelineComplete",
	}
	for _, name := range required {
		if findNode(*tree, name) == nil {
			t.Fatalf("production Superpowers tree missing %q", name)
		}
	}
	if !containsNodeType(*tree, "HumanApprovalGate") {
		t.Fatalf("production Superpowers tree must use native HumanApprovalGate")
	}
}

func containsNodeType(node evolution.SerializableNode, nodeType string) bool {
	if node.Type == nodeType {
		return true
	}
	for _, child := range node.Children {
		if containsNodeType(child, nodeType) {
			return true
		}
	}
	return false
}
