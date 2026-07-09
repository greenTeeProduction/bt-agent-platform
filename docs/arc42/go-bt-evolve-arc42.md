---
title: "go-bt-evolve Architecture Documentation"
subtitle: "arc42 Template — Behavior Tree Agent Platform"
date: "2026-06-03"
updated: "2026-07-08"
version: "1.1.0"
status: "Generated; full refresh 2026-07-04 — kept current per landing run by the autonomous arc42 sync stage"
arc42_version: "8.2"
repository: "https://github.com/greenTeeProduction/bt-agent-platform"
---

# arc42 Architecture Documentation — go-bt-evolve

## Table of Contents

1. [Introduction and Goals](#arc42-section-1--introduction-and-goals)
2. [Architecture Constraints](#arc42-section-2--architecture-constraints)
3. [Context and Scope](#arc42-section-3--context-and-scope)
4. [Solution Strategy](#arc42-section-4--solution-strategy)
5. [Building Block View](#arc42-section-5--building-block-view)
6. [Runtime View](#arc42-section-6--runtime-view)
7. [Deployment View](#arc42-section-7--deployment-view)
8. [Crosscutting Concepts](#arc42-section-8--crosscutting-concepts)
9. [Architecture Decisions](#arc42-section-9--architecture-decisions)
10. [Quality Requirements](#arc42-section-10--quality-requirements)
11. [Risks and Technical Debt](#arc42-section-11--risks-and-technical-debt)
12. [Glossary](#arc42-section-12--glossary)

---


---

# arc42 Section 1 — Introduction and Goals

## 1.1 Requirements Overview

go-bt-evolve is a Go behavior tree agent platform that provides:

- **BT Execution Engine** — Builds and executes behavior trees with 34 node types: composites (Sequence, Selector, Parallel, ReactiveParallel, MemSequence, MemSelector, PersistentMemSequence, UtilitySelector, BanditSelector, DecisionTree), leaves (Action, Condition, ChainAction, CachedCondition), decorators (Retry, Timeout, Repeater, Inverter, Succeeder, AlwaysSucceed, Runner, CircuitBreaker, RateLimit, Budget, SemaphoreGuard, Monitor, AbortOnEvent, QualityGate, CheckpointVerifier, ReviewCycle, ForEachTask, HumanApprovalGate, SubTreeRef), and planning (PlannerNode).
- **55+ Trees across 8 Categories** — domain (23 trees incl. the goap_fusion / goap_fusion_loop self-improvement runners, bt_fusion research indexer, notebooklm pipeline trees, superpowers_workflow, hermes_update), finance (23), research (deep/quick), startup roles, thinktank (synthesis, peer_review, report), evolution, composed blocks, core.
- **Autonomous Self-Improvement Loop** — the scheduled goap-fusion daemon researches (NotebookLM literature + Claude code review with commits/structure/failures mode rotation), derives up to three file-scoped goals or multi-cycle programs, writes goal-driven multi-task plans, implements them via Claude Code RED→GREEN in isolated worktrees, verifies (tests, build, changed-package suites, lint parity), lands hook-gated commits on the bare master, pushes, and syncs this arc42 document — unattended.
- **Research Memory** — a content-hash-deduplicating knowledge store (`~/.go-bt-evolve/research/knowledge.json`) records every finding, NotebookLM answer, and implemented goal; a program store (`programs.json`) persists multi-cycle change programs executed one milestone per cycle.
- **3 MCP Servers** — bt-agent (63 tools, incl. the deterministic `bt_evolve_qd` MAP-Elites, `bt_evolve_multiobjective` NSGA-II, `bt_evolve_island` island-model, `bt_evolve_bottlenecks` algorithm-routed bottleneck-evolution (CMA-ES parameter tuning with an experience-grounded genetic fallback), `bt_evolve_memetic` GA-plus-local-search, and `bt_evolve_qlearning` Q-table-guided evolution tools), bt-evaluator (5 tools), bt-langagent (3 tools), all via JSON-RPC 2.0 over stdio.
- **Dashboard** — HTTP server on :9800 with 8 tabs (Overview, ThinkTank, Company, Tasks, Tree View, Evolution, Agents, MindMap).
- **Evolution Engine** — Stockfish-adapted mutation ordering, Pareto multi-objective front, MAP-Elites quality diversity, Island Model with migration, Q-Learning epsilon-greedy.
- **Agent Platform** — YAML-defined agents with registry, scheduler, circuit breakers, dead letter queue, A2A (Agent-to-Agent) protocol, memory store, and webhook publishing.
- **Knowledge Graph** — Semantic index of all trees with embeddings, capabilities, and cross-tree relationships for discovery and auto-creation.
- **Factory** — Tree breeding via crossover from parent templates, archetype-based generation.

## 1.2 Quality Goals (Top 3)

| # | Quality Goal | Motivation |
|---|---|---|
| Q1 | **Correctness** | Trees must route correctly through PreGate→StrategyRouter→OutcomeSelector. All 350+ registered engine actions/conditions must register and invoke properly. ChainAction nodes must produce valid LLM output. |
| Q2 | **Evolvability** | The platform must improve over time. Six evolution algorithms (Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert Knowledge) drive mutation and selection. Git-versioned trees enable rollback. Benchmarks gate acceptance. |
| Q3 | **Reliability** | Panic recovery (SafeGo), circuit breakers (3-state), retry with exponential backoff (full jitter), dead letter queue, and output quality validation ensure the platform degrades gracefully rather than failing silently. |

## 1.3 Stakeholders

| Stakeholder | Role | Interests |
|---|---|---|
| Nico | Platform Architect | Fast iteration, BT-first execution, reliable cron automation |
| Hermes Agent | Primary Operator | MCP tools for task delegation, tree discovery, agent management |
| Dashboard Users | Observability Consumers | Tree status, fitness scores, agent history, sprint progress |
| Cron Watchers | Scheduled Automation | Reliable recurring execution with circuit breakers and DLQ |
| Ollama (qwen3.6:35b) | Local LLM Provider | Prompt→completion at :11434, 2-3 min per call |
| DeepSeek API | Escalation LLM Provider | Batch/complex prompts at api.deepseek.com, 5-10s per call |

---

*Generated by bt-agent arc42 pipeline — section1IntroGoals tree*


---

# arc42 Section 2 — Architecture Constraints

## Technical Constraints

| Constraint | Type | Explanation |
|---|---|---|
| Go 1.26.3 | Runtime | Language version enforced by go.mod |
| Platform: Linux ARM64 (Jetson) | Hardware | 12-core NVIDIA Jetson, 61GB RAM, 57GB eMMC + 1.8TB NVMe. No x86-specific optimizations allowed |
| MCP transport: stdio only (ADR-002) | Protocol | All MCP servers use JSON-RPC 2.0 over stdin/stdout. No HTTP/SSE transport |
| File-based persistence (ADR-003) | Storage | JSON files with atomic writes (write .tmp → rename). No SQL database. `~/.go-bt-evolve/` for agents, history, reflections, scheduler, DLQ |
| LLM: Ollama qwen3.6:35b primary | Dependency | Local LLM at localhost:11434. 2-3 min per call. DeepSeek v4-flash as escalation path |
| Single developer (Nico) | Organizational | All code authored and reviewed by one person. No PR approval gates needed |
| 120s task timeout | Runtime | `RunTask()` applies `context.WithTimeout(120s)`. Longer tasks must use checkpoint/resume |
| 1000-tick safety limit | Runtime | Trees that don't reach a terminal state in 1000 ticks are terminated as partial |
| Hermes gateway spawning | Deployment | bt-evaluator and bt-langagent are spawned by hermes-gateway as MCP child processes. The goap-fusion daemon is the independent systemd USER service `bt-agent.service` running `bt-agent --no-mcp` against the bare main repo |
| NotebookLM quota (~50 metered calls/day) | External | Enforced locally by the nlm quota economy: per-Pacific-day query cache + daily budgets (30 queries, 2 web-research starts) at the nlmRun choke point; over budget → Claude review fallback |
| Claude Code CLI | Dependency | The self-improvement loop implements plans via the `claude` CLI (restricted tool allowlists per phase); session rate limits park plans for a later resume and open a durable backoff window (`goap_fusion_claude_backoff_until`, default 6h via `BT_GOAP_CLAUDE_BACKOFF`) during which subsequent ticks skip Claude attempts in milliseconds |

## Organizational Constraints

| Constraint | Impact |
|---|---|
| Single developer | All changes go through one person. Fast iteration, no merge conflicts, but no peer review safety net |
| Behavior-tree-first execution | All cron jobs must use BT agents. Shell scripts are stopgaps. New automation → build a tree |
| Git-versioned trees with conventional commits | Every tree mutation creates a git commit. Evolution is auditable and reversible |
| Skill-based documentation | Project knowledge lives in SKILL.md files, not traditional docs. Skills drive both human and agent workflows |
| Free-tier utilization (100%) | Minimize API costs. DeepSeek v4-flash for batch work (cheap), Ollama for interactive |

## Conventions

| Convention | Description |
|---|---|
| Conventional Commits | `feat(scope):`, `fix(scope):`, `test(scope):` format enforced |
| Go code edits via `patch` tool | Never `sed -i`. Edit Go files only through the patch tool |
| `go-bt` library conventions | `Run(ctx)` not `Execute`. `btleaf.NewAction` not `btcore.NewActionNode` |
| Blackboard must include Reflections+TreeStore | `{Task, LLM, Reflections, TreeStore}` required. Without, bt-manager fails silently |
| Gateway reload vs restart | `systemctl --user reload hermes-gateway` (SIGHUP) for config. Full restart for MCP binary changes |

---

*Generated by bt-agent arc42 pipeline — section2Constraints tree*


---

# arc42 Section 3 — Context and Scope

## 3.1 Business Context

### Communication Partners

| Partner | Inputs | Outputs |
|---|---|---|
| Hermes Agent | Tasks, delegated work, agent management commands | Execution results, reflections, fitness scores, agent status |
| BT Dashboard users | HTTP requests (navigation, API calls) | HTML pages, JSON responses, sprint status |
| Ollama qwen3.6:35b | Prompts (via ChainAction nodes) | Completions (text/markdown/code) |
| DeepSeek API | Escalated prompts, batch LLM work | Completions (5-10s latency) |
| Cron job system | Scheduled triggers (every 1h, daily, etc.) | Agent output delivered to Telegram/local/files |
| Hermes gateway | MCP spawn requests, tool invocations | MCP tool responses (JSON-RPC 2.0) |
| A2A clients | Agent card discovery, task requests | Task results, agent cards |
| Webhook subscribers | Agent events (lifecycle, outcome) | HTTP POST to configured endpoints |
| git (version control) | Tree mutations, evolution commits | Versioned tree history, rollback capability |

## 3.2 Technical Context

### External Interfaces

| Interface | Protocol | Endpoint | Purpose |
|---|---|---|---|
| bt-agent MCP | JSON-RPC 2.0 / stdio | (stdin/stdout) | 63 tools: tree execution, agent management, knowledge graph, evolution |
| bt-evaluator MCP | JSON-RPC 2.0 / stdio | (stdin/stdout) | 5 tools: fitness evaluation, mutation ordering, iterative deepening |
| bt-langagent MCP | JSON-RPC 2.0 / stdio | (stdin/stdout) | 3 tools: evolved langchain agent execution |
| bt-dashboard | HTTP/1.1 | `:9800` | REST API + embedded web UI (8 tabs) |
| Ollama | HTTP/1.1 | `localhost:11434` | OpenAI-compatible `/api/generate`, `/api/chat` |
| DeepSeek API | HTTPS | `api.deepseek.com` | `/v1/chat/completions` |
| A2A Server | HTTP/1.1 | `:8686` | Agent-to-Agent protocol: card discovery, task delegation |
| Hermes Webhook Bridge | HTTP/1.1 | `localhost:8644` | AgentBus events → Hermes gateway |

### System Boundary

```
┌─────────────────────────────────────────────────────────┐
│  Hermes Gateway                                         │
│  ┌──────────┐  ┌──────────────┐  ┌───────────────┐     │
│  │ bt-agent │  │ bt-evaluator │  │ bt-langagent  │     │
│  │ (stdio)  │  │   (stdio)    │  │   (stdio)     │     │
│  └────┬─────┘  └──────┬───────┘  └───────┬───────┘     │
│       │               │                  │              │
│  ─────┼───────────────┼──────────────────┼────────────  │
│       │               │                  │              │
│  ┌────▼─────┐  ┌──────▼───────┐  ┌───────▼───────┐     │
│  │ Ollama   │  │ bt-dashboard │  │ A2A Server   │     │
│  │ :11434   │  │ :9800        │  │ :8686        │     │
│  └──────────┘  └──────────────┘  └───────────────┘     │
│                                                         │
│  ─────────────────────────────────────────────────────  │
│                         │                               │
│                    ┌────▼─────┐                          │
│                    │ DeepSeek │                          │
│                    │ API (ext)│                          │
│                    └──────────┘                          │
└─────────────────────────────────────────────────────────┘
```

All local services run on the Jetson ARM64 host. Only DeepSeek API is external. Hermes gateway manages the MCP child process lifecycle. The dashboard and A2A server run independently via systemd or the gateway.

---

*Generated by bt-agent arc42 pipeline — section3ContextScope tree*


---

# arc42 Section 4 — Solution Strategy

## Quality Goals → Solution Approaches

| Quality Goal | Scenario | Solution Approach | Details |
|---|---|---|---|
| Q1: Correctness | Tree routes through PreGate→StrategyRouter→OutcomeSelector correctly | **Behavior Trees as Execution Model** (ADR-001) | Sequence/Selector/Action/Condition/ChainAction nodes. 175+ registered engine actions. Tree validation before execution. |
| Q1: Correctness | LLM produces valid structured output | **Output Quality Validation** | `validateOutputQuality()` checks min length (30 chars), error patterns, markdown structure, code blocks. QualityScore 0.0-1.0. |
| Q2: Evolvability | Tree improves over successive mutations | **Stockfish-Adapted Evolution** (ADR-005) | Transposition table with move ordering. Multi-dimensional fitness evaluation. Mutation ordering by predicted fitness delta. |
| Q2: Evolvability | Multiple fitness dimensions must be balanced | **Pareto Front + MAP-Elites** | MultiFitness scores across correctness, completeness, conciseness, actionability. ParetoFront tracks non-dominated solutions. MAP-Elites maintains quality diversity. |
| Q2: Evolvability | Evolution must not regress | **Git-Versioned + Benchmark Gating** (ADR-005) | Every mutation creates a git commit. Benchmarks compare before/after. Rollback on regression. |
| Q3: Reliability | Goroutine panics don't crash the process | **SafeGo + Panic Recovery** (ADR-007) | All goroutines wrapped in SafeGo. Tree-level panic recovery in RunTask(). Circuit breakers prevent cascading failures. |
| Q3: Reliability | Transient LLM errors self-heal | **Retry with Exponential Backoff** (ADR-007) | Full jitter retry: 1s→2s→4s→8s (base 500ms, max 30s). 3 retry classes: standard, LLM-specific, unknown. |
| Q3: Reliability | Exhausted retries don't lose work | **Dead Letter Queue** (ADR-007) | Persistent JSON file at `~/.go-bt-evolve/dead_letter_queue.json`. Failed tasks preserved for manual inspection/replay. |
| — | Task→tree mapping must be automatic | **Knowledge Graph + Factory** | Semantic discovery via embeddings. Tree breeding via crossover (PreGate from A × StrategyRouter from B). 7 categories with capability edges. |
| — | External tools must be accessible | **MCP Protocol Layer** (ADR-002) | JSON-RPC 2.0 over stdio. 3 servers expose 71 total tools. Hermes gateway manages lifecycle. |
| — | Agent state must survive restarts | **File-Based Persistence** (ADR-003) | Atomic writes (write .tmp → rename). YAML for agent definitions, JSON for scheduler/history/reflections. Git-friendly. |
| — | LLM must be integrated into BT nodes | **ChainAction Architecture** (ADR-006) | 10 chain types (llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action). Template variables: {{.Task}}, {{.Plan}}, {{.Result}}. |

## Key Technology Decisions

1. **go-bt library** (`github.com/rvitorper/go-bt`) — Mature Go behavior tree implementation. `Run(ctx)` not `Execute`.
2. **SerializableNode** — JSON-serializable intermediate representation between YAML definitions and go-bt runtime trees.
3. **Blackboard pattern** — Shared state object passed through tree ticks. Carries Task, Plan, Result, Outcome, ChainState, ChainTools, Reflections, TreeStore.
4. **ChainAction as BT node** — LLM calls are first-class behavior tree nodes, enabling PreGate gating, retry wrapping, and StrategyRouter selection.
5. **GOAP planning** — PlannerNode extends UtilitySelector with goal-driven action selection using world state + available actions.

---

*Generated by bt-agent arc42 pipeline — section4SolutionStrategy tree*


---

# arc42 Section 5 — Building Block View

## 5.1 Whitebox Overall System

### Layer Model

```
┌─────────────────────────────────────────────────────────────┐
│ ENTRYPOINTS (cmd/)                                          │
│ bt-agent  bt-dashboard  bt-evaluator  bt-langagent          │
│ bt-gardener  bt-agent-cli  benchcmp  bt-docgen              │
├─────────────────────────────────────────────────────────────┤
│ SERVICE LAYER                                               │
│ agent/  agentexec/  dashboard/  thinktank/  startup/        │
│ a2a/  hitl/  audit/  api/                                   │
├─────────────────────────────────────────────────────────────┤
│ CORE ENGINE (internal/engine/)                              │
│ tree.go  chains.go  registry.go  tools_real.go              │
│ Blackboard  BuildTree  RunTask  ChainAction  ActionRegistry │
├─────────────────────────────────────────────────────────────┤
│ EVOLUTION ENGINE (internal/evolution/)                      │
│ Stockfish  Pareto  MAP-Elites  Island  Q-Learning           │
│ Mutate  Expert  Learning  VaultManager                      │
├─────────────────────────────────────────────────────────────┤
│ KNOWLEDGE LAYER                                             │
│ knowledge/ (graph, discovery, embeddings)                   │
│ research/ (knowledge store, program store, quota economy)   │
│ factory/ (agent factory, tree generator)                    │
├─────────────────────────────────────────────────────────────┤
│ INFRASTRUCTURE                                              │
│ security/  reliability/  tracing/  config/  blackboard/     │
│ domains/  goap/  fusion/  blocks/  cicd/  doormate/         │
│ llm/  benchmark/  util/  gardener/  evaluator/              │
└─────────────────────────────────────────────────────────────┘
```

### Responsibility Table

| Layer | Package(s) | Responsibility |
|---|---|---|
| Entrypoints | `cmd/bt-agent`, `cmd/bt-dashboard`, `cmd/bt-evaluator`, `cmd/bt-langagent`, `cmd/bt-gardener`, `cmd/bt-agent-cli`, `cmd/benchcmp`, `cmd/bt-docgen` | Process entry points. Each is a standalone binary with its own main.go |
| Service | `internal/agent`, `internal/dashboard`, `internal/thinktank`, `internal/startup`, `internal/a2a`, `internal/domains` | Agent lifecycle (registry, scheduler, memory), dashboard API, thinktank analysis, startup simulation, A2A protocol (incl. auction-based task allocation), domain-specific trees |
| Core Engine | `internal/engine` | Behavior tree runtime: BuildTree, RunTask, Blackboard, chains (10 types), action/condition registry (175+ nodes), event bus |
| Evolution | `internal/evolution` | 6 algorithms (Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert), mutation operators (10 types), fitness scoring, vault manager |
| Knowledge | `internal/knowledge`, `internal/research`, `internal/factory` | Knowledge graph (capabilities, embeddings, discovery, auto-creation; optional runtime-feedback persistence in `feedback_persist.go`: `SaveFeedback`/`LoadFeedback` snapshot the RecordRun-mutated fields — Fitness, RunCount, LastOutcome, LastDuration — plus `uses_tool` edges to an atomic JSON file, merging into already-registered trees without clobbering static metadata; a debounced `FlushFeedback(force)` wrapper throttles bursty writes — now driven end to end by the `internal/agent` scheduler lifecycle via `SchedulerConfig.FeedbackPath`, closing the learn→evolve loop across restarts, see §8.4); graph-based impact analysis (`impact.go`: `BuildImpactGraph` links each source file to the tests a change can affect, via import-based edges + directory proximity, for change-scoped test selection — self-contained, no production consumer yet); research memory: content-hash dedup knowledge store + multi-cycle program store (ADR-003 JSON); factory breeding (crossover + archetypes) |
| Infrastructure | `internal/security`, `internal/reliability`, `internal/tracing`, `internal/config`, `internal/llm`, `internal/benchmark`, `internal/gardener`, `internal/cicd`, `internal/util`, `internal/api`, `internal/domains`, `internal/blackboard`, `internal/hitl`, `internal/audit` | Auth, rate limiting, circuit breakers, retry, DLQ, OpenTelemetry (facade in `internal/tracing`; local Grafana/Tempo/Loki stack via `make observability-up`), config, LLM providers, benchmarks, gardener evolution daemon, CI doctoring, scoped blackboard persistence, HITL approvals, audit log — 30 internal packages total |

## 5.2 Level 2: Core Engine Whitebox

```
internal/engine/
├── tree.go          — BuildTree(), RunTask(), Blackboard, actionForName, conditionForName
├── chains.go        — 10 chain types (llm_call, agent, rag_query, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_call, tool_action)
├── registry.go      — RegisterAction(), RegisterCondition(), GetAction(), GetCondition()
├── tools_real.go    — Real tool implementations for chain actions
├── arc42_nodes.go   — 22 arc42-specific actions + 5 conditions
├── goap_nodes.go    — GOAP planner integration
├── engine.go        — Init(), logging
└── *_test.go        — 10+ test files
```

Key flow: `RunTask(bb, tree)` → `BuildTree(serTree, bb)` → `buildNode()` → `go-bt Command[Blackboard]` → tick loop (1000 max) → validateOutputQuality()

## 5.3 Level 2: Evolution Engine Whitebox

```
internal/evolution/
├── stockfish.go         — TranspositionTable, mutation ordering, alpha-beta search
├── pareto.go            — MultiFitness, ParetoFront, ParetoPopulation
├── map_elites.go        — BehavioralDescriptor, MAPElitesGrid
├── island.go            — IslandModel with periodic migration, cumulative TotalMigrations counter
├── q_learning.go        — State→Action epsilon-greedy policy
├── local_search.go      — LocalSearcher (hill-climb / simulated-annealing / tabu), Population.MemeticEvolve
├── cmaes.go             — CMAESOptimizer (λ,μ-CMA-ES over normalized [0,1] solutions), TunableParam extraction/apply (ExtractParameters reads TimeoutMs/MaxRetries struct fields and Metadata keys; ApplyParameters writes back with bounds clamping), TuneTreeParameters Extract→Optimize→Apply seam
├── expert.go            — 6 design patterns, 5 anti-patterns, TreeArchetypes
├── mutations.go         — 10 mutation operators (add_before, add_after, wrap_retry, prune, swap_children, etc.)
├── learning.go          — cloneTree (sole deep-copy implementation), Population.Evolve + EvolveWithExperience (bank-warm-started variant) + EvolveQLearning (QTable-guided mutation-category selection), RetrieveExperienceHints (exported top-K bank retrieval reused by the gardener's candidate biasing)
├── experience_bank.go   — ExperienceBank: persisted successful-mutation entries (EvoRepair-style), Jaccard retrieval by tree type, bounded at 500 entries with quality-aware eviction; every full-file write path (addEntry, MarkReused, Persist) runs the lock→merge→write sequence so the two concurrent writers (daemon + gardener) never clobber each other
├── file_lock.go         — acquireExperienceLock: exclusive advisory flock on the experience.json.lock sidecar, held across merge→rename; flock attaches to the open file description, so even two opens within one process exclude each other; idempotent release func
├── vault_manager.go     — Tree vault with checkpoint/restore
├── types.go             — SerializableNode, Individual, Population, Fitness
└── fitness.go           — Per-tree fitness via reflection.FilterByTreeName
```

## 5.4 Level 2: Dashboard Whitebox

```
cmd/bt-dashboard/
├── main.go              — HTTP server on :9800, embed FS for static files, 8 route groups; owns the scalability substrate (dashTaskQueue + dashAgentRouter over a local executor) read by /api/scalability
├── pipeline_handlers.go — Sprint/quarter/year pipeline API handlers; enqueues each run onto dashTaskQueue and drains it on completion
└── static/              — Embedded web UI (HTML/JS/CSS)

internal/dashboard/
├── agents.go            — Agent listing, CRUD operations
├── executor.go          — AgentExecutor: in-process RunOnce via agent.RunAgent; Hermes CLI fallback when runner unavailable
├── workflow_engine.go   — Workflow orchestration
├── workflow_orchestrator.go — Multi-agent workflow coordination
└── tasks.go             — Task CRUD
```

## 5.5 Level 3: Chain Types Detail

| # | Chain Type | Description | Template Variables |
|---|---|---|---|
| 1 | `llm_call` | Single LLM invocation | `{{.Task}}`, `{{.Plan}}`, `{{.Result}}`, `{{.CachedResult}}`, `{{.ChainState.*}}` |
| 2 | `agent` | ReAct loop (Thought→Action→Observation→Final Answer) | Same as llm_call |
| 3 | `rag_query` | Retrieval-augmented QA using `bb.KgResults` | `{{.KgResults}}`, `{{.Task}}` |
| 4 | `tool_call` | Named tool invocation via LLM reasoning | `{{.ChainTools}}` |
| 5 | `structured_output` | JSON output with schema constraint | `{{.Task}}` + output schema from metadata |
| 6 | `refine` | Iterative self-improvement (2 passes) | Pass 1: initial answer, Pass 2: critique + improve |
| 7 | `map_reduce` | Decompose → process → combine | Parallel subtask processing |
| 8 | `conversation` | Multi-turn with memory | `{{.ChainMemory}}` |
| 9 | `retrieval_qa` | Two-phase retrieve-then-answer | `{{.KgResults}}` → `{{.Task}}` |
| 10 | `tool_action` | Direct tool invocation (no LLM) | Tool name + input in node config |

Each ChainAction reads config from node `Name` (format: `chain_type:prompt_text`) and `Metadata` (max_tokens, temperature, etc.).

---

*Generated by bt-agent arc42 pipeline — section5BuildingBlocks tree*


---

# arc42 Section 6 — Runtime View

## 6.1 Task Execution Scenario

**Trigger:** Hermes Agent calls MCP tool `bt_run_task` with a task string.

```
Hermes Agent                    bt-agent (MCP)                 Engine                    Ollama
    │                               │                            │                         │
    │──bt_run_task("Review code")──▶│                            │                         │
    │                               │──bb.Task = "Review code"──▶│                         │
    │                               │                            │──BuildTree(serTree)────▶│
    │                               │                            │──RunTask(bb, bt)        │
    │                               │                            │   ┌─tick loop (1000 max)│
    │                               │                            │   │ PreGate             │
    │                               │                            │   │  ├─ValidateInput    │
    │                               │                            │   │  └─SetupDevTools    │
    │                               │                            │   │ StrategyRouter      │
    │                               │                            │   │  ├─PrimaryPath      │
    │                               │                            │   │  │  └─ChainAction───▶│──prompt──▶
    │                               │                            │   │  │                  │◀─result───
    │                               │                            │   │  └─FallbackPath     │
    │                               │                            │   │ OutcomeSelector     │
    │                               │                            │   │  ├─WasSuccessful     │
    │                               │                            │   │  └─SelfCorrect       │
    │                               │                            │   └─bb.Outcome=success  │
    │                               │                            │──validateOutputQuality─▶│
    │                               │◀────result, outcome────────│                         │
    │◀────ToolResult───────────────│                            │                         │
```

**Duration:** Typical: 2-4 minutes (Ollama qwen3.6:35b). Fast path: 5-10 seconds (DeepSeek v4-flash). Timeout: 120s hard limit.

**Error Path:** ChainAction panic → SafeGo recover → RecordFailure → CircuitBreaker check → RetryWithBackoff (1s/2s/4s) → DeadLetterQueue.

## 6.2 Evolution Cycle

**Trigger:** bt-gardener cron (or manual `bt_evolve` MCP call).

```
Gardener                       bt-evaluator (MCP)            Evolution Engine              git
  │                               │                            │                            │
  │──ev_evaluate()───────────────▶│                            │                            │
  │                               │──MultiFitness eval────────▶│                            │
  │                               │◀──scores───────────────────│                            │
  │──ev_order_mutations()────────▶│                            │                            │
  │                               │──TT lookup + ordering─────▶│                            │
  │                               │◀──ranked mutations─────────│                            │
  │                               │                            │                            │
  │ Apply top mutation ──────────────────────────────────────▶│                            │
  │                               │                            │──cloneTree (sole impl)────▶│
  │                               │                            │──mutate (10 operators)────▶│
  │──ev_evaluate()───────────────▶│                            │                            │
  │                               │──compare fitness──────────▶│                            │
  │                               │◀──delta────────────────────│                            │
  │                               │                            │                            │
  │ If delta > 0: ACCEPT ────────────────────────────────────▶│──git commit───────────────▶│
  │ If delta ≤ 0: ROLLBACK ──────────────────────────────────▶│──git checkout─────────────▶│
```

**Key:** 97.3% of mutations currently regress (no quality gates enforced — see Section 11 Risks). Per-tree fitness via `reflection.FilterByTreeName` + seed records.

**Experience integration (ADR-021):** In the v2 cycle (`RunCycleV2`), the ranked-mutation step is experience-biased — `biasCandidatesWithExperience` boosts `OrderMutations` candidates whose op/target matches high-quality past `ExperienceBank` entries for the tree type — and every ACCEPT additionally records the mutation into the shared bank via `AddFromMutation` with its per-candidate fitness delta. A nil bank leaves both steps at the historical behavior.

## 6.3 Sprint Execution

**Trigger:** Dashboard user POSTs to `/api/sprint` with company/quarter info.

```
Browser                         bt-dashboard (:9800)           Goroutine                   bt-agent (MCP)
  │                               │                            │                            │
  │──POST /api/sprint────────────▶│                            │                            │
  │                               │──orch.RunSprint()─────────▶│                            │
  │                               │                            │──Create tasks (5 roles)──▶│
  │                               │                            │──for each task:           │
  │                               │                            │   agent.RunAgent() in-process
  │                               │                            │   "delegate to {tree}"───▶│──bt_run_task()──▶
  │                               │                            │                            │◀──result────────
  │                               │                            │──mark task done           │
  │◀──{sprint_id}─────────────────│                            │                            │
  │                               │                            │                            │
  │──GET /api/sprint/status──────▶│                            │                            │
  │◀──{progress, tasks}───────────│                            │                            │
```

**Duration:** 5-15 minutes (5+ Ollama calls per sprint). Poll-based status via `/api/sprint/status`.

## 6.4 Self-Improvement Cycle (goap-fusion loop)

**Trigger:** scheduler fires `goap-fusion-loop-runner` (cron `0,30 * * * *`; cycles serialize; per-cycle ceiling 2h).

```
Preflight ─▶ Research ─▶ Goals ─▶ Plan ─▶ Implement ─▶ Verify ─▶ Land
   │            │           │        │         │           │        │
   │ materialize│ grill     │ dedup  │ goal-   │ per task: │ tests, │ hook-gated
   │ build tree │ (cached)  │ vs     │ driven  │ RED→ver→  │ build, │ commit in
   │ CIRCUIT-   │ NotebookLM│ know-  │ multi-  │ GREEN→ver │ chgd-  │ worktree,
   │ POLICY gate│ query ────│ ledge  │ task    │ snapshot  │ pkg    │ ff bare
   │ resume     │  └quota?  │ store  │ (≤3     │ commits   │ suites │ master,
   │ saved plan │  fallback:│ (impl. │ tasks,  │ (partial  │ + lint │ push;
   │ (stale →   │  Claude   │ goals  │ files   │ landing)  │ parity │ record
   │  clear)    │  review,  │ never  │ from    │ arc42 doc │        │ implemented
   │            │  mode     │ re-    │ goals,  │ sync      │        │ goals +
   │            │  rotation)│ done)  │ risk    │           │        │ program
   │            │ +programs │        │ tiers)  │           │        │ milestone
```

**Partial landing:** a later task's failure discards only that task's edits (per-task snapshot commits in the run worktree); the completed, verified work still lands and the failed goal is carried forward to the next cycle. All-or-nothing is preserved for first-task failures and outside worktree-apply mode.

**Rate-limit backoff:** a Claude rate-limited outcome in any tick persists a backoff deadline (`goap_fusion_claude_backoff_until`, RFC3339 in the agent-scope blackboard with ChainState fallback — the same durable pattern as the saved plan) so the *next* ticks consume it instead of re-attempting against a quota known to be closed. Both Claude consumers honor it on entry: the plan-resume runtime (`runSuperpowersRuntimeFromExistingPlanAction`) degrades to ScheduledAnalysisPath instantly — before worktree creation and the 45-minute batch attempt — with the exact rate-limited Result/Outcome shape, so the plan carryover is preserved for the tick after the window expires; and the Claude review research fallback returns rate-limited in milliseconds without invoking Claude, letting the ResearchRouter fall through to its non-fatal skip. The window is env-configurable (`BT_GOAP_CLAUDE_BACKOFF`, default 6h) on the implementation path and a fixed 1h on the review path; an elapsed window self-clears (half-open, the ADR-010 lesson) and malformed state reads as inactive, so stale or corrupt backoff can never permanently block Claude attempts.

**Research economy:** NotebookLM answers are cached per Pacific day by question hash; daily budgets (30 queries / 2 web-research starts) refuse further metered calls with an error the ResearchRouter routes to the Claude review fallback (commits → structure → failures mode rotation, persisted round counter). The router is itself non-fatal: in both fusion trees (`domain:goap_fusion` and `domain:goap_fusion_loop`) it ends in a terminal `AlwaysSucceed` "ResearchOptional" leaf, so a doubly-unavailable research stage — NotebookLM quota closed *and* the Claude review fallback rate-limited or barren — degrades to the vault-context read phase (ReadVaultResearch onward) instead of aborting the run.

## 6.5 Error Recovery

```
Any goroutine                   SafeGo wrapper                 CircuitBreaker              DeadLetterQueue
  │                               │                            │                            │
  │──PANIC!──────────────────────▶│                            │                            │
  │                               │──recover()─────────────────│                            │
  │                               │──RecordFailure()──────────▶│                            │
  │                               │                            │──State check──────────────│
  │                               │                            │   CLOSED → allow retry    │
  │                               │                            │   OPEN → skip + queue────▶│──Push(entry)
  │                               │                            │   HALF_OPEN → test probe  │
  │                               │                            │                            │
  │                               │──RetryWithBackoff()───────▶│                            │
  │                               │   1s → 2s → 4s → 8s       │                            │
  │                               │   (full jitter)            │                            │
  │                               │   Max 3 retries            │                            │
  │                               │                            │                            │
  │                               │   Exhausted ─────────────────────────────────────────▶│──Persist JSON
```

**Circuit breaker config:** Threshold from `cfg.CBThreshold`, cooldown from `cfg.CBCooldownSecs`. Per-agent circuit breakers via `AgentCircuitBreakerStore`.

---

*Generated by bt-agent arc42 pipeline — section6RuntimeView tree*


---

# arc42 Section 7 — Deployment View

## 7.1 Infrastructure Level 1

### Hardware

| Resource | Specification |
|---|---|
| Platform | NVIDIA Jetson ARM64 |
| CPU | 12 cores |
| RAM | 61 GB |
| Storage | 57 GB eMMC (system) + 1.8 TB NVMe (`/mnt/ssd/`) |
| Network | Tailscale VPN (100.123.73.66) |
| Kernel | Linux 5.10.120-tegra |

### Process Inventory

| Process | Type | Port/Transport | Manager |
|---|---|---|---|
| hermes-gateway | Python (systemd user) | — | `systemctl --user` |
| bt-agent (daemon) | Go, systemd user `bt-agent.service` (`--no-mcp`) | :8686 A2A + scheduler | `systemctl --user` |
| bt-agent (MCP) | Go (MCP child) | stdio | hermes-gateway |
| bt-evaluator | Go (MCP child) | stdio | hermes-gateway |
| bt-langagent | Go (MCP child) | stdio | hermes-gateway |
| bt-dashboard | Go (systemd user) | :9800 | `systemctl --user` |
| bt-gardener | Go, systemd user `bt-gardener.service` (sandboxed evolution cycles) | — | `systemctl --user` |
| Ollama | C++ (systemd) | :11434 | System service |
| DeepSeek API | External SaaS | HTTPS | — |

## 7.2 Infrastructure Level 2

### 7.2.1 Process Tree

```
systemd --user
├── hermes-gateway (Python)
│   ├── bt-agent (Go, stdio MCP)
│   ├── bt-evaluator (Go, stdio MCP)
│   └── bt-langagent (Go, stdio MCP)
├── bt-dashboard (Go, HTTP :9800)
└── bt-gardener (Go, launched ad-hoc)

systemd (system)
└── ollama (C++, HTTP :11434)
```

**Key detail:** MCP servers are NOT independent systemd units. They are spawned by hermes-gateway as child processes. A `SIGHUP` reload of the gateway does NOT restart MCP children — they need a full restart to pick up new binary code. bt-agent CANNOT be started via `terminal(background=true)` because the MCP stdio server exits when stdin closes.

### 7.2.2 Storage Layout

```
~/.go-bt-evolve/
├── agents/                  — Installed agent YAML definitions
├── history/                 — Agent run history (JSON)
├── memory/                  — Per-agent memory stores
├── blackboard/              — Scoped blackboard persistence (agent/run/session)
├── jobs/                    — Scheduler persistence (scheduler-jobs.json)
├── research/                — knowledge.json (dedup store), programs.json
│                              (multi-cycle programs), nlm-query-cache.json +
│                              nlm-usage.json (quota economy)
├── experience/              — experience.json: ExperienceBank of successful
│                              mutations (warm-starts bt_evolve_genetic /
│                              bt_evolve_bottlenecks across restarts; shared
│                              with bt-gardener, whose cycles record into and
│                              bias from the same file — ADR-021); plus the
│                              experience.json.lock flock sidecar serializing
│                              the two writers' rewrites (ADR-024)
├── hitl/                    — Human-in-the-loop approval requests
├── audit/                   — Audit log
├── logs/                    — bt.log
├── feedback.json            — Knowledge-graph runtime-feedback snapshot
│                              (Fitness/RunCount/tool-edges); agent.FeedbackFile()
├── dead_letter_queue.json   — Failed task persistence
└── vault/                   — Tree vault (checkpoint/restore)

~/.go-bt-reflections/        — Reflection records + gardener tree store

/mnt/ssd/
├── .hermes/                 — Hermes Agent runtime
│   ├── skills/              — SKILL.md files (~50 skills)
│   ├── cron/output/         — Cron job output delivery
│   └── audio_cache/         — TTS output cache
├── clawd/wiki/bt-research/  — Obsidian vault + BT research docs
└── clawd/wiki/bt-research/  — Obsidian research vault (syntheses/, plans/)

/home/nico/go-bt-evolve/     — BARE main repo (master never checked out); live
                               binaries at the repo root with .previous backups;
                               run worktrees under /tmp/worktrees/; durable run
                               artifacts in docs/superpowers/runs/<id>/
```

### 7.2.3 Network Topology

```
Internet
    │
    ├── api.deepseek.com:443 ─── DeepSeek API (escalation LLM)
    │
Tailscale (100.123.73.66)
    │
    ├── localhost:9800 ─── bt-dashboard (HTTP)
    ├── localhost:8686 ─── A2A server (HTTP)
    ├── localhost:8644 ─── Hermes webhook bridge
    ├── localhost:11434 ── Ollama (HTTP)
    └── stdio ─────────── 3 MCP servers (bt-agent, bt-evaluator, bt-langagent)
```

All services bind to localhost except the dashboard (accessible via Tailscale). No public internet exposure.

---

*Generated by bt-agent arc42 pipeline — section7Deployment tree*


---

# arc42 Section 8 — Crosscutting Concepts

## 8.1 Behavior Tree Execution Model

**What:** All agent logic is expressed as behavior trees — hierarchical structures of Sequence, Selector, Action, Condition, ChainAction, and decorator nodes. ADR-001.

**Why:** BT nodes are composable, testable, and evolvable. The tick-based execution model supports multi-step planning with interleaved LLM calls. StrategyRouter + OutcomeSelector provide structured fallback.

**Where:** `internal/engine/tree.go` (BuildTree, RunTask, Blackboard), `internal/engine/registry.go` (action/condition registration), `internal/domains/` (tree definitions).

**Effect:** Every task flows through PreGate→StrategyRouter→OutcomeSelector. New capabilities are added by registering actions — no control flow changes needed.

## 8.2 ChainAction Nodes

**What:** LLM calls wrapped as behavior tree leaf nodes. 10 chain types (`llm_call`, `agent`, `rag_query`, `tool_call`, `structured_output`, `refine`, `map_reduce`, `conversation`, `retrieval_qa`, `tool_action`). ADR-006.

**Why:** Integrating LLM calls as BT nodes enables PreGate gating (don't call LLM if preconditions fail), retry wrapping, and StrategyRouter selection (try primary prompt, fall back to alternate prompt).

**Where:** `internal/engine/chains.go`. Config read from node `Name` (format: `chain_type:prompt_text`) and `Metadata` (max_tokens, temperature).

**Template variables:** `{{.Task}}`, `{{.Plan}}`, `{{.Result}}`, `{{.CachedResult}}`, `{{.ChainState.*}}`, `{{.ChainMemory}}`, `{{.ChainTools}}`, `{{.KgResults}}`.

**Effect:** LLM integration is a first-class BT concept, not a side-effect. Chains can be retried, gated, and selected just like any other action.

## 8.3 MCP Protocol Layer

**What:** All tools are exposed via JSON-RPC 2.0 over stdio. 3 MCP servers: bt-agent (63 tools), bt-evaluator (5 tools), bt-langagent (3 tools). ADR-002.

**Why:** MCP provides a standardized interface between Hermes Agent and the Go BT platform. No custom protocols, no REST overhead. Stdio transport keeps it simple and gateway-managed.

**Where:** `internal/mcp/` (server implementation), `cmd/bt-agent/tools.go` (tool registration), `cmd/bt-agent/main.go` (server setup).

**Effect:** Hermes Agent sees 71 MCP tools. Adding a tool is a single `server.RegisterTool()` call. Gateway handles lifecycle (spawn, restart, health check).

**In-process seam:** `engine.Server` also exposes `HasTool(name) bool` and `Invoke(name, args) (*ToolResult, bool)` (`internal/engine/mcp_server.go`) so a registered tool can be asserted and driven by name in-process — without standing up the stdio JSON-RPC loop. This is a test/in-process seam only: it reads the private handler registry directly and deliberately bypasses the auth, rate-limit, sanitization, and tracing wrapping applied on the `tools/call` path, so it must never become a production request route.

## 8.4 File-Based Persistence

**What:** All state stored as JSON/YAML files with atomic writes (write .tmp → rename). No SQL database. ADR-003.

**Why:** Git-friendly (diffs are readable), no database dependency, single-file atomicity prevents corruption. Simpler than SQL for a single-machine platform.

**Where:** `~/.go-bt-evolve/` directory tree. Agent YAMLs, scheduler JSON, history JSON, reflection records, DLQ JSON, tree store JSON.

**Effect:** State survives restarts. Git can version agent definitions. Manual inspection and repair is possible with any text editor.

**Scoped-blackboard scope-ID normalization (cross-tool contract):** The persisted blackboard key-space is addressed by `blackboard.Scope{Kind, ID}`, and two independent surfaces construct that scope from user input: the daemon's MCP blackboard tools via `parseBBScope` (`cmd/bt-agent/blackboard_tools.go`) and the CLI inspector via `parseBBScopeFlag` (`cmd/bt-agent-cli/bb.go`). Both must normalize identically — each trims whitespace from the scope ID before building the scope — otherwise an entry persisted under a daemon-normalized scope becomes unreadable when the CLI is given a padded `--id`. The contract is pinned by `cmd/bt-agent-cli/bb_test.go`: a table-driven test over all three scope kinds plus a round-trip test that writes through a daemon-parsed scope and reads back through the CLI parse path, using the `bbManagerAt(dir)` construction seam so the production manager-setup path runs against a temp directory. The reflection feedback identifiers follow the same canonicalize-once contract: `recordUserFeedback` (`cmd/bt-agent/feedback_tools.go`) trims `user` and `treeID` once at entry and uses the trimmed values throughout — the stored record, the per-user workspace slug, and the cumulative `FilterByTreeNameStrict` satisfaction tally — where it previously validated on the trimmed value but stored the raw one, so a trailing-space tree id created records no later strict lookup could ever match (pinned by `TestRecordUserFeedback_TrimsUserAndTreeID`).

**Knowledge-graph feedback (wired via the scheduler lifecycle):** The knowledge graph's runtime feedback — the RecordRun-mutated fields (Fitness, RunCount, LastOutcome, LastDuration) and `uses_tool` edges — is in-memory and would otherwise be lost on restart. `internal/knowledge/feedback_persist.go` adds a same-pattern (atomic write .tmp → rename) JSON snapshot: `SaveFeedback`/`LoadFeedback` serialize only the feedback subset (static tree metadata is excluded, and Load merges into already-registered trees rather than clobbering them), and a debounced `FlushFeedback(force)` — driven by `MarkFeedbackDirty` and a min-interval throttle — avoids rewriting the whole graph on every bursty RecordRun. The writer takes no `internal/agent` dependency. The `internal/agent` scheduler now drives that lifecycle end to end. `SchedulerConfig` carries an optional `FeedbackPath` (and `FeedbackFlushInterval`, defaulting to 30s when zero); when the path is set, `NewScheduler` re-hydrates prior feedback with `LoadFeedback` (logging, not failing, on error — matching the missing-file-no-error contract) and arms the debounced writer via `ConfigureFeedbackPersistence`. Both `RecordRun` call sites (`RunNow` and the scheduled `runJob`) then call `persistRunFeedback`, which marks the graph dirty and attempts a throttled best-effort `FlushFeedback(false)`; `Stop()` issues a forced `FlushFeedback(true)` so feedback pending inside the throttle window is durably written on shutdown. `ConfigureFeedbackPersistence` resets the throttle clock on each arming so a re-armed process-global `GlobalGraph` always flushes on its first dirty mark. Together this closes the learn→evolve loop across restarts: a fresh process reads back the accumulated Fitness/RunCount/tool-edges instead of resetting them. The production daemon supplies that path: `cmd/bt-agent/main.go` factors the whole `SchedulerConfig` assembly out of `main()` into a `buildSchedulerConfig(cfg, reg, hist)` helper, which sets `SchedulerConfig.FeedbackPath` to `agent.FeedbackFile()` — `~/.go-bt-evolve/feedback.json`, the single canonical snapshot location, resolved through the package-level `feedbackSnapshotPath()` helper — alongside the durable `FileJobStore` and per-agent circuit-breaker store. Extracting the helper lets `wiring_test.go` assert the assembled config end-to-end (`TestDaemonSchedulerConfigWiresFeedbackPath`) rather than only checking the `feedbackSnapshotPath()` helper in isolation, so a regression that drops the `FeedbackPath` line — silently disabling persistence — now fails a test instead of shipping dormant.

## 8.5 Evolution Pipeline

**What:** Common pattern for tree improvement: evaluate → order mutations → apply top mutation → re-evaluate → compare fitness → accept (commit) or rollback.

**Why:** Multiple algorithms (Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert) share this pattern. A unified pipeline reduces duplication and ensures consistent safety checks.

**Where:** `internal/evolution/` — each algorithm file, `internal/gardener/` (evolution_v2.go for cycle orchestration).

**Effect:** Evolution is auditable (git commits), reversible (rollback), and measurable (fitness delta tracking).

**Experience-grounded memory (ADR-017):** The genetic path additionally learns across runs. `Population.EvolveWithExperience` (`internal/evolution/learning.go`) warm-starts operator selection from the persistent `ExperienceBank` — the top-5 `RetrieveByTreeType` hints for the population's tree type bias 50% of mutations toward operators that previously improved fitness on similar trees — and records every fitness-improving mutation back via `AddFromMutation` (regressions are discarded, so the bank only accumulates successes). A nil bank degrades to plain `Evolve`. The daemon constructs one bank at startup (`~/.go-bt-evolve/experience/experience.json`, honoring `BT_AGENT_HOME`) and plumbs it through `mcpDeps` into `bt_evolve_genetic` and `bt_evolve_bottlenecks`, so mutation experience compounds across restarts the same way knowledge-graph feedback does (§8.4). The bank is bounded at 500 entries (`experienceBankCap`) with quality-aware eviction — lowest `QualityScore` first, oldest first among equal quality, entries with `TimesReused >= 3` evicted only after every less-proven entry is gone — enforced on every `Add` and, for oversized legacy files, on load in `NewExperienceBank` (ADR-018).

**Gardener adoption (ADR-021):** The same bank now also feeds the 24/7 gardener cycle. `gardener.Config` carries an optional `*evolution.ExperienceBank`; when set, `evolveTreeV2` (`internal/gardener/evolve_v2.go`) records every *accepted* mutation via `AddFromMutation` with its per-candidate composite delta, and `biasCandidatesWithExperience` reorders the `evaluator.OrderMutations` heuristic ranking before candidates are tried — the top-5 `RetrieveExperienceHints` for the tree type (quality ≥ 0.5) boost matching op/target candidates by 0.15 × quality under a stable sort, and matched entries are `MarkReused`. `cmd/bt-gardener` constructs the bank at `agent.HomeDir()/experience` — deliberately the daemon's directory — so gardener and bt-agent accumulate mutation experience into one shared store. A nil bank degrades to the historical no-recording, heuristic-order-only behavior. Because both binaries rewrite the whole `experience.json` on every `Add`, `addEntry` first re-merges the on-disk entries by ID (adopting entries only on disk, keeping the higher `TimesReused` on conflicts) before appending and persisting, so concurrent daemon and gardener writers no longer clobber each other's recent entries (ADR-022); the merged set still passes the ADR-018 cap enforcement. The merge→rename window itself is serialized cross-process by an exclusive advisory flock on the `experience.json.lock` sidecar (`acquireExperienceLock`, `internal/evolution/file_lock.go`), held from the disk merge until the atomic rename completes, and every full-file write path — `addEntry`, `MarkReused`, and the exported `Persist()` — runs the same lock→merge→write sequence through a shared `persistLocked` helper, so no exported path can rewrite the file from stale memory; if the lock cannot be acquired (e.g. read-only directory), the write degrades to the unlocked merged path rather than dropping the entry (ADR-024).

**Within-run reinforcement (ADR-019):** Complementing the cross-run ExperienceBank, `Population.EvolveQLearning` (`internal/evolution/learning.go`) learns *within* a single evolution run: each offspring mutation's category is chosen epsilon-greedily from a caller-owned `QTable` keyed by the child's structural state, and the fitness delta is fed back via `Update` — regressions are discarded by the same quality-gate pattern but still recorded, so the table learns which categories to avoid per state. `Population.MemeticEvolve` (`internal/evolution/local_search.go`) instead refines offspring with a pluggable `LocalSearcher` (hill-climb, simulated annealing, or tabu). Both are reachable in production via the deterministic `bt_evolve_qlearning` and `bt_evolve_memetic` MCP tools.

**Parameter tuning vs structural mutation (ADR-020):** Not every underperforming tree needs new topology — some just need better numbers. `TuneTreeParameters` (`internal/evolution/cmaes.go`) is the single Extract→Optimize→Apply seam: `ExtractParameters` collects `TunableParam`s from both `Metadata` keys and the `TimeoutMs`/`MaxRetries` struct fields (seeding `InitValue` from the live field value), `CMAESOptimizer.Optimize` searches normalized [0,1] space with an adapter that denormalizes each candidate onto a scratch clone, and the winner is applied to a fresh clone — the input tree is never mutated, and `ok=false` is returned without any fitness calls when no parameters exist. `bt_evolve_bottlenecks` routes on that flag: trees with tunable parameters get CMA-ES tuning (`"algorithm":"cmaes"`), parameterless trees fall back to structural `EvolveWithExperience` (`"algorithm":"genetic"`), and the report's top-level `algorithms` tally shows the split.

## 8.6 Error Resiliency

**What:** SafeGo (panic recovery) + CircuitBreaker (3-state) + RetryWithBackoff (full jitter, 3 classes) + DeadLetterQueue (persistent JSON). ADR-007.

**Why:** LLM calls can fail (Ollama OOM, DeepSeek rate limits, network timeouts). Goroutines must not crash the process. Failed work must not be silently lost.

**Where:** `internal/reliability/` — SafeGo, CircuitBreaker, RetryPolicy, DeadLetterQueue. Applied in scheduler runner (`main.go:276`), ChainAction execution, and all goroutine spawns.

**Effect:** The platform degrades gracefully. A single LLM failure doesn't cascade. Failed tasks are preserved for inspection.

## 8.7 Quality Gates

**What:** Output validation before declaring success. Minimum length, error pattern detection, structure scoring (markdown, bullets, code blocks). QualityScore 0.0-1.0.

**Why:** LLMs sometimes produce truncated/garbage output (e.g., max_tokens=10 producing a few words). Without validation, agents report "success" with useless output.

**Where:** `internal/engine/tree.go:validateOutputQuality()`. Applied after every RunTask() and in ReflectOnOutcome action.

**Effect:** Structured zero-LLM output (alert_router, agent_monitor) scores correctly. Garbage output is flagged. Quality scores feed into fitness evaluation.

## 8.8 Tool Protocol

**What:** ChainAction nodes use tool stubs (Name/Description/Call) populated at PreGate. Tools inject file I/O, shell execution, and codebase inspection capabilities into LLM chains.

**Why:** LLMs need access to real tools (read files, run commands, query the graph) during chain execution. Tool stubs provide a uniform interface.

**Where:** `internal/engine/tools_real.go` (real implementations), `internal/engine/tree.go:toolStub` (lightweight wrapper). Setup actions: `SetupDefaultTools`, `SetupDevTools`, `SetupResearchTools`.

**Effect:** ChainAction prompts can reference `{{.ChainTools}}` and the LLM can reason about available capabilities.

## 8.9 Research Memory and Quota Economy

**What:** A content-hash-deduplicating knowledge store (`internal/research`) records research findings, NotebookLM answers, and implemented goals; a program store persists multi-cycle change programs. The nlm quota economy (per-day query cache + daily budgets at the `nlmRun` choke point) keeps the ~50/day NotebookLM quota spent on new questions only. On the query-derivation side, `deriveNotebookLMResearchQuery` recognizes agent/implementation boilerplate (empty or over-long text, `domain:` scaffolding, schedule cadence, anti-fabrication evidence-gate language) via a shared `isBoilerplateResearchTopic` guard applied to *both* the raw task *and* any next-milestone goal, so an implementation-task milestone (code paths, `domain:<name>`) can no longer seed a degenerate query — the derivation falls through to a genuine curated topic instead. A goal-actionability filter in `goap_research_goals.go` keeps degenerate NotebookLM echoes out of the plan: prose lead-ins ("review complete", "in summary", …) and source-citation prose — a `NotebookLM research:` meta prefix, a `(Community NN)` cluster label, a bracketed reference range like `[1-4]`, or LaTeX math such as `$RFC 6902$` — are rejected *even when the deterministic file-scoper has already appended `(files: …)` paths*, so a paraphrased research-paper architecture (e.g. the recurring KernelNode/VALIDPATCH "PatchBoard" transition) can never launder itself into a task that fabricates scope for files that do not exist.

**Why:** Scheduled research previously re-asked identical questions (147 identical grill transcripts), re-proposed already-landed goals, and lost results to per-day file overwrites. The plan quota was oversubscribed 3× on repeats.

**Where:** `internal/research/` (store.go, programs.go), `internal/engine/nlm_quota.go`, `internal/engine/goap_research_goals.go`; state under `~/.go-bt-evolve/research/`.

**Effect:** Research compounds instead of repeating: every cycle sees what is already known and implemented; repeated questions are free; budget exhaustion degrades to the Claude review fallback instead of burning quota — and if that fallback is itself rate-limited or barren, the ResearchRouter's terminal non-fatal skip (present in both fusion trees, §6.4) degrades the run to vault context rather than aborting it. A rate-limited fallback additionally records the durable Claude backoff deadline (§6.4), so subsequent ticks skip the fallback without spending its 15-minute run at all.

## 8.10 Autonomous Landing Pipeline

**What:** Every self-improvement run executes in an isolated git worktree, verifies with tests/build/changed-package suites/lint parity, commits through the full pre-commit hook, fast-forwards the bare master via ancestry-checked sync, and pushes. Partial landing preserves completed tasks when a later task fails. An arc42 sync stage updates this document in the same commit.

**Why:** Autonomous code changes need the same (or stronger) gates as human changes, without a human in the loop; failed cycles must not destroy verified work; documentation must not drift.

**Where:** `internal/engine/superpowers_*.go` (task executor, apply, worktree sync, arc42 sync), `internal/engine/actions_superpowers_prod.go`.

**Effect:** The loop lands multiple verified multi-task commits per day unattended; a failed task costs only itself; master and origin stay synchronized; the architecture documentation stays current.

## 8.11 Observability

**What:** OpenTelemetry (traces + logs) behind the `internal/tracing` facade; every registered action/condition is wrapped in a tracing decorator (`bt.action/<name>` spans). Local Grafana/Tempo/Loki stack via `make observability-up`; telemetry activates only when OTLP endpoints are configured.

**Where:** `internal/tracing/`, decorators in `internal/engine/registry.go`, run summaries via the scheduler webhook → Hermes gateway → Telegram.

**Prometheus histogram exposition (2026-07-08, ADR-023):** the metrics exposition (`internal/dashboard/metrics_utils.go`, served as `/api/metrics` by bt-dashboard) now renders the observed duration `LabeledHistogram`s as full Prometheus histogram series — cumulative `_bucket` lines including `+Inf`, plus `_sum` and `_count` — through a shared `writeLabeledHistogram` renderer backed by the cumulative per-bucket counts `Histogram.SnapshotStats` now returns (`HistogramSnap.Bounds`/`CumulativeCounts`). This makes `bt_node_duration_ms` and `bt_block_duration_ms` — observed since their introduction in `bt_nodes.go` but never rendered, hence invisible to alerting — scrapeable, and adds a new per-agent `bt_agent_task_duration_ms` histogram that `RecordTask` observes alongside the existing `bt_agent_duration_ms_total` counter, so latency-percentile alerts can be defined per node type, per block, and per agent.

**Build identity (2026-07-08, ADR-023):** the exposition always carries exactly one `bt_build_info{revision,dirty}` gauge (value 1, identity in labels). The long-running binaries (`cmd/bt-agent`, `cmd/bt-gardener`) read their VCS identity at startup via `runtime/debug.ReadBuildInfo` (`dashboard.InstallBuildIdentity`) and log revision/commit-time/dirty; a process that never installs one self-identifies at scrape time, and unstamped builds degrade to an `"unknown"` sentinel rather than an empty label (which Prometheus matchers would silently match everything on). The recurring stale-daemon-binary drift (three incidents to date, previously diagnosed only by DLQ-message text heuristics) is thereby detectable by comparing the running revision against repo HEAD.

## 8.12 A2A Auction Task Allocation

**What:** A contract-net allocation protocol over the A2A transport. `Auctioneer.RunAuction` composes announce → evaluate → dispatch end to end: it fans a `TaskAnnouncement` out to candidate agents via `CollectBids`, each candidate scores the announcement against its own agent card with `ScoreAnnouncement` and answers with a `Bid` (cost/confidence) or declines — `CollectBids` attributes each collected bid to the identity the announcement was delivered under (the candidates-map key), not the untrusted self-reported `BidderName`, so a candidate that misreports its name still resolves against the candidates map at Award/dispatch time — a `ScoreEvaluator` awards the lowest-cost eligible bid, and the winner alone is dispatched the real task text — returning an `AuctionResult` (Award + execution result); `CollectBids` bounds each candidate's fan-out by a per-candidate deadline derived from the announcement's `Deadline` (a 30s `defaultBidDeadline` when unset) so one hung candidate cannot stall the auction — and the winning-agent dispatch reuses that same `candidateContext` deadline rather than the raw caller context, so a winner that hangs after Award cannot block `RunAuction` indefinitely — and `RunAuction` rejects an announcement with an empty `Description` before dispatch. On the responder side, `BTAgentExecutor.Execute` detects an incoming JSON announcement and replies with a bid artifact on a completed task instead of running the announcement JSON as literal task text, declining silently when ineligible. An engine-side `AuctionDelegate` behavior-tree action makes this auction reachable from within any tree: it reads `bb.Task` and delegates to an injected `AuctionDelegateFn(task, chainState) → (result, awarded, err)` hook — the same injection-hook pattern as `DelegateToA2AFn`/`DelegateToTreeFn`, used because `internal/a2a` imports `internal/engine` and the dependency cannot be reversed — falling back, when no eligible bidder is awarded, to running the task through a delegate tree named by `chain_state.delegate_tree_id` via `DelegateToTreeFn`.

**Why:** The three stages (announce/collect, evaluate→Award, dispatch) existed but nothing wired announce→evaluate→dispatch together, so a picked Award was never acted on; and a real bt-agent server treated the announcement JSON as literal task text, so no production agent could ever return a well-formed Bid and the fan-out was inert. Scoring bids from an agent's own card — confidence as the fraction of card capabilities the task demands, cost as the count of irrelevant ones — lets a focused specialist win over a diluted generalist.

**Where:** `internal/a2a/auction.go` (Auctioneer, RunAuction, AuctionResult, ScoreAnnouncement, ScoreEvaluator, and the production `AuctionDelegate` / `AuctionCardsFn` / `candidateContext` seams), `internal/a2a/server.go` (RespondToAnnouncement, parseAnnouncement, the bid-aware Execute branch, `Server.AuctionCardSource`), `internal/a2a/card.go` (EligibleBidders, treeTags), `internal/engine/actions_a2a.go` (the `AuctionDelegate` action and `AuctionDelegateFn` injection seam), `internal/agentexec/wiring.go` (installs `engine.AuctionDelegateFn = a2a.AuctionDelegate` at link time), `cmd/bt-agent/main.go` (installs `a2a.AuctionCardsFn` from the live card registry at startup), `internal/domains/trees.go` (the `auction_demo` tree).

**Effect:** Multi-agent task allocation now closes end to end: an auctioneer can announce, collect real bids from live per-agent endpoints, award the best-fit agent, and dispatch it the work. Unreachable, silent, malformed, foreign-task, and name-misreporting candidates are tolerated without failing the auction. The engine `AuctionDelegate` node and its `AuctionDelegateFn` seam are now injected in production: `internal/agentexec`'s `init` installs `engine.AuctionDelegateFn = a2a.AuctionDelegate` as a link-time side effect (so the node never reports *auction delegate not configured* once the binary is linked, guarded by the binary-level `TestDaemonConfiguresAuctionDelegateHook`), and the daemon supplies the live candidate source at startup via `a2a.AuctionCardsFn = a2aSrv.AuctionCardSource()`. `AuctionDelegate` builds the candidate map (agent name → A2A URL) from the registered agents' A2A cards (`EligibleBidders` restricted by `RequiredTags`, resolved to `cardURL`), runs the `Auctioneer` over the real `BTAgentClient` transport, and honors `chainState` overrides — `auction_candidates` (a name→URL map that replaces the derived set), `auction_required_tags`, `auction_min_confidence`, and `auction_task_id` — for candidate selection and announcement shaping. A curated `auction_demo` domain tree (program milestone 5/5, `internal/domains/trees.go`) exposes the auction via `switch_tree` behind an `IsAuctionTask` gate. Earlier revisions fronted `AuctionDelegate` with separate `AnnounceTask` and `CollectBids` `Action` nodes so the tree "read as" the full protocol, but those names are unregistered in the engine and resolved to the permissive success fallback — a silent no-op that reported success while surfacing no announcement/bid evidence. Because `RunAuction` already performs the announce and collect stages inside `AuctionDelegate`, the honest tree collapses to that single real seam; the runtime test `TestAuctionDemoTreeHasNoSilentNoOps` fails if any node is a silent no-op.

## 8.13 Leaf-Node Structural Validation

**What:** A tree-authoring invariant enforced before build: node types whose `engine.buildNode` builders construct childless leaves — `Action`, `Condition`, `AlwaysSucceed` — must not declare `Children`. Both validation entry points reject a violation: `walkValidate` (the 5-stage `ValidateTreeFull`) appends `node %q: leaf type %q must not declare children (got N)`, and the flat `validateNode` (`ValidateTree`, consumed by preflight / `BuildAndValidate` callers) appends `<name>: <type> leaf must not declare children`. The two paths share a single `leafNodeTypes` set so they cannot drift.

**Why:** `buildNode` silently discards `node.Children` for these types (e.g. `tree.go` `case "AlwaysSucceed"` returns a childless action ignoring any declared children). Before this rule an author could nest a subtree under such a leaf and lose it at build time with no validation signal — the same silent-no-op class the platform guards against elsewhere (§8.12, R11). Surfacing it as a validation error turns a silent structural mistake into an actionable message at both authoring choke points.

**Where:** `internal/engine/verifier.go` (`leafNodeTypes`, `walkValidate`), `internal/engine/validate.go` (`validateNode`), against the childless builders in `internal/engine/tree.go` (`buildNode`).

**Effect:** A leaf-type node carrying children fails validation instead of building into a runnable tree that quietly drops the subtree; the shared `leafNodeTypes` map keeps the deep (`ValidateTreeFull`) and flat (`ValidateTree`) validators in lockstep.

## 8.14 Fleet-Wide Node Description Coverage

**What:** A tree-authoring invariant over the registered fleet: every node of every type — root, interior composites (Sequence, Selector, decorators), and leaves (Condition, Action, ChainAction) — in every registered domain tree — including, since 2026-07-05, the 13 generated `arc42:*` trees — must carry a non-empty `Description`, and every tree a non-empty `Descriptions` entry. The invariant extends beyond the `AllDomainTrees` registry to the production-reachable non-registry trees — the smoke-tested extras (`hermes_evolve` and the six `kanban_*` trees) and the resolver-reachable extras (`hermes_obsidian`, `superpowers_pipeline`) — guarded per node only, since `TestDescriptionsHaveNoOrphans` deliberately forbids them a root `Descriptions` entry. The `seq()`/`sel()` authoring helpers (`internal/domains/trees.go`) take a mandatory description parameter, so an undescribed composite cannot even be constructed through the helper API; the arc42 `chain()` helper (`internal/domains/arc42_trees.go`) likewise takes a mandatory description, so an undescribed LLM stage cannot be built either.

**Why:** Node descriptions are the human-readable rationale the gardener and the bt-agent `switch_tree` tool surface. Coverage grew incrementally — root `Descriptions` entries, then Conditions, then leaves — but interior composites stayed exempt: the helpers built every `StrategyRouter` Selector (the primary routing decision point) and every Sequence stage (`PreGate`, `BugDetection`, `BuildPath`, …) with a blank `Description`, so exactly the nodes that encode routing and stage semantics were the unexplained ones.

**Where:** `internal/domains/trees.go` (`seq`/`sel` signatures and their curated call sites), described composite literals across the other registered-tree files in `internal/domains/` and the evolution-built subtrees (`internal/evolution/goap_trees.go`, `internal/evolution/notebooklm_workflow.go`); guards in `internal/domains/domains_test.go` — `TestAllDomainTreeSelectorsHaveDescriptions` (router anchor) and the consolidated `TestAllDomainTreeNodesHaveDescriptions`, which walks every node of every curated registered tree, `TestNonRegistryDomainTreeNodesHaveDescriptions`, which applies the same full-node walk to the non-registry trees (`internal/domains/hermes_evolve.go`, `hermes_obsidian.go`, `kanban.go`), and `TestArc42DomainTreeNodesHaveDescriptions`, which applies it to the 13 `arc42:*` registry trees.

**Effect:** Every routing decision and execution stage in the registered fleet — curated and generated alike — is self-documenting at the node level, including the production-reachable trees outside the registry, whose composites and Action/ChainAction leaves were previously exempt; the consolidated full-node guards prevent any future tree from regressing on any node class, while the narrower sibling guards (root, condition, leaf, selector) remain as targeted regression anchors.

## 8.15 Validation-Gated Composition Activation

**What:** The `bt_blocks_compose` MCP tool treats operator input as a contract, not a hint. A non-empty `strategy` id that `resolveTree` cannot resolve is rejected with `unknown strategy tree %q` — via the shared `resolveStrategyTree` helper hoisted above all three compose branches (preset, task-tree, block-list) — instead of composing with the StrategyRouter silently omitted. With `save: true`, the composed tree reaches the tree store and replaces the live tree only after `engine.ValidateTree` returns no messages *and* `treeStore.Save` succeeds; either failure returns an MCP error carrying the validation messages or save error and leaves the active tree untouched. A gated save that actually happened is signalled explicitly by `"saved": true` in the success payload.

**Why:** Both prior behaviors were the §8.12/§8.13 silent-no-op class surfacing at the operator boundary: a strategy typo produced a tree that "read as" routed but had its requested condition-node routing amputated while still reporting `composed: true`, and an invalid composition — unknown node types, the same failure class the `auction_demo` tree hit at build time — was unconditionally activated as the live tree with its `Save` error discarded, so the operator learned nothing until the tree dead-lettered.

**Where:** `cmd/bt-agent/blocks_tools.go` (`resolveStrategyTree`, the gated `params.Save` block); pinned by `TestBTBlocksComposeRejectsUnknownStrategyTree` and `TestBTBlocksComposeSaveGatesActivation` (`cmd/bt-agent/blocks_tools_test.go`).

**Effect:** A composition either does exactly what was asked or fails loudly with an actionable message, and the live tree can no longer be replaced by a composition the engine's own validator would refuse. The same fail-loud sweep hardened the operator CLI: `requireNameArg` (`cmd/bt-agent-cli/main.go`, pinned by `TestRequireNameArg`) guards the `test`/`logs`/`delete` subcommands' positional agent-name read, printing `Error: agent name required` plus usage and exiting 1 where the unguarded `os.Args[2]` read previously panicked with index-out-of-range.

---

*Generated by bt-agent arc42 pipeline — section8Concepts tree; extended 2026-07-08*


---

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
- ✅ Production wiring landed (2026-07-05): the daemon now persists knowledge-graph runtime feedback (Fitness/RunCount/tool-edges) to the canonical `~/.go-bt-evolve/feedback.json` (`agent.FeedbackFile()`) by setting `SchedulerConfig.FeedbackPath`, so the learn→evolve loop rehydrates instead of resetting on restart — the config assembly is factored into a `buildSchedulerConfig` helper and pinned end-to-end by `TestDaemonWiresFeedbackPersistencePath` and `TestDaemonSchedulerConfigWiresFeedbackPath` (see §8.4)
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
- ✅ Failed work preserved: DLQ enables manual inspection and replay — and, cross-process, drop-safe requeue (2026-07-09, ADR-025): the dashboard flags an entry for the executor to retry instead of removing it
- ✅ DLQ entries are self-diagnosable (2026-07-08): the scheduler retry closure's non-success branch (`cmd/bt-agent`) folds the run-output tail into the attempt error via the exported `agent.OutcomeErrorDetail` (a package-level `attemptOutcomeError` helper), matching what `RunOnce` already recorded internally — so a retry-exhausted `agent outcome: …` DLQ record carries the last ~400 bytes of run output (newlines flattened to `" | "`, `"no run output"` when empty) instead of a bare outcome word
- ✅ Per-agent circuit breakers: One misbehaving agent doesn't block others
- ⚠️ Retry delays add latency (1s→2s→4s→8s backoff)
- ✅ DLQ bounded (2026-07-09, ADR-025): `DeadLetterQueue.Push` caps retained entries at `MaxDeadLetterEntries` (1000) with oldest-first eviction, and a poison-pill entry whose replay `Attempts` reach `MaxReplayAttempts` (5) is terminally flagged `Abandoned` and excluded from further auto-requeue — resolving the earlier unbounded-growth caveat

---

## ADR-008: Auction-Based A2A Task Allocation

**Context:** Multi-agent coordination needed to route a unit of work to the best-fit agent among several candidates rather than a hard-coded delegate. The A2A layer already held the pieces — announce/collect, bid evaluation → Award, and task dispatch — but nothing composed them, and per-agent servers ran announcement JSON as literal task text, so no live agent could return a well-formed bid.

**Decision:** Adopt a contract-net protocol on the A2A transport. `Auctioneer.RunAuction` composes announce → evaluate → dispatch: candidates score a `TaskAnnouncement` against their own agent card (`ScoreAnnouncement`) and reply with a `Bid`; a `ScoreEvaluator` awards the lowest-cost eligible bid; the winner alone is dispatched the real task. Per-agent endpoints become bid-aware — `BTAgentExecutor.Execute` recognizes announcements and answers with a bid artifact instead of running the JSON as a task. The auction is reachable from any behavior tree through an engine-side `AuctionDelegate` action that calls an injected `AuctionDelegateFn` hook (mirroring the `DelegateToA2AFn`/`DelegateToTreeFn` seams that keep `internal/engine` free of an `internal/a2a` import) and falls back to `DelegateToTreeFn` when no bidder wins.

**Status:** Accepted (2026-07-04)

**Consequences:**
- ✅ Task allocation closes end to end over the live A2A transport
- ✅ Bidding is card-driven: focused specialists beat diluted generalists on cost/confidence
- ✅ Bad candidates (unreachable, silent, malformed, foreign-task) are tolerated, never failing the auction
- ✅ The auction is composable inside behavior trees (the `AuctionDelegate` node), with a DelegateToTree fallback when no bidder wins — the curated `auction_demo` tree (milestone 5/5) is selectable via `switch_tree` and collapses to the single `AuctionDelegate` seam rather than fronting it with unregistered no-op stages (guarded by `TestAuctionDemoTreeHasNoSilentNoOps`)
- ✅ Production wiring landed (2026-07-04): `internal/agentexec` installs `engine.AuctionDelegateFn` at link time and the daemon supplies the live candidate source (`a2a.AuctionCardsFn` from the A2A card registry), so the node auctions over the real `BTAgentClient` transport instead of reporting *not configured* — pinned by the binary-level `TestDaemonConfiguresAuctionDelegateHook`
- ✅ Registry-seam hardening (2026-07-04, milestone 4/4): `AuctionDelegate` now registers through `engine.RegisterAction` rather than writing straight into `actionRegistry`, so it gains the shared `bt.action/AuctionDelegate` tracing span (§8.11) and the duplicate-registration guard like every other engine action — pinned by `TestAuctionDelegate_EmitsTracingSpan`
- ⚠️ Card-based scoring is a coarse capability proxy; no bid signing or trust verification yet — though bid attribution is now anchored to the delivery identity (candidates-map key) rather than the self-reported `BidderName`, so a candidate cannot mis-attribute its bid to another name
- ⚠️ Adds a second interpretation of A2A task text (announcement vs. ordinary task) at the responder

---

## ADR-009: Deterministic, LLM-Free Evolution MCP Tools

**Context:** MAP-Elites quality-diversity and NSGA-II multi-objective optimization existed in `internal/evolution`, but neither had a standalone MCP entry point, and their full drivers (e.g. `EvolveMAPElites`) invoke the LLM supervisor — non-deterministic and unusable under `-short`. Separately, asserting or exercising a registered MCP tool by name required driving the stdio JSON-RPC loop, because `Server.tools`/`Server.handler` are private.

**Decision:** Expose two deterministic, LLM-free tools from `cmd/bt-agent`: `bt_evolve_qd` (evolves a `Population` with the shared `structuralFitnessFn`, inserts into `NewMAPElitesGrid`, and reports `diversity_score`/`cell_count`/`elites`/`specialist_distribution`) and `bt_evolve_multiobjective` (runs `NSGAIIPopulation.Evolve` over the fixed `DimSuccessRate`/`DimNodeEfficiency`/`DimStability` axes using `StructuralMultiFitness`, reporting best `node_count`, per-dimension bests, and Pareto-front size). Both reuse the existing structural metrics rather than the LLM path, keeping them `-short`-safe. Add a matching in-process seam to the shared `engine.Server` — `HasTool(name)` and `Invoke(name, args)` — so tools can be asserted and driven by name without the stdio loop (§8.3).

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ MAP-Elites and NSGA-II are reachable as deterministic, `-short`-safe MCP tools that reuse one structural-fitness definition
- ✅ `engine.Server` tools are now unit-testable by name via `HasTool`/`Invoke`
- ✅ The first deterministic caller of `NSGAIIPopulation.Evolve` surfaced and fixed a crowding-distance index-out-of-range in `internal/evolution/multi_objective.go`: the stale `assignCrowdingDistance(front.Indices)` call (which indexed pre-update slices with combined-population indices) was removed — crowding distance is recomputed by `Evaluate` against the rebuilt population
- ⚠️ `Invoke` deliberately skips auth/rate-limit/tracing, so it must stay a test/in-process seam, never a production request path

---

## ADR-010: Non-Wedging Self-Halting Circuit Gates for the GOAP Fusion Loop

**Context:** The scheduled GOAP fusion loop (`goap-fusion-loop-runner`, §6.4) derives its whole halt/continue verdict from the published state-hash history (`goap_fusion_state_hashes`): `PublishGoapFusionStateHash` is the producer, `EvaluateScheduledGoapFusionCircuitBreaker`/`RunScheduledGoapFusionLoop` the consumers. Two guards protect against the Activity-Progress Confusion failure mode — a repeated-state breaker over a bounded window (`goapFusionCircuitHistoryWindow = 3`) and a runaway-loop backstop that halts once the history reaches a finite ceiling (`goapFusionMaxLoopIterations = 50`) even when every hash is distinct. Both guards, as first shipped, could *permanently* wedge the recurring runner rather than merely halting the offending cycle: the backstop halted but never pruned the durable history, and the history cap equalled the backstop threshold, so every subsequent cron tick re-tripped the backstop and dead-lettered. Symmetrically, an idle tick (no active program milestone and an empty prioritized goal queue) re-derived the identical empty-queue hash and, appended each tick, could pile up a window of identical idle hashes and falsely trip the repeated-state breaker while the loop was merely waiting for work.

**Decision:** Make the self-halting gates halt-but-never-wedge. (1) The runaway backstop is **half-open**: on trip `RunScheduledGoapFusionLoop` calls `ClearGoapFusionStateHashes` before HALTing, so the next cron tick starts from a fresh window instead of re-HALTing forever. (2) The durable history cap is held **strictly greater** than the backstop threshold — `goapFusionStateHashHistoryCap = goapFusionMaxLoopIterations + goapFusionCircuitHistoryWindow` — so the cap alone never pins the persisted history at the trip point; only genuine accumulation reaches the backstop, and that is self-cleared by the half-open trip. (3) `PublishGoapFusionStateHash` skips publishing entirely on an idle tick (empty queue **and** no milestone), leaving the durable history untouched so repeated-state detection stays reserved for real work.

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ A runaway or stuck cycle still self-halts, but the runner recovers on the next tick instead of dead-lettering forever (removes the recurring `goap-fusion-loop-runner` permanent-wedge class)
- ✅ The repeated-state breaker no longer false-trips on an idle loop; a repeated hash counts as a cycle signal only when there is a populated queue or an active milestone
- ✅ Cap headroom keeps both guards functional after truncation (the window and backstop still see enough history)
- ⚠️ A half-open backstop trades a hard stop for a bounded retry — a genuinely pathological plan can burn one more window of cycles per tick before re-tripping, relying on the plan-clear-on-failure path (§6.4) to break out

---

## ADR-011: Adopting the Horizontal-Scaling Substrate (RemoteExecutor + AgentRouter)

**Context:** `internal/reliability` already held the pieces for multi-node task distribution — `RemoteExecutor` (an HTTP-backed `AgentExecutor` that can share a `ConnPool`), `LocalExecutor` (the in-process fallback), and `AgentRouter` (health-aware round-robin/least-connections routing with per-executor zombie cooldown and local fallback). But no production binary constructed them from real runtime state: the router was reachable only from tests, so a running `bt-agent` had exactly one task-execution path (risk R2). The A2A layer separately maintained a live card registry of peer nodes, but nothing reduced those cards to routable endpoints.

**Decision:** Wire the substrate into the daemon from the live A2A card registry. `reliability.NewRouterFromEndpoints(local, endpoints)` is the adoption seam: it turns each `AgentEndpoint` with a non-empty `BaseURL` into a `RemoteExecutor` and installs the passed-in `LocalExecutor` as the fallback (never adopting the first remote as local). `AgentEndpoint` is a reduced, transport-agnostic shape so `reliability` keeps no dependency on the A2A card types. In `cmd/bt-agent`, `endpointsFromCards` reduces `a2aSrv.CardCache` to endpoints — each card's JSON-RPC interface URL is collapsed to its scheme+host base, the daemon's own `selfBaseURL` is excluded so the router never routes back to itself, unreachable cards are skipped, and peers are de-duplicated by base URL so each node yields one executor rather than one per advertised agent. `newLocalAgentExecutor` adapts the daemon's `RunDeps.RunOnce` to a `reliability.AgentResult` as the fallback. Alongside, `ConnPool.Stats` was corrected to report the transport's actual limits (and a new `IsShared` flag) instead of hard-coded constants.

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ `bt-agent` is the first production binary to construct the `RemoteExecutor` + `AgentRouter` substrate from real runtime state (the A2A card registry), pinned by binary-level wiring tests
- ✅ A single-node registry yields no peers, so the router routes every task to the local executor — single-node deployments behave exactly as before, and the seam goes live the moment peer cards join the registry
- ✅ `ConnPool.Stats` now reflects real transport limits, so pool diagnostics are trustworthy for multi-node connection reuse
- ⚠️ Milestones 1–2 of 5: the router is constructed and logged but not yet threaded into the scheduler/MCP task path, so it does not yet distribute live work — routing adoption and peer-discovery refresh are later milestones
- ⚠️ Endpoint reduction trusts card interface URLs; there is no peer authentication or health pre-check at construction time beyond the router's own runtime health/cooldown gating

---

## ADR-012: Wiring the Scalability Substrate into the Dashboard Endpoint and Probe (Milestones 3–4)

**Context:** ADR-011 constructed the `RemoteExecutor` + `AgentRouter` substrate in `bt-agent`, but the surfaces that report and exercise it lagged behind. The dashboard's `/api/scalability` endpoint (`handleScalability`) still passed hard-coded placeholders to `NewScalabilityStatus` — `0` queue pending, `0`/`0` router total/healthy, `nil` heartbeat — even though the dashboard had no router of its own to read. The `bt-scalability-probe` only poked each node's execute endpoint independently, so it never demonstrated that a routed task stream actually fans out across backends.

**Decision:** Adopt the substrate in the two remaining surfaces. (Milestone 3) `cmd/bt-dashboard` now constructs `dashTaskQueue` (`reliability.NewTaskQueue`) and `dashAgentRouter` (`reliability.NewAgentRouter` fronting a single in-process `LocalExecutor` that adapts the dashboard's own agent executor) at startup. `handleScalability` reads their live state — queue depth via `TaskQueue.Len`, executor total/healthy via `AgentRouter.Executors`/`HealthyExecutors`, plus `ConsecutiveFailures` and `HeartbeatStats` — replacing the former `0`/`nil` arguments. `handlePipelineRun` (`pipeline_handlers.go`) enqueues each run onto the queue and drains it on completion, so the endpoint reports real pending depth rather than a constant `0`. (Milestone 4) `cmd/bt-scalability-probe`, when `--execute` is set, builds one `RemoteExecutor` per node, fronts them with a round-robin `AgentRouter`, and issues a routed dispatch stream; the emitted `distributed_dispatch` report records `distinct_nodes` (from the identities echoed in each result's `Output`) as direct evidence the stream reached more than one backend.

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ `/api/scalability` reflects the dashboard's live substrate (queue depth and router executor health) instead of static placeholders
- ✅ The scalability probe asserts genuine distributed dispatch — `OK` requires ≥2 dispatches over ≥2 distinct nodes — rather than independent per-node liveness checks
- ⚠️ The dashboard router starts single-node (one local executor) and gains remote peers only when `RemoteExecutor` peers are configured, so a single-node deployment still reports exactly one executor
- ⚠️ Milestone 5 of 5 remains open: threading the router into the live scheduler/MCP task path so production work is actually distributed across peers

---

## ADR-013: Making the Production Superpowers Pipeline Tree Operator-Selectable and Guarded

**Context:** `SuperpowersPipelineTree()` (`internal/domains/superpowers_pipeline.go`) is a production Superpowers SDLC tree — design artifact → safe worktree/baseline → implementation plan → native HITL approval gate → Claude Code TDD execution → verification → finish evidence — with no ChainAgent placeholders or unconditional skip paths. Yet `ResolveTreeID` (`internal/domains/tree_resolver.go`, the resolver consumed by bt-agent, A2A, and `switch_tree`) had no id mapping to it, so any request for it fell through the resolver's final `return evolution.DefaultTree()` and operators could never actually select it. It also escaped every coverage guard: it is absent from the `AllDomainTrees` registry (so `*HaveDescriptions` never checked it) and from the executable-structure smoke registries, leaving it exposed to blank-description and build-nil regressions.

**Decision:** Add an explicit resolver case (`"superpowers_pipeline"` → `SuperpowersPipelineTree()`) alongside the existing `hermes_obsidian` case, making the tree genuinely reachable via `switch_tree`. With it reachable, register it in the `resolverReachableExtraDomainTrees()` test registry — the same registry that guards `hermes_obsidian` — so `TestResolverReachableDomainTreesHaveSmokeStructure` builds it through the real engine (failing on panic/nil) and `TestResolverReachableDomainTreesHaveConditionDescriptions` walks every Condition node for a non-empty Description. `TestSuperpowersPipelineIsGuarded` additionally pins both the registry membership and resolver reachability so a rename cannot silently orphan the tree.

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ Operators can `switch_tree` onto `superpowers_pipeline`; the production Superpowers SDLC tree no longer collapses to `DefaultTree()`
- ✅ The tree is permanently protected from blank-description / build-nil regressions by the resolver-reachable coverage guards, the same protection `hermes_obsidian` already had
- ✅ Adds only the resolver case and a test-registry entry — no change to the tree's own definition or to `AllDomainTrees`
- ⚠️ The coverage guards live in the domains test registry, not in the resolver itself; a future resolver-reachable domains tree must be added to `resolverReachableExtraDomainTrees()` by hand to inherit the same protection

---

## ADR-014: Mandatory Descriptions for Every Node Class in Curated Domain Trees

**Context (2026-07-05):** Description coverage over curated trees had accreted piecemeal — guards existed for root `Descriptions` entries, Condition nodes, and leaves — but interior composites were structurally exempt: the `seq()`/`sel()` helpers (`internal/domains/trees.go`) constructed every Sequence and Selector without a `Description`, leaving the `StrategyRouter` routing points and stage sequences (`PreGate`, `BugDetection`, `BuildPath`, …) that the gardener and `switch_tree` surface unexplained, against the precedent set by the hand-built `agent_monitor` StrategyRouter.

**Decision:** Make descriptions mandatory for all node classes. `seq()` and `sel()` now take a description parameter (undescribed composites cannot be built through the helper API); all curated call sites and remaining composite literals — including the evolution-built subtrees in `internal/evolution/goap_trees.go` and `notebooklm_workflow.go` — were described; and the consolidated `TestAllDomainTreeNodesHaveDescriptions` guard asserts every node of every type in every curated (non-arc42) registered tree, plus the root `Descriptions` entry, is non-empty. Narrower guards (leaf, condition, `TestAllDomainTreeSelectorsHaveDescriptions`) stay as targeted regression anchors. See §8.14.

**Amended (2026-07-05):** The original guard was scoped to the `AllDomainTrees` registry, so the production-reachable non-registry trees — the smoke-tested `hermes_evolve` and six `kanban_*` trees, and the resolver-reachable `hermes_obsidian` and `superpowers_pipeline` — kept Condition-only coverage: their composites (`PreGate`, `OutcomeSelector`, pipeline routers) and Action/ChainAction leaves could still ship blank. Every node in those trees is now described (`internal/domains/hermes_evolve.go`, `hermes_obsidian.go`, `kanban.go`), and `TestNonRegistryDomainTreeNodesHaveDescriptions` extends the consolidated full-node walk to both non-registry sets — per node only, since `TestDescriptionsHaveNoOrphans` forbids non-registry root `Descriptions` entries.

**Amended (2026-07-05, arc42 exemption closed):** The last exempt tree class is now covered. The `chain()` helper (`internal/domains/arc42_trees.go`) — the sole source of blank-description nodes across the 13 generated `arc42:*` trees — takes a mandatory description (`chain(desc, prompt, maxTokens)`), and each of its 17 LLM call sites carries a concise per-section description; the `Descriptions` map (`internal/domains/trees.go`) gained curated entries for all 13 arc42 trees, which the gardener (`domain_arc42:*` builtins) and the bt-agent `switch_tree` tool had until now surfaced as blank. The `arc42:` skips were removed from `TestAllDomainTreesHaveDescriptions`, `TestDescriptionsHaveNoOrphans`, and the consolidated `TestAllDomainTreeNodesHaveDescriptions`, and a dedicated `TestArc42DomainTreeNodesHaveDescriptions` full-node walk guards the prefix, failing outright if the arc42 trees ever disappear from the registry.

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ Fleet-wide self-documentation: every routing decision and stage carries its rationale; no node class can regress on future trees — in the registry or in the production-reachable extras
- ✅ The helper signatures enforce the invariant at authoring time, not just at test time
- ✅ Description coverage is uniform across the whole registry: the `arc42:` exemption is closed, and `switch_tree`/gardener no longer surface blank descriptions for `domain_arc42:*` builtins
- ⚠️ A tree built without the described helpers relies on the consolidated guards alone
- ⚠️ The non-registry sets are enumerated by hand in the test (mirroring the smoke registry and `resolverReachableExtraDomainTrees()`); a new production-reachable tree outside `AllDomainTrees` must be added there to inherit full-node coverage

---

## ADR-015: Domain-Mapped Island-Model Evolution as a Deterministic MCP Tool

**Context (2026-07-05):** `IslandModel` (`internal/evolution/island.go`) — the algorithm documented since ADR-005 as "maintaining genetic diversity across domains" — was constructed by zero production binaries, and its instrumentation could not support one: migration counts were discarded (`Migrate()` returned a per-call count nobody accumulated) and `EvolveAll` incremented `Generation` a second time whenever migration fired, skewing the `MigrationInterval` cadence and misreporting generation numbers.

**Decision:** Fix the instrumentation and give the type its first production caller, extending the ADR-009 pattern. `IslandModel` gains a cumulative `TotalMigrations` counter (incremented by `Migrate()`, surfaced as `IslandStats.Migrations` and in `Summary()`), and `EvolveAll` advances `Generation` exactly once per call; migration selection is unchanged. `cmd/bt-agent` registers `bt_evolve_island` (tool 60), deterministic and LLM-free like `bt_evolve_qd`/`bt_evolve_multiobjective`: it seeds one `Population` per island from the resolved base tree, loops `EvolveAll` with the shared `structuralFitnessFn`, and reports `per_island_best`, `migrations`, `cross_diversity`, `generations`, and `islands`. An optional `domains` parameter (comma-separated registered domain-tree names) makes islands map to real domains — each name seeds its own island from `resolveTree("domain:"+name)`, keying `per_island_best` by domain name (the numeric `islands` param is then ignored); any unresolvable name aborts with `{"error":"unknown domain: <name>"}` before any evolution work.

**Status:** Accepted (2026-07-05)

**Consequences:**
- ✅ The island model is reachable in production for the first time, in its documented cross-domain role rather than only as anonymous same-seed islands
- ✅ Generation and migration reporting are now trustworthy: the `MigrationInterval` cadence holds exactly, and cumulative migrations survive across `EvolveAll` calls
- ✅ Deterministic and `-short`-safe, consistent with the ADR-009 tool family (single shared structural fitness, no LLM path)
- ⚠️ Structural fitness is the same for every island, so cross-island diversity comes only from independent mutation drift and migration — domain-specific fitness pressure remains future work

---

## ADR-016: Durable Claude Rate-Limit Backoff for the GOAP Fusion Loop

**Context (2026-07-08):** Rate-limited Claude outcomes were recorded but consumed nowhere: `goap_fusion_claude_review_rate_limited` was set by the review fallback and read only by its own test, and the plan-resume runtime re-attempted its 45-minute batch every tick against a quota known to be closed — so a closed Claude session burned a 15-minute doomed review run plus a doomed resume attempt on every half-hourly cron tick until the quota reopened.

**Decision:** Persist the rate-limit signal across ticks and make both Claude consumers honor it on entry. `saveClaudeBackoffState`/`loadClaudeBackoffState`/`claudeBackoffActive` (`internal/engine/actions_goap_fusion.go`) store an RFC3339 `goap_fusion_claude_backoff_until` deadline in the agent-scope blackboard (ChainState fallback), following the existing grill/plan durable-state pattern. `runClaudeCodeReviewResearch` short-circuits with the rate-limited outcome in milliseconds while the window is open and records a 1h backoff when `isClaudeRateLimit` fires; `runSuperpowersRuntimeFromExistingPlanAction` records the env-configurable window (`claudeBackoffWindow()`, `BT_GOAP_CLAUDE_BACKOFF`, default 6h) alongside the plan carryover and, on entry, degrades to ScheduledAnalysisPath before creating a worktree — emitting the exact `goap_fusion_rate_limited` shape whose deferred-clear guard preserves the carryover. Per ADR-010's non-wedging rule, an elapsed window self-clears (half-open) and a malformed timestamp reads as inactive, so stale or corrupt state can never permanently block Claude attempts.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ A closed Claude quota costs the loop one detection instead of a 15–60-minute doomed attempt per consumer per tick; deterministic ScheduledAnalysisPath work continues meanwhile
- ✅ The rate-limited plan carryover survives the backoff window and resumes on the first tick after expiry (pinned by tests on both consumers)
- ⚠️ The "resets \<time\>" hint in the CLI output is not machine-parsed, so the window is a heuristic: too short re-probes a still-closed quota, too long idles a reopened one — the half-open expiry bounds the damage to one skipped-or-doomed tick either way

---

## ADR-017: Experience-Grounded Evolution Closes the Learn→Discover→Evolve Loop

**Context (2026-07-08):** `ExperienceBank` (`internal/evolution/experience_bank.go`) — fully built and tested, and whose package header promised an `EvolveWithExperience` warm-start path — had zero production callers, so mutation experience was discarded after every run. Independently, the knowledge graph's `ComputeAnalytics().Bottlenecks` list (trees with `RunCount >= 3 && Fitness < 30`) was emitted only as human-readable `SuggestedActions` strings that nothing consumed: the persisted KG feedback (§8.4) fed no evolution.

**Decision:** Implement the promised path and wire it end to end. `Population.EvolveWithExperience` (`internal/evolution/learning.go`) runs the genetic algorithm with operator selection warm-started from the bank's top-5 `RetrieveByTreeType` hints (a 0.5 bias toward hint operators, `MarkReused` bumping reuse stats) and records each fitness-improving mutation back via `AddFromMutation`, discarding regressions; a nil bank degrades to plain `Evolve`. `cmd/bt-agent/main.go` constructs one persistent bank at `~/.go-bt-evolve/experience/experience.json` (via `agent.HomeDir()`, so `BT_AGENT_HOME` redirection holds; construction failure logs a warning and runs memoryless) and plumbs it through `mcpDeps`. `bt_evolve_genetic` now routes through `EvolveWithExperience` and reports `experience_bank_entries` and `experience_retrieval_hits`; a new deterministic `bt_evolve_bottlenecks` tool (tool 61, ADR-009 family: `structuralFitnessFn`, LLM-free, `-short`-safe) iterates every KG bottleneck, evolves each resolvable tree with the same bank, and returns a per-tree before/after fitness report, skipping — not aborting on — KG entries without a real tree. Wiring is pinned by `cmd/bt-agent/wiring_test.go` (`TestDaemonWiresExperienceBankPath`, `TestDaemonPlumbsExperienceBankIntoMCPDeps`, `TestBTEvolveGeneticRoutesThroughExperienceBank`) and seeded-deterministic bank tests in `internal/evolution/experience_bank_test.go`.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Mutation experience is durable for the first time: successful operators discovered in one run bias later runs on similar tree types, across restarts
- ✅ The learn→discover→evolve loop the persisted KG feedback (§8.4) was built for is closed — underperforming trees surfaced by runtime feedback now receive targeted, experience-grounded evolution via one tool call
- ⚠️ The bottleneck report's `before_fitness` is the KG runtime success rate while `after_fitness` is structural fitness — two different metrics, so the per-tree delta is directional evidence, not a like-for-like comparison
- ⚠️ Evolved bottleneck trees are reported, not auto-persisted — accepting an improved tree into the tree store remains a follow-up milestone of the Q2 program

---

## ADR-018: Bounded ExperienceBank with Quality-Aware Eviction

**Context (2026-07-08):** Once ADR-017 wired the `ExperienceBank` into the daemon, it accumulated forever: `Add` only ever appended, and `Persist` rewrites the whole `experience.json` on every addition, so an unbounded bank meant unbounded per-Add I/O and an ever-growing O(n) `Retrieve` scan. With the bank now fed by every fitness-improving mutation across restarts, growth was structural, not hypothetical.

**Decision:** Cap the bank at 500 entries (`experienceBankCap`, `internal/evolution/experience_bank.go`) with quality-aware eviction in `enforceCapLocked`: lowest `QualityScore` evicted first, oldest `CreatedAt` first among equal quality, and entries with `TimesReused >= experienceReuseProtection` (3) protected — they are evicted only after every less-proven entry is gone, so the cap still holds in a fully protected bank. The cap is enforced at both mutation points: `addEntry` (every `Add`) and `NewExperienceBank` on load, so oversized files written by earlier unbounded builds are trimmed on the first startup rather than only after the next Add. The ADR-003 atomic-write persistence format (`entries` wrapper, `.tmp` → rename) is unchanged; eviction is invisible to readers of the file format. Pinned by `TestExperienceBank_CapEnforcedOnAdd`, `_EvictsLowestQualityFirst`, `_EvictsOldestAmongEqualQuality`, `_ProtectsHighReuseEntries`, and `_CapEnforcedOnLoad`.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Per-Add persistence cost and retrieval scan time are bounded regardless of daemon uptime; legacy oversized files self-heal on load
- ✅ Eviction prefers keeping what demonstrably pays off: high-quality and repeatedly reused experiences outlive low-quality one-offs
- ⚠️ Eviction is permanent — a low-quality entry that would have become relevant to a future tree type is lost; the reuse-protection threshold (3) is a heuristic, not learned

---

## ADR-019: Production Entry Points for Memetic and Q-Learning Evolution

**Context (2026-07-08):** Two more registered evolution capabilities had no production caller (Q2 Evolvability, "Give every registered evolution algorithm a production entry point" program, milestones 2–3): `Population.MemeticEvolve` with the `LocalSearcher` strategies in `internal/evolution/local_search.go`, and the `QTable` reinforcement loop (`GetState`/`SelectAction`/`Update`, `internal/evolution/learning.go`) — the latter reachable only through `ReinforcementLearner.Suggest`, which nothing drove across generations.

**Decision:** Register two more ADR-009-family tools (deterministic, LLM-free, shared `structuralFitnessFn`, `-short`-safe) in `cmd/bt-agent/tools.go`. `bt_evolve_memetic` runs `MemeticEvolve` with a selectable `strategy` parameter mapping to `HillClimbSearch`/`SimulatedAnnealingSearch`/`TabuSearch`; an omitted strategy defaults to hill-climb, but an *unknown* value is rejected with `{"error":"unknown strategy: <value>"}` rather than silently defaulting, so caller typos surface. `bt_evolve_qlearning` drives a new `Population.EvolveQLearning`: each offspring mutation encodes the child via `QTable.GetState`, picks its mutation category epsilon-greedily via `SelectAction` (a pointer-typed `epsilon` distinguishes an explicit 0 — deterministic greedy — from an omitted default of 0.2), applies it through the `MCTSMutator` op materializer with a fitness-delta reward fed back through `Update` (regressions are discarded by the quality gate but still recorded, so the table learns to avoid them), and the tool reports the learned greedy policy via the new `QTable.LearnedActions` alongside `best_fitness`/`total_mutations`/`regressions`. Tree IDs are sanitized (`:` → `_`) before use as the QTable category so they cannot corrupt the `category:bucket:depth` state encoding. Pinned by `TestBTEvolveMemeticRegisteredAndValidatesStrategy` (per-strategy subtests plus the unknown-strategy rejection) and `TestBTEvolveQLearningRegisteredAndLearnsGreedily` (deterministic epsilon=0) in `cmd/bt-agent/tools_test.go`.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Memetic local search (all three strategies) and Q-learning-guided evolution are reachable in production; bt-agent grows from 61 to 63 tools
- ✅ The QTable's learn-across-generations loop finally has a driver, and its learned policy is observable per call (`learned_actions`/`learned_states`)
- ⚠️ The QTable is constructed per tool call — the learned policy is not persisted across invocations (unlike the ADR-017 ExperienceBank), so learning restarts from scratch each call
- ⚠️ Of the registered algorithms, CMA-ES remains without a production entry point (later milestones of the same program)

---

## ADR-020: CMA-ES Parameter Tuning Routed Through the Bottleneck Tool

**Context (2026-07-08):** CMA-ES was the last registered evolution algorithm without a production entry point (the open ⚠️ of ADR-019). It was also unreachable in practice: `collectParams` gated every extraction behind `node.Metadata != nil` and only recognized `timeout_ms`/`threshold` as Metadata keys, while `ApplyParameters` wrote back to the `TimeoutMs`/`MaxRetries` struct fields — so real trees with `TimeoutMs > 0` or `MaxRetries > 0` but nil Metadata yielded an empty parameter set and any selection gate on "has tunable parameters" could never fire.

**Decision:** Make extraction see what apply writes, then expose one seam and route through it. `ExtractParameters` now emits `timeout_ms` when `node.TimeoutMs > 0` (seeding `InitValue` from the actual field value, deduped against the Metadata-key case so a node yields at most one timeout param) and `max_retries` whenever `node.MaxRetries > 0` regardless of Metadata; all existing Metadata-key extractions are unchanged. A new exported `TuneTreeParameters(tree, populationSize, maxGenerations, fitnessFn)` runs the full Extract→Optimize→Apply pipeline: it returns `ok=false` without work when extraction is empty, otherwise optimizes via `CMAESOptimizer.Optimize` with an adapter that denormalizes each [0,1] candidate onto a scratch `cloneTree` copy scored by `fitnessFn`, and applies the best solution to a tuned clone — never mutating the input. `bt_evolve_bottlenecks` (`cmd/bt-agent/tools.go`) calls this seam per bottleneck: `ok=true` records `"algorithm":"cmaes"` with `tuned_params` and the CMA-ES `after_fitness`; otherwise the existing `NewPopulation` + `EvolveWithExperience` genetic path runs (`"algorithm":"genetic"`), preserving the skip-unknown-tree, nil-KG, and degenerate-population behaviors, with a top-level `algorithms` tally in the JSON result.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Every registered evolution algorithm now has a production entry point, closing the ADR-019 gap — the "GA establishes topology, CMA-ES fine-tunes parameters" division of labor from the package header is finally realized in one tool
- ✅ Bottleneck reports state which algorithm handled each tree, so operators can distinguish parameter-tuned from structurally-evolved outcomes
- ⚠️ The routing is all-or-nothing per tree: a tree with even one tunable parameter goes to CMA-ES only, so structural defects in parameterized trees are not addressed by this tool call
- ⚠️ CMA-ES-routed trees bypass the ExperienceBank — parameter tuning neither consumes nor records mutation experience (ADR-017 applies only to the genetic fallback)

---

## ADR-021: Gardener Cycles Record Into and Retrieve From the Shared ExperienceBank

**Context (2026-07-08):** ADR-017 made mutation experience durable for the daemon's MCP evolution tools, but the platform's primary mutation producer — bt-gardener's `RunCycleV2` — was still experience-blind: accepted mutations (op, target, measured fitness delta) were discarded after each cycle, and candidate ordering relied solely on the `evaluator.OrderMutations` heuristic (Q2 Evolvability, "Make the gardener experience-grounded" program, milestones 1–3).

**Decision:** Wire the gardener into the same bank on both sides of the loop. `gardener.Config` gains an optional `*evolution.ExperienceBank` (nil degrades to the historical no-op). On the *record* side, `evolveTreeV2` calls `AddFromMutation` for every accepted mutation with the per-candidate `candidateFitness.Composite − currentFitness.Composite` delta, captured before `currentFitness` advances so deltas are per-mutation, not cumulative; recording failures log a warning without aborting the cycle. On the *retrieve* side, `biasCandidatesWithExperience` (`internal/gardener/evolve_v2.go`) reorders the `OrderMutations` ranking via the new exported `evolution.RetrieveExperienceHints` (the same top-K `RetrieveByTreeType` query the ADR-017 warm-start uses): hints with `QualityScore ≥ 0.5` boost matching op/target candidates by `0.15 × quality` under a stable sort — non-matching candidates keep their relative heuristic order — and matched entries are `MarkReused`. `cmd/bt-gardener` constructs the bank at `agent.HomeDir()/experience`, deliberately identical to the daemon's `experienceBankDir()`, so both binaries compound into one store (`BT_AGENT_HOME` remains the redirection seam); unlike the daemon's warn-and-run-memoryless fallback, `buildGardenerConfig` fails fast if the bank cannot be opened. Pinned by `TestBuildGardenerConfig_ExperienceBankSharedWithDaemon` (`cmd/bt-gardener/config_test.go`) and fake/seeded-bank tests in `internal/gardener/evolve_v2_test.go` (`TestEvolveTreeV2_RecordsAcceptedMutationExperience`, `_NilExperienceBankIsNoOp`, `TestBiasCandidatesWithExperience_SeededBankReordersCandidates`, `_NilOrEmptyBankKeepsOrder`).

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ The learn side of the loop now includes its highest-volume producer: mutations the gardener accepts in 24/7 cycles bias later gardener cycles *and* the daemon's `bt_evolve_genetic`/`bt_evolve_bottlenecks` runs, and vice versa — cross-binary experience sharing through one file
- ✅ Candidate ordering is no longer purely heuristic: operators proven on similar tree types are tried first, inside the existing quality-gate/rollback safety envelope
- ⚠️ Biasing only reorders candidates `OrderMutations` already proposed — the bank cannot introduce a mutation the heuristic didn't generate
- ⚠️ The two binaries write the same `experience.json` via read-at-construction + rewrite-on-Add; as first shipped, concurrent gardener and daemon processes could lose each other's recent entries between loads — closed 2026-07-08 by ADR-022's merge-on-Add reconciliation

---

## ADR-022: Two-Writer-Safe ExperienceBank Persistence and Uniform Evolve-Population Validation

**Context (2026-07-08):** ADR-021 made daemon and gardener share one `experience.json`, but each writer's `Add` rewrote the whole file from its own in-memory view loaded at construction — so two concurrent processes silently dropped each other's entries (the documented ADR-021 single-writer caveat). Separately, `bt_evolve_qd` and `bt_evolve_island` still handled degenerate populations with bare `population <= 0` defaulting instead of the shared `resolveEvolvePopulation` boundary check the other evolve tools use, so an explicit `population: 1` reached the engine's clamp paths rather than being rejected ("Make the gardener experience-grounded" program, milestones 4–5, Q3 Reliability).

**Decision:** Reconcile before rewriting, and finish the boundary-check rollout. `ExperienceBank.addEntry` (`internal/evolution/experience_bank.go`) now calls `mergeFromDiskLocked` before appending: the persisted file is reloaded and merged into memory by entry ID — disk-only entries are adopted, and for IDs present on both sides the higher `TimesReused` wins, so reuse counts recorded by the other writer survive this writer's rewrite; a missing or corrupt file leaves the in-memory state untouched, and the merged set still flows through the ADR-018 `enforceCapLocked` eviction. In `cmd/bt-agent/tools.go`, `bt_evolve_qd` and `bt_evolve_island` switch to a pointer-typed `population` parameter routed through `resolveEvolvePopulation`: an omitted population keeps the documented defaults (20 for qd; island preserves its per-island default of 10), while an explicit `population < 2` is rejected with `{"error":"population must be at least 2"}` before any engine work — making every population-taking `bt_evolve_*` tool share the one boundary contract. Pinned by `TestExperienceBank_TwoWriterInterleavedWritesPreserveAllEntries` and `TestExperienceBank_TwoWriterMergePreservesHigherTimesReused` (`internal/evolution/experience_bank_test.go`) and the table-driven `TestEvolveToolsRejectDegeneratePopulationAtMCPBoundary` (`cmd/bt-agent/tools_test.go`), which also proves rejection fires before dependency checks and never leaks partial happy-path results.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Concurrent daemon and gardener writes compound instead of clobbering — closes the ADR-021 single-writer-at-a-time caveat, so cross-binary experience sharing holds under real 24/7 overlap
- ✅ All evolve tools reject `population < 2` uniformly at the MCP boundary (defense-in-depth above the engine-side eliteCount clamp), with per-tool defaults unchanged for callers who omit the parameter
- ⚠️ The merge shrinks the loss window to a single Add (read → rewrite) but is not a lock: two Adds racing inside that window can still drop one writer's newest entry — closed 2026-07-08 by ADR-024's sidecar flock, which holds that window exclusively
- ⚠️ Only `TimesReused` is reconciled field-wise on ID conflicts — divergence in other fields resolves to whichever writer rewrites last

---

## ADR-023: Full Prometheus Histogram Exposition and a Build-Identity Gauge

**Context (2026-07-08):** Q3 Reliability program "Make platform health measurable and deployment drift self-evident", milestones 1–3 of 4. The node/block duration `LabeledHistogram`s (`nodeDurationHist`/`blockDurationHist`, `internal/dashboard/bt_nodes.go`) were observed on every tick but rendered nowhere on the exposition — `HistogramSnap` carried only `Sum`/`Count`, so no bucket data existed to derive percentiles from and no latency alert could fire. `RecordTask` accumulated only a per-agent total-duration counter (`bt_agent_duration_ms_total`), which cannot answer percentile questions either. And the recurring stale-daemon-binary drift (three incidents to date) was detectable only by DLQ-message text heuristics — the running binaries carried no machine-readable identity.

**Decision:** Three additions to `internal/dashboard/metrics_utils.go`. (1) `Histogram.SnapshotStats` returns cumulative per-bucket counts (`HistogramSnap.Bounds`/`CumulativeCounts`, Prometheus semantics: the implicit `+Inf` bucket equals `Count`), and a shared `writeLabeledHistogram` renderer emits full histogram series (`_bucket` including `+Inf`, `_sum`, `_count`) for `bt_node_duration_ms` and `bt_block_duration_ms`. (2) `RecordTask` observes each task's duration into a new per-agent `LabeledHistogram` exported through the same renderer as `bt_agent_task_duration_ms`, alongside the unchanged counter. (3) The long-running binaries embed their VCS build identity: `dashboard.InstallBuildIdentity` (called at startup by `cmd/bt-agent` and `cmd/bt-gardener`) reads `runtime/debug.ReadBuildInfo`, logs revision/commit-time/dirty, and pins the `bt_build_info{revision,dirty}` gauge — always exactly one series (`SetBuildIdentity` replaces rather than accumulates), never-empty labels (missing VCS stamping degrades to an `"unknown"` sentinel, since an empty `revision` label would silently match every Prometheus matcher), and a scrape-time self-identify fallback so a binary that never installs one still exposes its own identity. Pinned by the histogram-rendering and build-identity tests in `internal/dashboard/metrics_utils_test.go` and the startup-logging tests in `cmd/bt-agent/main_test.go`.

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Latency-percentile alerting is possible for the first time — per node type/name, per block operation, and per agent — from data the platform was already observing
- ✅ Deployment drift is self-evident: the running revision is comparable against repo HEAD via a startup log line and a scrapeable gauge instead of DLQ-text forensics
- ⚠️ Detection is passive — no alert rule or watchdog yet compares `bt_build_info` revision against HEAD or restarts a stale daemon (the program's remaining milestone)
- ⚠️ The exposition is served only by bt-dashboard (`/api/metrics` on :9800); in bt-agent and bt-gardener the identity surfaces primarily as the startup log line unless those processes gain their own exposition endpoint

## ADR-024: Fail-Loud Compose, Feedback, and CLI Input Boundaries

**Context and decision (2026-07-08):** `bt_blocks_compose` silently dropped an unresolvable `strategy` tree while still reporting `composed: true`, and its `save: true` path discarded the `treeStore.Save` error and unconditionally activated the composition as the live tree — so a typo'd router id or an invalid composition (unknown node types, the `auction_demo` precedent of §8.12) shipped without any signal. The tool's boundary is now fail-loud (§8.15): a shared `resolveStrategyTree` rejects unknown strategy ids across all three compose branches, and persistence + live-tree activation are gated on `engine.ValidateTree` passing and `Save` succeeding, with `"saved": true` as the explicit success signal and the active tree left untouched on any failure. The same landing canonicalizes `recordUserFeedback`'s `user`/`treeID` once at entry (`cmd/bt-agent/feedback_tools.go`) so the stored record and the cumulative `FilterByTreeNameStrict` tally see the identifier the validator saw — previously a trailing-space tree id created reflection records no strict lookup could ever match — and guards the CLI's `test`/`logs`/`delete` positional-argument read (`requireNameArg`, `cmd/bt-agent-cli/main.go`) so a missing agent name prints a usage error instead of panicking. **Status:** Accepted (2026-07-08). Pinned by `TestBTBlocksComposeRejectsUnknownStrategyTree`, `TestBTBlocksComposeSaveGatesActivation`, `TestRecordUserFeedback_TrimsUserAndTreeID`, and `TestRequireNameArg`.

---

## ADR-024: Sidecar flock Serializes All ExperienceBank Write Paths

**Context (2026-07-08):** ADR-022's merge-on-Add narrowed the daemon/gardener lost-update window but left two residual races it documented itself. First, the in-process mutex does not exclude a second process: between one writer's `mergeFromDiskLocked` and its `os.Rename`, the other process could rename its own snapshot into place and have it silently overwritten. Second — and strictly worse — `MarkReused` and the exported `Persist()` rewrote the whole `experience.json` from this process's memory *without* merging at all, so a gardener bank loaded at startup that later marked reuse (which ADR-021's candidate biasing does on every matched hint) would erase every entry the daemon had persisted since load.

**Decision:** Serialize the whole merge→rename window under a cross-process lock and route every full-file write path through it. `acquireExperienceLock` (`internal/evolution/file_lock.go`) opens/creates the `<persistPath>.lock` sidecar and takes a blocking exclusive advisory `syscall.Flock(LOCK_EX)`, returning an idempotent release func; because flock attaches to the open file description, two separate opens of the sidecar exclude each other even within one process — the same shape the daemon/gardener cross-process case reduces to under test. In `internal/evolution/experience_bank.go`, `addEntry`, `MarkReused`, and `Persist()` now all run the same lock→merge→apply→cap→write+rename sequence via the extracted `persistLocked` write-and-rename helper, holding the sidecar lock from `mergeFromDiskLocked` through the atomic rename; `MarkReused` applies its reuse increments after the merge so they land on the merged view. If the lock cannot be acquired (e.g. read-only directory), the write degrades to ADR-022's unlocked merged path rather than dropping the entry. Pinned by `TestAcquireExperienceLockMutualExclusion`, `TestAcquireExperienceLockReleaseIdempotent`, and `TestAcquireExperienceLockMissingDir` (`internal/evolution/file_lock_test.go`) plus `TestMarkReusedDoesNotDropConcurrentWriterEntries` and `TestExperienceBank_ConcurrentWritersLoseNoEntries` (`internal/evolution/experience_bank_test.go`).

**Status:** Accepted (2026-07-08)

**Consequences:**
- ✅ Closes ADR-022's residual single-Add race: the merge→rename window is held exclusively, so concurrent daemon and gardener writes compound under real overlap instead of racing inside the window
- ✅ No exported write path bypasses the guard — `MarkReused` and `Persist()` no longer rewrite the file from stale memory, so gardener reuse-marking can no longer erase daemon entries added since load
- ⚠️ The lock is advisory Linux flock (the platform target) and blocks without timeout; a wedged holder would stall the other writer's persists
- ⚠️ ADR-022's field-wise caveat stands: only `TimesReused` is reconciled on ID conflicts — divergence in other fields still resolves to whichever writer rewrites last

---

## ADR-025: Drop-Safe, Bounded Dead-Letter Replay and an Honest Deferred SLO Outcome

**Context (2026-07-09):** Q3 Reliability program "Make the dead-letter queue drop-safe and truly replayable." Three residual hazards remained after the executor gained a replay path. (1) The dashboard runs in a *separate process* from bt-agent's executor and has no tree runner, yet `handleDLQReplay` called `dlq.Replay`, which removes the entry and persists the removal — a cross-process silent drop, since the executor would never see the task again. (2) `DeadLetterQueue` was unbounded (ADR-007's open caveat): under a sustained failure storm it grew without limit in memory and on disk. (3) A "poison pill" task that failed every replay could be auto-requeued forever. Separately, the scheduler recorded a graceful Claude rate-limit carryover (the `goap_fusion_rate_limited` sentinel, §6.4/ADR-016) via `SLOMetrics.RecordSuccess`, so an expected backoff inflated the success count and success-latency totals the gardener's validation gate reads.

**Decision:** Make dead-letter replay drop-safe across processes and bound it, and give the deferral its own SLO outcome. `DeadLetterEntry` gains a `RequeuedAt time.Time` field and the queue a `Reload()` (re-reads the file, discarding the stale in-memory view) plus a `Requeue(id)` method that stamps `RequeuedAt` and persists *without removing* the entry, so bt-agent's executor picks it up on its next scan. `handleDLQReplay` (`cmd/bt-dashboard/main.go`) now `Reload()`s then `Requeue()`s — returning `status: "requeued"` (OpenAPI updated in `internal/api/openapi.go`) instead of destructively replaying. Graceful-degradation guards land in `DeadLetterQueue` (`internal/reliability/reliability.go`): `Push` caps entries at `MaxDeadLetterEntries` (1000) with oldest-first eviction, and `Requeue` terminally flags an entry `Abandoned` once its replay `Attempts` reach `MaxReplayAttempts` (5), excluding it from further auto-requeue. On the metrics side, `SLOMetrics` gains a `DeferredCalls` counter and a `RecordDeferred()` method that increments *only* that counter (leaving `TotalCalls`/`SuccessfulCalls`/`FailedCalls` and the latency totals untouched, persisted via `SLOSnapshot.DeferredCalls`); the `cmd/bt-agent` scheduler retry closure — refactored into `recordSchedulerAttempt` — routes the `goap_fusion_rate_limited` carryover to `RecordDeferred` as a terminal, non-retried, non-dead-lettered disposition rather than `RecordSuccess`.

**Status:** Accepted (2026-07-09)

**Consequences:**
- ✅ Dashboard-initiated replay is drop-safe cross-process: the entry survives on disk flagged for retry rather than being silently removed by a process that cannot run it; `Reload` before `Requeue` prevents a stale in-memory copy from clobbering the executor's concurrent changes
- ✅ Resolves ADR-007's unbounded-DLQ caveat: the queue is capped with oldest-first eviction under a failure storm
- ✅ Poison pills cannot drive an infinite replay loop: an entry is terminally `Abandoned` after `MaxReplayAttempts` and retained only for inspection
- ✅ Rate-limit backoffs no longer skew reliability stats: a deferral is counted separately, so success rate and success-latency (which the gardener's validation gate reads) reflect only real outcomes
- ⚠️ Requeue relies on the executor scanning `RequeuedAt`; an executor that never re-scans leaves flagged entries pending (visible in `dlq.Len()` but unretried)
- Pinned by `cmd/bt-dashboard/main_test.go` (entry survives and is flagged), `internal/reliability/reliability_test.go` (eviction bound + poison-pill exclusion), and `cmd/bt-agent/main_test.go` (deferred recording leaves success/failure stats untouched)

---

## ADR-026: QD/Island Elite Fitness Write-Back into the Knowledge Graph

**Context (2026-07-09):** Q2 Evolvability program "Make the MAP-Elites quality-diversity archive durable and accumulating." The deterministic `bt_evolve_qd` (ADR-009) and `bt_evolve_island` (ADR-015) tools illuminated and evolved elites but only *reported* their fitness in the JSON response — nothing wrote it back into the `KnowledgeGraph`. So an archive-improved tree kept whatever runtime-EMA fitness (§8.4) it had before evolution, and `KnowledgeGraph` discovery could never surface it as improved. This was the QD/island analogue of ADR-017's open caveat that evolved trees are reported, not fed back.

**Decision:** Add a monotone, clamped `"evolved"` outcome to `KnowledgeGraph.RecordRun` (`internal/knowledge/feedback.go`) and call it from both tools. On an `"evolved"` record, `RecordRun` sets `tree.Fitness` straight from the elite's structural fitness (carried in `RunRecord.Quality`) via the new `evolvedFitness(current, elite)` helper — **bypassing** the 0.9/0.1 success EMA — but only when the elite improves on the current fitness, and clamped to `[0,100]`. A weaker elite never regresses a tree a stronger run already illuminated. `cmd/bt-agent/tools.go` gains a `recordEvolvedFitness(deps, treeID, eliteFitness)` helper (no-op when the graph is nil or the tree ID is empty — `RecordRun` also ignores unknown tree IDs, so the call is safe unconditionally): `bt_evolve_qd` writes back `grid.Stats().BestFitness`, and `bt_evolve_island` writes back the strongest island's best elite (the max over `stats.BestPerDomain`). Pinned by `internal/knowledge/feedback_test.go` (`TestRecordRun_Evolved_BumpsFitness`, `_Monotone`, `_Clamped`).

**Status:** Accepted (2026-07-09)

**Consequences:**
- ✅ Archive-improved trees carry their elite fitness into the `KnowledgeGraph`, so fitness-aware discovery can surface them and the improvement accumulates across runs rather than being discarded with the tool response
- ✅ The write-back is monotone and clamped, so it can only raise a tree's fitness within the valid range — a lucky-then-unlucky evolution sequence cannot degrade a tree below a prior best
- ⚠️ Only the elite's *fitness* is fed back, not the evolved tree structure itself; persisting the illuminated tree into the tree store remains a later milestone (consistent with ADR-017's open caveat)
- ⚠️ Elite structural fitness and the runtime-success EMA are different metrics sharing one `Fitness` field, so a high evolved value can outrank a tree's live success rate — the same directional-evidence caveat ADR-017 records for the bottleneck delta

## ADR-027: Fitness-Driven Selection Pressure in Deterministic Breeding and Discovery

**Context (2026-07-09):** Q2 Evolvability program "Close the selection-pressure gap — make tree breeding and deterministic discovery fitness-driven." ADR-026 wrote elite fitness back into the `KnowledgeGraph` so that fitness-aware discovery *could* surface improved trees — but the two always-on deterministic paths that consume the graph applied no such pressure. `Factory.selectParents` (`internal/knowledge/factory.go`) drew 2–3 breeding parents with a uniform `rand.Shuffle`, and the deterministic discovery fallback `stringMatch`/`matchScore` (`internal/knowledge/graph.go`) resolved equally-matching keyword and capability-overlap ties by sorted tree ID. Only the Ollama-gated embedding path (`embeddings.go:125`, `0.7*sim + 0.3*(fitness/100)`) actually blended fitness. So the fitness accumulated by runtime feedback (§8.4) and the ADR-026 write-back exerted zero influence on which tree was bred from or discovered when embeddings were unavailable.

**Decision:** Make both deterministic paths fitness-weighted, sharing one cold-start-aware helper.
- *Milestone 1/5 — breeding:* `selectParents` now draws parents via roulette-wheel sampling **without replacement** over `templateSelectionWeight` (a template's cold-start-discounted fitness) instead of a uniform shuffle, so high-fitness parents are drawn far more often. A `weightFloor` (0.01) keeps unrated templates reachable and degrades an all-zero pool gracefully to uniform. A seedable per-factory `rng` (`Factory.SetSeed`) makes the draw reproducible in tests; nil falls back to the process-global `math/rand`.
- *Milestone 2/5 — discovery:* both `stringMatch` phases blend persisted fitness into their tie-breaks — the keyword phase's equal-specificity tie and the capability-overlap phase's equal-score tie now resolve toward the fitter tree, mirroring the embedding path. Fitness **only breaks ties**: the raw match score still gates the 0.3 threshold and sets the returned confidence, and sorted tree ID remains the final deterministic fallback so map-iteration order can never decide a winner.
- *Milestone 3/5 — cold-start confidence:* a shared `coldStartWeightedFitness(fitness, runCount)` helper (`graph.go`) discounts fitness by `coldStartConfidence = (runCount+1)/(runCount+1+coldStartPrior)` with `coldStartPrior = 10`, called from both `selectParents` and `stringMatch`, so a single lucky `fitness=100/RunCount=1` tree cannot out-select a proven `fitness=70/RunCount=50` one. The `+1` keeps the multiplier equal across trees of equal run count, so fitness still fully discriminates two equally-unproven trees. Pinned by `TestFactory_SelectParentsFavorsHighFitness`, `TestStringMatch_FitnessBreaksKeywordTie`/`_FitnessBreaksCapabilityTie`, and `TestStringMatch_ColdStartDiscountsLuckyTree`/`TestSelectParents_ColdStartDiscountsLuckyTemplate`.
- *Milestone 4/5 — live fitness at breed time:* templates cached the KG fitness snapshot taken when `NewFactory` extracted them, so a fitness update applied afterward (by §8.4 feedback or the ADR-026 write-back) never reached breeding. `refreshTemplateFitness` (`factory.go`) now re-reads `KnowledgeGraph.Trees[SourceID].Fitness`/`RunCount` into the template's `Metadata` at breed time — invoked by `selectParents` over each candidate and by `Breed` over caller-supplied parents — so every breeding path weights off current fitness, not the stale snapshot. Templates whose `SourceID` is no longer registered keep their snapshot values. Pinned by a test that raises a tree's KG fitness after factory construction and asserts subsequent breeding prefers it.
- *Milestone 5/5 — selection pressure visible to the loop:* `ComputeAnalytics` now emits a `SelectionPressure []SelectionPressureEntry` list (`analytics.go`) of *proven-but-underbred* trees — `Fitness >= provenFitnessThreshold` (70) yet `RunCount < underbredRunThreshold` (5), sorted by fitness descending. These winners the loop keeps on the shelf are surfaced both in `FormatAnalytics` and as `SuggestedActions` ("Breed/exercise … apply selection pressure"), making unused proven trees actionable rather than idle. Pinned by an `analytics_test.go` case asserting such a tree appears in the report.

**Status:** Accepted (2026-07-09)

**Consequences:**
- ✅ The learn→evolve loop is closed end to end for the deterministic paths: fitness accumulated by §8.4 feedback and written back by ADR-026 now biases which trees are bred from and discovered, not just those matched by the optional embedding path
- ✅ Cold-start discounting is shared by both callers, so a lucky single run cannot dominate either breeding or discovery, and the deterministic tie-break behavior (sorted-ID fallback) is preserved when fitness is equal
- ✅ Breeding weights off *live* graph fitness (milestone 4), and proven-but-underbred trees are now reported by `ComputeAnalytics` (milestone 5), so a high-fitness tree the loop is starving of runs is both preferred when bred and visible for the loop to exercise
- ⚠️ Selection weight reads the same `Fitness` field that mixes runtime-success EMA and evolved elite fitness (the ADR-026/ADR-017 caveat), so an evolved-but-unproven tree can attract breeding/discovery pressure ahead of its live success rate until it accrues runs

---

*Generated by bt-agent arc42 pipeline — section9Decisions tree*


---

# arc42 Section 10 — Quality Requirements

## 10.1 Quality Tree

```
go-bt-evolve
├── #reliable
│   ├── Panic recovery: SafeGo on all goroutines, tree-level defer/recover
│   ├── Circuit breaker: 3-state (closed/open/half-open), per-agent, configurable threshold
│   ├── Retry with backoff: Full jitter, 3 classes (standard, LLM, unknown), max 3 retries
│   └── Dead letter queue: Persistent JSON, exhausted retries preserved
├── #evolvable
│   ├── 6 evolution algorithms: Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert
│   ├── Git-versioned trees: Every accepted mutation is a commit
│   ├── Benchmark gating: Pre/post fitness comparison with rollback
│   └── 10 mutation operators: add_before, add_after, wrap_retry, prune, swap_children, etc.
├── #secure
│   ├── Rate limiting: Token bucket, configurable rate
│   ├── API key auth: Bearer token validation on MCP and HTTP
│   ├── IP filtering: Allowlist/blocklist with CIDR support
│   ├── CSRF protection: Double-submit cookie pattern
│   ├── Security headers: X-Content-Type-Options, X-Frame-Options, etc.
│   ├── Audit logging: Request/response logging with dedup
│   └── Key rotation: Periodic API key refresh
├── #testable
│   ├── 295 test files across all packages
│   ├── 24+ passing packages (go test ./...)
│   ├── 78% average coverage (aspirational target: 85%)
│   ├── Test Watchdog cron: Detects new failures within 4h
│   └── Benchmark suite: BFCL, BTPG, ToolBench, SWE-bench integrations
├── #operable
│   ├── slog structured logging: JSON format, levels (debug/info/warn/error)
│   ├── Prometheus metrics: counters, gauges, full histogram series (_bucket/_sum/_count) + bt_build_info on /metrics
│   ├── Health endpoint: LLM availability check via bt_health
│   ├── Trace reader: OpenTelemetry spans with console tracer
│   └── Dashboard: 8-tab web UI on :9800
└── #flexible
    ├── 55+ trees across 8 categories: domain (23), finance (23), research, startup, thinktank, evolution, composed, core
    ├── 21-path merged main tree
    ├── 10 chain types: llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action
    └── YAML-defined agents: Easy creation, templating, import/export
```

## 10.2 Quality Scenarios

| # | Scenario | Stimulus | Response | Measure |
|---|---|---|---|---|
| QS1 | Agent process crash | Goroutine panic in ChainAction | SafeGo recovers in <1s, DLQ persists task, circuit breaker opens for agent | Recovery <1s, no process restart needed |
| QS2 | 100 consecutive evolutions | bt-gardener runs evolution cycle | No fitness drop >20% from baseline | Fitness delta tracked per-mutation (aspirational) |
| QS3 | Dashboard tree listing | GET /api/tree with 55+ trees | Returns all trees with metadata | Response <500ms |
| QS4 | Test regression detection | New test failure introduced | Test Watchdog cron detects within 4h | Detection latency <4h |
| QS5 | Concurrent MCP calls | 3 simultaneous bt_run_task | bt-agent handles all 3 without deadlock | All 3 complete within timeout |
| QS6 | Ollama outage | LLM health check fails | All LLM-dependent tools return degraded error, non-LLM tools continue | Graceful degradation, no crashes |
| QS7 | Disk full during persistence | writeFile fails with ENOSPC | Error logged, operation returns failure, no corruption (atomic write aborted) | No partial/corrupt files |
| QS8 | Config validation | Invalid config.yaml on startup | Load fails with clear error message, defaults used as fallback | Config validation error reported |

---

*Generated by bt-agent arc42 pipeline — section10Quality tree*


---

# arc42 Section 11 — Risks and Technical Debt

## Prioritized Risk Table

| ID | Severity | Risk | Impact | Mitigation |
|---|---|---|---|---|
| R1 | **RESOLVED** (2026-07-03) | ~~Mutation Death Spiral~~ — gardener overhaul: sandboxed benchmark cycles (Blackboard.Sandbox), per-tree quality gates (ValidateFor/IsDisabledFor), persisted trees loaded over builtins, rescaled gate floor. First fixed cycle: 22/50 trees mutated, 13 improved. | Evolution is productive again; post-convergence plateaus expected until fresh reflections arrive. | Keep per-tree metrics; feed new reflection records. |
| R2 | **HIGH** | **Single Point of Failure** — bt-agent is the sole task execution path for MCP tools. If it crashes or hangs, all Hermes Agent BT operations fail. | Dashboard sprints, cron jobs, and manual task delegation all blocked. | Add worker pool for horizontal scaling. Implement health-aware load shedding in gateway. **In progress (2026-07-05, ADR-011/ADR-012):** the daemon constructs the `RemoteExecutor` + `AgentRouter` substrate from the live A2A card registry (`NewRouterFromEndpoints`), the dashboard `/api/scalability` endpoint and `bt-scalability-probe` now read/drive it (milestones 3–4 done); still single-node until peer cards join and the router is threaded into the live task path (milestone 5). |
| R3 | **MEDIUM** | **Dead Code** — Graphify reports 327 isolated nodes (no edges to the main graph). Dangling functions, unused types, dead test helpers. | Binary bloat, confusing codebase navigation, wasted maintenance effort. | Dead Code Sweeper cron removes isolated nodes weekly. Graphify community analysis flags candidates. |
| R4 | **MEDIUM** | **Package Sprawl** — 36 packages for ~136 source files (3.8 files per package). Many packages are thin wrappers (2-3 files). | Import complexity, circular dependency risk, harder to understand boundaries. | Consolidate to ~22 packages. Merge thin packages into domain-coherent groups. |
| R5 | **MEDIUM** | **Dashboard Untested** — 910-line `cmd/bt-dashboard/main.go` with 0 dedicated tests. Pipeline handlers, task CRUD, and agent management have no test coverage. | Dashboard bugs go undetected until manual testing. Sprint failures are silent. | Add handler tests for all API endpoints. Add integration tests for sprint execution. |
| R6 | **MEDIUM** | **MCP + A2A Duplication** — Two separate server implementations (MCP stdio vs A2A HTTP) with overlapping auth, rate limiting, and tool registration. | Duplicated security logic, inconsistent behavior between protocols. | Extract shared server base. Unify auth, rate limiting, and middleware. |
| R7 | **LOW** | **DeepSeek API Dependency** — Escalation path depends on external API (api.deepseek.com). Outage or rate limiting blocks batch LLM work. | Batch processing delayed. Local Ollama is always available as fallback. | Monitor API health. Keep Ollama as always-available fallback. Consider additional providers. |
| R8 | **LOW** | **Evolution Engine Sprawl** — 13 graphify communities for evolution code. Overlapping strategies between Stockfish and Pareto causing redundant optimization. | Harder to reason about which algorithm applies when. Maintenance burden. | Strategy interface consolidation. Unify common pipeline stages across algorithms. |

### New Risks (2026-07-04)

| ID | Severity | Risk | Impact | Mitigation |
|---|---|---|---|---|
| R9 | **MEDIUM** | **Notebook corpus pollution** — weeks of self-description web research imported junk sources into the 334-source literature notebook. | Research answers cite junk; grill quality degraded. | Prune junk sources (needs human judgment on which of the 334 are legitimate); researcher now derives real queries. |
| R10 | **MEDIUM** | **Self-improvement navel-gazing** — with the literature source offline, all goals came from reviewing the loop's own commits; a full night landed only pipeline meta-improvements. | Platform capabilities stagnate while the pipeline polishes itself. | Structure-mode prompts steer to platform packages; seeded auction-allocation program; NotebookLM restored 2026-07-04. |
| R11 | **LOW** | **Loop-authored dormant scaffolding** — code written by the loop but never production-executed can hide landmines that only arm when wired (preflight cycle driver failed on first real execution). | Fleet-wide fast failures after wiring changes. | Contract tests at the binary level (cmd/bt-agent wiring test); arm dormant nodes with a manual verification run. |
| R12 | **LOW** | **Worktree/DLQ growth** — failed-run worktrees (24h grace) and the dead-letter queue accumulate. | Disk pressure on /tmp (1.9GB observed), noisy forensics. | Sweeper reaps >24h worktrees; DLQ growth now bounded — capped at `MaxDeadLetterEntries` (1000) with oldest-first eviction plus poison-pill abandonment (ADR-025). |

### New Risks (2026-07-08)

| ID | Severity | Risk | Impact | Mitigation |
|---|---|---|---|---|
| R13 | **MEDIUM** | **Stale daemon binary drift** — the long-running processes (`bt-agent.service`, bt-gardener, the gateway-spawned MCP children) keep serving code older than repo HEAD after autonomous landings; three incidents to date were diagnosed only by DLQ-message text heuristics (e.g. HALT text missing a fixed phrase). | Fixed behavior ships but does not run; recurring dead-letters are misattributed to code that is already fixed; triage is slow and heuristic. | Build identity embedded (ADR-023): startup log line + single-series `bt_build_info{revision,dirty}` gauge make the running revision directly comparable against repo HEAD. Automated mismatch alerting/restart remains open (program milestone 4/4). |

## Known Technical Debt

| Debt Item | Status | Resolution |
|---|---|---|
| Duplicate utility functions (strings, maps, template) | **Fixed** (2026-05-31) | Extracted to `internal/util/` with comprehensive tests (90.5% coverage) |
| Silent error suppression in test helpers | **Fixed** (2026-05-31) | All `_ = err` patterns replaced with explicit checks |
| DefaultTree god node (750+ lines) | **Fixed** (2026-05-31) | Extracted to 21-path merged tree across 7 category files |
| max_tokens audit (nodes with max_tokens=1) | **Aspirational** | Identify and fix ChainAction nodes with insufficient token budgets |
| Mutation quality gates | **Aspirational** | Add pre/post fitness delta comparison to evolution pipeline |
| Dead Code Sweeper cron | **Aspirational** | Weekly cron to detect and remove isolated code |

---

*Generated by bt-agent arc42 pipeline — section11Risks tree*


---

# arc42 Section 12 — Glossary

| Term | Definition |
|---|---|
| **A2A** | Agent-to-Agent protocol. Enables agents to discover each other via agent cards and delegate tasks. Runs on HTTP :8686. |
| **Action** | A leaf node in a behavior tree that performs a side-effecting operation (read file, call LLM, write output). Returns Success (1) or Failure (0). |
| **ADR** | Architecture Decision Record. Documents a significant design choice with context, decision, and consequences. Immutable once accepted. |
| **Auction (Contract Net)** | A2A task-allocation protocol: an Auctioneer announces a task, candidate agents bid from their agent-card capabilities, and the lowest-cost eligible bid is awarded and dispatched the work. `Auctioneer.RunAuction`, `internal/a2a/auction.go`. |
| **Award** | The auction outcome naming the winning bidder and its winning Bid for an announced task. Produced by ScoreEvaluator. |
| **Bid** | A candidate agent's offer to perform an announced task, carrying cost and confidence scored from its agent card via ScoreAnnouncement. Returned by the bid-aware A2A responder. |
| **Blackboard** | Shared state object passed through behavior tree ticks. Carries Task, Plan, Result, Outcome, ChainState, ChainTools, Reflections, TreeStore. |
| **Build Identity** | The VCS identity a binary was built from (revision, commit time, dirty flag), read via `runtime/debug.ReadBuildInfo`. Logged at startup by bt-agent and bt-gardener and exposed as the single-series `bt_build_info{revision,dirty}` gauge on the metrics exposition; unstamped builds report an `unknown` sentinel instead of an empty label. Makes stale-daemon-binary drift detectable by comparing against repo HEAD. ADR-023. |
| **BuildTree** | Converts a SerializableNode tree definition into a runnable go-bt Command tree. Validates structure before building. |
| **ChainAction** | A behavior tree leaf node that wraps an LLM call. 10 chain types available. Reads config from node Name and Metadata. |
| **Chain Type** | One of 10 LLM workflow patterns: llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action. |
| **Circuit Breaker** | 3-state pattern (closed/open/half-open) that prevents cascading failures. Per-agent isolation via AgentCircuitBreakerStore. |
| **Claude Backoff** | Durable rate-limit window for the GOAP fusion loop: a rate-limited Claude outcome persists an RFC3339 `goap_fusion_claude_backoff_until` deadline (agent-scope blackboard, ChainState fallback); while active, both Claude consumers skip in milliseconds — the plan-resume runtime degrades to ScheduledAnalysisPath preserving its carryover, the review fallback returns rate-limited without invoking Claude. Half-open: an elapsed or malformed deadline reads as inactive. `BT_GOAP_CLAUDE_BACKOFF` (default 6h) on the implementation path, fixed 1h on the review path. ADR-016. |
| **CMA-ES** | Covariance Matrix Adaptation Evolution Strategy (`internal/evolution/cmaes.go`): continuous optimization of node-level numeric parameters (timeouts, retry counts, thresholds) rather than tree topology. `TuneTreeParameters` is the production seam — Extract→Optimize→Apply on clones, never mutating the input; reached via `bt_evolve_bottlenecks`, which routes parameterized trees to CMA-ES and parameterless ones to genetic evolution (ADR-020). |
| **Condition** | A leaf node that evaluates a boolean predicate. Used in PreGate and OutcomeSelector for branching decisions. |
| **Dead Letter Queue (DLQ)** | Persistent JSON file (`dead_letter_queue.json`) that stores tasks whose retries have been exhausted. Bounded at `MaxDeadLetterEntries` (1000) with oldest-first eviction. The dashboard flags an entry for retry via `Requeue` (stamps `RequeuedAt`, keeps the entry on disk) rather than destructively replaying it cross-process; an entry whose replay `Attempts` reach `MaxReplayAttempts` (5) is terminally flagged `Abandoned` and excluded from further auto-requeue. ADR-025. |
| **Deferred Outcome** | A graceful, expected scheduler pause (e.g. the `goap_fusion_rate_limited` Claude rate-limit carryover) recorded via `SLOMetrics.RecordDeferred` into a dedicated `DeferredCalls` counter. It is terminal — neither retried nor dead-lettered — and deliberately leaves the success/failure counters and latency totals untouched so a backoff never inflates the success rate or success-latency the gardener's validation gate reads. ADR-025. |
| **DefaultTree** | The fallback behavior tree used when no specific tree matches. Extracted from a 750-line god node into 21 paths across 7 category files. |
| **Evolution** | The process of systematically improving behavior trees through mutation, fitness evaluation, and selection. 6 algorithms available. |
| **Experience Bank** | Persistent store of successful mutation experiences (`~/.go-bt-evolve/experience/experience.json`), EvoRepair-inspired. Warm-starts `EvolveWithExperience` operator selection via tree-type retrieval; only fitness-improving mutations are recorded. Wired into `bt_evolve_genetic` and `bt_evolve_bottlenecks` (ADR-017) and shared with bt-gardener, whose v2 cycles record accepted mutations and bias candidate ordering from the same file (ADR-021). Bounded at 500 entries: quality-aware eviction (lowest quality/oldest first, `TimesReused >= 3` protected) enforced on Add and on load (ADR-018). Safe for the two concurrent writers: every full-file write path (Add, MarkReused, Persist) re-merges on-disk entries by ID and holds an exclusive flock on the `experience.json.lock` sidecar across merge→rename (ADR-022, ADR-024). |
| **Expert Knowledge** | Curated design patterns (6) and anti-patterns (5) that guide tree evolution. Includes TreeArchetypes for each category. |
| **Fitness Score** | Multi-dimensional evaluation of a behavior tree's performance. Dimensions include correctness, completeness, conciseness, actionability. |
| **Gardener** | The evolution orchestrator (`cmd/bt-gardener`). Runs evolution cycles: evaluate → order mutations (experience-biased, ADR-021) → apply → re-evaluate → accept (recorded to the shared Experience Bank) or rollback. |
| **GOAP** | Goal-Oriented Action Planning. PlannerNode extends UtilitySelector with goal management, world state, and available actions. |
| **GOAP Fusion Loop** | The scheduled self-improvement cycle (`domain:goap_fusion_loop`, cron 0,30): research → goals → plan → implement → verify → land, wired with Phase-0 preflight, circuit gates, and state-hash producers. |
| **Grill** | Multi-turn critical NotebookLM review ("what is the framework missing?") rotating rounds 1-3 across cycles; answers feed the shared goal list. Round questions are served from the per-day query cache. |
| **HITL** | Human-in-the-loop approval gate node (HumanApprovalGate). Requests persist under `~/.go-bt-evolve/hitl/`; policy can auto-approve. |
| **Knowledge Store** | Content-hash-deduplicating research memory (`~/.go-bt-evolve/research/knowledge.json`): findings, vault notes, NotebookLM answers, and implemented goals; consulted before any research is reported. |
| **Partial Landing** | Multi-task run semantics: per-task snapshot commits let a later task's failure discard only its own edits; completed verified work still lands and the failed goal carries forward. |
| **Program / Milestone** | A research-proposed multi-cycle change (title + file-scoped milestones) persisted in `programs.json`; each cycle executes the next pending milestone at [P0] queue head and marks it done on a verified apply. |
| **Quota Economy** | Per-Pacific-day NotebookLM answer cache + daily budgets (queries/research starts) enforced at the nlmRun choke point; over budget the ResearchRouter falls back to Claude review, and a doubly-unavailable stage (fallback rate-limited too) skips non-fatally to vault context. |
| **Superpowers Run** | One durable implementation run: typed state (run.json), plan, per-task RED/GREEN evidence, verification artifacts, finish report — under `docs/superpowers/runs/<id>/`. |
| **Island Model** | An evolution algorithm where sub-populations evolve in isolation with periodic migration of top individuals. Reachable in production via the deterministic `bt_evolve_island` MCP tool; its optional `domains` parameter seeds one island per registered domain tree, matching the type's stated purpose of maintaining genetic diversity across domains. |
| **Knowledge Graph** | In-memory graph of all 41+ trees with capabilities, keywords, embeddings, and cross-tree relationships. Powers discovery and auto-creation. Its runtime-feedback fields (Fitness, RunCount, LastOutcome, LastDuration) and `uses_tool` edges can be snapshotted to / restored from an atomic JSON file via `feedback_persist.go` (`SaveFeedback`/`LoadFeedback`, debounced `FlushFeedback`) — now wired into the `internal/agent` scheduler lifecycle (`SchedulerConfig.FeedbackPath`: load on startup, throttled flush after each run, forced flush on Stop), so feedback survives restarts and the learn→evolve loop compounds. |
| **MAP-Elites** | Multi-dimensional Archive of Phenotypic Elites. Maintains a grid of high-performing individuals across behavioral dimensions for quality diversity. |
| **MCP** | Model Context Protocol. JSON-RPC 2.0 over stdio. 3 servers (bt-agent, bt-evaluator, bt-langagent) expose 71 total tools to Hermes Agent. |
| **Memetic Evolution** | Genetic algorithm hybridized with per-individual local search refinement (`Population.MemeticEvolve` + `LocalSearcher`, `internal/evolution/local_search.go`). Three strategies: hill-climb, simulated annealing (Metropolis acceptance), tabu search (genome-hash tabu list). Reachable in production via the deterministic `bt_evolve_memetic` MCP tool, which rejects unknown strategy values instead of silently defaulting (ADR-019). |
| **Mutation** | A structural change to a behavior tree. 10 operators: add_before, add_after, wrap_retry, prune, swap_children, rename_node, change_type, insert_fallback, clone_subtree, delete_subtree. |
| **OutcomeSelector** | The final stage of the universal BT pattern. Checks WasSuccessful → if not, triggers SelfCorrect. |
| **Pareto Front** | Set of non-dominated solutions in multi-objective optimization. Tracks trees that are not strictly worse than any other across all fitness dimensions. |
| **PlannerNode** | A behavior tree node that extends UtilitySelector with GOAP goal management. Selects actions based on world state and goal satisfaction. |
| **PreGate** | The first stage of the universal BT pattern. Validates preconditions (input valid, tools available, graph fresh) before executing the strategy. |
| **Q-Learning** | Reinforcement learning algorithm. State→Action mapping with epsilon-greedy exploration. Used for mutation strategy selection. Reachable in production via the deterministic `bt_evolve_qlearning` MCP tool, whose `Population.EvolveQLearning` drives per-generation mutation-category selection through `QTable.GetState`/`SelectAction`/`Update` and reports the learned greedy policy (`LearnedActions`) alongside the evolved winner (ADR-019). |
| **RetryWithBackoff** | Exponential backoff with full jitter. 3 retry classes: standard (500ms base), LLM-specific (1s base), unknown (1s base). Max 3 retries. |
| **RunTask** | Executes a behavior tree to completion. Tick loop (1000 max). Sets outcome (success/failure/partial). Validates output quality. |
| **SafeGo** | Wrapper around `go func()` that recovers panics and records them. Applied to all goroutine spawns. |
| **Selector** | A composite node that tries children in order until one succeeds. Used for StrategyRouter (primary → fallback → last resort). |
| **Sequence** | A composite node that executes children in order until one fails. Used for PreGate and ordered execution paths. |
| **SerializableNode** | JSON-serializable intermediate representation of a behavior tree. The bridge between YAML definitions and go-bt runtime trees. |
| **Stockfish Evolution** | Adaptation of Stockfish chess engine techniques: transposition table for caching, move ordering by predicted fitness delta, alpha-beta pruning. |
| **StrategyRouter** | The second stage of the universal BT pattern. A Selector that tries execution strategies in priority order (primary → fallback). |
| **Tick** | One execution pass through a behavior tree. Multi-tick decorators (Repeat) return Running (0) between ticks. Max 1000 ticks per RunTask. |
| **Transposition Table (TT)** | Cache of evaluated mutation states. Prevents re-evaluating identical tree configurations. Key component of Stockfish evolution. |
| **Tree Store** | Persistent storage for behavior tree definitions. Loads on startup, saves on mutation. Located in `~/.go-bt-evolve/`. Compositions saved via `bt_blocks_compose` are validation-gated before they persist or replace the live tree (§8.15, ADR-024). |
| **UtilitySelector** | A Selector variant that scores children by multi-dimensional utility and picks the highest-scoring path. |
| **Vault Manager** | Checkpoint/restore system for tree evolution. Saves tree snapshots to `~/.go-bt-evolve/vault/` for rollback. |

---

*Generated by bt-agent arc42 pipeline — section12Glossary tree*



---

*Generated by bt-agent arc42 pipeline — assembleDoc tree*
*Platform: go-bt-evolve | 55+ trees, 8 categories, 34 node types, 30 internal packages, 3 MCP servers*
*Full refresh: 2026-07-04 — kept current per landing run by the arc42 sync stage*
*Repository: https://github.com/greenTeeProduction/bt-agent-platform*
