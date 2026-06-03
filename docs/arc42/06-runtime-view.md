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

## 6.3 Sprint Execution

**Trigger:** Dashboard user POSTs to `/api/sprint` with company/quarter info.

```
Browser                         bt-dashboard (:9800)           Goroutine                   bt-agent (MCP)
  │                               │                            │                            │
  │──POST /api/sprint────────────▶│                            │                            │
  │                               │──orch.RunSprint()─────────▶│                            │
  │                               │                            │──Create tasks (5 roles)──▶│
  │                               │                            │──for each task:           │
  │                               │                            │   hermes chat -q          │
  │                               │                            │   "delegate to {tree}"───▶│──bt_run_task()──▶
  │                               │                            │                            │◀──result────────
  │                               │                            │──mark task done           │
  │◀──{sprint_id}─────────────────│                            │                            │
  │                               │                            │                            │
  │──GET /api/sprint/status──────▶│                            │                            │
  │◀──{progress, tasks}───────────│                            │                            │
```

**Duration:** 5-15 minutes (5+ Ollama calls per sprint). Poll-based status via `/api/sprint/status`.

## 6.4 Error Recovery

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
