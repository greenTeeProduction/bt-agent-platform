package evaluator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// ─── load() deep coverage: trim overflow with >maxSize entries ───

func TestTranspositionTable_Load_TrimOverflowExact(t *testing.T) {
	tmpDir := t.TempDir()
	// Write a file with 5 entries directly
	tree := evolution.DefaultTree()
	entries := make(map[string]TranspositionEntry)
	treeHash := hashTree(tree)
	for i := 0; i < 5; i++ {
		key := treeHash + ":" + string(rune('a'+i))
		entries[key] = TranspositionEntry{Outcome: "success"}
	}
	data, _ := json.Marshal(entries)
	path := filepath.Join(tmpDir, "transposition.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// load with maxSize=3 — should trim to 3
	tt := &TranspositionTable{
		entries: make(map[string]TranspositionEntry),
		path:    path,
		maxSize: 3,
	}
	tt.load()

	if tt.Stats() != 3 {
		t.Errorf("expected 3 entries after trim, got %d", tt.Stats())
	}
}

// ─── json.Unmarshal with malformed data (silent error) ───

func TestTranspositionTable_Load_JSONError(t *testing.T) {
	tmpDir := t.TempDir()
	// Write a file that is valid but has the wrong structure (an array instead of map)
	path := filepath.Join(tmpDir, "transposition.json")
	if err := os.WriteFile(path, []byte("[1,2,3]"), 0644); err != nil {
		t.Fatal(err)
	}

	tt, err := NewTranspositionTable(tmpDir, 100)
	if err != nil {
		t.Fatal(err)
	}
	// json.Unmarshal will fail but silently — entries should be empty
	if tt.Stats() != 0 {
		t.Errorf("expected 0 entries after unmarshal error, got %d", tt.Stats())
	}
}

// ─── NewTranspositionTable with directory create error ───

func TestNewTranspositionTable_MkdirError(t *testing.T) {
	// /proc is not writable
	_, err := NewTranspositionTable("/proc/readonly-test", 100)
	if err == nil {
		t.Error("expected error creating TT in /proc")
	}
}
