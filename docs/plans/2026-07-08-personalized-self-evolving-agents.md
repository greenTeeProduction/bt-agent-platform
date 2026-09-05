# Personalized Self-Evolving Agents — Strategy and Implementation Plan

> **Status:** Strategy (approved scope: review + roadmap). Implementation phases are
> sized to land incrementally behind `make check-quick` gates.

**Vision:** The platform grows from a *pre-authored tree catalog with evolution* into a
system where a **personalized agent** observes the user, derives **goals** from the
collaboration, **plans** with GOAP, **compiles plans into persistent behavior trees**,
runs them, and **evolves them from user feedback** — closing the loop
`observe → goal → plan → tree → run → reflect → evolve`.

---

## Part 1 — Review: Current State vs arc42 Goals

### What arc42 promises and what actually exists

| arc42 claim | Reality (verified 2026-07-08) | Gap |
|---|---|---|
| Q2 Evolvability: 6 algorithms drive improvement | GA, Q-Learning, MAP-Elites, NSGA-II, Island, CMA-ES, memetic all exist; gardener v2 is the production path with benchmark + quality + validation gates | Delivered — but evolution only *mutates existing* trees; it never *creates* trees from user intent |
| §1.1 "Factory — tree breeding via crossover from parent templates" | `knowledge.Factory.Breed` exists (MCP `bt_factory_create`) but crossover uses **category-generic templates**, not the actual parent `SerializableNode` structures (`internal/knowledge/factory.go` `extractTemplates` stores metadata only) | Crossover is shallow; parents contribute no structural DNA |
| §1.1 "Knowledge Graph … discovery and auto-creation" | `bt_kg_auto_create` breeds and registers a KG entry — but `domains.ResolveTreeID` cannot resolve the new ID, so the agent runner falls back to `DefaultTree()` | **Creation ↔ execution disconnect**: auto-created trees are never actually run |
| §4 "GOAP planning — PlannerNode extends UtilitySelector" | Two parallel GOAP implementations: (a) `internal/goap` A* planner wired via 7 engine actions (`PlanGoapActions`, `ExecuteGoapStep`, …), (b) a separate `engine.PlannerNode` with its own A* that does **not** import `internal/goap` and appears only in tests | Duplication (echoes risk R8); plans execute dynamically and are **never compiled into persistent BTs** |
| ADR-004 Agent Platform + memory store | Per-**agent** `MemoryStore`, history JSONL, reflections — keyed by agent name | **No per-user concept** anywhere in the agent platform; the only `UserProfile` lives in the isolated DoorMate dashboard feature (`internal/doormate/models.go`) |
| R1 Mutation death spiral mitigation "aspirational" | Actually largely fixed: gardener v2 has quality gate (floor 30, ≤20% regression, 5-strike disable), benchmark pre-validation, snapshots, validation gate | arc42 §11 is stale; update it |
| §1.1 "Factory" (internal/factory) | `internal/factory` is a **SKILL.md → tree compiler** (LLM analysis → fixed PreGate/StrategyRouter/OutcomeSelector scaffold), not a breeding factory. Known routing gap: generator never emits logic that sets `ChainState["route"]` for `DecisionTree` | Naming confusion between `internal/factory` and `knowledge.Factory`; router gap |

### Idle assets that the vision needs (build on, don't rebuild)

| Asset | Location | State |
|---|---|---|
| `goap.Agent` (plan→execute→replan loop) | `internal/goap/agent.go` | Library-only, unused in production |
| `goap.GoalQueue` + `InterleaveCheck` (priority goals, preemption) | `internal/goap/multi_goal.go` | Unused |
| `goap.BlackboardBridge` (world state ↔ blackboard sync) | `internal/goap/integration.go` | Tests only |
| `Goal.Deadline` | `internal/goap/state.go` | Never read |
| Agent memory category `"preference"` | `internal/agent/memory.go` | Exists, agent-scoped |
| `HumanApprovalGate` node | engine | Registered, usable for automation approval |
| `ExperienceBank` (mutation priors) | `internal/evolution/experience_bank.go` | Global, not per-user |
| Blocks composition (`bt_blocks_compose`) | `internal/blocks` | Working runtime tree composition |

