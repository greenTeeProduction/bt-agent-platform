package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestRunOncePreservesTerminalDelegationAcrossOuterRetry(t *testing.T) {
	old, legacy := engine.AuctionDelegateWithContextFn, engine.AuctionDelegateFn
	defer func() { engine.AuctionDelegateWithContextFn = old; engine.AuctionDelegateFn = legacy }()
	for _, kind := range []string{"persistence", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			var diagnostic error = &reliability.ExecutionPersistenceError{Err: errors.New("child history unavailable")}
			if kind == "uncertain" {
				diagnostic = &reliability.ExecutionUncertainError{Err: errors.New("child completion unknown")}
			}
			engine.AuctionDelegateWithContextFn = func(context.Context, string, map[string]any) (string, bool, error) {
				calls++
				return "attributable completed child evidence", true, diagnostic
			}
			tree := &evolution.SerializableNode{Type: "Retry", MaxRetries: 3, Children: []evolution.SerializableNode{{Type: "Action", Name: "AuctionDelegate"}}}
			hist, err := NewHistory(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			reg, err := NewRegistry(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reg.Create(Definition{Name: "fixture", Tree: "fixture", Quality: &QualitySpec{MinLength: 1000}}); err != nil {
				t.Fatal(err)
			}
			deps := &RunDeps{Registry: reg, History: hist, ResolveTree: func(string) *evolution.SerializableNode { return tree }}
			var result *RunResult
			err = (&reliability.RetryPolicy{MaxRetries: 3, RetryUnknown: true}).ExecuteContext(context.Background(), func() error {
				var err error
				result, err = deps.RunOnce(context.Background(), "fixture", "allocate fixture", RunOptions{RecordHistory: true, EnforceQuality: true, DisableBlackboard: true, DisableAgentPromote: true, SkipSLORecording: true})
				return err
			})
			want := "aborted"
			if kind == "uncertain" {
				want = "uncertain"
			}
			if calls != 1 || result.Outcome != want || !strings.Contains(result.Output, "child evidence") || !errors.Is(err, diagnostic) {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
			}
			records := hist.List("fixture", 0)
			if len(records) != 1 || records[0].Outcome != want || (!strings.Contains(records[0].Error, diagnostic.Error()) || !strings.Contains(records[0].Error, "min_length")) {
				t.Fatalf("records=%+v", records)
			}
		})
	}
}
