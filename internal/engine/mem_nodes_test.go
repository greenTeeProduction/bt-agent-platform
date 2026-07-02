package engine

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// Shared test helpers for all new-node tests (Tasks 1-8).
// btcore.NewBTContext initializes MemSequenceState (go-bt core, line 37).
func newTestBlackboard() *Blackboard {
	return &Blackboard{ChainState: map[string]any{}}
}

func newTestBTContext(bb *Blackboard) *btcore.BTContext[Blackboard] {
	return btcore.NewBTContext(context.Background(), bb)
}

// countingAction returns RUNNING the first `runningTicks` calls, then SUCCESS,
// and counts invocations — used to prove MemSequence skips completed children.
func registerCountingAction(t *testing.T, name string, runningTicks int, calls *int) {
	t.Helper()
	ticks := 0
	RegisterAction(name, func(_ *btcore.BTContext[Blackboard]) int {
		*calls++
		ticks++
		if ticks <= runningTicks {
			return 0
		}
		return 1
	})
}

func TestMemSequence_DoesNotRetickCompletedChildren(t *testing.T) {
	var aCalls, bCalls int
	registerCountingAction(t, "TestMemSeqA", 0, &aCalls) // succeeds immediately
	registerCountingAction(t, "TestMemSeqB", 2, &bCalls) // RUNNING twice, then SUCCESS

	node := &evolution.SerializableNode{
		Type: "MemSequence", Name: "MemSeqUnderTest",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "TestMemSeqA"},
			{Type: "Action", Name: "TestMemSeqB"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != 0 {
		t.Fatalf("tick1: want RUNNING, got %d", got)
	}
	if got := cmd.Run(ctx); got != 0 {
		t.Fatalf("tick2: want RUNNING, got %d", got)
	}
	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick3: want SUCCESS, got %d", got)
	}
	if aCalls != 1 {
		t.Fatalf("child A re-ticked: %d calls, want 1", aCalls)
	}
}
