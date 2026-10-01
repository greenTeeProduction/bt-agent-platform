package engine

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func TestPersistedCursorsRejectInvalidState(t *testing.T) {
	invalid := []any{-1, 99, 1.5, math.NaN(), math.Inf(1), math.Ldexp(1, strconv.IntSize-1), "1"}
	for _, typ := range []string{"PersistentMemSequence", "ForEachTask"} {
		for i, value := range invalid {
			t.Run(fmt.Sprintf("%s/%d", typ, i), func(t *testing.T) {
				bb := newTestBlackboard()
				executed := []int{}
				name := fmt.Sprintf("CursorRegressionBody_%s_%d", typ, i)
				RegisterAction(name, func(ctx *btcore.BTContext[Blackboard]) int {
					if typ == "ForEachTask" {
						idx, _ := chainStateInt(ctx.Blackboard, "superpowers_task_index")
						executed = append(executed, idx)
					} else {
						executed = append(executed, len(executed))
					}
					return 1
				})
				node := &evolution.SerializableNode{Type: typ, Name: "cursor", Children: []evolution.SerializableNode{{Type: "Action", Name: name}}}
				if typ == "PersistentMemSequence" {
					node.Children = append(node.Children, node.Children[0])
					bb.ChainState["memseq/cursor"] = value
				} else {
					setSuperpowersRun(bb, &SuperpowersRun{Tasks: []SuperpowersTask{{Title: "first"}, {Title: "second"}}})
					bb.ChainState["foreach/cursor/index"] = value
				}
				if got := buildNode(node, bb, "").Run(newTestBTContext(bb)); got != 1 {
					t.Fatalf("status=%d", got)
				}
				if !reflect.DeepEqual(executed, []int{0, 1}) {
					t.Fatalf("invalid cursor skipped work: %v", executed)
				}
			})
		}
	}
}

func TestChainStateIntRejectsOverflowAndFraction(t *testing.T) {
	bb := newTestBlackboard()
	for _, v := range []float64{1.5, math.NaN(), math.Inf(-1), math.Ldexp(1, strconv.IntSize-1), -math.Ldexp(1, strconv.IntSize-1) * 2} {
		bb.ChainState["cursor"] = v
		if n, ok := chainStateInt(bb, "cursor"); ok {
			t.Fatalf("accepted %v as %d", v, n)
		}
	}
}

func TestForEachTaskChildRemovesRun(t *testing.T) {
	bb := newTestBlackboard()
	setSuperpowersRun(bb, &SuperpowersRun{Tasks: []SuperpowersTask{{Title: "first"}}})
	RegisterAction("CursorRemoveRun", func(ctx *btcore.BTContext[Blackboard]) int {
		delete(ctx.Blackboard.ChainState, chainKeySuperpowersRun)
		return 1
	})
	node := &evolution.SerializableNode{Type: "ForEachTask", Name: "removed", Children: []evolution.SerializableNode{{Type: "Action", Name: "CursorRemoveRun"}}}
	if got := buildNode(node, bb, "").Run(newTestBTContext(bb)); got != -1 {
		t.Fatalf("missing run status=%d", got)
	}
}

func TestPersistentMemSequenceResumesAndCompletes(t *testing.T) {
	bb := newTestBlackboard()
	calls := 0
	RegisterAction("CursorResume", func(*btcore.BTContext[Blackboard]) int { calls++; return 1 })
	node := &evolution.SerializableNode{Type: "PersistentMemSequence", Name: "resume", Children: []evolution.SerializableNode{{Type: "Action", Name: "CursorResume"}, {Type: "Action", Name: "CursorResume"}}}
	for _, start := range []int{1, 2} {
		calls = 0
		bb.ChainState["memseq/resume"] = float64(start)
		if got := buildNode(node, bb, "").Run(newTestBTContext(bb)); got != 1 || calls != 2-start {
			t.Fatalf("start=%d status=%d calls=%d", start, got, calls)
		}
	}
}
