# Logging & Tracing Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship platform logs and traces to a local Grafana stack (Tempo + Loki) through the official OpenTelemetry-Go SDK, with per-node spans and run-correlated logs.

**Architecture:** `internal/tracing` becomes a thin facade over the OTel SDK (homegrown batcher/exporter/w3c/reader deleted). slog fans out to the rotating file (source of truth) plus an OTLP log bridge. Instrumentation is added at four seams: `RunOnce` (run root span), the action/condition registry (per-node spans), `internal/llm` (LLM call spans), and the scheduler webhook path.

**Tech Stack:** Go 1.26, `go.opentelemetry.io/otel` (+sdk, otlptracehttp, sdk/log, otlploghttp, contrib/bridges/otelslog), Grafana + Tempo + Loki 3.x via Docker Compose.

**Spec:** `docs/superpowers/specs/2026-07-02-logging-tracing-design.md`

## Global Constraints

- Go binary: `/usr/local/go/bin/go` — NOT on default PATH; prefix all go/git-hook commands with `PATH=/usr/local/go/bin:$PATH`.
- Telemetry must never block or fail a run; export is enabled only when `OTEL_EXPORTER_OTLP_ENDPOINT` (or alias `BT_OTLP_ENDPOINT`) is set. Unset → no-op tracer + file-only logging, identical to today.
- Stdout is JSON-RPC only in MCP mode — logging goes to stderr + file, never stdout.
- The rotating file `~/.go-bt-evolve/logs/bt.log` receives every log record regardless of OTLP state.
- Metrics are OUT OF SCOPE. `monitoring/prometheus-alerts.yml` stays untouched.
- Every commit must pass the pre-commit hook (gofmt, vet, golangci-lint on staged pkgs, mod-tidy, ci-doctor, short tests). Run `make check-quick` before each commit.
- Host ports in compose must be env-overridable (defaults: Grafana 3000, Loki 3100, Tempo 4318).

---

### Task 1: Grafana stack (Tempo + Loki + Grafana) in monitoring/

**Files:**
- Create: `monitoring/docker-compose.yml`
- Create: `monitoring/tempo.yaml`
- Create: `monitoring/loki.yaml`
- Create: `monitoring/grafana/provisioning/datasources/datasources.yaml`
- Create: `monitoring/grafana/provisioning/dashboards/dashboards.yaml`
- Create: `monitoring/grafana/dashboards/bt-agent-runs.json`
- Modify: `Makefile` (add `observability-up` / `observability-down` targets + help lines)

**Interfaces:**
- Produces: OTLP/HTTP ingest at `http://localhost:4318` (Tempo, traces) and `http://localhost:3100/otlp` (Loki, logs); Grafana at `http://localhost:3000`. Later tasks point `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318` for traces and `BT_OTLP_LOGS_ENDPOINT=http://localhost:3100/otlp` for logs.

- [ ] **Step 1: Write `monitoring/tempo.yaml`**

```yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        http:
          endpoint: 0.0.0.0:4318

storage:
  trace:
    backend: local
    local:
      path: /var/tempo/blocks
    wal:
      path: /var/tempo/wal

compactor:
  compaction:
    block_retention: 168h   # 7 days (spec)
```

- [ ] **Step 2: Write `monitoring/loki.yaml`**

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory

schema_config:
  configs:
    - from: 2024-01-01
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  retention_period: 336h   # 14 days (spec)
  allow_structured_metadata: true
  otlp_config:
    resource_attributes:
      attributes_config:
        - action: index_label
          attributes: [service.name]

compactor:
  retention_enabled: true
  delete_request_store: filesystem
```

- [ ] **Step 3: Write `monitoring/docker-compose.yml`**

```yaml
name: bt-observability

services:
  tempo:
    image: grafana/tempo:2.6.1
    command: ["-config.file=/etc/tempo.yaml"]
    volumes:
      - ./tempo.yaml:/etc/tempo.yaml:ro
      - tempo-data:/var/tempo
    ports:
      - "${BT_TEMPO_OTLP_PORT:-4318}:4318"
      - "${BT_TEMPO_HTTP_PORT:-3200}:3200"
    restart: unless-stopped

  loki:
    image: grafana/loki:3.3.2
    command: ["-config.file=/etc/loki/loki.yaml"]
    volumes:
      - ./loki.yaml:/etc/loki/loki.yaml:ro
      - loki-data:/loki
    ports:
      - "${BT_LOKI_PORT:-3100}:3100"
    restart: unless-stopped

  grafana:
    image: grafana/grafana:11.4.0
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
      GF_SECURITY_ADMIN_PASSWORD: admin
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    ports:
      - "${BT_GRAFANA_PORT:-3000}:3000"
    depends_on: [tempo, loki]
    restart: unless-stopped

volumes:
  tempo-data:
  loki-data:
  grafana-data:
```

- [ ] **Step 4: Write Grafana provisioning**

`monitoring/grafana/provisioning/datasources/datasources.yaml`:

```yaml
apiVersion: 1
datasources:
  - name: Tempo
    uid: tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
    jsonData:
      tracesToLogsV2:
        datasourceUid: loki
        spanStartTimeShift: "-5m"
        spanEndTimeShift: "5m"
        filterByTraceID: true
        customQuery: false
  - name: Loki
    uid: loki
    type: loki
    access: proxy
    url: http://loki:3100
    jsonData:
      derivedFields:
        - name: trace_id
          matcherType: label
          matcherRegex: trace_id
          url: "$${__value.raw}"
          datasourceUid: tempo
```

`monitoring/grafana/provisioning/dashboards/dashboards.yaml`:

```yaml
apiVersion: 1
providers:
  - name: bt-platform
    folder: BT Platform
    type: file
    options:
      path: /var/lib/grafana/dashboards
```

- [ ] **Step 5: Write starter dashboard `monitoring/grafana/dashboards/bt-agent-runs.json`**

```json
{
  "title": "BT Agent Runs",
  "uid": "bt-agent-runs",
  "schemaVersion": 39,
  "time": {"from": "now-6h", "to": "now"},
  "panels": [
    {
      "type": "timeseries", "title": "Agent runs (spans/min)",
      "gridPos": {"h": 8, "w": 12, "x": 0, "y": 0},
      "datasource": {"uid": "tempo"},
      "targets": [{"queryType": "traceqlSearch", "query": "{name =~ \"agent.run/.*\"}", "refId": "A"}]
    },
    {
      "type": "table", "title": "Slowest actions (last 1h)",
      "gridPos": {"h": 8, "w": 12, "x": 12, "y": 0},
      "datasource": {"uid": "tempo"},
      "targets": [{"queryType": "traceql", "query": "{span.bt.node.kind = \"action\"} | select(span.bt.node.name, duration)", "refId": "A"}]
    },
    {
      "type": "logs", "title": "Recent errors",
      "gridPos": {"h": 10, "w": 24, "x": 0, "y": 8},
      "datasource": {"uid": "loki"},
      "targets": [{"expr": "{service_name=~\"bt-.*\"} | json | level = `ERROR`", "refId": "A"}]
    }
  ]
}
```

- [ ] **Step 6: Add Makefile targets** (after the `security-medium` target)

```make
observability-up:
	docker compose -f monitoring/docker-compose.yml up -d
	@echo "Grafana: http://localhost:$${BT_GRAFANA_PORT:-3000}  Tempo OTLP: :$${BT_TEMPO_OTLP_PORT:-4318}  Loki: :$${BT_LOKI_PORT:-3100}"

