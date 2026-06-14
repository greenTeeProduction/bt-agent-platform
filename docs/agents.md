# BT Agents — Operator Guide

This guide explains how **YAML-defined agents** work in the BT Agent Platform: installation, creation, execution, scheduling, memory, workflows, and known operational footguns.

For architecture decisions, see [ADR-004: YAML-Defined Agent Platform](./adr/ADR-004-agent-platform.md). For MCP tool schemas, see [API Reference](./API_REFERENCE.md).

---

## Mental model

An **agent** is not separate runtime code. It is a **named, schedulable binding to a behavior tree (BT)**:

```
Agent YAML  →  Registry  →  Scheduler (optional)  →  resolveTree()  →  BuildTree()  →  RunTask()
```

- **Capability** lives in Go-defined behavior trees (`internal/domains/`, `internal/evolution/`, etc.).
- **Lifecycle** (name, schedule, metadata, memory, history) lives in YAML under the agent home directory.
- **Execution** always ends in `engine.RunTask()` regardless of entry point.

---

## Three ways to run “agents”

The repo contains three related but distinct models. Do not mix them up.

| Model | Entry | Stateful? | Use when |
|-------|-------|-----------|----------|
| **YAML + BT** | `bt-agent` MCP, scheduler, A2A `:8686` | Registry, memory, history, DLQ | Cron ops, domain tasks, MCP automation |
| **LangChain ReAct** | `bt-langagent`, `bt-gardener` | ReAct session | Conversational control of trees / evolution |
| **DoorMate REST** | Dashboard `POST /api/doormate/intent` | Profile + sessions | Page-first UI assistant (not a BT agent) |

The shipped template `agents/templates/doormate.yaml` references `tree: domain:doormate`, but **no behavior tree exists for that ID**. DoorMate runs via the REST API in `internal/doormate/`, not through `bt_agent_run`.

---

## Agent home directory

All runtime agent state lives under a single root. Default:

| OS | Path |
|----|------|
| Linux / macOS | `~/.go-bt-evolve/` |
| Windows | `%USERPROFILE%\.go-bt-evolve\` |

Override with environment variable `BT_AGENT_HOME` (preferred) or legacy `BT_HOME`.

### Layout

```
${AGENT_HOME}/
├── agents/                    # Installed agent definitions (registry)
│   ├── <name>.yaml
│   ├── templates/             # Copy of shipped templates (optional)
│   └── workflows/             # Multi-step pipeline YAML (optional)
├── memory/<agent>/memory.json # Per-agent persistent memory
├── history/<agent>.jsonl      # Run history
├── jobs/scheduler-jobs.json     # Scheduler state (InFlight, next run)
├── dead_letter_queue.json       # Failed scheduled runs after retries
└── slo/                       # SLO evidence for evolution validation gate
```

Shipped templates and workflows live in the **repository** at `agents/templates/` and `agents/workflows/`. They are **not** copied automatically — install them into `${AGENT_HOME}` (see [First-time setup](#first-time-setup)).

> **Path warning:** Some older code paths use `~/go-bt-evolve/` (no leading dot) or `$HOME/templates`. The canonical location is **`~/.go-bt-evolve/`**. Always prefer the dotted path when copying files or debugging “template not found” errors.

---

## Agent YAML schema

Each installed agent is `${AGENT_HOME}/agents/<name>.yaml`:

```yaml
name: code-reviewer
description: Reviews code for bugs, security vulnerabilities, and style issues
version: "1.0.0"
tree: domain:code_review          # Required — namespaced BT ID
schedule: on_demand               # Cron, "every 1h", or on_demand
inputs:                           # Validated on run when defined (see fields table)
  - name: code
    type: text
    required: true
outputs:                          # Enforced on run when defined (see fields table)
  - name: report
    type: markdown
quality:                          # Enforced on scheduler + bt_agent_run when present
  min_length: 100
  required_sections: ["Bugs", "Security", "Style"]
metadata:
  category: software-development
  tags: "code-review,bugs,security"
