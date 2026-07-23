# 7. Deployment View

Physical/operational view: one node, its processes, storage, and network.
Runtime interactions between these processes are in
[§6](06-runtime-view.md); the software units mapped below are the building
blocks of [§5](05-building-blocks.md).

## 7.1 Infrastructure Level 1

**Motivation:** the whole platform runs on a single self-hosted ARM64 edge
node — LLM inference stays local (near-zero operating cost,
[§1.1](01-introduction-goals.md) business goals) and every service stays
inside a Tailscale-only network boundary ([§3.2](03-context-scope.md)).
Reliability under this single-node constraint comes from process supervision
(systemd) and the platform's own resiliency machinery
([§8](08-crosscutting-concepts.md) Error Resiliency), not from redundancy.

### Hardware

| Resource | Specification |
|---|---|
| Platform | NVIDIA Jetson ARM64 |
| CPU | 12 cores |
| RAM | 61 GB |
| Storage | 57 GB eMMC (system) + 1.8 TB NVMe (`/mnt/ssd/`) |
| Network | Tailscale VPN (100.123.73.66) |
| Kernel | Linux 5.10.120-tegra |

### Process Inventory

| Process | Type | Port/Transport | Manager |
|---|---|---|---|
| hermes-gateway | Python (systemd user) | — | `systemctl --user` |
| bt-agent (daemon) | Go, systemd user `bt-agent.service` (`--no-mcp`) | :8686 A2A + scheduler | `systemctl --user` |
| bt-agent (MCP) | Go (MCP child) | stdio | hermes-gateway |
| bt-evaluator | Go (MCP child) | stdio | hermes-gateway |
| bt-langagent | Go (MCP child) | stdio | hermes-gateway |
| bt-dashboard | Go (systemd user) | :9800 | `systemctl --user` |
| bt-gardener | Go, systemd user `bt-gardener.service` (sandboxed evolution cycles) | — | `systemctl --user` |
| Ollama | C++ (systemd) | :11434 | System service |
| DeepSeek API | External SaaS | HTTPS | — |

### Software → Node Mapping

All 13 binaries (inventory in [§5.1](05-building-blocks.md)) deploy onto the
single Jetson node; the long-running ones appear in the process inventory
above, the rest are invoked on demand:

| Building block ([§5](05-building-blocks.md)) | Execution context on the node | Lifecycle |
|---|---|---|
| `bt-agent` | systemd user service `bt-agent.service` (daemon, `--no-mcp`) **and** hermes-gateway MCP child (stdio) — two deployment forms of one binary | long-running |
| `bt-evaluator`, `bt-langagent` | hermes-gateway MCP children (stdio) | long-running |
| `bt-dashboard` | systemd user service (HTTP :9800) | long-running |
| `bt-gardener` | systemd user service `bt-gardener.service` | long-running |
| `bt-agent-cli`, `bt-assistant` | operator shell | on-demand CLIs |
| `benchcmp`, `bt-docgen`, `bt-ci-doctor` | make targets / git pre-commit hook / CI | build-time utilities |
| `bt-scalability-probe`, `bt-security-probe`, `bt-tree-integration` | manual or CI probe runs | on-demand utilities |
| hermes-gateway (external, Python) | systemd user service; spawns the three MCP children | long-running |
| Ollama (external) | system systemd service (HTTP :11434) | long-running |
| DeepSeek API (external) | external SaaS, reached over HTTPS | — |

Recurring work reaches the platform two ways: agent schedules executed inside
the bt-agent daemon's own scheduler (e.g. the goap-fusion loop-runner, cron
`0,30 * * * *` — [§6.4](06-runtime-view.md)), and Hermes-side cron jobs whose
output is delivered under `/mnt/ssd/.hermes/cron/output/` (7.2.2).

## 7.2 Infrastructure Level 2

### 7.2.1 Process Tree

```
systemd --user
├── hermes-gateway (Python)
│   ├── bt-agent (Go, stdio MCP)
│   ├── bt-evaluator (Go, stdio MCP)
│   └── bt-langagent (Go, stdio MCP)
├── bt-agent (Go, daemon `--no-mcp`, A2A :8686 + scheduler)
├── bt-dashboard (Go, HTTP :9800)
└── bt-gardener (Go, bt-gardener.service)

systemd (system)
└── ollama (C++, HTTP :11434)
```

**Key detail:** MCP servers are NOT independent systemd units. They are spawned by hermes-gateway as child processes. A `SIGHUP` reload of the gateway does NOT restart MCP children — they need a full restart to pick up new binary code. bt-agent CANNOT be started via `terminal(background=true)` because the MCP stdio server exits when stdin closes.

### 7.2.2 Storage Layout

The durable-archive mechanics behind the per-tree `*_archive-*.json` files —
the shared persistence idiom, caps, and benchmark gates — are described once
in [§5.3](05-building-blocks.md) and [§8](08-crosscutting-concepts.md)
Evolution Pipeline; entries below say what each file is and carry `(→ ADR-NNN)`
pointers only.

