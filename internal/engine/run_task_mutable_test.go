package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// Deterministic external-mutation harness — NO timing races:
//  1. The tree contains a muttest_gate action that returns Running until a
//     channel placed in ChainState["gate_ch"] before the run is closed.
//  2. A helper goroutine waits for the run to appear in the registry,
//     enqueues the op, then POLLS THE JOURNAL until the op is applied or
//     rejected — only then closes the gate channel.
//  3. The gate sees the closed channel on its next tick, which is strictly
//     after the boundary that applied the op. No sleeps, no tick counting.
func init() {
	RegisterAction("muttest_mark_a", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "a")
		return 1
	})
	RegisterAction("muttest_mark_b", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "b")
		return 1
	})
	RegisterAction("muttest_grafted", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "grafted")
		ctx.Blackboard.Result = "grafted node executed in the same run and did a bunch of useful work here"
		return 1
	})
	// Grafts a sibling action after itself on first execution, then returns
	// Running once so the next tick boundary applies the graft.
	RegisterAction("muttest_self_graft", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		if b.ChainState["grafted"] != true {
			b.ChainState["grafted"] = true
			_, err := b.EnqueueMutation(MutationOp{
				Kind: "add", ParentPath: "", Index: -1, Origin: OriginTree,
				Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_grafted"},
			})
			if err != nil {
				b.Result = "enqueue failed: " + err.Error()
				return -1
			}
			return 0 // Running → RunTask loops → boundary applies the graft
		}
		return 1
	})
	// Running until ChainState["gate_ch"] (chan struct{}) is closed.
	RegisterAction("muttest_gate", func(ctx *btcore.BTContext[Blackboard]) int {
		ch, _ := ctx.Blackboard.ChainState["gate_ch"].(chan struct{})
		if ch == nil {
			return -1 // misconfigured test
		}
		select {
		case <-ch:
			return 1
		default:
			return 0
		}
	})
}

func muttestMark(b *Blackboard, s string) {
	// Actions run only on the run goroutine in these tests — no lock needed.
	b.Results = append(b.Results, "mark:"+s)
}

func marksOf(b *Blackboard) []string {
	var out []string
	for _, r := range b.Results {
		if strings.HasPrefix(r, "mark:") {
			out = append(out, strings.TrimPrefix(r, "mark:"))
		}
	}
	return out
}

// enqueueWhenLiveThenRelease finds the run by treeID, enqueues op, polls the
// journal until the op lands (applied or rejected), then closes gate. The
// t.Deadline-free 10s cap only guards a hung engine — the happy path never
// waits on wall time.
func enqueueWhenLiveThenRelease(t *testing.T, treeID string, op MutationOp, gate chan struct{}) chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(gate)
		deadline := time.Now().Add(10 * time.Second)
		var runID, opID string
		for time.Now().Before(deadline) && opID == "" {
			for _, s := range ListLiveRuns() {
				if s.TreeID == treeID {
					runID = s.RunID
					id, err := EnqueueLiveMutation(runID, op)
					if err != nil {
						t.Errorf("enqueue: %v", err)
						return
					}
					opID = id
				}
			}
		}
		for time.Now().Before(deadline) {
			recs, err := LiveMutationJournal(runID)
			if err != nil {
				return // run ended already; gate close is a no-op then
			}
			for _, r := range recs {
				if r.OpID == opID {
					return // applied or rejected — release the gate
				}
			}
		}
		t.Error("op never appeared in the journal within 10s")
	}()
	return done
}

func newGateBB(task string) (*Blackboard, chan struct{}) {
	gate := make(chan struct{})
	return &Blackboard{Task: task, ChainState: map[string]any{"gate_ch": gate}}, gate
}

func TestRunTaskMutableGraftExecutesSameRun(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "muttest_self_graft"}}}
	bb := &Blackboard{Task: "graft test"}
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "graftcase"}); err != nil {
		t.Fatal(err)
	}
	marks := marksOf(bb)
	if len(marks) == 0 || marks[len(marks)-1] != "grafted" {
		t.Fatalf("grafted action must execute within the same run, marks=%v outcome=%s result=%q", marks, bb.Outcome, bb.Result)
	}
	if _, err := LiveMutationJournal(bb.RunID); err == nil {
		t.Fatal("run must deregister from the live-run registry after completion")
	}
	if len(tree.Children) != 1 {
		t.Fatal("caller-owned tree must stay untouched; run mutates a private clone")
	}
}

