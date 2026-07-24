# 5. Building Block View

Static decomposition of the platform into packages and modules. Runtime
scenarios live in [§6](06-runtime-view.md), deployment in
[§7](07-deployment.md), and decision rationale in
[§9](09-decisions.md) — blocks here carry only `(→ ADR-NNN)`
pointers.

## 5.0 Composable Blocks

Reusable **SubTreeRef** blocks (`core:*`) in `internal/blocks` compose
on-demand task trees (→ ADR-131). `BuildAndValidate` expands refs at build
time via `RegisterTreeExpander` (`internal/engine/tree_expand.go`, wired from
`internal/blocks` init through `blocks.Expand`). Default pipelines:

- `DefaultTaskBlocks` — pre_gate → tools profile → tool_execution → error_handling
- Presets `agentic`, `full` — plan/RAG/clarify/HITL variants via `ComposePreset`

Persistence: custom blocks under `~/.go-bt-evolve/blocks/`. Evolution can
mutate block refs (`FilterEvolutionMutations`). Managed over MCP by the
`bt_blocks_*` tool family (5.1).

## 5.1 Whitebox Overall System

**Motivation.** The decomposition follows dependency direction: entrypoints
depend on services, services on the two engines, engines on the knowledge and
infrastructure layers — never the reverse. `internal/engine` receives
collaborators through injection hooks rather than importing service packages
(keeping it import-cycle-free), and the frequently-evolving layers (trees,
evolution algorithms) are separated from stable infrastructure so mutation and
rollback never touch reliability or security code.

### Layer Model

```
┌─────────────────────────────────────────────────────────────┐
│ ENTRYPOINTS (cmd/)                                          │
│ bt-agent  bt-dashboard  bt-evaluator  bt-langagent          │
│ bt-gardener  bt-agent-cli  bt-assistant  benchcmp           │
│ bt-docgen  bt-ci-doctor  bt-scalability-probe               │
│ bt-security-probe  bt-tree-integration                      │
├─────────────────────────────────────────────────────────────┤
│ SERVICE LAYER                                               │
│ a2a/  agent/  agentexec/  api/  audit/  dashboard/          │
│ domains/  hitl/  startup/  thinktank/                       │
├─────────────────────────────────────────────────────────────┤
│ CORE ENGINE (internal/engine/)                              │
│ tree.go  chains.go  registry.go  tools_real.go              │
│ Blackboard  BuildTree  RunTask  ChainAction  ActionRegistry │
├─────────────────────────────────────────────────────────────┤
│ EVOLUTION ENGINE (internal/evolution/, internal/evaluator/) │
│ Stockfish  Pareto  MAP-Elites  Island  Q-Learning           │
│ Mutate  Expert  Learning  VaultManager                      │
├─────────────────────────────────────────────────────────────┤
│ KNOWLEDGE LAYER                                             │
│ factory/ (agent factory, tree generator)                    │
│ knowledge/ (graph, discovery, embeddings)                   │
│ research/ (knowledge store, program store, quota economy)   │
├─────────────────────────────────────────────────────────────┤
│ INFRASTRUCTURE                                              │
│ benchmark/  blackboard/  blocks/  cicd/  config/            │
│ doormate/  fusion/  gardener/  goap/  llm/  persona/        │
│ reliability/  security/  tracing/  util/                    │
└─────────────────────────────────────────────────────────────┘
```

### Contained Blackboxes

One row per package — all 31 `internal/` packages appear exactly once,
alphabetical within each layer (matching the diagram order):

