package gardener

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestOrdinaryAdoptionFailurePreservesCommittedEvidence(t *testing.T) {
	for _, failure := range []string{"snapshot", "tree-write", "validation"} {
		t.Run(failure, func(t *testing.T) {
			bank, err := evolution.NewExperienceBank(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			g, entry := experienceRecordingGardener(t, bank)
			if err := g.cfg.Registry.SaveTree(entry); err != nil {
				t.Fatal(err)
			}
			before := marshalTree(t, entry.Tree)
			diskBefore, err := os.ReadFile(entry.FilePath)
			if err != nil {
				t.Fatal(err)
			}
			originalPath := entry.FilePath
			blocker := filepath.Join(t.TempDir(), "blocked")
			if err := os.WriteFile(blocker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "snapshot":
				g.cfg.SnapshotDir = filepath.Join(blocker, "snapshots")
			case "tree-write":
				entry.FilePath = filepath.Join(blocker, "tree.json")
			case "validation":
				g.cfg.ValidationGate = DefaultValidationGateConfig()
			}
			m := g.evolveTreeV2(entry, EvolveV2Config{DisableLocalSearch: true})
			if m.Mutations != 0 || m.Improved || m.Delta != 0 {
				t.Fatalf("uncommitted adoption acknowledged: %+v", m)
			}
			if failure != "validation" && !m.SaveFailed {
				t.Fatalf("persistence failure not reported: %+v", m)
			}
			if bank.Count() != 0 {
				t.Fatal("uncommitted proposal recorded as experience")
			}
			if !bytes.Equal(before, marshalTree(t, entry.Tree)) {
				t.Fatal("live predecessor changed")
			}
			diskAfter, err := os.ReadFile(originalPath)
			if err != nil || !bytes.Equal(diskBefore, diskAfter) {
				t.Fatal("persisted predecessor changed")
			}
		})
	}
}

func TestDeepSearchWriteFailurePreservesCommittedEvidence(t *testing.T) {
	bank, err := evolution.NewExperienceBank(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g, entry := experienceRecordingGardener(t, bank)
	g.cfg.MaxMutations = 0
	g.cfg.SnapshotDir = ""
	g.cfg.TranspositionTablePath = t.TempDir()
	if err := g.cfg.Registry.SaveTree(entry); err != nil {
		t.Fatal(err)
	}
	before := marshalTree(t, entry.Tree)
	originalPath := entry.FilePath
	diskBefore, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	entry.FilePath = filepath.Join(blocker, "tree.json")
	m := g.evolveTreeV2(entry, EvolveV2Config{DisableLocalSearch: true})
	if !m.DeepSearchUsed || !m.SaveFailed {
		t.Fatalf("deep adoption write not exercised: %+v", m)
	}
	if m.Mutations != 0 || m.Delta != 0 || m.Improved || bank.Count() != 0 {
		t.Fatalf("failed adoption acknowledged: %+v entries=%d", m, bank.Count())
	}
	if !bytes.Equal(before, marshalTree(t, entry.Tree)) {
		t.Fatal("live predecessor changed")
	}
	diskAfter, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(diskBefore, diskAfter) {
		t.Fatal("persisted predecessor changed")
	}
}
