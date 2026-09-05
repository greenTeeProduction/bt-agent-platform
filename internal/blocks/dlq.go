package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// DLQEscalateBlock pushes the failed task to the dead letter queue.
func DLQEscalateBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "DLQEscalate",
		Description: "Escalate exhausted task to dead letter queue",
		Children: []evolution.SerializableNode{
			{Type: "Condition", Name: "PersistentFailures", Description: "Retries exhausted"},
			{Type: "Action", Name: "PushToDLQ", Description: "Enqueue task for operator replay"},
		},
	}
}
