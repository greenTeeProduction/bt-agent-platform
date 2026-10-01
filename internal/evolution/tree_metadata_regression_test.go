package evolution

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestTreeMetadataAtomicPrivateWriteAndFailurePreservesFile(t *testing.T) {
	store, err := NewTreeStore(filepath.Join(t.TempDir(), "nested", "trees"))
	if err != nil {
		t.Fatal(err)
	}
	original := &EvolutionMetadata{TreeID: "tree", Fitness: FitnessRecord{Score: 42}}
	if err := store.SaveMeta(original); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.MetaPath())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.MetaPath())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("metadata is not private")
	}
	invalid := &EvolutionMetadata{TreeID: "uncommitted", Fitness: FitnessRecord{Score: math.NaN()}}
	if err := store.SaveMeta(invalid); err == nil {
		t.Fatal("invalid metadata write acknowledged")
	}
	after, err := os.ReadFile(store.MetaPath())
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("failed metadata write changed complete file")
	}
	loaded, err := store.LoadMeta()
	if err != nil || loaded.TreeID != original.TreeID || loaded.Fitness.Score != 42 {
		t.Fatalf("metadata reload=%+v %v", loaded, err)
	}
}

func TestTreeStoreReadsRemainWithinConfiguredRoot(t *testing.T) {
	for _, kind := range []string{"tree", "metadata"} {
		t.Run(kind, func(t *testing.T) {
			store, err := NewTreeStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside.json")
			if err := os.WriteFile(outside, []byte(`{"name":"outside","tree_id":"outside"}`), 0600); err != nil {
				t.Fatal(err)
			}
			path := store.Path()
			if kind == "metadata" {
				path = store.MetaPath()
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Fatal(err)
			}
			if kind == "metadata" {
				_, err = store.LoadMeta()
			} else {
				_, err = store.Load()
			}
			if err == nil {
				t.Fatal("read escaped configured storage root")
			}
		})
	}
}

func TestTreeStoreRequiresExplicitRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	if _, err := NewTreeStore(""); err == nil {
		t.Fatal("empty root accepted")
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Fatal("empty root created cwd state")
	}
	if _, err := NewTreeStore("."); err != nil {
		t.Fatalf("explicit relative root rejected: %v", err)
	}
}