### Structural blockers for self-evolving personalized trees

1. **Resolver blindness** — `domains.ResolveTreeID` only knows compiled-in Go trees.
   Any generated tree (factory, KG auto-create, future plan-compiler output) is
   unreachable by agents/scheduler.
2. **Evidence gate freeze** — the gardener skips trees without reflections
   (`EvolveWithoutReflections=false`). Freshly generated personal trees would never
   evolve until they accumulate run history *and* that history reaches the gardener.
3. **No user identity** — reflections, memory, experience bank, feedback.json are all
   global or agent-scoped. Personal growth requires per-user partitioning.
4. **Plans are transient** — GOAP output lives in `ChainState["goap_steps"]` for one
   run. Nothing turns a *repeatedly successful plan* into a durable, evolvable tree.

---

## Part 2 — Target Architecture

```
                       ┌──────────────────────────────────────────────┐
                       │                internal/persona              │
                       │  UserProfile · InteractionLog · HabitMiner   │
                       └──────┬────────────────────────────┬──────────┘
                              │ observed patterns          │ preferences bias
                              ▼                            ▼
        ┌─────────────────────────────┐    ┌─────────────────────────────────┐
        │   Goal Factory (goap/goals) │    │   Tree Factory v2               │
        │  intent → goap.Goal         │───▶│  plan → SerializableNode BT     │
        │  GoalQueue (priorities)     │    │  real crossover · block compose │
        └─────────────┬───────────────┘    └───────────────┬─────────────────┘
                      │ goap.Planner (A*)                   │ validate + bench
                      ▼                                     ▼
        ┌──────────────────────────────────────────────────────────────────┐
        │  Per-user workspace  ~/.go-bt-evolve/users/<user>/               │
        │  trees/ · goals/ · memory/ · reflections/ · experience/          │
        └───────┬──────────────────────────────────────────────┬───────────┘
                │ DynamicResolver (new)                        │ reflections
                ▼                                              ▼
        ┌───────────────────┐                     ┌────────────────────────┐
        │  Engine / Runner  │────────────────────▶│  Gardener (per-user    │
        │  (existing)       │   run records       │  registry, user-       │
        └───────────────────┘                     │  feedback fitness)     │
                                                  └────────────────────────┘
```

Design principles:

- **Reuse over rebuild** — activate `goap.Agent`, `GoalQueue`, `BlackboardBridge`,
  `HumanApprovalGate`, `ExperienceBank`; do not introduce parallel implementations.
- **Everything generated must be executable** — no tree is created without being
  resolvable, validated (`ValidateTreeFull`), and benchmark-smoke-tested
  (`benchmark.QuickValidate`).
- **ADR-003 persistence** — atomic-write JSON/YAML under `~/.go-bt-evolve/users/`,
  git-friendly, no databases.
- **HITL for new automations** — an auto-proposed automation only becomes a scheduled
  agent after `HumanApprovalGate` / `bt_hitl_approve` (reuses existing HITL plumbing).
- **Safety rails carry over** — quality gate, snapshots, rollback, circuit breakers
  apply to personal trees exactly as to catalog trees (Q3 Reliability preserved).

---

## Part 3 — Phased Roadmap

### Phase 0 — Close the creation→execution loop (prerequisite, small)

> **Status: IMPLEMENTED (2026-07-08).** `evolution.TreeFileName`/`SaveNamed`/
> `LoadNamed` (`internal/evolution/named_trees.go`), `domains.DynamicResolveFn`
> hook wired via `agentexec` (`internal/agentexec/wiring.go`), validation +
> persistence in `bt_kg_auto_create`/`bt_factory_create`
> (`cmd/bt-agent/tools.go` `persistGeneratedTree`), model-routed StrategyRouter
> in `internal/factory/generator.go`. Guarded by
> `TestDynamicTreeResolverIsProductionWired`, `TestResolveGeneratedTree_EndToEnd`,
> `TestResolveTreeID_ConsultsDynamicResolver`, and
> `TestGenerator_StrategyRouterIsModelRouted`.

