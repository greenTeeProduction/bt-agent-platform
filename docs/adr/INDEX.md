# Architecture Decision Records

| ADR | Title | Status | Date |
|---|---|---|---|
| [ADR-001](./ADR-001-execution-model.md) | Behavior Trees as Core Execution Model | Accepted | 2026-05-26 |
| [ADR-002](./ADR-002-mcp-interface.md) | MCP as External Interface | Accepted | 2026-05-26 |
| [ADR-003](./ADR-003-file-persistence.md) | File-Based Persistence over SQL | Accepted | 2026-05-27 |
| [ADR-004](./ADR-004-agent-platform.md) | YAML-Defined Agent Platform | Accepted | 2026-05-27 |
| [ADR-005](./ADR-005-evolution-engine.md) | Stockfish-Adapted Evolution Engine | Accepted | 2026-05-27 |
| [ADR-006](./ADR-006-chainaction-architecture.md) | ChainAction — LLM Integration via BT Nodes | Accepted | 2026-05-28 |
| [ADR-007](./ADR-007-reliability-architecture.md) | Reliability Architecture — Circuit Breakers, Retry, DLQ | Accepted | 2026-05-29 |

## What is an ADR?

Architecture Decision Records document significant design choices with:
- **Context** — the problem being solved and alternatives considered
- **Decision** — the chosen approach and rationale
- **Consequences** — positive and negative outcomes of the decision

ADRs are immutable once accepted. Superseded decisions should be marked as `Superseded` with a link to the replacement ADR.

## Status Values

- **Proposed**: Under discussion
- **Accepted**: Approved and implemented
- **Deprecated**: Replaced by a newer ADR
- **Superseded**: Overridden by a later decision