observability-down:
	docker compose -f monitoring/docker-compose.yml down
```

Add both to `.PHONY` and to the `help` target output.

- [ ] **Step 7: Verify the stack comes up**

Run:
```bash
make observability-up && sleep 20
curl -sf http://localhost:3200/ready && echo TEMPO-OK
curl -sf http://localhost:3100/ready && echo LOKI-OK
curl -sf http://localhost:3000/api/health | grep -q ok && echo GRAFANA-OK
```
Expected: `TEMPO-OK`, `LOKI-OK`, `GRAFANA-OK` (Loki may need up to ~60s; retry once). Leave the stack running for later tasks.

- [ ] **Step 8: Commit**

```bash
git add monitoring/ Makefile
PATH=/usr/local/go/bin:$PATH git commit -m "feat(observability): local Grafana stack (Tempo+Loki+Grafana) with provisioning and make targets"
```

---

### Task 2: OTel SDK trace facade in internal/tracing

**Files:**
- Create: `internal/tracing/otel.go`
- Create: `internal/tracing/otel_test.go`
- Modify: `go.mod` / `go.sum` (new deps)

**Interfaces:**
- Consumes: existing `Span`, `Attr`, `SpanContext`, `Tracer` interfaces and `SetGlobalTracer` in `internal/tracing/tracing.go` (unchanged in this task).
- Produces:
  - `tracing.InitFromEnv(serviceName string) (shutdown func(context.Context) error)` — installs the SDK-backed global tracer iff an endpoint is configured.
  - `tracing.Endpoint() string` — `OTEL_EXPORTER_OTLP_ENDPOINT`, falling back to `BT_OTLP_ENDPOINT`.
  - `tracing.NewOTelTracer(t oteltrace.Tracer) Tracer` — used by tests.
  - `tracing.SpanContextFrom(ctx context.Context) (SpanContext, bool)` — used by the log-correlation handler (Task 4).
  - `tracing.ContextWithTraceParentHeader(ctx context.Context, header string) context.Context` — replaces the homegrown W3C parsing for the MCP server (Task 3).

- [ ] **Step 1: Add dependencies**

```bash
PATH=/usr/local/go/bin:$PATH go get \
  go.opentelemetry.io/otel@latest \
  go.opentelemetry.io/otel/sdk@latest \
  go.opentelemetry.io/otel/trace@latest \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@latest
PATH=/usr/local/go/bin:$PATH go mod tidy
```

- [ ] **Step 2: Write the failing test `internal/tracing/otel_test.go`**

```go
package tracing

import (
	"context"
	"errors"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newRecordingTracer() (*tracetest.SpanRecorder, Tracer) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	return rec, NewOTelTracer(tp.Tracer("test"))
}

func TestOTelSpan_LifecycleAndAttributes(t *testing.T) {
	rec, tr := newRecordingTracer()
	ctx, span := tr.StartSpan(context.Background(), "op")
	if !span.IsRecording() {
		t.Fatal("span should be recording")
	}
	span.SetAttribute("k", "v")
	span.AddEvent("evt", Attr{Key: "a", Value: "b"})
	span.RecordError(errors.New("boom"))
	sc := span.SpanContext()
	if len(sc.TraceID) != 32 || len(sc.SpanID) != 16 {
		t.Fatalf("unexpected ids: %+v", sc)
	}
	// Child span nests under parent via ctx.
	_, child := tr.StartSpan(ctx, "child")
	if child.SpanContext().TraceID != sc.TraceID {
		t.Fatal("child must share parent trace id")
	}
	child.End()
	span.End()
	if got := len(rec.Ended()); got != 2 {
		t.Fatalf("ended spans = %d, want 2", got)
	}
}

func TestSpanContextFrom(t *testing.T) {
	_, tr := newRecordingTracer()
	ctx, span := tr.StartSpan(context.Background(), "op")
	defer span.End()
	sc, ok := SpanContextFrom(ctx)
	if !ok || sc.TraceID != span.SpanContext().TraceID {
		t.Fatalf("SpanContextFrom = %+v ok=%v", sc, ok)
	}
	if _, ok := SpanContextFrom(context.Background()); ok {
		t.Fatal("empty ctx must return ok=false")
	}
}

func TestInitFromEnv_NoEndpointIsNoop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("BT_OTLP_ENDPOINT", "")
	shutdown := InitFromEnv("test-svc")
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("noop shutdown: %v", err)
	}
}