**Objective:** any generated tree is runnable and gardener-visible.

- **Dynamic resolver:** extend `domains.ResolveTreeID` with a registered fallback hook
  (`domains.DynamicResolveFn`, wired in `internal/agentexec/wiring.go` per the existing
  injection-hook pattern) that loads `tree-<id>.json` from the tree store / user
  workspace via `evolution.TreeStore`, then `engine.BuildAndValidate`.
- **Auto-register generated trees in the KG:** after `bt_kg_auto_create` /
  `bt_factory_create` persist a tree, add it to `knowledge.GlobalGraph` *and* the tree
  store so discovery, resolution, and gardener registry all see it.
- **Fix `internal/factory` routing gap:** generator emits a route-setting step so
  `DecisionTree` strategy paths are actually reachable.
- **Update arc42 §1.1/§11** to reflect reality (gardener gates shipped; factory naming).

**Verification:** integration test — `bt_kg_auto_create` a tree, then `bt_run_task`
with that tree ID executes it (not `DefaultTree`), and the gardener registry lists it.

### Phase 1 — Personalization layer (`internal/persona`)

> **Status: CORE IMPLEMENTED (2026-07-08).** `internal/persona` package
> (`profile.go` — Profile/ApprovalPolicy/Store/Workspace, `interaction.go` —
> JSONL interaction Log, `habitminer.go` — HabitMiner with embedding + keyword
> fallback), `agent.UsersDir()`, MCP tools `bt_persona_get` /
> `bt_persona_set_preference` / `bt_persona_patterns`
> (`cmd/bt-agent/persona_tools.go`), and `bt_run_task` gained an optional
> `user` argument (profile context injection via
> `ChainState["persona_context"]` + interaction logging). bt-agent now exposes
> 66 tools. Remaining for this phase: DoorMate migration onto persona, and
> user-scoped construction of MemoryStore/reflections/ExperienceBank (their
> APIs already take base dirs; actual per-user instances land with Phases 2/5
> which consume them).

**Objective:** the platform knows *who* it is working with and what they do repeatedly.

- **`persona.Profile`** — generalize DoorMate's `UserProfile`: identity, preference
  tags, preferred output style, tool habits, LLM/prompt style hints, approval
  thresholds. Persisted at `~/.go-bt-evolve/users/<user>/profile.json`.
- **Per-user workspace paths** in `internal/agent/paths.go`: `users/<user>/{trees,
  goals, memory, reflections, experience}`. `MemoryStore`, `evolution.Store`
  (reflections), and `ExperienceBank` accept a base-dir so they can be user-scoped
  without forking their logic.
- **Interaction log + HabitMiner** — every `bt_run_task` (and dashboard/CLI run)
  appends `{task, tree, outcome, duration, corrections}` to the user's interaction
  log. `HabitMiner` clusters tasks by embedding similarity (reuse
  `knowledge.EmbeddingClient`; keyword fallback when Ollama is down) and emits
  `RecurringPattern{examples, frequency, suggested_goal}` once a pattern crosses a
  threshold (e.g. 3 similar tasks in 14 days).
- **MCP surface:** `bt_persona_get`, `bt_persona_set_preference`,
  `bt_persona_patterns` on `bt-agent`. Blackboard injection: profile context block
  joins `agent.InjectMemory` so ChainAction prompts see user preferences.
- **Migrate DoorMate** to read/write through `persona` (single source of truth).

### Phase 2 — Goal Factory