```

### Fields that matter today

| Field | Enforced? | Notes |
|-------|-----------|-------|
| `name` | Yes | File name and registry key |
| `tree` | Yes | Must resolve via `resolveTree()` in `bt-agent` |
| `schedule` | Yes | Non-`on_demand` schedules auto-register on `bt-agent` startup |
| `description` | Yes | Used as **task text** for scheduled runs |
| `inputs` | **Partial** | Validated on **scheduler**, **`bt_agent_run`**, **`bt-agent-cli run`**, and dashboard pipelines when `inputs` is set |
| `outputs` | **Partial** | Type contract enforced when defined: non-empty output; `json` type must parse (including fenced JSON) |
| `quality` | **Partial** | Enforced on scheduler, `bt_agent_run`, CLI run, and dashboard pipelines when `quality` is set |

---

## Tree IDs (capability binding)

The `tree` field selects which behavior tree runs. Common prefixes:

| Prefix | Example | Resolver |
|--------|---------|----------|
| `domain:` | `domain:code_review`, `domain:agent_monitor` | `domains.AllDomainTrees()` |
| `finance:` | `finance:pitch_agent` | `evolution.AllFinanceTrees()` |
| `research:` | `research:deep_research` | `evolution.ResearchTrees()` |
| `startup:` | `startup:ceo` | `startup.StartupTrees()` |
| `thinktank:` | `thinktank:synthesis` | Built-in switch |
| `composed:` | `composed:task:hitl` | Block composition |
| Bare ID | `godev`, `notebooklm-bridge` | Special cases in `resolveTree()` |

Full tree catalog: see README **Agent Categories** and `internal/domains/trees.go`.

---

## First-time setup

### Linux / macOS

```bash
AGENT_HOME="${BT_AGENT_HOME:-$HOME/.go-bt-evolve}"
mkdir -p "$AGENT_HOME/agents" "$AGENT_HOME/agents/templates" "$AGENT_HOME/agents/workflows"
mkdir -p "$AGENT_HOME/memory" "$AGENT_HOME/history" "$AGENT_HOME/jobs"

