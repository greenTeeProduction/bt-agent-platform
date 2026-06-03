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
│ agent/  dashboard/  workflow/  thinktank/  startup/         │
│ a2a/  kanban/  notebooklm/                                  │
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
│ knowledge/ (graph, discovery, embeddings, factory)          │
│ factory/ (agent factory, tree generator)                    │
├─────────────────────────────────────────────────────────────┤
│ INFRASTRUCTURE                                              │
│ security/  reliability/  metrics/  tracing/  config/        │
│ mcp/  reflection/  domains/  api/                           │
│ llm/  benchmark/  util/  gardener/                          │
└─────────────────────────────────────────────────────────────┘
```

### Responsibility Table

| Layer | Package(s) | Responsibility |
|---|---|---|
| Entrypoints | `cmd/bt-agent`, `cmd/bt-dashboard`, `cmd/bt-evaluator`, `cmd/bt-langagent`, `cmd/bt-gardener`, `cmd/bt-agent-cli`, `cmd/benchcmp`, `cmd/bt-docgen` | Process entry points. Each is a standalone binary with its own main.go |
| Service | `internal/agent`, `internal/dashboard`, `internal/thinktank`, `internal/startup`, `internal/a2a`, `internal/domains` | Agent lifecycle (registry, scheduler, memory), dashboard API, thinktank analysis, startup simulation, A2A protocol, domain-specific trees |
| Core Engine | `internal/engine` | Behavior tree runtime: BuildTree, RunTask, Blackboard, chains (10 types), action/condition registry (175+ nodes), event bus |
| Evolution | `internal/evolution` | 6 algorithms (Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert), mutation operators (10 types), fitness scoring, vault manager |
| Knowledge | `internal/knowledge`, `internal/factory` | Knowledge graph (38+ trees, capabilities, embeddings), tree discovery, auto-creation, factory breeding (crossover + archetypes) |
| Infrastructure | `internal/security`, `internal/reliability`, `internal/metrics`, `internal/tracing`, `internal/config`, `internal/mcp`, `internal/llm`, `internal/benchmark`, `internal/gardener`, `internal/util`, `internal/api`, `internal/domains` | Auth, rate limiting, circuit breakers, retry, DLQ, Prometheus metrics, OpenTelemetry, config loading, MCP protocol, LLM providers, benchmarks, evolution orchestration |

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
├── island_model.go      — IslandModel with periodic migration
├── q_learning.go        — State→Action epsilon-greedy policy
├── expert.go            — 6 design patterns, 5 anti-patterns, TreeArchetypes
├── mutations.go         — 10 mutation operators (add_before, add_after, wrap_retry, prune, swap_children, etc.)
├── learning.go          — cloneTree (sole deep-copy implementation)
├── vault_manager.go     — Tree vault with checkpoint/restore
├── types.go             — SerializableNode, Individual, Population, Fitness
└── fitness.go           — Per-tree fitness via reflection.FilterByTreeName
```

## 5.4 Level 2: Dashboard Whitebox

```
cmd/bt-dashboard/
├── main.go              — HTTP server on :9800, embed FS for static files, 8 route groups
├── pipeline_handlers.go — Sprint/quarter/year pipeline API handlers
└── static/              — Embedded web UI (HTML/JS/CSS)

internal/dashboard/
├── agents.go            — Agent listing, CRUD operations
├── executor.go          — AgentExecutor: shells out to `hermes chat` for BT delegation
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