> **Status: CORE IMPLEMENTED (2026-07-08).** `internal/goap` gained
> `vocabulary.go` (world-state key registry with canonical-key grounding,
> `StandardVocabulary`, and `AutomationActions()` — planner operators for the
> detect→compile→HITL→schedule automation pipeline plus quality/turnaround/
> watcher paths), `goalfactory.go` (`GoalFactory.FromIntent` with LLM
> extraction + repair loop + archetype fallback, `FromPattern`,
> `ArchetypeGoal`, plannability validation via A* from a neutral initial
> state), and `goal_store.go` (atomic per-user `goals.json` persistence that
> rehydrates `GoalQueue`). `Goal.Deadline` now breaks priority ties in
> `GoalQueue` ordering (earlier deadline first, 0 = none last). MCP tools
> `bt_goal_add` / `bt_goal_list` / `bt_goal_remove` / `bt_goal_from_pattern`
> (`cmd/bt-agent/goal_tools.go`); bt-agent now exposes 70 tools. Remaining
> for this phase: scheduler-tick integration of `SelectGoal`/`InterleaveCheck`
> (lands with the Phase 5 gardener/scheduler work that consumes the queue).

**Objective:** turn observed patterns and explicit user statements into first-class
GOAP goals with a persistent per-user goal library.

- **New `internal/goap/goalfactory.go`** (stays inside goap; avoids R4 package sprawl):
  - `FromIntent(llm, userText) (goap.Goal, error)` — LLM structured-output extraction
    of `Conditions` (world-state keys), `Priority`, optional `Deadline`. Grounded in a
    **world-state vocabulary registry** (curated key catalog + the effects of
    `StandardActions()` and registered engine actions) so goals are always plannable —
    ungrounded conditions are rejected or mapped to nearest known keys.
  - `FromPattern(RecurringPattern) goap.Goal` — habit-miner patterns become
    automation goals (`task_automated=true`, `quality>=threshold`).
  - **Goal archetypes** — parameterized templates: *automate-recurring-task*,
    *improve-output-quality*, *reduce-turnaround*, *watch-and-alert*.
- **Activate `GoalQueue`** — per-user persistent queue at `users/<user>/goals/`;
  the scheduler tick calls `SelectGoal`/`InterleaveCheck` so higher-priority personal
  goals can preempt.
- **Wire `Goal.Deadline`** into queue ordering (currently dead).
- **MCP surface:** `bt_goal_add` (from text), `bt_goal_list`, `bt_goal_remove`,
  `bt_goal_from_pattern`.

### Phase 3 — Tree Factory v2 (plan→BT compiler + real crossover)

> **Status: CORE IMPLEMENTED (2026-07-08).** `goap.CompilePlanToTree`
> (`internal/goap/compile.go`) compiles a plan into the standard scaffold
> (PreGate → StrategyRouter → ReflectOnOutcome → OutcomeSelector); each step
> becomes `Sequence[GoapStateMatches guard → Action|ChainAction →
> ApplyGoapEffects write]`, with the PreGate seeding the initial world state
> so guards hold exactly as the planner proved. Steps map to registered
> engine actions via `CompileOptions.KnownAction`, else to ChainActions
> using `LLMPrompts` templates or derived prompts with persona style hints;
> a `GoapReplanPath` (SetupGoapTools → PlanGoapActions → ExecuteGoapStep) is
> the router's last branch. New engine leaves `GoapStateMatches:<k=v,…>` /
> `ApplyGoapEffects:<k=v,…>` (`internal/engine/goap_compiled_nodes.go`) are
> name-parameterized like ChainAction and accepted by both validators.
> Provenance (goal, plan hash, steps, user) lands in root metadata.
> **Real structural crossover:** `knowledge.Factory` gained `Resolve`/
> `Validate` hooks — `crossoverBreed` now splices parent A's actual PreGate ×
> parent B's actual StrategyRouter (deep-copied, validation-gated, cached on
> templates), falling back to synthetic templates only when parents have no
> stored structure (fixes R10). Wired in `cmd/bt-agent` (`newTreeFactory`)
> for `bt_factory_create` + `bt_kg_auto_create`, plus new `bt_goal_compile`
> tool: stored goal → GOAP plan → compiled tree → validate → persist →
> KG-register (71 tools). Remaining: consuming `goap.ActionRegistry` at
> execution time (engine actions cover it), QuickValidate benchmark gating
> at breed time (full validation gates instead).

