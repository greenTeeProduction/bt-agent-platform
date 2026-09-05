package engine

import (
	"context"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

func TestReviewParallelStatuses(t *testing.T) {
	for _, mode := range []ParallelMode{ParallelAll, ParallelAny, ParallelRace, ParallelMonitor} {
		for _, tc := range []struct {
			codes []int
			want  int
		}{{[]int{0}, 0}, {[]int{1}, 1}, {[]int{-1}, -1}, {[]int{0, 0}, 0}} {
			children := []btcore.Command[Blackboard]{}
			for _, code := range tc.codes {
				children = append(children, okCommand{code})
			}
			result := make(chan int, 1)
			go func() {
				result <- runReactiveParallel(children, mode, nil, nil, true, btcore.NewBTContext(t.Context(), &Blackboard{}))
			}()
			select {
			case got := <-result:
				if got != tc.want {
					t.Errorf("mode %d codes %v got %d want %d", mode, tc.codes, got, tc.want)
				}
			case <-time.After(100 * time.Millisecond):
				t.Errorf("mode %d codes %v hung", mode, tc.codes)
			}
		}
	}
}
func TestReviewParallelRetickMerge(t *testing.T) {
	var sideEffects atomic.Int32
	registerReviewAction(t, "review_success", func(ctx *btcore.BTContext[Blackboard]) int {
		sideEffects.Add(1)
		ctx.Blackboard.Result = "first"
		ctx.Blackboard.Results = append(ctx.Blackboard.Results, "first")
		ctx.Blackboard.TokensUsed += 3
		return 1
	})
	registerReviewAction(t, "review_running", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.ChainState == nil {
			bb.ChainState = map[string]any{}
		}
		n, _ := bb.ChainState["ticks"].(int)
		bb.ChainState["ticks"] = n + 1
		bb.TokensUsed += 2
		if n == 0 {
			bb.Outcome = "pending_approval"
			return 0
		}
		bb.Outcome = "success"
		bb.Result = "second"
		bb.Results = append(bb.Results, "second")
		return 1
	})
	bb := &Blackboard{}
	node := &evolution.SerializableNode{Type: "Parallel", Children: []evolution.SerializableNode{{Type: "Action", Name: "review_success"}, {Type: "Action", Name: "review_running"}}}
	cmd := BuildParallel(node, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	if got := cmd.Run(ctx); got != 0 {
		t.Errorf("first tick=%d", got)
	}
	if bb.Outcome != "pending_approval" || bb.TokensUsed != 5 {
		t.Errorf("missing flags/budgets: %+v", bb)
	}
	if got := cmd.Run(ctx); got != 1 {
		t.Errorf("second tick=%d", got)
	}
	if sideEffects.Load() != 1 {
		t.Errorf("repeated successful side effect %d", sideEffects.Load())
	}
	if bb.Result != "second" || !reflect.DeepEqual(bb.Results, []string{"first", "second"}) || bb.TokensUsed != 7 {
		t.Errorf("outputs not merged: %+v", bb)
	}
}
func TestReviewParallelCancelsAndJoins(t *testing.T) {
	for _, mode := range []ParallelMode{ParallelAny, ParallelRace, ParallelMonitor} {
		started := make(chan struct{})
		var stopped atomic.Bool
		slow := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			close(started)
			if ctx.Context == nil {
				return -1
			}
			<-ctx.Done()
			stopped.Store(true)
			return -1
		})
		winner := btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
			<-started
			if mode == ParallelMonitor {
				return -1
			}
			return 1
		})
		done := make(chan int, 1)
		go func() {
			done <- runReactiveParallel([]btcore.Command[Blackboard]{winner, slow}, mode, []int{0}, []int{1}, true, btcore.NewBTContext(t.Context(), &Blackboard{}))
		}()
		select {
		case <-done:
			if !stopped.Load() {
				t.Errorf("mode %d returned without cancel/join", mode)
			}
		case <-time.After(time.Second):
			t.Errorf("mode %d cancellation hung", mode)
		}
	}
}
func TestReviewForkMutablePointers(t *testing.T) {
	type state struct{ Values []string }
	original := &state{Values: []string{"original"}}
	bb := &Blackboard{ChainState: map[string]any{"nested": map[string]any{"state": original}}}
	a, b := forkBlackboard(bb), forkBlackboard(bb)
	a.ChainState["nested"].(map[string]any)["state"].(*state).Values[0] = "changed"
	if original.Values[0] != "original" || b.ChainState["nested"].(map[string]any)["state"].(*state).Values[0] != "original" {
		t.Fatal("fork aliases nested mutable state")
	}
}
func TestReviewRunTaskCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var calls int
	bb := &Blackboard{TraceContext: ctx}
	RunTask(bb, btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int { calls++; return 1 }))
	if calls != 0 || bb.Outcome != "failure" {
		t.Fatalf("cancelled run executed: calls=%d outcome=%s", calls, bb.Outcome)
	}
}
func TestReviewRunningAllowsExternalProgress(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	ready := make(chan struct{})
	go func() { close(ready) }()
	bb := &Blackboard{}
	RunTask(bb, btleaf.NewAction(func(_ *btcore.BTContext[Blackboard]) int {
		select {
		case <-ready:
			return 1
		default:
			return 0
		}
	}))
	if bb.Outcome != "success" {
		t.Fatalf("reticks exhausted before external goroutine progressed: %s", bb.Outcome)
	}
}
func TestReviewAnyPublishesOnlyWinningBranchAcrossTicks(t *testing.T) {
	registerReviewAction(t, "review_any_running", func(ctx *btcore.BTContext[Blackboard]) int {
		ctx.Blackboard.Result = "loser"
		ctx.Blackboard.Results = append(ctx.Blackboard.Results, "loser")
		return 0
	})
	registerReviewAction(t, "review_any_winner", func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.ChainState == nil {
			b.ChainState = map[string]any{}
		}
		if b.ChainState["prepared"] == true {
			return 1
		}
		b.ChainState["prepared"] = true
		b.Result = "winner"
		b.Results = append(b.Results, "winner")
		return 0
	})
	b := &Blackboard{}
	cmd := BuildParallel(&evolution.SerializableNode{Type: "Parallel", Metadata: map[string]any{"success_policy": "any"}, Children: []evolution.SerializableNode{{Type: "Action", Name: "review_any_running"}, {Type: "Action", Name: "review_any_winner"}}}, b)
	ctx := btcore.NewBTContext(t.Context(), b)
	if got := cmd.Run(ctx); got != 0 {
		t.Fatal(got)
	}
	if len(b.Results) != 0 || b.Result != "" {
		t.Errorf("published unwon branch output: %+v", b.Results)
	}
	if got := cmd.Run(ctx); got != 1 {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(b.Results, []string{"winner"}) || b.Result != "winner" {
		t.Errorf("winner lost prepared output or inherited loser: %v %q", b.Results, b.Result)
	}
}
func TestReviewParallelParentCancellation(t *testing.T) {
	for _, mode := range []ParallelMode{ParallelAll, ParallelAny, ParallelRace, ParallelMonitor} {
		ctx, cancel := context.WithCancel(t.Context())
		started := make(chan struct{})
		joined := make(chan struct{})
		done := make(chan int, 1)
		child := btleaf.NewAction(func(c *btcore.BTContext[Blackboard]) int { close(started); <-c.Done(); close(joined); return -1 })
		go func() {
			done <- runReactiveParallel([]btcore.Command[Blackboard]{child}, mode, []int{0}, nil, true, btcore.NewBTContext(ctx, &Blackboard{}))
		}()
		<-started
		cancel()
		select {
		case got := <-done:
			if got != -1 {
				t.Errorf("mode %d cancelled result=%d", mode, got)
			}
			select {
			case <-joined:
			default:
				t.Error("returned before child joined")
			}
		case <-time.After(time.Second):
			t.Fatalf("mode %d cancellation hung", mode)
		}
	}
}
func TestReviewBuiltReactiveSingleRunning(t *testing.T) {
	registerReviewAction(t, "review_running_only", func(*btcore.BTContext[Blackboard]) int { return 0 })
	for _, mode := range []string{"all", "any", "race", "monitor"} {
		b := &Blackboard{}
		cmd := BuildReactiveParallel(&evolution.SerializableNode{Type: "ReactiveParallel", Metadata: map[string]any{"mode": mode}, Children: []evolution.SerializableNode{{Type: "Action", Name: "review_running_only"}}}, b)
		if got := cmd.Run(btcore.NewBTContext(t.Context(), b)); got != 0 {
			t.Errorf("built %s returned %d", mode, got)
		}
	}
}

