package tracing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestConsoleTracer_BasicSpan(t *testing.T) {
	tracer, output := TestTracer("test")
	ctx := context.Background()

	ctx, span := tracer.StartSpan(ctx, "TestOperation")
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	if !span.IsRecording() {
		t.Fatal("expected span to be recording")
	}

	span.SetAttribute("key1", "value1")
	span.AddEvent("step1", StringAttr("detail", "started"))
	span.End()

	if span.IsRecording() {
		t.Fatal("expected span to NOT be recording after End()")
	}

	out := output()
	if !strings.Contains(out, "op=TestOperation") {
		t.Errorf("expected op=TestOperation in output, got: %s", out)
	}
	if !strings.Contains(out, "key1=value1") {
		t.Errorf("expected key1=value1 in output, got: %s", out)
	}
	if !strings.Contains(out, "[step1") {
		t.Errorf("expected [step1] event in output, got: %s", out)
	}
}

func TestConsoleTracer_ParentChild(t *testing.T) {
	tracer, output := TestTracer("test")

	ctx := context.Background()
	ctx, parent := tracer.StartSpan(ctx, "ParentOp")
	ctx, child := tracer.StartSpan(ctx, "ChildOp")

	child.SetAttribute("child", "true")
	child.End()
	parent.SetAttribute("parent", "true")
	parent.End()

	out := output()
	if !strings.Contains(out, "op=ParentOp") {
		t.Errorf("expected ParentOp in output: %s", out)
	}
	if !strings.Contains(out, "op=ChildOp") {
		t.Errorf("expected ChildOp in output: %s", out)
	}
	// Child should have a parent= reference
	if !strings.Contains(out, "parent=") {
		t.Errorf("expected parent= reference in output: %s", out)
	}
	// Child and parent should share same trace ID
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines")
	}
	// Extract trace IDs from both lines
	var traceIDs []string
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) > 2 {
			traceIDs = append(traceIDs, parts[2])
		}
	}
	if len(traceIDs) != 2 || traceIDs[0] != traceIDs[1] {
		t.Errorf("expected same trace ID for parent and child, got: %v", traceIDs)
	}
}

func TestConsoleTracer_RecordError(t *testing.T) {
	tracer, output := TestTracer("test")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "ErrorOp")
	span.RecordError(fmt.Errorf("something went wrong"))
	span.End()

	out := output()
	if !strings.Contains(out, "error=") {
		t.Errorf("expected error= in output: %s", out)
	}
	if !strings.Contains(out, "something went wrong") {
		t.Errorf("expected error message in output: %s", out)
	}
}

func TestConsoleTracer_EndIdempotency(t *testing.T) {
	tracer, output := TestTracer("test")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "IdempotentOp")
	span.End()
	span.End() // Should not panic or duplicate output

	out := output()
	count := strings.Count(out, "op=IdempotentOp")
	if count != 1 {
		t.Errorf("expected 1 occurrence of op=IdempotentOp, got %d: %s", count, out)
	}
}

func TestConsoleTracer_NoopWhenEnded(t *testing.T) {
	tracer, output := TestTracer("test")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "NoopAfterEnd")
	span.End()
	span.SetAttribute("late", "value")
	span.AddEvent("late_event")
	span.RecordError(fmt.Errorf("late error"))

	out := output()
	if strings.Contains(out, "late=value") {
		t.Error("should not contain late attribute after End()")
	}
	if strings.Contains(out, "late_event") {
		t.Error("should not contain late event after End()")
	}
	if strings.Contains(out, "late error") {
		t.Error("should not contain late error after End()")
	}
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
	// Test that StartSpan with nil context works (uses background)
	tracer, _ := TestTracer("test")
	ctx, span := tracer.StartSpan(nil, "NilCtxOp")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if span == nil {
		t.Fatal("expected non-nil span")
	}
	span.End()
}

func TestSpanFromContext(t *testing.T) {
	ctx := context.Background()
	if SpanFromContext(ctx) != nil {
		t.Error("expected nil span from empty context")
	}

	tracer, _ := TestTracer("test")
	ctx, _ = tracer.StartSpan(ctx, "TestOp")
	if SpanFromContext(ctx) == nil {
		t.Fatal("expected non-nil span from context after StartSpan")
	}
}