func TestRunTaskMutableRemoveSkipsBranch(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "muttest_gate"},   // Running until removal applied
			{Type: "Action", Name: "muttest_mark_b"}, // removed before it can run
			{Type: "Action", Name: "muttest_mark_a"},
		}}
	bb, gate := newGateBB("remove test")
	done := enqueueWhenLiveThenRelease(t, "removecase",
		MutationOp{Kind: "remove", Path: "1", ExpectName: "muttest_mark_b", Origin: OriginOperator}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "removecase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	marks := marksOf(bb)
	for _, m := range marks {
		if m == "b" {
			t.Fatalf("removed branch must not execute, marks=%v", marks)
		}
	}
	if len(marks) == 0 || marks[len(marks)-1] != "a" {
		t.Fatalf("surviving sibling must still execute, marks=%v", marks)
	}
}

func TestRunTaskMutableMemSequenceKeepsPlace(t *testing.T) {
	// Load-bearing migration case: a MemSequence completes side-effectful
	// step a, then blocks in the gate. An unrelated graft lands at ROOT while
	// the cursor sits at the gate. After the rebuild the MemSequence must NOT
	// re-run step a, and the grafted node must run.
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "MemSequence", Name: "memphase", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "muttest_mark_a"},
				{Type: "Action", Name: "muttest_gate"},
			}},
		}}
	bb, gate := newGateBB("memseq migration")
	done := enqueueWhenLiveThenRelease(t, "memcase",
		MutationOp{Kind: "add", ParentPath: "", Index: -1, Origin: OriginOperator,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_mark_b"}}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "memcase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	countA, sawB := 0, false
	for _, m := range marksOf(bb) {
		if m == "a" {
			countA++
		}
		if m == "b" {
			sawB = true
		}
	}
	if countA != 1 {
		t.Fatalf("MemSequence must keep its cursor across an unrelated graft; step a ran %d times (marks=%v)", countA, marksOf(bb))
	}
	if !sawB {
		t.Fatalf("grafted root child must execute after the memseq completes, marks=%v", marksOf(bb))
	}
}

func TestRunTaskMutableRejectionKeepsRunHealthy(t *testing.T) {
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "muttest_gate"},
			{Type: "Action", Name: "muttest_mark_a"},
		}}
	bb, gate := newGateBB("reject test")
	// This test's actions only ever write short "mark:x" markers (never
	// bb.Result), and it asserts on bb.Outcome directly. Without a
	// substantial bb.Result, RunTask's terminal quality backstop
	// (validateOutputQuality, tree.go) falls back to resolvedResult's last
	// short bb.Results entry ("mark:a"), scores it below its length
	// threshold, and flips an otherwise-correct Success to Failure — a
	// pre-existing (2026-07-14, commit 6e948f8) content-quality heuristic
	// entirely unrelated to mutation-apply correctness. The other four tests
	// in this file don't hit it: they don't assert on bb.Outcome
	// (Remove/MemSequence/Persist), or their last action writes a substantial
	// bb.Result (Graft's muttest_grafted). Pre-set bb.Result here the same
	// way, so resolvedResult uses it instead of falling back.
	bb.Result = "reject test: gate resolved, sequence should complete successfully"
	// Root removal is always rejected; the goroutine sees the rejected
	// journal record and releases the gate — proving the run survived.
	done := enqueueWhenLiveThenRelease(t, "rejectcase",
		MutationOp{Kind: "remove", Path: "", Origin: OriginOperator}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "rejectcase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("rejected op must not fail the run, outcome=%s result=%q", bb.Outcome, bb.Result)
	}
}

func TestRunTaskMutablePersistHookInvoked(t *testing.T) {
	var persisted *evolution.SerializableNode
	var persistedInfo LiveRunInfo
	old := PersistMutatedTreeFn
	PersistMutatedTreeFn = func(info LiveRunInfo, tr *evolution.SerializableNode) error {
		persistedInfo, persisted = info, tr
		return nil
	}
	defer func() { PersistMutatedTreeFn = old }()
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "muttest_gate"}}}
	bb, gate := newGateBB("persist test")
	done := enqueueWhenLiveThenRelease(t, "persistcase",
		MutationOp{Kind: "add", ParentPath: "", Index: -1, Persist: true, Origin: OriginOperator,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_mark_a"}}, gate)
	if _, err := RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "persistcase"}); err != nil {
		t.Fatal(err)
	}
	<-done
	if persisted == nil || persistedInfo.TreeID != "persistcase" {
		t.Fatal("persist hook must receive the mutated tree and run info")
	}
	if len(persisted.Children) != 2 {
		t.Fatalf("persisted tree must include the graft, has %d children", len(persisted.Children))
	}
}
