package blackboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_PersistenceSessionScope(t *testing.T) {
	dir := t.TempDir()
	m1 := DefaultManager()
	if err := m1.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeSession, ID: "pipe_abc"}
	if err := m1.Set(scope, "steps/a/output", "hello persisted", "summary", "text"); err != nil {
		t.Fatal(err)
	}

	path := m1.persistFile(scope)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected persist file: %v", err)
	}

	m2 := DefaultManager()
	if err := m2.EnablePersistence(dir); err != nil {
		t.Fatal(err)
	}
	e, err := m2.Get(scope, "steps/a/output")
	if err != nil {
		t.Fatal(err)
	}
	if e.Value != "hello persisted" {
		t.Fatalf("reload mismatch: %q", e.Value)
	}
}

func TestManager_RunScopeNotPersisted(t *testing.T) {
	dir := t.TempDir()
	m := DefaultManager()
	_ = m.EnablePersistence(dir)
	scope := Scope{Kind: ScopeRun, ID: "run_ephemeral"}
	_ = m.Set(scope, "work/x", "temp", "", "text")
	if _, err := os.Stat(filepath.Join(dir, "run", "run_ephemeral.json")); !os.IsNotExist(err) {
		t.Fatal("run scope should not create persist file")
	}
}
