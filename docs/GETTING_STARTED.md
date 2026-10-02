# Go BT Platform — Getting Started

The Go BT Platform is a behavior-tree-driven agent framework with domain trees, MCP tools, a web dashboard, and continuous self-evolution.

## Quickstart

### 1. Prerequisites
- Go 1.26.5+
- Ollama (optional, for LLM-powered agents)

### 2. Install
```bash
git clone https://github.com/nico/go-bt-evolve.git
cd go-bt-evolve
```

### 3. Run tests
```bash
BT_SKIP_LLM_TESTS=1 go test -short -count=1 ./... # Excludes live LLM tests
```

### 4. Start the dashboard
```bash
go build -o bin/bt-dashboard ./cmd/bt-dashboard/
export BT_API_KEY="$(openssl rand -hex 32)"
./bin/bt-dashboard &
# Keep this shell open for authenticated API examples.
# Open http://localhost:9800
```

### 5. Run your first task
```bash
# Via MCP (if registered with Hermes Agent):
# bt_run_task "Review this Go code for bugs"
```

## Architecture

```
cmd/
  bt-agent/       MCP stdio server (framework tools)
  bt-evaluator/   Stockfish-style tree evaluator
  bt-langagent/   LangChain ReAct agent
  bt-dashboard/   Web UI on :9800
  bt-gardener/    24/7 tree evolution daemon

internal/
  engine/         BT builder, registered nodes, RunTask
  evolution/      Mutation, GA, Q-learning, Stockfish
  agent/          Agent SDK, registry, scheduler, history
  workflow/       Multi-agent orchestration
  api/            JSON Schema, type contracts, versioning
  config/         Env-based config with validation
  security/       Rate limiting, input sanitization
  dashboard/      HTTP services and metrics export
  reliability/    Circuit breaker, backoff, worker pool
  benchmark/      BFCL, SWE-bench, τ-bench, ToolBench, BTPG
  ...see docs/arc42/05-building-blocks.md for the full inventory
```

## Key Concepts

- **Behavior Trees**: Composable decision trees (Sequence, Selector, Condition, Action)
- **ChainAction**: LLM-powered agent nodes (agent, refine, rag_query, tool_call, etc.)
- **Trees**: Registered domain-specific trees (finance, research, startup, thinktank, etc.)
- **Evolution**: Stockfish-style mutation ordering, genetic algorithms, Q-learning
- **MCP**: Server-specific tool registrations for Hermes Agent integration

## Configuration

All settings via environment variables with sensible defaults:

```bash
BT_DASHBOARD_PORT=9800       # Dashboard port
BT_API_KEY=your-private-key  # Required for protected dashboard routes
BT_OLLAMA_MODEL=qwen3.6:35b  # LLM model
BT_FEATURE_GARDENER=true     # Enable evolution daemon
BT_RATE_LIMIT_RPS=100        # Requests per second per client
BT_SUPERPOWERS_PROVIDER=codex
BT_SUPERPOWERS_CODEX_ONLY=true
BT_SUPERPOWERS_RATE_LIMIT_FAILOVER=false
BT_SUPERPOWERS_CODEX_MODEL=auto
```

See `internal/config/config.go` for shared settings and the
[coding delegation runbook](coding-delegation.md) for coding subprocesses.
An unset API key leaves protected routes unavailable. For a key-authenticated
request from the same shell as the dashboard:

```bash
curl --fail -sS -H "X-API-Key: $BT_API_KEY" http://localhost:9800/api/summary
```

Browser login uses that key and issues a session cookie; cookie-authenticated
mutations also need a CSRF token. Keep the default loopback bind unless you
have configured the remote access boundary described in arc42 §7/§8.

## API Endpoints

| Endpoint | Description |
|---|---|
| `/` | Dashboard HTML |
| `/api/health` | Health check (public) |
| `/api/metrics` | Prometheus metrics |
| `/api/alerts` | Prometheus alert evaluation (public) |
| `/api/scalability` | Scalability component snapshot (public) |
| `/api/openapi.json` | OpenAPI 3.0 specification (public) |
| `/api/swagger` | Swagger UI (public) |
| `/api/summary` | Platform summary |
| `/api/trees` | All registered trees |
| `/api/thinktank/analyze` | Run think tank analysis |
| `/api/sprint/execute` | Execute company sprint |
| `/api/chat` | Chat with LLM agents |
| `/api/dlq` | Dead letter queue management |

## Links

- Dashboard: http://localhost:9800
- **BT Agents operator guide:** [docs/agents.md](./agents.md)
- Architecture Decision Records: `docs/arc42/09-decisions.md`
- Hands-on tutorial: `docs/TUTORIAL.md`
- Troubleshooting guide: `docs/TROUBLESHOOTING.md`
- Video walkthrough script and operator demo checklist: `docs/VIDEO_WALKTHROUGH.md`
- License: MIT
