# ADR-003: File-Based Persistence over SQL

**Status:** Accepted
**Date:** 2026-05-27
**Deciders:** Nico (via Hermes Agent)

## Context

The platform needed persistence for reflections, agent definitions, run history, scheduler state, and dead letter queue. Options:
1. **SQLite** — queryable, transactional, but adds CGO dependency on ARM
2. **File-based JSON** — simple, no deps, git-friendly, but no query support

## Decision

Use JSON files for all persistence. Each domain gets its own directory under `~/.go-bt-evolve/`. File format is newline-delimited JSON for append-only logs (history), single JSON arrays for state (DLQ, queue, scheduler).

## Consequences

- **Positive**: Zero dependencies — works on any platform including Jetson ARM64
- **Positive**: Human-readable and git-friendly for debugging
- **Positive**: Atomic writes (write to .tmp, rename) prevent corruption
- **Negative**: No query support — filtering requires full file scan
- **Negative**: Large history files (>100K entries) may need pagination
