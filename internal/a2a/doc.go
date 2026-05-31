// Package a2a integrates the Agent-to-Agent (A2A) protocol into the BT platform.
//
// It provides:
//   - card.go — auto-generates A2A Agent Cards from BT agent definitions
//   - server.go — A2A server wrapping the agent registry, executing BT trees
//   - client.go — A2A client for BT trees to delegate to external agents
//   - task_bridge.go — maps A2A task lifecycle to BT Blackboard outcomes
//
// The A2A server runs alongside the existing MCP stdio server on a configurable
// HTTP port (default: 8686). Every registered BT agent is exposed as an A2A endpoint
// reachable at /.well-known/agent-card.json.
package a2a
