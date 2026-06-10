package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveSLOMetrics_LoadSLOEvidence_Roundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slo", "slo-metrics.json")

	m := GetSLOMetrics("roundtrip-agent", "roundtrip-tree")
	m.RecordSuccess(100 * time.Millisecond)
	m.RecordSuccess(200 * time.Millisecond)
	m.RecordFailure(50 * time.Millisecond)
	m.RecordRecovery(150 * time.Millisecond)

	if err := SaveSLOMetrics(path); err != nil {
		t.Fatalf("SaveSLOMetrics: %v", err)
	}

	snapshots, err := LoadSLOEvidence(path)
	if err != nil {
		t.Fatalf("LoadSLOEvidence: %v", err)
	}

	var found *SLOSnapshot
	for i := range snapshots {
		if snapshots[i].AgentName == "roundtrip-agent" && snapshots[i].TreeName == "roundtrip-tree" {
			found = &snapshots[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected snapshot for roundtrip-agent/roundtrip-tree, got %d snapshots", len(snapshots))
	}
	if found.TotalCalls != 3 {
		t.Errorf("TotalCalls = %d, want 3", found.TotalCalls)
	}
	if found.SuccessfulCalls != 2 {
		t.Errorf("SuccessfulCalls = %d, want 2", found.SuccessfulCalls)
	}
	if found.FailedCalls != 1 {
		t.Errorf("FailedCalls = %d, want 1", found.FailedCalls)
	}
	if found.RecoveredCalls != 1 {
		t.Errorf("RecoveredCalls = %d, want 1", found.RecoveredCalls)
	}
}

func TestSaveSLOMetrics_AtomicNoTmpLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slo-metrics.json")

	GetSLOMetrics("atomic-agent", "atomic-tree").RecordSuccess(10 * time.Millisecond)

	if err := SaveSLOMetrics(path); err != nil {
		t.Fatalf("SaveSLOMetrics: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected metrics file at %s: %v", path, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file left behind after save")
	}
}

func TestLoadSLOEvidence_MissingFile(t *testing.T) {
	snapshots, err := LoadSLOEvidence(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err == nil {
		t.Error("expected error for missing evidence file")
	}
	if len(snapshots) != 0 {
		t.Errorf("expected no snapshots from missing file, got %d", len(snapshots))
	}
}

func TestSLOSnapshot_Rates(t *testing.T) {
	s := SLOSnapshot{TotalCalls: 10, SuccessfulCalls: 9, FailedCalls: 1, RecoveredCalls: 1}
	if got := s.SuccessRate(); got != 0.9 {
		t.Errorf("SuccessRate = %v, want 0.9", got)
	}
	if got := s.RecoveryRate(); got != 1.0 {
		t.Errorf("RecoveryRate = %v, want 1.0", got)
	}

	empty := SLOSnapshot{}
	if got := empty.SuccessRate(); got != 1.0 {
		t.Errorf("empty SuccessRate = %v, want 1.0 (matches SLOMetrics semantics)", got)
	}
	if got := empty.RecoveryRate(); got != 0 {
		t.Errorf("empty RecoveryRate = %v, want 0", got)
	}
}