```
~/.go-bt-evolve/
├── agents/                  — Installed agent YAML definitions
├── history/                 — Agent run history (JSON)
├── memory/                  — Per-agent memory stores
├── blackboard/              — Scoped blackboard persistence (agent/run/session)
├── jobs/                    — Scheduler persistence (scheduler-jobs.json)
├── research/                — knowledge.json (dedup store), programs.json
│                              (multi-cycle programs), nlm-query-cache.json +
│                              nlm-usage.json (quota economy)
├── experience/              — experience.json: ExperienceBank of successful
│                              mutations, shared by bt-agent and bt-gardener,
│                              plus the experience.json.lock flock sidecar
│                              serializing the two writers' rewrites
│                              (→ ADR-021, ADR-024)
├── hitl/                    — Human-in-the-loop approval requests
├── users/                   — Per-user personalization workspaces
│                              (internal/persona, agent.UsersDir()): profile.json,
│                              interactions.jsonl, trees/, goals/, memory/,
│                              reflections/, experience/ — one directory per
│                              SanitizeUserID-derived user ID
├── audit/                   — Audit log
├── logs/                    — bt.log
├── feedback.json            — Knowledge-graph runtime-feedback snapshot
│                              (Fitness/RunCount/tool-edges); agent.FeedbackFile()
├── island_archive-*.json    — Durable IslandModel archives (islands, generation,
│                              cumulative migrations), one per sanitized
│                              base-tree ID; warm-started and re-persisted by
│                              bt_evolve_island, cap-bounded on Load,
│                              benchmark-gated on Save (→ ADR-033, ADR-034,
│                              ADR-040, ADR-096)
├── qtable_archive-*.json    — Durable QTable archives (state→action→Q-value),
│                              one per sanitized base-tree ID; warm-started and
│                              re-persisted by bt_evolve_qlearning, cap-bounded,
│                              benchmark-gated (→ ADR-041, ADR-111)
├── expert_archive-*.json    — Durable ExpertKnowledge.LearnedPatterns archives
│                              (action/category/gain triples), one per sanitized
│                              base-tree ID; appended by bt_evolve_qlearning,
│                              warm-starts bt_evolve_expert; capped at 500
│                              entries, lowest-gain evicted first
│                              (→ ADR-095, ADR-103)
├── map_elites_archive-*.json — Durable MAPElitesGrid archives (illuminated
│                              behavior-space cells), one per sanitized
│                              base-tree ID; warm-started and re-persisted by
│                              bt_evolve_qd, cap-bounded, benchmark-gated
│                              (→ ADR-033, ADR-043, ADR-111)
├── pareto_front_archive-*.json — Durable ParetoFront archives (non-dominated
│                              individuals), one per sanitized base-tree ID;
│                              warm-started and re-persisted by bt_evolve_pareto,
│                              cap-bounded, benchmark-gated (→ ADR-091, ADR-113)
├── nsga_archive-*.json      — Durable NSGAIIPopulation archives
│                              (ParetoFront-backed, same shape/Cap contract as
│                              pareto_front_archive-*.json), one per sanitized
│                              base-tree ID; warm-started and re-persisted by
│                              bt_evolve_multiobjective, benchmark-gated
│                              (→ ADR-091, ADR-096)
├── track_record-*.json      — Durable TrackRecord of benchmark-gate
│                              accept/reject outcomes, one per sanitized
│                              base-tree ID, deliberately shared across all five
│                              benchmark-gated evolve tools so any tool's
│                              rejection raises every tool's next generation
│                              budget on that tree (→ ADR-119)
├── dead_letter_queue.json   — Failed task persistence
└── vault/                   — Tree vault (checkpoint/restore)

~/.go-bt-reflections/        — Reflection records + gardener tree store

~/.go-bt-gardener/           — gardener-metrics.json (aggregate cycle-history
                               document read cross-process by the dashboard's
                               gardener panel — → ADR-032, ADR-115),
                               slo-metrics.json, snapshots/ (sequentially-
                               numbered per-tree pre-mutation snapshot revisions
                               plus index files, backing ListRevisions/
                               RestoreTreeRevision, the gardener_rollback tool,
                               and the automatic fail-closed rollback —
                               → ADR-093, ADR-115), selector-stats.json
                               (durable per-Selector telemetry for the
                               learned-ordering pass — → ADR-029, ADR-079),
                               dt-stats.json (durable DTAnalyzer entropy/Gini
                               path telemetry backing the mirrored
                               domain-tree reordering pass, opt-in via
                               wireDTOrdering — → ADR-171, ADR-172)

/mnt/ssd/
├── .hermes/                 — Hermes Agent runtime
│   ├── skills/              — SKILL.md files (~50 skills)
│   ├── cron/output/         — Cron job output delivery
│   └── audio_cache/         — TTS output cache
└── clawd/wiki/bt-research/  — Obsidian research vault + BT research docs
                               (syntheses/, plans/)

/home/nico/go-bt-evolve/     — BARE main repo (master never checked out); live
                               binaries with .previous backups — bt-agent and
                               bt-agent-cli at the repo root, bt-gardener and
                               bt-dashboard under bin/, matching each one's
                               systemd unit ExecStart; run worktrees under
                               /tmp/worktrees/; durable run artifacts in
                               docs/superpowers/runs/<id>/
```

### 7.2.3 Network Topology

```
Internet
    │
    ├── api.deepseek.com:443 ─── DeepSeek API (escalation LLM)
    │
Tailscale (100.123.73.66)
    │
    ├── localhost:9800 ─── bt-dashboard (HTTP)
    ├── localhost:8686 ─── A2A server (HTTP)
    ├── localhost:8644 ─── Hermes webhook bridge
    ├── localhost:11434 ── Ollama (HTTP)
    └── stdio ─────────── 3 MCP servers (bt-agent, bt-evaluator, bt-langagent)
```

All services bind to localhost except the dashboard (accessible via Tailscale). No public internet exposure.

---

*Generated by bt-agent arc42 pipeline — section7Deployment tree*
