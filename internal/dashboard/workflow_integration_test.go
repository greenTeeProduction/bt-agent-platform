package dashboard

import (
	"context"
	"fmt"
	"testing"
)

// ─── Integration Scenarios ───

func TestPipeline_IncidentResponse(t *testing.T) {
	steps := []string{}
	runner := &Runner{
		RunAgent: func(agentName, _, _ string) (string, string, error) {
			steps = append(steps, agentName)
			return "success", fmt.Sprintf("%s result", agentName), nil
		},
	}

	pipeline := Pipeline{
		Name: "incident-response",
		Steps: []Step{
			{ID: "detect", Kind: StepAgent, Agent: "monitor", Input: "check health", OnFailure: "abort"},
			{ID: "diagnose", Kind: StepAgent, Agent: "researcher", Input: "diagnose: {{.prev.detect.output}}", OnFailure: "skip"},
			{ID: "notify", Kind: StepAgent, Agent: "router", Input: "alert: {{.prev.diagnose.output}}", OnFailure: "skip"},
			{ID: "fix", Kind: StepAgent, Agent: "reviewer", Input: "fix: {{.prev.diagnose.output}}", OnFailure: "retry"},
		},
	}

	result, err := runner.Run(context.Background(), pipeline, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "success" {
		t.Errorf("expected success, got %s", result.Outcome)
	}
	if len(steps) != 4 {
		t.Errorf("expected 4 steps executed, got %d", len(steps))
	}
}

func TestPipeline_SkipOnFailure(t *testing.T) {
	runner := &Runner{
		RunAgent: func(agentName, _, _ string) (string, string, error) {
			if agentName == "flaky" {
				return "failure", "failed", fmt.Errorf("oops")
			}
			return "success", "ok", nil
		},
	}

	pipeline := Pipeline{
		Name: "skip-test",
		Steps: []Step{
			{ID: "first", Kind: StepAgent, Agent: "good", Input: "do"},
			{ID: "flaky", Kind: StepAgent, Agent: "flaky", Input: "try", OnFailure: "skip"},
			{ID: "last", Kind: StepAgent, Agent: "good", Input: "finish"},
		},
	}

	result, _ := runner.Run(context.Background(), pipeline, "")
	if result.Outcome != "success" {
		t.Errorf("expected success when skipping failure, got %s", result.Outcome)
	}
}

func TestPipeline_ConditionalGate(t *testing.T) {
	runner := &Runner{
		RunAgent: func(_, _, _ string) (string, string, error) {
			return "success", "degraded", nil
		},
	}

	pipeline := Pipeline{
		Name: "conditional-gate",
		Steps: []Step{
			{ID: "check", Kind: StepAgent, Agent: "monitor", Input: "status"},
			{ID: "gate", Kind: StepCondition, Condition: "{{.prev.check.output}} == degraded"},
		},
	}

	result, _ := runner.Run(context.Background(), pipeline, "")
	if result.Steps[1].Outcome != "success" {
		t.Errorf("gate should pass when condition matches, got %s", result.Steps[1].Outcome)
	}
}

func TestPipeline_ConditionalGateFails(t *testing.T) {
	runner := &Runner{
		RunAgent: func(_, _, _ string) (string, string, error) {
			return "success", "healthy", nil
		},
	}

	pipeline := Pipeline{
		Name: "conditional-gate-fail",
		Steps: []Step{
			{ID: "check", Kind: StepAgent, Agent: "monitor", Input: "status"},
			{ID: "gate", Kind: StepCondition, Condition: "{{.prev.check.output}} == degraded"},
		},
	}

	result, _ := runner.Run(context.Background(), pipeline, "")
	if result.Steps[1].Outcome != "skipped" {
		t.Errorf("gate should skip when condition not met, got %s", result.Steps[1].Outcome)
	}
}

func TestPipeline_ApprovalStep(t *testing.T) {
	runner := &Runner{
		RunAgent: func(_, _, _ string) (string, string, error) {
			return "success", "ok", nil
		},
	}

	pipeline := Pipeline{
		Name: "approval-test",
		Steps: []Step{
			{ID: "work", Kind: StepAgent, Agent: "worker", Input: "do"},
			{ID: "approve", Kind: StepApproval, Input: "Please approve the result"},
		},
	}

	result, _ := runner.Run(context.Background(), pipeline, "")
	if result.Steps[1].Outcome != "pending_approval" {
		t.Errorf("approval step should be pending, got %s", result.Steps[1].Outcome)
	}
}

func TestPipeline_UnknownStepKind(t *testing.T) {
	runner := &Runner{
		RunAgent: func(_, _, _ string) (string, string, error) {
			return "success", "ok", nil
		},
	}

	pipeline := Pipeline{
		Name: "unknown-kind",
		Steps: []Step{
			{ID: "bad", Kind: "nonexistent_kind", Agent: "test", Input: "test"},
		},
	}

	result, err := runner.Run(context.Background(), pipeline, "")
	if err != nil {
		t.Log("unknown kind error:", err)
	}
	if result.Steps[0].Outcome == "success" {
		t.Error("unknown step kind should not succeed")
	}
}

func TestPipeline_ParallelMixedResults(t *testing.T) {
	runner := &Runner{
		RunAgent: func(agentName, _, _ string) (string, string, error) {
			if agentName == "failing" {
				return "failure", "failed", fmt.Errorf("err")
			}
			return "success", "ok", nil
		},
	}

	pipeline := Pipeline{
		Name: "parallel-mixed",
		Steps: []Step{
			{
				ID:   "parallel-step",
				Kind: StepParallel,
				Steps: []Step{
					{ID: "a", Kind: StepAgent, Agent: "good", Input: "task"},
					{ID: "b", Kind: StepAgent, Agent: "failing", Input: "task"},
				},
			},
		},
	}

	result, _ := runner.Run(context.Background(), pipeline, "")
	if result.Steps[0].Outcome != "partial" {
		t.Errorf("parallel with mixed results should be partial, got %s", result.Steps[0].Outcome)
	}
}

func TestPipeline_LoopMaxIterations(t *testing.T) {
	count := 0
	runner := &Runner{
		RunAgent: func(_, _, _ string) (string, string, error) {
			count++
			return "success", fmt.Sprintf("iter-%d", count), nil
		},
	}

	pipeline := Pipeline{
		Name: "loop-max",
		Steps: []Step{
			{
				ID:            "loop-step",
				Kind:          StepLoop,
				MaxIterations: 2,
				Steps: []Step{
					{ID: "body", Kind: StepAgent, Agent: "worker", Input: "work"},
				},
			},
		},
	}

	result, _ := runner.Run(context.Background(), pipeline, "")
	if result.Outcome != "success" {
		t.Errorf("expected success, got %s", result.Outcome)
	}
	if count != 2 {
		t.Errorf("expected 2 iterations, got %d", count)
	}
}
