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
│   ├── 71+ test files across all packages
│   ├── 24+ passing packages (go test ./...)
│   ├── 78% average coverage (aspirational target: 85%)
│   ├── Test Watchdog cron: Detects new failures within 4h
│   └── Benchmark suite: BFCL, BTPG, ToolBench, SWE-bench integrations
├── #operable
│   ├── slog structured logging: JSON format, levels (debug/info/warn/error)
│   ├── Prometheus metrics: Counters, gauges, histograms on /metrics
│   ├── Health endpoint: LLM availability check via bt_health
│   ├── Trace reader: OpenTelemetry spans with console tracer
│   └── Dashboard: 8-tab web UI on :9800
├── #flexible
│   ├── 41 trees across 7 categories: domain, finance, research, startup, thinktank, evolution, core
│   ├── 21-path merged main tree
│   ├── 10 chain types: llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action
│   └── YAML-defined agents: Easy creation, templating, import/export
└── #personalized (roadmap — ADR-010)
    ├── Persona layer: per-user profile, interaction log, habit mining
    ├── Goal factory: intent/pattern → grounded goap.Goal, persistent GoalQueue
    ├── Tree factory v2: plan→BT compiler, real structural crossover
    ├── Executable-by-construction: dynamic resolver + KG registration for every generated tree
    ├── HITL automation proposals: approval before scheduling auto-created agents
    └── Feedback-as-fitness: user_satisfaction dimension, per-user gardener + experience bank
```

## 10.2 Quality Scenarios

| # | Scenario | Stimulus | Response | Measure |
|---|---|---|---|---|
| QS1 | Agent process crash | Goroutine panic in ChainAction | SafeGo recovers in <1s, DLQ persists task, circuit breaker opens for agent | Recovery <1s, no process restart needed |
| QS2 | 100 consecutive evolutions | bt-gardener runs evolution cycle | No fitness drop >20% from baseline | Fitness delta tracked per-mutation (aspirational) |
| QS3 | Dashboard tree listing | GET /api/tree with 41 trees | Returns all trees with metadata | Response <500ms |
| QS4 | Test regression detection | New test failure introduced | Test Watchdog cron detects within 4h | Detection latency <4h |
| QS5 | Concurrent MCP calls | 3 simultaneous bt_run_task | bt-agent handles all 3 without deadlock | All 3 complete within timeout |
| QS6 | Ollama outage | LLM health check fails | All LLM-dependent tools return degraded error, non-LLM tools continue | Graceful degradation, no crashes |
| QS7 | Disk full during persistence | writeFile fails with ENOSPC | Error logged, operation returns failure, no corruption (atomic write aborted) | No partial/corrupt files |
| QS8 | Config validation | Invalid config.yaml on startup | Load fails with clear error message, defaults used as fallback | Config validation error reported |
| QS9 | Generated tree executability (roadmap) | `bt_kg_auto_create` / plan→BT compile produces a tree | Tree is resolvable via `ResolveTreeID`, validates, and executes (not `DefaultTree` fallback) | ≥90% of auto-created trees run end-to-end |
| QS10 | Habit detection (roadmap) | User issues a similar task for the 3rd time in 14 days | HabitMiner emits RecurringPattern; automation proposal appears in HITL queue next session | Proposal latency ≤1 session |
| QS11 | Plan compilation quality (roadmap) | Goal Factory goal → A* plan → CompilePlanToTree | Compiled tree passes ValidateTreeFull + benchmark.QuickValidate on first compile | ≥80% first-compile pass rate |
| QS12 | Personal tree evolution safety (roadmap) | 10 gardener cycles on a personal tree with user feedback | `user_satisfaction` fitness non-decreasing; regressions roll back from snapshots | Quality gate: ≤20% regression, floor 30 |
| QS13 | Automation spam guard (roadmap) | Agent detects many candidate patterns | Only patterns ≥3 occurrences proposed; per-user cap on active auto-created agents; HITL default-on | 0 unapproved scheduled automations |

---

*Generated by bt-agent arc42 pipeline — section10Quality tree*
