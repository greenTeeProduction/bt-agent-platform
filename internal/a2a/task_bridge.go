package a2a

import (
	"context"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// TaskStateBridge maps A2A task states to BT outcomes and vice versa.
type TaskStateBridge struct{}

// BTToA2A maps a BT outcome string to an A2A task state.
func (b *TaskStateBridge) BTToA2A(outcome string) a2a.TaskState {
	if agent.IsHealthyOutcome(outcome) {
		return a2a.TaskStateCompleted
	}
	switch outcome {
	case "failure":
		return a2a.TaskStateFailed
	case "running":
		return a2a.TaskStateWorking
	case "input-required":
		return a2a.TaskStateInputRequired
	case "pending_approval":
		return a2a.TaskStateInputRequired
	case "goap_fusion_rate_limited":
		return a2a.TaskStateInputRequired
	case "auth-required":
		return a2a.TaskStateAuthRequired
	case "cancelled":
		return a2a.TaskStateCanceled
	case "rejected":
		return a2a.TaskStateRejected
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
	case a2a.TaskStateInputRequired:
		return "input-required"
	case a2a.TaskStateAuthRequired:
		return "auth-required"
	case a2a.TaskStateRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

// IsTerminal returns true if the state is a terminal state.
func (b *TaskStateBridge) IsTerminal(state a2a.TaskState) bool {
	return state.Terminal()
}

// InitEngineDelegate wires the A2A client into the engine's DelegateToA2A action node.
// Must be called before any BT tree executes a DelegateToA2A node.
func InitEngineDelegate() {
	client := NewBTAgentClient()
	engine.DelegateToA2AFn = func(ctx context.Context, targetURL, task string) (string, error) {
		return client.SendTask(ctx, targetURL, task)
	}
}
