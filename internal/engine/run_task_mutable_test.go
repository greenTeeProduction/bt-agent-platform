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
	RegisterAction("muttest_mark_c", func(ctx *btcore.BTContext[Blackboard]) int {
		muttestMark(ctx.Blackboard, "c")
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
			// Record success observably (ChainState is only written from the
			// run goroutine — no lock needed) so tests can assert the gate
			// was actually reached and resolved, not silently skipped by a
			// cursor landing past it.
			n, _ := ctx.Blackboard.ChainState["gate_success"].(int)
			ctx.Blackboard.ChainState["gate_success"] = n + 1
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

// waitForFirstTick blocks until treeID's live run has completed at least one
// real tick. enqueueWhenLiveThenRelease enqueues its op as soon as the run
// is merely REGISTERED — visible in ListLiveRuns — which happens after the
// whole tree is already built (RunTaskMutable registers right before the
// first RunTask call), so the helper's tight poll loop reliably WINS the
// race against RunTask's first applyPending call: an op enqueued that way
// applies before any node has ever run, before any MemSequence cursor
// exists. Empirically 100% reproducible here (5/5 runs), not a rare flake —
// so a cursor-arithmetic test cannot just enqueue its real op directly; it
// would apply too early and never exercise the shift.
//
// Fix: send an always-rejected root-removal probe first (same op as
// TestRunTaskMutableRejectionKeepsRunHealthy — rejected before any clone is
// kept, so it is 100% structurally inert) and wait for its journal record.
// MutationRecord writes happen strictly after that applyPending call's
// drain(), and RunTask always ticks the tree once between consecutive
// applyPending calls (applyPending; tree.Run(); loop: applyPending;
// tree.Run(); ...) — so an op enqueued only AFTER observing the probe's
// record cannot land in the same drain() batch as the probe, and is
// therefore provably deferred to a later applyPending call, i.e. after at
// least one real tick has elapsed. No sleeps, no tick counting.
func waitForFirstTick(t *testing.T, treeID string) {
	t.Helper()
	probeGate := make(chan struct{})
	<-enqueueWhenLiveThenRelease(t, treeID, MutationOp{Kind: "remove", Path: "", Origin: OriginOperator}, probeGate)
}

func TestRunTaskMutableCursorArithmeticAdd(t *testing.T) {
	// Load-bearing arithmetic case (distinct from TestRunTaskMutableMemSequence-
	// KeepsPlace, which mutates the ROOT and only exercises generic pointer-
	// keyed state migration): an add DIRECTLY inside the blocked MemSequence,
	// at an index at-or-below the cursor, must shift the cursor forward
	// (cursor+1 — applyPending's shifts loop in run_task_mutable.go) so the
	// rebuilt tree resumes at the SAME child (the gate) instead of re-ticking
	// whatever now occupies the old cursor slot.
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "MemSequence", Name: "memphase", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "muttest_mark_a"},
				{Type: "Action", Name: "muttest_gate"},
				{Type: "Action", Name: "muttest_mark_b"},
			}},
		}}
	bb, gate := newGateBB("cursor add test")
	// See TestRunTaskMutableRejectionKeepsRunHealthy: no action here ever
	// writes a substantial bb.Result (only short "mark:x" entries), so
	// pre-set one the same way to satisfy RunTask's terminal quality
	// backstop (validateOutputQuality, tree.go) instead of tripping it.
	bb.Result = "cursor add test: memseq should resume at the gate and complete successfully"

	// RunTaskMutable blocks until the gate is released, so it must run on
	// its own goroutine while this one drives the two-phase mutation
	// sequencing (see waitForFirstTick). Only runErr is shared with the main
	// goroutine, and only read after <-runDone, which happens-after the
	// write via the channel close — no data race.
	var runErr error
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_, runErr = RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "cursoraddcase"})
	}()
	waitForFirstTick(t, "cursoraddcase") // step a has run; the gate has blocked once, cursor=1

	// Insert at the MemSequence itself (ParentPath "0" = root.Children[0]),
	// index 0 — strictly at-or-below the cursor (1, sitting on the gate).
	<-enqueueWhenLiveThenRelease(t, "cursoraddcase",
		MutationOp{Kind: "add", ParentPath: "0", Index: 0, Origin: OriginOperator,
			Subtree: &evolution.SerializableNode{Type: "Action", Name: "muttest_grafted"}}, gate)
	<-runDone
	if runErr != nil {
		t.Fatal(runErr)
	}

	countA, countGrafted, countB := 0, 0, 0
	for _, m := range marksOf(bb) {
		switch m {
		case "a":
			countA++
		case "grafted":
			countGrafted++
		case "b":
			countB++
		}
	}
	// Without the +1, the cursor (still 1, migrated verbatim) lands on the
	// new children[1] after the insert-at-0 shift — which is step a, not the
	// gate — so the memseq re-ticks and re-runs it.
	if countA != 1 {
		t.Fatalf("MemSequence cursor arithmetic must not re-run step a after a direct add at/below the cursor; step a ran %d times (marks=%v)", countA, marksOf(bb))
	}
	if countGrafted != 0 {
		t.Fatalf("node inserted behind the cursor must never execute this run, marks=%v", marksOf(bb))
	}
	if countB != 1 {
		t.Fatalf("step b must execute exactly once after the gate releases, marks=%v", marksOf(bb))
	}
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("run must complete successfully, outcome=%s result=%q", bb.Outcome, bb.Result)
	}
}

