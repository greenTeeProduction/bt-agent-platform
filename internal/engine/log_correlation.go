package engine

import (
	"context"
	"log/slog"
	"os"
	"path"
	"sync"

	"github.com/nico/go-bt-evolve/internal/tracing"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
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

// logExportTarget derives the OTLP/HTTP log exporter target from an
// endpoint string, reusing the tracing package's endpoint normalization
// (scheme-less "host:port" endpoints assume http, trailing slashes are
// trimmed). urlPath is the custom path prefix joined with the OTLP
// "v1/logs" suffix, or empty when the endpoint carries no path (the
// exporter then uses its default /v1/logs). ok is false when the endpoint
// is empty or unparseable.
func logExportTarget(endpoint string) (host, urlPath string, insecure, ok bool) {
	host, prefix, insecure, ok := tracing.ParseOTLPTarget(endpoint)
	if !ok {
		return "", "", false, false
	}
	if prefix != "" {
		urlPath = path.Join(prefix, "v1/logs")
	}
	return host, urlPath, insecure, true
}

var (
	logExportMu          sync.Mutex
	logExportInitialized bool
)

// InitLogExport attaches an OTLP log bridge to the global logger when
// BT_OTLP_LOGS_ENDPOINT is set (Loki 3.x native OTLP ingest, e.g.
// http://localhost:3100/otlp). Returns a shutdown func; no-op when unset
// or on any setup error — file logging is never affected. Idempotent:
// a second call warns and returns a no-op instead of double-attaching
// the bridge.
func InitLogExport(serviceName string) func(context.Context) error {
	noop := func(context.Context) error { return nil }
	endpoint := os.Getenv("BT_OTLP_LOGS_ENDPOINT")
	if endpoint == "" {
		return noop
	}
	host, urlPath, insecure, ok := logExportTarget(endpoint)
	if !ok {
		slog.Warn("log export disabled: invalid OTLP logs endpoint", "endpoint", endpoint)
		return noop
	}

	logExportMu.Lock()
	defer logExportMu.Unlock()
	if logExportInitialized {
		slog.Warn("log export already initialized")
		return noop
	}

	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(host)}
	if urlPath != "" {
		opts = append(opts, otlploghttp.WithURLPath(urlPath))
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	exp, err := otlploghttp.New(context.Background(), opts...)
	if err != nil {
		slog.Warn("log export disabled: OTLP exporter init failed", "endpoint", endpoint, "error", err)
		return noop
	}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)),
		sdklog.WithResource(sdkresource.NewSchemaless(attribute.String("service.name", serviceName))),
	)
	bridge := otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(provider))
	attachLogHandler(bridge)
	logExportInitialized = true
	return provider.Shutdown
}
