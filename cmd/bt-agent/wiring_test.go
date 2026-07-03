package main

import (
	"testing"
)

// TestDaemonResolvesWiredGoapFusionLoopTree pins that THE DAEMON BINARY —
// whatever its import graph looks like in the future — resolves the
// scheduled goap_fusion_loop tree with production wiring applied. Today the
// wiring arrives via internal/agentexec's init (linked through tools.go);
// if that import is ever dropped, this test fails instead of the scheduled
// loop silently running unwired again (no preflight, no circuit gate, empty
// CIRCUITPOLICY state-hash history → breaker always answers CONTINUE).
func TestDaemonResolvesWiredGoapFusionLoopTree(t *testing.T) {
	tree := resolveTree("domain:goap_fusion_loop")
	if tree == nil {
		t.Fatal("domain:goap_fusion_loop did not resolve")
	}
	if len(tree.Children) == 0 || tree.Children[0].Name != "GoapFusionPreflight" {
		t.Fatalf("daemon must resolve the WIRED goap_fusion_loop tree (preflight first); first child = %q", tree.Children[0].Name)
	}
}
