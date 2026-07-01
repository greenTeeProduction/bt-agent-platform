# Logging & Tracing Overhaul — Design

**Date:** 2026-07-02
**Status:** Approved (brainstorming session)
**Goal:** Ship the platform's logs and traces to a queryable local backend and instrument deeply enough that a failed scheduled run can be debugged entirely from Grafana.

## Context

Today the platform has:

- A structured slog JSON logger (`internal/engine/app_logger.go`) with rotation to `~/.go-bt-evolve/logs/bt.log` + stderr — but 13 files still use raw `log.Printf`, and log records carry no run/trace correlation.
- A homegrown tracing package (`internal/tracing`: span interface, console tracer, OTLP/HTTP JSON exporter, batcher, W3C propagation) instrumented at only 6 call sites — one span per whole tree run; individual BT node execution is invisible.
- `cmd/bt-otlp-collector`, a minimal collector that appends spans to log files — not queryable.
- `TestEndToEndOTLPExport` fails whenever no collector listens on :4318.

Decisions from the session: **real backend pipeline** as primary goal, **local Grafana stack** as backend, **full observability pass** for signal depth, **approach B** — migrate to the official OpenTelemetry-Go SDK.

## Architecture

Three parts:

1. **Go platform** emits traces and logs through the OpenTelemetry-Go SDK.
2. **Grafana stack** — Tempo (traces) + Loki 3.x (logs) + Grafana (UI) — one Docker Compose file in `monitoring/`. Three containers; no shipper agent: logs go OTLP-direct to Loki's native OTLP ingest.
3. **File log stays source of truth.** slog fans out to the rotating file handler AND the OTel log bridge. Loki being down never loses logs.

`internal/tracing` becomes a **thin facade** over the OTel SDK:

- Keeps the `StartSpan(ctx, name)`-style API and `Span` interface the engine already imports.
- Deletes the homegrown SDK internals — `batcher.go`, `exporter.go`, `w3c.go`, `reader.go` — replaced by SDK batching, OTLP export, and W3C propagation.
- Only the facade imports `go.opentelemetry.io/otel`; engine/domain code never does.

`cmd/bt-otlp-collector` is superseded by Tempo and removed.

**Activation:** tracing/log export is enabled only when an endpoint is configured — standard `OTEL_EXPORTER_OTLP_ENDPOINT` (and `OTEL_SERVICE_NAME`), with the existing `BT_OTLP_ENDPOINT` honored as an alias. Unset → no-op tracer and file-only logging, exactly like today.

## Instrumentation

- **Run root span** in `scheduler.runJob` / `RunOnce`: attrs `run_id`, `agent`, `tree`, outcome, attempt number. One trace per scheduled cycle (and per manual/MCP run).
- **Per-node spans at the registry seam.** `RegisterAction` / `RegisterCondition` wrap every registered function once in a tracing decorator: span named after the action/condition, attrs for node kind, returned status, duration. One change point covers all current and future nodes; no tree definitions change. BT structural ticks (Selector/Sequence) stay untraced to avoid span explosion from tick loops — leaf executions are where the work happens.
- **LLM call spans** via a decorator in `internal/llm` (same wrapping pattern as `ErrorRecorder`, but kept a separate type — one concern each; `RunOnce` stacks them): model, provider, duration, error class, rate-limit / retry-after when present.
- **Scheduler extras:** webhook publish span; DLQ pushes recorded as span events on the run span.

## Log correlation & cleanup

- A slog `Handler` wrapper injects `trace_id` / `span_id` from `context.Context` into every record emitted through context-aware calls.
- `RunOnce` binds a run-scoped logger (pre-bound `run_id`, `agent`, `tree` attrs) carried via the Blackboard, so engine actions log with full correlation without per-call ceremony.
- The 13 raw `log.Printf` files migrate to the structured logger. Stdout remains untouched (MCP stdio constraint: stdout is JSON-RPC only; logging already targets stderr + file).
- Grafana provisioning wires Tempo trace-to-logs (span → Loki query by `trace_id`) and Loki derived fields (log line → trace).

## Infra (`monitoring/`)

- `docker-compose.yml`: Tempo (OTLP :4318), Loki 3.x (:3100 with OTLP ingest), Grafana (:3000; all host ports env-overridable for clashes).
- Provisioned Grafana datasources (Tempo + Loki, correlation both directions) and a starter **"BT Agent Runs"** dashboard: runs over time, failure rate, slowest actions, recent error logs.
- Retention defaults: Tempo 7 days, Loki 14 days; data on named volumes.
- `make observability-up` / `make observability-down` targets.
- Existing `monitoring/prometheus-alerts.yml` untouched — **metrics are out of scope** for this design.

## Error handling

Telemetry must never break the platform:

- Exporter failures drop after the SDK batcher's bounded queue/retry; they never block or fail a run.
- The rotating file handler always receives every record regardless of OTLP state.
- Facade initialization failure degrades to no-op tracing + file logging with a single warning line.

## Testing

- Unit: facade span lifecycle (start/end/attrs/error recording), slog correlation handler (trace_id injection, run-scoped fields), registry tracing decorator (span per invocation, status attr).
- Integration: `TestEndToEndOTLPExport` rewritten to **skip when no collector is reachable** and pass against Tempo when the stack is up (fixes the perpetual local/CI failure).
- `make check-full` green throughout; new OTel dependencies accepted as approach-B cost (ARM build time).

## Staging (one commit each)

1. Compose stack + Grafana provisioning + make targets.
2. OTel SDK migration behind the `internal/tracing` facade (delete homegrown internals; existing 6 call sites keep working).
3. Log correlation: slog handler wrapper, run-scoped logger, OTLP log bridge with file fanout.
4. Instrumentation breadth: run root span, registry decorator, LLM decorator, scheduler extras.
5. Cleanup: migrate 13 `log.Printf` files, remove `cmd/bt-otlp-collector`, rewrite e2e test.

## Success criteria

- A failed `goap-fusion-loop-runner` cycle can be located in Grafana by agent name, its trace shows every action executed with durations and the failing node, and one click reaches the correlated log lines.
- With the stack down, the platform behaves exactly as today (file + stderr logging, no-op tracing, zero errors in runs).
