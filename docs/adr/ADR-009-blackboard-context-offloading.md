# ADR-009: Scoped Blackboard for Context Offloading

**Status:** Accepted  
**Date:** 2026-06-14  
**Deciders:** Platform team

## Context

Agent runs accumulated context in three places with no shared model:

1. **Prompt stuffing** — `injectMemoryContext()` appended full memory blocks and prior run outputs to every task string.
2. **Ephemeral engine blackboard** — `engine.Blackboard` held `Task`, `Plan`, `Result`, and unstructured `ChainState` for a single tree tick loop only.
3. **Pipeline string passing** — workflow steps exchanged outputs via `wfState.prev` template expansion, not a queryable store.

Agents could not read/write structured context during ReAct loops, operators had no MCP access to run/session state, and large histories inflated prompts.

## Decision

Introduce `internal/blackboard/` with a **Manager** partitioning key-value **Entries** by scope:

| Scope | Lifetime | Persistence |
|-------|----------|-------------|
| `run` | Single agent execution | In-memory only |
| `session` | Workflow/pipeline run ID | JSON under `${AGENT_HOME}/blackboard/session/` |
| `agent` | Agent name | JSON under `${AGENT_HOME}/blackboard/agent/` |

Each `RunOnce` attaches a `blackboard.Handle` to `engine.Blackboard` and registers ReAct tools: `bb_read`, `bb_write`, `bb_list`, plus `bb_session_*` when a pipeline session ID is present.

Memory/history injection **seeds run-scoped keys** (`memory/*`, `history/runs`) and leaves a short `bb_read` hint in the task instead of full payloads.

Workflow orchestrator promotes step outputs to session keys (`steps/{id}/output`, `prev/output`, `input`).

Chain templates support `{{.BB.run_id}}`, `{{.BB.session_id}}`, `{{.BB.agent}}`, and `{{.BB.<key>}}` (run scope, summary when large).

MCP exposes `bt_bb_read`, `bt_bb_write`, `bt_bb_list`, `bt_bb_delete` for operator and external agent access.

## Consequences

- **Positive**: Large context moves off-prompt; agents fetch on demand via tools or templates.
- **Positive**: Pipeline steps share session-scoped state without copying megabyte strings between YAML templates.
- **Positive**: Aligns with ADR-003 file persistence — no new database.
- **Negative**: Run-scoped data is lost after execution unless copied to session/agent scope explicitly.
- **Negative**: Template `{{.BB.key}}` returns summaries for large values; full payload requires `bb_read`.
- **Negative**: Agent-scoped persistence is available but not yet auto-populated from runs (manual/MCP only).

## Related

- [ADR-003 File-Based Persistence](./ADR-003-file-persistence.md)
- [ADR-004 YAML-Defined Agent Platform](./ADR-004-agent-platform.md)
- Operator guide: [docs/agents.md](../agents.md) — Blackboard section
