package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// 2026-08-01, measured on the live fleet: ~/.go-bt-evolve/slo/slo-metrics.json
// held exactly four agents — bob-approved, bob-rejected, bob-pending, godev —
// and NONE of the eleven agents in ~/.go-bt-evolve/agents/. This is the file the
// gardener's validation gate and the dashboard read as fleet-health evidence,
// which is why health showed green while the fleet was failing every cycle.
//
// Mechanism: sloRegistry is a package-level, PER-PROCESS global, and
// SaveSLOMetrics serialized the whole registry over the shared path. The write
// is not daemon-gated — internal/agent/runner.go's RunOnce defers one on EVERY
// run in EVERY process — so any short-lived sibling (the MCP bt-agent each goap
// cycle's Claude session boots, bt-agent-cli, a dashboard worker) that runs one
// agent replaced the daemon's entire evidence file with its own partial view.
//
// The path is documented as "the cross-process SLO evidence file", so the write
// must MERGE: entries this process does not track are other processes' evidence
// and must survive.

func writeSLOFile(t *testing.T, path string, snaps []SLOSnapshot) {
	t.Helper()
	data, err := json.MarshalIndent(snaps, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSLOFile(t *testing.T, path string) map[string]SLOSnapshot {
	t.Helper()
	snaps, err := LoadSLOEvidence(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]SLOSnapshot{}
	for _, s := range snaps {
		out[s.AgentName+":"+s.TreeName] = s
	}
	return out
}

// resetSLORegistry empties the process-global registry so a test starts from a
// known state, and restores nothing afterwards — every test that touches it
// must seed what it needs.
func resetSLORegistry(t *testing.T) {
	t.Helper()
	for k := range AllSLOMetrics() {
		sloRegistry.Delete(k)
	}
	t.Cleanup(func() {
		for k := range AllSLOMetrics() {
			sloRegistry.Delete(k)
		}
	})
}

// A process that tracks only its own agent must not erase evidence written by
// another process. This is the exact live failure.
func TestSaveSLOMetrics_PreservesOtherProcessesEvidence(t *testing.T) {
	resetSLORegistry(t)
	path := filepath.Join(t.TempDir(), "slo-metrics.json")
	writeSLOFile(t, path, []SLOSnapshot{
		{AgentName: "goap-fusion-loop-runner", TreeName: "domain:goap_fusion_loop", TotalCalls: 226, SuccessfulCalls: 29},
		{AgentName: "bt-fusion", TreeName: "domain:bt_fusion", TotalCalls: 64, SuccessfulCalls: 64},
	})

	// This process has run exactly one agent — the sibling case.
	GetSLOMetrics("bob-approved", "goal:automate_approved").RecordSuccess(5)

	if err := SaveSLOMetrics(path); err != nil {
		t.Fatal(err)
	}

	got := readSLOFile(t, path)
	if _, ok := got["bob-approved:goal:automate_approved"]; !ok {
		t.Fatal("this process's own metrics are missing from the file")
	}
	for _, key := range []string{"goap-fusion-loop-runner:domain:goap_fusion_loop", "bt-fusion:domain:bt_fusion"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("evidence for %q was erased by a process that does not track it — "+
				"a sibling running one agent must not replace the fleet's whole SLO file", key)
		}
	}
	if n := len(got); n != 3 {
		t.Fatalf("file holds %d entries, want 3 (2 preserved + 1 new): %v", n, got)
	}
}

// A key this process DOES track is authoritative for that key: its live counters
// replace the on-disk ones rather than being merged arithmetically (the registry
// counters are already cumulative for this process, so adding them would
// double-count on every save).
func TestSaveSLOMetrics_OwnKeysWinOverStaleDiskEntries(t *testing.T) {
	resetSLORegistry(t)
	path := filepath.Join(t.TempDir(), "slo-metrics.json")
	writeSLOFile(t, path, []SLOSnapshot{
		{AgentName: "self-review", TreeName: "domain:self_review", TotalCalls: 1, SuccessfulCalls: 1},
	})

	m := GetSLOMetrics("self-review", "domain:self_review")
	for i := 0; i < 7; i++ {
		m.RecordSuccess(3)
	}

	if err := SaveSLOMetrics(path); err != nil {
		t.Fatal(err)
	}

	got := readSLOFile(t, path)
	entry, ok := got["self-review:domain:self_review"]
	if !ok {
		t.Fatal("own key vanished")
	}
	if entry.TotalCalls != 7 {
		t.Fatalf("TotalCalls = %d, want 7: the writing process is authoritative for keys it "+
			"tracks; summing with the on-disk value would double-count on every save", entry.TotalCalls)
	}
	if n := len(got); n != 1 {
		t.Fatalf("file holds %d entries, want 1 — the same key must not be duplicated", n)
	}
}

// An unreadable or corrupt existing file must not block the write: the process's
// own evidence is still worth persisting, and refusing would lose it entirely.
func TestSaveSLOMetrics_CorruptExistingFileDoesNotBlockTheWrite(t *testing.T) {
	resetSLORegistry(t)
	path := filepath.Join(t.TempDir(), "slo-metrics.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	GetSLOMetrics("bt-fusion", "domain:bt_fusion").RecordSuccess(1)

	if err := SaveSLOMetrics(path); err != nil {
		t.Fatalf("SaveSLOMetrics must still persist this process's metrics over a corrupt file: %v", err)
	}
	if got := readSLOFile(t, path); len(got) != 1 {
		t.Fatalf("file holds %d entries, want 1", len(got))
	}
}
