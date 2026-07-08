# arc42 Section 11 — Risks and Technical Debt

## Prioritized Risk Table

| ID | Severity | Risk | Impact | Mitigation |
|---|---|---|---|---|
| R1 | ~~HIGH~~ **MITIGATED** | **Mutation Death Spiral** — 97.3% of mutations regressed fitness with no acceptance gates. | Evolution produced worse trees over time. | **Shipped:** gardener v2 pipeline gates every candidate — `benchmark.QuickValidate` pre-check, QualityGate (fitness floor 30, ≤20% regression, 5-strike disable), pre-mutation snapshots with rollback, ValidationGate before persist. Residual: MCP evolution tools still use structural-only fitness. |
| R2 | **HIGH** | **Single Point of Failure** — bt-agent is the sole task execution path for MCP tools. If it crashes or hangs, all Hermes Agent BT operations fail. | Dashboard sprints, cron jobs, and manual task delegation all blocked. | Add worker pool for horizontal scaling. Implement health-aware load shedding in gateway. |
| R3 | **MEDIUM** | **Dead Code** — Graphify reports 327 isolated nodes (no edges to the main graph). Dangling functions, unused types, dead test helpers. | Binary bloat, confusing codebase navigation, wasted maintenance effort. | Dead Code Sweeper cron removes isolated nodes weekly. Graphify community analysis flags candidates. |
| R4 | **MEDIUM** | **Package Sprawl** — 36 packages for ~136 source files (3.8 files per package). Many packages are thin wrappers (2-3 files). | Import complexity, circular dependency risk, harder to understand boundaries. | Consolidate to ~22 packages. Merge thin packages into domain-coherent groups. |
| R5 | **MEDIUM** | **Dashboard Untested** — 910-line `cmd/bt-dashboard/main.go` with 0 dedicated tests. Pipeline handlers, task CRUD, and agent management have no test coverage. | Dashboard bugs go undetected until manual testing. Sprint failures are silent. | Add handler tests for all API endpoints. Add integration tests for sprint execution. |
| R6 | **MEDIUM** | **MCP + A2A Duplication** — Two separate server implementations (MCP stdio vs A2A HTTP) with overlapping auth, rate limiting, and tool registration. | Duplicated security logic, inconsistent behavior between protocols. | Extract shared server base. Unify auth, rate limiting, and middleware. |
| R7 | **LOW** | **DeepSeek API Dependency** — Escalation path depends on external API (api.deepseek.com). Outage or rate limiting blocks batch LLM work. | Batch processing delayed. Local Ollama is always available as fallback. | Monitor API health. Keep Ollama as always-available fallback. Consider additional providers. |
| R8 | **LOW** | **Evolution Engine Sprawl** — 13 graphify communities for evolution code. Overlapping strategies between Stockfish and Pareto causing redundant optimization. | Harder to reason about which algorithm applies when. Maintenance burden. | Strategy interface consolidation. Unify common pipeline stages across algorithms. |
| R9 | **HIGH** | **Creation ↔ Execution Disconnect** — `bt_kg_auto_create` / `bt_factory_create` register trees in the KG, but `domains.ResolveTreeID` only resolves compiled-in Go trees; generated trees silently fall back to `DefaultTree()`. | Blocks the entire Q4 personalization roadmap: no generated tree is actually runnable. | Phase 0 of ADR-010 plan: `DynamicResolveFn` hook loading `tree-<id>.json` from tree store / user workspace + KG auto-registration. Integration test: auto-created tree executes end-to-end. |
| R10 | **MITIGATED** (2026-07-08) | **Shallow Crossover** — `knowledge.Factory.Breed` used category-generic templates; parent trees contributed no structural DNA. | "Tree breeding" claim in §1.1 was only nominally true. | Fixed in Phase 3: `Factory.Resolve`/`Validate` hooks; `structuralCrossover` splices parent A's real PreGate × parent B's real StrategyRouter, deep-copied and gated by `ValidateTreeFull`; synthetic templates remain only as the no-structure fallback. |
| R11 | ~~MEDIUM~~ **MITIGATED** (2026-07-08) | **GOAP Duplication + Transient Plans** — two parallel planners (`internal/goap` A* wired via engine actions; separate test-only `engine.PlannerNode` A*). | Duplicate maintenance for two search implementations. | Phase 3 delivered `goap.CompilePlanToTree` (plans become durable, evolvable trees) and activated `GoalQueue` + `Goal.Deadline` (Phase 2). Phase 6 deleted the parallel search: `engine.PlannerNode.Plan` now delegates to `goap.NewPlanner` (single A* implementation). |
| R12 | **MEDIUM** | **No User Identity** — memory, reflections, experience bank, and feedback.json are global or agent-scoped; the only `UserProfile` lives in the isolated DoorMate feature. | Personalization (Q4) impossible: nothing partitions learning per user. | Phase 1: `internal/persona` + per-user workspace `~/.go-bt-evolve/users/<user>/`; base-dir parameterization of MemoryStore, reflection store, ExperienceBank; DoorMate migrates onto persona. |
| R13 | **MEDIUM** | **Evidence Gate Freezes New Trees** — gardener skips trees without reflections (`EvolveWithoutReflections=false`); freshly generated personal trees would never evolve. | Self-evolution loop stalls at birth for every auto-created tree. | Phase 5: seed compile-time plan validation + first supervised runs as initial reflections; keep the gate closed otherwise. |

## Known Technical Debt

| Debt Item | Status | Resolution |
|---|---|---|
| Duplicate utility functions (strings, maps, template) | **Fixed** (2026-05-31) | Extracted to `internal/util/` with comprehensive tests (90.5% coverage) |
| Silent error suppression in test helpers | **Fixed** (2026-05-31) | All `_ = err` patterns replaced with explicit checks |
| DefaultTree god node (750+ lines) | **Fixed** (2026-05-31) | Extracted to 21-path merged tree across 7 category files |
| max_tokens audit (nodes with max_tokens=1) | **Aspirational** | Identify and fix ChainAction nodes with insufficient token budgets |
| Mutation quality gates | **Fixed** (gardener v2) | QualityGate + benchmark pre-validation + snapshots/rollback + ValidationGate in `internal/gardener/evolve_v2.go` |
| Dead Code Sweeper cron | **Aspirational** | Weekly cron to detect and remove isolated code |
| `internal/factory` routing gap | **Open** (Phase 0) | Generator never emits logic setting `ChainState["route"]`, so `DecisionTree` strategy paths are unreachable in skill-compiled trees |
| Dead code slated for activation or removal (ADR-010) | **Resolved** (Phase 6, 2026-07-08) | Activated: `goap.Agent`, `GoalQueue`, `BlackboardBridge`, `Goal.Deadline`, `LLMActions`, gardener `CrisisDetector` (rewired into v2). Removed: `factory.SkillSpec`, gardener v1 `RunCycle`/`evolveTree` (v1-only rails — evidence gate, bloat cap, crisis detection — ported into `evolveTreeV2`), unused `Config.TT`, test-only `engine.PlannerNode` A* (delegates to `internal/goap`) |
| Naming confusion: `internal/factory` vs `knowledge.Factory` | **Open** | Skill compiler vs breeding factory share the "factory" name; clarify when Tree Factory v2 lands |

---

*Generated by bt-agent arc42 pipeline — section11Risks tree*