**Objective:** the agent can *manufacture* new, persistent, evolvable trees.

- **Plan-to-BT compiler** — `goap.CompilePlanToTree(plan, opts) SerializableNode`:
  each `Action` step becomes a `Sequence` of [`Condition` guards from preconditions →
  executable node → blackboard effect writes]. Steps map to **registered engine
  actions when the action name matches the registry** (finally consuming
  `ActionRegistry`/`LLMActions`, which today are dead), else `ChainAction` with an
  LLM prompt derived from the step + user profile style. The whole plan is wrapped in
  the standard scaffold (PreGate → plan body → `ReflectOnOutcome` → OutcomeSelector)
  so gardener mutations and reflections work unmodified. Replan-on-failure is
  preserved by keeping a `GoapReplanPath` fallback (dynamic `PlanGoapActions` +
  `ExecuteGoapStep`) as the selector's last branch.
- **Real structural crossover** — fix `knowledge.Factory.extractTemplates` to load
  actual parent `SerializableNode` JSON from the tree store and splice real subtrees
  (PreGate of A × StrategyRouter paths of B), with `ValidateTreeFull` +
  `QuickValidate` gating the child. Falls back to archetype templates only when
  parents have no stored structure.
- **Preference-aware generation** — the factory consults `persona.Profile` when
  emitting ChainAction prompts (style, verbosity, preferred tools) and records
  provenance in `EvolutionMetadata` (parent goals, plan hash, user).
- **Every generated tree**: validated → benchmarked → saved to
  `users/<user>/trees/tree-<id>.json` → KG-registered → resolvable (Phase 0).
- **MCP surface:** `bt_tree_from_goal` (goal → plan → compiled tree),
  `bt_tree_from_plan`, upgraded `bt_factory_create` (real crossover).

### Phase 4 — Automatic GOAP-BT creation during collaboration

> **Status: CORE IMPLEMENTED (2026-07-08).** Interaction-time autopilot in
> `cmd/bt-agent/autopilot.go`: after every good user-attributed `bt_run_task`
> (and via the new in-tree `ConsiderTreeCompile` engine action, wired through
> the `engine.ConsiderAutomationFn` injection hook +
> `ChainState["persona_user"]`), `considerAutomation` mines the user's habits
> (keyword clustering — deliberately LLM-free so the hook adds no latency),
> and for the first recurring pattern without a ledger entry runs the full
> pipeline: `GoalFactory.FromPattern` → GOAP plan → `CompilePlanToTree` →
> validate → persist → KG-register → HITL proposal via `hitl.DefaultStore`
> ("I noticed you asked for X n times; approve to schedule agent Y").
> `bt_hitl_approve` activates approved proposals as scheduled
> `agent.Definition`s (suggested cron from observed frequency: daily ≥0.75
> runs/day, else weekly); `bt_hitl_reject` records the rejection so the habit
> is never re-proposed. Anti-spam rails: per-pattern dedup + rejection memory
> in `persona.AutomationStore` (`users/<user>/automations.json`, keyed by
> keyword signature), the profile's `MaxAutoCreatedAgents` cap, and HITL
> default-on (`ApprovalPolicy.AutoApproveAutomations` opts into direct
> activation). New MCP tool `bt_automation_propose` triggers the loop
> on demand (72 tools). Remaining: `ConsiderTreeCompile` is registered but
> not yet emitted into the standard scaffold's GoapPlanningPath (the
> bt_run_task session hook covers production runs).

**Objective:** while working with the user, the agent proposes and builds automations
on its own — the "self-customizing" behavior.

