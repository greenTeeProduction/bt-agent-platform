package blackboard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestReviewBlackboardSharedWrites(t *testing.T) {
	root := t.TempDir()
	scope := Scope{Kind: ScopeAgent, ID: "shared"}
	managers := make([]*Manager, 0, 3)
	for range 3 {
		m := DefaultManager()
		if err := m.EnablePersistence(root); err != nil {
			t.Fatal(err)
		}
		managers = append(managers, m)
	}
	var wg sync.WaitGroup
	for j, m := range managers {
		for i := range 30 {
			wg.Go(func() {
				if err := m.Set(scope, fmt.Sprintf("%d-%d", j, i), "value", "", "text"); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	entries, err := m.List(scope, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 90 {
		t.Fatalf("persisted %d want 90", len(entries))
	}
}
func TestReviewBlackboardScopeCollisions(t *testing.T) {
	root := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a/b", "a_b"} {
		if err := m.Set(Scope{Kind: ScopeAgent, ID: id}, "key", id, "", "text"); err != nil {
			t.Fatal(err)
		}
	}
	m = DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a/b", "a_b"} {
		e, err := m.Get(Scope{Kind: ScopeAgent, ID: id}, "key")
		if err != nil {
			t.Fatal(err)
		}
		if e.Value != id {
			t.Errorf("scope %q contains %q", id, e.Value)
		}
	}
	ids, err := m.ListPersistedScopeIDs(ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Errorf("ids=%v", ids)
	}
}
func TestReviewBlackboardProcesses(t *testing.T) {
	if root := os.Getenv("REVIEW_BB_ROOT"); root != "" {
		m := DefaultManager()
		if err := m.EnablePersistence(root); err != nil {
			t.Fatal(err)
		}
		for range 20 {
			if _, err := m.Append(Scope{Kind: ScopeAgent, ID: "process"}, "all", "x", "", "text"); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	root := t.TempDir()
	cmds := make([]*exec.Cmd, 0, 3)
	for range 3 {
		cmd := exec.Command(os.Args[0], "-test.run=^TestReviewBlackboardProcesses$")
		cmd.Env = append(os.Environ(), "REVIEW_BB_ROOT="+root)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	e, err := m.Get(Scope{Kind: ScopeAgent, ID: "process"}, "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Value) != 60 {
		t.Fatalf("lost process appends: %d of 60", len(e.Value))
	}
}
func TestReviewBlackboardLegacyMigration(t *testing.T) {
	root := t.TempDir()
	m := DefaultManager()
	if err := m.EnablePersistence(root); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"entries":{"key":{"value":"legacy"}}}`)
	for _, id := range []string{"legacy", "ambiguous_name"} {
		if err := os.WriteFile(filepath.Join(root, "agent", id+".json"), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if e, err := m.Get(Scope{Kind: ScopeAgent, ID: "legacy"}, "key"); err != nil || e.Value != "legacy" {
		t.Fatalf("safe legacy read %v %v", e, err)
	}
	if _, err := m.Get(Scope{Kind: ScopeAgent, ID: "ambiguous_name"}, "key"); err == nil {
		t.Error("ambiguous legacy payload attributed without proof")
	}
	if err := m.Set(Scope{Kind: ScopeAgent, ID: "legacy"}, "new", "new", "", "text"); err != nil {
		t.Fatal(err)
	}
	ids, err := m.ListPersistedScopeIDs(ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate migrated scope %q", id)
		}
		seen[id] = true
	}
}
func TestReviewRunScopeCardinalityBound(t *testing.T) {
	m := DefaultManager()
	accepted := 0
	for i := range 1100 {
		if err := m.Set(Scope{Kind: ScopeRun, ID: fmt.Sprint(i)}, "key", "value", "", "text"); err == nil {
			accepted++
		}
	}
	if accepted > 1024 {
		t.Fatalf("retained %d ephemeral scopes, want bounded at 1024", accepted)
	}
}
