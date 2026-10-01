package dashboard

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestWorkflowLocalApprovalStopsEveryContainer(t *testing.T) {
	for _, container := range []string{"sequence", "loop", "subworkflow", "parallel"} {
		for _, policy := range []string{"retry", "skip"} {
			t.Run(container+"/"+policy, func(t *testing.T) {
				var calls atomic.Int64
				runner := &Runner{RunID: "review-run", RunAgent: func(context.Context, string, string, string) (string, string, error) {
					calls.Add(1)
					return "success", "unapproved operation", nil
				}}
				approve := Step{ID: "approve", Kind: StepApproval, OnFailure: policy}
				rootStep := approve
				switch container {
				case "loop":
					rootStep = Step{ID: "loop", Kind: StepLoop, MaxIterations: 3, Steps: []Step{{ID: "check", Kind: StepCondition, Condition: "true"}, approve, {ID: "body-later", Kind: StepAgent}}}
				case "subworkflow":
					rootStep = Step{ID: "nested", Kind: StepSubworkflow, Steps: []Step{approve, {ID: "body-later", Kind: StepAgent}}}
				case "parallel":
					rootStep = Step{ID: "parallel", Kind: StepParallel, OnFailure: policy, Steps: []Step{approve, {ID: "check", Kind: StepCondition, Condition: "false"}}}
				}
				steps := []Step{rootStep, {ID: "later", Kind: StepAgent}}
				result, err := runner.Run(t.Context(), Pipeline{Name: "review", Steps: steps}, "fixture")
				if calls.Load() != 0 || result.Outcome != "pending_approval" || !reliability.IsExecutionPause(result.Outcome, err) {
					t.Fatalf("calls=%d result=%+v err=%v", calls.Load(), result, err)
				}
				if !strings.Contains(result.Steps[0].Output, "wf:review:approve:review-run") {
					t.Fatalf("approval owner evidence missing: %+v", result)
				}
			})
		}
	}
}

func TestWorkflowApprovalRejectionCannotBeSkippedOrRetried(t *testing.T) {
	for _, policy := range []string{"skip", "retry"} {
		t.Run(policy, func(t *testing.T) {
			decisions, operations := 0, 0
			runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
				operations++
				return "success", "unapproved operation", nil
			}, WaitApproval: func(context.Context, Step, *wfState) (ApprovalWaitResult, error) {
				decisions++
				return ApprovalWaitResult{TaskID: "owned-approval", RequestID: "owned-request"}, nil
			}}
			result, err := runner.Run(t.Context(), Pipeline{Name: "rejection", Steps: []Step{{ID: "approval", Kind: StepApproval, OnFailure: policy}, {ID: "later", Kind: StepAgent}}}, "fixture")
			if decisions != 1 || operations != 0 || result.Outcome != "rejected" || !reliability.IsExecutionStoppedError(err) || result.Steps[0].HitlRequestID != "owned-request" {
				t.Fatalf("decisions=%d operations=%d result=%+v err=%v", decisions, operations, result, err)
			}
		})
	}
}

func TestWorkflowRawAgentWaitCannotReplayOrRunFollowingStep(t *testing.T) {
	calls := 0
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		calls++
		return "auth-required", "owner needs authentication", nil
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "raw-wait", Steps: []Step{{ID: "wait", Kind: StepAgent, OnFailure: "retry"}, {ID: "later", Kind: StepAgent}}}, "fixture")
	if calls != 1 || result.Outcome != "auth-required" || !reliability.IsExecutionPause(result.Outcome, err) {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestWorkflowLoopExecutesEveryBodyStep(t *testing.T) {
	var tasks []string
	runner := &Runner{RunAgent: func(_ context.Context, name, _, task string) (string, string, error) {
		tasks = append(tasks, name+":"+task)
		return "success", name + " result", nil
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "body", Steps: []Step{{ID: "loop", Kind: StepLoop, MaxIterations: 2, Steps: []Step{{ID: "first", Kind: StepAgent, Agent: "first", Input: "{{.input}}"}, {ID: "second", Kind: StepAgent, Agent: "second", Input: "{{.prev.first.output}}"}}}}}, "initial")
	if err != nil || result.Outcome != "success" || len(tasks) != 4 || tasks[1] != "second:first result" || tasks[3] != "second:first result" {
		t.Fatalf("tasks=%v result=%+v err=%v", tasks, result, err)
	}
}

func TestWorkflowOrdinaryParallelFaultCannotClaimSuccess(t *testing.T) {
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		return "failure", "fault evidence", errors.New("ordinary fault")
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "partial", Steps: []Step{{ID: "parallel", Kind: StepParallel, Steps: []Step{{ID: "fault", Kind: StepAgent}}}}}, "fixture")
	if err != nil || result.Outcome != "partial" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestWorkflowApprovalTimeoutPreservesRequestAndStopsRetry(t *testing.T) {
	decisions, operations := 0, 0
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		operations++
		return "success", "unapproved operation", nil
	}, WaitApproval: func(ctx context.Context, _ Step, _ *wfState) (ApprovalWaitResult, error) {
		decisions++
		<-ctx.Done()
		return ApprovalWaitResult{TaskID: "owned-task", RequestID: "owned-request"}, ctx.Err()
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "bounded", Steps: []Step{{ID: "approval", Kind: StepApproval, Timeout: "1ms", OnFailure: "retry"}, {ID: "later", Kind: StepAgent}}}, "fixture")
	if decisions != 1 || operations != 0 || result.Outcome != "timeout" || !errors.Is(err, context.DeadlineExceeded) || !reliability.IsExecutionStoppedError(err) || result.Steps[0].HitlRequestID != "owned-request" {
		t.Fatalf("decisions=%d operations=%d result=%+v err=%v", decisions, operations, result, err)
	}
}

