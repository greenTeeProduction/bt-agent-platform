# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added

- **(blackboard):** Scoped context store (`run`, `session`, `agent`) with ReAct `bb_*` tools, memory/history offloading, pipeline session sharing, MCP `bt_bb_*` tools, `{{.BB.*}}` chain templates, and session/agent JSON persistence under `${AGENT_HOME}/blackboard/` (ADR-009).
- **(blackboard):** Agent-scope auto-promotion (`runs/latest/*`), `run_id`/`session_id` on run responses, dashboard `GET /api/blackboard`, workflow session key viewer.
- **(blackboard):** CLI `bb list/read/scopes`, dashboard Agents tab BB panel, `GET /api/blackboard/scopes`.
- **(agents):** Unified agent platform — canonical `~/.go-bt-evolve/` paths, `RunOnce()` runner with memory/quality/input enforcement, registry-first dashboard listing, Workflows tab, async pipeline API, HITL approval wiring, MCP `bt_workflow_run` pipeline mode, operator guide (`docs/agents.md`).
- **(agents):** Runtime YAML contracts — `inputs`, `quality`, and `outputs` validation on scheduler, MCP, CLI, and dashboard runs.
- **(agents):** Live scheduling — `bt-agent-cli schedule` and dashboard create persist `scheduler-jobs.json`; running `bt-agent` syncs registry changes each tick.
- **(observability):** Local Grafana stack (Tempo + Loki + Grafana) via `monitoring/docker-compose.yml` with provisioned trace↔log correlation and a BT Agent Runs dashboard; `make observability-up/down`.
- **(observability):** OTel-Go SDK behind the `internal/tracing` facade; per-node spans via the action/condition registry, `agent.run` root spans, LLM call spans, webhook/DLQ spans; slog gains trace_id/span_id correlation, a run-scoped logger, and an OTLP log bridge to Loki (`BT_OTLP_LOGS_ENDPOINT`).

### Changed

- **(agents):** Dashboard and CLI execute agents in-process via `RunOnce()` (Hermes CLI fallback only when runner unavailable).
- **(agents):** Agent delete clears scheduler jobs across dashboard, CLI, MCP, and `bt-assistant`.
- **(engine):** Superpowers/GOAP-fusion claude runs default to `--model opus` when `BT_SUPERPOWERS_CLAUDE_MODEL` is unset — set the env var to `auto` (or `default`/`none`) to restore the CLI's own default model. Skip-permissions mode no longer drops the model flag.
- **(agents):** Webhook event fields `failure_reason` and `nodes` carry raw values (error message, `a → b → c` node trace) without embedded display labels — consumer templates do the labeling.
- **(logging):** All library packages log through structured slog (`log.Printf` eliminated from `internal/`); binaries `slog.SetDefault(engine.L())`.

### Removed

- **(engine):** Deleted the dead GOAP fusion apply path — `ApplyImprovementWithClaude`, `ReadImprovementPlan`, `IsApplyRequest`, `IsResearchOrGapRequest` and their helpers (`runClaudeCode`, `buildClaudeFusionPrompt`, stash/reset/push git machinery) were registered but referenced by no tree; the live loop implements via `RunSuperpowersClaudeImplementation` (worktree-isolated, commits via `superpowers_apply.go`).
- **(observability):** Homegrown tracing internals (console tracer, OTLP exporter, batcher, W3C parser, trace reader) and `cmd/bt-otlp-collector` — superseded by the OTel SDK and Tempo.

### Fixed

- **(engine):** GOAP fusion "grill me" escalation now actually advances rounds 1→2→3 — grill round and NotebookLM conversation id persist in the agent-scope blackboard across scheduled runs (previously stored as a string but read back with numeric type assertions, and ChainState died with each run, so every cycle was round 1 in a fresh conversation). After round 3 the cycle wraps to round 1 with a fresh conversation.
- **(engine):** `VerifyGoapBuild` no longer self-destructs on slow hosts: `runGoapShell` gained a per-call timeout, the build gets 180s and the 180s-budget test run gets a 240s outer deadline (previously both were killed at the fixed 120s shell timeout and misreported as build/test failures); timeout kills are now labeled in the error.
- **(engine):** claude CLI `--allowedTools` default rewritten as one prefix per `Bash()` rule (the colon-joined multi-command form parses as a single unmatched prefix, silently denying every shell command in `--print` mode) and now covers the absolute Go path the prompts use.
- **(engine):** `ReadVaultResearch` caps vault reads to the newest 8 syntheses / 4 evolution reports / 4 plans (the vault had grown to 769 synthesis files, all re-read every 30-minute cycle) and stats files once before sorting.
- **(reliability):** Rate-limit handling is functional end-to-end — 429 detection moved before response-body parsing in the DeepSeek and OpenAI-compatible clients (shared `checkRateLimit` helper), the typed `RateLimitError` survives `FallbackLLM` aggregation (`errors.Join`) and BT engine execution (LLM error recorder in `RunOnce`), and rate-limit classification runs ahead of the generic LLM patterns with a typed `errors.As` fast path.
- **(reliability):** Server `Retry-After` values are clamped to 5m with additive jitter (no uncapped, lockstep sleeps); missing or already-elapsed headers fall back to the policy's backoff instead of a hardcoded 60s; bare `429`/`rpm`/`tpm` substrings no longer misclassify permanent failures as retryable rate limits; the openai-compatible 429 message reports the per-call model.
- **(agents):** Scheduler webhook events carry the current run's node trace via the `AgentRunner` return path — previously a stale trace from the prior run was published after panics, and the full `RunResult` (including LLM output) was pinned on the registry `Instance` indefinitely.