| Layer | Package | Responsibility |
|---|---|---|
| Entrypoints | `cmd/` (13 binaries) | Standalone binaries, each with its own main.go: MCP servers (`bt-agent`, `bt-evaluator`, `bt-langagent`), `bt-dashboard`, the `bt-gardener` daemon, CLIs (`bt-agent-cli`, `bt-assistant`), and build/CI/probe utilities (`benchcmp`, `bt-docgen`, `bt-ci-doctor`, `bt-scalability-probe`, `bt-security-probe`, `bt-tree-integration`) |
| Service | `internal/a2a` | Agent-to-Agent (A2A) protocol integration, incl. auction-based task allocation ([§8](08-crosscutting-concepts.md) A2A Auction Task Allocation) |
| Service | `internal/agent` | Agent lifecycle: registry, scheduler, memory, pub/sub AgentBus |
| Service | `internal/agentexec` | In-process run-dependency wiring (`NewRunDeps`, dynamic tree resolvers); `ResolveGeneratedTree`/`ResolveGeneratedTreeForUser` consult the matching `persona.AutomationRecord.Status` before returning a tree, refusing pending/rejected/flagged automations; the same gate is exported as `AutomationBlocked` and now also guards `cmd/bt-agent`'s `resolveTreeForUser` (wired as `agent.RunDeps.ResolveTreeForUser`), which previously fell through unchecked to `domains.ResolveTreeIDForUser`'s default-tree fallback instead of refusing a flagged automation (→ ADR-133) |
| Service | `internal/api` | API design primitives (OpenAPI route definitions) |
| Service | `internal/audit` | Append-only JSONL audit logging for agent tasks |
| Service | `internal/dashboard` | Dashboard API, metrics collection, SSE streaming (5.4) |
| Service | `internal/domains` | Domain-specific behavior trees (catalog below); `ExpectedDomainIDs` converts a tree registry to the `domain:<name>` ID form `knowledge.KnowledgeGraph.ExpectedDomains` expects, giving `cmd/bt-agent` and `cmd/bt-dashboard` one canonical conversion instead of each duplicating it (→ ADR-182) |
| Service | `internal/hitl` | Human-in-the-loop approval for behavior tree execution |
| Service | `internal/startup` | Startup-company simulation |
| Service | `internal/thinktank` | Collaborative analytical think tank |
| Core Engine | `internal/engine` | Behavior tree runtime: BuildTree, RunTask, Blackboard, chains (5.5), action/condition registry (inventory in [§1.1](01-introduction-goals.md)), GOAP/planner nodes (5.2), event bus |
| Evolution | `internal/evaluator` | Stockfish-adapted tree evaluator behind the `bt-evaluator` MCP server |
| Evolution | `internal/evolution` | Six evolution algorithms, mutation operators, fitness scoring, durable per-algorithm archives, safety gates, vault manager (5.3) |
| Knowledge | `internal/factory` | Skill-to-behavior-tree compiler; breeding via crossover + archetypes |
| Knowledge | `internal/knowledge` | Knowledge graph: semantic index of all trees with capabilities and embeddings (hardened embedding client → ADR-074); discovery and auto-creation; runtime-feedback persistence closing the learn→evolve loop across restarts ([§8](08-crosscutting-concepts.md) File-Based Persistence); change-impact graph consumed by `bt-agent-cli impact` and the `bt_impact_tests` MCP tool (→ ADR-052, ADR-070, ADR-071) |
| Knowledge | `internal/research` | Research memory: content-hash-dedup knowledge store + multi-cycle program store (ADR-003 JSON); `UpdatePrograms` holds `reliability.AcquireFileLock` across the whole open→mutate→save cycle so a milestone read-modify-write can't lose a concurrent sibling's update the way the old per-call `OpenPrograms`+`Save()` pattern could — now adopted by every `internal/engine` program-store writer (GOAP-fusion milestone-charge/refund/red-pass/program-registration/milestone-completion and the self-fix seeder's own write), closing the engine-wide gap; the remaining `research.OpenPrograms` call sites (`arc42_seeder.go`, `goap_seed_program.go`, `actions_superpowers.go`, `graphify_components.go`, `nlm_quota.go`) are read-only lookups with nothing to persist (→ ADR-183); `ProgramStore.ClaimActiveForCycle`/`ReleaseClaim` add a bounded per-program claim/lease (`Program.ClaimedBy`/`ClaimedAt`, default lease = the cycle budget) so a sibling cycle's GOAP-fusion charge pass skips a program another agent is still actively landing instead of double-planning it, with a stale claim past the lease — or an explicit release on refund/red-pass-reset/milestone-completion-success — reclaimable rather than wedging the queue |
| Infrastructure | `internal/benchmark` | A/B testing and statistical mutation-quality benchmark suites; `RunSuite` now cross-checks each task's detected `Path` against its declared `TaskCase.ExpectedPath`/`PossiblePaths` (`pathMatches`, `Result.PathMatched`, `RunMetrics.PathMatchRate`), and `RunABTest`/`ScoreMutation` weight the `PathMatchRate` delta so a mutation that reroutes tasks onto the wrong `StrategyRouter` branch scores non-positive even when mocked actions on the wrong path still report `bb.Outcome == "success"` |
| Infrastructure | `internal/blackboard` | Scoped blackboard persistence (Manager, per-scope limits) |
| Infrastructure | `internal/blocks` | Composable SubTreeRef blocks (5.0) |
| Infrastructure | `internal/cicd` | Local and GitHub Actions CI/CD validation (CI doctor) |
| Infrastructure | `internal/config` | Environment-based configuration |
| Infrastructure | `internal/doormate` | Adaptive intent/page assistant (IntentSession, PageSchema, UserProfile) |
| Infrastructure | `internal/fusion` | Multi-model panel fusion: RunPanel → Judge → Synthesize |
| Infrastructure | `internal/gardener` | 24/7 tree-evolution daemon; `RunCycleV2` ranks trees via `Config.KnowledgeGraph`'s `ComputeAnalytics()` (Bottlenecks first, then SelectionPressure), spending the per-cycle mutation budget on trees that need it before falling back to alphabetical order when unset (→ ADR-146); `cmd/bt-gardener/config.go`'s `buildGardenerConfig` now wires a live `*knowledge.KnowledgeGraph` (with persisted feedback loaded) so this ranking runs in production, and `evolveTreeV2` writes an `"evolved"` `RunRecord` back to it whenever a cycle accepts a mutation, mirroring `recordEvolvedFitness` (`cmd/bt-agent/tools.go`) and closing the daemon's side of the learn→evolve loop; `Registry.Rescan()` re-runs the user-tree scan and is called once per `cmd/bt-gardener` ticker cycle, so autopilot-compiled/HITL-approved personal trees become visible without a daemon restart (→ ADR-133); `CollectAgentSLOs` now reads persisted cross-process evidence via `engine.LoadSLOEvidence(EvidencePath)` instead of the in-process-only `engine.AllSLOMetrics()` sync.Map — always empty here since the gardener process never executes trees itself — mirroring `ValidationGate`'s file-fallback (`validation_gate.go`), so `RunCycleV2`'s per-cycle `slo-metrics.json` export and the dashboard's `GardenerMetrics.SLOs` (5.4) are no longer permanently empty in production |
| Infrastructure | `internal/goap` | GOAP A* planner — the single search implementation (→ ADR-133) |
| Infrastructure | `internal/llm` | LLM providers: Ollama client + DeepSeek escalation; `Client.GenerateWithMaxTokens` threads a `ChainConfig.MaxTokens` budget (5.5) through to the Ollama call (`num_predict`), forwarded by the `ErrorRecorder`/`TracedLLM`/`FallbackLLM` decorators via a `maxTokensCapable` type-assertion (mirroring `internal/engine/chains.go`'s `maxTokensLLM`) so the cap survives wrapping instead of being silently discarded on every real call |
| Infrastructure | `internal/persona` | Per-user personalization layer (→ ADR-133, 5.6) |
| Infrastructure | `internal/reliability` | Circuit breakers, retry, DLQ, error categorization; canonical block-fitness outcome scoring (`ScoreOutcome`), reused by `internal/blocks`' `ScoreFromBlackboard`, `internal/engine`'s `fitnessScoreFromBB`, and `internal/dashboard`'s `recordBlockFitnessMetric` (5.4) in place of the formula previously triplicated across all three; the DLQ's `.lock`-sidecar flock (→ ADR-036) is now exported as `AcquireFileLock` so other packages guarding their own sidecar file can share one idiom instead of reimplementing it — first reused by `research.UpdatePrograms`; `CircuitBreaker` is the platform's canonical 3-state (closed/open/half-open) breaker — `internal/agent`'s scheduler types (`AgentCircuitBreaker`/`AgentCircuitBreakerStore`) are now aliases/thin wrappers over it, retaining only the scheduler-specific named registry, dashboard JSON persistence, and A2A winner-breaker key coexistence, in place of a second hand-rolled state machine; `CircuitBreakerStore` generalizes the named-registry pattern for future callers (not yet adopted elsewhere) |
| Infrastructure | `internal/security` | Auth, rate limiting, production security primitives |
| Infrastructure | `internal/tracing` | OpenTelemetry facade (local Grafana/Tempo/Loki via `make observability-up`) |
| Infrastructure | `internal/util` | Shared utility functions |

### Tree Catalog

~75 built-in trees (counts by category in [§1.1](01-introduction-goals.md));
the two main registries:

| Registry | Notable trees |
|---|---|
| `domains.AllDomainTrees()` (`internal/domains/trees.go` + `arc42_trees.go`) | Self-improvement/GOAP: `goap_fusion`, `goap_fusion_loop`, `goap_planning`, `goap_research`, `goap_devops` · research/fusion: `bt_fusion`, `notebooklm`, `notebooklm_consumer`, `notebooklm_plan_implement` · automation: `superpowers_workflow`, `hermes_update`, `bt_manager`, `arc42_seeder`, `auction_demo` · dev/ops: `code_review`, `devops_ci`, `refactoring`, `security_audit`, `crash_investigator`, `agent_monitor`, `alert_router`, `data_pipeline` · general: `meeting_notes`, `game_ai`, `trading_signal` · docs: `arc42:section1`…`arc42:section12`, `arc42:assemble` (root wrapping: 5.2) |
| `evolution.AllFinanceTrees()` (`internal/evolution/finance_trees.go`) | `pitch_agent`, `earnings_reviewer`, `market_researcher`, `model_builder`, `meeting_prep`, `valuation_reviewer`, `gl_reconciler`, `month_end_closer`, `statement_auditor`, `kyc_screener` |

Research, startup-role, thinktank, kanban, evolution, composed-block, and core
trees live beside these in `internal/evolution` (`research_trees.go`,
`goap_trees.go`, `fusion_trees.go`), `internal/startup`, `internal/thinktank`,
and `internal/domains/kanban.go`.

### MCP Tool Families (`cmd/bt-agent`)

The bt-agent server's 80 tools group into families, each backed by one
building block:

| Family | Tools | Backing block |
|---|---|---|
| Execution & routing (7) | `bt_run_task`, `bt_get_tree`, `bt_delegate_to_tree`, `bt_use_domain_tree`, `bt_use_finance_tree`, `bt_use_go_tree`, `bt_use_research_tree` | engine + domains |
| Evolution (12) | `bt_evolve`, `bt_evolve_genetic`, `bt_evolve_bottlenecks`, `bt_evolve_selection_pressure`, `bt_evolve_island`, `bt_evolve_qd`, `bt_evolve_qlearning`, `bt_evolve_memetic`, `bt_evolve_multiobjective`, `bt_evolve_pareto`, `bt_evolve_expert`, `bt_evolve_selectors` | evolution (5.3) |
| Composable blocks (9) | `bt_blocks_compose`, `bt_blocks_compose_evolve`, `bt_blocks_register`, `bt_blocks_get`, `bt_blocks_list`, `bt_blocks_list_profiles`, `bt_blocks_fitness`, `bt_blocks_freeze`, `bt_blocks_promote` | blocks (5.0) |
| Agent platform (10) | `bt_agent_create`, `bt_agent_run`, `bt_agent_list`, `bt_agent_delete`, `bt_agent_schedule`, `bt_agent_history`, `bt_agent_memory_read`, `bt_agent_memory_write`, `bt_agent_memory_delete`, `bt_create_agent` | agent |
| Knowledge graph (7) | `bt_kg_discover`, `bt_kg_query`, `bt_kg_list`, `bt_kg_summary`, `bt_kg_analytics`, `bt_kg_explain`, `bt_kg_auto_create` | knowledge |
| HITL (5) | `bt_hitl_list`, `bt_hitl_get`, `bt_hitl_approve`, `bt_hitl_reject`, `bt_hitl_compose_task` | hitl |
| Blackboard (4) | `bt_bb_read`, `bt_bb_write`, `bt_bb_list`, `bt_bb_delete` | blackboard |
| Personalization (10) | `bt_persona_get`, `bt_persona_patterns`, `bt_persona_set_preference`, `bt_goal_add`, `bt_goal_list`, `bt_goal_remove`, `bt_goal_from_pattern`, `bt_goal_compile`, `bt_automation_propose`, `bt_feedback` | persona + goap + hitl (`bt_automation_propose` and repeated-negative `bt_feedback` both raise `hitl.NewRequest` escalations; `escalateFlaggedTreeForReview` skips re-raising one while the tree is already flagged and pending review; `bt_hitl_approve`/`bt_hitl_reject` (HITL family below) resume the automation via the binary-agnostic `persona.FinalizeAutomationApproval`/`persona.FinalizeFeedbackEscalation` (`internal/persona/automation_finalize.go`), or leave it paused, closing the feedback-escalation loop; the dashboard's own HITL approve/reject path calls the same two functions (5.4), so dashboard-resolved automations activate/resume/quarantine identically) (→ ADR-133, 5.6) |
| Reliability/ops (5) | `bt_dlq_list`, `bt_dlq_replay`, `bt_circuit_status`, `bt_health`, `bt_reset` | reliability |
| Thinktank/startup/workflow (5) | `bt_thinktank_analyze`, `bt_startup_simulate`, `bt_startup_summary`, `bt_workflow_run`, `bt_workflow_approve` | thinktank + startup + dashboard |
| Misc (6) | `bt_factory_create`, `bt_get_fitness`, `bt_get_reflections`, `bt_list_finance_trees`, `bt_impact_tests`, `bt_gardener_dt_diagnostics` | factory, evolution, knowledge, gardener (`bt_gardener_dt_diagnostics` runs `Gardener.AnalyzeTreeDiagnostics` read-only against a named tree for HITL review) |

The bt-evaluator and bt-langagent servers' tool inventories are in
[§3.2](03-context-scope.md).

## 5.2 Core Engine

Level 2 whitebox of `internal/engine` (~112 source files; the load-bearing
blocks). Rows ordered alphabetically by (first) filename:

| Block | Responsibility | Interface |
|---|---|---|
| `arc42_nodes.go` | arc42 documentation pipeline nodes | 22 actions + 5 conditions |
| `chains.go` | 11 LLM chain types as first-class BT nodes (inventory in 5.5) | `ChainAction`; node `Name` = `chain_type:prompt_text` |
| `error_handler_node.go` | Self-extending Claude error handler wrapped around every domain tree root: on root failure, propose and gate a recovery node | `ClaudeErrorHandler` node type |
| `goap_nodes.go`, `goap_compiled_nodes.go` | GOAP planner integration: run the `internal/goap` A* planner and store the plan on the blackboard, then execute it step-wise; compiled-plan counterparts maintain the same blackboard state | `PlanGoapActions` (plan → blackboard), `ExecuteGoapStep` (next plan step) |
| `planner_node.go` | Utility-scored child selection as a composite node type; `PlannerNode` extends `UtilitySelector` with GOAP-style goal management (single-planner decision in [§4](04-solution-strategy.md) Key Technology Decisions #5) | `UtilitySelector`, `PlannerNode` node types (`BuildUtilitySelector`) |
| `registry.go` | Action/condition registry backing every leaf node (count in [§1.1](01-introduction-goals.md)); populated by the `actions_*.go`/`conditions_*.go` families (domain, finance, GOAP-fusion, superpowers, notebooklm, A2A, …) | `RegisterAction`, `RegisterCondition`, `GetAction`, `GetCondition` |
| `tools_real.go` | Real tool implementations available to chain actions | blackboard `ChainTools` |
| `tree.go` | Builds go-bt runtime trees from the SerializableNode IR and executes them with output-quality validation | `BuildTree`, `RunTask`, `Blackboard`, `buildNode`, `actionForName`/`conditionForName` |
| `tree_expand.go` | Expands `SubTreeRef` nodes at build time (5.0) | `RegisterTreeExpander` |

Key flow: `RunTask(bb, tree)` → `BuildTree(serTree, bb)` → `buildNode()` →
`go-bt Command[Blackboard]` → tick loop (1000 max) → `validateOutputQuality()`

## 5.3 Evolution Engine

Level 2 whitebox of `internal/evolution`. Two shared mechanisms, stated once:
all durable archives use one persistence idiom — atomic tmp+rename under the
`acquireExperienceLock` sidecar flock, merge-on-load, optional cap,
missing-file cold start, corrupt-file error (→ ADR-024, ADR-033) — and all
eight evolve variants (`Evolve`, `EvolveWithExperience`, `EvolveQLearning`,
`MemeticEvolve`, `EvolvePareto`, `EvolveAll`, NSGA-II `Evolve`,
`EvolveMAPElites`) run the shared per-generation self-healing envelope:
crisis detect → emergency mutation rate → specialist-elite observe →
reproduce → resurrect → streak reset (→ ADR-038, ADR-051, ADR-121).
Rows ordered alphabetically by (first) filename:

| Block | Responsibility | Interface |
|---|---|---|
| `cmaes.go` | (λ,μ)-CMA-ES numeric parameter tuning over normalized [0,1] solutions; extracts `TimeoutMs`/`MaxRetries`/metadata params and writes them back with bounds clamping (→ ADR-020) | `CMAESOptimizer`, `TuneTreeParameters` (Extract → Optimize → Apply) |
| `crisis_detector.go` | Proactive diversity-collapse/regression-spiral/quality-crash detection with a calibrated emergency mutation rate feeding both the GA envelope and the gardener's mutation budget (→ ADR-031, ADR-102) | `DetectPopulation`, `Detect`/`Intervene`, `GetEmergencyMutationRate` |
| `decision_tree.go` | C4.5/CART information-gain metrics over Selector paths with durable count-summing archive; degrades to a no-op on empty telemetry (→ ADR-029) | `DTAnalyzer`, `BTOptimizer.OptimizeSelectors` |
| `experience_bank.go` | Persisted successful-mutation entries (EvoRepair-style), Jaccard retrieval by tree type, 500-entry quality-aware eviction, two-writer-safe lock→merge→write on every full-file path, source-aware cross-domain seeding (→ ADR-018, ADR-024, ADR-062) | `Add`/`Retrieve`/`RetrieveByTreeType`, `TransferExperiences`/`SeedDomain`, `MarkReused`, `Persist` |
| `expert.go` | Expert knowledge: 6 design patterns, 5 anti-patterns, tree archetypes, benchmark-validated specialist seeds (→ ADR-031); durable `LearnedPatterns` archive capped at 500 with lowest-gain eviction, fed by five production algorithms (→ ADR-095, ADR-103, ADR-104, ADR-106, ADR-110) | `ExpertKnowledge.Observe/Save/Load`, `SeedSpecialists`; read by `bt_evolve_expert` |
| `file_lock.go` | Cross-process exclusive advisory flock on the `.lock` sidecar, held across merge→rename; excludes two opens even within one process; idempotent release (→ ADR-024) | `acquireExperienceLock` |
| `finance_trees.go`, `fusion_trees.go`, `goap_trees.go`, `research_trees.go` | Built-in tree definitions living beside the algorithms (catalog in 5.1) | `AllFinanceTrees()` |
| `island.go` | Island model with periodic migration and per-domain durable merge archive bounded by `Cap`/`IslandCap` with observable eviction (→ ADR-033, ADR-034, ADR-040); cross-domain experience transfer riding migration via optional `Bank` (→ ADR-062, ADR-076); cross-island winner benchmark-gated (→ ADR-096); real within-island breeding + optional `ExpertKnowledge` (→ ADR-104, ADR-106) | `IslandModel.EvolveAll`/`Migrate`/`Save`/`Load`, `TotalMigrations`; driven by `bt_evolve_island` |
| `learning.go` | GA core: `Population`/`Individual` types, sole deep-copy (`cloneTree`), home of the shared self-healing envelope; Q-learning evolution with durable bounded QTable (→ ADR-041, ADR-095); read-only health snapshot (→ ADR-032); top-K experience retrieval | `Population.Evolve`/`EvolveWithExperience`/`EvolveQLearning`, `selfHealGeneration`, `QTable.Save/Load` (`Cap`), `HealthSnapshot`, `RetrieveExperienceHints` |
| `local_search.go` | Memetic refinement (hill-climb / simulated annealing / tabu) with the breeding step inside the shared envelope (→ ADR-121) | `LocalSearcher`, `Population.MemeticEvolve`; driven by `bt_evolve_memetic` |
| `map_elites.go` | MAP-Elites quality diversity over behavioral descriptors; durable capped merge-safe grid (→ ADR-033, ADR-043); shared envelope replaced its hand-inlined crisis loop, fixing a single-niche-collapse blind spot in `DiversityScore()` (→ ADR-121); optional `ExpertKnowledge` (→ ADR-104) | `BehavioralDescriptor`, `MAPElitesGrid.Save/Load` (`Cap`), `MAPElitesPopulation.EvolveMAPElites`; driven by `bt_evolve_qd` (→ ADR-110, gate → ADR-111) |
| `mcts_mutate.go`, `llm_supervisor.go` | MCTS-guided mutation search; deterministic, `-short`-safe heuristic supervisor mirroring the LLM policy contract (→ ADR-110) | `MCTSMutator.Mutate`, `LLMSupervisor.Guide` |
| `meta_validator.go` | P0 structural-safety gate distinct from fitness/quality/SLO scoring: root-type, node-name, structural-limit, selector-shape, retry-bound, anti-pattern, archetype-fit, and fitness-floor/regression checks in one explainable MetaAccept/MetaWarn/MetaReject decision, consulted last in the gardener's acceptance loop (→ ADR-088) | `ValidateMutation`; wired via `gardener.Config.MetaValidator` |
| `multi_objective.go` | NSGA-II: fast non-dominated sort, crowding distance, seeded specialists (→ ADR-051); durable final-front archive delegating to `ParetoFront` (→ ADR-091); persistence benchmark-gated (→ ADR-096); optional `ExpertKnowledge` observation (→ ADR-104, ADR-106) | `NSGAIIPopulation.Evolve`/`Save`/`Load` (`Cap`/`Archive`); driven by `bt_evolve_multiobjective` |
| `mutate.go` | SerializableNode IR + 10 mutation operators (add_before, add_after, wrap_retry, prune, swap_children, …) | `SerializableNode`, `MutationOp`, `ApplyMutations` |
| `pareto.go` | Pareto-front evolution with front-elitism crossover (→ ADR-038); durable dominance-merged, cap-evicting archive (→ ADR-091); benchmark-gated (→ ADR-113); optional `ExpertKnowledge` (→ ADR-104, ADR-106) | `MultiFitness`, `ParetoFront.Save/Load`, `ParetoPopulation.EvolvePareto`, `StructuralMultiFitness`; driven by `bt_evolve_pareto` (→ ADR-057) |
| `quality_gate.go` | Reactive regression/floor gate with per-tree consecutive-failure disable; multi-revision pre-mutation snapshot history, each optionally tagged with its composite fitness, so fail-closed rollback walks back past a multi-cycle regression streak to the last known-good revision rather than only the immediately-preceding one (→ ADR-093, ADR-115) | `Validate`/`ValidateFor`, `SnapshotTree`/`SnapshotTreeWithFitness`, `RestoreTree`/`RestoreTreeRevision`/`RestoreTreeBeforeRegressionStreak`/`ListRevisions`; `gardener_rollback` tool |
| `reflection_store.go` | Per-tree fitness evidence from run reflections | `FilterByTreeName`(`Strict`) |
| `selector_optimizer.go` | Per-Selector child success/failure/running telemetry, learned priority ordering, durable merge-summing stats (→ ADR-029, ADR-079) | `SelectorOptimizer`, `OrderChildren`, `ApplyLearnedOrdering`, `SaveSelectorStats`/`LoadSelectorStats`, `ParseSelectorOrderingStrategy` (validates an ordering-strategy string, defaulting to `OrderBySuccessRate`; shared by every production wiring site); driven by `bt_evolve_selectors` |
| `specialist_registry.go` | Best validated archetype per `specialist:<type>` so crisis recovery can resurrect an extinct niche; consulted by every production evolve variant through the shared envelope (→ ADR-031, ADR-038, ADR-051, ADR-121) | `Observe`/`ExtinctSpecialists`/`Resurrect`; seeded via `SeedSpecialistRegistry` |
| `stockfish_evolve.go` | Stockfish-adapted mutation ordering: transposition table + alpha-beta-style search (→ ADR-005) | `TranspositionTable` |
| `vault_manager.go` | Tree vault with checkpoint/restore | `VaultManager` |

## 5.4 Dashboard

Level 2 whitebox of `cmd/bt-dashboard` + `internal/dashboard` (HTTP server on
:9800, [§7](07-deployment.md)). Rows grouped by package (`cmd/bt-dashboard`
first, then `internal/dashboard`), alphabetical by filename within each:

| Block | Responsibility | Interface |
|---|---|---|
| `cmd/bt-dashboard/main.go` | HTTP server, embedded static FS, 8 route groups; owns the scalability substrate (task queue + agent router over a local executor) read by `/api/scalability`; `/api/trees` surfaces fitness + evolution lineage and merges in every `domains.AllDomainTrees()` catalog entry (as `domain:<name>`) not already registered in the runtime knowledge graph, so it can serve as one complete tree catalog (→ ADR-011, ADR-012, ADR-087, ADR-089, ADR-100, ADR-116, ADR-126); a shared, once-initialized `AgentCircuitBreakerStore` gates `/api/agents/execute` and `/api/agents/run` (503 if open) before worker-pool submission, mirroring the scheduler's job-skip check (→ ADR-063); `buildDashboardKnowledgeGraph` overlays persisted runtime feedback (`agent.FeedbackFile()`) onto the static tree catalog via `KnowledgeGraph.LoadFeedback` before wiring `dashboard.DiscoverTreeFn`, mirroring `cmd/bt-gardener/config.go`'s daemon-side wiring, so discovery and analytics reflect real fitness/run history instead of a zero-feedback seed catalog (→ ADR-158); a package-level `persona.Store` loaded from `agent.UsersDir()` and the in-process agent runner's `*agent.Registry` are wired into `dashboard.PersonaStore`/`dashboard.AgentRegistry` at startup, the same injection-hook pattern as `dashboard.DiscoverTreeFn`, so `internal/dashboard/hitl_handlers.go`'s HITL resolution can finalize automations (5.1 Personalization); the same hook pattern also wires `dashboard.KGAnalyticsRefreshFn` to this process's own `kg.ComputeAnalytics()` + `dashboard.RecordKGAnalytics`, so the KG analytics gauges (5.1 Knowledge graph tool family) refresh on every `/api/metrics` scrape (`PrometheusHandler`) from bt-dashboard's own in-process graph instead of depending solely on the separate bt-agent process's `bt_kg_analytics` MCP tool handler updating that other process's gauges; `buildDashboardKnowledgeGraph` also sets `kg.ExpectedDomains` from `domains.ExpectedDomainIDs(domains.AllDomainTrees())` — the same conversion `cmd/bt-agent/main.go` uses — closing the gap where the `bt_kg_coverage_gaps` gauge this scrape publishes was structurally pinned at 0 (the graph fell back to `knowledge.defaultExpectedDomains`, whose 8 entries are always registered) (→ ADR-182 self-fix); the same hook pattern also wires `dashboard.DLQCategoriesFn` to this process's own `dlq.CategoryCounts`, so `/api/dlq`'s `categories` field and `dashboard.Collect`'s `Metrics.DLQCategories` (5.4) both surface the live per-error-category DLQ rollup; main.go also constructs an in-process `internal/a2a.Server` from this same agent registry and installs `a2a.AuctionCardsFn = a2aSrv.AuctionCardSource()`, mirroring `cmd/bt-agent/main.go`'s identical wiring ([§8](08-crosscutting-concepts.md) A2A Auction Task Allocation) — without it, `internal/dashboard/executor.go`'s `PickTreeForTask` (below) routes auction-shaped tasks to `auction_demo` but the in-process `AuctionDelegate` node deterministically found zero bidders | `POST /api/agents/execute`, `GET /api/trees`, `POST /api/workflow/run-full-pipeline`, `buildDashboardKnowledgeGraph`, … |
| `cmd/bt-dashboard/pipeline_handlers.go` | Sprint/quarter/year pipeline API handlers; enqueues each run onto the dashboard task queue and drains it on completion | `/api/pipeline/*` |
| `cmd/bt-dashboard/static/` | Embedded web UI (HTML/JS/CSS), 8 tabs ([§1.1](01-introduction-goals.md)) | embed FS |
| `internal/dashboard/agents.go` | Agent listing + CRUD; `cb_status` reflects durable per-agent circuit-breaker state (→ ADR-063) | `ListAgentsWithCB` |
| `internal/dashboard/executor.go` | In-process agent execution with Hermes CLI fallback; task→tree routing checks auction-shaped text first (→ ADR-073), then the knowledge-graph discovery seam, with the static keyword switch as fallback (→ ADR-100); optional `CBStore` records each `RunTaskResult` outcome and persists it to `circuit_breakers.json`, mirroring the scheduler's `reportAgentOutcome`/`Save` (→ ADR-063); `RunTaskResult` also reports the `bt_agent_tasks_total`/`bt_agent_task_duration_ms`/`bt_block_fitness_score` Prometheus series for every run, closing the dashboard's dead-metrics gap (→ ADR-142) | `AgentExecutor.RunOnce`, `RunTaskResult`, `PickTreeForTask`, `DiscoverTreeFn`, `CBStore` |
| `internal/dashboard/hitl_handlers.go` | HITL approval REST surface; `AgentRegistry`/`PersonaStore` package vars are `main.go`'s injection hooks (nil-safe no-op until wired); on approve/reject, `finalizeHITLResolution` calls `persona.FinalizeAutomationApproval`/`persona.FinalizeFeedbackEscalation` (same shared functions the MCP `bt_hitl_approve`/`bt_hitl_reject` tools call, 5.1 Personalization) so a dashboard-resolved automation activates, resumes, or quarantines its tree exactly like the MCP path | `GET /api/hitl/pending`, `POST /api/hitl/{id}/{approve,reject,escalate}`, `finalizeHITLResolution` |
| `internal/dashboard/metrics.go` | Platform metrics assembly; parses the aggregate gardener-metrics document (→ ADR-032); ranks live knowledge-graph `TreeSnapshot`s into `TopWinners` — no UI panel reads it yet (→ ADR-126); `DLQCategoriesFn` injection hook (nil-safe, wired to `dlq.CategoryCounts` in main.go) fills `Metrics.DLQCategories` when set | `Collect`, `GardenerMetrics`, `DLQCategoriesFn` |
| `internal/dashboard/tasks.go` | Task CRUD with Approval audit records and priority-then-sprint dispatch ordering (→ ADR-072); atomic tmp+rename persistence, fail-loud on corruption (→ ADR-101) | `TaskStore.Approve`/`Reject`/`Approved` |
| `internal/dashboard/workflow_engine.go` | Workflow orchestration (Workflow/WorkflowTask/Approval, priority ordinals); production-wired task derivation, HTTP approval gates, and full-pipeline execution; bidirectional task-store sync (→ ADR-080, ADR-081, ADR-082, ADR-086, ADR-089, ADR-116) | `RecommendationsToTasks`, `Prioritize`, `PendingApprovals`/`ApproveTask`/`RejectTask`, `ExecuteSprint`, `RunFullPipeline`, `SetTaskStatus` |
| `internal/dashboard/workflow_orchestrator.go` | Multi-agent workflow coordination | — |

## 5.5 Chain Types

Level 3 detail of the engine's ChainAction block (5.2):

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
| 11 | `fusion` | Multi-model panel: fans the prompt to configured analysis models and synthesizes a final answer (→ ADR-130) | Same as llm_call; results in `{{.ChainState.fusion_*}}` |

Each ChainAction reads config from node `Name` (format:
`chain_type:prompt_text`) and `Metadata` (max_tokens, temperature, etc.).

## 5.6 Planned Building Blocks (ADR-133)

Roadmap blocks from `docs/plans/2026-07-08-personalized-self-evolving-agents.md`:

```
internal/persona/              (NEW — Phase 1)
├── profile.go        — Profile: identity, preference tags, style, tool habits,
│                       approval thresholds (generalizes doormate.UserProfile)
├── interaction.go    — Interaction log: {task, tree, outcome, duration, corrections}
└── habitminer.go     — Embedding-clustered pattern detection → RecurringPattern

internal/goap/                 (EXTEND — Phases 2–3)
├── goalfactory.go    — FromIntent (LLM structured extraction, world-state-grounded),
│                       FromPattern, goal archetypes
├── multi_goal.go     — GoalQueue activated: per-user persistent queue, deadlines
└── compile.go        — CompilePlanToTree: goap.Plan → SerializableNode BT

internal/domains/              (EXTEND — Phase 0)
└── tree_resolver.go  — DynamicResolveFn hook: resolve generated tree-<id>.json
                        from tree store / user workspace

internal/knowledge/            (FIX — Phase 3)
└── factory.go        — Real structural crossover from parent SerializableNode JSON

internal/gardener/             (EXTENDED — Phase 5 implemented)
├── user_trees.go     — Registry scan of ~/.go-bt-evolve/users/<user>/trees/, strict
│                       per-tree evidence, per-user experience banks
└── (evaluator)       — user_satisfaction fitness dimension from bt_feedback records
```

Per-user persistence layout (ADR-003 atomic writes):

```
~/.go-bt-evolve/users/<user>/
├── profile.json      — persona.Profile
├── trees/            — generated/evolved personal trees (tree-<id>.json)
├── goals/            — persistent GoalQueue
├── memory/           — user-scoped MemoryStore
├── reflections/      — user-scoped run reflections (feeds gardener evidence gate)
└── experience/       — per-user ExperienceBank (mutation priors)
```

---

*Generated by bt-agent arc42 pipeline — section5BuildingBlocks tree*
