# 1. Introduction and Goals

## 1.1 Requirements Overview

go-bt-evolve is a Go behavior-tree agent platform: it runs AI agent workflows
as evolvable, git-versioned behavior trees, exposes them to LLM operators
through MCP servers, and improves both the trees and its own codebase
autonomously over time.

**Business goals**

- Automate recurring development and operations work (code review, CI,
  research, documentation sync) as auditable, git-versioned behavior trees
  rather than ad-hoc scripts.
- Make the platform self-improving: an unattended loop researches, plans,
  implements, verifies, and lands its own changes.
- Operate at near-zero cost: local LLM first (Ollama), cheap escalation
  (DeepSeek), and quota economies for metered services (NotebookLM).

**Essential capabilities** (detailed inventories live in [§5 Building Block View](05-building-blocks.md)):

- **BT Execution Engine** — builds and executes behavior trees from 35 node
  types (composites, leaves, decorators, planning) backed by ~570 registered
  engine actions/conditions.
- **Tree Catalog** — ~75 built-in trees: domain (38), finance (10),
  research (2), startup roles (6), thinktank (3), plus kanban, evolution,
  composed-block, and core trees.
- **Autonomous Self-Improvement Loop** — the scheduled goap-fusion daemon
  researches, plans, implements, verifies, lands, and syncs this
  documentation — unattended.
- **Research Memory** — a content-hash-deduplicating knowledge store
  (`~/.go-bt-evolve/research/knowledge.json`) records every finding, NotebookLM
  answer, and implemented goal; a program store (`programs.json`) persists
  multi-cycle change programs executed one milestone per cycle.
- **3 MCP Servers** — bt-agent (79 tools), bt-evaluator (5 tools),
  bt-langagent (3 tools), all via JSON-RPC 2.0 over stdio.
- **Evolution Engine** — six evolution algorithms: Stockfish-adapted mutation
  ordering, Pareto multi-objective front, MAP-Elites quality diversity,
  Island Model with migration, Q-Learning epsilon-greedy, and Expert
  Knowledge.
- **Agent Platform & Observability** — YAML-defined agents with registry,
  scheduler, circuit breakers, dead letter queue, A2A (Agent-to-Agent)
  protocol, memory store, and webhook publishing; dashboard on :9800 with
  8 tabs (Overview, ThinkTank, Company, Tasks, Tree View, Evolution, Agents,
  MindMap).
- **Knowledge Graph & Factory** — semantic index of all trees with embeddings,
  capabilities, and cross-tree relationships for discovery and auto-creation;
  two factory layers: `internal/factory` compiles SKILL.md files into
  executable trees, `knowledge.Factory` breeds trees from parent templates
  and archetypes.

### 1.1a Target Vision — Personalized Self-Evolving Agents (Roadmap)

The platform is evolving from a *pre-authored tree catalog with mutation-based
evolution* into a system where a **personalized agent** grows alongside its user
(see [the personalization plan](../plans/2026-07-08-personalized-self-evolving-agents.md)):

- **Persona layer** — per-user profile, preferences, interaction log, and habit
  mining under `~/.go-bt-evolve/users/<user>/`.
- **Goal Factory** — user intent and mined recurring patterns become first-class
  GOAP goals in a persistent per-user goal queue.
- **Tree Factory v2** — GOAP plans are compiled into persistent, validated,
  evolvable behavior trees (plan→BT compiler); crossover uses real parent tree
  structures.
- **Automatic GOAP-BT creation** — while collaborating with the user, the agent
  detects repeatedly successful plans and proposes compiled automations through
  HITL approval, then schedules them as YAML agents.
- **Self-evolution from user signal** — user feedback becomes a fitness dimension;
  per-user gardener registries and experience banks evolve personal trees under
  the existing quality-gate/rollback safety rails.

Closing loop: `observe → goal → plan → tree → run → reflect → evolve`.

## 1.2 Quality Goals

| # | Quality Goal | Motivation |
|---|---|---|
| Q1 | **Correctness** | Trees must route correctly through PreGate→StrategyRouter→OutcomeSelector. All registered engine actions/conditions (§1.1) must register and invoke properly. ChainAction nodes must produce valid LLM output. |
| Q2 | **Evolvability** | The platform must improve over time. The six evolution algorithms (§1.1) drive mutation and selection. Git-versioned trees enable rollback. Benchmarks gate acceptance. |
| Q3 | **Reliability** | Panic recovery (SafeGo), circuit breakers (3-state), retry with exponential backoff (full jitter), dead letter queue, and output quality validation ensure the platform degrades gracefully rather than failing silently. |
| Q4 | **Personalization & Self-Growth** | The agent must adapt to its user: observe interactions, derive goals, generate its own GOAP behavior trees, and improve them from user feedback. Every generated tree must be executable (resolver-visible), validated, and evolvable. New automations require HITL approval. |
| Q5 | **Consistency & Reuse** | One canonical implementation per concept: no duplicated Go functionality across packages and daemons (shared concerns like outcome classification, retry, persistence have exactly one owner package), no semantically duplicate trees in the catalog, no near-copy actions in the registry. New features must fit the framework, not a single tree — capabilities land as engine actions, decorators, or composed blocks registered in the knowledge graph so any tree can reuse them, and project conventions apply uniformly across engine, daemons, and MCP tools. Detected duplication seeds a consolidation program. |

Detailed quality scenarios refining these goals live in
[§10 Quality Requirements](10-quality.md); the solution approaches that
achieve them are mapped in [§4 Solution Strategy](04-solution-strategy.md).

## 1.3 Stakeholders

| Role | Contact | Expectations |
|---|---|---|
| Platform Architect | Nico | Fast iteration, BT-first execution, reliable cron automation |
| Personalization Consumer | End User (persona owner) | Agent that learns their habits, proposes automations, respects approval thresholds, improves from their feedback |
| Primary Operator | Hermes Agent | MCP tools for task delegation, tree discovery, agent management |
| Observability Consumers | Dashboard Users | Tree status, fitness scores, agent history, sprint progress |
| Scheduled Automation | Cron Watchers | Reliable recurring execution with circuit breakers and DLQ |
| Local LLM Provider | Ollama (qwen3.6:35b) at :11434 | Prompt→completion, 2-3 min per call |
| Escalation LLM Provider | DeepSeek API (api.deepseek.com) | Batch/complex prompts, 5-10s per call |

---

*Generated by bt-agent arc42 pipeline — section1IntroGoals tree*
