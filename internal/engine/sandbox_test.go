package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// TestSandboxBlocksRealActions verifies that a Blackboard in sandbox mode never
// invokes registered action implementations — the benchmark harness must not
// trigger real side effects (subprocesses, network calls, quota consumption).
func TestSandboxBlocksRealActions(t *testing.T) {
	executed := false
	RegisterAction("SandboxTestSideEffect", func(_ *btcore.BTContext[Blackboard]) int {
		executed = true
		return 1
	})

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "SandboxTestSideEffect"},
		},
	}

	bb := &Blackboard{Task: "sandbox test", Sandbox: true}
	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if executed {
		t.Fatal("sandbox mode executed a real registered action")
	}
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("sandboxed run outcome = %q, want success", bb.Outcome)
	}
}

// TestSandboxOffExecutesRealActions verifies normal (non-sandbox) execution
// still dispatches to the registered implementation.
func TestSandboxOffExecutesRealActions(t *testing.T) {
	executed := false
	RegisterAction("SandboxTestRealDispatch", func(_ *btcore.BTContext[Blackboard]) int {
		executed = true
		return 1
	})

	tree := &evolution.SerializableNode{
		Type: "Sequence",
		Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "SandboxTestRealDispatch"},
		},
	}

	bb := &Blackboard{Task: "real dispatch test"}
	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	if !executed {
		t.Fatal("non-sandbox mode did not execute the registered action")
	}
}