func TestContextWithTraceParentHeader(t *testing.T) {
	_, tr := newRecordingTracer()
	ctx := ContextWithTraceParentHeader(context.Background(),
		"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	_, span := tr.StartSpan(ctx, "child")
	defer span.End()
	if span.SpanContext().TraceID != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("trace id not propagated: %+v", span.SpanContext())
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/tracing/ -run 'TestOTelSpan|TestSpanContextFrom|TestInitFromEnv|TestContextWithTraceParent' -count=1`
Expected: FAIL — `undefined: NewOTelTracer`, `undefined: SpanContextFrom`, etc.

- [ ] **Step 4: Write `internal/tracing/otel.go`**

```go
package tracing

import (
	"context"
	"net/url"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// otelSpan adapts an OTel SDK span to the platform Span interface.
type otelSpan struct{ s oteltrace.Span }

func (o otelSpan) End() { o.s.End() }

func (o otelSpan) AddEvent(name string, attrs ...Attr) {
	kv := make([]attribute.KeyValue, 0, len(attrs))
	for _, a := range attrs {
		kv = append(kv, attribute.String(a.Key, a.Value))
	}
	o.s.AddEvent(name, oteltrace.WithAttributes(kv...))
}

func (o otelSpan) SetAttribute(key, value string) {
	o.s.SetAttributes(attribute.String(key, value))
}

func (o otelSpan) RecordError(err error) {
	if err == nil {
		return
	}
	o.s.RecordError(err)
	o.s.SetStatus(codes.Error, err.Error())
}

func (o otelSpan) SpanContext() SpanContext {
	sc := o.s.SpanContext()
	return SpanContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}
}

func (o otelSpan) IsRecording() bool { return o.s.IsRecording() }

// otelTracer adapts an OTel tracer to the platform Tracer interface.
type otelTracer struct{ t oteltrace.Tracer }

func (t otelTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	ctx, s := t.t.Start(ctx, name)
	return ctx, otelSpan{s: s}
}

// NewOTelTracer wraps an OTel tracer in the platform Tracer interface.
func NewOTelTracer(t oteltrace.Tracer) Tracer { return otelTracer{t: t} }

// SpanContextFrom extracts the active span's identifiers from ctx.
// ok is false when ctx carries no valid span.
func SpanContextFrom(ctx context.Context) (SpanContext, bool) {
	sc := oteltrace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return SpanContext{}, false
	}
	return SpanContext{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}, true
}

// ContextWithTraceParentHeader returns ctx with the W3C traceparent header
// applied, so spans started from it join the remote trace. Invalid headers
// return ctx unchanged.
func ContextWithTraceParentHeader(ctx context.Context, header string) context.Context {
	if strings.TrimSpace(header) == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{"traceparent": header}
	return propagation.TraceContext{}.Extract(ctx, carrier)
}

// Endpoint resolves the OTLP trace endpoint: the standard OTel env var wins,
// the legacy BT_OTLP_ENDPOINT is honored as an alias. Empty means disabled.
func Endpoint() string {
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		return v
	}
	return os.Getenv("BT_OTLP_ENDPOINT")
}