- **In-run decision path** — extend the merged tree's `GoapPlanningPath`: after
  `PlanGoapActions` succeeds and the run outcome is good, a new `ConsiderTreeCompile`
  action checks — *has this plan (or a habit-miner pattern) succeeded ≥N times?* If
  yes → compile via Phase 3 → register → propose.
- **Proposal flow with HITL** — the compiled automation is presented via the existing
  HITL queue (`bt_hitl_list/approve`): "I noticed you ask for X weekly; I built a tree
  and an agent definition — approve to schedule?" On approval, write the agent YAML
  (`Definition{Tree: <new id>, Schedule: <suggested cron>}`) into the registry.
  Auto-approve only if the profile's approval threshold allows it.
- **Session hook** — `bt_run_task` gains an optional `user` argument; when present,
  interaction logging, profile injection, and the consider-compile path activate.
  Without it, behavior is exactly as today (no regression for Hermes).

### Phase 5 — Self-evolution of personal trees

> **Status: CORE IMPLEMENTED (2026-07-08).** Personal trees now live in the
> user's workspace and evolve on the user's own signal:
> - **Per-user persistence + resolution** — user-attributed compiles
>   (`bt_goal_compile`, autopilot) persist via `persistGeneratedTreeForUser`
>   into `users/<user>/trees/tree-<id>.json` (`evolution.SaveNamedTree`);
>   `agentexec.ResolveGeneratedTree` gained a user-workspace fallback so
>   scheduled automations still resolve the personal tree by ID.
> - **Per-user gardener registry** — `gardener.NewRegistryWithUsers` scans
>   `<usersRoot>/<user>/trees`; entries carry `User` and are named by the
>   tree's root node (= tree ID) so reflection filtering and resolution agree.
>   `cmd/bt-gardener` wires `agent.UsersDir()`. Snapshots/rollback per tree
>   as before; `SaveTree` writes back into the owner's workspace.
> - **Strict evidence for personal trees** — `evolution.FilterByTreeNameStrict`
>   (no global-pool fallback) + an evidence gate in `evolveTreeV2`: a user tree
>   with zero own reflections is skipped (`SkippedNoEvidence`), never mutated
>   blind. `EvolveWithoutReflections` stays false.
> - **Unfreeze new trees safely** — `seedCompileReflection` writes the
>   compile-time plan validation as the tree's first reflection (stable
>   `seed-<id>` TaskID, so recompiles overwrite instead of accumulating
>   synthetic evidence); the gardener can then evolve it from day one.
> - **User feedback as fitness** — `bt_feedback` MCP tool (tool #73:
>   👍/👎 + correction text) stores reflections with
>   `Record.UserFeedback`; `evaluator.EvaluateTree` folds them into the new
>   `user_satisfaction` dimension (composite rescaled 90% base + 10%
>   satisfaction only when feedback exists). Two negatives flag the tree for
>   supervised review.
> - **Per-user `ExperienceBank`** — `gardener.Config.UserExperienceRoot`;
>   personal trees bias against and record into
>   `users/<user>/experience` (lazily opened, cached, falls back to the
>   shared bank).
>
> Remaining (deliberately deferred): implicit feedback signals (task re-asked
> = negative, output reused = positive) and folding `user_satisfaction` into
> the NSGA-II `MultiFitness` vector for population-level evolution.

**Objective:** personal trees improve from *this user's* signal, not just benchmarks.

- **Per-user gardener registry** — `gardener.Registry` gains a user-workspace scan
  root; `cmd/bt-gardener` iterates user dirs. Snapshots/rollback per user.
- **Unfreeze new trees safely** — seed each compiled tree with its compile-time plan
  validation + first supervised runs as initial reflections, so the evidence gate has
  data instead of freezing the tree; keep `EvolveWithoutReflections=false`.
