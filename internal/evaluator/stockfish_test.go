package evaluator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── Store/Probe round trip ───

func TestTranspositionTable_StoreProbeRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 10)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()

	tt.Store(tree, "task-a", TranspositionEntry{Outcome: "success", SuccessRate: 0.9})

	entry, ok := tt.Probe(tree, "task-a")
	if !ok {
		t.Fatal("expected entry to be found after Store")
	}
	if entry.Outcome != "success" || entry.SuccessRate != 0.9 {
		t.Errorf("round trip mismatch: got %+v", entry)
	}
	// Store must stamp a monotonically increasing marker (insertion order) on every
	// entry so eviction can later pick the lowest-value one deterministically.
	if entry.InsertedAt <= 0 {
		t.Errorf("expected Store to stamp a positive InsertedAt marker, got %d", entry.InsertedAt)
	}
}

func TestTranspositionTable_StoreProbeMiss(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 10)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()

	if _, ok := tt.Probe(tree, "never-stored"); ok {
		t.Error("expected Probe to miss for a task that was never stored")
	}
}

// ─── Save/load round trip ───

func TestTranspositionTable_SaveLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 10)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()

	tt.Store(tree, "task-a", TranspositionEntry{Outcome: "success", SuccessRate: 0.5})
	tt.Store(tree, "task-b", TranspositionEntry{Outcome: "failure", SuccessRate: 0.1})

	if err := tt.Save(); err != nil {
		t.Fatal(err)
	}

	tt2, err := NewTranspositionTable(tmpDir, 10)
	if err != nil {
		t.Fatal(err)
	}

	entryA, ok := tt2.Probe(tree, "task-a")
	if !ok {
		t.Fatal("expected task-a to survive save/reload")
	}
	entryB, ok := tt2.Probe(tree, "task-b")
	if !ok {
		t.Fatal("expected task-b to survive save/reload")
	}

	if entryA.Outcome != "success" || entryB.Outcome != "failure" {
		t.Errorf("save/load mismatch: a=%+v b=%+v", entryA, entryB)
	}

	// The insertion-order marker must survive persistence too — eviction after a
	// reload needs it to keep picking deterministically.
	if entryA.InsertedAt <= 0 || entryB.InsertedAt <= 0 {
		t.Errorf("expected InsertedAt markers to survive reload, got a=%d b=%d", entryA.InsertedAt, entryB.InsertedAt)
	}
	if entryA.InsertedAt >= entryB.InsertedAt {
		t.Errorf("expected task-a InsertedAt (%d) < task-b InsertedAt (%d) after reload", entryA.InsertedAt, entryB.InsertedAt)
	}
}

// ─── Save persists atomically ───

// TestTranspositionTable_Save_CreatesMissingParentDirs verifies that Save can
// write to a path whose parent directories don't yet exist. NewTranspositionTable
// always creates its dir up front, so this test builds the struct directly to
// exercise the case a caller-supplied path is missing its nested parents.
func TestTranspositionTable_Save_CreatesMissingParentDirs(t *testing.T) {
	tt := &TranspositionTable{
		entries: map[string]TranspositionEntry{
			"key-a": {Outcome: "success", SuccessRate: 0.9, InsertedAt: 1},
		},
		path:    filepath.Join(t.TempDir(), "nested", "sub", "transposition.json"),
		maxSize: 10,
	}

	if err := tt.Save(); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if _, err := os.Stat(tt.path); err != nil {
		t.Fatalf("expected file to exist at %s: %v", tt.path, err)
	}
}

// TestTranspositionTable_Save_NoLeftoverTmpFile mirrors
// util/persist_test.go's TestSaveJSONAtomic_NoLeftoverTmpFile.
func TestTranspositionTable_Save_NoLeftoverTmpFile(t *testing.T) {
	tt := &TranspositionTable{
		entries: map[string]TranspositionEntry{
			"key-a": {Outcome: "success", SuccessRate: 0.9, InsertedAt: 1},
		},
		path:    filepath.Join(t.TempDir(), "transposition.json"),
		maxSize: 10,
	}

	if err := tt.Save(); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if _, err := os.Stat(tt.path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .tmp file, stat err = %v", err)
	}
}