// InitFromEnv installs an OTel-SDK-backed global tracer when an OTLP endpoint
// is configured and returns a shutdown func. Without an endpoint the global
// noop tracer stays installed and shutdown is a no-op. Export failures are
// dropped by the SDK batcher — telemetry never blocks a run.
func InitFromEnv(serviceName string) func(context.Context) error {
	endpoint := Endpoint()
	if endpoint == "" {
		return func(context.Context) error { return nil }
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return func(context.Context) error { return nil }
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(u.Host)}
	if u.Scheme != "https" {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return func(context.Context) error { return nil }
	}
	res := sdkresource.NewSchemaless(attribute.String("service.name", serviceName))
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	SetGlobalTracer(NewOTelTracer(tp.Tracer(serviceName)))
	return tp.Shutdown
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/tracing/ -run 'TestOTelSpan|TestSpanContextFrom|TestInitFromEnv|TestContextWithTraceParent' -count=1 -v`
Expected: 4× PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tracing/otel.go internal/tracing/otel_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(tracing): OTel SDK facade — adapter, InitFromEnv, W3C helper"
```

---

### Task 3: Delete homegrown tracing internals, migrate consumers

**Files:**
- Delete: `internal/tracing/batcher.go`, `batcher_test.go`, `exporter.go`, `exporter_test.go`, `reader.go`, `reader_test.go`, `w3c.go`, `w3c_test.go`, `http.go`, `http_test.go`
- Modify: `internal/tracing/tracing.go` (remove `ConsoleTracer` + samplers; keep `SpanContext`, `Span`, `Attr`, `Tracer`, noop impls, global tracer + package-level `StartSpan`)
- Modify: `internal/tracing/tracing_test.go` (drop ConsoleTracer/sampler tests, keep global/noop tests)
- Modify: `internal/tracing/otlp_e2e_test.go` (skip-when-unreachable)
- Modify: `cmd/bt-agent/main.go:281-287`, `cmd/bt-dashboard/main.go:124-130`, `cmd/bt-evaluator/main.go:170-176`, `cmd/bt-langagent/main.go:201-207` (replace ConsoleTracer init with `InitFromEnv`)
- Modify: `internal/engine/mcp_server.go:366-386` (traceparent handling via `ContextWithTraceParentHeader`)

**Interfaces:**
- Consumes: `tracing.InitFromEnv`, `tracing.ContextWithTraceParentHeader` from Task 2.
- Produces: `internal/tracing` contains ONLY the facade; `tracing.StartSpan(ctx, name)` package function keeps its exact signature — all 6 existing call sites compile unchanged.

- [ ] **Step 1: Survey before deleting** (safety check — expect no hits outside internal/tracing and the listed files)

```bash
grep -rn "ConsoleTracer\|ConfigureOTLPFromEnv\|NewTraceReader\|ParseTraceParent\|ContextWithTraceParent\b\|NewOTLPHTTPExporter\|SpanBatcher" \
  --include="*.go" internal/ cmd/ | grep -v internal/tracing/
```
Expected hits ONLY in: the four `cmd/*/main.go` tracer-init blocks and `internal/engine/mcp_server.go`. If anything else appears, migrate it the same way before deleting.

- [ ] **Step 2: Delete the homegrown internals**

```bash
git rm internal/tracing/batcher.go internal/tracing/batcher_test.go \
       internal/tracing/exporter.go internal/tracing/exporter_test.go \
       internal/tracing/reader.go internal/tracing/reader_test.go \
       internal/tracing/w3c.go internal/tracing/w3c_test.go \
       internal/tracing/http.go internal/tracing/http_test.go
```

- [ ] **Step 3: Trim `tracing.go`** — delete the `ConsoleTracer` type and everything only it uses (samplers section included), keep: `SpanContext`, `Span`, `Attr`, `Tracer`, `noopSpan`, `noopTracer`, the global-tracer var + `SetGlobalTracer`/`GlobalTracer`, and the package-level `StartSpan`. Update the package doc comment to:

```go
// Package tracing is a thin facade over the OpenTelemetry SDK for the BT
// platform. Engine code imports only this package; the SDK is wired in
// otel.go and activated by InitFromEnv when an OTLP endpoint is configured.
```

Remove now-unused imports (`fmt`, `io`, `os`, `sync/atomic`, `time` — keep what the remaining code needs). Trim `tracing_test.go` to the tests covering noop + global tracer behavior.

- [ ] **Step 4: Replace the tracer init in all four binaries.** In `cmd/bt-agent/main.go` replace:

```go
	// ── Tracing ────────────────────────────────────────────────────────────
	tracingLogPath := filepath.Join(agent.LogsDir(), "traces.log")
	if f, err := os.OpenFile(tracingLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		tracer := tracing.NewConsoleTracer("bt-agent", f)
		tracing.ConfigureOTLPFromEnv(tracer)
		tracing.SetGlobalTracer(tracer)
	}
```

with:

```go
	// ── Tracing (OTel SDK; no-op unless OTEL_EXPORTER_OTLP_ENDPOINT/BT_OTLP_ENDPOINT set) ──
	tracingShutdown := tracing.InitFromEnv("bt-agent")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tracingShutdown(ctx)
	}()
```

Apply the same replacement in `cmd/bt-dashboard/main.go` (service name `"bt-dashboard"`), `cmd/bt-evaluator/main.go` (`"bt-evaluator"`), `cmd/bt-langagent/main.go` (`"bt-langagent"`). Remove imports that become unused in each file.

- [ ] **Step 5: Migrate the MCP traceparent block** in `internal/engine/mcp_server.go`. Replace the `ParseTraceParent`/`ContextWithTraceParent` usage with:

```go
		traceCtx := context.Background()
		if params.Traceparent != "" {
			traceCtx = tracing.ContextWithTraceParentHeader(traceCtx, params.Traceparent)
		}
```

(keep the existing `tracing.StartSpan(traceCtx, "mcp:"+params.Name)` and the `span.SetAttribute("traceparent", "injected")` line gated on `params.Traceparent != ""`).

- [ ] **Step 6: Rewrite `otlp_e2e_test.go`** to skip when no collector answers:

```go
package tracing

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestEndToEndOTLPExport verifies spans reach a real OTLP collector (Tempo
// from monitoring/docker-compose.yml). Skips when nothing listens on :4318.
func TestEndToEndOTLPExport(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "localhost:4318", 500*time.Millisecond)
	if err != nil {
		t.Skip("no OTLP collector on localhost:4318 — run `make observability-up`")
	}
	_ = conn.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	shutdown := InitFromEnv("bt-e2e-test")
	_, span := StartSpan(context.Background(), "e2e-test-span")
	span.SetAttribute("test", "true")
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("export/shutdown failed: %v", err)
	}
	SetGlobalTracer(noopTracer{})
}
```

- [ ] **Step 7: Build + full package tests**

Run: `PATH=/usr/local/go/bin:$PATH go build ./... && PATH=/usr/local/go/bin:$PATH go test ./internal/tracing/ ./internal/engine/ -count=1`
Expected: build OK; tracing tests pass (e2e passes if Task 1 stack is still up, otherwise SKIP); engine tests pass.

- [ ] **Step 8: Run `make check-quick`, then commit**

```bash
git add -A internal/tracing/ cmd/ internal/engine/mcp_server.go
PATH=/usr/local/go/bin:$PATH git commit -m "refactor(tracing): delete homegrown SDK internals; binaries + MCP use OTel facade"
```

---

### Task 4: Log correlation — trace-aware slog handler + OTLP log bridge

**Files:**
- Create: `internal/engine/log_correlation.go`
- Create: `internal/engine/log_correlation_test.go`
- Modify: `internal/engine/app_logger.go` (`Init` builds handler chain: correlation → fanout(file, otel-bridge))
- Modify: `go.mod` (log bridge deps)

**Interfaces:**
- Consumes: `tracing.SpanContextFrom(ctx)` (Task 2).
- Produces:
  - `newTraceContextHandler(next slog.Handler) slog.Handler` — injects `trace_id`/`span_id` attrs when the record's context carries a span.
  - `engine.InitLogExport(serviceName string) func(context.Context) error` — attaches the OTLP log bridge when `BT_OTLP_LOGS_ENDPOINT` is set (Loki OTLP ingest, e.g. `http://localhost:3100/otlp`); returns shutdown.
  - Context-aware logging: `engine.L().InfoContext(ctx, ...)` and all logs emitted through handlers gain correlation fields.

- [ ] **Step 1: Add log-bridge dependencies**

```bash
PATH=/usr/local/go/bin:$PATH go get \
  go.opentelemetry.io/otel/log@latest \
  go.opentelemetry.io/otel/sdk/log@latest \
  go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp@latest \
  go.opentelemetry.io/contrib/bridges/otelslog@latest
PATH=/usr/local/go/bin:$PATH go mod tidy
```

- [ ] **Step 2: Write the failing test `internal/engine/log_correlation_test.go`**

```go
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTraceContextHandler_InjectsTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTraceContextHandler(slog.NewJSONHandler(&buf, nil)))

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	tr := tracing.NewOTelTracer(tp.Tracer("test"))
	ctx, span := tr.StartSpan(context.Background(), "op")
	defer span.End()

	logger.InfoContext(ctx, "with span")
	logger.Info("without span")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var first, second map[string]any
	if err := json.Unmarshal(lines[0], &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(lines[1], &second); err != nil {
		t.Fatal(err)
	}
	if first["trace_id"] != span.SpanContext().TraceID {
		t.Fatalf("trace_id = %v, want %s", first["trace_id"], span.SpanContext().TraceID)
	}
	if first["span_id"] == "" || first["span_id"] == nil {
		t.Fatal("span_id missing")
	}
	if _, ok := second["trace_id"]; ok {
		t.Fatal("record without span ctx must not carry trace_id")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run TestTraceContextHandler -count=1`
Expected: FAIL — `undefined: newTraceContextHandler`.

- [ ] **Step 4: Write `internal/engine/log_correlation.go`**

```go
package engine

import (
	"context"
	"log/slog"

	"github.com/nico/go-bt-evolve/internal/tracing"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"net/url"
	"os"
)

// traceContextHandler injects trace_id/span_id from the record context so
// Loki lines correlate with Tempo spans.
type traceContextHandler struct{ next slog.Handler }

func newTraceContextHandler(next slog.Handler) slog.Handler {
	return &traceContextHandler{next: next}
}

func (h *traceContextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *traceContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc, ok := tracing.SpanContextFrom(ctx); ok {
		r.AddAttrs(slog.String("trace_id", sc.TraceID), slog.String("span_id", sc.SpanID))
	}
	return h.next.Handle(ctx, r)
}

func (h *traceContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceContextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *traceContextHandler) WithGroup(name string) slog.Handler {
	return &traceContextHandler{next: h.next.WithGroup(name)}
}

// fanoutHandler delivers every record to all children; the file handler is
// listed first and always receives the record (source of truth).
type fanoutHandler struct{ children []slog.Handler }

func (h *fanoutHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, c := range h.children {
		if c.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, c := range h.children {
		if c.Enabled(ctx, r.Level) {
			if err := c.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(h.children))
	for i, c := range h.children {
		out[i] = c.WithAttrs(attrs)
	}
	return &fanoutHandler{children: out}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(h.children))
	for i, c := range h.children {
		out[i] = c.WithGroup(name)
	}
	return &fanoutHandler{children: out}
}

// InitLogExport attaches an OTLP log bridge to the global logger when
// BT_OTLP_LOGS_ENDPOINT is set (Loki 3.x native OTLP ingest, e.g.
// http://localhost:3100/otlp). Returns a shutdown func; no-op when unset
// or on any setup error — file logging is never affected.
func InitLogExport(serviceName string) func(context.Context) error {
	noop := func(context.Context) error { return nil }
	endpoint := os.Getenv("BT_OTLP_LOGS_ENDPOINT")
	if endpoint == "" {
		return noop
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return noop
	}
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(u.Host),
		otlploghttp.WithURLPath(u.Path + "/v1/logs"),
	}
	if u.Scheme != "https" {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	exp, err := otlploghttp.New(context.Background(), opts...)
	if err != nil {
		return noop
	}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
	)
	bridge := otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(provider))
	attachLogHandler(bridge)
	return provider.Shutdown
}
```

- [ ] **Step 5: Rework `app_logger.go` handler chain.** Replace the body of `Init()`'s handler construction (the `logger = slog.New(...)` lines) so every path builds: `newTraceContextHandler(fanout(file/stderr JSON handler [, attached extras]))`. Add:

```go
var extraHandlers []slog.Handler

// attachLogHandler adds a handler (e.g. the OTLP bridge) to the global
// logger's fanout. Must be called after Init; rebuilds the logger.
func attachLogHandler(h slog.Handler) {
	mu.Lock()
	defer mu.Unlock()
	extraHandlers = append(extraHandlers, h)
	logger = nil // next L() rebuilds via Init path
	buildLogger()
}
```

Refactor `Init()` so the construction lives in `buildLogger()` (called under `mu`): it creates the base JSON handler (file+stderr MultiWriter exactly as today), wraps `fanoutHandler{children: append([]slog.Handler{base}, extraHandlers...)}` in `newTraceContextHandler`, and assigns `logger`. `Init()` becomes: if `logger != nil` return; `buildLogger()`.

- [ ] **Step 6: Run tests**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestTraceContextHandler' -count=1 -v && PATH=/usr/local/go/bin:$PATH go build ./...`
Expected: PASS; build OK.

- [ ] **Step 7: Wire `InitLogExport` into `cmd/bt-agent/main.go`** directly after the tracing init from Task 3:

```go
	logShutdown := engine.InitLogExport("bt-agent")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = logShutdown(ctx)
	}()
```

Same two lines (service names adjusted) in `cmd/bt-dashboard/main.go`, `cmd/bt-evaluator/main.go`, `cmd/bt-langagent/main.go`.

- [ ] **Step 8: check-quick, commit**

```bash
git add internal/engine/log_correlation.go internal/engine/log_correlation_test.go internal/engine/app_logger.go cmd/ go.mod go.sum
PATH=/usr/local/go/bin:$PATH git commit -m "feat(logging): trace-correlated slog handler + OTLP log bridge to Loki"
```

---

### Task 5: Run-scoped logger on the Blackboard

**Files:**
- Modify: `internal/engine/tree.go` (Blackboard gets `Logger *slog.Logger` field + `Log()` accessor)
- Modify: `internal/agent/runner.go` (RunOnce binds the run-scoped logger)
- Create: `internal/engine/blackboard_logger_test.go`

**Interfaces:**
- Consumes: `engine.L()` global logger.
- Produces: `bb.Log() *slog.Logger` — never nil; pre-bound with `run_id`, `agent`, `tree` when set by RunOnce. Engine actions use `bb.Log()` instead of `engine.L()` where a Blackboard is in scope.

- [ ] **Step 1: Write failing test `internal/engine/blackboard_logger_test.go`**

```go
package engine

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestBlackboardLog_FallsBackToGlobal(t *testing.T) {
	bb := &Blackboard{}
	if bb.Log() == nil {
		t.Fatal("Log() must never return nil")
	}
}

func TestBlackboardLog_UsesBoundLogger(t *testing.T) {
	var buf bytes.Buffer
	bb := &Blackboard{Logger: slog.New(slog.NewJSONHandler(&buf, nil)).With(
		"run_id", "r-1", "agent", "a-1", "tree", "t-1")}
	bb.Log().Info("hello")
	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["run_id"] != "r-1" || rec["agent"] != "a-1" || rec["tree"] != "t-1" {
		t.Fatalf("missing bound fields: %v", rec)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run TestBlackboardLog -count=1`
Expected: FAIL — `bb.Logger undefined` / `bb.Log undefined`.

- [ ] **Step 3: Implement.** In `internal/engine/tree.go`, add to the `Blackboard` struct (next to `TraceContext`):

```go
	Logger *slog.Logger `json:"-"` // run-scoped logger (run_id/agent/tree bound); use Log()
```

and below the struct:

```go
// Log returns the run-scoped logger when bound, else the global logger.
func (b *Blackboard) Log() *slog.Logger {
	if b.Logger != nil {
		return b.Logger
	}
	return L()
}
```

(add `"log/slog"` to tree.go imports). In `internal/agent/runner.go` `RunOnce`, directly after `result.RunID = runID` inside the `!opts.DisableBlackboard` block, add:

```go
		bb.Logger = engine.L().With("run_id", runID, "agent", agentName, "tree", result.TreeID)
```

- [ ] **Step 4: Run tests, check-quick, commit**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ ./internal/agent/ -count=1` → PASS.

```bash
git add internal/engine/tree.go internal/engine/blackboard_logger_test.go internal/agent/runner.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(logging): run-scoped logger bound to the Blackboard"
```

---

### Task 6: Per-node spans at the registry seam

**Files:**
- Modify: `internal/engine/registry.go:35-50` (`RegisterAction` / `RegisterCondition` wrap with tracing decorator)
- Create: `internal/engine/registry_tracing_test.go`

**Interfaces:**
- Consumes: `tracing.StartSpan`, `bb.TraceContext` (`internal/engine/tree.go:84`).
- Produces: every registered action/condition emits a span `bt.action/<name>` or `bt.condition/<name>` with attrs `bt.node.kind` (`action`|`condition`), `bt.node.name`, `bt.status` (action: `success`|`running`|`failure` from the int; condition: `true`|`false`). Parent context: `bb.TraceContext` when set. The span-bearing context is placed back into `bb.TraceContext` for the duration of the call so nested LLM spans nest correctly, then restored.

- [ ] **Step 1: Write failing test `internal/engine/registry_tracing_test.go`**

```go
package engine

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/tracing"
	btcore "github.com/rvitorper/go-bt/core"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func withRecordingGlobalTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })
	return rec
}

