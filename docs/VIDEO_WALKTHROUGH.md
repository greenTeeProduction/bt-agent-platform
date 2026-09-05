# BT Platform Video Walkthrough

This document is the production-ready script and capture checklist for a 12-minute BT Platform walkthrough. It closes the documentation gap between the written quickstart/tutorial and an operator-facing visual demo.

## Goal

By the end of the walkthrough, a new operator should understand how to:

1. Verify the platform is healthy.
2. Run the short test suite and build all binaries.
3. Start and inspect the dashboard.
4. Execute a task through a behavior tree.
5. Inspect observability endpoints.
6. Understand the maturity safeguards: security, configuration, scalability, and CI/CD.

## Recording Setup

- Resolution: 1920x1080 or 2560x1440.
- Terminal font: 14–16 pt monospace.
- Browser: dashboard at `http://localhost:9800`.
- Shell prefix for every Go command on Jetson/cron-compatible environments:

```bash
export PATH=$PATH:/usr/local/go/bin
```

Before recording, ensure the repository is clean:

```bash
cd /home/nico/go-bt-evolve
git status --short
```

## Chapter 1 — Platform Health (0:00–1:00)

**Narration:**

> The BT Platform is the Go behavior-tree runtime behind Hermes Agent. It provides serialized behavior trees, MCP tools, a dashboard, evolution loops, reliability primitives, and production middleware. We start by checking the live repository and health endpoints.

**Commands:**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve
go version
git log --oneline -5
```

If the dashboard is running:

```bash
curl -s http://localhost:9800/api/health
curl -s http://localhost:9800/api/summary | jq .
```

**Expected highlight:** dashboard health returns JSON, and the latest commits show conventional maturity-sprint history.

## Chapter 2 — Build and Test Confidence (1:00–2:30)

**Narration:**

> Production readiness begins with repeatable verification. The short suite avoids slow Ollama-dependent tests while still validating packages, middleware, reliability primitives, and behavior-tree execution.

**Commands:**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve
go test -short -count=1 -timeout 60s ./...
go build ./...
```

**Expected highlight:** all packages pass. If any test is slow or flaky, pause the walkthrough and apply the troubleshooting guide before continuing.

## Chapter 3 — Dashboard Tour (2:30–4:30)

**Narration:**

> The dashboard is the browser control plane. It exposes platform health, tree inventory, task workflow, observability, OpenAPI documentation, scalability status, and security-aware API routes.

**Commands:**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve
go build -o bt-dashboard ./cmd/bt-dashboard/
```

Start the dashboard in a separate managed process if it is not already running:

```bash
/home/nico/go-bt-evolve/bt-dashboard
```

**Browser clicks:**

1. Open `http://localhost:9800`.
2. Show Overview stat cards.
3. Open Trees and MindMap.
4. Open Evolution.
5. Open `http://localhost:9800/api/swagger`.

**Expected highlight:** route catalog and OpenAPI docs align with the API reference.

## Chapter 4 — Behavior Tree Execution (4:30–6:30)

**Narration:**

> Behavior trees are the core abstraction. Tasks enter a PreGate, route through a Selector, execute domain-specific action or chain nodes, and finish through outcome validation. The MergedTree can route many task domains from one universal tree.

**Demo options:**

If Hermes MCP tools are available, call `bt_delegate_to_tree` with a Go-related task. If running outside Hermes, use the dashboard agent execution endpoint or repository tests.

**Safe local verification:**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve
go test -short -count=1 -run 'TestRunTask|TestValidateTree' ./internal/engine/ ./internal/evolution/
```

**Expected highlight:** tree validation and RunTask paths complete without requiring real Ollama in short mode.

## Chapter 5 — Observability and Diagnostics (6:30–8:00)

**Narration:**

> Production operators need live diagnostics. The platform exposes health, metrics, alerts, trace logs, config, scalability, and OpenAPI endpoints.

**Commands:**

```bash
curl -s http://localhost:9800/api/metrics | head -40
curl -s http://localhost:9800/api/alerts | jq .
curl -s http://localhost:9800/api/scalability | jq .
curl -s http://localhost:9800/api/config | jq .
curl -s 'http://localhost:9800/api/traces?limit=5' | jq .
```

**Expected highlight:** secrets are redacted in config, metrics are Prometheus-compatible, and alerts include all-clear or actionable firing rules.

## Chapter 6 — Security and Configuration (8:00–9:30)

**Narration:**

> Security is layered. The dashboard and MCP servers support API keys, sessions, bearer tokens, CSRF protection, content-type checks, sanitization, rate limiting, security headers, and request IDs. Configuration is environment and file driven, with validation and redacted runtime reporting.

**Commands:**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve
go test -short -count=1 ./internal/security/ ./internal/config/
```

**Expected highlight:** security and config tests pass quickly without external dependencies.

## Chapter 7 — Scalability and Reliability (9:30–11:00)

**Narration:**

> The platform can scale from one Jetson to multiple dashboard nodes. Reliability primitives include circuit breakers, categorized retry policies, dead-letter queues, worker pools, priority queues, remote executors, failover routing, and heartbeat-based node health.

**Commands:**

```bash
export PATH=$PATH:/usr/local/go/bin
cd /home/nico/go-bt-evolve
go test -short -count=1 ./internal/reliability/ ./internal/agent/
```

**Expected highlight:** AgentRouter and reliability tests validate local and remote execution paths without requiring a live cluster.

## Chapter 8 — Evolution Pipeline and Maturity Goal (11:00–12:00)

**Narration:**

> The maturity goal is tracked as ten dimensions. Each cron sprint improves the lowest incomplete dimension, verifies with build and tests, commits a conventional message, and updates the progress tracker. The target is exactly 100 percent across every dimension — 95 percent is explicitly not complete.

**Commands:**

```bash
sed -n '8,25p' /mnt/ssd/clawd/wiki/bt-research/goals/maturity-progress.md
sed -n '87,100p' /mnt/ssd/clawd/wiki/bt-research/goals/maturity-progress.md
```

**Expected highlight:** the tracker shows evidence for each dimension and an IN PROGRESS status until every dimension reaches 100%.

## Operator Checklist

Use this checklist after recording or when giving a live demo:

- [ ] `go test -short -count=1 -timeout 60s ./...` passes.
- [ ] `go build ./...` passes.
- [ ] Dashboard `/api/health` responds.
- [ ] `/api/openapi.json` and `/api/swagger` load.
- [ ] `/api/config` redacts secrets.
- [ ] `/api/metrics` emits Prometheus text.
- [ ] `/api/alerts` returns a structured report.
- [ ] `/api/scalability` returns worker/router/queue status.
- [ ] At least one tree validation or execution test is shown.
- [ ] The maturity tracker is shown with the 100% success threshold.

## Troubleshooting During Recording

| Symptom | Fix |
|---|---|
| `go: command not found` | Run `export PATH=$PATH:/usr/local/go/bin`. |
| Dashboard port busy | Build first, kill old `bt-dashboard` in a separate command, restart with a managed process. |
| Ollama tests hang | Use `-short`; full Ollama tests belong in nightly/self-hosted runs. |
| Empty `go build` output | Treat as success when exit code is 0. |
| API returns 401/403 | Include configured `X-API-Key` or session cookie if auth is enabled. |
| Config endpoint reveals no secrets | Correct: `Config.Sanitized()` redacts sensitive values. |

## Maintenance

Update this walkthrough whenever one of these changes:

- Dashboard navigation or endpoint names.
- Required Go version or command prefixes.
- Authentication defaults.
- Maturity tracker schema.
- New production-readiness dimension evidence.
