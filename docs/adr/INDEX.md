# ADR-001: Behavior Trees as Core Execution Model

**Status:** Accepted  
**Date:** 2026-05-26  
**Deciders:** Nico (via Hermes Agent)

## Context

The platform needed a structured execution model for autonomous AI agents. Options considered:
1. **Raw LLM chains** — flexible but unstructured; hard to debug or evolve
2. **Finite state machines** — predictable but rigid; hard to compose
3. **Behavior trees** — composable, observable, evolvable; standard in game AI/robotics

## Decision

Use behavior trees (via `rvitorper/go-bt`) as the core execution model. Trees compose Sequence, Selector, Condition, and Action nodes into decision pipelines. LLM integration happens through `ChainAction` leaf nodes.

## Consequences

- **Positive**: Trees are introspectable (dashboard visualization), versionable (git), and evolvable (Stockfish-style mutation ordering)
- **Positive**: Domain experts can author trees declaratively (YAML/JSON) without writing Go
- **Negative**: Tree depth limits expressiveness for open-ended agent loops; mitigated by ReAct-style `agent` chain type
- **Negative**: Trees must be manually ordered for correct routing (most-specific paths first)

---

# ADR-002: MCP as External Interface

**Status:** Accepted  
**Date:** 2026-05-26  
**Deciders:** Nico (via Hermes Agent)

## Context

The platform needed to integrate with Hermes Agent for autonomous task execution. Options:
1. **REST API** — simple but requires polling for long-running tasks
2. **gRPC** — fast but complex to set up with Hermes' Python runtime
3. **MCP (Model Context Protocol)** — stdio-based, JSON-RPC 2.0, designed for tool servers

## Decision

Expose all platform capabilities via MCP stdio servers. Three servers: `bt-agent` (core tools), `bt-evaluator` (evolution), `bt-langagent` (ReAct agent).

## Consequences

- **Positive**: Zero network config — stdio transport works instantly
- **Positive**: Hermes auto-discovers tools; no manual registration needed after initial setup
- **Negative**: MCP is single-client per process; multi-client needs connection pooling
- **Negative**: Stdio transport means servers must be child processes of the gateway

---

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