func TestRegisteredActionEmitsSpan(t *testing.T) {
	rec := withRecordingGlobalTracer(t)
	RegisterAction("TraceProbeAction", func(ctx *btcore.BTContext[Blackboard]) int { return 1 })
	bb := &Blackboard{TraceContext: context.Background()}
	status := GetAction("TraceProbeAction")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "bt.action/TraceProbeAction" {
		t.Fatalf("spans = %v", spans)
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs["bt.node.kind"] != "action" || attrs["bt.status"] != "success" {
		t.Fatalf("attrs = %v", attrs)
	}
}

func TestRegisteredConditionEmitsSpan(t *testing.T) {
	rec := withRecordingGlobalTracer(t)
	RegisterCondition("TraceProbeCondition", func(bb *Blackboard) bool { return false })
	bb := &Blackboard{}
	if GetCondition("TraceProbeCondition")(bb) {
		t.Fatal("want false")
	}
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "bt.condition/TraceProbeCondition" {
		t.Fatalf("spans = %v", spans)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ -run 'TestRegisteredActionEmitsSpan|TestRegisteredConditionEmitsSpan' -count=1`
Expected: FAIL (no spans recorded — decorator missing). Note: `tracing.GlobalTracer()` must exist from Task 3's trim; if it was package-private, export it there.

- [ ] **Step 3: Implement the decorator in `registry.go`.** Replace the two register functions:

```go
// RegisterAction adds an action to the global registry, wrapped in a tracing
// decorator: every invocation emits a span bt.action/<name> parented on
// bb.TraceContext. One seam instruments all current and future nodes.
func RegisterAction(name string, fn ActionFunc) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := actionRegistry[name]; exists {
		panic(fmt.Sprintf("action %q already registered", name))
	}
	actionRegistry[name] = tracedAction(name, fn)
}

// RegisterCondition adds a condition to the global registry, wrapped in a
// tracing decorator emitting bt.condition/<name> spans.
func RegisterCondition(name string, fn ConditionFunc) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, exists := conditionRegistry[name]; exists {
		panic(fmt.Sprintf("condition %q already registered", name))
	}
	conditionRegistry[name] = tracedCondition(name, fn)
}

func tracedAction(name string, fn ActionFunc) ActionFunc {
	return func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		parent := context.Background()
		if bb != nil && bb.TraceContext != nil {
			parent = bb.TraceContext
		}
		spanCtx, span := tracing.StartSpan(parent, "bt.action/"+name)
		span.SetAttribute("bt.node.kind", "action")
		span.SetAttribute("bt.node.name", name)
		var prev context.Context
		if bb != nil {
			prev = bb.TraceContext
			bb.TraceContext = spanCtx // nested spans (LLM calls) parent here
		}
		status := fn(ctx)
		if bb != nil {
			bb.TraceContext = prev
		}
		span.SetAttribute("bt.status", actionStatusString(status))
		span.End()
		return status
	}
}

