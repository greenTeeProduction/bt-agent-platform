# 6. Runtime View

Architecturally significant runtime scenarios. Participants are the building
blocks of [§5](05-building-blocks.md); the infrastructure they run on is in
[§7](07-deployment.md); the system-wide concepts these scenarios instantiate
are in [§8](08-crosscutting-concepts.md).

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
    │                               │                            │   │  ├─MarkSuccessful (quality-gated)│
    │                               │                            │   │  ├─SelfCorrect (retry x3)│
    │                               │                            │   │  └─EscalateToDeepSeek│
    │                               │                            │   └─bb.Outcome=success  │
    │                               │                            │──validateOutputQuality─▶│
    │                               │◀────result, outcome────────│                         │
    │◀────ToolResult───────────────│                            │                         │
```

**Duration:** Typical: 2-4 minutes (Ollama qwen3.6:35b). Fast path: 5-10 seconds (DeepSeek v4-flash). Timeout: 120s hard limit.

**Error Path:** ChainAction panic → SafeGo recover → RecordFailure → CircuitBreaker check → RetryWithBackoff (1s/2s/4s) → DeadLetterQueue.

**Terminal backstop (ADR-085):** Regardless of whether a tree routes through `OutcomeSelector` at all, `RunTask`'s final `validateOutputQuality` call now flips `bb.Outcome` to failure when the resolved result is low-quality, non-empty, and not a recognized structured/zero-LLM result or a `bb.Sandbox` run — covering trees (e.g. compiled GOAP fusion trees) whose terminal leaf never reaches `MarkSuccessful`/`SelfCorrect`/`EscalateToDeepSeek`.

## 6.2 Evolution Cycle

**Trigger:** bt-gardener cron (or manual `bt_evolve` MCP call).

```
bt-gardener                    bt-evaluator (MCP)            Evolution Engine              git
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

**Key:** 97.3% of mutations currently regress (no quality gates enforced — see [§11](11-risks-debt.md)). Per-tree fitness via `reflection.FilterByTreeName` + seed records.

**Experience integration (ADR-021):** In the v2 cycle (`RunCycleV2`), the ranked-mutation step is experience-biased — `biasCandidatesWithExperience` boosts `OrderMutations` candidates whose op/target matches high-quality past `ExperienceBank` entries — and every ACCEPT additionally records the mutation into the shared bank via `AddFromMutation` with its per-candidate fitness delta. A nil bank leaves both steps at the historical behavior. Since 2026-07-16 (ADR-125), both sides condition on a new `lastFailureTask(records)` helper — the `Task` text of the tree's most recent `evolution.Failure` reflection record: when non-empty, recorded entries' `Context` is tagged `failing_task=<text>` and retrieval routes through the tree-type-agnostic `bank.Retrieve(query, ...)` instead of the tree-type-only `RetrieveExperienceHints`; when empty (no failing record yet), both sides fall back to the tree-type-scoped behavior described above verbatim.

**Two structural-mutation generators, one competition (2026-08-01):** the `ev_order_mutations()` step above is no longer the cycle's only source of candidates. After `biasCandidatesWithExperience` and before the per-candidate benchmark/gate loop, `evolveTreeV2` calls `augmentWithMCTSCandidates`, which (1) asks `evolution.SelectStructuralStrategy` whether *this* tree earns a speculative search — it averages the specialist registry's archetype affinity and the Selector optimizer's learned-ordering affinity (both read via the now-shared `seedSelectorOptimizer`) and augments at ≥ 0.5, so a preserved archetype whose Selectors are fully telemetered keeps the heuristic-only ordering; (2) on `StrategyMCTSAugmented`, runs `evolution.MCTSMutator.Candidates` for `MCTSIterations` iterations (default 12), each costing one `evaluator.EvaluateTree` call against the tree's own reflection records — no benchmark, no LLM; and (3) folds the search's root-level finds into `OrderMutations`' list with `evolution.MergeScoredMutations`, collapsing a duplicate op/target to the higher-scoring side. The merged list is one descending-score competition: MCTS output lands in the `(0.5, 1.0]` band, above the loop's own 0.45 cutoff but damped when the best observed gain was marginal, and every merged candidate — whichever generator proposed it — still clears the same benchmark, pre-score, quality gate and meta-validation before it is applied. On by default via `DefaultEvolveV2Config` and therefore in the production daemon; `MCTSStructuralSearch=false` restores the single-generator flow ([§5.1](05-building-blocks.md), [§5.3](05-building-blocks.md), [§8](08-crosscutting-concepts.md) Evolution Pipeline).

