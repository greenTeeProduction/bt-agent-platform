package agent

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// The webhook span published from runJob must join the run's trace (via the
// traceparent carried on RunResult) instead of rooting a new trace. See
// internal/agent/runner_tracing_test.go for the analogous run-root-span check.
func TestRunJob_WebhookSpanJoinsRunTrace(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	prevBus := GlobalAgentBus
	InitAgentBus(10)
	t.Cleanup(func() { GlobalAgentBus = prevBus })

	// Derive a well-formed, known TraceID/SpanID from a real started span,
	// as a stand-in for the run's root span context.
	_, runRootSpan := tracing.StartSpan(context.Background(), "agent.run/webhook-trace-agent")
	runSC := runRootSpan.SpanContext()
	runRootSpan.End()

	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "webhook-trace-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	hist, err := NewHistory(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: time.Hour,
	})

	job := &ScheduledJob{
		ID:        "job_webhook-trace-agent_test",
		AgentName: "webhook-trace-agent",
		Schedule:  "every 1h",
		Timeout:   "30s",
	}

	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "did the thing", &RunResult{
			AgentName: ctx.AgentName,
			Outcome:   "success",
			TraceID:   runSC.TraceID,
			SpanID:    runSC.SpanID,
		}, nil
	}

	sched.runJob(job, runner)

	var webhookSpan sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "agent.webhook_publish" {
			webhookSpan = s
		}
	}
	if webhookSpan == nil {
		t.Fatalf("no agent.webhook_publish span recorded; spans: %v", rec.Ended())
	}

	// Trace-parent extraction produces a remote parent, so the webhook span's
	// own SpanID differs from the run's — only the TraceID must match.
	gotTraceID := webhookSpan.SpanContext().TraceID().String()
	if gotTraceID != runSC.TraceID {
		t.Fatalf("webhook span trace id = %s, want %s (run's trace id)", gotTraceID, runSC.TraceID)
	}
}
