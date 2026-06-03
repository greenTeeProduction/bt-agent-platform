# BT Agent Platform

**Behavior-tree-driven AI agent framework** — 41 trees, 7 categories, 3 MCP servers, continuous self-evolution.

[![Go](https://img.shields.io/badge/Go-1.26.3-00ADD8?logo=go)](go.mod)
[![Platform](https://img.shields.io/badge/platform-Linux%20ARM64-009639?logo=linux)](https://github.com/greenTeeProduction/bt-agent-platform)
[![Tests](https://img.shields.io/badge/tests-71%2B%20files-brightgreen)](#)
[![Coverage](https://img.shields.io/badge/coverage-78%25-yellow)](#)
[![Trees](https://img.shields.io/badge/trees-41-blue)](#)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## What is this?

A Go framework for building, executing, and evolving behavior-tree-based AI agents. Agents are YAML-defined, BT-driven, MCP-exposed, and continuously improved through 6 evolution algorithms.

```
Agent YAML → Registry → Scheduler → resolveTree() → BuildTree() → RunTask() → Blackboard
```

## Quickstart

```bash
# Prerequisites: Go 1.26+, Ollama (optional for LLM agents)
git clone https://github.com/greenTeeProduction/bt-agent-platform.git
cd bt-agent-platform

# Run tests (no LLM needed)
go test -short -count=1 ./...   # ~5s, 24 passing packages

# Start the dashboard
go run ./cmd/bt-dashboard/ &
open http://localhost:9800       # 8-tab web UI

# Start bt-agent (MCP server)
go run ./cmd/bt-agent/           # JSON-RPC 2.0 over stdio, 36 tools
```

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│ ENTRYPOINTS                                             │
│ bt-agent  bt-dashboard  bt-evaluator  bt-langagent      │
│ bt-gardener  bt-agent-cli  benchcmp                     │
├─────────────────────────────────────────────────────────┤
│ SERVICE LAYER  (agent, dashboard, thinktank, startup)   │
├─────────────────────────────────────────────────────────┤
│ CORE ENGINE   (tree execution, chains, blackboard)      │
├─────────────────────────────────────────────────────────┤
│ EVOLUTION     (Stockfish, Pareto, MAP-Elites, Island,   │
│                Q-Learning, Expert Knowledge)            │
├─────────────────────────────────────────────────────────┤
│ KNOWLEDGE     (graph, embeddings, discovery, factory)   │
├─────────────────────────────────────────────────────────┤
│ INFRASTRUCTURE (security, reliability, metrics, mcp)    │
└─────────────────────────────────────────────────────────┘
```

## Key Features

- **41 Behavior Trees** across 7 categories: domain (10), finance (10), research (2), startup (8), thinktank (3), evolution (3), core (5)
- **10 ChainAction types** for LLM integration: `llm_call`, `agent`, `rag_query`, `tool_call`, `structured_output`, `refine`, `map_reduce`, `conversation`, `retrieval_qa`, `tool_action`
- **3 MCP Servers** (JSON-RPC 2.0 / stdio): bt-agent (36 tools), bt-evaluator (5), bt-langagent (2)
- **6 Evolution Algorithms**: Stockfish (chess-adapted), Pareto front, MAP-Elites, Island model, Q-Learning, Expert Knowledge
- **10 Mutation Operators**: add_before, add_after, wrap_retry, prune, swap_children, rename_node, change_type, insert_fallback, clone_subtree, delete_subtree
- **YAML-Defined Agents**: 24 templates, registry, scheduler, circuit breakers, dead letter queue
- **Web Dashboard** on :9800 with 8 tabs (Overview, ThinkTank, Company, Tasks, Tree View, Evolution, Agents, MindMap)
- **GOAP Planning** via PlannerNode with world state + goal-driven action selection
- **Reliability**: SafeGo panic recovery, 3-state circuit breakers, retry with full jitter, dead letter queue
- **Observability**: slog structured logging, Prometheus metrics, OpenTelemetry tracing

## Project Structure

```
cmd/                          # Entrypoints (7 binaries)
├── bt-agent/                 # Main MCP server (36 tools)
├── bt-dashboard/             # Web UI + REST API (:9800)
├── bt-evaluator/             # Fitness evaluation MCP server
├── bt-langagent/             # Langchain agent MCP server
├── bt-gardener/              # Evolution cycle orchestrator
├── bt-agent-cli/             # CLI for ad-hoc agent runs
└── benchcmp/                 # Benchmark comparison tool

internal/                     # Core libraries
├── engine/                   # BT runtime (tree, chains, registry)
├── evolution/                # 6 algorithms, mutations, fitness
├── knowledge/                # Knowledge graph, discovery, factory
├── agent/                    # Agent registry, scheduler, memory
├── dashboard/                # Dashboard backend
├── thinktank/                # Multi-perspective analysis
├── startup/                  # Company simulation
├── domains/                  # 41 domain-specific tree definitions
├── reliability/              # Circuit breaker, retry, DLQ
├── security/                 # Auth, rate limiting, CSRF
├── metrics/                  # Prometheus integration
├── tracing/                  # OpenTelemetry spans
├── mcp/                      # MCP protocol implementation
├── a2a/                      # Agent-to-Agent protocol (:8686)
├── llm/                      # LLM providers (Ollama, DeepSeek)
├── config/                   # Configuration loading/validation
├── benchmark/                # BFCL, BTPG, ToolBench integration
└── util/                     # Shared utilities

agents/                       # Agent YAML definitions
├── templates/                # 24 installable templates
└── workflows/                # 4 workflow definitions

docs/                         # Documentation
├── adr/                      # 7 Architecture Decision Records
├── arc42/                    # 12-section arc42 documentation
├── GETTING_STARTED.md
├── API_REFERENCE.md
└── TUTORIAL.md
```

## MCP Tools (43 total)

| Server | Tools |
|---|---|
| **bt-agent** | bt_run_task, bt_agent_create, bt_agent_list, bt_agent_run, bt_agent_schedule, bt_agent_history, bt_agent_memory_read, bt_agent_memory_write, bt_kg_discover, bt_kg_query, bt_kg_auto_create, bt_kg_summary, bt_kg_list, bt_kg_explain, bt_kg_analytics, bt_factory_create, bt_evolve, bt_evolve_genetic, bt_evolve_expert, bt_get_tree, bt_get_fitness, bt_get_reflections, bt_reset, bt_health, bt_use_domain_tree, bt_use_finance_tree, bt_use_research_tree, bt_use_go_tree, bt_thinktank_analyze, bt_workflow_run, bt_workflow_approve, bt_startup_simulate, bt_startup_summary, bt_delegate_to_tree, bt_list_finance_trees, bt_circuit_status |
| **bt-evaluator** | ev_evaluate, ev_order_mutations, ev_deepen, ev_tt_stats, ev_tt_save |
| **bt-langagent** | la_run, la_fitness, la_evolve |

## Agent Categories

| Category | Trees | Examples |
|---|---|---|
| `domain` | 10 | code_review, devops_ci, agent_monitor, refactoring, security_audit, data_pipeline, meeting_notes, crash_investigator, game_ai, trading_signal |
| `finance` | 10 | pitch_agent, earnings_reviewer, market_researcher, model_builder, meeting_prep, valuation_reviewer, gl_reconciler, month_end_closer, statement_auditor, kyc_screener |
| `research` | 2 | deep_research, quick_research |
| `startup` | 8 | CEO, CTO, PM, Engineer, Marketing, Sales, Designer, Operations |
| `thinktank` | 3 | synthesis, peer_review, report |
| `evolution` | 3 | stockfish_evolve, stockfish_loop, vault_manager |
| `core` | 5 | default, godev, hermes_evolve, notebooklm, kanban workflows |

## Tree Execution Flow

Every tree follows the universal pattern:

```
Sequence "Main"
├── Sequence "PreGate"           ← Validate inputs, setup tools
│   ├── Condition "ValidateInput"
│   └── Action "SetupTools"
├── Selector "StrategyRouter"    ← Try strategies in order
│   ├── Sequence "PrimaryPath"   ← Best approach
│   │   └── ChainAction "llm_call:..."
│   └── Sequence "FallbackPath"  ← Degraded approach
│       └── ChainAction "llm_call:..."
├── Action "ReflectOnOutcome"    ← Quality scoring
└── Selector "OutcomeSelector"   ← Self-correct or accept
    ├── Condition "WasSuccessful"
    └── ChainAction "llm_call:Self-correct..."
```

## Evolution Pipeline

```
evaluate → order_mutations → apply_top → re-evaluate → accept (commit) / rollback
```

6 algorithms operate on this pipeline. Every accepted mutation is a git commit. Trees are versioned and reversible.

## Documentation

- [arc42 Architecture (12 sections)](docs/arc42/go-bt-evolve-arc42.md)
- [Architecture Decision Records](docs/adr/INDEX.md) (7 ADRs)
- [Getting Started](docs/GETTING_STARTED.md)
- [API Reference](docs/API_REFERENCE.md)
- [Tutorial](docs/TUTORIAL.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)

## Reliability

| Layer | Mechanism |
|---|---|
| Panic recovery | SafeGo on all goroutines, tree-level defer/recover |
| Transient failures | RetryWithBackoff (full jitter, 1s→2s→4s→8s) |
| Cascading failures | CircuitBreaker (closed/open/half-open, per-agent) |
| Exhausted retries | DeadLetterQueue (persistent JSON) |
| LLM health | HealthMonitor with degraded-mode responses |
| Output quality | validateOutputQuality() — min length, error patterns, structure scoring |

## License

MIT — see [LICENSE](LICENSE)

---

Built with Go, go-bt, langchaingo, and Ollama. Deployed on Jetson ARM64. Self-improving via Stockfish-adapted evolution.
