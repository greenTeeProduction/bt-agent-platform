# arc42 Section 12 — Glossary

| Term | Definition |
|---|---|
| **A2A** | Agent-to-Agent protocol. Enables agents to discover each other via agent cards and delegate tasks. Runs on HTTP :8686. |
| **Action** | A leaf node in a behavior tree that performs a side-effecting operation (read file, call LLM, write output). Returns Success (1) or Failure (0). |
| **ADR** | Architecture Decision Record. Documents a significant design choice with context, decision, and consequences. Immutable once accepted. |
| **Blackboard** | Shared state object passed through behavior tree ticks. Carries Task, Plan, Result, Outcome, ChainState, ChainTools, Reflections, TreeStore. |
| **BuildTree** | Converts a SerializableNode tree definition into a runnable go-bt Command tree. Validates structure before building. |
| **ChainAction** | A behavior tree leaf node that wraps an LLM call. 10 chain types available. Reads config from node Name and Metadata. |
| **Chain Type** | One of 10 LLM workflow patterns: llm_call, agent, rag_query, tool_call, structured_output, refine, map_reduce, conversation, retrieval_qa, tool_action. |
| **Circuit Breaker** | 3-state pattern (closed/open/half-open) that prevents cascading failures. Per-agent isolation via AgentCircuitBreakerStore. |
| **Condition** | A leaf node that evaluates a boolean predicate. Used in PreGate and OutcomeSelector for branching decisions. |
| **Dead Letter Queue (DLQ)** | Persistent JSON file (`dead_letter_queue.json`) that stores tasks whose retries have been exhausted. |
| **DefaultTree** | The fallback behavior tree used when no specific tree matches. Extracted from a 750-line god node into 21 paths across 7 category files. |
| **Evolution** | The process of systematically improving behavior trees through mutation, fitness evaluation, and selection. 6 algorithms available. |
| **Expert Knowledge** | Curated design patterns (6) and anti-patterns (5) that guide tree evolution. Includes TreeArchetypes for each category. |
| **Fitness Score** | Multi-dimensional evaluation of a behavior tree's performance. Dimensions include correctness, completeness, conciseness, actionability. |
| **Gardener** | The evolution orchestrator (`cmd/bt-gardener`). Runs evolution cycles: evaluate → order mutations → apply → re-evaluate → accept/rollback. |
| **GOAP** | Goal-Oriented Action Planning. PlannerNode extends UtilitySelector with goal management, world state, and available actions. |
| **Island Model** | An evolution algorithm where sub-populations evolve in isolation with periodic migration of top individuals. |
| **Knowledge Graph** | In-memory graph of all 41+ trees with capabilities, keywords, embeddings, and cross-tree relationships. Powers discovery and auto-creation. |
| **MAP-Elites** | Multi-dimensional Archive of Phenotypic Elites. Maintains a grid of high-performing individuals across behavioral dimensions for quality diversity. |
| **MCP** | Model Context Protocol. JSON-RPC 2.0 over stdio. 3 servers (bt-agent, bt-evaluator, bt-langagent) expose 43 total tools to Hermes Agent. |
| **Mutation** | A structural change to a behavior tree. 10 operators: add_before, add_after, wrap_retry, prune, swap_children, rename_node, change_type, insert_fallback, clone_subtree, delete_subtree. |
| **OutcomeSelector** | The final stage of the universal BT pattern. Checks WasSuccessful → if not, triggers SelfCorrect. |
| **Pareto Front** | Set of non-dominated solutions in multi-objective optimization. Tracks trees that are not strictly worse than any other across all fitness dimensions. |
| **PlannerNode** | A behavior tree node that extends UtilitySelector with GOAP goal management. Selects actions based on world state and goal satisfaction. |
| **PreGate** | The first stage of the universal BT pattern. Validates preconditions (input valid, tools available, graph fresh) before executing the strategy. |
| **Q-Learning** | Reinforcement learning algorithm. State→Action mapping with epsilon-greedy exploration. Used for mutation strategy selection. |
| **RetryWithBackoff** | Exponential backoff with full jitter. 3 retry classes: standard (500ms base), LLM-specific (1s base), unknown (1s base). Max 3 retries. |
| **RunTask** | Executes a behavior tree to completion. Tick loop (1000 max). Sets outcome (success/failure/partial). Validates output quality. |
| **SafeGo** | Wrapper around `go func()` that recovers panics and records them. Applied to all goroutine spawns. |
| **Selector** | A composite node that tries children in order until one succeeds. Used for StrategyRouter (primary → fallback → last resort). |
| **Sequence** | A composite node that executes children in order until one fails. Used for PreGate and ordered execution paths. |
| **SerializableNode** | JSON-serializable intermediate representation of a behavior tree. The bridge between YAML definitions and go-bt runtime trees. |
| **Stockfish Evolution** | Adaptation of Stockfish chess engine techniques: transposition table for caching, move ordering by predicted fitness delta, alpha-beta pruning. |
| **StrategyRouter** | The second stage of the universal BT pattern. A Selector that tries execution strategies in priority order (primary → fallback). |
| **Tick** | One execution pass through a behavior tree. Multi-tick decorators (Repeat) return Running (0) between ticks. Max 1000 ticks per RunTask. |
| **Transposition Table (TT)** | Cache of evaluated mutation states. Prevents re-evaluating identical tree configurations. Key component of Stockfish evolution. |
| **Tree Store** | Persistent storage for behavior tree definitions. Loads on startup, saves on mutation. Located in `~/.go-bt-evolve/`. |
| **UtilitySelector** | A Selector variant that scores children by multi-dimensional utility and picks the highest-scoring path. |
| **Vault Manager** | Checkpoint/restore system for tree evolution. Saves tree snapshots to `~/.go-bt-evolve/vault/` for rollback. |

---

*Generated by bt-agent arc42 pipeline — section12Glossary tree*
