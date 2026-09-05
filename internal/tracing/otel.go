package tracing

import (
	"context"
	"log/slog"
	"net/url"
	"os"
	"path"
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

// ParseOTLPTarget normalizes an OTLP endpoint into exporter target parts.
// Endpoints without a scheme (bare "host:port") are treated as http://.
// urlPath is the endpoint's custom path prefix (reverse-proxy setups) with
// trailing slashes trimmed and WITHOUT any signal suffix — callers join
// their own "v1/traces" / "v1/logs" suffix. It is empty when the endpoint
// carries no path. insecure is true for any scheme other than https.
// ok is false when the endpoint is empty or unparseable.
func ParseOTLPTarget(endpoint string) (host, urlPath string, insecure, ok bool) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", "", false, false
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "http://" + endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", "", false, false
	}
	return u.Host, strings.TrimRight(u.Path, "/"), u.Scheme != "https", true
}

// parseOTLPEndpoint is the trace-specific wrapper around ParseOTLPTarget:
// when the endpoint carries a custom path prefix, the returned urlPath is
// already joined with the OTLP "v1/traces" suffix.
func parseOTLPEndpoint(endpoint string) (host, urlPath string, insecure, ok bool) {
	host, prefix, insecure, ok := ParseOTLPTarget(endpoint)
	if !ok {
		return "", "", false, false
	}
	if prefix != "" {
		urlPath = path.Join(prefix, "v1/traces")
	}
	return host, urlPath, insecure, true
}

// InitFromEnv installs an OTel-SDK-backed global tracer when an OTLP endpoint
// is configured and returns a shutdown func. Without an endpoint the global
// noop tracer stays installed and shutdown is a no-op. Export failures are
// dropped by the SDK batcher — telemetry never blocks a run. Misconfigured
// endpoints disable tracing with a warning instead of failing the run.
func InitFromEnv(serviceName string) func(context.Context) error {
	noop := func(context.Context) error { return nil }
	endpoint := Endpoint()
	if endpoint == "" {
		return noop
	}
	host, urlPath, insecure, ok := parseOTLPEndpoint(endpoint)
	if !ok {
		slog.Warn("tracing disabled: invalid OTLP endpoint", "endpoint", endpoint)
		return noop
	}
	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(host)}
	if insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	if urlPath != "" {
		opts = append(opts, otlptracehttp.WithURLPath(urlPath))
	}
	exp, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		slog.Warn("tracing disabled: OTLP exporter init failed", "endpoint", endpoint, "error", err)
		return noop
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
