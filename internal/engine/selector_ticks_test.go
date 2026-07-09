package engine

import (
	"fmt"
	"testing"

	btcore "github.com/rvitorper/go-bt/core"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// registerTickTestAction registers a test action returning the given tick code.
func registerTickTestAction(t *testing.T, name string, code int) {
	t.Helper()
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := actionRegistry[name]; exists {
		t.Fatalf("action %q already registered", name)
	}
	actionRegistry[name] = func(ctx *btcore.BTContext[Blackboard]) int { return code }
	t.Cleanup(func() {
		regMu.Lock()
		defer regMu.Unlock()
		delete(actionRegistry, name)
	})
}

func findChildTick(ticks []ChildTick, parent, child string) *ChildTick {
	for i := range ticks {
		if ticks[i].Parent == parent && ticks[i].Child == child {
			return &ticks[i]
		}
	}
	return nil
}

// The observability wrapper must record every terminal (success/failure) child
// tick with its parent composite on the per-run blackboard — the telemetry
// producer the 2026-07-09 selector-ordering landings shipped without. The
// agent runner flushes Selector-attributed ticks into the durable per-tree
// selector stats at run end; without this producer that file never gains data
// and learned Selector reordering can never activate.
func TestRunRecordsTerminalChildTicksWithParents(t *testing.T) {
	registerTickTestAction(t, "TickFailingChild", -1)
	registerTickTestAction(t, "TickSucceedingChild", 1)

	tree := &evolution.SerializableNode{
		Type: "Sequence", Name: "TickRoot",
		Children: []evolution.SerializableNode{
			{
				Type: "Selector", Name: "TickRouter",
				Children: []evolution.SerializableNode{
					{Type: "Action", Name: "TickFailingChild"},
					{Type: "Action", Name: "TickSucceedingChild"},
				},
			},
		},
	}

	bb := &Blackboard{}
	bt := BuildTree(tree, bb)
	RunTask(bb, bt)

	ticks := bb.ChildTicks()
	failed := findChildTick(ticks, "TickRouter", "TickFailingChild")
	if failed == nil || failed.Status != "failure" {
		t.Fatalf("expected a failure tick for TickRouter/TickFailingChild, got %+v (ticks: %+v)", failed, ticks)
	}
	succeeded := findChildTick(ticks, "TickRouter", "TickSucceedingChild")
	if succeeded == nil || succeeded.Status != "success" {
		t.Fatalf("expected a success tick for TickRouter/TickSucceedingChild, got %+v (ticks: %+v)", succeeded, ticks)
	}
	// The Selector itself is a child of the root Sequence and must be
	// attributed to it.
	router := findChildTick(ticks, "TickRoot", "TickRouter")
	if router == nil || router.Status != "success" {
		t.Fatalf("expected a success tick for TickRoot/TickRouter, got %+v", router)
	}
}

// The per-run tick record is bounded: a runaway re-ticking tree must not grow
// the run's memory without limit.
func TestRecordChildTickBounded(t *testing.T) {
	bb := &Blackboard{}
	for i := 0; i < maxChildTicks+200; i++ {
		bb.recordChildTick("P", fmt.Sprintf("c%d", i), "success")
	}
	if got := len(bb.ChildTicks()); got != maxChildTicks {
		t.Fatalf("ChildTicks length = %d, want capped at %d", got, maxChildTicks)
	}
}
