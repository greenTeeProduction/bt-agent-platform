# 12. Glossary

**Sol-only policy:** ordinary built-in BT inference/coding selects `gpt-6.1-sol` through Codex login. NotebookLM generation/research, external embeddings/session indexing and legacy memory extraction are explicit exceptions. **Session renewal:** validated cookie rotation that reduces expiry without guaranteeing permanent access. See [model policy](../sol-model-policy.md).

Canonical terms used across the architecture, sorted alphabetically. Keep
runtime details in §§5–8 and decision history in [§9](09-decisions.md).

| Term | Definition |
|---|---|
| **A2A** | Agent-to-Agent discovery and task exchange over HTTP. Peer trust, transport and availability depend on deployment; see [§3](03-context-scope.md). |
| **A2A task owner** | The SDK execution identified by task/context ID. Submitted/working tasks are polled by ID; HTTP completion or a polling timeout does not itself cancel that owner. Task state is process-local (ADR-268). |
| **Aborted delegation tree** | A surrounding workflow stopped after a completed child reported a persistence diagnostic. Child evidence is preserved; skipped remaining steps are not reported as completed. |
| **Action** | A behavior-tree leaf that performs work and returns an execution status. Its side effects and cancellation behavior belong to the registered implementation. |
| **ADR** | Architecture Decision Record: a dated decision with context, rationale and consequences. Historical records live in [§9](09-decisions.md); current behavior lives in §§3–8. |
| **Agent Card Signing** | Origin/integrity checks for A2A capability cards, using the configured signing-key policy. A valid signature is not evidence that a peer is currently healthy. |
| **Auction (Contract Net)** | A2A task allocation in which peers bid and an eligible winner receives an award. |
| **Award** | The decision assigning an auction task to the selected peer. |
| **Bid** | A peer's offer to execute an auction task, evaluated by the allocation policy. |
| **Blackboard** | Mutable state shared by nodes within an execution. Distinguish the engine's typed Blackboard from the scoped persistence manager in `internal/blackboard`. |
| **Build Identity** | The revision/build metadata of a running binary. Repository HEAD, a file on disk and the process executable can represent different revisions. |
| **BuildTree** | Engine conversion of serializable nodes into executable `go-bt` commands. Building and validating are distinct operations unless the caller uses a combined API. |
| **Cache publication** | Installing a staged state mutation after its persistent commit succeeds. A failed blackboard mutation/commit preserves the preceding entries, byte accounting and eviction state (ADR-271). |
| **Chain Type** | The declarative LLM/tool workflow selected by ChainAction metadata; the current kind inventory is in [§5.5](05-building-blocks.md#55-chain-types). |
| **ChainAction** | A behavior-tree node that invokes a declarative chain, such as a direct tool action, prompt or agent loop. |
| **Circuit Breaker** | A closed/open/half-open admission policy for repeated failures. A closed breaker describes scheduling health, not proof that a GOAP run landed code. |
| **Claude Backoff** | Historical name for Claude-specific durable quota state. Codex has separate state; see Provider Cooldown. |
| **CMA-ES** | Covariance Matrix Adaptation Evolution Strategy, used for numerical parameter tuning. Library/tool availability does not imply every gardener cycle invokes it. |
| **Codex** | An external coding CLI supported by the implementation/review delegation seam. Its account/model configuration is independent of node-level inference. |
| **Completed workflow prefix** | Healthy child work already performed before the surrounding workflow/container stopped. A typed partial stop prevents automatic replay of that prefix; no rollback or durable resume is implied (ADR-270). |
| **Condition** | A behavior-tree leaf that tests state and returns success/failure without selecting a new architecture policy. |
| **Crisis Detector** | Evolution component identifying stagnation/diversity symptoms that can trigger configured recovery interventions. |
| **DLQ replay claim** | Durable exact attempt identity committed before replay dispatch. It survives process exit and cannot expire or be removed by ordinary maintenance; trusted reconciliation requires quiescence and outcome evidence (ADR-280). |
| **Dead Letter Queue (DLQ)** | Persistent failed-work records retained for inspection and replay. Insertion, retryability and retention are caller/policy-specific. |
| **DefaultTree** | The platform's general fallback tree. A failed generated-tree lookup must not be confused with successful execution of the requested tree. |
| **Deferred Outcome** | An expected pause, such as provider quota carryover, recorded separately from ordinary success/failure. Scheduler behavior is defined in [§6.4](06-runtime-view.md#64-self-improvement-cycle-goap-fusion-loop). |
| **Degraded Outcome** | A completed run with reduced capability, such as analysis without eligible implementation. It may keep the breaker closed while delivering no code. |
| **Deploy Drift** | A difference between intended committed source and the revision served by a running process. Detection and automatic remediation are separate policies. |
| **Evolution** | Tree improvement through candidate generation, evaluation and selection. Different adoption paths have different gates; see [§8.5](08-crosscutting-concepts.md#85-evolution-pipeline). |
| **Evolution Lineage** | The relationship between a base tree and evolved descendants, recorded in the runtime knowledge graph. |
| **Experience Bank** | Durable mutation experiences used to bias later operator selection; distinct from the research knowledge store. |
| **Expert Knowledge** | Curated or learned structural patterns and anti-patterns that inform evaluation/evolution. |
| **Fitness Score** | An evaluation measure for a tree. Runtime-success EMA, structural fitness, benchmark score and user satisfaction are different signals, not interchangeable percentages. |
| **Gardener** | The service that observes registered trees and orchestrates configured evolution passes, validation and persistence. |
| **GOAP** | Goal-Oriented Action Planning. `internal/goap` owns the canonical planner and world-state/goal model; engine nodes adapt it to tree execution. |
| **GOAP Fusion Loop** | The scheduled research-to-code workflow that gathers goals, plans work, delegates implementation, verifies and attempts landing. Schedule is deployment configuration. |
| **Graphify Graph** | The repository analysis graph under `graphify-out/`, used to navigate source relationships. It is distinct from the runtime Knowledge Graph and may require regeneration. |
| **Grill** | Iterative critical review of research, commonly through NotebookLM. Research evidence can inform goals without authorizing code changes by itself. |
| **HITL** | Human-in-the-loop requests and gates. Policy may permit auto-approval; approval state, dashboard task approval and authenticated sessions are separate concepts. |
| **Impact Graph** | Source-to-test relationships used to select relevant validation. Impact-based selection does not replace required broader gates. |
| **Island Model** | Evolution with separate populations and migration. The live champion-adoption path still has documented gate/snapshot differences (R23). |
| **Knowledge Graph** | Runtime tree/capability/relationship and feedback registry in `internal/knowledge`; used for discovery and learning, not the Graphify source graph. |
| **Knowledge Store** | Deduplicated research findings and supporting evidence in `internal/research`; separate from tree capabilities and execution feedback. |
| **Known execution stop** | Typed evidence of non-completed owned work (failed, canceled, rejected or waiting). Automatic retry/fallback stops; a wait is deferred, not delivery. Joined admitted failure/uncertainty outranks a wait (ADR-269). |
| **MAP-Elites** | A quality-diversity archive retaining strong candidates across behavior niches. |
| **MCP** | Model Context Protocol. This platform's servers use JSON-RPC over stdio; transport authentication differs from HTTP headers/cookies. |
| **MCTS Affinity** | A heuristic score determining whether speculative structural search augments ordinary ordering for a tree. Current asymmetry is documented as R21. |
| **MCTS-Guided Mutation** | Bounded Monte Carlo Tree Search proposing structural mutations into the ordinary scored competition. It is candidate generation, not a separate acceptance exemption. |
| **Memetic Evolution** | Population evolution augmented by local search of individual candidates. |
| **MetaValidator** | Structural/safety validation applied on particular adoption paths after candidate scoring. Its existence does not imply all paths invoke it. |
| **Mutation** | An operation changing a tree's structure or metadata. Current operators are defined by implementation rather than a copied fixed count. |
| **Non-admission evidence** | A trusted peer's explicit assertion that a rejected request did not enter execution. An HTTP error status alone is insufficient. |
| **NSGA-II** | Non-dominated Sorting Genetic Algorithm II: multi-objective population selection using dominance ranking and crowding distance. |
| **OutcomeSelector** | An engine control node selecting behavior from prior outcome/state according to its configured routing rules. |
| **Pareto Front** | Candidates not dominated by another candidate across all chosen objectives. |
| **Partial Landing** | A workflow result where only eligible completed work is landed and remaining work stays explicit in artifacts; it is not completion of the full goal. |
| **Persona** | Per-user profile, interactions, habits and automation preferences. A user ID scopes state; it does not authenticate the caller. |
| **PlannerNode** | The behavior-tree adapter to GOAP planning; not a second independent A* implementation. |
| **Pre-Mutation Snapshot** | A copy of the predecessor tree used for rejection/rollback evidence. Snapshot timing matters: capturing after adoption cannot restore the previous tree. |
| **PreGate** | A composed sequence of input/prerequisite checks before a task's main work. |
| **Program / Milestone** | Durable research/improvement goals grouped into a program with tracked milestones; distinct from a transient planner path. |
| **Provider Cooldown** | Provider-specific durable quota timing that controls eligibility for later CLI attempts. Failover may try one alternate when explicitly enabled. |
| **Q-Learning** | Reinforcement-learning approach that updates action values from observed rewards, used in supported evolution/selection components. |
| **Quota Economy** | Policies and caches intended to reduce metered calls and respect provider quotas; not a guarantee of free or unlimited execution. |
| **Recover** | A configured recovery path after failure. Its exact retry/rollback effects depend on the node or caller, unlike the narrower SafeGo primitive. |
| **RetryWithBackoff** | A retry helper with configured attempts, classification and delay. Not every failure or node automatically uses it. |
| **Rooted workflow selection** | Reading a selected catalog basename through the configured directory boundary, with escaping symlinks rejected. This is filesystem confinement, not per-user authorization (ADR-272). |
| **RunTask** | The engine execution loop around a built tree and blackboard, with a cooperative context budget and tick limit. |
| **SafeGo** | Goroutine wrapper recovering/logging panics and invoking an optional callback. It does not itself guarantee retry, DLQ persistence or goroutine restart. |
| **Selector** | A control node trying alternatives until one succeeds or remains running, according to its implementation's resume policy. |
| **Selector-Ordering Telemetry** | Observed child success/cost information used to rank selector alternatives. |
| **Sequence** | A control node evaluating children in order until a child fails or remains running. |
| **SerializableNode** | The editable/persistable intermediate representation of a tree node, including children, metadata and edges. |
| **Session** | In-memory dashboard authentication state represented in the browser by an HttpOnly cookie. It expires and is invalidated by logout or process restart. |
| **Stockfish Evolution** | The project's search/evaluation approach inspired by game-engine techniques, including ordering and cached evaluation; not a chess engine dependency. |
| **StrategyRouter** | A routing point choosing an appropriate execution strategy. Skill-compiled trees use model routing with a configured executable fallback. |
| **Structural Fitness** | A tree-shape/evaluation signal kept separate from genuine-run success history. |
| **Superpowers Run** | Durable artifacts for a coding workflow, including plan, tasks, implementation and verification evidence. The historical name is provider-neutral. |
| **Sprint batch budget** | A five-minute context owned by accepted asynchronous work, including queue time. Expiry stops new dispatch and returns only proven unstarted claims; capacity stays owned until actual cleanup (ADR-276). |
| **Recovery hold** | Persisted inactive scheduler disposition requiring trusted operator reconciliation after interrupted or unrecorded execution. Restart, registry sync and ordinary scheduling do not prove side effects failed (ADR-277). |
| **Restart admission seal** | Process-local exclusion of new owned work during accepted or uncertain restart handoff; proven rejection reopens admission. Shared dashboard/gardener gates and target-owned sibling requests use it; it is not a durable recovery claim or protection from arbitrary systemd calls (ADR-278/279). |
| **Sprint metadata reconciliation** | Retrying a retained observed task result against its original in-progress owner without running the action again. Failed/conflicting writes and execution uncertainty block new admission; evidence is process-local (ADR-275). |
| **Task Approval (dashboard)** | The dashboard task workflow's execution decision. It is separate from login authentication and may be distinct from an engine HITL request. |
| **Tick** | One evaluation step of a behavior tree returning success, failure or running; a synchronous tick can contain slow work. |
| **Transposition Table (TT)** | Cache of evaluations keyed by state/tree identity, used to reuse previous search results. |
| **Tree Store** | Persisted serializable trees loaded by the appropriate registry/resolver. Global and per-user scopes must remain explicit. |
| **Uncertain execution** | A dispatched operation whose completion cannot be established. Automatic replay stops; reconcile evidence before an operator chooses another attempt (ADR-267). |
| **UtilitySelector** | A selector that ranks alternatives by configured utility/evidence before execution. |
| **Vault Manager** | Integration for managing research notes and related knowledge-vault content. |
| **Worktree** | An isolated Git checkout for a change. The main checkout is a normal non-bare repository; worktrees share Git object/history storage. |

---

*Generated by bt-agent arc42 pipeline — section12Glossary tree*