// ─── Deterministic lowest-value eviction ───

// TestTranspositionTable_Store_EvictsLowestValueDeterministically verifies that once
// entries exceed maxSize, Store evicts the entry with the lowest InsertedAt marker
// (the oldest inserted entry) rather than whichever key Go's random map iteration
// order happens to visit first.
func TestTranspositionTable_Store_EvictsLowestValueDeterministically(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 3)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()

	tasks := []string{"task-a", "task-b", "task-c", "task-d"}
	for _, task := range tasks {
		tt.Store(tree, task, TranspositionEntry{Outcome: "success"})
	}

	if tt.Stats() != 3 {
		t.Fatalf("expected 3 entries after eviction, got %d", tt.Stats())
	}

	// task-a was inserted first (lowest InsertedAt) so it must be the one evicted.
	if _, ok := tt.Probe(tree, "task-a"); ok {
		t.Error("expected task-a (oldest / lowest InsertedAt) to be evicted, but it survived")
	}
	for _, task := range []string{"task-b", "task-c", "task-d"} {
		if _, ok := tt.Probe(tree, task); !ok {
			t.Errorf("expected %s to survive eviction, but it was evicted", task)
		}
	}
}

// TestTranspositionTable_Store_EvictsLowestValueRepeated repeats the eviction
// scenario several times. Go's map iteration order is randomized per-run, so a
// random-order eviction would non-deterministically evict a different entry across
// trials; a fix based on comparing InsertedAt values evicts the same, oldest entry
// every time.
func TestTranspositionTable_Store_EvictsLowestValueRepeated(t *testing.T) {
	for trial := range 5 {
		tmpDir := t.TempDir()
		tt, err := NewTranspositionTable(tmpDir, 2)
		if err != nil {
			t.Fatal(err)
		}
		tree := evolution.DefaultTree()

		tt.Store(tree, "first", TranspositionEntry{Outcome: "success"})
		tt.Store(tree, "second", TranspositionEntry{Outcome: "success"})
		tt.Store(tree, "third", TranspositionEntry{Outcome: "success"})

		if _, ok := tt.Probe(tree, "first"); ok {
			t.Errorf("trial %d: expected 'first' (oldest) to be evicted deterministically", trial)
		}
		if _, ok := tt.Probe(tree, "second"); !ok {
			t.Errorf("trial %d: expected 'second' to survive eviction", trial)
		}
		if _, ok := tt.Probe(tree, "third"); !ok {
			t.Errorf("trial %d: expected 'third' to survive eviction", trial)
		}
	}
}

// TestTranspositionTable_Store_EvictionPreservesLowestAmongSurvivors verifies that
// after an eviction, the surviving entry with the next-lowest InsertedAt is the
// next one evicted — i.e. eviction always removes the single lowest-value entry,
// not an arbitrary one.
func TestTranspositionTable_Store_EvictionPreservesLowestAmongSurvivors(t *testing.T) {
	tmpDir := t.TempDir()
	tt, err := NewTranspositionTable(tmpDir, 2)
	if err != nil {
		t.Fatal(err)
	}
	tree := evolution.DefaultTree()

	tt.Store(tree, "one", TranspositionEntry{Outcome: "success"})
	tt.Store(tree, "two", TranspositionEntry{Outcome: "success"})
	// Table is now full at maxSize=2 with {one, two}. Storing "three" must evict "one".
	tt.Store(tree, "three", TranspositionEntry{Outcome: "success"})
	// Table is now {two, three}. Storing "four" must evict "two" (now the oldest).
	tt.Store(tree, "four", TranspositionEntry{Outcome: "success"})

	if _, ok := tt.Probe(tree, "one"); ok {
		t.Error("expected 'one' to have been evicted first")
	}
	if _, ok := tt.Probe(tree, "two"); ok {
		t.Error("expected 'two' to have been evicted second")
	}
	if _, ok := tt.Probe(tree, "three"); !ok {
		t.Error("expected 'three' to survive")
	}
	if _, ok := tt.Probe(tree, "four"); !ok {
		t.Error("expected 'four' to survive")
	}
}
