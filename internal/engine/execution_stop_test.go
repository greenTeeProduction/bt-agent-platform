package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

type executionOwnerKey struct{}

func TestDelegationDiagnosticStopsRetrySelectorAndSequence(t *testing.T) {
	for _, kind := range []string{"persistence", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			calls, fallbacks := 0, 0
			old := DelegateToA2AFn
			defer func() { DelegateToA2AFn = old }()
			var diagnostic error = &reliability.ExecutionPersistenceError{Err: errors.New("history write failed")}
			if kind == "uncertain" {
				diagnostic = &reliability.ExecutionUncertainError{Err: errors.New("goap_fusion_rate_limited response lost")}
			}
			DelegateToA2AFn = func(context.Context, string, string) (string, error) {
				calls++
				return "completed child evidence", diagnostic
			}
			name := "TerminalFallback" + kind
			RegisterAction(name, func(ctx *btcore.BTContext[Blackboard]) int {
				fallbacks++
				ctx.Blackboard.Result = "replaced output"
				return 1
			})
			defer func() { regMu.Lock(); delete(actionRegistry, name); regMu.Unlock() }()
			tree := &evolution.SerializableNode{Type: "Sequence", Children: []evolution.SerializableNode{
				{Type: "Selector", Children: []evolution.SerializableNode{
					{Type: "Retry", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "DelegateToA2A"}}},
					{Type: "Action", Name: name},
				}},
				{Type: "Action", Name: name},
			}}
			bb := &Blackboard{Task: "fixture", ChainState: map[string]any{"a2a_target_url": "http://fixture"}}
			RunTask(bb, BuildTree(tree, bb))
			RunTask(bb, BuildTree(tree, bb)) // a later tick/rebuild must not clear evidence
			want := "aborted"
			if kind == "uncertain" {
				want = "uncertain"
			}
			if calls != 1 || fallbacks != 0 || bb.Outcome != want || bb.Result != "completed child evidence" || !errors.Is(bb.ExecutionError(), diagnostic) {
				t.Fatalf("calls=%d fallback=%d outcome=%s output=%q err=%v", calls, fallbacks, bb.Outcome, bb.Result, bb.ExecutionError())
			}
		})
	}
}

func TestAuctionHookReceivesTreeContextAndPreservesDiagnostic(t *testing.T) {
	old, legacy := AuctionDelegateWithContextFn, AuctionDelegateFn
	defer func() { AuctionDelegateWithContextFn = old; AuctionDelegateFn = legacy }()
	key := executionOwnerKey{}
	ctx := context.WithValue(t.Context(), key, "owner")
	diagnostic := &reliability.ExecutionUncertainError{Err: errors.New("lost winner response")}
	calls := 0
	AuctionDelegateWithContextFn = func(got context.Context, _ string, _ map[string]any) (string, bool, error) {
		calls++
		if got.Value(key) != "owner" {
			t.Error("caller context lost")
		}
		if _, ok := got.Deadline(); !ok {
			t.Error("tree budget lost")
		}
		return "winner evidence", true, diagnostic
	}
	AuctionDelegateFn = func(string, map[string]any) (string, bool, error) {
		t.Error("legacy hook overrode context hook")
		return "", false, nil
	}
	tree := &evolution.SerializableNode{Type: "Retry", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "AuctionDelegate"}}}
	bb := &Blackboard{Task: "allocate fixture", TraceContext: ctx}
	RunTask(bb, BuildTree(tree, bb))
	if calls != 1 || bb.Outcome != "uncertain" || bb.Result != "winner evidence" || !errors.Is(bb.ExecutionError(), diagnostic) {
		t.Fatalf("calls=%d bb=%+v diagnostic=%v", calls, bb, bb.ExecutionError())
	}
}

func TestParallelForkSharesExecutionStopAndUncertaintyWins(t *testing.T) {
	bb := &Blackboard{executionStop: &executionStop{}}
	first, second := forkBlackboard(bb), forkBlackboard(bb)
	first.stopExecution("completed evidence", &reliability.ExecutionPersistenceError{Err: errors.New("record failed")})
	second.stopExecution("unknown evidence", &reliability.ExecutionUncertainError{Err: errors.New("lost response")})
	if !bb.applyExecutionStop() || bb.Outcome != "uncertain" || bb.Result != "unknown evidence" || !reliability.IsExecutionUncertainError(bb.ExecutionError()) || reliability.IsExecutionPersistenceError(bb.ExecutionError()) {
		t.Fatalf("joined stop=%v outcome=%s result=%q", bb.ExecutionError(), bb.Outcome, bb.Result)
	}
}

