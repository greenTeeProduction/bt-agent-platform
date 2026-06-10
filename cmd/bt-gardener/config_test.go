package main

import (
	"os"
	"testing"
)

// TestBuildGardenerConfig_SafetyComponentsWired proves that the production config
// constructor wires all three safety components that the audit found missing.
func TestBuildGardenerConfig_SafetyComponentsWired(t *testing.T) {
	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	if cfg.Gate == nil {
		t.Error("Gate is nil — quality gate not wired into production config")
	}
	if cfg.SnapshotDir == "" {
		t.Error("SnapshotDir is empty — snapshot directory not wired into production config")
	}
	if cfg.CrisisDetector == nil {
		t.Error("CrisisDetector is nil — crisis detector not wired into production config")
	}
	if cfg.ValidationGate.EvidencePath != "/tmp/slo-evidence.json" {
		t.Errorf("ValidationGate.EvidencePath = %q — SLO evidence file not wired into production config (B1)",
			cfg.ValidationGate.EvidencePath)
	}
}

// TestBuildGardenerConfig_SnapshotDirCreated proves that buildGardenerConfig
// creates the snapshot directory on disk (MkdirAll with 0700).
func TestBuildGardenerConfig_SnapshotDirCreated(t *testing.T) {
	baseDir := t.TempDir()
	snapDir := baseDir + "/snapshots"
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	_, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	info, statErr := os.Stat(snapDir)
	if statErr != nil {
		t.Fatalf("snapshot dir was not created: %v", statErr)
	}
	if !info.IsDir() {
		t.Errorf("snapshot path exists but is not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("snapshot dir permissions = %04o, want 0700", perm)
	}
}
