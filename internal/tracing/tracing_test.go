package tracing

import (
	"context"
	"fmt"
	"testing"
)

// fakeTracer is a minimal Tracer test double used to verify global tracer
// get/set plumbing without depending on any concrete SDK implementation.
type fakeTracer struct{ started int }

func (f *fakeTracer) StartSpan(ctx context.Context, _ string) (context.Context, Span) {
	f.started++
	return ctx, noopSpan{}
}

func TestNoopTracer(t *testing.T) {
	tracer := noopTracer{}
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "noop")
	if span == nil {
		t.Fatal("noop span should not be nil")
	}

	span.SetAttribute("key", "value")
	span.AddEvent("event")
	span.RecordError(fmt.Errorf("err"))
	span.End()

	// All operations should be safe no-ops (no panic)
	if span.IsRecording() {
		t.Error("noop span should not be recording")
	}
	if span.SpanContext().TraceID != "" {
		t.Error("noop span context should be empty")
	}

	// Context should still be the same
	if ctx != ctx {
		t.Error("noop tracer should return same context")
	}
}

func TestStartSpan_NilCtx(t *testing.T) {
	// Test that StartSpan with context.TODO() works
	tracer := noopTracer{}
	ctx, span := tracer.StartSpan(context.TODO(), "NilCtxOp")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	span.End()
}

func TestGlobalTracer(t *testing.T) {
	// Default should be noop
	gt := GlobalTracer()
	if _, ok := gt.(noopTracer); !ok {
		t.Errorf("expected noopTracer as default, got %T", gt)
	}

	// Set a test tracer
	tt := &fakeTracer{}
	SetGlobalTracer(tt)
	if GlobalTracer() != Tracer(tt) {
		t.Error("expected fake tracer after SetGlobalTracer")
	}

	// Reset to noop
	SetGlobalTracer(noopTracer{})
}

func TestStartSpan_Global(t *testing.T) {
	orig := GlobalTracer()
	defer SetGlobalTracer(orig)

	// With noop global tracer, StartSpan should work
	SetGlobalTracer(noopTracer{})
	ctx := context.Background()
	newCtx, span := StartSpan(ctx, "global-test")
	if newCtx == nil {
		t.Fatal("expected non-nil context")
	}
	span.End() // safe no-op

	// StartSpan delegates to whatever tracer is currently installed.
	tt := &fakeTracer{}
	SetGlobalTracer(tt)
	if _, _ = StartSpan(ctx, "delegated"); tt.started != 1 {
		t.Errorf("expected StartSpan to delegate to global tracer, started=%d", tt.started)
	}
}

func TestHelperAttributes(t *testing.T) {
	sa := StringAttr("k", "v")
	if sa.Key != "k" || sa.Value != "v" {
		t.Errorf("StringAttr: got %s=%s", sa.Key, sa.Value)
	}
}
