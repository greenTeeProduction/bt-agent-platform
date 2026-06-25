package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestGoapFusion_Structure is a structural smoke test for GoapFusionTree.
// It deliberately does NOT execute the Claude path. It verifies the production
// dual-mode shape: both paths use auto-approved HumanApprovalGate for code changes.
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
		"ApproveScheduledGoapApply",
	}
	for _, name := range requiredNodes {
		if findNode(*tree, name) == nil {
			t.Errorf("GoapFusionTree missing expected node %q", name)
		}
	}
	if containsNodeType(*tree, "ChainAgent") {
		t.Fatalf("GoapFusionTree must not contain ChainAgent fallback")
	}
	if !containsNodeType(*tree, "HumanApprovalGate") {
		t.Fatalf("explicit GOAP apply path must use HumanApprovalGate")
	}

	requiredConditions := []string{
		"ValidateInput",
		"IsFusionTask",
		"IsApplyRequest",
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
		"ReportSuperpowersImplementation",
		"WriteFusionAnalysis",
		"ReportFusionCycle",
		"ReflectOnOutcome",
		"UpdateBehaviorTree",
	}
	for _, name := range requiredActions {
		if engine.GetAction(name) == nil {
			t.Errorf("action %q not registered in engine", name)
		}
	}

	assertNoExecutePlanStubs(t, "goap_fusion", *tree)
}
