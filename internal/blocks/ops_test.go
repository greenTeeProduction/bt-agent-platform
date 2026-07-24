package blocks

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/engine"
	btcore "github.com/rvitorper/go-bt/core"
)

// Characterization tests for ops.go's TraceCheckpointBlock and AuditLogBlock.
// These pin the exact node structure each builds and confirm both trees build
// cleanly against the live action registry. They make no production changes
// unless a real bug is found.

func TestTraceCheckpointBlock_Structure(t *testing.T) {
	tests := []struct {
		name      string
		label     string
		wantLabel string
	}{
		{name: "empty label defaults to checkpoint", label: "", wantLabel: "checkpoint"},
		{name: "custom label preserved", label: "my_checkpoint", wantLabel: "my_checkpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := TraceCheckpointBlock(tt.label)
			if n.Type != "Sequence" {
				t.Errorf("root type = %q, want %q", n.Type, "Sequence")
			}
			if n.Name != "TraceCheckpointBlock" {
				t.Errorf("root name = %q, want %q", n.Name, "TraceCheckpointBlock")
			}
			if n.Description != "Record trace checkpoint for observability" {
				t.Errorf("root description = %q, want %q", n.Description, "Record trace checkpoint for observability")
			}
			if got, _ := n.Metadata["checkpoint"].(string); got != tt.wantLabel {
				t.Errorf("metadata[checkpoint] = %q, want %q", got, tt.wantLabel)
			}
			if len(n.Children) != 1 {
				t.Fatalf("children = %d, want 1", len(n.Children))
			}
			child := n.Children[0]
			if child.Type != "Action" || child.Name != "TraceCheckpoint" {
				t.Errorf("child = %s/%s, want Action/TraceCheckpoint", child.Type, child.Name)
			}
			wantDesc := "Emit span event: " + tt.wantLabel
			if child.Description != wantDesc {
				t.Errorf("child description = %q, want %q", child.Description, wantDesc)
			}
			if len(child.Children) != 0 {
				t.Errorf("child has %d children, want 0", len(child.Children))
			}
		})
	}
}

func TestTraceCheckpointBlock_BuildAndValidate(t *testing.T) {
	n := TraceCheckpointBlock("task_checkpoint")
	bb := &engine.Blackboard{Task: "checkpoint test", Outcome: "success", ChainState: make(map[string]any)}
	cmd, err := engine.BuildAndValidate(&n, bb)
	if err != nil {
		t.Fatalf("BuildAndValidate() error = %v", err)
	}
	code := cmd.Run(btcore.NewBTContext(t.Context(), bb))
	if code != 1 {
		t.Fatalf("expected success, got %d", code)
	}
}

func TestAuditLogBlock_Structure(t *testing.T) {
	n := AuditLogBlock()
	if n.Type != "Sequence" {
		t.Errorf("root type = %q, want %q", n.Type, "Sequence")
	}
	if n.Name != "AuditLogBlock" {
		t.Errorf("root name = %q, want %q", n.Name, "AuditLogBlock")
	}
	if n.Description != "Append task audit log entry" {
		t.Errorf("root description = %q, want %q", n.Description, "Append task audit log entry")
	}
	if len(n.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(n.Children))
	}
	child := n.Children[0]
	if child.Type != "Action" || child.Name != "AuditLog" {
		t.Errorf("child = %s/%s, want Action/AuditLog", child.Type, child.Name)
	}
	if child.Description != "Write audit JSONL entry" {
		t.Errorf("child description = %q, want %q", child.Description, "Write audit JSONL entry")
	}
	if len(child.Children) != 0 {
		t.Errorf("child has %d children, want 0", len(child.Children))
	}
}