func tracedCondition(name string, fn ConditionFunc) ConditionFunc {
	return func(bb *Blackboard) bool {
		parent := context.Background()
		if bb != nil && bb.TraceContext != nil {
			parent = bb.TraceContext
		}
		_, span := tracing.StartSpan(parent, "bt.condition/"+name)
		span.SetAttribute("bt.node.kind", "condition")
		span.SetAttribute("bt.node.name", name)
		result := fn(bb)
		if result {
			span.SetAttribute("bt.status", "true")
		} else {
			span.SetAttribute("bt.status", "false")
		}
		span.End()
		return result
	}
}

func actionStatusString(status int) string {
	switch {
	case status > 0:
		return "success"
	case status == 0:
		return "running"
	default:
		return "failure"
	}
}
```

Add imports `"context"` and `"github.com/nico/go-bt-evolve/internal/tracing"` to registry.go.

- [ ] **Step 4: Run the new tests AND the full engine suite** (the decorator wraps every registered node — the whole suite exercises it with the default noop tracer):

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/engine/ ./internal/domains/ -count=1`
Expected: all PASS. If any test registered duplicate names or asserts on raw registry contents, fix the test expectations (behavior of the functions is unchanged; only spans are added).

- [ ] **Step 5: check-quick, commit**

```bash
git add internal/engine/registry.go internal/engine/registry_tracing_test.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(tracing): per-node spans via registry decorator"
```

---

### Task 7: Run root span + scheduler webhook span

**Files:**
- Modify: `internal/agent/runner.go` (RunOnce root span; set `bb.TraceContext`)
- Modify: `internal/agent/scheduler.go` (webhook publish span; DLQ note)
- Modify: `cmd/bt-agent/main.go` (DLQ push span event)
- Create: `internal/agent/runner_tracing_test.go`

**Interfaces:**
- Consumes: `tracing.StartSpan`, registry decorator behavior (Task 6): node spans parent on `bb.TraceContext`.
- Produces: span `agent.run/<agentName>` wrapping tree execution with attrs `run_id`, `agent`, `tree`, `outcome`; every node span nests under it. Scheduler emits `agent.webhook_publish` spans. DLQ pushes emit `agent.dlq_push` spans with agent/task attrs.

- [ ] **Step 1: Write failing test `internal/agent/runner_tracing_test.go`**

```go
package agent

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// RunOnce against a minimal registry-backed tree must produce a root span
// agent.run/<name> and child node spans sharing its trace id.
func TestRunOnce_EmitsRunRootSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	deps := newTestRunDeps(t) // reuse the existing runner test fixture helper in runner_test.go; if named differently, use that name
	_, _ = deps.RunOnce(nil, "trace-test-agent", "do nothing", RunOptions{})

	var root sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "agent.run/trace-test-agent" {
			root = s
		}
	}
	if root == nil {
		t.Fatalf("no agent.run root span; spans: %v", rec.Ended())
	}
}
```

(Adapt the fixture line to the actual helper used in `internal/agent/runner_test.go` — the test file already constructs `RunDeps` with a registry, tree resolver, and MockLLM; reuse that construction verbatim. If no helper exists, copy the `RunDeps` construction from the first test in `runner_test.go`.)

- [ ] **Step 2: Run to verify failure**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/agent/ -run TestRunOnce_EmitsRunRootSpan -count=1`
Expected: FAIL — no root span.

- [ ] **Step 3: Implement in `runner.go` RunOnce.** Directly before `_ = engine.RunTask(bb, bt)`:

```go
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	spanCtx, runSpan := tracing.StartSpan(runCtx, "agent.run/"+agentName)
	runSpan.SetAttribute("run_id", result.RunID)
	runSpan.SetAttribute("agent", agentName)
	runSpan.SetAttribute("tree", result.TreeID)
	bb.TraceContext = spanCtx
	_ = engine.RunTask(bb, bt)
	runSpan.SetAttribute("outcome", bb.Outcome)
	if bb.Outcome != "success" && bb.Outcome != "" {
		runSpan.RecordError(fmt.Errorf("agent outcome: %s", bb.Outcome))
	}
	runSpan.End()
