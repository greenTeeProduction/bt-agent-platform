package dashboard

import (
	"context"
	"fmt"
	"strings"
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

func TestEvaluateCondition_ExactTrueMatchOnly(t *testing.T) {
	state := &wfState{prev: map[string]StepResult{}}

	cases := []struct {
		name string
		cond string
		want bool
	}{
		{"exact true", "true", true},
		{"prefix truest", "truest", false},
		{"prefix true with trailing text", "true but only partially", false},
		{"prefix truthfully", "truthfully, that is correct", false},
		{"exact false", "false", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateCondition(tc.cond, state)
			if got != tc.want {
				t.Errorf("evaluateCondition(%q) = %v, want %v", tc.cond, got, tc.want)
			}
		})
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

func TestWorkflow_ParallelSubStepPanicRecovered(t *testing.T) {
	runner := &Runner{
		RunAgent: func(ctx context.Context, agentName, _, _ string) (string, string, error) {
			if agentName == "panicking-agent" {
				panic("boom: simulated sub-step panic")
			}
			time.Sleep(5 * time.Millisecond)
			return "success", fmt.Sprintf("%s done", agentName), nil
		},
	}

	wf := Pipeline{
		Name: "test-parallel-panic",
		Steps: []Step{
			{
				ID:   "parallel-step",
				Kind: StepParallel,
				Steps: []Step{
					{ID: "a", Kind: StepAgent, Agent: "agent-a", Input: "task a"},
					{ID: "b", Kind: StepAgent, Agent: "panicking-agent", Input: "task b"},
					{ID: "c", Kind: StepAgent, Agent: "agent-c", Input: "task c"},
				},
			},
		},
	}

	result, err := runner.Run(context.Background(), wf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("expected 1 top-level step (the parallel step), got %d", len(result.Steps))
	}

	parallelResult := result.Steps[0]
	if parallelResult.Outcome != "partial" {
		t.Errorf("expected parallel step outcome 'partial' (one sub-step errored), got %s", parallelResult.Outcome)
	}
	// The panicking sub-step must not crash the process, and every sibling
	// (including the panicking one) must still be represented in results.
	if !strings.Contains(parallelResult.Output, "agent-a done") {
		t.Errorf("expected sibling 'a' result present in output, got: %s", parallelResult.Output)
	}
	if !strings.Contains(parallelResult.Output, "agent-c done") {
		t.Errorf("expected sibling 'c' result present in output, got: %s", parallelResult.Output)
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

// TestWorkflow_ApprovalEscalatedIsDistinctAndHalts guards against an HITL
// escalation being conflated with either a genuine approval or a plain
// rejection. An escalated approval must get its own "escalated" outcome
// (not "success"/"approved", not silently reused as "rejected") and must
// still halt the pipeline so no side-effecting step downstream of the gate
// runs on an un-reviewed escalation.
func TestWorkflow_ApprovalEscalatedIsDistinctAndHalts(t *testing.T) {
	neverReached := false
	runner := &Runner{
		RunAgent: func(ctx context.Context, agentName, _, _ string) (string, string, error) {
			if agentName == "never-reached" {
				neverReached = true
			}
			return "success", "ok", nil
		},
		WaitApproval: func(ctx context.Context, step Step, state *wfState) (ApprovalWaitResult, error) {
			return ApprovalWaitResult{Escalated: true, TaskID: "wf:test-escalate:approve:1", RequestID: "hitl-escalated"}, nil
		},
	}

	wf := Pipeline{
		Name: "test-escalate",
		Steps: []Step{
			{ID: "approve", Kind: StepApproval, Input: "confirm"},
			{ID: "side-effect", Kind: StepAgent, Agent: "never-reached", Input: "task"},
		},
	}

	result, err := runner.Run(context.Background(), wf, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Steps[0].Outcome != "escalated" {
		t.Errorf("expected approval step outcome %q, got %q", "escalated", result.Steps[0].Outcome)
	}
	if result.Steps[0].Output == "approved" {
		t.Errorf("escalated approval must not report Output %q", "approved")
	}
	if result.Steps[0].Error == "" {
		t.Errorf("expected a non-empty error on an escalated approval step")
	}
	if result.Outcome != "failure" {
		t.Errorf("expected pipeline outcome %q on escalation, got %q", "failure", result.Outcome)
	}
	if len(result.Steps) != 1 {
		t.Errorf("expected pipeline to halt after the escalated approval step, got %d steps", len(result.Steps))
	}
	if neverReached {
		t.Errorf("side-effecting step must not run after an escalated approval")
	}
}