func TestWorkflowNestedContainerBudgetReachesChildren(t *testing.T) {
	for _, kind := range []StepKind{StepSubworkflow, StepLoop, StepParallel} {
		t.Run(string(kind), func(t *testing.T) {
			calls := 0
			runner := &Runner{RunAgent: func(ctx context.Context, _ string, _ string, _ string) (string, string, error) {
				calls++
				<-ctx.Done()
				return "input-required", "wait evidence", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("known input wait")}
			}}
			result, err := runner.Run(t.Context(), Pipeline{Name: "container-budget", Steps: []Step{{ID: "container", Kind: kind, Timeout: "1ms", Steps: []Step{{ID: "owned", Kind: StepAgent}}}, {ID: "later", Kind: StepAgent}}}, "fixture")
			if calls != 1 || result.Outcome != "input-required" || !reliability.IsExecutionPause(result.Outcome, err) || len(result.Steps[0].Steps) != 1 || result.Steps[0].Steps[0].Output != "wait evidence" {
				t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
			}
		})
	}
}

func TestWorkflowCompletedContainerPrefixCannotReplayOrBeSkipped(t *testing.T) {
	for _, kind := range []StepKind{StepSubworkflow, StepLoop, StepParallel} {
		for _, policy := range []string{"retry", "skip"} {
			t.Run(string(kind)+"/"+policy, func(t *testing.T) {
				var completed, failed, later atomic.Int64
				runner := &Runner{RunAgent: func(_ context.Context, name, _, _ string) (string, string, error) {
					if name == "complete" {
						completed.Add(1)
						return "success", "completed evidence", nil
					}
					if name == "fault" {
						failed.Add(1)
						return "failure", "fault evidence", nil
					}
					later.Add(1)
					return "success", "must not run", nil
				}}
				result, err := runner.Run(t.Context(), Pipeline{Name: "partial", Steps: []Step{{ID: "container", Kind: kind, OnFailure: policy, MaxIterations: 3, Steps: []Step{{ID: "complete", Kind: StepAgent, Agent: "complete"}, {ID: "fault", Kind: StepAgent, Agent: "fault"}}}, {ID: "later", Kind: StepAgent}}}, "fixture")
				if completed.Load() != 1 || failed.Load() != 1 || later.Load() != 0 || result.Outcome != "partial" || !reliability.IsExecutionStoppedError(err) || len(result.Steps[0].Steps) != 2 {
					t.Fatalf("completed=%d failed=%d later=%d result=%+v err=%v", completed.Load(), failed.Load(), later.Load(), result, err)
				}
			})
		}
	}
}

func TestWorkflowEligibleRetryUsesOriginalInputAndPriorState(t *testing.T) {
	calls := 0
	var inputs []string
	runner := &Runner{RunAgent: func(_ context.Context, _, _, task string) (string, string, error) {
		calls++
		inputs = append(inputs, task)
		if calls == 1 {
			return "failure", "failed-attempt-output", nil
		}
		return "success", "completed evidence", nil
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "retry-input", Steps: []Step{{ID: "retry", Kind: StepAgent, Input: "{{.input}} {{.prev.retry.output}}", OnFailure: "retry"}}}, "original input")
	if err != nil || result.Outcome != "success" || calls != 2 || inputs[0] != inputs[1] {
		t.Fatalf("inputs=%v result=%+v err=%v", inputs, result, err)
	}
}
