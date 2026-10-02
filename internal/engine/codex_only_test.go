package engine

import (
	"context"
	"testing"
)

func TestCodexOnlyPolicyRejectsClaudeAndFailover(t *testing.T) {
	for _, policy := range []string{"", "true", "malformed"} {
		t.Run(policy, func(t *testing.T) {
			t.Setenv("BT_SUPERPOWERS_CODEX_ONLY", policy)
			t.Setenv("BT_SUPERPOWERS_PROVIDER", "claude")
			t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
			if _, err := resolvedSuperpowersProvider(); err == nil {
				t.Fatal("Claude configuration accepted")
			}
			if rateLimitFailoverEnabled() {
				t.Fatal("cross-provider failover enabled")
			}
			claude, codex := &routingClaudeRunner{}, &fakeCodexRunner{}
			d := delegatingRunner{claude: claude, codex: codex, provider: func() (DelegationProvider, error) { return DelegationProviderClaude, nil }}
			if res := d.RunClaude(context.Background(), t.TempDir(), "test"); res.Err == nil {
				t.Fatal("injected provider bypassed policy")
			}
			if claude.calls != 0 || codex.calls != 0 {
				t.Fatal("rejected request invoked a runner")
			}
			if res := (execClaudeRunner{Bin: "/nonexistent"}).RunClaude(context.Background(), t.TempDir(), "test"); res.Err == nil || res.Command != "" {
				t.Fatal("direct Claude adapter bypassed policy")
			}
			t.Setenv("BT_SUPERPOWERS_PROVIDER", "")
			if p, err := resolvedSuperpowersProvider(); err != nil || p != DelegationProviderCodex {
				t.Fatalf("default=%s err=%v", p, err)
			}
		})
	}
}

func TestCodexOnlyQuotaFailureDoesNotInvokeClaude(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_CODEX_ONLY", "true")
	t.Setenv("BT_SUPERPOWERS_RATE_LIMIT_FAILOVER", "true")
	claude, codex := &routingClaudeRunner{}, &fakeCodexRunner{err: errInvalidProvider{}, out: "usage limit reached"}
	d := delegatingRunner{claude: claude, codex: codex, provider: func() (DelegationProvider, error) { return DelegationProviderCodex, nil }}
	if res := d.RunClaude(context.Background(), t.TempDir(), "test"); res.Err == nil {
		t.Fatal("quota failure swallowed")
	}
	if codex.calls != 1 || claude.calls != 0 {
		t.Fatalf("calls codex=%d claude=%d", codex.calls, claude.calls)
	}
}