**Durable pre-mutation snapshot and rollback (2026-07-15, ADR-093):** Before the mutation loop above begins, `evolveTreeV2` also calls `evolution.SnapshotTree(tree, entry.Name, g.cfg.SnapshotDir)`, writing the pre-cycle tree into `~/.go-bt-gardener/snapshots/` — durable state a process crash mid-cycle can't take with it, unlike the in-memory `originalTree` clone the ACCEPT/ROLLBACK comparison above already used. ~~The snapshot slot is overwritten on every cycle, so it always reflects the start of the *most recent* cycle, not full history.~~ — resolved 2026-07-15 (ADR-115): each call now writes a new sequentially-numbered `snapshot_<name>_<seq>.json` revision plus an index file instead of clobbering one slot, so history accumulates and `ListRevisions`/`RestoreTreeRevision` can recover any prior cycle's state. `Registry.RollbackTree(name, snapshotDir)` restores the newest revision and durably re-persists it, reachable on demand via the `gardener_rollback` langchain tool.

**Automatic rollback on fail-closed (2026-07-15, ADR-115 milestone 2):** The quality-gate-disabled check `evolveTreeV2` makes at loop entry ([§8](08-crosscutting-concepts.md) Evolution Pipeline) now runs *before* the pre-mutation snapshot above rather than after — snapshotting the tree's current, already-regressed state first would make it the new "most recent" revision and defeat the rollback. When `g.cfg.Gate.IsDisabledFor(entry.Name)` is true, the cycle no longer just skips mutations and returns: it calls `g.cfg.Registry.RollbackTree(entry.Name, g.cfg.SnapshotDir)` to restore the tree's last-known-good revision immediately, records the outcome on `CycleMetrics.Rollbacks` (1 on success, 0 on a failed or unconfigured rollback), and returns without entering the mutation loop at all — the tree is actively repaired the same cycle the gate trips, not left frozen in its regressed state until a process restart or an operator-triggered `gardener_rollback` call.

**Gardener-embedded transposition table (2026-07-15, ADR-094):** `evolveTreeV2` now also calls `evaluator.TranspositionTable.Store` right after computing `baseFitness` — before the evidence gate can short-circuit the rest of the pipeline — so every processed tree contributes a cached `(tree, task)` evaluation, and `IterativeDeepening` probes ahead from the post-cycle tree once the mutation loop finishes. Since 2026-07-15 (ADR-107), when that probe's `BestMutation` beats the cycle's current fitness, `evolveTreeV2` applies it directly to the live tree instead of only recording it in `CycleMetrics` — the deep search can now improve a tree even when the greedy per-candidate loop's mutation budget found nothing better. Since 2026-07-15 (ADR-112), that apply is itself re-gated: `evolveTreeV2` re-runs `ValidationGate` against the mutated tree before this second `Registry.SaveTree` call, exactly mirroring the greedy loop's own gate above, and reverts the tree to its pre-deep-search state on rejection — closing the gap ADR-107 had flagged where the deep-search path bypassed the cycle's own quality-threshold check. `RunCycleV2` saves the table to disk right after `MetricsTracker.Save()`, on the same per-tree cadence. Both steps are no-ops when `Config.TranspositionTablePath` is unset ([§8](08-crosscutting-concepts.md) Evolution Pipeline).

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

