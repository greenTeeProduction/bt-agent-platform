# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased] — 2026-05-28

### Added

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

