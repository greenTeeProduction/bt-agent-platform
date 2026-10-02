package engine

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nico/go-bt-evolve/internal/config"
)

// DelegationProvider identifies which external coding CLI the platform
// delegates to. Codex is the default. Legacy Claude transport is available
// only when an operator explicitly disables the Codex-only policy.
//
// (Named DelegationProvider, not Provider, to avoid colliding with the BT
// action/condition registration interface `Provider` in registry.go.)
type DelegationProvider string

const (
	DelegationProviderClaude DelegationProvider = "claude"
	DelegationProviderCodex  DelegationProvider = "codex"
)

// Valid reports whether p names a supported delegation provider.
func (p DelegationProvider) Valid() bool {
	return p == DelegationProviderClaude || p == DelegationProviderCodex
}

// codexOnlyDelegation defaults closed: unset or malformed configuration cannot
// enable another coding provider. The legacy transport opt-out is explicit.
func codexOnlyDelegation() bool {
	if config.SolOnly() {
		return true
	}
	raw := strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_CODEX_ONLY"))
	enabled, err := strconv.ParseBool(raw)
	return err != nil || enabled
}

// resolvedSuperpowersProvider reads the process environment on each call.
// Service environment changes require a restart; Codex is the unset default.
func resolvedSuperpowersProvider() (DelegationProvider, error) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("BT_SUPERPOWERS_PROVIDER")))
	if raw == "" {
		return DelegationProviderCodex, nil
	}
	p := DelegationProvider(raw)
	if !p.Valid() {
		return "", fmt.Errorf("invalid BT_SUPERPOWERS_PROVIDER %q: must be %q or %q", raw, DelegationProviderClaude, DelegationProviderCodex)
	}
	if codexOnlyDelegation() && p != DelegationProviderCodex {
		return "", fmt.Errorf("codex-only delegation rejects provider %q", p)
	}
	return p, nil
}

// delegationBinary returns the configured provider's CLI binary path: the
// matching BT_SUPERPOWERS_CLAUDE_BIN / BT_SUPERPOWERS_CODEX_BIN env var when
// set, otherwise the historical default. It mirrors the fallback the exec
// runners (execClaudeRunner / execCodexRunner) apply, so preflights check the
// exact binary a delegation will actually invoke.
func delegationBinary(p DelegationProvider) string {
	if p == DelegationProviderCodex {
		return getenvDefault("BT_SUPERPOWERS_CODEX_BIN", "/home/nico/.local/bin/codex")
	}
	return getenvDefault("BT_SUPERPOWERS_CLAUDE_BIN", "/home/nico/.local/bin/claude")
}

// delegatingRunner is the shared provider-selection point. It implements
// ClaudeRunner (the historical interface every delegation seam already calls)
// and routes each call to either the Claude or the Codex exec runner based on
// the configured provider, so flipping BT_SUPERPOWERS_PROVIDER reroutes every
// wired path without touching call sites.
type delegatingRunner struct {
	claude ClaudeRunner
	codex  CodexRunner
	// provider resolves the active provider; nil falls back to
	// resolvedSuperpowersProvider (env). Tests inject a stub.
	provider func() (DelegationProvider, error)
}

func (d delegatingRunner) RunClaude(ctx context.Context, repoDir string, prompt string) CommandResult {
	if d.claude == nil {
		d.claude = execClaudeRunner{}
	}
	if d.codex == nil {
		d.codex = execCodexRunner{}
	}
	if d.provider == nil {
		d.provider = resolvedSuperpowersProvider
	}
	p, err := d.provider()
	if err != nil {
		return CommandResult{Command: "delegation", Dir: repoDir, Err: err}
	}
	if rateLimitFailoverEnabled() {
		return d.runWithRateLimitFailover(ctx, repoDir, prompt, p)
	}
	return d.runProvider(ctx, repoDir, prompt, p)
}

// newImplementationDelegatingRunner builds the default write-capable
// delegation runner for the implementation seams (RED/GREEN, design loop,
// brainstorm expansion, program seeding, arc42/drift doc sync). Claude keeps
// its full write tool list; Codex uses --sandbox workspace-write.
func newImplementationDelegatingRunner() delegatingRunner {
	return delegatingRunner{
		claude:   execClaudeRunner{},
		codex:    execCodexRunner{},
		provider: resolvedSuperpowersProvider,
	}
}

// newImplementationDelegatingRunnerWithTools is the write-capable delegation
// runner for seams that pin their own Claude --allowedTools list rather than
// the shared default (the PR shepherd, whose allowed list extends the default
// with `go mod tidy` / `make check-quick` for CI repair). Claude keeps the
// caller's explicit tool list; Codex uses --sandbox workspace-write. Provider
// selection routes through it like every other delegation seam.
func newImplementationDelegatingRunnerWithTools(claudeAllowedTools string) delegatingRunner {
	return delegatingRunner{
		claude:   execClaudeRunner{AllowedTools: claudeAllowedTools},
		codex:    execCodexRunner{},
		provider: resolvedSuperpowersProvider,
	}
}

// newReadOnlyDelegatingRunner builds the read-only delegation runner for the
// review/proposal seams (error handler, GOAP review fallback, self review).
// The read-only contract is enforced per provider: Claude is pinned to the
// caller's restricted --allowedTools list with ForceReadOnly (so the
// skip-permissions env override cannot widen it), and Codex is pinned to
// --sandbox read-only with ForceReadOnly.
func newReadOnlyDelegatingRunner(allowedTools string) delegatingRunner {
	return delegatingRunner{
		claude:   execClaudeRunner{AllowedTools: allowedTools, ForceReadOnly: true},
		codex:    execCodexRunner{Sandbox: "read-only", ForceReadOnly: true},
		provider: resolvedSuperpowersProvider,
	}
}
