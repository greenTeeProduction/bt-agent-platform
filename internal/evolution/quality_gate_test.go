package evolution

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQualityGateValidate(t *testing.T) {
	tests := []struct {
		name         string
		preComposite  float64
		postComposite float64
		expected     GateResult
	}{
		{
			name:         "accept improvement",
			preComposite:  50,
			postComposite: 55,
			expected:     GateAccepted,
		},
		{
			name:         "accept small regression within threshold",
			preComposite:  50,
			postComposite: 49,
			expected:     GateAccepted,
		},
		{
			name:         "accept exact threshold boundary",
			preComposite:  50,
			postComposite: 40,
			expected:     GateAccepted, // exactly 20% — not strictly below
		},
		{
			name:         "reject below composite floor",
			preComposite:  40,
			postComposite: 0.2,
			expected:     GateRejected,
		},
		{
			name:         "rollback large regression",
			preComposite:  50,
			postComposite: 20,
			expected:     GateRollback,
		},
		{
			name:         "accept when pre is zero (new tree)",
			preComposite:  0,
			postComposite: 30,
			expected:     GateAccepted,
		},
		{
			name:         "reject when pre is very low and post is below floor",
			preComposite:  0.1,
			postComposite: 0.09,
			expected:     GateRejected, // postComposite < MinComposite (0.3)
		},
		{
			name:         "rollback on 25% regression",
			preComposite:  100,
			postComposite: 74,
			expected:     GateRollback, // 26% drop > 20% threshold
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

func TestCloneTree(t *testing.T) {
	original := &SerializableNode{
		Type: "Selector",
		Name: "Original",
		Children: []SerializableNode{
			{Type: "Action", Name: "ChildA"},
		},
		Metadata: map[string]any{"max_tokens": float64(2048)},
	}

	clone := CloneTree(original)
	if clone == nil {
		t.Fatal("CloneTree returned nil")
	}

	// Verify deep copy (modifying clone doesn't affect original)
	clone.Name = "Modified"
	if original.Name != "Original" {
		t.Errorf("original.Name = %s, want Original (deep copy failed)", original.Name)
	}

	clone.Children[0].Name = "ModifiedChild"
	if original.Children[0].Name != "ChildA" {
		t.Errorf("original.Children[0].Name = %s, want ChildA (deep copy failed)", original.Children[0].Name)
	}
}

func TestCloneTreeNil(t *testing.T) {
	clone := CloneTree(nil)
	if clone != nil {
		t.Error("CloneTree(nil) should return nil")
	}
}

func TestSnapshotRestoreNonexistent(t *testing.T) {
	_, err := RestoreTree("nonexistent", "/tmp/does_not_exist")
	if err == nil {
		t.Error("expected error for nonexistent snapshot")
	}
}
