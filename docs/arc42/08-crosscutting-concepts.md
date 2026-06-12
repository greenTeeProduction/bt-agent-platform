# arc42 Section 8 — Crosscutting Concepts

## 8.1 Behavior Tree Execution Model

**What:** All agent logic is expressed as behavior trees — hierarchical structures of Sequence, Selector, Action, Condition, ChainAction, and decorator nodes. ADR-001.

**Why:** BT nodes are composable, testable, and evolvable. The tick-based execution model supports multi-step planning with interleaved LLM calls. StrategyRouter + OutcomeSelector provide structured fallback.

**Where:** `internal/engine/tree.go` (BuildTree, RunTask, Blackboard), `internal/engine/registry.go` (action/condition registration), `internal/domains/` (tree definitions).

**Effect:** Every task flows through PreGate→StrategyRouter→OutcomeSelector. New capabilities are added by registering actions — no control flow changes needed.

## 8.2 ChainAction Nodes

**What:** LLM calls wrapped as behavior tree leaf nodes. 10 chain types (`llm_call`, `agent`, `rag_query`, `tool_call`, `structured_output`, `refine`, `map_reduce`, `conversation`, `retrieval_qa`, `tool_action`). ADR-006.

**Why:** Integrating LLM calls as BT nodes enables PreGate gating (don't call LLM if preconditions fail), retry wrapping, and StrategyRouter selection (try primary prompt, fall back to alternate prompt).

**Where:** `internal/engine/chains.go`. Config read from node `Name` (format: `chain_type:prompt_text`) and `Metadata` (max_tokens, temperature).

**Template variables:** `{{.Task}}`, `{{.Plan}}`, `{{.Result}}`, `{{.CachedResult}}`, `{{.ChainState.*}}`, `{{.ChainMemory}}`, `{{.ChainTools}}`, `{{.KgResults}}`.

**Effect:** LLM integration is a first-class BT concept, not a side-effect. Chains can be retried, gated, and selected just like any other action.

## 8.3 MCP Protocol Layer

**What:** All tools are exposed via JSON-RPC 2.0 over stdio. 3 MCP servers: bt-agent (36 tools), bt-evaluator (5 tools), bt-langagent (2 tools). ADR-002.

**Why:** MCP provides a standardized interface between Hermes Agent and the Go BT platform. No custom protocols, no REST overhead. Stdio transport keeps it simple and gateway-managed.

**Where:** `internal/mcp/` (server implementation), `cmd/bt-agent/tools.go` (tool registration), `cmd/bt-agent/main.go` (server setup).

**Effect:** Hermes Agent sees 43 MCP tools. Adding a tool is a single `server.RegisterTool()` call. Gateway handles lifecycle (spawn, restart, health check).

## 8.4 File-Based Persistence

**What:** All state stored as JSON/YAML files with atomic writes (write .tmp → rename). No SQL database. ADR-003.

**Why:** Git-friendly (diffs are readable), no database dependency, single-file atomicity prevents corruption. Simpler than SQL for a single-machine platform.

**Where:** `~/.go-bt-evolve/` directory tree. Agent YAMLs, scheduler JSON, history JSON, reflection records, DLQ JSON, tree store JSON.

**Effect:** State survives restarts. Git can version agent definitions. Manual inspection and repair is possible with any text editor.

## 8.5 Evolution Pipeline

**What:** Common pattern for tree improvement: evaluate → order mutations → apply top mutation → re-evaluate → compare fitness → accept (commit) or rollback.

**Why:** Multiple algorithms (Stockfish, Pareto, MAP-Elites, Island, Q-Learning, Expert) share this pattern. A unified pipeline reduces duplication and ensures consistent safety checks.

**Where:** `internal/evolution/` — each algorithm file, `internal/gardener/` (evolution_v2.go for cycle orchestration).

**Effect:** Evolution is auditable (git commits), reversible (rollback), and measurable (fitness delta tracking).

## 8.6 Error Resiliency

**What:** SafeGo (panic recovery) + CircuitBreaker (3-state) + RetryWithBackoff (full jitter, 3 classes) + DeadLetterQueue (persistent JSON). ADR-007.

**Why:** LLM calls can fail (Ollama OOM, DeepSeek rate limits, network timeouts). Goroutines must not crash the process. Failed work must not be silently lost.

**Where:** `internal/reliability/` — SafeGo, CircuitBreaker, RetryPolicy, DeadLetterQueue. Applied in scheduler runner (`main.go:276`), ChainAction execution, and all goroutine spawns.

**Effect:** The platform degrades gracefully. A single LLM failure doesn't cascade. Failed tasks are preserved for inspection.

## 8.7 Quality Gates

**What:** Output validation before declaring success. Minimum length, error pattern detection, structure scoring (markdown, bullets, code blocks). QualityScore 0.0-1.0.

**Why:** LLMs sometimes produce truncated/garbage output (e.g., max_tokens=10 producing a few words). Without validation, agents report "success" with useless output.

**Where:** `internal/engine/tree.go:validateOutputQuality()`. Applied after every RunTask() and in ReflectOnOutcome action.

**Effect:** Structured zero-LLM output (alert_router, agent_monitor) scores correctly. Garbage output is flagged. Quality scores feed into fitness evaluation.

## 8.8 Tool Protocol

**What:** ChainAction nodes use tool stubs (Name/Description/Call) populated at PreGate. Tools inject file I/O, shell execution, and codebase inspection capabilities into LLM chains.

**Why:** LLMs need access to real tools (read files, run commands, query the graph) during chain execution. Tool stubs provide a uniform interface.

**Where:** `internal/engine/tools_real.go` (real implementations), `internal/engine/tree.go:toolStub` (lightweight wrapper). Setup actions: `SetupDefaultTools`, `SetupDevTools`, `SetupResearchTools`.

**Effect:** ChainAction prompts can reference `{{.ChainTools}}` and the LLM can reason about available capabilities.

---

*Generated by bt-agent arc42 pipeline — section8Concepts tree*
### 8.9 Composable blocks and expand-at-build

Block IDs (`core:plan`, `core:tools_dev`, …) are registered in `internal/blocks` and referenced as `SubTreeRef` nodes. The engine expands them before `buildNode` so reliability decorators (Timeout, CircuitBreaker) and actions execute correctly. HITL blocks integrate with `internal/hitl` and dashboard approve/reject APIs.


