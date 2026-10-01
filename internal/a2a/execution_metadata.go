package a2a

import (
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// executionMetadataKey is a BT extension, merged into the SDK task store by
// status events. It contains JSON values only and never changes A2A task state.
const executionMetadataKey = "bt_execution"

func executionStatus(info a2a.TaskInfoProvider, state a2a.TaskState, message *a2a.Message, outcome string, diagnostic error) *a2a.TaskStatusUpdateEvent {
	event := a2a.NewStatusUpdateEvent(info, state, message)
	kind := reliability.ExecutionErrorKind(diagnostic)
	if diagnostic != nil && kind == "" {
		kind = reliability.ExecutionStoppedKind
	}
	detail := ""
	if diagnostic != nil {
		detail = diagnostic.Error()
	}
	event.SetMeta(executionMetadataKey, map[string]any{"outcome": outcome, "error_kind": kind, "error": detail})
	return event
}

func taskExecutionDiagnostic(task *a2a.Task) error {
	raw, present := task.Metadata[executionMetadataKey]
	if !present {
		return nil // older/external agents need not implement the BT extension
	}
	metadata, ok := raw.(map[string]any)
	if !ok {
		return uncertainResponse("invalid BT execution metadata")
	}
	kind, kindOK := metadata["error_kind"].(string)
	detail, detailOK := metadata["error"].(string)
	outcome, outcomeOK := metadata["outcome"].(string)
	if !kindOK || !detailOK || !outcomeOK || (kind == "" && detail != "") || (kind != "" && detail == "") {
		return uncertainResponse("incomplete BT execution metadata")
	}
	switch kind {
	case "":
		if outcome != string(task.Status.State) &&
			(task.Status.State != a2a.TaskStateCompleted || !agent.IsHealthyOutcome(outcome)) &&
			!validTaskStoppedOutcome(task.Status.State, outcome) {
			return uncertainResponse("execution metadata contradicts task state")
		}
		return nil
	case reliability.ExecutionPersistenceKind:
		if task.Status.State == a2a.TaskStateCompleted && outcome != string(task.Status.State) && !agent.IsHealthyOutcome(outcome) {
			return uncertainResponse("completed persistence metadata contradicts task outcome")
		}
		if task.Status.State != a2a.TaskStateCompleted && outcome != "aborted" {
			return fmt.Errorf("task %s: persistence failed: %s", task.ID, detail)
		}
		return &reliability.ExecutionPersistenceError{Err: fmt.Errorf("task %s: %s", task.ID, detail)}
	case reliability.ExecutionStoppedKind:
		if !validTaskStoppedOutcome(task.Status.State, outcome) {
			return uncertainResponse("stopped metadata contradicts task state")
		}
		return &reliability.ExecutionStoppedError{Outcome: outcome, Err: fmt.Errorf("task %s: %s", task.ID, detail)}
	case reliability.ExecutionUncertainKind:
		return uncertainResponse(fmt.Sprintf("task %s: %s", task.ID, detail))
	default:
		return uncertainResponse("unknown BT execution error kind " + kind)
	}
}

func validTaskStoppedOutcome(state a2a.TaskState, outcome string) bool {
	return reliability.IsStoppedOutcome(outcome) &&
		((&TaskStateBridge{}).BTToA2A(outcome) == state || (state == a2a.TaskStateFailed && agent.IsRateLimitCarryover(outcome)))
}

func stoppedTaskDiagnostic(task *a2a.Task, diagnostic error) error {
	outcome := (&TaskStateBridge{}).A2AToBT(task.Status.State)
	if raw, present := task.Metadata[executionMetadataKey]; present {
		metadata := raw.(map[string]any) // validated by taskExecutionDiagnostic
		reported := metadata["outcome"].(string)
		if reported != string(task.Status.State) {
			outcome = reported
		}
	}
	if !validTaskStoppedOutcome(task.Status.State, outcome) {
		return uncertainResponse("task outcome metadata contradicts its state")
	}
	// For an old peer's failed history write, do not assert that failed work
	// completed. An aborted surrounding task can still retain its child's typed
	// completed diagnostic, with stopped taking precedence for overall health.
	detail := fmt.Errorf("a2a: task %s did not complete: state=%s status=%s", task.ID, task.Status.State, safetyGetMessageText(task.Status.Message))
	return &reliability.ExecutionStoppedError{Outcome: outcome, Err: errors.Join(detail, diagnostic)}
}

func diagnosticSuffix(err error) string {
	if err == nil {
		return ""
	}
	return "; " + err.Error()
}
