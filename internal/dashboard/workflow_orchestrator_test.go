package dashboard

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestWorkflow_Sequential(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, agentName, _, task string) (string, string, error) {
			return "success", fmt.Sprintf("agent %q completed task: %s", agentName, task), nil
		},
	}

	wf := Pipeline{
		Name: "test-sequential",
		Steps: []Step{
			{ID: "step1", Kind: StepAgent, Agent: "agent-a", Input: "task one"},
			{ID: "step2", Kind: StepAgent, Agent: "agent-b", Input: "task two: {{.prev.step1.output}}"},
		},
	}

	result, err := runner.Run(context.Background(), wf, "initial")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected success, got %s", result.Outcome)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(result.Steps))
	}
	if result.Steps[1].Output != `agent "agent-b" completed task: task two: agent "agent-a" completed task: task one` {
		t.Errorf("template expansion failed, got: %s", result.Steps[1].Output)
	}
}

func TestWorkflow_Conditional(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
			return "success", "degraded", nil
		},
	}

	wf := Pipeline{
		Name: "test-conditional",
		Steps: []Step{
			{ID: "check", Kind: StepAgent, Agent: "monitor", Input: "check health"},
			{ID: "gate", Kind: StepCondition, Condition: "{{.prev.check.output}} == degraded"},
		},
	}

	result, err := runner.Run(context.Background(), wf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected success, got %s", result.Outcome)
	}
	if result.Steps[1].Outcome != "success" {
		t.Errorf("condition should have passed, got: %s", result.Steps[1].Outcome)
	}
}

func TestWorkflow_Parallel(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, agentName, _, _ string) (string, string, error) {
			time.Sleep(10 * time.Millisecond)
			return "success", fmt.Sprintf("%s done", agentName), nil
		},
	}

	wf := Pipeline{
		Name: "test-parallel",
		Steps: []Step{
			{
				ID:   "parallel-step",
				Kind: StepParallel,
				Steps: []Step{
					{ID: "a", Kind: StepAgent, Agent: "agent-a", Input: "task a"},
					{ID: "b", Kind: StepAgent, Agent: "agent-b", Input: "task b"},
					{ID: "c", Kind: StepAgent, Agent: "agent-c", Input: "task c"},
				},
			},
		},
	}

	result, err := runner.Run(context.Background(), wf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected success, got %s", result.Outcome)
	}
}

func TestWorkflow_OnFailureAbort(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, agentName, _, _ string) (string, string, error) {
			if agentName == "failing-agent" {
				return "failure", "error output", fmt.Errorf("simulated failure")
			}
			return "success", "ok", nil
		},
	}

	wf := Pipeline{
		Name: "test-abort",
		Steps: []Step{
			{ID: "good", Kind: StepAgent, Agent: "good-agent", Input: "task", OnFailure: "abort"},
			{ID: "bad", Kind: StepAgent, Agent: "failing-agent", Input: "task", OnFailure: "abort"},
			{ID: "never", Kind: StepAgent, Agent: "never-reached", Input: "task"},
		},
	}

	result, err := runner.Run(context.Background(), wf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "failure" {
		t.Errorf("expected failure, got %s", result.Outcome)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 steps (aborted before 3rd), got %d", len(result.Steps))
	}
}

func TestWorkflow_Loop(t *testing.T) {
	iterations := 0
	runner := &Runner{
		RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
			iterations++
			return "success", fmt.Sprintf("iteration %d", iterations), nil
		},
	}

	wf := Pipeline{
		Name: "test-loop",
		Steps: []Step{
			{
				ID:            "loop-step",
				Kind:          StepLoop,
				MaxIterations: 3,
				Condition:     "{{.prev.loop-body.output}} == iteration 3",
				Steps: []Step{
					{ID: "loop-body", Kind: StepAgent, Agent: "looper", Input: "do work"},
				},
			},
		},
	}

	result, err := runner.Run(context.Background(), wf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected success, got %s", result.Outcome)
	}
}

func TestWorkflow_StepTimeout(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
			select {
			case <-ctx.Done():
				return "timeout", "", ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return "success", "done", nil
			}
		},
	}

	wf := Pipeline{
		Name: "test-timeout",
		Steps: []Step{
			{ID: "slow", Kind: StepAgent, Agent: "slow-agent", Input: "work", Timeout: "50ms", OnFailure: "abort"},
		},
	}

	result, _ := runner.Run(context.Background(), wf, "")
	if result.Outcome != "failure" {
		t.Errorf("expected failure outcome, got %s", result.Outcome)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(result.Steps))
	}
	if result.Steps[0].Outcome != "timeout" {
		t.Errorf("expected timeout step outcome, got %s", result.Steps[0].Outcome)
	}
}

func TestWorkflow_RunIDPropagation(t *testing.T) {
	externalID := "abc123run"
	runner := &Runner{
		RunID: externalID,
		RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
			return "success", "ok", nil
		},
	}
	result, err := runner.Run(context.Background(), Pipeline{Name: "rid-test", Steps: []Step{
		{ID: "s1", Kind: StepAgent, Agent: "a", Input: "go"},
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID != externalID {
		t.Fatalf("expected run_id %q, got %q", externalID, result.RunID)
	}
}

func TestWorkflow_OnFailureRetry(t *testing.T) {
	attempts := 0
	runner := &Runner{
		RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
			attempts++
			if attempts == 1 {
				return "failure", "first attempt failed", fmt.Errorf("fail")
			}
			return "success", "retry succeeded", nil
		},
	}

	wf := Pipeline{
		Name: "test-retry",
		Steps: []Step{
			{ID: "retry-step", Kind: StepAgent, Agent: "flaky-agent", Input: "task", OnFailure: "retry"},
		},
	}

	result, _ := runner.Run(context.Background(), wf, "")
	if result.Steps[0].Outcome != "success" {
		t.Errorf("expected success after retry, got %s", result.Steps[0].Outcome)
	}
}
