// RunTaskMutable: RunTask with a live mutation queue applied at tick
// boundaries (spec: docs/superpowers/specs/2026-07-17-runtime-tree-mutation-design.md).
package engine

import (
	"fmt"
	"time"

	"github.com/nico/go-bt-evolve/internal/audit"
	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// PersistMutatedTreeFn persists a mutated live tree so future runs inherit it.
// Nil-checked injection hook wired from cmd/bt-agent (engine must not import
// higher layers). Persist snapshots the ENTIRE current live tree — including
// earlier ephemeral ops from the same run (documented spec semantic).
var PersistMutatedTreeFn func(info LiveRunInfo, tree *evolution.SerializableNode) error

// RunTaskMutable validates, builds, and runs serTree with runtime mutation
// support. It mirrors BuildAndValidate's build semantics, always operating on
// a private clone (resolver-returned trees may be shared package state), and
// registers the run so bb.EnqueueMutation and EnqueueLiveMutation reach it.
func RunTaskMutable(bb *Blackboard, serTree *evolution.SerializableNode, info LiveRunInfo) (string, error) {
	if bb == nil {
		return "", fmt.Errorf("nil blackboard")
	}
	if bb.childTicks == nil {
		bb.childTicks = &childTickLog{}
	}
	expanded, err := prepareTreeForBuild(serTree)
	if err != nil {
		return "", err
	}
	if serTree.TimeoutMs > 0 {
		bb.TreeTimeoutMs = serTree.TimeoutMs
	}
	vinfo := ValidateTreeFull(expanded)
	if !vinfo.Valid() {
		return "", fmt.Errorf("tree validation failed: %v", vinfo.Errors)
	}
	cur := cloneNode(expanded)
	bb.buildCapture = map[*evolution.SerializableNode]btcore.Command[Blackboard]{}
	cmd := buildNode(cur, bb, "")
	lr := registerLiveRun(bb, info)
	lr.cur, lr.capture = cur, bb.buildCapture
	defer deregisterLiveRun(lr.runID)
	_ = RunTask(bb, cmd)
	return bb.Result, nil
}

// applyPending drains and applies queued mutations. Called from RunTask at
// tick boundaries — quiescent points where no node is executing (parallel
// composites join within their tick), so the tree needs no lock. Returns the
// command tree to keep ticking: the old one when nothing applied, the rebuilt
// one otherwise.
func (lr *liveRun) applyPending(ctx *btcore.BTContext[Blackboard], bb *Blackboard, tree btcore.Command[Blackboard]) btcore.Command[Blackboard] {
	ops := lr.drain()
	if len(ops) == 0 {
		return tree
	}
	type appliedShift struct {
		parent *evolution.SerializableNode // node in the FINAL tree coordinates
		kind   string
		index  int
	}
	// corrTotal maps nodes of the CURRENT live tree (lr.cur) to their
	// counterparts in the evolving candidate. Correspondence is ALWAYS
	// computed by mapCorrespondence AFTER an op runs — never captured during
	// a clone — because applyMutationOp's insert can reallocate a parent's
	// children array and move sibling addresses.
	working := cloneNode(lr.cur)
	corrTotal := map[*evolution.SerializableNode]*evolution.SerializableNode{}
	mapCorrespondence(lr.cur, working, nil, "", 0, corrTotal)
	var shifts []appliedShift
	applied := 0
	persistWanted := false
	for _, q := range ops {
		candidate := cloneNode(working)
		parent, at, err := applyMutationOp(candidate, q.op)
		if err == nil {
			err = validateMutatedTree(candidate, q.op)
		}
		if err != nil {
			lr.record(MutationRecord{OpID: q.id, Op: q.op, Status: "rejected",
				Error: err.Error(), Generation: lr.generation, At: time.Now()})
			bb.Log().Warn("tree mutation rejected", "run", lr.runID, "op", q.id, "kind", q.op.Kind, "err", err)
			continue
		}
		// Accept: pair working → candidate around this op's shift, then
		// compose into corrTotal and re-point recorded shifts.
		corrStep := map[*evolution.SerializableNode]*evolution.SerializableNode{}
		mapCorrespondence(working, candidate, parent, q.op.Kind, at, corrStep)
		for old, mid := range corrTotal {
			corrTotal[old] = corrStep[mid] // nil when mid was removed — pairing ends there
		}
		for i := range shifts {
			shifts[i].parent = corrStep[shifts[i].parent]
		}
		working = candidate
		shifts = append(shifts, appliedShift{parent: parent, kind: q.op.Kind, index: at})
		applied++
		persistWanted = persistWanted || q.op.Persist
		lr.record(MutationRecord{OpID: q.id, Op: q.op, Status: "applied",
			Generation: lr.generation + 1, At: time.Now()})
	}
	if applied == 0 {
		return tree
	}
	// Rebuild and migrate pointer-keyed library state old → new for every
	// node of the final tree that has a counterpart in the previous tree.
	oldCapture := lr.capture
	newCapture := map[*evolution.SerializableNode]btcore.Command[Blackboard]{}
	bb.buildCapture = newCapture
	newCmd := buildNode(working, bb, "")
	inv := map[*evolution.SerializableNode]*evolution.SerializableNode{} // new → old
	for old, nw := range corrTotal {
		if nw != nil {
			inv[nw] = old
		}
	}
	var migrate func(n *evolution.SerializableNode)
	migrate = func(n *evolution.SerializableNode) {
		if old := inv[n]; old != nil {
			if oldCmd, ok := oldCapture[old]; ok {
				if newCmdN, ok2 := newCapture[n]; ok2 {
					migrateNodeState(ctx, oldCmd, newCmdN)
				}
			}
		}
		for i := range n.Children {
			migrate(&n.Children[i])
		}
	}
	migrate(working)
	// Cursor arithmetic for directly-mutated MemSequence parents: keep the
	// cursor pointing at the same child despite the index shift.
	for _, s := range shifts {
		if s.parent == nil || s.parent.Type != "MemSequence" {
			continue
		}
		cmdP, ok := newCapture[s.parent]
		if !ok {
			continue
		}
		cursor, ok := ctx.MemSequenceState[cmdP]
		if !ok {
			continue
		}
		switch {
		case s.kind == "add" && s.index <= cursor:
			ctx.MemSequenceState[cmdP] = cursor + 1
		case s.kind == "remove" && s.index < cursor:
			ctx.MemSequenceState[cmdP] = cursor - 1
		}
	}
	lr.mu.Lock()
	lr.generation++
	gen := lr.generation
	lr.mu.Unlock()
	lr.cur, lr.capture = working, newCapture
	bb.Log().Info("tree mutated at tick boundary", "run", lr.runID, "applied", applied, "generation", gen)
	if bb.EventBus != nil {
		bb.EventBus.Publish("tree_mutated", EventMessage{
			Source: "engine.applyPending", Timestamp: time.Now(), Type: "tree_mutated",
			Data: map[string]any{"run_id": lr.runID, "applied": applied, "generation": gen},
		})
	}
	_ = audit.Append(audit.Entry{
		Timestamp: time.Now(), Agent: lr.info.Agent, Action: "tree_mutation",
		Detail: fmt.Sprintf("applied %d op(s), generation %d, tree %s", applied, gen, lr.info.TreeID),
	})
	if persistWanted {
		if PersistMutatedTreeFn == nil {
			bb.Log().Warn("tree mutation persist requested but no persistence hook wired", "run", lr.runID)
		} else if err := PersistMutatedTreeFn(lr.info, cloneNode(working)); err != nil {
			// Persist failure is journaled, never fails the mutation or run.
			lr.record(MutationRecord{OpID: lr.runID + "-persist", Status: "rejected",
				Error: "persist: " + err.Error(), Generation: gen, At: time.Now()})
			bb.Log().Warn("tree mutation persist failed", "run", lr.runID, "err", err)
		}
	}
	return newCmd
}

// migrateNodeState moves every go-bt per-node state entry keyed by the old
// command pointer to the new one. MemSequence cursors, Repeat/Retry counters,
// and Timeout/Sleep stamps survive rebuilds for unchanged nodes.
func migrateNodeState(ctx *btcore.BTContext[Blackboard], oldCmd, newCmd btcore.Command[Blackboard]) {
	if oldCmd == nil || newCmd == nil || oldCmd == newCmd {
		return
	}
	if v, ok := ctx.MemSequenceState[oldCmd]; ok {
		ctx.MemSequenceState[newCmd] = v
		delete(ctx.MemSequenceState, oldCmd)
	}
	if v, ok := ctx.RepeaterState[oldCmd]; ok {
		ctx.RepeaterState[newCmd] = v
		delete(ctx.RepeaterState, oldCmd)
	}
	if v, ok := ctx.RetryState[oldCmd]; ok {
		ctx.RetryState[newCmd] = v
		delete(ctx.RetryState, oldCmd)
	}
	if v, ok := ctx.RetryTimeState[oldCmd]; ok {
		ctx.RetryTimeState[newCmd] = v
		delete(ctx.RetryTimeState, oldCmd)
	}
	if v, ok := ctx.TimeoutState[oldCmd]; ok {
		ctx.TimeoutState[newCmd] = v
		delete(ctx.TimeoutState, oldCmd)
	}
	if v, ok := ctx.SleepState[oldCmd]; ok {
		ctx.SleepState[newCmd] = v
		delete(ctx.SleepState, oldCmd)
	}
}
