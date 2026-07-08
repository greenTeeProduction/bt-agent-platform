# arc42 Section 4 — Solution Strategy

## Quality Goals → Solution Approaches

| Quality Goal | Scenario | Solution Approach | Details |
|---|---|---|---|
| Q1: Correctness | Tree routes through PreGate→StrategyRouter→OutcomeSelector correctly | **Behavior Trees as Execution Model** (ADR-001) | Sequence/Selector/Action/Condition/ChainAction nodes. 175+ registered engine actions. Tree validation before execution. |
| Q1: Correctness | LLM produces valid structured output | **Output Quality Validation** | `validateOutputQuality()` checks min length (30 chars), error patterns, markdown structure, code blocks. QualityScore 0.0-1.0. |
| Q2: Evolvability | Tree improves over successive mutations | **Stockfish-Adapted Evolution** (ADR-005) | Transposition table with move ordering. Multi-dimensional fitness evaluation. Mutation ordering by predicted fitness delta. |
| Q2: Evolvability | Multiple fitness dimensions must be balanced | **Pareto Front + MAP-Elites** | MultiFitness scores across correctness, completeness, conciseness, actionability. ParetoFront tracks non-dominated solutions. MAP-Elites maintains quality diversity. |
| Q2: Evolvability | Evolution must not regress | **Git-Versioned + Benchmark Gating** (ADR-005) | Every mutation creates a git commit. Benchmarks compare before/after. Rollback on regression. |
| Q3: Reliability | Goroutine panics don't crash the process | **SafeGo + Panic Recovery** (ADR-007) | All goroutines wrapped in SafeGo. Tree-level panic recovery in RunTask(). Circuit breakers prevent cascading failures. |
| Q3: Reliability | Transient LLM errors self-heal | **Retry with Exponential Backoff** (ADR-007) | Full jitter retry: 1s→2s→4s→8s (base 500ms, max 30s). 3 retry classes: standard, LLM-specific, unknown. |
| Q3: Reliability | Exhausted retries don't lose work | **Dead Letter Queue** (ADR-007) | Persistent JSON file at `~/.go-bt-evolve/dead_letter_queue.json`. Failed tasks preserved for manual inspection/replay. |
| — | Task→tree mapping must be automatic | **Knowledge Graph + Factory** | Semantic discovery via embeddings. Tree breeding via crossover (PreGate from A × StrategyRouter from B). 7 categories with capability edges. |
| — | External tools must be accessible | **MCP Protocol Layer** (ADR-002) | JSON-RPC 2.0 over stdio. 3 servers expose 43 total tools. Hermes gateway manages lifecycle. |
| — | Agent state must survive restarts | **File-Based Persistence** (ADR-003) | Atomic writes (write .tmp → rename). YAML for agent definitions, JSON for scheduler/history/reflections. Git-friendly. |
| — | LLM must be integrated into BT nodes | **ChainAction Architecture** (ADR-006) | 10 chain types (llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action). Template variables: {{.Task}}, {{.Plan}}, {{.Result}}. |
| Q4: Personalization | Agent knows who it works with and what they do repeatedly | **Persona Layer** (`internal/persona`, planned — ADR-010) | Per-user profile + interaction log + HabitMiner (embedding clustering with keyword fallback). Workspace: `~/.go-bt-evolve/users/<user>/{trees,goals,memory,reflections,experience}`. |
| Q4: Personalization | User intent and habits become plannable goals | **Goal Factory** (planned — ADR-010) | LLM structured extraction of `goap.Goal` grounded in a world-state vocabulary registry; goal archetypes; activates the existing `goap.GoalQueue` per user. |
| Q4: Personalization | Successful plans become durable automations | **Plan→BT Compiler / Tree Factory v2** (planned — ADR-010) | `goap.CompilePlanToTree` emits precondition guards → registered actions/ChainActions → effect writes, wrapped in the standard PreGate/Reflect scaffold with a dynamic-replan fallback. Real structural crossover from parent tree JSON. |
| Q4: Personalization | Generated trees must actually run | **Dynamic Tree Resolver** (planned — ADR-010) | `domains.ResolveTreeID` fallback hook loads `tree-<id>.json` from tree store / user workspace, then `BuildAndValidate`. Generated trees auto-register in the knowledge graph. |
| Q4: Personalization | Personal trees improve from user signal | **Feedback-as-Fitness** (implemented — ADR-010 Phase 5) | `user_satisfaction` dimension in the evaluator's FitnessScore fed by `bt_feedback` (explicit 👍/👎 + correction; implicit signals planned); per-user gardener registry scan, strict per-tree evidence, compile-time seed reflections, and per-user experience banks under the existing quality-gate/rollback rails. |
| Q4: Personalization | Autonomy must stay safe | **HITL Automation Proposals** (planned — ADR-010) | Auto-compiled automations enter the existing HITL queue; on approval an agent YAML with schedule is written. Pattern thresholds and per-user caps prevent automation spam. |

## Key Technology Decisions

1. **go-bt library** (`github.com/rvitorper/go-bt`) — Mature Go behavior tree implementation. `Run(ctx)` not `Execute`.
2. **SerializableNode** — JSON-serializable intermediate representation between YAML definitions and go-bt runtime trees.
3. **Blackboard pattern** — Shared state object passed through tree ticks. Carries Task, Plan, Result, Outcome, ChainState, ChainTools, Reflections, TreeStore.
4. **ChainAction as BT node** — LLM calls are first-class behavior tree nodes, enabling PreGate gating, retry wrapping, and StrategyRouter selection.
5. **GOAP planning** — `internal/goap` A* planner is the single search implementation (ADR-010 Phase 6): it is wired via engine actions (`PlanGoapActions`, `ExecuteGoapStep`, …), backs `engine.PlannerNode.Plan` by delegation, and `goap.CompilePlanToTree` compiles plans into persistent, evolvable trees. PlannerNode's BT builder extends UtilitySelector with goal-driven action selection.
6. **Reuse over rebuild** — the personalization roadmap activates existing idle assets (`goap.Agent`, `goap.GoalQueue`, `BlackboardBridge`, `HumanApprovalGate`, `ExperienceBank`) instead of introducing parallel systems.
7. **Per-user workspaces** — all personalization state lives under `~/.go-bt-evolve/users/<user>/` with ADR-003 atomic file writes; no databases.

---

*Generated by bt-agent arc42 pipeline — section4SolutionStrategy tree*
