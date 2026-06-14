package dashboard

import (
	"context"
	"sync"
	"testing"
	"time"
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
