package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestVerifierFull_ValidTree(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "TestTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "DoSomething"},
			{Type: "Condition", Name: "CheckSomething"},
		},
	}

	info := ValidateTreeFull(tree)
	if !info.Valid() {
		t.Errorf("expected valid tree, got errors: %v", info.Errors)
	}
}

func TestVerifierFull_UnknownNodeType(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "UnknownType",
		Name: "BadTree",
	}

	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Error("expected invalid tree for unknown node type")
	}
	expectedErr := "unknown node type: UnknownType"
	found := false
	for _, err := range info.Errors {
		if err == expectedErr {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error %q, got %v", expectedErr, info.Errors)
	}
}

func TestVerifierFull_DepthExceeded(t *testing.T) {
	// Build a deep tree (depth > 16)
	tree := buildDeepTree(20)
	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Error("expected invalid tree for depth > 16")
	}
}

func TestVerifierFull_UnboundedRetry(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type:       "Sequence",
		Name:       "RetryTree",
		MaxRetries: 100, // exceeds limit
	}

	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Error("expected invalid tree for unbounded retry")
	}
}

func TestVerifierFull_GuardEdgeWithoutCondition(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "GuardTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Action1"},
		},
		Edges: []evolution.TypedEdge{
			{Type: evolution.EdgeGuard, ChildIndex: 0}, // missing condition
		},
	}

	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Error("expected invalid tree for guard edge without condition")
	}
}

func TestVerifierFull_EffectEdgeWithoutEffect(t *testing.T) {
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "EffectTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Action1"},
		},
		Edges: []evolution.TypedEdge{
			{Type: evolution.EdgeEffect, ChildIndex: 0}, // missing effect
		},
	}

	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Error("expected invalid tree for effect edge without effect")
	}
}

func TestVerifierFull_BackwardCompat(t *testing.T) {
	// Tree with no Edges field should work (backward compat)
	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "CompatTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Action1"},
			{Type: "Selector", Name: "Sel1", Children: []evolution.SerializableNode{
				{Type: "Condition", Name: "Cond1"},
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
		Type: "Sequence",
		Name: "EdgeTree",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "Action1"},
		},
		Edges: []evolution.TypedEdge{
			{Type: evolution.EdgeChild, ChildIndex: 5}, // out of range
		},
	}

	info := ValidateTreeFull(tree)
	if info.Valid() {
		t.Error("expected invalid tree for out-of-range child index")
	}
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
		info := ValidateTreeFull(tree)
		if !info.Valid() {
			t.Errorf("node type %q should be known, got errors: %v", nodeType, info.Errors)
		}
	}
}

// buildDeepTree creates a tree with the given depth.
func buildDeepTree(depth int) *evolution.SerializableNode {
	if depth <= 0 {
		return &evolution.SerializableNode{Type: "Action", Name: "Leaf"}
	}
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "DeepNode",
		Children: []evolution.SerializableNode{
			*buildDeepTree(depth - 1),
		},
	}
}