**`RunSprint` no longer blocks concurrent state reads for its full duration (2026-07-30):** `CompanyOrchestrator.RunSprint` (the `orch.RunSprint()` step above) runs its `EngineerTree`/`MarketingTree`/`SalesTree` calls — each with a 120s timeout, up to ~6 minutes worst case per sprint — with `CompanyState`'s lock (ADR-236, [§8](08-crosscutting-concepts.md)) held only for two short snapshot/apply windows immediately before and after them, not across them. Previously the lock was held for the whole method body, so any concurrent `state.Lock()` caller — `handleDefaultCompany`'s `GET /api/company/default` (fetched by the dashboard on every page load) or `Summary()` — blocked for the full sprint duration whenever a sprint was running synchronously in a request goroutine (e.g. via `handleWorkflowRunFullPipeline` → `RunFullPipeline` → `ExecuteSprint` → `RunSprint`). `RunSprint` now mirrors the unlock-around-`RunSprint()` pattern `ExecuteSprint` already used for the same reason.

**Dispatch order (ADR-072):** the `/api/sprint/execute` handler (`handleSprintExecute`) dispatches whatever `TaskStore.Approved()` returns, in order. Since 2026-07-13, `Approved()` sorts its result by priority (critical → high → medium → low → backlog, the same ordinal `workflow_engine.go` declares as `WorkflowPriority`) and then by sprint number, so a critical-priority task approved after a low-priority one still dispatches first — previously the loop ran approved tasks in whatever order they appeared in the store.

**Tree selection (ADR-073, ADR-100):** when a task has no `TreeID` set, the sprint loop and `handleAnalyze`/manual task creation all call `dashboard.PickTreeForTask` to pick `{tree}`. Since 2026-07-13, auction/delegation-shaped task text (mirroring the exported `engine.AuctionTaskKeywords`) routes to `auction_demo` ahead of the other keyword picks, so a live sprint task can reach the A2A announce→bid→award auction machinery ([§8](08-crosscutting-concepts.md) A2A Auction Task Allocation) through this path instead of only via an explicit `switch_tree`. Since 2026-07-15, a task that clears the auction check is next routed through the knowledge graph: `main.go` wires `dashboard.DiscoverTreeFn = kg.Discover`, and `PickTreeForTask` returns the graph's confident answer ahead of its remaining static bug/build/security/research/test/refactor keyword picks — the static switch now only decides tasks the knowledge graph itself has no confident match for.

**Task derivation (ADR-080):** `handleAnalyze` now runs the thinktank orchestrator's full research→debate→synthesis sequence (previously it ran only the research round) and checks each phase's error, returning `{"error": ...}` on the first failure instead of silently falling through. On success it derives dashboard tasks via `internal/dashboard/workflow_engine.go`'s `Workflow.RecommendationsToTasks`/`Prioritize` — the engine's first production caller — instead of minting `dashboard.Task` values directly from raw research-finding insight text, so tasks entering the sprint-execution path above carry real `AssigneeRole`/`SprintTarget`/`Approval` state.

**Workflow-level approval gate (ADR-081):** `handleAnalyze` also retains the `*dashboard.Workflow` it builds in a package-level `currentWorkflow` var, and three new endpoints — `GET /api/workflow/pending`, `POST /api/workflow/approve?id=`, `POST /api/workflow/reject?id=&reason=` — call `Workflow.PendingApprovals`/`ApproveTask`/`RejectTask` on it directly, alongside the pre-existing `/api/tasks/approve`/`/api/tasks/reject`. This is a second, independent approval surface: it mutates `currentWorkflow.Tasks[i]` (keyed on the bare `WorkflowTask.ID`, e.g. `rec-001`), not the `dashboard.Task` records in `taskStore` (keyed on `wf.ID + "-" + wt.ID`) that `handleSprintExecute`'s dispatch loop above actually reads via `TaskStore.Approved()`. Deciding a task through `/api/workflow/approve` does not approve its `taskStore` counterpart, and vice versa — the dispatch order described above is still governed exclusively by the `/api/tasks/approve`/`/api/tasks/reject` (ADR-072) path.

