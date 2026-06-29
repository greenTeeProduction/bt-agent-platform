package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestGoapFusion_Structure is a structural smoke test for GoapFusionTree.
// It verifies the production dual-mode shape: new research-backed gaps use the
// Superpowers implementation path, while unchanged goals can fall back to
// deterministic analysis without creating HITL gates.
func TestGoapFusion_Structure(t *testing.T) {
	bb := &engine.Blackboard{
		Task: "analyze research gaps and apply the highest-priority improvement: implement one concrete fix",
		LLM:  &engine.MockLLM{},
	}

	for _, withVerifier := range []bool{false, true} {
		tree := GoapFusionTree(withVerifier)
		if tree == nil {
			t.Fatalf("GoapFusionTree(%v) returned nil", withVerifier)
		}
		if cmd := engine.BuildTree(tree, bb); cmd == nil {
			t.Fatalf("BuildTree returned nil for GoapFusionTree(%v)", withVerifier)
		}
	}

	tree := GoapFusionTree(false)

	requiredNodes := []string{
		"PreGate",
		"ExecutionRouter",
		"ClaudeSuperpowersPath",
		"ScheduledAnalysisPath",
		"ApproveGoapFusionApply",
	}
	for _, name := range requiredNodes {
		if findNode(*tree, name) == nil {
			t.Errorf("GoapFusionTree missing expected node %q", name)
		}
	}
	if containsNodeType(*tree, "ChainAgent") {
		t.Fatalf("GoapFusionTree must not contain ChainAgent fallback")
	}
	if containsNodeName(*tree, "SelfCorrect") || containsNodeName(*tree, "EscalateToDeepSeek") {
		t.Fatalf("GoapFusionTree must not use LLM self-correction/escalation; scheduled outputs need deterministic evidence gates")
	}
	if !containsNodeType(*tree, "HumanApprovalGate") {
		t.Fatalf("explicit GOAP apply path must use HumanApprovalGate")
	}

	requiredConditions := []string{
		"ValidateInput",
		"IsFusionTask",
		"IsApplyRequest",
		"HasNewGaps",
		"NoNewGaps",
	}
	for _, name := range requiredConditions {
		if engine.GetCondition(name) == nil {
			t.Errorf("condition %q not registered in engine", name)
		}
	}

	requiredActions := []string{
		"SetupFusionTools",
		"ReadVaultResearch",
		"ReadGraphifyReport",
		"AnalyzeImprovementGaps",
		"PrioritizeGoapGoals",
		"WriteSuperpowersImplementationPlan",
		"RunSuperpowersClaudeImplementation",
		"VerifyGoapBuild",
		"RunGraphifyUpdate",
		"ReportSuperpowersImplementation",
		"WriteFusionAnalysis",
		"ReportFusionCycle",
		"VerifyGoapFusionEvidence",
		"MarkSuccessful",
	}
	for _, name := range requiredActions {
		if engine.GetAction(name) == nil {
			t.Errorf("action %q not registered in engine", name)
		}
	}

	scheduled := findNode(*tree, "ScheduledAnalysisPath")
	if scheduled == nil {
		t.Fatal("missing ScheduledAnalysisPath")
	}
	if containsNodeType(*scheduled, "HumanApprovalGate") {
		t.Fatalf("scheduled/default GOAP path must be analysis-only and must not create HITL gates")
	}
	if containsNodeName(*scheduled, "RunSuperpowersClaudeImplementation") {
		t.Fatalf("scheduled/default GOAP path must not invoke Claude/Superpowers implementation")
	}
	if !containsNodeName(*scheduled, "NoNewGaps") {
		t.Fatalf("scheduled/default analysis fallback must be guarded by NoNewGaps so implementation failures are not swallowed")
	}
	for _, name := range []string{"WriteFusionAnalysis", "VerifyGoapBuild", "RunGraphifyUpdate", "ReportFusionCycle"} {
		if !containsNodeName(*scheduled, name) {
			t.Fatalf("scheduled/default GOAP path missing %q", name)
		}
	}

	assertNoExecutePlanStubs(t, "goap_fusion", *tree)
}

func containsNodeName(node evolution.SerializableNode, name string) bool {
	if node.Name == name {
		return true
	}
	for _, child := range node.Children {
		if containsNodeName(child, name) {
			return true
		}
	}
	return false
}
