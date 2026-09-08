package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistenceRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/../escaped.json"
	if err := SaveJSONAtomic(path, "unsafe"); err == nil {
		t.Fatal("accepted parent traversal")
	}
	var v any
	if err := LoadJSON(path, &v); err == nil {
		t.Fatal("accepted parent traversal read")
	}
}

func TestPersistenceDoesNotFollowTemporarySymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.Symlink(victim, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := SaveJSONAtomic(path, "safe"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("temporary symlink clobbered victim: %q, %v", data, err)
	}
}

func TestPersistenceRejectsEscapingReadSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.json")
	if err := os.WriteFile(victim, []byte(`"secret"`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := LoadJSON(path, &got); err == nil {
		t.Fatalf("read escaping symlink: %q", got)
	}
}

func TestPersistencePrivateDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := SaveJSONAtomic(path, "private"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, filepath.Dir(path)} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0007 != 0 {
			t.Errorf("world accessible: %s %o", p, st.Mode().Perm())
		}
	}
}
