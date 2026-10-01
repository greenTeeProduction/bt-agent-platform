package dashboard

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestWorkflowPauseRetainsAdmittedSiblingFailure(t *testing.T) {
	for _, sibling := range []string{"failure", "error", "panic", "success"} {
		t.Run(sibling, func(t *testing.T) {
			var calls atomic.Int64
			runner := &Runner{RunAgent: func(_ context.Context, name, _, _ string) (string, string, error) {
				calls.Add(1)
				if name == "pause" {
					return "input-required", "awaits human input", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("network timeout awaits human input")}
				}
				switch sibling {
				case "panic":
					panic("sibling fault")
				case "error":
					return "failure", "fault evidence", errors.New("ordinary error")
				case "failure":
					return "failure", "fault evidence", nil
				default:
					return "success", "completed evidence", nil
				}
			}}
			result, err := runner.Run(t.Context(), Pipeline{Name: "mixed-pause", Steps: []Step{{ID: "parallel", Kind: StepParallel, OnFailure: "retry", Steps: []Step{{ID: "pause", Agent: "pause", Kind: StepAgent}, {ID: "sibling", Agent: "sibling", Kind: StepAgent}}}, {ID: "later", Kind: StepAgent}}}, "fixture")
			want := "failure"
			if sibling == "success" {
				want = "input-required"
			}
			if calls.Load() != 2 || !reliability.IsExecutionStoppedError(err) || result.Outcome != want || reliability.ExecutionStopOutcome(err) != want {
				t.Fatalf("calls=%d result=%+v err=%v", calls.Load(), result, err)
			}
		})
	}
}

func TestWorkflowPauseDoesNotTurnConditionalSkipIntoFault(t *testing.T) {
	runner := &Runner{RunAgent: func(context.Context, string, string, string) (string, string, error) {
		return "input-required", "pause evidence", &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("needs input")}
	}}
	result, err := runner.Run(t.Context(), Pipeline{Name: "pause-and-skip", Steps: []Step{{ID: "parallel", Kind: StepParallel, Steps: []Step{{ID: "pause", Kind: StepAgent}, {ID: "skip", Kind: StepCondition, Condition: "false"}}}}}, "fixture")
	if !reliability.IsExecutionPause(result.Outcome, err) || result.Outcome != "input-required" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
