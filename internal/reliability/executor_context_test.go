package reliability

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ctxRecordingExecutor records the context each Execute call receives and can
// cancel a shared context mid-attempt to simulate the caller's deadline
// expiring while an attempt is in flight.
type ctxRecordingExecutor struct {
	name    string
	calls   int
	lastCtx context.Context
	cancel  context.CancelFunc // when set, fired during Execute
	res     *AgentResult
	err     error
}

func (c *ctxRecordingExecutor) Execute(ctx context.Context, agent, task string) (*AgentResult, error) {
	c.calls++
	c.lastCtx = ctx
	if c.cancel != nil {
		c.cancel()
	}
	return c.res, c.err
}
func (c *ctxRecordingExecutor) Health() error  { return nil }
func (c *ctxRecordingExecutor) String() string { return c.name }

// The scheduler runs every attempt under a job deadline, but the router/
// executor seam dropped the context (Execute(agent, task)), so a routed
// attempt could not observe the deadline at all — on 2026-07-22 the 22:37
// cycle ran 2h29m against a 2h job ctx, blind-retrying healthy work into
// false DLQ entry #239. The seam must carry the caller's context down to the
// executor.
func TestAgentRouterExecute_PropagatesCallerContext(t *testing.T) {
	deadline := time.Now().Add(90 * time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	exec := &ctxRecordingExecutor{name: "local", res: &AgentResult{Agent: "a", Success: true, Outcome: "success"}}
	r := NewAgentRouter(exec)

	if _, err := r.Execute(ctx, "a", "task"); err != nil {
		t.Fatal(err)
	}
	if exec.calls != 1 {
		t.Fatalf("calls = %d, want 1", exec.calls)
	}
	if exec.lastCtx == nil {
		t.Fatal("executor received no context")
	}
	got, ok := exec.lastCtx.Deadline()
	if !ok || !got.Equal(deadline) {
		t.Fatalf("executor saw deadline (%v, %v), want the caller's %v — the router must pass the caller's context through", got, ok, deadline)
	}
}

// Once the caller's context is done, the failover loop must stop: trying the
// next executor — or re-trying the adopted local fallback — after the job
// deadline has passed burns another full attempt the scheduler will already
// classify as failed.
func TestAgentRouterExecute_StopsFailoverWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := &ctxRecordingExecutor{name: "first", cancel: cancel, err: errors.New("boom")}
	second := &ctxRecordingExecutor{name: "second", res: &AgentResult{Success: true, Outcome: "success"}}
	r := NewAgentRouter(first, second)

	res, err := r.Execute(ctx, "a", "task")
	if err == nil {
		t.Fatalf("want an error once the context died mid-failover, got result %+v", res)
	}
	if first.calls != 1 {
		t.Fatalf("first.calls = %d, want exactly 1 — the dead context must also skip the local-fallback re-try of the same executor", first.calls)
	}
	if second.calls != 0 {
		t.Fatalf("second executor was tried after the caller's context was already done (calls=%d); failover must stop", second.calls)
	}
}
