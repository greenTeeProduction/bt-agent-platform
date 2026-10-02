package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestIslandAdoptionRejectsMetaValidation(t *testing.T) {
	g, entry, im, records := islandAdoptionFixture(t, "island_meta")
	g.cfg.MetaValidator = evolution.NewMetaValidator(evolution.MetaValidatorConfig{MinComposite: 1000})
	assertIslandAdoptionSkipped(t, g, entry, im, records)
}

func TestIslandAdoptionSnapshotsPredecessor(t *testing.T) {
	g, entry, _, records := islandAdoptionFixture(t, "island_snapshot")
	before, _ := json.Marshal(entry.Tree)
	g.cfg.SnapshotDir = t.TempDir()
	if !g.adoptIslandWinner(entry, records, islandV2Config()) {
		t.Fatal("eligible winner rejected")
	}
	restored, err := evolution.RestoreTree(entry.Name, g.cfg.SnapshotDir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(restored)
	if string(got) != string(before) {
		t.Fatalf("snapshot lost predecessor: %s", got)
	}
}

func TestIslandAdoptionFailurePreservesLiveTree(t *testing.T) {
	for _, failure := range []string{"snapshot", "tree-write"} {
		t.Run(failure, func(t *testing.T) {
			g, entry, _, records := islandAdoptionFixture(t, "island_failure_"+failure)
			before, _ := json.Marshal(entry.Tree)
			original, err := os.ReadFile(entry.FilePath)
			if err != nil {
				t.Fatal(err)
			}
			blocker := filepath.Join(t.TempDir(), "blocked")
			if err := os.WriteFile(blocker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			if failure == "snapshot" {
				g.cfg.SnapshotDir = filepath.Join(blocker, "snapshots")
			} else {
				entry.FilePath = filepath.Join(blocker, "tree.json")
			}
			if g.adoptIslandWinner(entry, records, islandV2Config()) {
				t.Fatal("failed persistence reported adoption")
			}
			after, _ := json.Marshal(entry.Tree)
			if string(after) != string(before) {
				t.Fatal("failed commit mutated live tree")
			}
			path := g.cfg.Registry.List()[0].FilePath
			disk, err := os.ReadFile(path)
			if err != nil || string(disk) != string(original) {
				t.Fatal("failed adoption changed predecessor file")
			}
		})
	}
}
