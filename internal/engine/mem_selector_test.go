package engine

import (
	"fmt"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

// Characterization tests for BuildMemSelector (mem_selector.go).
//
// They pin the shipped contract of the MemSelector node: a Selector whose
// cursor lives in Blackboard.ChainState under "memsel/<node name>" so it
// survives ADR-003 JSON persistence. Children at indices below the cursor are
// not re-ticked; the cursor is cleared on every terminal outcome.
//
// Shared helpers (newTestBlackboard, newTestBTContext) live in mem_nodes_test.go.

// memSelCharAction registers a scripted action under name: its i-th tick
// returns codes[i], the last entry repeating once the script runs out, and
// every tick increments *calls. Names must be unique across the package test
// binary — RegisterAction panics on duplicates.
func memSelCharAction(t *testing.T, name string, codes []int, calls *int) {
	t.Helper()
	if len(codes) == 0 {
		t.Fatalf("memSelCharAction(%q): codes must not be empty", name)
	}
	tick := 0
	RegisterAction(name, func(_ *btcore.BTContext[Blackboard]) int {
		*calls++
		i := tick
		tick++
		if i >= len(codes) {
			i = len(codes) - 1
		}
		return codes[i]
	})
}

// memSelCharNode builds a MemSelector node named nodeName whose children are
// Action leaves referencing the given registered action names.
func memSelCharNode(nodeName string, childActions ...string) *evolution.SerializableNode {
	node := &evolution.SerializableNode{Type: "MemSelector", Name: nodeName}
	for _, a := range childActions {
		node.Children = append(node.Children, evolution.SerializableNode{Type: "Action", Name: a})
	}
	return node
}

func TestBuildMemSelector_SingleTickOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		childCodes [][]int
		wantCode   int
		wantCalls  []int
		wantCursor any // nil => the cursor key must be absent
	}{
		{name: "NoChildren", wantCode: -1},
		{name: "FirstSucceeds", childCodes: [][]int{{1}, {1}}, wantCode: 1, wantCalls: []int{1, 0}},
		{name: "FirstRunning", childCodes: [][]int{{0}, {1}}, wantCode: 0, wantCalls: []int{1, 0}, wantCursor: 0},
		{name: "FirstFailsSecondSucceeds", childCodes: [][]int{{-1}, {1}}, wantCode: 1, wantCalls: []int{1, 1}},
		{name: "FirstFailsSecondRunning", childCodes: [][]int{{-1}, {0}}, wantCode: 0, wantCalls: []int{1, 1}, wantCursor: 1},
		{name: "AllFail", childCodes: [][]int{{-1}, {-1}, {-1}}, wantCode: -1, wantCalls: []int{1, 1, 1}},
		// Status is classified by sign, not by exact code: any code > 0 is
		// SUCCESS and is normalized to 1, any code < 0 is FAILURE.
		{name: "PositiveCodeAboveOneIsSuccess", childCodes: [][]int{{7}, {1}}, wantCode: 1, wantCalls: []int{1, 0}},
		{name: "NegativeCodeBelowMinusOneIsFailure", childCodes: [][]int{{-9}, {1}}, wantCode: 1, wantCalls: []int{1, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := make([]int, len(tc.childCodes))
			names := make([]string, len(tc.childCodes))
			for i, codes := range tc.childCodes {
				names[i] = fmt.Sprintf("MemSelCharOutcome_%s_%d", tc.name, i)
				memSelCharAction(t, names[i], codes, &calls[i])
			}
			nodeName := "MemSelCharOutcome" + tc.name
			key := "memsel/" + nodeName
			bb := newTestBlackboard()
			cmd := BuildMemSelector(memSelCharNode(nodeName, names...), bb)

			if got := cmd.Run(newTestBTContext(bb)); got != tc.wantCode {
				t.Fatalf("Run() = %d, want %d", got, tc.wantCode)
			}
			for i, want := range tc.wantCalls {
				if calls[i] != want {
					t.Errorf("child %d ticked %d times, want %d", i, calls[i], want)
				}
			}
			got, ok := bb.ChainState[key]
			if tc.wantCursor == nil {
				if ok {
					t.Errorf("ChainState[%q] = %v, want cleared on a terminal outcome", key, got)
				}
				return
			}
			if !ok || got != tc.wantCursor {
				t.Errorf("ChainState[%q] = %v (present=%t), want %v", key, got, ok, tc.wantCursor)
			}
		})
	}
}

