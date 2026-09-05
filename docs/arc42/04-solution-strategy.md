# 4. Solution Strategy

Quality goals Q1-Q4 are defined in [§1.2](01-introduction-goals.md); some rows
trace to a [§2](02-constraints.md) constraint instead of, or in addition to, a
quality goal.

## Quality Goals → Solution Approaches

| Goal / Constraint | Scenario | Solution Approach | Details |
|---|---|---|---|
| Q1 | Tree routes through PreGate→StrategyRouter→OutcomeSelector correctly | **Behavior Trees as Execution Model** (→ ADR-001) | Sequence/Selector/Action/Condition/ChainAction nodes backed by the registered engine actions/conditions (inventory in [§1.1](01-introduction-goals.md)); tree validation before execution ([§5](05-building-blocks.md), [§8](08-crosscutting-concepts.md)) |
| Q1 | LLM produces valid structured output | **Output Quality Validation** | `validateOutputQuality()` applies length/pattern/structure checks yielding a QualityScore — [§8](08-crosscutting-concepts.md) Quality Gates |
| Q2 | Tree improves over successive mutations | **Stockfish-Adapted Evolution** (→ ADR-005) | Transposition table with move ordering; multi-dimensional fitness evaluation; mutation ordering by predicted fitness delta — one of two structural-mutation generators feeding the daemon's scored competition (see next row) ([§5](05-building-blocks.md), [§8](08-crosscutting-concepts.md)) |
| Q2 | Hand-written mutation rules cannot propose what they don't encode | **MCTS-Guided Structural Search** (→ ADR-246) | Bounded per-tree MCTS scores candidate mutations against the tree's own records (no benchmark, no LLM) and merges them with the heuristic ordering into one descending-score competition; a per-tree affinity check (specialist archetype + Selector telemetry) decides whether the search runs, and every candidate still clears the same benchmark/quality gate before it is applied ([§5](05-building-blocks.md), [§8](08-crosscutting-concepts.md)) |
| Q2 | Multiple fitness dimensions must be balanced | **Pareto Front + MAP-Elites** | MultiFitness across correctness, completeness, conciseness, actionability; ParetoFront tracks non-dominated solutions; MAP-Elites maintains quality diversity ([§5](05-building-blocks.md)) |
| Q2 | Evolution must not regress | **Git-Versioned + Benchmark Gating** (→ ADR-005) | Every mutation creates a git commit; benchmarks compare before/after; rollback on regression ([§8](08-crosscutting-concepts.md) Quality Gates) |
| Q3 | Goroutine panics don't crash the process | **SafeGo + Panic Recovery** (→ ADR-007) | All goroutines wrapped in SafeGo; tree-level panic recovery in RunTask(); circuit breakers prevent cascading failures ([§8](08-crosscutting-concepts.md) Error Resiliency) |
| Q3 | Transient LLM errors self-heal | **Retry with Exponential Backoff** (→ ADR-007) | Full jitter retry: 1s→2s→4s→8s (base 500ms, max 30s); 3 retry classes: standard, LLM-specific, unknown ([§8](08-crosscutting-concepts.md)) |
| Q3 | Exhausted retries don't lose work | **Dead Letter Queue** (→ ADR-007) | Persistent JSON file at `~/.go-bt-evolve/dead_letter_queue.json`; failed tasks preserved for manual inspection/replay |
| Q3 | Runaway or stuck trees must not hang the platform | **Bounded Execution Guardrails** | `RunTask()` applies `context.WithTimeout(120s)`; a 1000-tick safety limit terminates non-terminal trees as partial; longer work uses checkpoint/resume ([§6](06-runtime-view.md)) |
| Q2/Q4 | Task→tree mapping must be automatic | **Knowledge Graph + Factory** | Semantic discovery via embeddings; tree breeding via crossover (PreGate from A × StrategyRouter from B); 7 categories with capability edges ([§5](05-building-blocks.md)) |
| [§2](02-constraints.md) stdio constraint | External tools must be accessible | **MCP Protocol Layer** (→ ADR-002) | JSON-RPC 2.0 over stdio; 3 servers (per-server tool inventory in [§3.2](03-context-scope.md)); Hermes gateway manages lifecycle |
| Q3 + [§2](02-constraints.md) git-versioning policy | Agent state must survive restarts | **File-Based Persistence** (→ ADR-003) | Atomic writes (write .tmp → rename); YAML for agent definitions, JSON for scheduler/history/reflections; no SQL database — state under `~/.go-bt-evolve/`; git-friendly ([§8](08-crosscutting-concepts.md)) |
| Q1/Q3 | LLM must be integrated into BT nodes | **ChainAction Architecture** (→ ADR-006) | Declarative chain types with template variables — inventory in [§5.5](05-building-blocks.md) |
| Q4 (personalization) | Agent knows who it works with and what they do repeatedly | **Persona Layer** (`internal/persona`, planned — → ADR-133) | Per-user profile + interaction log + HabitMiner (embedding clustering with keyword fallback); workspace: `~/.go-bt-evolve/users/<user>/{trees,goals,memory,reflections,experience}` |
| Q4 (personalization) | User intent and habits become plannable goals | **Goal Factory** (planned — → ADR-133) | LLM structured extraction of `goap.Goal` grounded in a world-state vocabulary registry; goal archetypes; activates the existing `goap.GoalQueue` per user |
| Q4 (personalization) | Successful plans become durable automations | **Plan→BT Compiler / Tree Factory v2** (planned — → ADR-133) | `goap.CompilePlanToTree` emits precondition guards → registered actions/ChainActions → effect writes, wrapped in the standard PreGate/Reflect scaffold with a dynamic-replan fallback; real structural crossover from parent tree JSON |
| Q4 (personalization) | Generated trees must actually run | **Dynamic Tree Resolver** (planned — → ADR-133) | `domains.ResolveTreeID` fallback hook loads `tree-<id>.json` from tree store / user workspace, then `BuildAndValidate`; generated trees auto-register in the knowledge graph |
| Q4 (personalization) | Personal trees improve from user signal | **Feedback-as-Fitness** (implemented — → ADR-133 Phase 5) | `user_satisfaction` dimension in the evaluator's FitnessScore fed by `bt_feedback` (explicit 👍/👎 + correction; implicit signals planned); per-user gardener registry scan, strict per-tree evidence, compile-time seed reflections, and per-user experience banks under the existing quality-gate/rollback rails |
| Q4 (personalization) | Autonomy must stay safe | **HITL Automation Proposals** (planned — → ADR-133) | Auto-compiled automations enter the existing HITL queue; on approval an agent YAML with schedule is written; pattern thresholds and per-user caps prevent automation spam |