func TestParallelPauseRetainsAdmittedSiblingFailure(t *testing.T) {
	for _, mode := range []string{"all", "any", "race"} {
		for _, fault := range []string{"failure", "panic", "success", "sequence-success", "retry-success"} {
			t.Run(mode+"/"+fault, func(t *testing.T) {
				entered := make(chan struct{})
				paused := make(chan struct{})
				pauseName, faultName := "ParallelPauseFixture", "ParallelFaultFixture"
				RegisterAction(pauseName, func(ctx *btcore.BTContext[Blackboard]) int {
					<-entered
					ctx.Blackboard.stopExecution("pause evidence", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("needs input")})
					ctx.Blackboard.applyExecutionStop()
					close(paused)
					return -1
				})
				RegisterAction(faultName, func(ctx *btcore.BTContext[Blackboard]) int {
					close(entered)
					<-paused // both branches were admitted before the pause
					switch fault {
					case "panic":
						panic("sibling fault")
					case "failure":
						ctx.Blackboard.Result = "failure evidence"
						ctx.Blackboard.Outcome = "failure"
						return -1
					default:
						ctx.Blackboard.Result = "completed evidence"
						ctx.Blackboard.Outcome = "success"
						return 1
					}
				})
				defer func() {
					regMu.Lock()
					delete(actionRegistry, pauseName)
					delete(actionRegistry, faultName)
					regMu.Unlock()
				}()
				faultNode := evolution.SerializableNode{Type: "Action", Name: faultName}
				if fault == "sequence-success" {
					faultNode = evolution.SerializableNode{Type: "Sequence", Children: []evolution.SerializableNode{faultNode}}
				}
				if fault == "retry-success" {
					faultNode = evolution.SerializableNode{Type: "Retry", MaxRetries: 3, Children: []evolution.SerializableNode{faultNode}}
				}
				tree := &evolution.SerializableNode{Type: "ReactiveParallel", Metadata: map[string]any{"mode": mode}, Children: []evolution.SerializableNode{{Type: "Action", Name: pauseName}, faultNode}}
				bb := &Blackboard{Task: "fixture"}
				RunTask(bb, BuildTree(tree, bb))
				want := "failure"
				if fault == "panic" {
					want = "panic"
				}
				if fault == "success" || fault == "sequence-success" || fault == "retry-success" {
					want = "input-required"
				}
				if bb.Outcome != want || reliability.ExecutionStopOutcome(bb.ExecutionError()) != want || !reliability.IsExecutionStoppedError(bb.ExecutionError()) {
					t.Fatalf("outcome=%s output=%q diagnostic=%v", bb.Outcome, bb.Result, bb.ExecutionError())
				}
			})
		}
	}
}

func TestParallelBlockedAdmissionDoesNotInventSiblingFailure(t *testing.T) {
	var calls int
	name := "BlockedParallelFixture"
	RegisterAction(name, func(*btcore.BTContext[Blackboard]) int { calls++; return -1 })
	defer func() { regMu.Lock(); delete(actionRegistry, name); regMu.Unlock() }()
	bb := &Blackboard{executionStop: &executionStop{}}
	bb.stopExecution("pause evidence", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("needs input")})
	child := &evolution.SerializableNode{Type: "Action", Name: name}
	cmd := BuildTree(child, bb)
	ctx := btcore.NewBTContext(t.Context(), bb)
	runReactiveParallel([]btcore.Command[Blackboard]{cmd}, ParallelAll, nil, nil, true, ctx)
	bb.applyExecutionStop()
	if calls != 0 || bb.Outcome != "input-required" || !reliability.IsExecutionPause(bb.Outcome, bb.ExecutionError()) {
		t.Fatalf("calls=%d outcome=%s err=%v", calls, bb.Outcome, bb.ExecutionError())
	}
}