```

(replacing the existing bare `_ = engine.RunTask(bb, bt)` line; add the `tracing` import).

- [ ] **Step 4: Webhook span in `scheduler.go`.** In `runJob`, wrap the `GlobalAgentBus.Publish(...)` call:

```go
		_, whSpan := tracing.StartSpan(runCtx.Context, "agent.webhook_publish")
		whSpan.SetAttribute("agent", job.AgentName)
		whSpan.SetAttribute("event_type", eventType)
		GlobalAgentBus.Publish(AgentEvent{
			// ... existing fields unchanged ...
		})
		whSpan.End()
```

DLQ span in `cmd/bt-agent/main.go` around the existing `dlq.Push(...)`:

```go
		if err != nil {
			_, dlqSpan := tracing.StartSpan(ctx.Context, "agent.dlq_push")
			dlqSpan.SetAttribute("agent", ctx.AgentName)
			dlqSpan.RecordError(err)
			dlq.Push(reliability.DeadLetterEntry{
				// ... existing fields unchanged ...
			})
			dlqSpan.End()
		}
```

- [ ] **Step 5: Run tests, check-quick, commit**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/agent/ -count=1` → PASS.

```bash
git add internal/agent/ cmd/bt-agent/main.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(tracing): run root span, webhook and DLQ spans"
```

---

### Task 8: LLM call spans

**Files:**
- Create: `internal/llm/traced.go`
- Create: `internal/llm/traced_test.go`
- Modify: `internal/agent/runner.go` (stack the traced wrapper around the error recorder)

**Interfaces:**
- Consumes: `tracing.StartSpan`, `reliability.RetryAfterFromError`, `llm.LLM` interface (`internal/llm/ollama.go:55-62`), `llm.ErrorRecorder` (Task exists already).
- Produces: `llm.NewTracedLLM(client LLM, provider string) LLM` — spans named `llm.generate/<provider>` with attrs `llm.provider`, `llm.error_class` (from `reliability.ClassifyError`), `llm.retry_after` (when rate-limited). Kept a separate type from `ErrorRecorder` (one concern each; `RunOnce` stacks them).

- [ ] **Step 1: Write failing test `internal/llm/traced_test.go`**

```go
package llm

import (
	"errors"
	"testing"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestTracedLLM_EmitsSpanWithErrorClass(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	traced := NewTracedLLM(&stubLLM{name: "s", err: errors.New("rate limit exceeded")}, "stub")
	if _, err := traced.Generate("p"); err == nil {
		t.Fatal("expected error")
	}
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "llm.generate/stub" {
		t.Fatalf("spans = %v", spans)
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	if attrs["llm.error_class"] != "rate_limited" {
		t.Fatalf("error_class = %q, want rate_limited", attrs["llm.error_class"])
	}
}

func TestTracedLLM_SuccessSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	traced := NewTracedLLM(&stubLLM{name: "ok"}, "stub")
	if _, err := traced.Generate("p"); err != nil {
		t.Fatal(err)
	}
	if len(rec.Ended()) != 1 {
		t.Fatalf("want 1 span, got %d", len(rec.Ended()))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/llm/ -run TestTracedLLM -count=1`
Expected: FAIL — `undefined: NewTracedLLM`.

- [ ] **Step 3: Write `internal/llm/traced.go`**

```go
package llm

import (
	"context"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/tracing"
)

// TracedLLM decorates an LLM with spans per Generate* call. Kept separate
// from ErrorRecorder — one concern each; RunOnce stacks them.
type TracedLLM struct {
	LLM
	provider string
}

// NewTracedLLM wraps client; provider labels the span (e.g. "fallback-chain").
func NewTracedLLM(client LLM, provider string) *TracedLLM {
	return &TracedLLM{LLM: client, provider: provider}
}

func (t *TracedLLM) span(ctx context.Context) (context.Context, tracing.Span) {
	spanCtx, span := tracing.StartSpan(ctx, "llm.generate/"+t.provider)
	span.SetAttribute("llm.provider", t.provider)
	return spanCtx, span
}

func (t *TracedLLM) finish(span tracing.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetAttribute("llm.error_class", reliability.ClassifyError(err).String())
		if ra := reliability.RetryAfterFromError(err); ra > 0 {
			span.SetAttribute("llm.retry_after", ra.String())
		}
	}
	span.End()
}

func (t *TracedLLM) Generate(prompt string) (string, error) {
	_, span := t.span(context.Background())
	result, err := t.LLM.Generate(prompt)
	t.finish(span, err)
	return result, err
}

func (t *TracedLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	spanCtx, span := t.span(ctx)
	result, err := t.LLM.GenerateCtx(spanCtx, prompt)
	t.finish(span, err)
	return result, err
}

func (t *TracedLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	_, span := t.span(context.Background())
	span.SetAttribute("llm.timeout", timeout.String())
	result, err := t.LLM.GenerateWithTimeout(prompt, timeout)
	t.finish(span, err)
	return result, err
}

var _ LLM = (*TracedLLM)(nil)
```

- [ ] **Step 4: Stack it in `runner.go`.** In the recorder block from the rate-limit work, change `llmForRun = llmRecorder` to:

```go
		llmForRun = llm.NewTracedLLM(llmRecorder, "agent-llm")
```

(the recorder stays innermost so it still sees raw client errors; spans wrap the whole call including fallback chains).

- [ ] **Step 5: Run tests, check-quick, commit**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/llm/ ./internal/agent/ -count=1` → PASS.

```bash
git add internal/llm/traced.go internal/llm/traced_test.go internal/agent/runner.go
PATH=/usr/local/go/bin:$PATH git commit -m "feat(tracing): LLM call spans with error class and retry-after"
```

---

### Task 9: Migrate raw log.Printf files to structured logging

**Files:**
- Modify: `internal/a2a/server.go`, `internal/agent/scheduler.go`, `internal/agent/scheduler_cb.go`, `internal/agent/webhook_publisher.go`, `internal/benchmark/benchmark.go`, `internal/config/config.go`, `internal/config/watcher.go`, `internal/gardener/evolve_v2.go`, `internal/gardener/gardener.go`, `internal/gardener/validation_gate.go`, `internal/reliability/panic_handler.go`, `internal/reliability/reliability.go` (12 files; `cmd/bt-otlp-collector` is deleted in Task 10)
- Modify: `cmd/bt-agent/main.go`, `cmd/bt-dashboard/main.go`, `cmd/bt-evaluator/main.go`, `cmd/bt-langagent/main.go`, `cmd/bt-gardener/main.go` (add `slog.SetDefault(engine.L())` right after `engine.Init()` or first logger use)

**Interfaces:**
- Produces: zero `log.Printf`/`log.Println` in `internal/` (verified by grep). Library packages use stdlib `log/slog` package-level functions (`slog.Info` etc.) — they inherit the engine handler via `slog.SetDefault`, avoiding engine import cycles (reliability/config must NOT import engine).

- [ ] **Step 1: Wire the default logger in the five binaries.** In each `main()`, immediately after logging init (or first statement if none):

```go
	engine.Init()
	slog.SetDefault(engine.L())