func TestBuildMemSelector_CursorKeyIsNamespacedByNodeName(t *testing.T) {
	tests := []struct {
		name     string
		nodeName string
		wantKey  string
	}{
		{name: "NamedNode", nodeName: "MemSelCharKeyed", wantKey: "memsel/MemSelCharKeyed"},
		{name: "UnnamedNodeKeepsBarePrefix", nodeName: "", wantKey: "memsel/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			action := "MemSelCharKey_" + tc.name
			memSelCharAction(t, action, []int{0}, &calls) // RUNNING, so the cursor is written
			bb := newTestBlackboard()
			cmd := BuildMemSelector(memSelCharNode(tc.nodeName, action), bb)

			if got := cmd.Run(newTestBTContext(bb)); got != 0 {
				t.Fatalf("Run() = %d, want 0 (RUNNING)", got)
			}
			if len(bb.ChainState) != 1 {
				t.Fatalf("ChainState = %v, want exactly one cursor entry", bb.ChainState)
			}
			if got, ok := bb.ChainState[tc.wantKey]; !ok || got != 0 {
				t.Errorf("ChainState[%q] = %v (present=%t), want 0", tc.wantKey, got, ok)
			}
		})
	}
}

func TestBuildMemSelector_ResumesFromPersistedCursor(t *testing.T) {
	tests := []struct {
		name           string
		cursor         any // nil => no cursor persisted at all
		wantFirstCalls int
	}{
		{name: "MissingKeyStartsAtZero", cursor: nil, wantFirstCalls: 1},
		{name: "Int", cursor: 1},
		{name: "Int64", cursor: int64(1)},
		{name: "Float64JSONRoundTrip", cursor: float64(1)},
		// chainStateInt reports !ok for other shapes and BuildMemSelector
		// ignores the flag, so an unparseable cursor restarts the pass.
		{name: "StringCursorRestartsAtZero", cursor: "not-a-number", wantFirstCalls: 1},
		{name: "BoolCursorRestartsAtZero", cursor: true, wantFirstCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var firstCalls, secondCalls int
			first := "MemSelCharResumeA_" + tc.name
			second := "MemSelCharResumeB_" + tc.name
			memSelCharAction(t, first, []int{-1}, &firstCalls)
			memSelCharAction(t, second, []int{1}, &secondCalls)

			nodeName := "MemSelCharResume" + tc.name
			key := "memsel/" + nodeName
			bb := newTestBlackboard()
			if tc.cursor != nil {
				bb.ChainState[key] = tc.cursor
			}
			cmd := BuildMemSelector(memSelCharNode(nodeName, first, second), bb)

			if got := cmd.Run(newTestBTContext(bb)); got != 1 {
				t.Fatalf("Run() = %d, want 1 (SUCCESS)", got)
			}
			if firstCalls != tc.wantFirstCalls {
				t.Errorf("child 0 ticked %d times, want %d", firstCalls, tc.wantFirstCalls)
			}
			if secondCalls != 1 {
				t.Errorf("child 1 ticked %d times, want 1", secondCalls)
			}
			if got, ok := bb.ChainState[key]; ok {
				t.Errorf("ChainState[%q] = %v, want cleared on SUCCESS", key, got)
			}
		})
	}
}

func TestBuildMemSelector_SkipsFailedChildrenOnLaterTicks(t *testing.T) {
	var failCalls, runCalls int
	memSelCharAction(t, "MemSelCharSkipFail", []int{-1}, &failCalls)
	memSelCharAction(t, "MemSelCharSkipRun", []int{0, 0, 1}, &runCalls)

	const nodeName = "MemSelCharSkip"
	key := "memsel/" + nodeName
	bb := newTestBlackboard()
	cmd := BuildMemSelector(memSelCharNode(nodeName, "MemSelCharSkipFail", "MemSelCharSkipRun"), bb)
	ctx := newTestBTContext(bb)

	for tick := 1; tick <= 2; tick++ {
		if got := cmd.Run(ctx); got != 0 {
			t.Fatalf("tick %d: Run() = %d, want 0 (RUNNING)", tick, got)
		}
		if got, ok := bb.ChainState[key]; !ok || got != 1 {
			t.Fatalf("tick %d: ChainState[%q] = %v (present=%t), want 1", tick, key, got, ok)
		}
		if failCalls != 1 {
			t.Fatalf("tick %d: failed child re-ticked (%d calls), want 1", tick, failCalls)
		}
	}
	if got := cmd.Run(ctx); got != 1 {
		t.Fatalf("tick 3: Run() = %d, want 1 (SUCCESS)", got)
	}
	if failCalls != 1 {
		t.Errorf("failed child ticked %d times across the whole pass, want 1", failCalls)
	}
	if runCalls != 3 {
		t.Errorf("running child ticked %d times, want 3", runCalls)
	}
	if got, ok := bb.ChainState[key]; ok {
		t.Errorf("ChainState[%q] = %v, want cleared on SUCCESS", key, got)
	}
}