func TestRunTaskMutableCursorArithmeticRemove(t *testing.T) {
	// Mirror of TestRunTaskMutableCursorArithmeticAdd for the remove side of
	// the same applyPending shifts loop (see waitForFirstTick for why the
	// probe-then-real sequencing is required): a remove DIRECTLY inside the
	// blocked MemSequence, below the cursor, must shift the cursor back
	// (cursor-1) so the rebuilt tree still resumes at the gate instead of
	// skipping over it entirely. MemSequence.Run fast-forwards through
	// consecutively-successful children within a single outer tick, so one
	// confirmed tick is enough to also land both a and b, same as the add
	// test's single step a.
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{
			{Type: "MemSequence", Name: "memphase", Children: []evolution.SerializableNode{
				{Type: "Action", Name: "muttest_mark_a"},
				{Type: "Action", Name: "muttest_mark_b"},
				{Type: "Action", Name: "muttest_gate"},
				{Type: "Action", Name: "muttest_mark_c"},
			}},
		}}
	bb, gate := newGateBB("cursor remove test")
	bb.Result = "cursor remove test: memseq should resume at the gate and complete successfully"

	var runErr error
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_, runErr = RunTaskMutable(bb, tree, LiveRunInfo{Agent: "t", TreeID: "cursorremovecase"})
	}()
	waitForFirstTick(t, "cursorremovecase") // a and b have run; the gate has blocked once, cursor=2

	// Remove mark_a (path "0.0" = root.Children[0].Children[0]), strictly
	// below the cursor (2, sitting on the gate once a and b have completed).
	<-enqueueWhenLiveThenRelease(t, "cursorremovecase",
		MutationOp{Kind: "remove", Path: "0.0", ExpectName: "muttest_mark_a", Origin: OriginOperator}, gate)
	<-runDone
	if runErr != nil {
		t.Fatal(runErr)
	}

	countB, countC := 0, 0
	for _, m := range marksOf(bb) {
		switch m {
		case "b":
			countB++
		case "c":
			countC++
		}
	}
	gateSuccess, _ := bb.ChainState["gate_success"].(int)
	// Without the -1, the cursor (still 2, migrated verbatim) lands on the
	// new children[2] after the remove-at-0 shift — which is step c, not the
	// gate — so the memseq silently SKIPS the gate and it never records a
	// success.
	if gateSuccess < 1 {
		t.Fatalf("MemSequence cursor arithmetic must keep resuming at the gate after a direct remove below the cursor; gate_success=%v marks=%v", gateSuccess, marksOf(bb))
	}
	if countB != 1 {
		t.Fatalf("step b must not be re-run, marks=%v", marksOf(bb))
	}
	if countC != 1 {
		t.Fatalf("step c must execute exactly once after the gate releases, marks=%v", marksOf(bb))
	}
	if bb.Outcome != string(evolution.Success) {
		t.Fatalf("run must complete successfully, outcome=%s result=%q", bb.Outcome, bb.Result)
	}
}
