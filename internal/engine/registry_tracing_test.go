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