// Each repeated fixture owns its registration and removes its captured state.
func registerReviewAction(t *testing.T, name string, action ActionFunc) {
	t.Helper()
	RegisterAction(name, action)
	t.Cleanup(func() {
		regMu.Lock()
		defer regMu.Unlock()
		delete(actionRegistry, name)
	})
}

func TestReviewForkPreservesDataGraphWithoutSharingBranches(t *testing.T) {
	shared := []any{"original"}
	original := &Blackboard{ChainState: map[string]any{"left": shared, "right": shared}}
	fork := forkBlackboard(original)
	fork.ChainState["left"].([]any)[0] = "changed"
	if fork.ChainState["right"].([]any)[0] != "changed" || shared[0] != "original" {
		t.Fatal("fork lost internal aliases or shared data with the parent")
	}
	cycle := make([]any, 1)
	cycle[0] = cycle
	cloned := cloneParallelValue(cycle)
	if reflect.ValueOf(cloned).Pointer() != reflect.ValueOf(cloned[0]).Pointer() || reflect.ValueOf(cloned).Pointer() == reflect.ValueOf(cycle).Pointer() {
		t.Fatal("slice cycle not preserved in isolated fork")
	}
	values := [1]*string{new("original")}
	copyValues := cloneParallelValue(values)
	*copyValues[0] = "changed"
	if *values[0] != "original" {
		t.Fatal("array pointer aliases parent")
	}
}
