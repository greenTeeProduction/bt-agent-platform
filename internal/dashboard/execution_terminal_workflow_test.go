package dashboard

import (
	"context"
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestWorkflowUnknownExecutionCannotRetryOrContinue(t *testing.T) {
	for _, policy := range []string{"retry", "skip"} {
		t.Run(policy, func(t *testing.T) {
			calls := 0
			diagnostic := &reliability.ExecutionUncertainError{Err: context.DeadlineExceeded}
			runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
				calls++
				return "success", "partial evidence", diagnostic
			}}
			result, err := runner.Run(context.Background(), Pipeline{Name: "uncertain", Steps: []Step{{ID: "one", Kind: StepAgent, OnFailure: policy}, {ID: "two", Kind: StepAgent}}}, "task")
			if !reliability.IsExecutionUncertainError(err) || calls != 1 || result.Outcome != "aborted" || result.Steps[0].Outcome != "uncertain" || result.Steps[0].Output != "partial evidence" {
				t.Fatalf("uncertain work repeated/lost: result=%+v err=%v calls=%d", result, err, calls)
			}
		})
	}
}

func TestWorkflowParallelUnknownExecutionCannotReplayGroup(t *testing.T) {
	calls := 0
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		calls++
		return "failure", "partial evidence", &reliability.ExecutionUncertainError{Err: context.DeadlineExceeded}
	}}
	result, err := runner.Run(context.Background(), Pipeline{Name: "parallel-uncertain", Steps: []Step{{ID: "parallel", Kind: StepParallel, OnFailure: "retry", Steps: []Step{{ID: "one", Kind: StepAgent}}}, {ID: "two", Kind: StepAgent}}}, "task")
	if !reliability.IsExecutionUncertainError(err) || calls != 1 || result.Outcome != "aborted" {
		t.Fatalf("uncertain group repeated: result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestWorkflowTerminalResultWinsRacingStepDeadline(t *testing.T) {
	for _, probe := range []string{"success", "completed", "no_change", "degraded", "persistence", "uncertain"} {
		t.Run(probe, func(t *testing.T) {
			calls := 0
			runner := &Runner{RunAgent: func(ctx context.Context, _, _, _ string) (string, string, error) {
				calls++
				<-ctx.Done()
				switch probe {
				case "persistence":
					return "success", "completed evidence", &reliability.ExecutionPersistenceError{Err: context.DeadlineExceeded}
				case "uncertain":
					return "failure", "partial evidence", &reliability.ExecutionUncertainError{Err: context.DeadlineExceeded}
				default:
					return probe, "completed evidence", nil
				}
			}}
			result, err := runner.Run(context.Background(), Pipeline{Name: "deadline-result", Steps: []Step{{ID: "one", Kind: StepAgent, OnFailure: "retry", Timeout: "1ms"}}}, "task")
			if calls != 1 {
				t.Fatalf("terminal work repeated %d times", calls)
			}
			switch probe {
			case "success", "completed", "no_change", "degraded":
				if err != nil || result.Outcome != "success" || result.Steps[0].Outcome != probe {
					t.Fatalf("completed work overwritten: result=%+v err=%v", result, err)
				}
			case "persistence":
				if !reliability.IsExecutionPersistenceError(err) || result.Steps[0].Outcome != "success" {
					t.Fatalf("completed diagnostic overwritten: result=%+v err=%v", result, err)
				}
			case "uncertain":
				if !reliability.IsExecutionUncertainError(err) || result.Steps[0].Outcome != "uncertain" {
					t.Fatalf("uncertainty overwritten: result=%+v err=%v", result, err)
				}
			}
		})
	}
}

func TestWorkflowUncertainRetryRetainsUnknownOutcome(t *testing.T) {
	calls := 0
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		calls++
		if calls == 1 {
			return "failure", "first failure", nil
		}
		return "failure", "uncertain retry evidence", &reliability.ExecutionUncertainError{Err: context.DeadlineExceeded}
	}}
	result, err := runner.Run(context.Background(), Pipeline{Name: "retry-uncertain", Steps: []Step{{ID: "one", Kind: StepAgent, OnFailure: "retry"}, {ID: "two", Kind: StepAgent}}}, "task")
	if !reliability.IsExecutionUncertainError(err) || calls != 2 || result.Outcome != "aborted" || result.Steps[0].Outcome != "uncertain" || result.Steps[0].Output != "uncertain retry evidence" {
		t.Fatalf("uncertain retry overwritten/continued: result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestWorkflowMixedParallelDiagnosticsRetainUnknownBranch(t *testing.T) {
	runner := &Runner{RunAgent: func(_ context.Context, name, _, _ string) (string, string, error) {
		if name == "complete" {
			return "success", "completed evidence", &reliability.ExecutionPersistenceError{Err: context.DeadlineExceeded}
		}
		return "failure", "partial evidence", &reliability.ExecutionUncertainError{Err: context.DeadlineExceeded}
	}}
	result, err := runner.Run(context.Background(), Pipeline{Name: "mixed", Steps: []Step{{ID: "parallel", Kind: StepParallel, Steps: []Step{{ID: "complete", Agent: "complete", Kind: StepAgent}, {ID: "unknown", Agent: "unknown", Kind: StepAgent}}}}}, "task")
	if !reliability.IsExecutionUncertainError(err) || reliability.IsExecutionPersistenceError(err) || reliability.ExecutionErrorKind(err) != reliability.ExecutionUncertainKind || result.Outcome != "aborted" {
		t.Fatalf("mixed parallel diagnostics collapsed: result=%+v err=%v", result, err)
	}
}
