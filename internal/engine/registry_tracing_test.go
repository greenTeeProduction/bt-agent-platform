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

// TestPanickingActionRestoresTraceContextAndEndsSpan guards the decorator's
// panic path: RunTask recovers panics above this frame and keeps using the
// same Blackboard, so the decorator must restore bb.TraceContext and End the
// span unconditionally (via defer). Otherwise every subsequent node parents
// under a dead, never-flushed span.
func TestPanickingActionRestoresTraceContextAndEndsSpan(t *testing.T) {
	rec := withRecordingGlobalTracer(t)
	RegisterAction("TracePanicAction", func(_ *btcore.BTContext[Blackboard]) int {
		panic("boom")
	})
	root := context.Background()
	bb := &Blackboard{TraceContext: root}

	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		GetAction("TracePanicAction")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	}()

	if !panicked {
		t.Fatal("panic did not propagate through the decorator")
	}
	if bb.TraceContext != root {
		t.Fatal("bb.TraceContext not restored to pre-call value after panic")
	}
	found := false
	for _, s := range rec.Ended() {
		if s.Name() == "bt.action/TracePanicAction" {
			found = true
		}
	}
	if !found {
		t.Fatalf("span bt.action/TracePanicAction not ended after panic; ended spans = %v", rec.Ended())
	}
}

// TestNestedActionSpansParentCorrectly simulates DelegateToTree-style
// re-entry: action A's fn invokes action B's wrapped fn synchronously on the
// SAME Blackboard. B's span must parent under A's span (via bb.TraceContext),
// and after A completes bb.TraceContext must be back to the original root.
func TestNestedActionSpansParentCorrectly(t *testing.T) {
	rec := withRecordingGlobalTracer(t)

	RegisterAction("TraceNestInnerB", func(_ *btcore.BTContext[Blackboard]) int {
		return 1
	})
	RegisterAction("TraceNestOuterA", func(ctx *btcore.BTContext[Blackboard]) int {
		// Re-enter the registry seam on the same Blackboard, as
		// DelegateToTreeFn does when running a sub-tree.
		return GetAction("TraceNestInnerB")(&btcore.BTContext[Blackboard]{Blackboard: ctx.Blackboard})
	})

	root := context.Background()
	bb := &Blackboard{TraceContext: root}
	status := GetAction("TraceNestOuterA")(&btcore.BTContext[Blackboard]{Blackboard: bb})
	if status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
	if bb.TraceContext != root {
		t.Fatal("bb.TraceContext not restored to root after nested calls")
	}

	spans := rec.Ended()
	var outer, inner sdktrace.ReadOnlySpan
	for _, s := range spans {
		switch s.Name() {
		case "bt.action/TraceNestOuterA":
			outer = s
		case "bt.action/TraceNestInnerB":
			inner = s
		}
	}
	if outer == nil || inner == nil {
		t.Fatalf("missing expected spans, got %d spans", len(spans))
	}
	if inner.Parent().SpanID() != outer.SpanContext().SpanID() {
		t.Fatalf("inner parent SpanID = %v, want outer SpanID = %v",
			inner.Parent().SpanID(), outer.SpanContext().SpanID())
	}
}