## Key Technology Decisions

1. **go-bt library** (`github.com/rvitorper/go-bt`) — mature Go behavior tree implementation; `Run(ctx)` not `Execute` (serves Q1: proven BT semantics; conventions in [§2](02-constraints.md)).
2. **SerializableNode** — JSON-serializable intermediate representation between YAML definitions and go-bt runtime trees (serves Q2: mutation operates on the IR; [§5](05-building-blocks.md)).
3. **Blackboard pattern** — shared state object passed through tree ticks; carries Task, Plan, Result, Outcome, ChainState, ChainTools, Reflections, TreeStore (serves Q1; contract in [§2](02-constraints.md) Conventions).
4. **ChainAction as BT node** — LLM calls are first-class behavior tree nodes, enabling PreGate gating, retry wrapping, and StrategyRouter selection (serves Q1/Q3; → ADR-006).
5. **Single GOAP planner** — the `internal/goap` A* planner is the single search implementation (→ ADR-133 Phase 6): engine planning actions and `engine.PlannerNode` delegate to it, and plan→tree compilation reuses it (mechanics in the Plan→BT Compiler row above) — serves Q4/Q1: one planner, no divergent plan semantics.
6. **Reuse over rebuild** — the personalization roadmap activates existing idle assets (`goap.Agent`, `goap.GoalQueue`, `BlackboardBridge`, `HumanApprovalGate`, `ExperienceBank`) instead of introducing parallel systems (serves Q4 under the [§2](02-constraints.md) single-developer constraint).
7. **Per-user workspaces** — all personalization state is kept per user with ADR-003 atomic file writes; no databases (workspace layout in the Persona Layer row above; serves Q4; → ADR-003).

---

*Generated by bt-agent arc42 pipeline — section4SolutionStrategy tree*