**Approval-surface reconciliation (ADR-086):** `handleWorkflowApprove`/`handleWorkflowReject` now also call `taskStore.Approve`/`taskStore.Reject` on the composed `wf.ID+"-"+taskID` record immediately after the `Workflow`-side call succeeds, so a decision made through `/api/workflow/approve`/`/api/workflow/reject` reaches the same `taskStore` record `handleSprintExecute` reads via `TaskStore.Approved()` above. The reverse direction (`/api/tasks/approve`/`/api/tasks/reject` updating `currentWorkflow.Tasks`) is unchanged, since that was never the direction gating sprint dispatch.

**Dispatch-time task-state sync (2026-07-14, ADR-089):** ADR-086 reconciled the *approval* decision in one direction (`/api/workflow/approve|reject` → `taskStore`); it left the *execution* outcome unreconciled in the other. `handleSprintExecute`'s per-task dispatch loop above updates `taskStore` (`UpdateStatus`/`SetOutput`) as each task starts, fails, times out, or completes, but never touched `currentWorkflow` at all — so once a sprint actually ran, every dashboard surface reading `currentWorkflow` (`GET /api/workflow/pending`, the sprint-goal UI, `Company.CurrentSprint`) stayed frozen at `"approved"` even after the task had finished. A new `syncWorkflowTaskStatus(taskID, status)` helper checks the dispatched `taskID` against `currentWorkflow.ID+"-"` (the same composed-ID convention `handleAnalyze`/`handleWorkflowApprove`/`handleWorkflowReject` already use) and, on a match, calls the new `Workflow.SetTaskStatus` ([§5.4](05-building-blocks.md)) with `StatusInProgress`/`StatusBlocked`/`StatusCompleted` at the same three points the loop already updates `taskStore` — advancing `Company.CurrentSprint` to the task's `SprintTarget` on completion, mirroring `ExecuteSprint`'s own convention. `currentWorkflow` and `taskStore` now converge from execution as well as from approval.

**`RunFullPipeline` gets a caller (2026-07-15, ADR-116):** the paragraphs above all describe `handleAnalyze`, which stops after `RunSynthesis` and never calls `Workflow.ExecuteSprint`. A new `POST /api/workflow/run-full-pipeline?topic=` handler drives the fuller `Workflow.RunFullPipeline` instead — research→debate→synthesis→peer-review→report-generation, then `RecommendationsToTasks`/`Prioritize`, then one `ExecuteSprint` call per distinct `SprintTarget` found in the derived tasks — persisting the result into `currentWorkflow` and `taskStore` the same way `handleAnalyze` does. `RunFullPipeline` now also halts with `w.Status = "failed"` before any task is created if any thinktank phase errors, and iterates every sprint actually present instead of a hardcoded sprint 1/2 pair ([§5.4](05-building-blocks.md)). Because `RunFullPipeline` still never auto-approves (ADR-081) and each call builds a brand-new `Workflow`, every task it derives starts `StatusPending`; its `ExecuteSprint` calls still run `compOrch.RunSprint()` (the company-state simulation) but advance no task status on that first pass — real per-task bt-agent dispatch for these tasks still depends on an explicit `/api/workflow/approve` or `/api/tasks/approve` decision and `handleSprintExecute` picking them up from `taskStore`, exactly as for tasks derived via `handleAnalyze`.

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

