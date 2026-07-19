package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func TestMutatedTreeOverrideRoundTrip(t *testing.T) {
	t.Setenv("BT_MUTATED_TREES_DIR", t.TempDir())
	tree := &evolution.SerializableNode{Type: "Sequence", Name: "root",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "x"}}}
	if err := SaveMutatedTree("goal:automate_demo", tree); err != nil {
		t.Fatal(err)
	}
	got := LoadMutatedTreeOverride("goal:automate_demo")
	if got == nil || got.Name != "root" || len(got.Children) != 1 || got.Children[0].Name != "x" {
		t.Fatalf("override round-trip failed: %+v", got)
	}
	if LoadMutatedTreeOverride("no_such_tree") != nil {
		t.Fatal("missing override must return nil")
	}
}

func TestMutatedTreeFilenameSanitized(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_MUTATED_TREES_DIR", dir)
	if err := SaveMutatedTree("../../etc/passwd", &evolution.SerializableNode{Type: "Sequence", Name: "r",
		Children: []evolution.SerializableNode{{Type: "Action", Name: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if LoadMutatedTreeOverride("../../etc/passwd") == nil {
		t.Fatal("sanitized ID must still round-trip")
	}
}

func TestLoadMutatedTreeOverride_CorruptFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BT_MUTATED_TREES_DIR", dir)
	path := filepath.Join(dir, sanitizeTreeID("goal:corrupt")+".json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadMutatedTreeOverride("goal:corrupt"); got != nil {
		t.Fatalf("corrupt override must return nil, got %+v", got)
	}
}
