# ADR-006: ChainAction — LLM Integration via Behavior Tree Nodes

**Status:** Accepted
**Date:** 2026-05-28
**Deciders:** Nico (via Hermes Agent)

## Context

The BT engine needed to integrate LLM capabilities without coupling the tree runtime to specific LLM libraries. Agents executing behavior trees require diverse LLM interaction patterns: single calls, multi-turn chat, tool use, structured output, and retrieval-augmented generation. Options:

1. **Embed LLM in every Action node** — tightly coupled, hard to test, duplicates logic
2. **Separate LLM service with tree callbacks** — adds network latency, complex state management
3. **ChainAction — configurable LLM integration nodes** — tree nodes with Metadata-driven chain configuration

## Decision

Implement `ChainAction` as a first-class BT node type with 10 chain types, all reading configuration from node Metadata:

| Chain Type | Purpose | LLM Calls |
|---|---|---|
| `llm_call` | Single LLM invocation with template support | 1 |
| `rag_query` | Retrieval-augmented QA using `bb.KgResults` | 1 |
| `tool_call` | Named tool invocation via LLM reasoning | 1 |
| `conversation` | Multi-turn chat with memory in `bb.ChainState` | N |
| `structured_output` | JSON output with schema constraint | 1 |
| `retrieval_qa` | Two-phase retrieve-then-answer | 2 |
| `map_reduce` | Decompose → process subtasks → combine | 1+N+1 |
| `refine` | Iterative self-improvement (2 passes) | 2 |
| `agent` | ReAct loop (Thought→Action→Observation→Final Answer) | 1-11 |
| `tool_action` | Direct tool invocation without agent overhead | 0 |

All chain types use `llm.LLM` interface for testability (mock LLM in tests, real Ollama in production). Template variables (`{{.Task}}`, `{{.Result}}`, etc.) enable cross-node data flow.

## Consequences

- **Positive**: LLM integration is testable — mock LLM enables fast CI without Ollama
- **Positive**: 10 chain types cover the full spectrum from simple calls to agentic tool loops
- **Positive**: Metadata-driven configuration — no recompilation needed to adjust prompts or parameters
- **Positive**: Blackboard primitives (`ChainMemory`, `ChainTools`, `ChainState`) enable cross-node state sharing
- **Negative**: Agent chain type on Jetson takes 20-40 min (11 Ollama calls × 2-4 min each) — limited to scheduled jobs
- **Negative**: `max_tokens` below 100 silently truncates agent output to 3-word garbage (auditing required post-evolution)
- **Negative**: Results accumulation required `bb.Results []string` for multi-agent-node trees (single `bb.Result` was overwritten)
