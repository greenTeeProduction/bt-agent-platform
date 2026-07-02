package engine_test

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// buildNodeSupportedTypes mirrors the switch in engine.buildNode (tree.go).
// buildNode is unexported and, because internal/domains transitively imports
// internal/engine (via blocks/startup/thinktank), an internal `package engine`
// test cannot import domains without creating an import cycle. So this external
// test statically asserts every node type in the workflow tree is one buildNode
// handles — i.e. the tree never falls through to the "unsupported node type"
// default — which is the property the brief asks for.
var buildNodeSupportedTypes = map[string]bool{
	"Sequence": true, "Selector": true, "MemSequence": true, "MemSelector": true,
	"PersistentMemSequence": true, "CachedCondition": true, "SemaphoreGuard": true,
	"ForEachTask": true, "ReviewCycle": true, "Parallel": true, "Budget": true,
	"RateLimit": true, "Timeout": true, "CircuitBreaker": true, "Inverter": true,
	"Succeeder": true, "Repeater": true, "Runner": true, "Monitor": true,
	"QualityGate": true, "Retry": true, "Action": true, "ChainAction": true,
	"Condition": true, "UtilitySelector": true, "DecisionTree": true,
	"PlannerNode": true, "AbortOnEvent": true, "ReactiveParallel": true,
	"CheckpointVerifier": true, "HumanApprovalGate": true, "SubTreeRef": true,
	"AlwaysSucceed": true,
}

func TestSuperpowersWorkflowTree_BuildsAndValidates(t *testing.T) {
	tree := domains.SuperpowersWorkflowTree()

	// 1. Every node type must be one buildNode handles (no unsupported default).
	var walk func(n *evolution.SerializableNode)
	walk = func(n *evolution.SerializableNode) {
		if !buildNodeSupportedTypes[n.Type] {
			t.Errorf("node %q has unsupported node type %q", n.Name, n.Type)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(tree)

	// 2. Memory-node name validation must be clean. ValidateTree also reports
	// unknown Action/Condition names; the five Task-11 actions are legitimately
	// unregistered on this branch, so we only require zero memory-node/duplicate
	// messages (per the brief's adjustment).
	for _, msg := range engine.ValidateTree(tree) {
		if strings.Contains(msg, "duplicate") || strings.Contains(msg, "memory node") {
			t.Errorf("unexpected memory-node validation message: %s", msg)
		}
	}
}
