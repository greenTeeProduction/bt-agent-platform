package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRateLimitFailoverIgnoresPromptEcho(t *testing.T) {
	for _, p := range []DelegationProvider{DelegationProviderClaude, DelegationProviderCodex} {
		t.Run(string(p), func(t *testing.T) {
			isolateBackoffStores(t)
			t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
			prompt := "Fix usage limit reached and HTTP 429 handling"
			first := failoverRunnerFunc(func(context.Context, string, string) CommandResult {
				return CommandResult{Output: "user\n" + prompt + "\nERROR: authentication failed", Err: errors.New("exit status 1")}
			})
			other := failoverRunnerFunc(func(context.Context, string, string) CommandResult {
				t.Error("prompt echo triggered failover")
				return CommandResult{}
			})
			d := delegatingRunner{claude: first, codex: other, provider: func() (DelegationProvider, error) { return p, nil }}
			if p == DelegationProviderCodex {
				d.claude = other
				d.codex = first
			}
			r := d.RunClaude(context.Background(), "/repo", prompt)
			if r.Err == nil {
				t.Fatal("ordinary failure hidden")
			}
			var attempt *delegationAttemptError
			if !errors.As(r.Err, &attempt) {
				t.Fatal("managed non-quota classification lost")
			}
			if _, ok := readSharedBackoff(backoffPathFor(p)); ok {
				t.Fatal("false cooldown")
			}
		})
	}
}

func TestRateLimitFailoverClaudeCancellationKillsChildren(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "claude")
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nsleep 2 &\nwait\n"), 0755); err != nil {
		t.Fatal(err)
	}
	d := delegatingRunner{claude: execClaudeRunner{Bin: bin}, codex: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		t.Error("cancellation triggered failover")
		return CommandResult{}
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := d.RunClaude(ctx, dir, "prompt")
	if !errors.Is(r.Err, context.DeadlineExceeded) {
		t.Fatalf("%+v", r)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Claude children held pipes past cancellation: %v", elapsed)
	}
}

func TestRateLimitFailoverExpiredPrimaryReopens(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	writeSharedBackoff(backoffPathFor(DelegationProviderCodex), time.Now().Add(-time.Minute), "test")
	calls := 0
	d := delegatingRunner{provider: func() (DelegationProvider, error) { return DelegationProviderCodex, nil }, codex: failoverRunnerFunc(func(context.Context, string, string) CommandResult { calls++; return CommandResult{Output: "ok"} })}
	if r := d.RunClaude(context.Background(), "/repo", "prompt"); r.Err != nil || calls != 1 {
		t.Fatalf("primary starved: %+v calls=%d", r, calls)
	}
}