- **User feedback as fitness** — add a `user_satisfaction` dimension to
  `MultiFitness`: explicit signals (`bt_feedback` MCP tool: 👍/👎/correction text) and
  implicit ones (task re-asked = negative, output reused = positive) recorded into the
  user's reflection store. Gardener v2's composite weights include it for user trees.
- **Per-user `ExperienceBank`** — mutation priors learned on one user's trees bias
  that user's future evolution (bank already supports a base dir after Phase 1).
- **Guardrails** — quality gate floor and 20%-regression cap unchanged; a personal
  tree that regresses rolls back from snapshots exactly like catalog trees; runaway
  automation is bounded by existing per-agent circuit breakers + `Budget` nodes.

### Phase 6 — Consolidation (debt aligned with arc42 §11)

> **Status: IMPLEMENTED (2026-07-08).**
>
> - **GOAP unified** — `engine.PlannerNode.Plan` now delegates to
>   `goap.NewPlanner` (the platform's single A* implementation); the parallel
>   search in `planner_node.go` (`plannerHeap`, `aStarPlan`, `greedyPlan`) was
>   deleted. The engine's bool-typed domain keys are seeded to `false` before
>   planning so missing-key-means-false semantics are preserved exactly;
>   `Mode: "greedy"` remains accepted and runs A* (which finds a complete plan
>   whenever the greedy walk did). `ParamPlannerNode` is unchanged on top.
> - **Dead code retired** — `factory.SkillSpec`; the gardener v1
>   `RunCycle`/`evolveTree` pipeline together with its idempotency helpers
>   (`hasNodeNamed`, `isNodeWrapped`, `getRetryCount`, `hasChildNamed`);
>   unused `Config.TT` (and its wiring in `cmd/bt-gardener` and every test).
>   The v1-only safety rails were **ported into `evolveTreeV2`** rather than
>   dropped: the evidence gate now covers all trees (not just personal ones),
>   the 20x bloat cap guards extreme growth, and `CrisisDetector` — wired in
>   production config but consumed only by v1, i.e. dead in practice — is live
>   again (stagnation detection doubles the mutation budget for the cycle).
> - **arc42 refresh** — §4 (single-planner strategy), §11 (R11 mitigated;
>   dead-code debt row resolved), API_REFERENCE gardener section; ADR-010
>   status updated to Phases 0–6. The bt-docgen pipeline targets the original
>   Linux workspace, so sections were updated in place.

---

## Part 4 — Sequencing, Risks, Metrics

**Order matters:** 0 → 1 → 2 → 3 → 4 → 5 → 6. Phases 0–1 are small and unlock
everything; 2 and 3 can be developed in parallel after 1; 4 needs 2+3; 5 needs 4
producing trees with run history.

| Risk | Mitigation |
|---|---|
| LLM goal extraction produces unplannable goals | World-state vocabulary grounding + `ValidatePlan` before any tree is compiled; reject/repair loop capped at 2 attempts |
| Automation spam (agent proposes too much) | Pattern threshold (≥3 occurrences), HITL approval default-on, per-user cap on active auto-created agents |
| Personal trees regress user experience | Feedback-as-fitness + existing quality gate/rollback; `bt_feedback` 👎 twice → tree flagged for supervised re-run |
| Per-user scaling of gardener cycles | User registries scanned round-robin with cycle budget; evidence gate already skips inactive trees |
| Ollama down → no embeddings/goal extraction | Keyword fallback for habit mining (existing KG pattern); goal factory degrades to archetype templates |

**Success metrics**

- ≥90% of auto-created trees are executable end-to-end (Phase 0 integration test).
- Habit miner: a task repeated 3× yields an automation proposal within the next
  session.
- Compiled GOAP trees pass `QuickValidate` on first compile ≥80% of the time.
- After 10 gardener cycles on a personal tree with feedback, `user_satisfaction`
  fitness is non-decreasing (gate-enforced).
- No regression in `make check-full`; all new persistence under
  `~/.go-bt-evolve/users/` follows ADR-003 atomic writes.
