package a2a

import (
	"context"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
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

// TestExecuteAgentNameFromCtxWinsOverContextID pins the agent-name resolution
// contract: handleAgentEndpoint carries the target agent name via ctx
// (agentNameKey), not by constructing anything per-request keyed off
// ContextID. execCtx.ContextID — the SDK's server-generated correlation id,
// validated on every emitted event — must be left untouched, and must NOT be
// used to resolve the agent when ctx already carries a name.
func TestExecuteAgentNameFromCtxWinsOverContextID(t *testing.T) {
	SetTreeResolver(func(string) *evolution.SerializableNode { return nil })
	reg, err := agent.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(agent.Definition{Name: "some-agent", Tree: "domain:code_review", Description: "test agent"}); err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	exec := &BTAgentExecutor{
		Reg:     reg,
		TreeMap: map[string]*evolution.SerializableNode{"some-agent": {Type: "AlwaysSucceed"}},
	}

	ctx := context.WithValue(context.Background(), agentNameKey{}, "some-agent")
	execCtx := &a2asrv.ExecutorContext{
		Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("run something")),
		ContextID: "sdk-generated-id",
	}

	var terminal a2a.TaskState
	for ev, err := range exec.Execute(ctx, execCtx) {
		if err != nil {
			t.Fatalf("Execute yielded error: %v", err)
		}
		if su, ok := ev.(*a2a.TaskStatusUpdateEvent); ok {
			terminal = su.Status.State
		}
	}

	if execCtx.ContextID != "sdk-generated-id" {
		t.Fatalf("ContextID overwritten to %q — SDK event validation will fail with 'context IDs don't match'", execCtx.ContextID)
	}
	if terminal == a2a.TaskStateFailed {
		t.Fatalf("Execute failed — agent name must resolve from ctx (%q), not fall back to ContextID (%q), which is not a registered agent", "some-agent", execCtx.ContextID)
	}
}
