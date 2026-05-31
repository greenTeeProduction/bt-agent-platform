# ADR-004: YAML-Defined Agent Platform

**Status:** Accepted
**Date:** 2026-05-27
**Deciders:** Nico (via Hermes Agent)

## Context

The platform needed a way to manage agents beyond raw tree execution. Agents require lifecycle management (create, schedule, monitor), quality scoring, run history, and cross-tree orchestration. Options:

1. **Raw tree files only** — simple but no lifecycle; agents are stateless, cannot be scheduled
2. **Agent definitions in Go code** — version-controlled but requires recompilation for each agent change
3. **YAML-defined agents with runtime registry** — declarative, hot-reloadable, versionable

## Decision

Implement an agent platform layered on top of behavior trees. Agents are defined in YAML files (`~/.go-bt-evolve/agents/<name>.yaml`) with fields for tree ID, schedule, timeout, retries, and metadata. The runtime (`internal/agent/`) provides:

- **Registry**: Create, Get, List, Delete agents with YAML persistence
- **Catalog**: Browse templates, search, install, export, skill→agent compilation
- **Scheduler**: Cron-based recurring execution with history recording and crash recovery (InFlight flag)
- **History**: JSONL run records with aggregate statistics (success rate, avg duration, quality score)
- **Workflow Orchestrator**: Sequential, parallel, conditional, loop execution across multiple agents

## Consequences

- **Positive**: Agents are declarative and versionable — YAML can be checked into git alongside tree definitions
- **Positive**: Template system enables rapid agent creation (10 built-in templates)
- **Positive**: Scheduler crash recovery (InFlight flag persisted at job start) survives bt-agent restarts
- **Positive**: Agent quality scoring composites success rate, output quality, speed, and robustness
- **Negative**: YAML schema is evolving; backward-compatible parsing required for deployed agents
- **Negative**: Concurrent agent execution is bounded by Jetson memory (qwen3.6:35b ~24 GB/instance)
