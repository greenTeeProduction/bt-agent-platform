package agent

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
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

	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	deps := &RunDeps{
		Registry: reg,
		LLM:      engine.NewMockLLM(),
		ResolveTree: func(_ string) *evolution.SerializableNode {
			return evolution.DefaultTree()
		},
	}

	_, _ = deps.RunOnce(context.Background(), "trace-test-agent", "do nothing", RunOptions{})

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
