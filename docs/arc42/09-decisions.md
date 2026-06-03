# arc42 Section 9 — Architecture Decisions

## ADR-001: Behavior Trees as Core Execution Model

**Context:** We needed a deterministic, composable execution model for AI agent workflows. Simple linear scripts couldn't handle branching, retry, or fallback. Full GOAP was over-engineered for most tasks.

**Decision:** Use behavior trees with Sequence, Selector, Action, Condition, ChainAction, and decorator nodes. All agent logic is a tree. The Blackboard pattern carries shared state through ticks.

**Status:** Accepted (2026-05-26)

**Consequences:**
- ✅ Composable: PreGate→StrategyRouter→OutcomeSelector is the universal pattern
- ✅ Evolvable: Trees are data — mutation, crossover, and versioning work naturally
- ✅ Testable: Each node type has clear contracts
- ⚠️ Learning curve: Developers must understand BT semantics (ticks, Running state, Selector fallthrough)
- ⚠️ 1000-tick safety limit can abort long-running agent loops

## ADR-002: MCP as External Interface

**Context:** Hermes Agent is a Python process. The BT platform is Go. We needed a protocol for them to communicate.

**Decision:** Use Model Context Protocol (MCP) — JSON-RPC 2.0 over stdio. Go binaries run as MCP servers spawned by the Hermes gateway.

**Status:** Accepted (2026-05-26)

**Consequences:**
- ✅ Standardized: No custom protocol design needed
- ✅ Gateway-managed: Lifecycle (spawn, health check, restart) handled by Hermes
- ✅ Discoverable: Tools are declared and introspectable
- ⚠️ Stdio-only: No HTTP/SSE transport — limits remote access (by design)
- ⚠️ Gateway restart needed for MCP binary updates (reload doesn't respawn children)

## ADR-003: File-Based Persistence over SQL

**Context:** We needed to persist agent state, history, and configuration. SQLite was considered but rejected for simplicity.

**Decision:** JSON/YAML files with atomic writes (write .tmp → rename) in `~/.go-bt-evolve/`. No database dependency.

**Status:** Accepted (2026-05-27)

**Consequences:**
- ✅ Git-friendly: Agent definitions are versionable YAML
- ✅ Zero dependencies: No database driver or migration tooling
- ✅ Debuggable: Any text editor can inspect state
- ⚠️ No query capability: List/filter operations are O(n) scans
- ⚠️ Concurrent writes risk: Mitigated by per-agent file granularity and mutexes

## ADR-004: YAML-Defined Agent Platform

**Context:** Agents need metadata (name, tree, schedule, I/O contracts, quality gates) separate from the tree definition itself. We needed a registry, scheduler, and catalog.

**Decision:** Agents are YAML files in `~/.go-bt-evolve/agents/`. The Registry loads them on startup. The Scheduler runs them on cron schedules. The Catalog provides browsing/installation from templates.

**Status:** Accepted (2026-05-27)

**Consequences:**
- ✅ Declarative: Agent config is human-readable YAML
- ✅ Template marketplace: 24 template agents in `agents/templates/`
- ✅ Scheduler persistence: FileJobStore survives restarts
- ⚠️ Registry is in-memory: All agents loaded at startup (O(n) memory)
- ⚠️ No hot-reload: Agent YAML changes require restart or explicit reload

## ADR-005: Stockfish-Adapted Evolution Engine

**Context:** Behavior trees can degrade with random mutations. We needed an evolution engine that systematically improves trees across multiple fitness dimensions.

**Decision:** Adapt Stockfish chess engine techniques — transposition table for caching evaluated mutations, move ordering by predicted fitness delta, alpha-beta pruning for search. Combine with Pareto front for multi-objective optimization and MAP-Elites for quality diversity.

**Status:** Accepted (2026-05-27)

**Consequences:**
- ✅ Six algorithms covering different optimization strategies
- ✅ Git-versioned: Every accepted mutation is a commit
- ✅ Reversible: Rollback on regression
- ⚠️ 97.3% mutation regression rate: Quality gates needed (see R1 in Section 11)
- ⚠️ Per-tree fitness still evolving (reflection.FilterByTreeName)

## ADR-006: ChainAction — LLM Integration via BT Nodes

**Context:** LLM calls were initially ad-hoc. We needed them as first-class behavior tree nodes so they benefit from PreGate gating, retry, and StrategyRouter selection.

**Decision:** ChainAction nodes wrap LLM calls in the behavior tree. 10 chain types cover single calls, agent loops, RAG, tool use, and multi-step workflows. Configuration is read from node Name and Metadata.

**Status:** Accepted (2026-05-28)

**Consequences:**
- ✅ LLM calls are BT-composable: gated, retried, selected
- ✅ Template variables enable context injection ({{.Task}}, {{.ChainState.*}})
- ✅ 10 chain types cover diverse LLM workflows
- ⚠️ ChainAction panic recovery needed SafeGo wrapper
- ⚠️ max_tokens audit detected nodes with max_tokens=1 (aspirational fix)

## ADR-007: Reliability Architecture — Circuit Breakers, Retry, DLQ

**Context:** LLM calls fail transiently (Ollama OOM, API rate limits). Goroutines can panic (nil dereference in chain processing). Failed tasks must not be silently lost.

**Decision:** Three-layer reliability: SafeGo (panic recovery in all goroutines), CircuitBreaker (3-state: closed/open/half-open with per-agent isolation), RetryWithBackoff (full jitter, 3 classes: standard, LLM, unknown), DeadLetterQueue (persistent JSON for exhausted retries).

**Status:** Accepted (2026-05-29)

**Consequences:**
- ✅ Graceful degradation: Single failure doesn't cascade
- ✅ Failed work preserved: DLQ enables manual inspection and replay
- ✅ Per-agent circuit breakers: One misbehaving agent doesn't block others
- ⚠️ Retry delays add latency (1s→2s→4s→8s backoff)
- ⚠️ DLQ grows unbounded without cleanup

---

*Generated by bt-agent arc42 pipeline — section9Decisions tree*
