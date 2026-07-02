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

func TestMemSelector_ResumesAtRunningChildAndSkipsFailed(t *testing.T) {
	var failCalls, runCalls int
	RegisterAction("TestMemSelFail", func(_ *btcore.BTContext[Blackboard]) int {
		failCalls++
		return -1
	})
	ticks := 0
	RegisterAction("TestMemSelRun", func(_ *btcore.BTContext[Blackboard]) int {
		runCalls++
		ticks++
		if ticks < 2 {
			return 0
		}
		return 1
	})
	node := &evolution.SerializableNode{
		Type: "MemSelector", Name: "MemSelUnderTest",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "TestMemSelFail"},
			{Type: "Action", Name: "TestMemSelRun"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != 0 {
		t.Fatalf("tick1: want RUNNING, got %d", got)
	}
	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick2: want SUCCESS, got %d", got)
	}
	if failCalls != 1 {
		t.Fatalf("failed child re-ticked while RUNNING: %d calls, want 1", failCalls)
	}
	if _, ok := bb.ChainState["memsel/MemSelUnderTest"]; ok {
		t.Fatal("cursor must be cleared on completion")
	}
}

func TestMemSelector_AllChildrenFailClearsCursorAndFails(t *testing.T) {
	var aCalls, bCalls int
	RegisterAction("TestMemSelAllFailA", func(_ *btcore.BTContext[Blackboard]) int {
		aCalls++
		return -1
	})
	RegisterAction("TestMemSelAllFailB", func(_ *btcore.BTContext[Blackboard]) int {
		bCalls++
		return -1
	})
	node := &evolution.SerializableNode{
		Type: "MemSelector", Name: "MemSelAllFail",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "TestMemSelAllFailA"},
			{Type: "Action", Name: "TestMemSelAllFailB"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)

	if got := cmd.Run(ctx); got != -1 {
		t.Fatalf("want FAILURE, got %d", got)
	}
	if aCalls != 1 {
		t.Fatalf("child A should be called exactly once: got %d calls", aCalls)
	}
	if bCalls != 1 {
		t.Fatalf("child B should be called exactly once: got %d calls", bCalls)
	}
	if _, ok := bb.ChainState["memsel/MemSelAllFail"]; ok {
		t.Fatal("cursor must be cleared when all children fail")
	}
}

func TestPersistentMemSequence_ResumesFromPersistedCursor(t *testing.T) {
	var aCalls int
	registerCountingAction(t, "TestPMSA", 0, &aCalls)
	done := false
	RegisterAction("TestPMSB", func(_ *btcore.BTContext[Blackboard]) int {
		if done {
			return 1
		}
		return 0
	})
	node := &evolution.SerializableNode{
		Type: "PersistentMemSequence", Name: "PMSUnderTest",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "TestPMSA"},
			{Type: "Action", Name: "TestPMSB"},
		},
	}
	bb := newTestBlackboard()
	cmd := buildNode(node, bb, "")
	ctx := newTestBTContext(bb)
	if got := cmd.Run(ctx); got != 0 {
		t.Fatalf("tick1: want RUNNING, got %d", got)
	}

	// Simulate process restart + JSON round-trip: rebuild tree on a blackboard
	// whose cursor came back as float64.
	bb2 := newTestBlackboard()
	bb2.ChainState["memseq/PMSUnderTest"] = float64(1)
	cmd2 := buildNode(node, bb2, "")
	ctx2 := newTestBTContext(bb2)
	done = true
	if got := cmd2.Run(ctx2); got != 1 {
		t.Fatalf("resumed tick: want SUCCESS, got %d", got)
	}
	if aCalls != 1 {
		t.Fatalf("child A must not re-run after restart resume: %d calls", aCalls)
	}
}

func TestValidate_MemoryNodesRequireUniqueNames(t *testing.T) {
	root := &evolution.SerializableNode{
		Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "PersistentMemSequence", Name: "", Children: []evolution.SerializableNode{{Type: "AlwaysSucceed"}}},
		},
	}
	msgs := ValidateTree(root) // signature: func ValidateTree(*evolution.SerializableNode) []string (validate.go:7)
	if len(msgs) == 0 {
		t.Fatal("expected validation message for unnamed PersistentMemSequence")
	}
}
