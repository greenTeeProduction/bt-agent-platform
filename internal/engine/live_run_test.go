package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestLiveRunRegistryLifecycle(t *testing.T) {
	bb := &Blackboard{RunID: "run-lifecycle-1"}
	lr := registerLiveRun(bb, LiveRunInfo{Agent: "agentX", TreeID: "treeY"})
	defer deregisterLiveRun(lr.runID)
	if bb.liveRun != lr {
		t.Fatal("registerLiveRun must attach the run to the blackboard")
	}
	found := false
	for _, s := range ListLiveRuns() {
		if s.RunID == "run-lifecycle-1" && s.Agent == "agentX" && s.TreeID == "treeY" {
			found = true
		}
	}
	if !found {
		t.Fatal("registered run must be listed")
	}
	deregisterLiveRun(lr.runID)
	if _, err := EnqueueLiveMutation("run-lifecycle-1", MutationOp{Kind: "remove", Path: "0"}); err == nil {
		t.Fatal("enqueue to a deregistered run must error")
	}
}

func TestLiveRunEnqueueAndDrain(t *testing.T) {
	bb := &Blackboard{RunID: "run-q-1"}
	lr := registerLiveRun(bb, LiveRunInfo{})
	defer deregisterLiveRun(lr.runID)
	id1, err := bb.EnqueueMutation(MutationOp{Kind: "remove", Path: "0"})
	if err != nil || id1 == "" {
		t.Fatalf("bb enqueue: id=%q err=%v", id1, err)
	}
	id2, err := EnqueueLiveMutation("run-q-1", MutationOp{Kind: "remove", Path: "1"})
	if err != nil || id2 == id1 {
		t.Fatalf("registry enqueue: id=%q err=%v", id2, err)
	}
	ops := lr.drain()
	if len(ops) != 2 || ops[0].id != id1 || ops[1].id != id2 {
		t.Fatalf("drain must return queued ops in order, got %+v", ops)
	}
	if len(lr.drain()) != 0 {
		t.Fatal("second drain must be empty")
	}
}

func TestLiveRunOpCap(t *testing.T) {
	bb := &Blackboard{RunID: "run-cap-1"}
	lr := registerLiveRun(bb, LiveRunInfo{})
	defer deregisterLiveRun(lr.runID)
	for i := 0; i < maxMutationsPerRun; i++ {
		if _, err := lr.enqueue(MutationOp{Kind: "remove", Path: "0"}); err != nil {
			t.Fatalf("enqueue %d unexpectedly failed: %v", i, err)
		}
	}
	if _, err := lr.enqueue(MutationOp{Kind: "remove", Path: "0"}); err == nil {
		t.Fatal("enqueue beyond maxMutationsPerRun must error")
	}
}

func TestEnqueueWithoutLiveRun(t *testing.T) {
	bb := &Blackboard{}
	if _, err := bb.EnqueueMutation(MutationOp{Kind: "remove", Path: "0"}); err == nil {
		t.Fatal("EnqueueMutation on a non-mutable run must error")
	}
}

func TestBuildNodeCapture(t *testing.T) {
	ser := &evolution.SerializableNode{Type: "MemSequence", Name: "memroot",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "s1"},
			{Type: "Action", Name: "s2"},
		}}
	bb := &Blackboard{buildCapture: map[*evolution.SerializableNode]btcore.Command[Blackboard]{}}
	cmd := buildNode(ser, bb, "")
	if cmd == nil {
		t.Fatal("buildNode returned nil")
	}
	if len(bb.buildCapture) != 3 {
		t.Fatalf("capture must record every built node, got %d entries", len(bb.buildCapture))
	}
	// The captured command must be the INNER command — the pointer the
	// library keys MemSequenceState by — not the observeNode wrapper that
	// buildNode returns.
	if bb.buildCapture[ser] == cmd {
		t.Fatal("captured command must be the inner command, not the observeNode wrapper")
	}
	if bb.buildCapture[&ser.Children[0]] == nil || bb.buildCapture[&ser.Children[1]] == nil {
		t.Fatal("children must be captured by their in-tree addresses")
	}
}