**Apply status is authoritative across trailing invocations (2026-07-23):** the plan-resume runtime (`runSuperpowersRuntimeFromExistingPlanAction`) can run more than once within the same cycle — a first invocation that lands a partial landing above calls `clearSuperpowersPlanState` on success, and a later, trailing invocation in the same cycle then hits the top-of-function "no plan path" guard with nothing left to resume. That guard now checks `goapFusionApplyAlreadyLanded` (the tracked run's `ApplyStatus` is `committed`/`committed_pr_opened`) before treating an empty plan path as failure: when true, the trailing invocation returns success and leaves `bb.Result` as the landing report the earlier invocation already wrote, instead of unconditionally calling `markGoapFusionImplDegraded` and the goals-unchanged fast path, which previously overwrote the evidence with a "no code landed" message (run `20260723T091452` landed `3d6a13b` yet the cycle logged `no_change`).

**Rate-limit backoff:** a Claude rate-limited outcome in any tick persists a backoff deadline (`goap_fusion_claude_backoff_until`, RFC3339 in the agent-scope blackboard with ChainState fallback — the same durable pattern as the saved plan) so the *next* ticks consume it instead of re-attempting against a quota known to be closed. Both Claude consumers honor it on entry: the plan-resume runtime (`runSuperpowersRuntimeFromExistingPlanAction`) degrades to ScheduledAnalysisPath instantly — before worktree creation and the 90-minute run-budget attempt (`superpowersRuntimeRunBudget`; 45 minutes until 2026-07-19, which SIGKILLed 3-milestone batches mid-implementation) — with the exact rate-limited Result/Outcome shape, so the plan carryover is preserved for the tick after the window expires; and the Claude review research fallback returns rate-limited in milliseconds without invoking Claude, letting the ResearchRouter fall through to its non-fatal skip. The window is env-configurable (`BT_GOAP_CLAUDE_BACKOFF`, default 6h) on the implementation path and a fixed 1h on the review path; an elapsed window self-clears (half-open, the ADR-010 lesson) and malformed state reads as inactive, so stale or corrupt backoff can never permanently block Claude attempts.

**Research economy:** NotebookLM answers are cached per Pacific day by question hash; daily budgets (30 queries / 2 web-research starts) refuse further metered calls with an error the ResearchRouter routes to the Claude review fallback (commits → structure → failures mode rotation, persisted round counter). The router is itself non-fatal: in both fusion trees (`domain:goap_fusion` and `domain:goap_fusion_loop`) it ends in a terminal `AlwaysSucceed` "ResearchOptional" leaf, so a doubly-unavailable research stage — NotebookLM quota closed *and* the Claude review fallback rate-limited or barren — degrades to the vault-context read phase (ReadVaultResearch onward) instead of aborting the run.

**Charge-stamp durability (2026-07-16, ADR-129):** `PrioritizeGoapGoals`'s four per-cycle charge stamps (`program_milestone_charged`, `program_milestone`, `research_goal_charged`, `research_goal_charged_text`) now write through a new `setGoapStateDurable` to both ChainState and the agent-scope blackboard — the same durable pattern the Claude backoff deadline above uses — instead of ChainState alone. `clearSuperpowersPlanState`, already called from every plan-retirement site (`actions_superpowers_prod.go:804,917,1066`), now also wipes all four durable keys alongside the two plan-resume keys it already cleared, so a completed or abandoned cycle can no longer leave a stale stamp for a later, unrelated cycle's failure handler to charge or refund against. The failure/refund readers (`chargeGoapResearchGoalFailure`, the refund path's `goapChargedMilestoneRef`) still read only `bb.ChainState`, so a tick that resumes straight into `Implement` off a saved plan — skipping `PrioritizeGoapGoals` — still cannot see the durable stamp on failure; read-back is not yet wired (open caveat, ADR-129).

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

**Cross-process replay pickup (2026-07-10, ADR-036):** a `Requeue` stamped by the dashboard or an MCP sibling lands only in the shared `dead_letter_queue.json`. The daemon's replay scan (every `dlqReplayScanInterval`, 5 min) therefore runs `Reload()` → `RequeuedReady()` → `Replay(id)` each tick: the reload adopts the sibling's on-disk stamp into the executor's in-memory view, and the executor — the one process with a tree runner — replays the entry. The queue's own saves re-merge on-disk state under a sidecar flock first, so no process's periodic save can clobber a stamp written between ticks ([§8](08-crosscutting-concepts.md) File-Based Persistence).

---

*Generated by bt-agent arc42 pipeline — section6RuntimeView tree*
