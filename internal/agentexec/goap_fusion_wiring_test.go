package agentexec

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/domains"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

func treeContainsNode(n *evolution.SerializableNode, name string) bool {
	if n == nil {
		return false
	}
	if n.Name == name {
		return true
	}
	for i := range n.Children {
		if treeContainsNode(&n.Children[i], name) {
			return true
		}
	}
	return false
}

// TestGoapFusionLoopTreeIsProductionWired pins the P0 gap the self-improvement
// loop re-discovered every cycle for days but could never close itself (its
// planner scoped edits to internal/engine only, and no layer between domains
// and here can import both domains and engine without an import cycle): any
// binary that links agentexec — bt-agent, bt-agent-cli, bt-dashboard — must
// resolve domain:goap_fusion_loop to the WIRED tree: Phase-0 preflight
// prepended, ClaudeSuperpowersPath gated, and the PublishGoapFusionStateHash
// producer spliced after PrioritizeGoapGoals so the CIRCUITPOLICY state-hash
// history is actually written in real cycles (an empty history makes the
// circuit breaker always answer CONTINUE — the exact "Activity-Progress
// Confusion" it exists to break).
func TestGoapFusionLoopTreeIsProductionWired(t *testing.T) {
	tree := domains.ResolveTreeID("domain:goap_fusion_loop")
	if tree == nil {
		t.Fatal("domain:goap_fusion_loop did not resolve")
	}
	for _, marker := range []string{"GoapFusionPreflight", "PublishGoapFusionStateHash"} {
		if !treeContainsNode(tree, marker) {
			t.Fatalf("production goap_fusion_loop tree is missing wired node %q — WireGoapFusionLoopTree is not applied on the production resolution path", marker)
		}
	}
	// Every catalog tree root is wrapped in a ClaudeErrorHandler decorator
	// (internal/domains/trees.go wrapWithErrorHandler); the previously-root
	// wired Sequence is now tree.Children[0], so the Phase-0 preflight check
	// descends one level.
	if len(tree.Children) == 0 {
		t.Fatalf("wired tree has no children (want ClaudeErrorHandler wrapper around the wired Sequence)")
	}
	inner := tree.Children[0]
	if len(inner.Children) == 0 {
		t.Fatal("wired tree must start with the Phase-0 preflight; wired Sequence has no children")
	}
	if inner.Children[0].Name != "GoapFusionPreflight" {
		t.Fatalf("wired tree must start with the Phase-0 preflight, first child = %q", inner.Children[0].Name)
	}
}