func TestBuildMemSelector_StaleCursorAtOrPastLastChildFailsWithoutTicking(t *testing.T) {
	tests := []struct {
		name   string
		cursor any
	}{
		{name: "ExactlyChildCount", cursor: 2},
		{name: "PastChildCount", cursor: 5},
		{name: "PastChildCountFloat", cursor: float64(9)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var firstCalls, secondCalls int
			first := "MemSelCharStaleA_" + tc.name
			second := "MemSelCharStaleB_" + tc.name
			memSelCharAction(t, first, []int{1}, &firstCalls)
			memSelCharAction(t, second, []int{1}, &secondCalls)

			nodeName := "MemSelCharStale" + tc.name
			key := "memsel/" + nodeName
			bb := newTestBlackboard()
			bb.ChainState[key] = tc.cursor
			cmd := BuildMemSelector(memSelCharNode(nodeName, first, second), bb)

			if got := cmd.Run(newTestBTContext(bb)); got != -1 {
				t.Fatalf("Run() = %d, want -1 (FAILURE)", got)
			}
			if firstCalls != 0 || secondCalls != 0 {
				t.Errorf("children ticked (%d, %d), want (0, 0)", firstCalls, secondCalls)
			}
			if got, ok := bb.ChainState[key]; ok {
				t.Errorf("ChainState[%q] = %v, want the stale cursor cleared", key, got)
			}
		})
	}
}

// TestBuildMemSelector_NegativePersistedCursorRestartsInsteadOfPanicking pins
// the one behavior the current implementation gets wrong. ChainState is
// JSON-persisted and externally writable (cmd/bt-docgen setChainState, the bb_*
// ReAct tools, hand-edited run state), so the cursor read back at
// mem_selector.go:21 is untrusted input. A negative value makes the loop index
// children[-1] and panic mid-tick. The sibling selector in the same package
// already range-checks the same kind of resume cursor
// (bandit_selector.go:318 — `idx >= 0 && idx < len(children)`) and falls back to
// a fresh pass; MemSelector should do the same. Cursors at or past the child
// count already degrade safely — see
// TestBuildMemSelector_StaleCursorAtOrPastLastChildFailsWithoutTicking.
func TestBuildMemSelector_NegativePersistedCursorRestartsInsteadOfPanicking(t *testing.T) {
	tests := []struct {
		name   string
		cursor any
	}{
		{name: "Int", cursor: -1},
		{name: "Int64", cursor: int64(-2)},
		{name: "Float64JSONRoundTrip", cursor: float64(-1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var firstCalls, secondCalls int
			first := "MemSelCharNegA_" + tc.name
			second := "MemSelCharNegB_" + tc.name
			memSelCharAction(t, first, []int{1}, &firstCalls)
			memSelCharAction(t, second, []int{1}, &secondCalls)

			nodeName := "MemSelCharNeg" + tc.name
			key := "memsel/" + nodeName
			bb := newTestBlackboard()
			bb.ChainState[key] = tc.cursor
			cmd := BuildMemSelector(memSelCharNode(nodeName, first, second), bb)

			code := -99
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("Run() panicked on a negative persisted cursor (%v = %v): %v; "+
							"an out-of-range cursor must be ignored, not indexed", key, tc.cursor, r)
					}
				}()
				code = cmd.Run(newTestBTContext(bb))
			}()

			if code != 1 {
				t.Fatalf("Run() = %d, want 1 (SUCCESS after restarting the pass at child 0)", code)
			}
			if firstCalls != 1 {
				t.Errorf("child 0 ticked %d times, want 1", firstCalls)
			}
			if secondCalls != 0 {
				t.Errorf("child 1 ticked %d times, want 0", secondCalls)
			}
			if got, ok := bb.ChainState[key]; ok {
				t.Errorf("ChainState[%q] = %v, want cleared on SUCCESS", key, got)
			}
		})
	}
}
