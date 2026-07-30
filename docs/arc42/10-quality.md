# 10. Quality Requirements

Refines the top-level quality goals Q1–Q5 in
[§1.2](01-introduction-goals.md); how each goal is achieved is in
[§4](04-solution-strategy.md).

## 10.1 Quality Tree

```
go-bt-evolve
├── #reliable
│   ├── Panic recovery: SafeGo on all goroutines, tree-level defer/recover
│   ├── Circuit breaker: 3-state (closed/open/half-open), per-agent, configurable threshold
│   ├── Retry with backoff: Full jitter, 3 classes (standard, LLM, unknown), max 3 retries
│   └── Dead letter queue: Persistent JSON, exhausted retries preserved
├── #evolvable
│   ├── Six evolution algorithms (§1.1): Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert
│   ├── Git-versioned trees: Every accepted mutation is a commit
│   ├── Benchmark gating: Pre/post fitness comparison with rollback
│   └── Mutation operators: add_before, add_after, wrap_retry, prune, swap_children, … (§12 Glossary)
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
├── #flexible
│   ├── Tree catalog across 8+ categories (inventory in §5.1)
│   ├── 21-path merged main tree
│   ├── Declarative chain types (inventory in §5.5)
│   └── YAML-defined agents: Easy creation, templating, import/export
├── #personalized (→ ADR-133)
│   ├── Persona layer: per-user profile, interaction log, habit mining
│   ├── Goal factory: intent/pattern → grounded goap.Goal, persistent GoalQueue
│   ├── Tree factory v2: plan→BT compiler, real structural crossover
│   ├── Executable-by-construction: dynamic resolver + KG registration for every generated tree
│   ├── HITL automation proposals: approval before scheduling auto-created agents
│   └── Feedback-as-fitness: user_satisfaction dimension, per-user gardener + experience bank
└── #reusable-consistent (→ Q5)
    ├── One owner per concept: canonical outcome classifier, single retry/backoff, single persistence path
    ├── Framework-first features: new capabilities become engine actions/composed blocks (KG-registered), not tree-local logic
    ├── Tree catalog hygiene: knowledge-graph similarity flags semantic duplicates for merge or explicit distinction
    ├── Action registry hygiene: self-extension grafts get promoted into the canonical registry or pruned
    ├── Convention uniformity: project-conventions rules enforced on every autonomous merge (go-conventions-reviewer)
    └── Graphify-anchored planning: GOAP runners consult the graphify knowledge graph before proposing work, so proposals reuse existing components instead of duplicating them
```

## 10.2 Quality Scenarios

