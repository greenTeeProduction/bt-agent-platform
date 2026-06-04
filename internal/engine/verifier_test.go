package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestVerifierFull_ValidTree(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "TestTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "GeneratePlan"},
			{Type: "Condition", Name: "ValidateInput"},
		},
	}

	info := ValidateTreeFull(tree)
	if !info.Valid() {
		t.Errorf("expected valid tree, got errors: %v", info.Errors)
	}
}

func TestVerifierFull_UnknownNodeType(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "UnknownType", Name: "BadTree"}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "unknown node type")
}

func TestVerifierFull_UnknownActionName(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Action", Name: "DefinitelyNotRegistered"}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "unknown action")
}

func TestVerifierFull_UnknownConditionName(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Condition", Name: "DefinitelyNotACondition"}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "unknown condition")
}

func TestVerifierFull_DepthExceeded(t *testing.T) {
	tree := buildDeepTree(20)
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "max depth")
}

func TestVerifierFull_UnboundedRetry(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:     "Retry",
		Name:     "RetryTree",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "GeneratePlan"}},
	}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "requires max_retries > 0")
}

func TestVerifierFull_MaxRetriesExceeded(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:       "Retry",
		Name:       "RetryTree",
		MaxRetries: 100,
		Children:   []evolution.SerializableNode{{Type: "Action", Name: "GeneratePlan"}},
	}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "max retries")
}

func TestVerifierFull_DestructiveActionRequiresApprovalGate(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Action",
		Name: "GeneratePlan",
		Metadata: map[string]any{
			"side_effect_class": "destroy",
		},
	}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "requires HumanApprovalGate")
}

func TestVerifierFull_DestructiveActionWithApprovalGate(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "HumanApprovalGate",
		Name: "Approval",
		Children: []evolution.SerializableNode{{
			Type: "Action",
			Name: "GeneratePlan",
			Metadata: map[string]any{
				"side_effect_class": "destroy",
			},
		}},
	}
	info := ValidateTreeFull(tree)
	if !info.Valid() {
		t.Fatalf("expected approval-gated destructive action to be valid, got %v", info.Errors)
	}
}

func TestVerifierFull_GuardEdgeWithoutCondition(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "GuardTree",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "GeneratePlan"}},
		Edges:    []evolution.TypedEdge{{Type: evolution.EdgeGuard, ChildIndex: 0}},
	}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "guard edges must have a condition")
}

func TestVerifierFull_EffectEdgeWithoutEffect(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "EffectTree",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "GeneratePlan"}},
		Edges:    []evolution.TypedEdge{{Type: evolution.EdgeEffect, ChildIndex: 0}},
	}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "effect edges must have an effect")
}

func TestVerifierFull_BackwardCompat(t *testing.T) {
	// Tree with no Edges field should work (backward compat).
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "CompatTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "GeneratePlan"},
			{Type: "Selector", Name: "Sel1", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "ValidateInput"},
			}},
		},
	}
	info := ValidateTreeFull(tree)
	if !info.Valid() {
		t.Errorf("backward compat tree should be valid, got errors: %v", info.Errors)
	}
}

func TestVerifierFull_EdgeChildIndexOutOfRange(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "EdgeTree",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "GeneratePlan"}},
		Edges:    []evolution.TypedEdge{{Type: evolution.EdgeChild, ChildIndex: 5}},
	}
	info := ValidateTreeFull(tree)
	assertInvalidContains(t, info, "child_index out of range")
}

func TestVerifierFull_KnownNodeTypes(t *testing.T) {
	types := []string{
		"Sequence", "Selector", "Parallel", "Retry", "RateLimit", "Timeout",
		"Budget", "CircuitBreaker", "Inverter", "Succeeder", "Repeater",
		"Runner", "Action", "Condition", "ChainAction", "StrategyRouter",
		"Monitor", "UtilitySelector", "PlannerNode", "ReactiveParallel",
		"AbortOnEvent", "HumanApprovalGate", "QualityGate",
	}

	for _, nodeType := range types {
		tree := &evolution.SerializableNode{Type: nodeType, Name: nodeType + "Node"}
		if nodeType == "Action" {
			tree.Name = "GeneratePlan"
		}
		if nodeType == "Condition" {
			tree.Name = "ValidateInput"
		}
		if nodeType == "Retry" || nodeType == "Repeater" {
			tree.MaxRetries = 1
			tree.Children = []evolution.SerializableNode{{Type: "Action", Name: "GeneratePlan"}}
		}
		info := ValidateTreeFull(tree)
		if !info.Valid() {
			t.Errorf("node type %q should be known, got errors: %v", nodeType, info.Errors)
		}
	}
}

func TestBuildTree_InvalidTreeReturnsFailureCommand(t *testing.T) {
	bb := &Blackboard{}
	cmd := BuildTree(&evolution.SerializableNode{Type: "Action", Name: "MissingAction"}, bb)
	if cmd == nil {
		t.Fatal("expected non-nil failure command")
	}
	outcome := RunTask(bb, cmd)
	if !strings.Contains(outcome, "tree validation failed") {
		t.Fatalf("expected validation failure outcome, got %q", outcome)
	}
}

func assertInvalidContains(t *testing.T, info *evolution.NodeValidationInfo, want string) {
	t.Helper()
	if info.Valid() {
		t.Fatalf("expected invalid tree containing %q", want)
	}
	for _, err := range info.Errors {
		if strings.Contains(err, want) {
			return
		}
	}
	t.Fatalf("expected error containing %q, got %v", want, info.Errors)
}

func buildDeepTree(depth int) *evolution.SerializableNode {
	if depth <= 0 {
		return &evolution.SerializableNode{Type: "Action", Name: fmt.Sprintf("LeafNode_%d", depth)}
	}
	return &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     fmt.Sprintf("DeepNode_L%d", depth),
		Children: []evolution.SerializableNode{*buildDeepTree(depth - 1)},
	}
}

// --- Cycle detection tests (Plan #1: NotebookLM CRITICAL gap) ---

func TestValidateTree_NoCycle(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "HealthCheckAgent"},
			{Type: "Action", Name: "GeneratePlan"},
		},
	}
	info := ValidateTreeFull(tree)
	if !info.Valid() {
		t.Fatalf("tree with unique node names should be valid, got: %v", info.Errors)
	}
}

func TestValidateTree_CycleDetected(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "Root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "StepA"},
			{Type: "Sequence", Name: "Root", // same name as ancestor → cycle
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "StepB"},
				},
			},
		},
	}
	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Fatal("expected cycle detection error, but tree was considered valid")
	}
	found := false
	for _, e := range info.Errors {
		if strings.Contains(e, "cycle detected") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'cycle detected' error, got: %v", info.Errors)
	}
}

func TestValidateTree_CycleDeep(t *testing.T) {
	// Cycle at depth 4: Root → A → B → Root
	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "DeepRoot",
		Children: []evolution.SerializableNode{
			{Type: "Sequence", Name: "Level1",
				Children: []evolution.SerializableNode{
					{Type: "Sequence", Name: "Level2",
						Children: []evolution.SerializableNode{
							{Type: "Sequence", Name: "DeepRoot", // cycle back to root
								Children: []evolution.SerializableNode{
									{Type: "Action", Name: "StepX"},
								},
							},
						},
					},
				},
			},
		},
	}
	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Fatal("expected cycle detection at depth 4, but tree was considered valid")
	}
	found := false
	for _, e := range info.Errors {
		if strings.Contains(e, "cycle detected") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'cycle detected' error, got: %v", info.Errors)
	}
}
