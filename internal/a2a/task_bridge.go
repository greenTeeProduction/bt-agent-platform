package a2a

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// TaskStateBridge maps A2A task states to BT outcomes and vice versa.
type TaskStateBridge struct{}

// BTToA2A maps a BT outcome string to an A2A task state.
func (b *TaskStateBridge) BTToA2A(outcome string) a2a.TaskState {
	switch outcome {
	case "success":
		return a2a.TaskStateCompleted
	case "failure":
		return a2a.TaskStateFailed
	case "running":
		return a2a.TaskStateWorking
	case "input-required":
		return a2a.TaskStateInputRequired
	default:
		return a2a.TaskStateFailed
	}
}

// A2AToBT maps an A2A task state to a BT outcome string.
func (b *TaskStateBridge) A2AToBT(state a2a.TaskState) string {
	switch state {
	case a2a.TaskStateCompleted:
		return "success"
	case a2a.TaskStateFailed:
		return "failure"
	case a2a.TaskStateCanceled:
		return "cancelled"
	case a2a.TaskStateWorking:
		return "running"
	default:
		return "unknown"
	}
}

// IsTerminal returns true if the state is a terminal state.
func (b *TaskStateBridge) IsTerminal(state a2a.TaskState) bool {
	return state == a2a.TaskStateCompleted ||
		state == a2a.TaskStateFailed ||
		state == a2a.TaskStateCanceled
}

// InitEngineDelegate wires the A2A client into the engine's DelegateToA2A action node.
// Must be called before any BT tree executes a DelegateToA2A node.
func InitEngineDelegate() {
	client := NewBTAgentClient()
	engine.DelegateToA2AFn = func(targetURL, task string) (string, error) {
		return client.SendTask(context.Background(), targetURL, task)
	}
}
