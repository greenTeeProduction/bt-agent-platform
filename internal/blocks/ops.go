package blocks

import "github.com/nico/go-bt-evolve/internal/evolution"

// TraceCheckpointBlock emits a trace event and optional blackboard snapshot.
func TraceCheckpointBlock(label string) evolution.SerializableNode {
	if label == "" {
		label = "checkpoint"
	}
	return evolution.SerializableNode{
		Type: "Sequence",
		// Must differ from the child Action's name below, or ValidateTreeFull's
		// cycle detector (name reused in ancestry path) rejects the tree.
		Name:        "TraceCheckpointBlock",
		Description: "Record trace checkpoint for observability",
		Metadata: map[string]any{
			"checkpoint": label,
		},
		Children: []evolution.SerializableNode{
			// Name must stay "TraceCheckpoint" — it's the key engine.RegisterAction uses.
			{Type: "Action", Name: "TraceCheckpoint", Description: "Emit span event: " + label},
		},
	}
}

// AuditLogBlock appends a structured audit entry for the current task.
func AuditLogBlock() evolution.SerializableNode {
	return evolution.SerializableNode{
		Type:        "Sequence",
		Name:        "AuditLogBlock",
		Description: "Append task audit log entry",
		Children: []evolution.SerializableNode{
			{Type: "Action", Name: "AuditLog", Description: "Write audit JSONL entry"},
		},
	}
}