```

(`cmd/bt-gardener` — check whether it calls `engine.Init()` already; add if missing. Add `"log/slog"` imports.)

- [ ] **Step 2: Migrate mechanically, file by file.** Transformation rules:

| Before | After |
|---|---|
| `log.Printf("Scheduler: agent %q panicked in runJob (recovered): %v", job.AgentName, r)` | `slog.Error("scheduler: agent panicked in runJob (recovered)", "agent", job.AgentName, "panic", r)` |
| `log.Printf("watcher: reload failed: %v", err)` | `slog.Warn("watcher: reload failed", "error", err)` |
| `log.Println("...")` (informational) | `slog.Info("...")` |
| `log.Fatalf(...)` in `internal/` | `slog.Error(...)` followed by `return err` / existing error path — never `os.Exit` from a library |

Choose the level by intent: errors → `Error`, recoverable/degraded → `Warn`, lifecycle → `Info`. Move format verbs into structured key-value fields; keep the message constant. Remove the `"log"` import from each file once clean; keep `log.Fatalf` only in `cmd/` main() functions.

- [ ] **Step 3: Verify zero remaining**

Run: `grep -rn "log\.Printf\|log\.Println" --include="*.go" internal/ | grep -v _test`
Expected: no output.

- [ ] **Step 4: Full test suite**

Run: `PATH=/usr/local/go/bin:$PATH go test ./... -short -count=1 2>&1 | grep -v "^ok"`
Expected: only `[no test files]` lines and the known env-dependent skips.

- [ ] **Step 5: check-quick, commit**

```bash
git add internal/ cmd/
PATH=/usr/local/go/bin:$PATH git commit -m "refactor(logging): migrate raw log.Printf to structured slog"
```

---

### Task 10: Remove bt-otlp-collector, changelog, end-to-end verification

**Files:**
- Delete: `cmd/bt-otlp-collector/` (entire directory)
- Modify: `CHANGELOG.md` (Unreleased entries)
- Modify: any references — run the survey step below

**Interfaces:**
- Consumes: the full pipeline from Tasks 1-9.
- Produces: spec success criteria verified against the live stack.

- [ ] **Step 1: Survey references**

```bash
grep -rn "bt-otlp-collector\|otlp-collector\|BT_OTLP_COLLECTOR" --include="*.go" --include="Makefile" --include="*.sh" --include="*.md" --include="*.yml" . | grep -v graphify-out | grep -v docs/superpowers
```
Remove/update every hit (AGENTS.md, docs, scripts). The `BINARIES` list in Makefile/check.sh does not include it (verified during planning) — but confirm.

- [ ] **Step 2: Delete the collector**

```bash
git rm -r cmd/bt-otlp-collector
```

- [ ] **Step 3: CHANGELOG entries** under `## [Unreleased]`:

```markdown
### Added
- **(observability):** Local Grafana stack (Tempo + Loki + Grafana) via `monitoring/docker-compose.yml` with provisioned trace↔log correlation and a BT Agent Runs dashboard; `make observability-up/down`.
- **(observability):** OTel-Go SDK behind the `internal/tracing` facade; per-node spans via the action/condition registry, `agent.run` root spans, LLM call spans, webhook/DLQ spans; slog gains trace_id/span_id correlation, a run-scoped logger, and an OTLP log bridge to Loki (`BT_OTLP_LOGS_ENDPOINT`).

### Removed
- **(observability):** Homegrown tracing internals (console tracer, OTLP exporter, batcher, W3C parser, trace reader) and `cmd/bt-otlp-collector` — superseded by the OTel SDK and Tempo.

### Changed
- **(logging):** All library packages log through structured slog (`log.Printf` eliminated from `internal/`); binaries `slog.SetDefault(engine.L())`.
```

- [ ] **Step 4: End-to-end verification against the spec's success criteria**

```bash
make observability-up && sleep 20
PATH=/usr/local/go/bin:$PATH go test ./internal/tracing/ -run TestEndToEndOTLPExport -count=1 -v   # must PASS (not skip)
# Fire one real run through the pipeline:
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 BT_OTLP_LOGS_ENDPOINT=http://localhost:3100/otlp \
  PATH=/usr/local/go/bin:$PATH go run ./cmd/bt-agent-cli run doormate-analyst --task "smoke observability" 2>/dev/null || true
sleep 10
# Trace arrived in Tempo:
curl -s "http://localhost:3200/api/search?tags=service.name%3Dbt-agent-cli&limit=5" | grep -q traceID && echo TRACE-IN-TEMPO
# Logs arrived in Loki:
curl -s -G "http://localhost:3100/loki/api/v1/query_range" --data-urlencode 'query={service_name=~"bt-.*"}' | grep -q '"values"' && echo LOGS-IN-LOKI
```
Expected: `TRACE-IN-TEMPO` and `LOGS-IN-LOKI`. (If `bt-agent-cli run` syntax differs, use any agent listed by `bt-agent-cli list`; the goal is one real RunOnce through the exporters.)

- [ ] **Step 5: Full gate + commit**

```bash
make check-full
git add -A
PATH=/usr/local/go/bin:$PATH git commit -m "feat(observability): remove superseded collector; verify Tempo/Loki pipeline end-to-end"
```

---

## Self-Review Notes

- **Spec coverage:** stack (T1), SDK-behind-facade + deletion + e2e skip (T2/T3), log correlation + OTLP bridge + file fanout (T4), run-scoped logger (T5), per-node spans (T6), run root/webhook/DLQ spans (T7), LLM spans (T8), log.Printf migration (T9), collector removal + success criteria (T10). Activation gating covered in T2 (`InitFromEnv`) and T4 (`InitLogExport`). MCP stdout constraint: no task writes to stdout.
- **Known execution risks called out in-task:** fixture name in T7 test (adapt to runner_test.go), `GlobalTracer()` export check in T6, gardener `engine.Init()` presence in T9, `bt-agent-cli run` syntax in T10.
