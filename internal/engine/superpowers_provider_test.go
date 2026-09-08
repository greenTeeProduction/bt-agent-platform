package engine

import (
	"context"
	"strings"
	"testing"
)

func TestResolvedSuperpowersProviderDefaultsToClaude(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "")

	p, err := resolvedSuperpowersProvider()
	if err != nil {
		t.Fatalf("resolvedSuperpowersProvider() err = %v, want nil", err)
	}
	if p != DelegationProviderClaude {
		t.Fatalf("resolvedSuperpowersProvider() = %q, want claude (backwards-compatible default)", p)
	}
}

func TestResolvedSuperpowersProviderCodex(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "codex")

	p, err := resolvedSuperpowersProvider()
	if err != nil {
		t.Fatalf("resolvedSuperpowersProvider() err = %v, want nil", err)
	}
	if p != DelegationProviderCodex {
		t.Fatalf("resolvedSuperpowersProvider() = %q, want codex", p)
	}
}

func TestResolvedSuperpowersProviderInvalid(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "gemini")

	if _, err := resolvedSuperpowersProvider(); err == nil {
		t.Fatalf("resolvedSuperpowersProvider() err = nil, want error for invalid provider")
	}
}

func TestResolvedSuperpowersProviderCaseInsensitive(t *testing.T) {
	t.Setenv("BT_SUPERPOWERS_PROVIDER", "CODEX")

	p, err := resolvedSuperpowersProvider()
	if err != nil {
		t.Fatalf("resolvedSuperpowersProvider() err = %v, want nil", err)
	}
	if p != DelegationProviderCodex {
		t.Fatalf("resolvedSuperpowersProvider() = %q, want codex", p)
	}
}

// fakeCodexRunner records RunCodex calls.
type fakeCodexRunner struct {
	calls int
	out   string
	err   error
}

func (f *fakeCodexRunner) RunCodex(_ context.Context, _ string, _ string) CommandResult {
	f.calls++
	return CommandResult{Output: f.out, Err: f.err}
}

// routingClaudeRunner records RunClaude calls (parallel to fakeReviewClaudeRunner).
type routingClaudeRunner struct {
	calls int
}

func (f *routingClaudeRunner) RunClaude(_ context.Context, _ string, _ string) CommandResult {
	f.calls++
	return CommandResult{}
}

func TestDelegatingRunnerRoutesToCodex(t *testing.T) {
	codex := &fakeCodexRunner{out: "codex-result"}
	claude := &routingClaudeRunner{}
	d := delegatingRunner{
		claude:   claude,
		codex:    codex,
		provider: func() (DelegationProvider, error) { return DelegationProviderCodex, nil },
	}

	res := d.RunClaude(context.Background(), "/repo", "prompt")
	if res.Output != "codex-result" {
		t.Fatalf("delegating RunClaude output = %q, want codex-result", res.Output)
	}
	if codex.calls != 1 {
		t.Fatalf("codex calls = %d, want 1", codex.calls)
	}
	if claude.calls != 0 {
		t.Fatalf("claude calls = %d, want 0 when provider is codex", claude.calls)
	}
}

func TestDelegatingRunnerRoutesToClaude(t *testing.T) {
	codex := &fakeCodexRunner{}
	claude := &routingClaudeRunner{}
	d := delegatingRunner{
		claude:   claude,
		codex:    codex,
		provider: func() (DelegationProvider, error) { return DelegationProviderClaude, nil },
	}

	d.RunClaude(context.Background(), "/repo", "prompt")
	if claude.calls != 1 {
		t.Fatalf("claude calls = %d, want 1 when provider is claude", claude.calls)
	}
	if codex.calls != 0 {
		t.Fatalf("codex calls = %d, want 0 when provider is claude", codex.calls)
	}
}

func TestDelegatingRunnerInvalidProviderReturnsError(t *testing.T) {
	codex := &fakeCodexRunner{}
	claude := &routingClaudeRunner{}
	d := delegatingRunner{
		claude:   claude,
		codex:    codex,
		provider: func() (DelegationProvider, error) { return "", errInvalidProvider{} },
	}

	res := d.RunClaude(context.Background(), "/repo", "prompt")
	if res.Err == nil {
		t.Fatalf("delegating RunClaude err = nil, want invalid-provider error")
	}
	if !strings.Contains(res.Err.Error(), "invalid provider") {
		t.Fatalf("delegating RunClaude err = %q, want it to mention invalid provider", res.Err)
	}
	if codex.calls != 0 || claude.calls != 0 {
		t.Fatalf("no runner may be invoked on an invalid provider (codex=%d claude=%d)", codex.calls, claude.calls)
	}
}

// errInvalidProvider is a minimal error for the routing test above.
type errInvalidProvider struct{}

func (errInvalidProvider) Error() string { return "invalid provider" }
