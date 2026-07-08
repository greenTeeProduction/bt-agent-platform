package main

import (
	"os"
	"path/filepath"
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
	// Production must allow trees without SLO evidence to persist: only 4 of
	// ~50 registry trees are executed by live agents, and strict fail-closed
	// froze evolution at 0 applied mutations for a month. Evidenced trees keep
	// full threshold enforcement.
	if !cfg.ValidationGate.AllowUnverified {
		t.Error("ValidationGate.AllowUnverified = false — unevidenced trees would fail closed forever")
	}
}

// TestBuildGardenerConfig_ExperienceBankSharedWithDaemon pins milestone 3/5 of
// the experience-grounded evolution program (Q2 Evolvability, Q3 Reliability):
// the production gardener config must carry a persistent ExperienceBank so
// RunCycleV2 records accepted mutations instead of discarding them, and that
// bank must default to the DAEMON'S experience directory —
// agent.HomeDir()/"experience", the exact dir bt-agent's experienceBankDir()
// resolves (see cmd/bt-agent/main.go) — so gardener and bt-agent accumulate
// into one shared on-disk bank. agent.HomeDir() honors BT_AGENT_HOME, which is
// both the configurability seam and what lets this test run against a temp
// dir instead of the real platform home.
func TestBuildGardenerConfig_ExperienceBankSharedWithDaemon(t *testing.T) {
	agentHome := t.TempDir()
	t.Setenv("BT_AGENT_HOME", agentHome)

	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	if cfg.ExperienceBank == nil {
		t.Fatal("ExperienceBank is nil — gardener runs experience-blind: RunCycleV2 discards accepted-mutation experience instead of recording it to the shared bank")
	}

	// NewExperienceBank MkdirAlls its directory, so the shared dir existing
	// under BT_AGENT_HOME proves the bank is rooted at the daemon's location
	// (agent.HomeDir()/experience) rather than some gardener-private path.
	sharedDir := filepath.Join(agentHome, "experience")
	info, statErr := os.Stat(sharedDir)
	if statErr != nil {
		t.Fatalf("shared experience dir %q was not created: %v — the gardener's bank must default to the daemon's experience dir (agent.HomeDir()/experience) so both binaries accumulate into one bank", sharedDir, statErr)
	}
	if !info.IsDir() {
		t.Errorf("shared experience path %q exists but is not a directory", sharedDir)
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