### Prior (2026-05-28)

### Added

- **(engine):** TypedEdge verifier hardening — recursive node/action/condition validation, side-effect approval gates, BuildTree fail-closed behavior, legacy builtin name registry, and edge-preserving clone support.
- **(plans):** BT connector node implementation plan for TreeRef/TreeInclude/TreeCall reusable tree references (`2026-06-02-bt-connector-nodes.md`).

- **(maturity):** observability — OpenTelemetry-ready tracing package with console exporter (b3b30b7)
- **(maturity):** scalability — define Queue and PriorityTaskQueue interfaces for pluggable backends (8a5c5ae)
- **(maturity):** Observability — Prometheus alert rules + alert evaluator (72bba17)
- **(maturity):** security — structured audit logging in MCP server (a1d0b10)
- **(maturity):** add RemoteExecutor for horizontal scaling of agent tasks (8efb247)
- **(maturity):** [security] add time-based rate limiting to all 3 MCP servers (f919062)
- **(maturity):** add JSON config file support with env overrides (57b377d)
- **(maturity):** observability — add log rotation to prevent unbounded log growth (a4c034d)
- **(maturity):** [scalability] AgentExecutor interface + AgentRouter with health-aware round-robin routing (ee282fb)
- **(eval):** 100% platform eval — all 220 tasks pass across 20 suites (bea0fe5)
- **(eval):** challenging eval suites — 220 tasks, 4 difficulty tiers (0d19aa9)
- **(eval):** comprehensive platform evaluation suite + top 20 use cases (6d55544)
- **(research):** NotebookLM 100% chat utilization (10% → 100%) (fdc30a4)
- **(maturity):** security — TLS support for dashboard with HSTS auto-enable (19ac4e1)
- **(maturity):** security - IP allowlist/blocklist + audit event logging (96db5c3)
- **(merged):** universal MergedTree combining all 46 BT trees (4db56ac)
- **(maturity):** security headers middleware — X-Content-Type-Options, X-Frame-Options, CSP, HSTS, CORS, request timeout (a05e5ed)
- **(observability):** wire structured log package into all 4 main binaries (f5822f5)
- **(maturity):** scalability - priority queue + concurrency limiter (4aba6f1)
- **(maturity):** MCP server security — arg sanitization + API key auth (3b29303)
- **(ci+docs):** Phase 4 — GitHub Actions CI/CD, getting-started, ADRs (461801c)
- **(reliability):** Phase 3 — circuit breaker, backoff, DLQ, worker pool, task queue (0c97dcf)
- **(config+api):** Phase 2 — typed config, API versioning, JSON Schema I/O (de4dccf)
- **(security+observability):** Phase 1 — rate limiting, input sanitization, Prometheus metrics (571f036)
- **(goap):** add Goal-Oriented Action Planning to the BT platform (ba2622d)
- **(maturity):** add API key auth middleware and health endpoint (11efa94)
- **(phase8):** Hermes agent platform — deep integration (9af451a)
- **(phase7):** Hermes agent platform integration (7a9ceda)
- **(phase6):** Agent scheduler + run history + long-running support (46a8e92)
- **(phase5):** Agent marketplace + skill-to-agent auto-generation (e58c812)
- **(phase4):** Agent validation suites + composite scoring (d918bd4)
- **(phase3):** Multi-agent workflow engine (0466e2f)
- **(phase2):** Agent SDK — definitions, registry, CLI, templates (d7db3f0)
- **(phase1.3-1.4):** panic recovery + structured logging (ff33549)
- **(evolution):** SelectorOptimizer + Memetic Local Search (f4d030a)

### Fixed

- **(eval):** clean detectPath with task-based routing for all 20 paths (1bde870)
- **(eval):** add 7 new condition handlers + 7 MergedTree strategy paths (45537a2)
- **(godev):** fix max_tokens (10→400, 5→400) and add HasClearTask PreGate condition (3bf3f59)
- add short guard to TestToolBench_EvaluateWithCodeReviewTree — all 17 packages pass clean in short mode (0038bc8)
- add short guard to ToolBench_EvaluateWithGoDevTree (3ba572c)
- add short guard to TestTauBench_EmptyEntries (19ff125)
- add short guards to remaining τ-bench Ollama tests (10a93c4)
- resolve 2 short-mode test failures + finalize coverage (386a090)
- **(phase1.2):** output quality gates + fix critical max_tokens bugs (d529296)
- **(phase1.1):** fix all unit tests + add camelCase word splitting (3842243)
- **(evolution):** resolve SelectorStats type collision between decision_tree and selector_optimizer (2cbe19d)

### Testing

- **(merged):** MergedTree routing verified — 15/15 paths passing (6e1be6a)
- **(coverage):** Phase 5 — coverage surge across 4 weak packages (b00327a)
- complete coverage push — evolution +11%, knowledge +9.6%, agent use cases (3361bfe)
- thinktank coverage 3.7% → 80.2% (+76.5%) (6e8cf96)
- integration tests — 31 trees, 7 chain types, quality gates, mutations (7e20ab3)
- boost coverage — 37 new tests across engine/agent/workflow (b778caa)

### Chores

- add .gitignore (d40a6f6)

### Miscellaneous

- evolution: add kanban trees (6) + notebooklm tree (1) (730eb42)
- init: go-bt-evolve framework (02ccdbb)

