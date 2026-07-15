package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualityGateValidate(t *testing.T) {
	tests := []struct {
		name          string
		preComposite  float64
		postComposite float64
		expected      GateResult
	}{
		{
			name:          "accept improvement",
			preComposite:  50,
			postComposite: 55,
			expected:      GateAccepted,
		},
		{
			name:          "accept small regression within threshold",
			preComposite:  50,
			postComposite: 49,
			expected:      GateAccepted,
		},
		{
			name:          "accept exact threshold boundary",
			preComposite:  50,
			postComposite: 40,
			expected:      GateAccepted, // exactly 20% — not strictly below
		},
		{
			name:          "reject below composite floor",
			preComposite:  40,
			postComposite: 0.2,
			expected:      GateRejected,
		},
		{
			// Above the floor, >20% drop: relative regression wins.
			name:          "rollback large regression",
			preComposite:  90,
			postComposite: 60,
			expected:      GateRollback,
		},
		{
			name:          "accept when pre is zero (new tree)",
			preComposite:  0,
			postComposite: 30,
			expected:      GateAccepted,
		},
		{
			name:          "reject when pre is very low and post is below floor",
			preComposite:  0.1,
			postComposite: 0.09,
			expected:      GateRejected, // declining below MinComposite (30.0)
		},
		{
			name:          "rollback on 25% regression",
			preComposite:  100,
			postComposite: 74,
			expected:      GateRollback, // 26% drop > 20% threshold
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qg := NewQualityGate("/tmp/test_qg")
			result := qg.Validate(tt.preComposite, tt.postComposite)
			if result != tt.expected {
				t.Errorf("Validate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestQualityGateConsecutiveFails(t *testing.T) {
	qg := NewQualityGate("/tmp/test_qg")
	qg.ConsecutiveFails = 3

	// 3 consecutive rejections should trigger disabled
	for i := 0; i < 3; i++ {
		qg.Validate(50, 0.1)
	}

	if !qg.IsDisabled() {
		t.Errorf("expected IsDisabled after %d consecutive rejects", qg.ConsecutiveFails)
	}
	if qg.FailCount() != 3 {
		t.Errorf("expected FailCount=3, got %d", qg.FailCount())
	}
}

func TestQualityGateReset(t *testing.T) {
	qg := NewQualityGate("/tmp/test_qg")
	qg.ConsecutiveFails = 3

	// Two fails
	qg.Validate(50, 0.1)
	qg.Validate(50, 0.1)

	if qg.FailCount() != 2 {
		t.Errorf("expected FailCount=2 after 2 rejects, got %d", qg.FailCount())
	}
	if qg.IsDisabled() {
		t.Error("should not be disabled after only 2 fails with threshold 3")
	}

	// Accept should reset
	qg.Validate(50, 55)

	if qg.FailCount() != 0 {
		t.Errorf("expected FailCount=0 after accept, got %d", qg.FailCount())
	}
	if qg.IsDisabled() {
		t.Error("should not be disabled after reset")
	}
}

func TestQualityGateResetFailCount(t *testing.T) {
	qg := NewQualityGate("/tmp/test_qg")
	qg.ConsecutiveFails = 3

	for i := 0; i < 3; i++ {
		qg.Validate(50, 0.1)
	}

	if !qg.IsDisabled() {
		t.Fatal("expected disabled")
	}

	qg.ResetFailCount()
	if qg.IsDisabled() {
		t.Error("should not be disabled after ResetFailCount")
	}
	if qg.FailCount() != 0 {
		t.Errorf("expected FailCount=0 after ResetFailCount, got %d", qg.FailCount())
	}
}

func TestQualityGateDisabledWithZeroThreshold(t *testing.T) {
	qg := NewQualityGate("/tmp/test_qg")
	qg.ConsecutiveFails = 0 // disabled

	qg.Validate(50, 0.1)

	if qg.IsDisabled() {
		t.Error("should not be disabled when ConsecutiveFails is 0")
	}
}

func TestSnapshotAndRestoreTree(t *testing.T) {
	tmpDir := t.TempDir()
	snapshotDir := filepath.Join(tmpDir, "snapshots")

	original := &SerializableNode{
		Type: "Sequence",
		Name: "TestTree",
		Children: []SerializableNode{
			{Type: "Condition", Name: "IsValid", Metadata: map[string]any{"key": "value"}},
			{Type: "Action", Name: "DoWork"},
		},
		MaxRetries: 3,
		Metadata:   map[string]any{"version": "1.0"},
	}

	// Snapshot
	path, err := SnapshotTree(original, "test_tree", snapshotDir)
	if err != nil {
		t.Fatalf("SnapshotTree() error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("snapshot file not created at %s", path)
	}

	// Restore
	restored, err := RestoreTree("test_tree", snapshotDir)
	if err != nil {
		t.Fatalf("RestoreTree() error: %v", err)
	}

	if restored.Name != "TestTree" {
		t.Errorf("restored.Name = %s, want TestTree", restored.Name)
	}
	if len(restored.Children) != 2 {
		t.Errorf("restored.Children len = %d, want 2", len(restored.Children))
	}
	if restored.MaxRetries != 3 {
		t.Errorf("restored.MaxRetries = %d, want 3", restored.MaxRetries)
	}
	if md, ok := restored.Metadata["version"]; !ok || md.(string) != "1.0" {
		t.Errorf("restored Metadata version = %v, want 1.0", restored.Metadata["version"])
	}
}

func TestSnapshotRestoreNonexistent(t *testing.T) {
	_, err := RestoreTree("nonexistent", "/tmp/does_not_exist")
	if err == nil {
		t.Error("expected error for nonexistent snapshot")
	}
}

// TestSnapshotTreeMultiRevision verifies that repeated SnapshotTree calls for
// the same tree name accumulate revision history instead of overwriting a
// single snapshot_<treeName>.json file — so a regression discovered several
// cycles after it was introduced can still roll back past just the
// immediately-preceding cycle.
func TestSnapshotTreeMultiRevision(t *testing.T) {
	tmpDir := t.TempDir()
	snapshotDir := filepath.Join(tmpDir, "snapshots")

	rev1 := &SerializableNode{Type: "Sequence", Name: "gen1"}
	rev2 := &SerializableNode{Type: "Sequence", Name: "gen2"}
	rev3 := &SerializableNode{Type: "Sequence", Name: "gen3"}

	if _, err := SnapshotTree(rev1, "multi_tree", snapshotDir); err != nil {
		t.Fatalf("SnapshotTree(rev1) error: %v", err)
	}
	if _, err := SnapshotTree(rev2, "multi_tree", snapshotDir); err != nil {
		t.Fatalf("SnapshotTree(rev2) error: %v", err)
	}
	if _, err := SnapshotTree(rev3, "multi_tree", snapshotDir); err != nil {
		t.Fatalf("SnapshotTree(rev3) error: %v", err)
	}

	revisions, err := ListRevisions("multi_tree", snapshotDir)
	if err != nil {
		t.Fatalf("ListRevisions() error: %v", err)
	}
	if len(revisions) != 3 {
		t.Fatalf("ListRevisions() returned %d revisions, want 3 (snapshots must not overwrite each other)", len(revisions))
	}

	// A regression discovered several cycles later must still be able to
	// roll back past just the immediately-preceding cycle — restore rev1,
	// not just rev2.
	oldest := revisions[0]
	restored, err := RestoreTreeRevision("multi_tree", snapshotDir, oldest)
	if err != nil {
		t.Fatalf("RestoreTreeRevision(oldest) error: %v", err)
	}
	if restored.Name != "gen1" {
		t.Errorf("RestoreTreeRevision(oldest).Name = %s, want gen1 (oldest revision)", restored.Name)
	}

	newest := revisions[len(revisions)-1]
	restoredNewest, err := RestoreTreeRevision("multi_tree", snapshotDir, newest)
	if err != nil {
		t.Fatalf("RestoreTreeRevision(newest) error: %v", err)
	}
	if restoredNewest.Name != "gen3" {
		t.Errorf("RestoreTreeRevision(newest).Name = %s, want gen3 (newest revision)", restoredNewest.Name)
	}

	// Plain RestoreTree (no revision arg) must still return the latest
	// revision, preserving backward compatibility for existing callers.
	latest, err := RestoreTree("multi_tree", snapshotDir)
	if err != nil {
		t.Fatalf("RestoreTree() error: %v", err)
	}
	if latest.Name != "gen3" {
		t.Errorf("RestoreTree().Name = %s, want gen3 (latest revision)", latest.Name)
	}
}

// TestListRevisionsEmpty verifies ListRevisions returns no error and no
// revisions for a tree with no snapshots.
func TestListRevisionsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	snapshotDir := filepath.Join(tmpDir, "snapshots")

	revisions, err := ListRevisions("never_snapshotted", snapshotDir)
	if err != nil {
		t.Fatalf("ListRevisions() error on empty history: %v", err)
	}
	if len(revisions) != 0 {
		t.Errorf("ListRevisions() = %d revisions, want 0", len(revisions))
	}
}