| # | Scenario | Stimulus | Response | Measure |
|---|---|---|---|---|
| QS1 | Agent process crash | Goroutine panic in ChainAction | SafeGo recovers in <1s, DLQ persists task, circuit breaker opens for agent | Recovery <1s, no process restart needed |
| QS2 | 100 consecutive evolutions | bt-gardener runs evolution cycle | No fitness drop >20% from baseline. `hashTree` fingerprints the full subtree (Children/Edges/Metadata, not just the root), so `Population.Diversity()`'s `diversity_collapse` signal can't be corrupted by root-only hash collisions (→ ADR-234) | Fitness delta tracked per-mutation (aspirational) |
| QS3 | Dashboard tree listing | GET /api/tree over the full tree catalog ([§5.1](05-building-blocks.md)) | Returns all trees with metadata | Response <500ms |
| QS4 | Test regression detection | New test failure introduced | Test Watchdog cron detects within 4h | Detection latency <4h |
| QS5 | Concurrent MCP calls | 3 simultaneous bt_run_task | bt-agent handles all 3 without deadlock | All 3 complete within timeout. **Since 2026-07-16 (ADR-123, milestone 1/5):** each response's task/result also stays correctly attributed to its own caller — `bbMu` serializes `bt_run_task`'s Task-assign → RunTask → response-read critical section on the shared `deps.bb`, pinned by `TestBTRunTaskConcurrentCallsDoNotRaceOnSharedBlackboard` under `-race`. **Since 2026-07-16 (ADR-123, milestone 4/5):** `bt_blocks_compose(save:true)`, `bt_hitl_compose_task(save:true)`, and `injectPersonaContext` also serialize on `bbMu`. Other `deps.bb`-touching tools (`bt_delegate_to_tree`, `bt_use_*_tree`) remain unguarded. **Since 2026-07-16 (ADR-123, milestone 5/5):** `internal/engine`'s `Server` itself gained an analogous `bbMu`/`RegisterBlackboardTool` registration-time locking primitive, proven race-free under `-race` by `TestServer_Run_MixedToolConcurrentCallsDoNotRaceOnSharedBlackboard`. **Since 2026-07-16 (ADR-124):** `cmd/bt-agent` migrated `bt_run_task`, `bt_use_*_tree`, and `bt_delegate_to_tree` (plus `bt_blocks_compose`/`bt_hitl_compose_task`) onto that primitive and removed `mcpDeps.bbMu`/`lockBB`/`unlockBB` entirely, so all previously-unguarded tools are now covered. |
| QS6 | Ollama outage | LLM health check fails | All LLM-dependent tools return degraded error, non-LLM tools continue | Graceful degradation, no crashes |
| QS7 | Disk full during persistence | writeFile fails with ENOSPC | Error logged, operation returns failure, no corruption (atomic write aborted) | No partial/corrupt files |
| QS8 | Config validation | Invalid config.yaml on startup | Load fails with clear error message, defaults used as fallback | Config validation error reported |
| QS9 | Generated tree executability (personalization — ADR-133) | `bt_kg_auto_create` / plan→BT compile produces a tree | Tree is resolvable via `ResolveTreeID`, validates, and executes (not `DefaultTree` fallback) | ≥90% of auto-created trees run end-to-end |
| QS10 | Habit detection (personalization — ADR-133) | User issues a similar task for the 3rd time in 14 days | HabitMiner emits RecurringPattern; automation proposal appears in HITL queue next session | Proposal latency ≤1 session |
| QS11 | Plan compilation quality (personalization — ADR-133) | Goal Factory goal → A* plan → CompilePlanToTree | Compiled tree passes ValidateTreeFull + benchmark.QuickValidate on first compile | ≥80% first-compile pass rate |
| QS12 | Personal tree evolution safety (personalization — ADR-133) | 10 gardener cycles on a personal tree with user feedback | `user_satisfaction` fitness non-decreasing; regressions roll back from snapshots | Quality gate: ≤20% regression, floor 30 |
| QS13 | Automation spam guard (personalization — ADR-133) | Agent detects many candidate patterns | Only patterns ≥3 occurrences proposed; per-user cap on active auto-created agents; HITL default-on | 0 unapproved scheduled automations |
| QS14 | Duplicated functionality (reuse — Q5) | A concern gains a second implementation (e.g. a daemon re-implements outcome classification) | Fleet review or lint flags it; a consolidation program is seeded within one review cycle | Zero concepts with more than one owner package; no "same bug fixed twice" recurrences |
| QS15 | Tree-specific one-off (consistency — Q5) | A capability needed by ≥2 trees is proposed inline in one tree | Proposal is reframed as an engine action/composed block and registered in the knowledge graph | KG capability query returns exactly one canonical provider |
| QS16 | Duplicate tree (reuse — Q5) | Factory/breeding/auto-create produces a tree semantically matching an existing catalog tree | Creation blocked or a merge proposal raised via KG similarity check | No catalog pairs above the similarity threshold without a documented distinction |
| QS17 | Code clones (reuse — Q5) | New Go code introduces a clone of existing code | Pre-commit/verify gate fails; the clone is consolidated before landing | No new clone groups above the lint threshold land |
| QS18 | Concurrent dashboard requests share company state | Two `*Workflow` instances (one per HTTP request) or a `*Workflow` and a `*CompanyOrchestrator` mutate the same shared `*CompanyState` pointer concurrently | `CompanyState`'s own mutex — not each wrapper's private `w.mu` — serializes every field read/write; `ExecuteSprint` releases `w.mu` before calling `orch.RunSprint()` to avoid a non-reentrant deadlock (→ ADR-236) | `go test -race` clean via `TestExecuteSprint_ConcurrentWorkflowsShareCompanyState` |
| QS19 | Gardener records an evolved-tree run | `evolveTreeV2` → `recordEvolvedRun` calls `KnowledgeGraph.RecordRun` with `Outcome: "evolved"` | `RecordRun` marks the feedback graph dirty for every caller by construction (genuine or evolved), so a later `FlushFeedback`/restart persists `EvolvedCount`/`StructuralFitness` instead of silently dropping it (→ ADR-235, §8.4) | Pinned by `TestRecordRun_MarksFeedbackDirty`, `TestRecordRun_Evolved_MarksFeedbackDirty` |
| QS20 | Malformed dashboard HITL request | POST to `/api/hitl/{id}/approve\|reject\|escalate` with an unparseable JSON body, or `encodeJSON` asked to marshal a non-encodable value | Malformed body returns 400 instead of being silently ignored and applied as an empty/zero-value body; on an encode failure, `encodeJSON` buffers first so it reports 500 instead of writing a success status then a superfluous second `WriteHeader` | Pinned by `TestHandleHITL_MalformedBody_ReturnsBadRequest`, `TestEncodeJSON_EncodeFailure_DoesNotDoubleWriteHeader` |

---

*Generated by bt-agent arc42 pipeline — section10Quality tree*
