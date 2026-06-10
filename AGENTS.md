# AGENTS.md

## Cursor Cloud specific instructions

### Product

**BT Agent Platform** — Go behavior-tree AI agent framework with MCP servers (`bt-agent`, `bt-evaluator`, `bt-langagent`) and web dashboard on port **9800**.

### Toolchain

- **Go 1.26.3** (see `go.mod`). Cloud VMs usually have `go` at `/usr/bin/go`.
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

- State files live under `~/.go-bt-evolve/` (e.g. `tasks.json`). Create the directory if task persistence errors appear on first write.

### Optional: Ollama

- LLM-backed flows and some `internal/config` runtime checks expect Ollama at `http://localhost:11434`.
- Not required for `go test -short`, `make build`, or dashboard shell + non-LLM APIs.
- Install/run per `docs/TUTORIAL.md` and `docs/runner-setup.md` when testing agents or `make tree-integration`.

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
