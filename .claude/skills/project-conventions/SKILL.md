---
name: project-conventions
description: BT Agent Platform architecture and merge conventions — package consolidation mapping, engine import-cycle rules, persistence format. Apply when merging upstream code, moving code between packages, or adding engine dependencies.
user-invocable: false
---

# BT Agent Platform Conventions

## Package consolidation (merge convention)

The b5c4d00 package consolidation is authoritative. New or upstream code that
references pre-consolidation packages gets rewritten to the current layout:

| Old package | Lives in now |
|-------------|--------------|
| `metrics` | `internal/dashboard` |
| `reflection` | `internal/evolution` |
| `mcp` | `internal/engine` |
| `finance`, `research` | `internal/evolution` |
| `log` | `internal/engine` |

When merging from origin, the consolidation wins — rewrite incoming imports,
never resurrect the old packages.

## Import cycles into engine

`internal/engine` must not import higher-level packages. When engine code needs
functionality from above, break the cycle with an injection-hook var declared in
engine and wired from `cmd/bt-agent`:

- `engine.DelegateToTreeFn` (`internal/engine/delegate_hooks.go`)
- `engine.RecordNodeTickFn` (`internal/engine/metrics_hooks.go`)

Follow that pattern for new cross-layer calls: nil-checked function var in engine,
assignment at startup in `cmd/bt-agent`.

## Persistence (ADR-003)

State is persisted as JSON files under `~/.go-bt-evolve/`, written atomically via
tmp-file + rename. New persistence code follows this — no databases, no partial
writes.

## Toolchain

Go 1.26 is at `/usr/local/go/bin/go`, not on the non-interactive PATH. Prefix
commands: `PATH=/usr/local/go/bin:$PATH go test ...`. The Makefile hardcodes this
path already.