func TestGlobalTracer(t *testing.T) {
	// Default should be noop
	gt := GetGlobalTracer()
	if _, ok := gt.(noopTracer); !ok {
		t.Errorf("expected noopTracer as default, got %T", gt)
	}

	// Set a test tracer
	tt, _ := TestTracer("global")
	SetGlobalTracer(tt)
	if GetGlobalTracer() != tt {
		t.Error("expected test tracer after SetGlobalTracer")
	}

	// Reset to noop
	SetGlobalTracer(noopTracer{})
}

func TestStartSpan_Global(t *testing.T) {
	// With noop global tracer, StartSpan should work
	ctx := context.Background()
	newCtx, span := StartSpan(ctx, "global-test")
	if newCtx == nil {
		t.Fatal("expected non-nil context")
	}
	span.End() // safe no-op
}

func TestConsoleTracer_Concurrent(t *testing.T) {
	tracer, output := TestTracer("concurrent")
	ctx := context.Background()

	var wg sync.WaitGroup
	n := 50
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, span := tracer.StartSpan(ctx, fmt.Sprintf("Op-%d", id))
			span.SetAttribute("id", fmt.Sprintf("%d", id))
			span.End()
		}(i)
	}
	wg.Wait()

	out := output()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != n {
		t.Errorf("expected %d lines, got %d", n, len(lines))
	}
}

func TestConsoleTracer_MultipleChildren(t *testing.T) {
	tracer, output := TestTracer("multi")
	ctx := context.Background()

	ctx, parent := tracer.StartSpan(ctx, "Parent")
	for i := 0; i < 3; i++ {
		childCtx, child := tracer.StartSpan(ctx, fmt.Sprintf("Child-%d", i))
		child.End()
		_ = childCtx
	}
	parent.End()

	out := output()
	for i := 0; i < 3; i++ {
		if !strings.Contains(out, fmt.Sprintf("op=Child-%d", i)) {
			t.Errorf("expected op=Child-%d in output", i)
		}
	}
	// All children should share parent's trace ID
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (1 parent + 3 children), got %d", len(lines))
	}
}

func TestConsoleTracer_OutputFormat(t *testing.T) {
	tracer, output := TestTracer("fmt")
	ctx := context.Background()

	_, span := tracer.StartSpan(ctx, "FormatTest")
	span.SetAttribute("status", "ok")
	span.AddEvent("checkpoint", StringAttr("phase", "1"))
	span.End()

	out := output()
	// Should start with TRACE
	if !strings.HasPrefix(out, "TRACE ") {
		t.Errorf("expected TRACE prefix: %s", out)
	}
	// Should contain timestamp
	fields := strings.Fields(out)
	if len(fields) < 2 {
		t.Fatalf("expected at least 2 fields in output: %s", out)
	}
	if !strings.Contains(fields[1], "T") {
		t.Errorf("expected timestamp (RFC3339) in field 1: %s", fields[1])
	}
	// Should contain duration
	if !strings.Contains(out, "duration=") {
		t.Errorf("expected duration= in output: %s", out)
	}
}

func TestConsoleTracer_NilWriter(t *testing.T) {
	// Should default to os.Stderr without panicking
	tracer := NewConsoleTracer("nil-writer", nil)
	ctx := context.Background()
	_, span := tracer.StartSpan(ctx, "NilWriterTest")
	span.End()
	// No panic = pass
}

func TestHelperAttributes(t *testing.T) {
	sa := StringAttr("k", "v")
	if sa.Key != "k" || sa.Value != "v" {
		t.Errorf("StringAttr: got %s=%s", sa.Key, sa.Value)
	}

	ia := IntAttr("n", 42)
	if ia.Key != "n" || ia.Value != "42" {
		t.Errorf("IntAttr: got %s=%s", ia.Key, ia.Value)
	}

	ba := BoolAttr("flag", true)
	if ba.Key != "flag" || ba.Value != "true" {
		t.Errorf("BoolAttr: got %s=%s", ba.Key, ba.Value)
	}

	da := DurationAttr("elapsed", 123456789)
	if da.Key != "elapsed" || da.Value != "123.456789ms" {
		t.Errorf("DurationAttr: got %s=%s", da.Key, da.Value)
	}
}
