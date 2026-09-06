# Coding Delegation Providers (Claude / Codex)

The goap-fusion platform delegates autonomous coding work — implementation,
review, and PR-shepherd CI repair — to an external coding CLI. Historically
that CLI was always **Claude Code**. It is now pluggable: the same delegation
seams can run against **OpenAI Codex CLI** instead.

The selection is controlled by a single environment variable:

| Env var | Values | Default | Effect |
|---|---|---|---|
| `BT_SUPERPOWERS_PROVIDER` | `claude` \| `codex` | `claude` | Which CLI every delegation seam invokes |

- Unset / empty → **claude** (fully backwards compatible).
- `claude` → Claude Code (`--print`).
- `codex` → Codex CLI (`codex exec`).
- Case-insensitive. Any other value is **rejected** (the affected delegation
  fails with a clear message) rather than silently defaulting to Claude.

## What routes through the selector

`internal/engine/superpowers_provider.go` defines `delegatingRunner`, a single
`RunClaude`-shaped seam that dispatches to either provider. Every delegation
path is wired through it:

| Seam | Runner | Claude mode | Codex mode |
|---|---|---|---|
| Implementation (RED/GREEN, design loop, brainstorm, program seeding, arc42/drift doc sync) | `newImplementationDelegatingRunner()` | `--print`, default tool list | `--sandbox workspace-write` |
| PR shepherd CI repair | `newImplementationDelegatingRunnerWithTools(...)` | `--print`, extended tool list (`BT_PR_SHEPHERD_ALLOWED_TOOLS`) | `--sandbox workspace-write` |
| Read-only review/proposal (error handler, GOAP review fallback, self review) | `newReadOnlyDelegatingRunner(...)` | `--print`, caller's restricted `--allowedTools`, `ForceReadOnly` | `--sandbox read-only`, `ForceReadOnly` |

The read-only contract is enforced **per provider**: the Claude side pins the
caller's restricted tool list even when `BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS`
is set, and the Codex side pins `--sandbox read-only` regardless of the
`BT_SUPERPOWERS_CODEX_SANDBOX` env. A configuration mistake can never widen a
review run into a write-capable session.

## Environment reference

### Provider selection

| Env var | Values | Default |
|---|---|---|
| `BT_SUPERPOWERS_PROVIDER` | `claude` \| `codex` | `claude` |

### Claude Code

| Env var | Effect | Default |
|---|---|---|
| `BT_SUPERPOWERS_CLAUDE_BIN` | Claude CLI binary | `/home/nico/.local/bin/claude` |
| `BT_SUPERPOWERS_CLAUDE_MODEL` | `--model`; `auto`/`default`/`none` omits the flag | `claude-opus-5` |
| `BT_SUPERPOWERS_CLAUDE_EFFORT` | `--effort`; `auto`/`default`/`none` omits the flag | `max` |
| `BT_SUPERPOWERS_CLAUDE_ALLOWED_TOOLS` | `--allowedTools` override (implementation seams) | see `defaultSuperpowersAllowedTools` |
| `BT_SUPERPOWERS_CLAUDE_SKIP_PERMISSIONS` | `true` → `--dangerously-skip-permissions` (never on read-only seams) | unset |

### Codex CLI

| Env var | Effect | Default |
|---|---|---|
| `BT_SUPERPOWERS_CODEX_BIN` | Codex CLI binary | `/mnt/ssd/npm-global/bin/codex` |
| `BT_SUPERPOWERS_CODEX_MODEL` | `-m`; `auto`/`default`/`none` omits the flag | `gpt-6-astra` |
| `BT_SUPERPOWERS_CODEX_SANDBOX` | `--sandbox` (implementation seams; review seams always pin `read-only`) | `workspace-write` |

### Per-seam tool overrides

| Env var | Seam |
|---|---|
| `BT_GOAP_REVIEW_ALLOWED_TOOLS` | GOAP review fallback read-only tool list |
| `BT_SELF_REVIEW_ALLOWED_TOOLS` | Self-review read-only tool list |
| `BT_PR_SHEPHERD_ALLOWED_TOOLS` | PR shepherd write tool list |

### Rate-limit backoff

| Env var | Provider | Default |
|---|---|---|
| `BT_GOAP_CLAUDE_BACKOFF` | Claude fallback window (no reset hint in output) | `6h` |
| `BT_GOAP_CODEX_BACKOFF` | Codex fallback window | `1h` |

## Provider-namespaced backoff state

Rate-limit cooldowns are durable (fleet-wide) so a rate-limited outcome in one
cron tick stops the next ticks of **every** agent from burning a doomed retry
budget. The state is keyed by provider (`internal/engine/goap_claude_backoff.go`):

- Claude → `~/.go-bt-evolve/claude_backoff.json` (historical path).
- Codex → `~/.go-bt-evolve/codex_backoff.json`.

A Claude rate limit never closes a Codex run, a Codex failure never writes or
clears the Claude cooldown, and vice versa. This is pinned by
`TestDelegationBackoffState_ProviderNamespaced` and
`TestDelegationBackoffState_ClearCodexLeavesClaudeIntact`.

## Configuring and restarting the daemon

The daemon is the systemd **user** unit `bt-agent.service` (running
`bt-agent --no-mcp`). Its environment is fixed from the unit's
`EnvironmentFile` at process start.

```bash
# 1. Edit the unit's EnvironmentFile (operator-managed, outside the repo):
#    ~/.config/systemd/user/bt-agent.service  (or its EnvironmentFile= path)
#    Add or change:
#      BT_SUPERPOWERS_PROVIDER=codex
#    (plus any Codex bin/model/sandbox vars you need)

# 2. Reload the unit definition and restart the daemon.
systemctl --user daemon-reload
systemctl --user restart bt-agent.service

# 3. Confirm the service came back and is using the new provider.
systemctl --user status bt-agent.service --no-pager | head -8
journalctl --user -u bt-agent.service -n 50 --no-pager | grep -iE "provider|codex|claude"
```

### Why a restart is required (not hot-reload)

`resolvedSuperpowersProvider()` reads `BT_SUPERPOWERS_PROVIDER` from the process
environment on **every call** — that is a *process-local* property. It lets a
long-lived test or an in-process config swap reroute on the next call. It does
**not** mean the deployed daemon hot-reloads its environment: the systemd unit's
`EnvironmentFile` is read once at process start, so changing the env var there
still requires the `systemctl --user restart bt-agent.service` above.

## Verifying the switch

The scheduled-cycle preflight `VerifyScheduledGoapFusionRuntime` now checks the
**configured provider's** binary (not a hardcoded Claude path) and fails with a
clear diagnosis when the provider env is invalid or the binary is missing/non-
executable. A successful switch shows:

```
## Scheduled GOAP Fusion Runtime Preflight Passed
Repository: `/home/nico/go-bt-evolve`
Delegation provider: `codex`
Binary: `/mnt/ssd/npm-global/bin/codex`
```

If `BT_SUPERPOWERS_PROVIDER` is set to anything other than `claude`/`codex`,
the preflight fails and the affected delegation seams reject the configuration
instead of defaulting to Claude.
