package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRateLimitFailoverBothLimitedEarliest(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "claude")
	var calls int
	limited := failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		calls++
		return CommandResult{Err: errors.New("usage limit reached")}
	})
	d := delegatingRunner{claude: limited, codex: limited}
	result := d.RunClaude(context.Background(), "/repo", "prompt")
	if result.Provider != DelegationProviderCodex {
		t.Fatalf("last actual provider=%q", result.Provider)
	}
	var err *DelegationRateLimitError
	if !errors.As(result.Err, &err) {
		t.Fatalf("missing typed error: %+v", result)
	}
	codex, _ := readSharedBackoff(backoffPathFor(DelegationProviderCodex))
	if !result.RetryAt.Truncate(time.Second).Equal(codex) || err.Provider != DelegationProviderCodex {
		t.Fatalf("retry=%v want %v", result.RetryAt, codex)
	}
	d.RunClaude(context.Background(), "/repo", "prompt")
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	bb := &Blackboard{}
	if until, active := delegationPreflightBackoff(bb, DelegationProviderClaude, time.Now()); !active || !until.Equal(codex) {
		t.Fatalf("preflight=%v %v", until, active)
	}
}

func TestRateLimitFailoverPreflightAlternateAvailable(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	bb := &Blackboard{ChainState: map[string]any{}}
	saveDelegationBackoffState(bb, DelegationProviderCodex, time.Now().Add(time.Hour))
	if _, active := delegationPreflightBackoff(bb, DelegationProviderCodex, time.Now()); active {
		t.Fatal("healthy alternate blocked")
	}
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "false")
	if _, active := delegationPreflightBackoff(bb, DelegationProviderCodex, time.Now()); !active {
		t.Fatal("disabled failover ignored primary cooldown")
	}
}

func TestRateLimitFailoverLegacyCooldown(t *testing.T) {
	isolateBackoffStores(t)
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")
	bb := &Blackboard{ChainState: map[string]any{backoffChainKey(DelegationProviderCodex): time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}}
	if _, active := delegationPreflightBackoff(bb, DelegationProviderCodex, time.Now()); active {
		t.Fatal("alternate should be available")
	}
	d := delegatingRunner{codex: failoverRunnerFunc(func(context.Context, string, string) CommandResult {
		t.Error("legacy-closed primary invoked")
		return CommandResult{}
	}), claude: failoverRunnerFunc(func(context.Context, string, string) CommandResult { return CommandResult{Output: "ok"} })}
	if r := d.RunClaude(context.Background(), "/repo", "prompt"); r.Output != "ok" {
		t.Fatalf("%+v", r)
	}
}

func TestRateLimitFailoverReadOnlyExecutables(t *testing.T) {
	for _, primary := range []DelegationProvider{DelegationProviderClaude, DelegationProviderCodex} {
		t.Run(string(primary), func(t *testing.T) {
			isolateBackoffStores(t)
			t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
			t.Setenv("BT_SUPERPOWERS_PROVIDER", string(primary))
			t.Setenv("BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS", "true")
			dir := t.TempDir()
			for _, p := range []DelegationProvider{DelegationProviderClaude, DelegationProviderCodex} {
				bin := filepath.Join(dir, string(p))
				script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + bin + ".args'\n"
				switch p {
				case primary:
					script += "printf 'usage limit reached\\n'; exit 1\n"
				case DelegationProviderCodex:
					script += codexOutputLastMessageSh
				default:
					script += "printf 'healthy\\n'\n"
				}
				if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
					t.Fatal(err)
				}
				key := "BT_SUPERPOWERS_CLAUDE_BIN"
				if p == DelegationProviderCodex {
					key = "BT_SUPERPOWERS_CODEX_BIN"
				}
				t.Setenv(key, bin)
			}
			r := newReadOnlyDelegatingRunner("Read").RunClaude(context.Background(), dir, "prompt")
			if r.Err != nil || r.Provider != alternateDelegationProvider(primary) {
				t.Fatalf("result=%+v", r)
			}
			codex, _ := os.ReadFile(filepath.Join(dir, "codex.args"))
			claude, _ := os.ReadFile(filepath.Join(dir, "claude.args"))
			if !strings.Contains(string(codex), "--sandbox\nread-only") || !strings.Contains(string(codex), "gpt-5.3-codex-spark") {
				t.Fatalf("codex argv=%s", codex)
			}
			if !strings.Contains(string(claude), "--allowedTools\nRead") || strings.Contains(string(claude), "--dangerously-skip-permissions") {
				t.Fatalf("claude argv=%s", claude)
			}
		})
	}
}
