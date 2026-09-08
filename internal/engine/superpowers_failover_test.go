package engine

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type failoverRunnerFunc func(context.Context, string, string) CommandResult

func (f failoverRunnerFunc) RunClaude(c context.Context, d, p string) CommandResult {
	return f(c, d, p)
}
func (f failoverRunnerFunc) RunCodex(c context.Context, d, p string) CommandResult { return f(c, d, p) }

func TestRateLimitFailoverDirections(t *testing.T) {
	for _, primary := range []DelegationProvider{DelegationProviderClaude, DelegationProviderCodex} {
		t.Run(string(primary), func(t *testing.T) {
			isolateBackoffStores(t)
			t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
			t.Setenv("BT_SUPERPOWERS_PROVIDER", string(primary))
			var calls []string
			limited := failoverRunnerFunc(func(_ context.Context, d, p string) CommandResult {
				calls = append(calls, "primary")
				return CommandResult{Err: errors.New("usage limit reached")}
			})
			healthy := failoverRunnerFunc(func(_ context.Context, d, p string) CommandResult {
				if d != "/repo" || p != "prompt" {
					t.Fatal("request changed")
				}
				calls = append(calls, "alternate")
				return CommandResult{Output: "healthy"}
			})
			d := delegatingRunner{claude: limited, codex: healthy}
			if primary == DelegationProviderCodex {
				d.claude = healthy
				d.codex = limited
			}
			for range 2 {
				if r := d.RunClaude(context.Background(), "/repo", "prompt"); r.Err != nil || r.Output != "healthy" {
					t.Fatalf("failover result: %+v", r)
				}
			}
			if len(calls) != 3 || calls[0] != "primary" || calls[1] != "alternate" || calls[2] != "alternate" {
				t.Fatalf("calls=%v", calls)
			}
			if os.Getenv("BT_SUPERPOWERS_PROVIDER") != string(primary) {
				t.Fatal("process environment mutated")
			}
			if _, ok := readSharedBackoff(backoffPathFor(primary)); !ok {
				t.Fatal("primary cooldown missing")
			}
		})
	}
}

func TestRateLimitFailoverStops(t *testing.T) {
	for _, mode := range []string{"disabled", "ordinary", "cancelled", "deadline", "cancel-during"} {
		t.Run(mode, func(t *testing.T) {
			isolateBackoffStores(t)
			t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
			if mode == "disabled" {
				t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "false")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var calls int
			first := failoverRunnerFunc(func(context.Context, string, string) CommandResult {
				calls++
				err := errors.New("usage limit reached")
				if mode == "ordinary" {
					err = errors.New("authentication failed")
				}
				if mode == "cancelled" {
					err = context.Canceled
				}
				if mode == "deadline" {
					err = context.DeadlineExceeded
				}
				if mode == "cancel-during" {
					cancel()
				}
				return CommandResult{Err: err, Output: "usage limit reached"}
			})
			other := failoverRunnerFunc(func(context.Context, string, string) CommandResult {
				t.Error("unexpected alternate")
				return CommandResult{}
			})
			d := delegatingRunner{claude: first, codex: other, provider: func() (DelegationProvider, error) { return DelegationProviderClaude, nil }}
			// Ordinary errors must not fail over merely because arbitrary output mentions quota.
			if mode == "ordinary" {
				first = failoverRunnerFunc(func(context.Context, string, string) CommandResult {
					calls++
					return CommandResult{Err: errors.New("authentication failed")}
				})
				d.claude = first
			}
			d.RunClaude(ctx, "/repo", "prompt")
			if calls != 1 {
				t.Fatal(calls)
			}
		})
	}
}

func TestRateLimitFailoverConcurrent(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	writeSharedBackoff(backoffPathFor(DelegationProviderCodex), time.Now().Add(time.Hour), "test")
	var calls atomic.Int32
	d := delegatingRunner{codex: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		t.Error("closed primary invoked")
		return CommandResult{}
	}), claude: failoverRunnerFunc(func(context.Context, string, string) CommandResult { calls.Add(1); return CommandResult{Output: "ok"} })}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if r := d.RunClaude(context.Background(), "/repo", "prompt"); r.Output != "ok" {
				t.Errorf("%+v", r)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 20 {
		t.Fatal(calls.Load())
	}
}
