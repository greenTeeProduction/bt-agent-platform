// Package engine provides the behavior tree runtime for the BT platform.
package engine

import "testing"

func TestChainStateInt_ReadsIntAndFloat64(t *testing.T) {
	bb := newTestBlackboard()
	bb.ChainState["a"] = 3
	bb.ChainState["b"] = float64(7) // JSON round-trip shape
	if v, ok := chainStateInt(bb, "a"); !ok || v != 3 {
		t.Fatalf("int read: got %v %v", v, ok)
	}
	if v, ok := chainStateInt(bb, "b"); !ok || v != 7 {
		t.Fatalf("float64 read: got %v %v", v, ok)
	}
	if _, ok := chainStateInt(bb, "missing"); ok {
		t.Fatal("missing key must return ok=false")
	}
}