# Copy shipped templates and workflows from the repo
cp agents/templates/*.yaml "$AGENT_HOME/agents/templates/"
cp agents/workflows/*.yaml   "$AGENT_HOME/agents/workflows/"

# Install agents into the registry (example)
cp "$AGENT_HOME/agents/templates/system-monitor.yaml" "$AGENT_HOME/agents/"
cp "$AGENT_HOME/agents/templates/code-reviewer.yaml"  "$AGENT_HOME/agents/"
```

### Windows (PowerShell)

```powershell
$AgentHome = if ($env:BT_AGENT_HOME) { $env:BT_AGENT_HOME } else { Join-Path $env:USERPROFILE ".go-bt-evolve" }
@("agents", "agents\templates", "agents\workflows", "memory", "history", "jobs") | ForEach-Object {
    New-Item -ItemType Directory -Force -Path (Join-Path $AgentHome $_) | Out-Null
}

# From repo root
Copy-Item -Force agents\templates\*.yaml (Join-Path $AgentHome "agents\templates\")
Copy-Item -Force agents\workflows\*.yaml   (Join-Path $AgentHome "agents\workflows\")

Copy-Item (Join-Path $AgentHome "agents\templates\system-monitor.yaml") (Join-Path $AgentHome "agents\")
Copy-Item (Join-Path $AgentHome "agents\templates\code-reviewer.yaml")  (Join-Path $AgentHome "agents\")
```

### Start the runtime

```bash
# Terminal 1 — MCP server + scheduler + A2A (port 8686)
go run ./cmd/bt-agent/

# Terminal 2 — optional dashboard UI (:9800)
go run ./cmd/bt-dashboard/
```

On startup, `bt-agent` auto-schedules every registry agent whose `schedule` is not `on_demand`.

---

## Creating agents

### Option A — Copy a template into the registry

```bash
cp ~/.go-bt-evolve/agents/templates/code-reviewer.yaml ~/.go-bt-evolve/agents/code-reviewer.yaml
# Restart bt-agent or wait for next registry reload (create via MCP is immediate)
```

### Option B — MCP `bt_agent_create` (custom)

```json
{
  "name": "my-reviewer",
  "description": "Review PRs nightly",
  "tree": "domain:code_review",
  "schedule": "0 2 * * *"
}
```

### Option C — MCP `bt_agent_create` (from template)

```json
{ "from_template": "code-reviewer" }
```

> Templates and registry paths use **`~/.go-bt-evolve/`** (or `BT_AGENT_HOME`). Install shipped templates with `go run ./cmd/bt-agent-cli/ install-templates`.

### Option D — `bt-agent-cli`

```bash
go run ./cmd/bt-agent-cli/ install-templates   # copy repo agents/templates + workflows to ~/.go-bt-evolve/
go run ./cmd/bt-agent-cli/ create --from-template code-reviewer
go run ./cmd/bt-agent-cli/ list
go run ./cmd/bt-agent-cli/ schedule code-reviewer --every "0 2 * * *"
go run ./cmd/bt-agent-cli/ run code-reviewer --input "Review PR #42"
```

Registry and templates use `~/.go-bt-evolve/` via `internal/agent/paths.go`.

---

## Running agents

### Recommended: scheduled execution (`bt-agent` scheduler)

**Best for production.** Scheduled runs get the full stack:

- Memory + previous-run context injection
- Retry with jitter
- Per-agent circuit breaker
- SLO evidence recording
- DLQ on exhausted retries
- History + quality scoring

Ensure the agent YAML is in `${AGENT_HOME}/agents/` with a non-`on_demand` schedule, and `bt-agent` is running.

### On-demand: MCP `bt_agent_run`

```json
{ "agent": "code-reviewer", "task": "Review this function for race conditions: ..." }
```

Resolves registry → tree → `RunTask`. Records history. **Does not** inject memory or previous-run context (unlike the scheduler).

### Direct tree execution (no agent YAML)

```json
{ "tree": "domain:code_review", "task": "..." }
```

Use `bt_run_task` or `bt_delegate_to_tree` when you do not need registry/history.

### A2A (port 8686)

External clients send tasks with **agent name as context ID**. Runs in-process with direct `RunTask` — no memory injection, no retries.

Env: `BT_A2A_PORT` (default `8686`), `BT_A2A_BASE_URL`.

### Dashboard “Run agent” / pipelines

Dashboard **Run agent**, **Execute**, and **Sprint** handlers use the same in-process `RunOnce()` path when the dashboard starts with a configured LLM. Pipelines and single-agent runs share registry-first resolution plus workflow aliases (`ResolvePipelineAgent`).

Hermes CLI is only used if the in-process runner failed to initialize at startup.

### `bt-agent-cli run`

Executes an agent in-process (no MCP server required):

```bash
go run ./cmd/bt-agent-cli/ run code-reviewer --input "Review main.go for bugs"
go run ./cmd/bt-agent-cli/ run my-agent --param topic=AI --json
```

Named YAML `inputs` are validated; use `--param key=value` for each required field.

---

## Execution path comparison

| Feature | Scheduler | `bt_agent_run` | `bt-agent-cli run` | Dashboard pipeline | A2A |
|---------|:---------:|:--------------:|:------------------:|:------------------:|:---:|
| Registry lookup | ✓ | ✓ | ✓ | ✓ | ✓ |
| Memory injection | ✓ | ✓ | ✓ | ✓ | ✗ |
| Previous run context | ✓ | ✓ | ✓ | ✓ | ✗ |
| Input validation | ✓ | ✓ | ✓ | ✓ | ✗ |
| Quality enforcement | ✓ | ✓ | ✓ | ✓ | ✗ |
| Retries + DLQ | ✓ | ✗ | ✗ | ✗ | ✗ |
| Circuit breaker | ✓ | ✗ | ✗ | ✗ | ✗ |
| Structured outcome | ✓ | ✓ | ✓ | ✓ | ✓ |

---

## Scheduling

### Schedule syntax

| Form | Example | Meaning |
|------|---------|---------|
| `on_demand` | default | Manual / MCP only |
| `every <duration>` | `every 1h`, `every 30m` | Fixed interval |
| 5-field cron | `0 9 * * *`, `*/5 * * * *` | Standard cron |

### MCP `bt_agent_schedule`

```json
{ "agent": "system-monitor", "schedule": "*/5 * * * *", "timeout": "30m" }
```

### CLI `bt-agent-cli schedule`

```bash
go run ./cmd/bt-agent-cli/ schedule system-monitor --every "*/5 * * * *" --timeout 30m
```

Updates registry YAML and `scheduler-jobs.json`. A running `bt-agent` picks up changes on the next scheduler tick (~1 minute).

### Scheduler behavior

- YAML registry is **source of truth** — `ReconcileWithRegistry()` on startup and each tick sync remove stale jobs and create missing ones.
- **InFlight** flag persisted before each run — survives `bt-agent` crashes.
- **Circuit breaker** opens after consecutive failures; skips runs until cooldown.
- Failed runs after retries → **dead letter queue** at `${AGENT_HOME}/dead_letter_queue.json`.

Check circuit state: MCP `bt_circuit_status`.

---

## Memory and history

### Memory (`${AGENT_HOME}/memory/<agent>/memory.json`)

Categories: `fact`, `pattern`, `pitfall`, `preference`, `state`. Priorities: `high`, `medium`, `low`.

MCP tools:

- `bt_agent_memory_write` — store a key/value
- `bt_agent_memory_read` — read key or full context block
- `bt_agent_memory_delete`

Scheduled runs auto-inject memory and history into the **run blackboard** (keys `memory/*`, `history/runs`) with a short prompt hint to use `bb_read`. Legacy prompt stuffing applies only when blackboard is disabled (`DisableBlackboard: true`).

### History (`${AGENT_HOME}/history/<agent>.jsonl`)

Each run records outcome, duration, output, quality score. MCP `bt_agent_history` returns runs + aggregate stats (success rate, avg quality).

---

## Blackboard (context offloading)

Scoped key-value store for moving large context off the prompt. See [ADR-009](./adr/ADR-009-blackboard-context-offloading.md).

| Scope | ID | Lifetime | On disk |
|-------|-----|----------|---------|
| `run` | per agent execution | ends when run completes | no |
| `session` | pipeline `run_id` | workflow execution | `${AGENT_HOME}/blackboard/session/<id>.json` |
| `agent` | agent name | until deleted | `${AGENT_HOME}/blackboard/agent/<name>.json` |

### ReAct tools (in `agent:` chains)

Run scope: `bb_read`, `bb_write`, `bb_list`.  
Pipeline session (when `SessionID` set): `bb_session_read`, `bb_session_write`, `bb_session_list`.

Workflow steps auto-populate session keys: `input`, `steps/<step_id>/output`, `prev/output`.

### Chain templates

- `{{.BB.run_id}}`, `{{.BB.session_id}}`, `{{.BB.agent}}`
- `{{.BB.<key>}}` — run-scope value (summary when large); falls back to session scope
- `{{.RunID}}` — alias for run id

### MCP tools

| Tool | Purpose |
|------|---------|
| `bt_bb_read` | Read entry (`scope`, `scope_id`, `key`) |
| `bt_bb_write` | Write entry |
| `bt_bb_list` | List keys (optional `prefix`, `limit`) |
| `bt_bb_delete` | Delete key |

Scopes: `run`, `session`, `agent`.

Successful agent runs auto-promote to agent scope: `runs/latest/output`, `runs/latest/task`, `runs/latest/run_id`, `runs/latest/at`.

### Dashboard API

`GET /api/blackboard?scope=session&scope_id=<pipeline_run_id>&prefix=steps/&limit=50` — list entries (auth required). Workflow tab shows session keys when a pipeline completes.

---

## Shipped templates (22)

Templates live in `agents/templates/`. Copy to `${AGENT_HOME}/agents/templates/` then install into `agents/`.

| Category | Templates |
|----------|-----------|
| Hermes ops | `hermes-researcher`, `hermes-monitor`, `hermes-cron-doctor`, `hermes-evolution-watcher`, `hermes-code-reviewer` |
| Dev / review | `code-reviewer`, `bt-implementer`, `maturity-sprint-agent` |
| Research | `daily-researcher`, `bt-research-agent`, `graphify-researcher-agent`, `autonomous-research-implementer` |
| Integration | `notebooklm-bridge`, `delegation-processor`, `data-pipeline`, `notification-router` |
| Memory / vault | `memory-extractor`, `session-indexer`, `skill-tree-syncer` |
| Product | `doormate` (REST only — see below), `meeting-summarizer` |
| Monitoring | `system-monitor` |

### Templates with known issues

| Template | Issue | Workaround |
|----------|-------|------------|
| `doormate` | `domain:doormate` tree does not exist | Use `/api/doormate/intent` on dashboard |

Workflow agent names resolve via registry first, then `ResolvePipelineAgent()` aliases (`notebooklm` → `notebooklm-bridge`, etc.). Install required agents before running pipelines.

---

## Multi-agent workflows (pipelines)

Workflow YAML files live in `agents/workflows/`:

| File | Steps | Notes |
|------|-------|-------|
| `code-review.yaml` | review → suggest → apply_fix | Uses `code-reviewer`, `bt-implementer` |
| `daily-research.yaml` | research → notebooklm → vault | Install `hermes-researcher`, `notebooklm-bridge`, `session-indexer` |
| `health-check.yaml` | monitor → diagnose → notify | Install `system-monitor`, `daily-researcher`, `notification-router` |
| `incident-response.yaml` | detect → diagnose → notify → fix | Install monitor, researcher, router, reviewer agents |

### Running a pipeline

1. Copy workflows to `${AGENT_HOME}/agents/workflows/` (`go run ./cmd/bt-agent-cli/ install-templates` copies them).
2. Dashboard (async):
   - **Workflows tab** in the dashboard UI — select pipeline, optional input, run, poll status, view step outcomes and HITL task IDs
   - `POST /api/pipelines/run` → `{ "run_id": "...", "status": "running" }`
   - `GET /api/pipelines/status?id=<run_id>` → poll until `status` is `complete` or `failed`
3. MCP: `bt_workflow_run` with `{ "pipeline": "daily-research", "input": "..." }` (blocking; returns `run_id` on result)

**Approval steps:** each approval step includes `hitl_task_id` and `hitl_request_id` in step results. Approve while running via `bt_workflow_approve`, `bt_hitl_approve`, or dashboard `/api/hitl/`.

Orchestrator supports `agent`, `condition`, `parallel`, `loop`, and `approval` step kinds.

- `approval` steps block on HITL when `WaitApproval` is set (dashboard/MCP pipelines use `WorkflowApprovalWait`). Approve via `bt_workflow_approve`, `bt_hitl_approve`, or dashboard `/api/hitl/`.
- `parallel` steps run with isolated step state (no shared `prev` map mutations).
- Step `timeout` in YAML is enforced per agent step (e.g. `timeout: "5m"`).
- `bt_workflow_run` runs YAML workflows (`pipeline` + `input`) or thinktank analysis (`topic`).

---

## MCP agent tools (quick reference)

| Tool | Purpose |
|------|---------|
| `bt_agent_create` | Create agent (custom or `from_template`) |
| `bt_agent_list` | List registry + stats |
| `bt_agent_run` | Run immediately |
| `bt_agent_history` | Run history + aggregates |
| `bt_agent_schedule` | Add/update scheduler job |
| `bt_agent_delete` | Remove from registry and clear scheduler jobs |
| `bt_agent_memory_write` / `_read` / `_delete` | Persistent memory |
| `bt_bb_read` / `bt_bb_write` / `bt_bb_list` / `bt_bb_delete` | Scoped blackboard (run/session/agent) |
| `bt_circuit_status` | Circuit breaker state |
| `bt_delegate_to_tree` | Run task on tree (bypass registry) |
| `bt_workflow_run` | Run YAML workflow (`pipeline` + `input`) or thinktank topic analysis |
| `bt_workflow_approve` | Approve/reject workflow HITL by `task_id` or `request_id` |

Related: `bt_blocks_*` (composable trees), `bt_hitl_*` (human approval queue).

---

## DoorMate (not a BT agent)

DoorMate is a **page-first assistant** exposed as REST on the dashboard:

| Endpoint | Purpose |
|----------|---------|
| `POST /api/doormate/intent` | Parse intent, generate page schema |
| `POST /api/doormate/bookmark` | Bookmark pages |
| `POST /api/doormate/rate` | Rate / feedback |
| `GET/POST /api/doormate/profile` | User profile |

Implementation: `internal/doormate/` (`PageAgent` + LLM). Do not install `doormate.yaml` expecting `bt_agent_run` to work.

---

## Reliability (scheduled agents)

Scheduled execution implements [ADR-007](./adr/ADR-007-reliability-architecture.md):

1. **Panic recovery** in scheduler runner — one bad agent does not stop others.
2. **Circuit breaker** — per agent; open → skip until cooldown → half-open test.
3. **Retry policy** — configurable jitter; failures recorded to SLO evidence.
4. **DLQ** — exhausted retries preserved in `dead_letter_queue.json` for inspection/replay (dashboard API when auth configured).

---

## Evolution and agents

`bt-gardener` evolves **behavior trees** in the reflection store, not agent YAML files.

- Agents keep the same `tree:` ID; evolved structure is loaded via `TreeStore` at run time.
- SLO evidence from scheduled runs feeds the gardener **validation gate**.
- There is no agent-level tree version pin in the registry today.

---

## Known limitations

Check git history for fixes after this doc was written.

1. **`outputs` in YAML** — enforced when the agent defines `outputs` and the run uses quality enforcement (`EnforceQuality`: scheduler, `bt_agent_run`, `bt-agent-cli run`, dashboard). Checks non-empty output and valid JSON when any output spec uses `type: json`. Multiple named outputs still share one runtime blob; use `quality.required_sections` for structure.
2. **Installed-only agents** appear in the dashboard Agents tab (registry at `~/.go-bt-evolve/agents/`). Templates in `agents/templates/` are install-only via CLI.
3. **`bt-agent-cli schedule`** updates registry YAML and `scheduler-jobs.json`. A running `bt-agent` reloads on the next scheduler tick (~1m) via `SyncFromRegistry`. Use MCP `bt_agent_schedule` for immediate in-process updates without waiting for the tick.
4. **DoorMate** (`rest_only: true` in template metadata) — use `/api/doormate/intent`, not `bt_agent_run`.

---

## Troubleshooting

### “no tree found for agent X”

```bash
# Verify tree ID in agent YAML
cat ~/.go-bt-evolve/agents/X.yaml | grep tree

# Common fixes:
# - Use domain: prefix (domain:code_review not code_review)
# - doormate → use REST API, not bt-agent
# - Check resolveTree() special cases in cmd/bt-agent/main.go
```

### “template not found” on create

Copy templates to **`~/.go-bt-evolve/agents/templates/`** (with dot). If MCP still fails, install by copying YAML directly into `~/.go-bt-evolve/agents/`.

### Scheduled agent never runs

1. Is `bt-agent` running? Scheduler lives inside it.
2. Is `schedule` non-`on_demand` in agent YAML?
3. Check circuit breaker: `bt_circuit_status`
4. Inspect `${AGENT_HOME}/jobs/scheduler-jobs.json`

### Agent runs but quality always low

Scheduled runs score output with `estimateQuality()` heuristics and, when the agent YAML defines `quality:`, with `ValidateQualitySpec()` (min length, required sections, blocked patterns). Compact but complete ops reports should score well. Generic LLM refusals score poorly. Failed quality gates mark the run as failure when enforced.

### Pipeline not found

Ensure workflow YAML exists under `${AGENT_HOME}/agents/workflows/`. If dashboard still fails, also try copying to `~/go-bt-evolve/agents/workflows/` (legacy path).

### LLM errors on run

MCP tools call `checkLLMHealth` and fail fast when Ollama is down. Start Ollama or set provider env vars — see [GETTING_STARTED](./GETTING_STARTED.md) and [TROUBLESHOOTING](./TROUBLESHOOTING.md).

---

## Operator checklist

- [ ] `${AGENT_HOME}` created with `agents/`, `templates/`, `workflows/`, `memory/`, `history/`, `jobs/`
- [ ] Shipped templates copied from repo
- [ ] Desired agents copied from `templates/` → `agents/` (registry)
- [ ] `bt-agent` running (scheduler + MCP)
- [ ] LLM available if agent tree uses ChainAction
- [ ] Verified tree resolves (`bt_agent_run` smoke test)
- [ ] For cron agents: checked `scheduler-jobs.json` and first history entry
- [ ] Workflows copied to `${AGENT_HOME}/agents/workflows/`; smoke-test via Workflows tab or `bt_workflow_run`
- [ ] DoorMate via REST, not YAML agent

---

## See also

- [ADR-004: YAML-Defined Agent Platform](./adr/ADR-004-agent-platform.md)
- [ADR-007: Reliability Architecture](./adr/ADR-007-reliability-architecture.md)
- [ADR-008: Composable Blocks](./adr/ADR-008-composable-blocks.md)
- [GETTING_STARTED](./GETTING_STARTED.md)
- [TROUBLESHOOTING](./TROUBLESHOOTING.md)
- [API Reference — MCP tools](./API_REFERENCE.md)
