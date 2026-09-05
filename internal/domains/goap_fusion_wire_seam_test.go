package domains

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// The wire seam lets a higher layer (internal/agent) wrap the raw
// goap_fusion_loop tree with engine production wiring without domains
// importing engine (domains' in-package tests import engine, so that
// import would be a test-build cycle).

func TestGoapFusionLoopWireSeamDefaultsToIdentity(t *testing.T) {
	// Evolution/gardener tooling operates on the raw tree: without an
	// installed hook the constructor must return the unwrapped tree.
	tree := GoapFusionLoopTree()
	if tree == nil || tree.Name != "GoapFusionLoop_Main" {
		t.Fatalf("raw tree root = %+v, want GoapFusionLoop_Main", tree)
	}
	if len(tree.Children) > 0 && tree.Children[0].Name == "GoapFusionPreflight" {
		t.Fatal("identity default must not wire the preflight; the hook is for higher layers")
	}
}

func TestGoapFusionLoopTreeAppliesInstalledWireHook(t *testing.T) {
	old := GoapFusionLoopWireFn
	t.Cleanup(func() { GoapFusionLoopWireFn = old })
	GoapFusionLoopWireFn = func(tree evolution.SerializableNode) evolution.SerializableNode {
		return evolution.SerializableNode{
			Type:     "Sequence",
			Name:     "WiredByHook",
			Children: []evolution.SerializableNode{tree},
		}
	}
	tree := GoapFusionLoopTree()
	if tree.Name != "WiredByHook" {
		t.Fatalf("constructor must return the hook-wrapped tree, got root %q", tree.Name)
	}
	if len(tree.Children) != 1 || tree.Children[0].Name != "GoapFusionLoop_Main" {
		t.Fatal("hook must receive the raw tree as input")
	}
}
