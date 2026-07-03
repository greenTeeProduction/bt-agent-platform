package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// Evolution progress must survive restarts: a persisted tree-<name>.json for a
// builtin name is the evolved state SaveTree wrote and takes precedence over
// the compiled-in builtin definition on reload.
func TestRegistry_PersistedTreeOverridesBuiltin(t *testing.T) {
	dir := t.TempDir()

	evolved := evolution.DefaultTree()
	evolved.Children = append(evolved.Children, evolution.SerializableNode{
		Type: "Action", Name: "EvolvedMarkerNode",
	})
	data, err := json.MarshalIndent(evolved, "", "  ")
	if err != nil {
		t.Fatalf("marshal evolved tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tree-default.json"), data, 0o644); err != nil {
		t.Fatalf("write evolved tree: %v", err)
	}

	reg := NewRegistry(dir)

	var found bool
	for _, e := range reg.List() {
		if e.Name != "default" {
			continue
		}
		found = true
		if !hasNodeNamed(e.Tree, "EvolvedMarkerNode") {
			t.Error("registry loaded builtin instead of persisted evolved tree for 'default'")
		}
	}
	if !found {
		t.Fatal("no 'default' entry in registry")
	}

	// No duplicate entry for the same name.
	count := 0
	for _, e := range reg.List() {
		if e.Name == "default" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'default' entry, got %d", count)
	}
}

// A corrupt persisted file must not shadow the builtin — fall back to code.
func TestRegistry_CorruptPersistedTreeFallsBackToBuiltin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tree-default.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt tree: %v", err)
	}

	reg := NewRegistry(dir)
	for _, e := range reg.List() {
		if e.Name == "default" {
			if e.Tree == nil {
				t.Error("corrupt persisted file left 'default' with nil tree")
			}
			return
		}
	}
	t.Fatal("no 'default' entry in registry")
}
