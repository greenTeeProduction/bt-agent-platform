package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
)

// TestGoapFusion_Structure is a structural smoke test for GoapFusionTree.
//
// It deliberately does NOT run the tree end-to-end: the ClaudePath reaches
// ApplyImprovementWithClaude, which launches Claude Code and is neither
// deterministic nor test-safe (see domains_test.go TestAllDomainTrees, which
// also restricts goap_fusion to structural smoke only). Instead we verify the
// tree builds without panicking, has the expected GOAP node shape, and that
// every condition and action it references is registered in the engine.
func TestGoapFusion_Structure(t *testing.T) {
	bb := &engine.Blackboard{
		Task: "analyze research gaps and apply the highest-priority improvement: implement one concrete fix",
		LLM:  &engine.MockLLM{},
	}

	// 1. BuildTree must not panic and must return a runnable command, for both
	//    the plain tree and the checkpoint-verifier-wrapped variant.
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

	// 2. Verify the GOAP node shape: deterministic research phase followed by a
	//    router that picks the Claude implementation path or the agent fallback.
	requiredNodes := []string{
		"PreGate",
		"ImplementationRouter",
		"ClaudePath",
		"ExecutionPath",
	}
	for _, name := range requiredNodes {
		if findNode(*tree, name) == nil {
			t.Errorf("GoapFusionTree missing expected node %q", name)
		}
	}

	// 3. Every condition the tree references must be registered in the engine.
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

	// 4. Every action node the tree references must be registered in the engine.
	requiredActions := []string{
		"SetupFusionTools",
		"ReadVaultResearch",
		"ReadGraphifyReport",
		"AnalyzeImprovementGaps",
		"PrioritizeGoapGoals",
		"ReadImprovementPlan",
		"ApplyImprovementWithClaude",
		"VerifyGoapBuild",
		"ReflectOnOutcome",
		"UpdateBehaviorTree",
	}
	for _, name := range requiredActions {
		if engine.GetAction(name) == nil {
			t.Errorf("action %q not registered in engine", name)
		}
	}

	// 5. Sanity: the tree must not contain deprecated stub nodes.
	assertNoExecutePlanStubs(t, "goap_fusion", *tree)
}
