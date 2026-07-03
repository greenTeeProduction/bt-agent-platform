package a2a

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
)

// TestExecuteFirstEventIsTask pins the a2a-go v2 stream contract: when no
// StoredTask exists yet, the FIRST yielded event must be an *a2a.Task (or a
// message) — the SDK's taskupdate manager rejects a leading
// TaskStatusUpdateEvent with "first event must be a Task or a message" and
// every SendMessage call fails instantly with INVALID_AGENT_RESPONSE while
// the tree still executes in the background (observed live 2026-07-02).
func TestExecuteFirstEventIsTask(t *testing.T) {
	exec := &BTAgentExecutor{}
	// Empty ContextID → the executor fails with "no agent specified", but the
	// submitted Task must still be yielded first so the failure becomes a
	// well-formed task-state transition rather than a protocol violation.
	execCtx := &a2asrv.ExecutorContext{
		Message: a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run something")),
	}

	var events []a2a.Event
	for ev, err := range exec.Execute(context.Background(), execCtx) {
		if err != nil {
			t.Fatalf("Execute yielded error: %v", err)
		}
		events = append(events, ev)
		if len(events) >= 2 {
			break
		}
	}
	if len(events) == 0 {
		t.Fatal("Execute yielded no events")
	}
	task, ok := events[0].(*a2a.Task)
	if !ok {
		t.Fatalf("first event = %T, want *a2a.Task (SDK rejects a leading status update)", events[0])
	}
	if task.Status.State != a2a.TaskStateSubmitted {
		t.Fatalf("first Task state = %q, want submitted", task.Status.State)
	}
	if len(events) > 1 {
		if _, ok := events[1].(*a2a.TaskStatusUpdateEvent); !ok {
			t.Fatalf("second event = %T, want *a2a.TaskStatusUpdateEvent", events[1])
		}
	}
}

// TestInterceptorCarriesNameWithoutTouchingContextID pins the interceptor
// contract: agent name travels via ctx; execCtx.ContextID (the SDK's
// correlation id, validated on every event) is left alone.
func TestInterceptorCarriesNameWithoutTouchingContextID(t *testing.T) {
	ic := &agentNameInterceptor{name: "some-agent"}
	execCtx := &a2asrv.ExecutorContext{ContextID: "sdk-generated-id"}
	ctx, err := ic.Intercept(context.Background(), execCtx)
	if err != nil {
		t.Fatalf("Intercept error: %v", err)
	}
	if execCtx.ContextID != "sdk-generated-id" {
		t.Fatalf("ContextID overwritten to %q — SDK event validation will fail with 'context IDs don't match'", execCtx.ContextID)
	}
	if name, _ := ctx.Value(agentNameKey{}).(string); name != "some-agent" {
		t.Fatalf("ctx agent name = %q, want some-agent", name)
	}
}
