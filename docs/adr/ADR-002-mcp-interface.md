# ADR-002: MCP as External Interface

**Status:** Accepted
**Date:** 2026-05-26
**Deciders:** Nico (via Hermes Agent)

## Context

The platform needed to integrate with Hermes Agent for autonomous task execution. Options:
1. **REST API** — simple but requires polling for long-running tasks
2. **gRPC** — fast but complex to set up with Hermes' Python runtime
3. **MCP (Model Context Protocol)** — stdio-based, JSON-RPC 2.0, designed for tool servers

## Decision

Expose all platform capabilities via MCP stdio servers. Three servers: `bt-agent` (core tools), `bt-evaluator` (evolution), `bt-langagent` (ReAct agent).

## Consequences

- **Positive**: Zero network config — stdio transport works instantly
- **Positive**: Hermes auto-discovers tools; no manual registration needed after initial setup
- **Negative**: MCP is single-client per process; multi-client needs connection pooling
- **Negative**: Stdio transport means servers must be child processes of the gateway
