package main

import (
	"context"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
)

// The local executor must thread the router's per-attempt context into
// RunOnce — it previously passed context.Background(), detaching every
// scheduled attempt from its job deadline: the 2026-07-22 22:37 cycle's
// attempts ran 2h29m against a 2h job ctx (attempt 2 kept running 28 minutes
// past the deadline), then blind-retried into false DLQ entry #239.
func TestNewLocalAgentExecutor_ThreadsContextIntoRunOnce(t *testing.T) {
	var gotCtx context.Context
	var gotOpts agent.RunOptions
	exec := newLocalAgentExecutor("node", func(ctx context.Context, agentName, task string, opts agent.RunOptions) (*agent.RunResult, error) {
		gotCtx = ctx
		gotOpts = opts
		return &agent.RunResult{AgentName: agentName, Task: task, Outcome: "success", Output: "ok"}, nil
	})

	deadline := time.Now().Add(45 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	res, err := exec.Execute(ctx, "goap-fusion-loop-runner", "cycle")
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Outcome != "success" {
		t.Fatalf("result = %+v, want success outcome", res)
	}
	if gotCtx == nil {
		t.Fatal("RunOnce received no context")
	}
	d, ok := gotCtx.Deadline()
	if !ok || !d.Equal(deadline) {
		t.Fatalf("RunOnce saw deadline (%v, %v), want the caller's %v — the executor must not detach attempts from the job deadline", d, ok, deadline)
	}
	if !gotOpts.InjectMemory || !gotOpts.EnforceQuality {
		t.Fatalf("RunOptions lost through the seam: %+v", gotOpts)
	}
}
