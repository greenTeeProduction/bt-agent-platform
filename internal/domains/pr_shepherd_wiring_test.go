package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestGoapFusionTreesIncludePRShepherd pins the PR pipeline shepherd wiring
// (spec docs/superpowers/specs/2026-07-22-pr-pipeline-shepherd-design.md):
// both goap fusion trees must carry the ShepherdFleetPR node — placed BEFORE
// the research/implementation phases so upstream merges are adopted and the
// landing PR is driven (open → fix red CI → merge green) at every cycle — and
// the action must be registered, or the node silently resolves to the
// engine's permissive unknown-action fallback (the AuctionDemoTree trap).
func TestGoapFusionTreesIncludePRShepherd(t *testing.T) {
	if engine.GetAction("ShepherdFleetPR") == nil {
		t.Fatal("ShepherdFleetPR is not registered in the engine")
	}
	for name, tree := range map[string]*evolution.SerializableNode{
		"goap_fusion":      GoapFusionTree(true),
		"goap_fusion_loop": GoapFusionLoopTree(),
	} {
		node := findNode(*tree, "ShepherdFleetPR")
		if node == nil {
			t.Errorf("tree %q is missing the ShepherdFleetPR node", name)
			continue
		}
		if node.Description == "" {
			t.Errorf("tree %q: ShepherdFleetPR node has no description", name)
		}
	}
}
