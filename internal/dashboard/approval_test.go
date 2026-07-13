package dashboard

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/hitl"
)

func TestWorkflow_ApprovalWaiter(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
			return "success", "work done", nil
		},
		WaitApproval: func(ctx context.Context, step Step, state *wfState) (ApprovalWaitResult, error) {
			if step.ID != "approve" {
				t.Fatalf("unexpected step %s", step.ID)
			}
			if state.input != "work done" {
				t.Fatalf("expected prior output in state, got %q", state.input)
			}
			return ApprovalWaitResult{Approved: true, TaskID: "wf:test:approve:1", RequestID: "hitl-test"}, nil
		},
	}

	result, err := runner.Run(context.Background(), Pipeline{
		Name: "approval-wait",
		Steps: []Step{
			{ID: "work", Kind: StepAgent, Agent: "worker", Input: "go"},
			{ID: "approve", Kind: StepApproval, Input: "confirm {{.prev.work.output}}"},
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "success" {
		t.Fatalf("expected success, got %s", result.Outcome)
	}
	if result.Steps[1].Outcome != "success" {
		t.Fatalf("approval step outcome: %s", result.Steps[1].Outcome)
	}
}

func TestWorkflowApprovalWait_EscalatedIsNotApproved(t *testing.T) {
	dir := t.TempDir()
	store, err := hitl.InitStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}

	origPolicy := hitl.GetPolicy()
	hitl.SetPolicy(hitl.Policy{Enabled: true, AutoApprove: false, Timeout: 5 * time.Second, DefaultPrompt: "test"})
	defer hitl.SetPolicy(origPolicy)

	step := Step{ID: "approve", Kind: StepApproval, Input: "confirm"}
	state := &wfState{input: "proposed", workflow: "wf-escalate-test", runID: "run1", prev: map[string]StepResult{}}
	taskID := WorkflowApprovalTaskID(state.workflow, step.ID, state.runID)

	type waitOutcome struct {
		result ApprovalWaitResult
		err    error
	}
	done := make(chan waitOutcome, 1)
	go func() {
		result, err := WorkflowApprovalWait(context.Background(), step, state)
		done <- waitOutcome{result, err}
	}()

	var req *hitl.Request
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r, ok := store.FindPendingByTaskID(taskID); ok {
			req = r
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if req == nil {
		t.Fatal("timed out waiting for pending HITL request to appear")
	}
	if _, err := store.Escalate(req.ID, "ops", "needs review"); err != nil {
		t.Fatalf("escalate: %v", err)
	}

	select {
	case out := <-done:
		if out.err != nil {
			t.Fatalf("WorkflowApprovalWait returned error: %v", out.err)
		}
		if out.result.Approved {
			t.Fatal("expected Approved == false for an escalated HITL request")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for WorkflowApprovalWait to return")
	}
}

func TestWorkflow_ParallelStateIsolation(t *testing.T) {
	var mu sync.Mutex
	writes := make(map[string]int)

	runner := &Runner{
		RunAgent: func(ctx context.Context, agentName, _, _ string) (string, string, error) {
			mu.Lock()
			writes[agentName]++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			return "success", agentName + "-out", nil
		},
	}

	result, err := runner.Run(context.Background(), Pipeline{
		Name: "parallel-iso",
		Steps: []Step{
			{
				ID:   "parallel-step",
				Kind: StepParallel,
				Steps: []Step{
					{ID: "a", Kind: StepAgent, Agent: "agent-a", Input: "{{.input}}"},
					{ID: "b", Kind: StepAgent, Agent: "agent-b", Input: "{{.input}}"},
				},
			},
		},
	}, "shared-input")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "success" {
		t.Fatalf("expected success, got %s", result.Outcome)
	}
	if writes["agent-a"] != 1 || writes["agent-b"] != 1 {
		t.Fatalf("expected each agent once, got %v", writes)
	}
}
