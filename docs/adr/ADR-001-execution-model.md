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
