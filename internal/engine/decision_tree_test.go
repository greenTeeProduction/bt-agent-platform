package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestDecisionTreeRoutesByChainStateMatch(t *testing.T) {
	registerDecisionTreeAction(t, "DecisionTreeTestCodePath", "code path selected")
	registerDecisionTreeAction(t, "DecisionTreeTestResearchPath", "research path selected")

	bb := &Blackboard{ChainState: map[string]any{"route": "code"}}
	tree := &evolution.SerializableNode{
		Type: "DecisionTree",
		Name: "TaskRouter",
		Metadata: map[string]any{
			"key": "route",
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "DecisionTreeTestResearchPath", Metadata: map[string]any{"match": "research"}},
			{Type: "Action", Name: "DecisionTreeTestCodePath", Metadata: map[string]any{"match": "code"}},
		},
	}

	bt := BuildTree(tree, bb)
	result := RunTask(bb, bt)

	if result != "code path selected" {
		t.Fatalf("expected code branch result, got %q", result)
	}
	if bb.Outcome != "success" {
		t.Fatalf("expected success, got %s", bb.Outcome)
	}
	if bb.CurrentPath != "TaskRouter/DecisionTreeTestCodePath" {
		t.Fatalf("expected CurrentPath to record selected decision path, got %q", bb.CurrentPath)
	}
}

func TestDecisionTreeValidatesAsKnownNodeType(t *testing.T) {
	registerDecisionTreeAction(t, "DecisionTreeTestValidatedPath", "validated")
	tree := &evolution.SerializableNode{
		Type: "DecisionTree",
		Name: "ValidationRouter",
		Metadata: map[string]any{
			"key": "route",
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "DecisionTreeTestValidatedPath", Metadata: map[string]any{"match": "validated"}},
		},
	}

	info := ValidateTreeFull(tree)
	if !info.Valid() {
		t.Fatalf("DecisionTree should be a valid node type, got errors: %v", info.Errors)
	}
}

func TestDecisionTreeUsesDefaultBranchWhenNoMatch(t *testing.T) {
	registerDecisionTreeAction(t, "DecisionTreeTestPrimaryPath", "primary selected")
	registerDecisionTreeAction(t, "DecisionTreeTestFallbackPath", "fallback selected")

	bb := &Blackboard{ChainState: map[string]any{"route": "unknown"}}
	tree := &evolution.SerializableNode{
		Type: "DecisionTree",
		Name: "TaskRouterWithDefault",
		Metadata: map[string]any{
			"key":     "route",
			"default": "fallback",
		},
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "DecisionTreeTestPrimaryPath", Metadata: map[string]any{"match": "primary"}},
			{Type: "Action", Name: "DecisionTreeTestFallbackPath", Metadata: map[string]any{"branch": "fallback"}},
		},
	}

	bt := BuildTree(tree, bb)
	result := RunTask(bb, bt)

	if result != "fallback selected" {
		t.Fatalf("expected fallback branch result, got %q", result)
	}
}

func registerDecisionTreeAction(t *testing.T, name, result string) {
	t.Helper()
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := actionRegistry[name]; exists {
		return
	}
	actionRegistry[name] = func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = result
		return 1
	}
}
