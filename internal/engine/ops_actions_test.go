package engine

import (
	"testing"

	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
)

// pushToDLQAction is the second DeadLetterEntry push site (the first is
// cmd/bt-agent/main.go's scheduler failure handler, which already stamps
// BuildRevision from dashboard.InstallBuildIdentity()). Deploy-drift
// diagnosis (program 94b0b31) can only tell "failed on a stale binary" from
// "failed on current code" if every push site stamps the revision — a DLQ
// entry from this action must carry it too.
func TestPushToDLQAction_StampsBuildRevision(t *testing.T) {
	origDLQ := TaskDLQ
	origRevision := BuildRevision
	t.Cleanup(func() {
		TaskDLQ = origDLQ
		BuildRevision = origRevision
	})

	dlq := reliability.NewDeadLetterQueue("")
	TaskDLQ = dlq
	BuildRevision = "test-revision-abc123"

	bb := &Blackboard{Task: "do the thing", Result: "boom", FailureCount: 3}
	ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

	pushToDLQAction(ctx)

	entries := dlq.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 DLQ entry, got %d", len(entries))
	}
	if entries[0].BuildRevision != "test-revision-abc123" {
		t.Errorf("BuildRevision = %q, want %q", entries[0].BuildRevision, "test-revision-abc123")
	}
}
