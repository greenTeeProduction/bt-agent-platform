# Go BT Platform — Getting Started

The Go BT Platform is a behavior-tree-driven agent framework with 40+ trees, MCP tools, a web dashboard, and continuous self-evolution.

## Quickstart (5 minutes)

### 1. Prerequisites
- Go 1.23+
- Ollama (optional, for LLM-powered agents)

### 2. Install
```bash
git clone https://github.com/nico/go-bt-evolve.git
cd go-bt-evolve
```

### 3. Run tests
```bash
go test -short -count=1 ./...   # Fast tests, no LLM needed (~5s)
```

### 4. Start the dashboard
```bash
go build -o bin/bt-dashboard ./cmd/bt-dashboard/
./bin/bt-dashboard &
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
  engine/         BT builder, 175+ nodes, RunTask
  evolution/      Mutation, GA, Q-learning, Stockfish
  agent/          Agent SDK, registry, scheduler, history
  workflow/       Multi-agent orchestration
  api/            JSON Schema, type contracts, versioning
  config/         Env-based config with validation
  security/       Rate limiting, input sanitization
  metrics/        Prometheus metrics export
  reliability/    Circuit breaker, backoff, worker pool
  benchmark/      BFCL, SWE-bench, τ-bench, ToolBench, BTPG
  ...and 15 more
```

## Key Concepts

- **Behavior Trees**: Composable decision trees (Sequence, Selector, Condition, Action)
- **ChainAction**: LLM-powered agent nodes (agent, refine, rag_query, tool_call, etc.)
- **Trees**: 41 domain-specific trees (finance, research, startup, thinktank, etc.)
- **Evolution**: Stockfish-style mutation ordering, genetic algorithms, Q-learning
- **MCP**: 40 tools across 3 servers for Hermes Agent integration

## Configuration

All settings via environment variables with sensible defaults:

```bash
BT_DASHBOARD_PORT=9800       # Dashboard port
BT_API_KEY=                  # Optional API key for /api/* auth
BT_OLLAMA_MODEL=qwen3.6:35b  # LLM model
BT_FEATURE_GARDENER=true     # Enable evolution daemon
BT_RATE_LIMIT_RPS=100        # Requests per second per client
```

See `internal/config/config.go` for full list.

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
- Architecture Decision Records: `docs/adr/`
- Hands-on tutorial: `docs/TUTORIAL.md`
- Troubleshooting guide: `docs/TROUBLESHOOTING.md`
- Video walkthrough script and operator demo checklist: `docs/VIDEO_WALKTHROUGH.md`
- License: MIT
