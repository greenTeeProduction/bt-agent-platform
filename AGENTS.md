# AGENTS.md

## Current architecture and coding-agent policy

Owner policy (2026-10-01): every BT LLM role uses `gpt-6.1-sol` through the
existing Codex login, including ordinary inference, planning, reflection,
evaluation, LangChain agents and coding. `BT_LLM_SOL_ONLY=true` enforces the
model and prevents alternate-provider fallback. The host CLI is
`/home/nico/.local/bin/codex` (the old npm CLI cannot access this model).
Retained legacy adapters are for isolated tests with an explicit policy
opt-out. Do not restore DeepSeek/Ollama/Claude for ordinary inference or `auto` model
selection in deployment. Owner-approved exceptions (2026-10-01): NotebookLM generation/research, external
embeddings/session indexing, and legacy memory extraction retain their own
configured providers. Owner-approved addition (2026-10-01): benchmark evaluation uses real
Ollama inference (fast qualified local model) with Sol 6.1 fallback when slow or
unavailable. Benchmark mocks cannot establish task or promotion evidence.
Do not block these scoped integrations with `BT_LLM_SOL_ONLY`.
Ordinary BT inference and coding still require Sol with no alternate fallback.

Read `graphify-out/GRAPH_REPORT.md` before source exploration; navigate its
wiki when available and use graph queries for cross-module relationships.
The canonical architecture is `docs/arc42/README.md`. Review all twelve
sections when behavior changes; preserve decision history and connect source,
tests and remaining risks. Run `graphify update .` after code edits.

Use Codex for BT coding agents and processes. Default provider is `codex`,
`BT_SUPERPOWERS_CODEX_ONLY=true` and rate-limit failover is disabled under that
policy. Retained Claude interface names and fake-adapter tests are historical
compatibility; they do not authorize running Claude Code. The deployment
policy and precedence are in `docs/coding-delegation.md` (ADR-261).

On Nico's host use `PATH=/usr/local/go/bin:$PATH` for Go/make. Current package,
injection-hook and persistence conventions are in
`.claude/skills/project-conventions/SKILL.md`; use the Codex conventions
review described in `.claude/agents/go-conventions-reviewer.md` after engine
changes. JSONL history/audit streams are permitted; shared JSON transactions
need bounded sidecar locks and atomic replacement. No new database.

## Cursor Cloud specific instructions

### Product

**BT Agent Platform** — Go behavior-tree AI agent framework with MCP servers (`bt-agent`, `bt-evaluator`, `bt-langagent`) and web dashboard on port **9800**.

### Toolchain

- **Go 1.26.5** (see `go.mod`). Cloud VMs usually have `go` at `/usr/bin/go`.
- The **Makefile** hardcodes `GO := /usr/local/go/bin/go`. If `make` fails with “go not found”, symlink system Go (one-time on the VM):

  ```bash
  sudo mkdir -p /usr/local/go/bin
  sudo ln -sf "$(command -v go)" /usr/local/go/bin/go
  sudo ln -sf "$(command -v gofmt)" /usr/local/go/bin/gofmt
  ```

### Dependency refresh (automatic)

On VM startup, run `go mod download` from the repo root (see update script). No `package.json` or Docker compose for core dev.

### Common commands

| Goal | Command |
|------|---------|
| Lint | `make lint` or `go vet ./...` |
| Fast tests (no LLM) | `go test -short -count=1 ./...` |
| Tests + race (CI-like) | `make test` |
| Full local CI | `make ci` (long; optional `bt-dashboard` on :9800 for scalability probe) |
| Build all binaries | `make build` → `bin/` |
| Run dashboard | `go run ./cmd/bt-dashboard/` or `./bin/bt-dashboard` |

### Running `bt-dashboard`

- Default URL: `http://localhost:9800` (`BT_DASHBOARD_PORT`).
- **Session-protected routes** (`/api/trees`, `/api/summary`, `/api/tasks`, etc.) require either:
  - `X-API-Key` header matching `BT_API_KEY`, or
  - a session cookie from `POST /api/login` (browser; CSRF applies).
- **Public routes** (no key): `/api/health`, `/api/metrics`, `/api/alerts`, `/api/scalability`, `/api/openapi.json`.
- Example dev start:

  ```bash
  export BT_API_KEY=dev-local-key
  ./bin/bt-dashboard
  curl -s -H "X-API-Key: $BT_API_KEY" http://localhost:9800/api/trees | head
  ```

- State files and engine logs follow `BT_AGENT_HOME` (default `~/.go-bt-evolve/`). Shared runner reflections honor `BT_REFLECTIONS_DIR`; their legacy default is `~/.go-bt-reflections/`.

### LLM authentication

BT LLM flows use `gpt-6.1-sol` through `/home/nico/.local/bin/codex` and the
existing Codex login. Ordinary Ollama inference adapters remain only for explicit legacy tests;
external embeddings and legacy memory extraction retain their own configuration.
Starting Ollama does not authorize ordinary BT inference on Ollama. See
`docs/sol-model-policy.md`.

### MCP binaries (`bt-agent`, etc.)

- JSON-RPC over **stdio**; must stay attached to a parent (e.g. Hermes gateway). Do not daemonize with closed stdin.

### Long-running services

Use **tmux** for `bt-dashboard` and optional `ollama serve` so sessions survive disconnects:

```bash
tmux -f /exec-daemon/tmux.portal.conf new-session -d -s bt-dashboard -c /workspace -- ./bin/bt-dashboard
```

### Test caveats on fresh VMs

- Without Ollama, `internal/config` `CheckRuntime` tests may fail (Ollama marked unreachable).
- `make test` enables `-race`; some packages (e.g. `internal/tracing` concurrent tracer tests) may report races that do not fail under `go test -short` without `-race`.
- `internal/engine` `TestNewGoTestTool_*` invokes `go test` as a subprocess; ensure `go` is on `PATH` for the dashboard process and test runner.

### Pre-commit hook

Optional: `scripts/git-hooks/pre-commit` (not installed automatically).
