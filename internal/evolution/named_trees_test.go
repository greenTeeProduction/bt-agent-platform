package evolution

import (
	"path/filepath"
	"testing"
)

func TestTreeFileName_SanitizesUnsafeCharacters(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{"core:automate_reports", "tree-core_automate_reports.json"},
		{"finance:pitch/agent", "tree-finance_pitch_agent.json"},
		{`hybrid:a\b c`, "tree-hybrid_a_b_c.json"},
		{"  godev  ", "tree-godev.json"},
		{"v1.2-agent", "tree-v1.2-agent.json"},
	}
	for _, c := range cases {
		if got := TreeFileName(c.id); got != c.want {
			t.Errorf("TreeFileName(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestTreeStore_SaveNamedLoadNamed_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	ts, err := NewTreeStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	tree := &SerializableNode{
		Type: "Sequence",
		Name: "Generated_Main",
		Children: []SerializableNode{
			{Type: "Condition", Name: "ValidateInput"},
			{Type: "Action", Name: "ReflectOnOutcome"},
		},
	}

	path, err := ts.SaveNamed("core:automate_reports", tree)
	if err != nil {
		t.Fatalf("SaveNamed: %v", err)
	}
	if want := filepath.Join(dir, "tree-core_automate_reports.json"); path != want {
		t.Errorf("SaveNamed path = %q, want %q", path, want)
	}

	loaded, err := ts.LoadNamed("core:automate_reports")
	if err != nil {
		t.Fatalf("LoadNamed: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadNamed returned nil for a persisted tree")
	}
	if loaded.Name != "Generated_Main" || len(loaded.Children) != 2 {
		t.Errorf("roundtrip mismatch: name=%q children=%d", loaded.Name, len(loaded.Children))
	}
}

func TestTreeStore_LoadNamed_MissingReturnsNil(t *testing.T) {
	ts, err := NewTreeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tree, err := ts.LoadNamed("never:persisted")
	if err != nil {
		t.Fatalf("LoadNamed missing: unexpected error %v", err)
	}
	if tree != nil {
		t.Errorf("LoadNamed missing: expected nil, got %+v", tree)
	}
}

func TestTreeStore_SaveNamed_RejectsEmptyIDAndNilTree(t *testing.T) {
	ts, err := NewTreeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.SaveNamed("", &SerializableNode{Type: "Sequence", Name: "X"}); err == nil {
		t.Error("SaveNamed with empty id should error")
	}
	if _, err := ts.SaveNamed("some:id", nil); err == nil {
		t.Error("SaveNamed with nil tree should error")
	}
}
