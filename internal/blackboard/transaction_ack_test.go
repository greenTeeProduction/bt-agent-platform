package blackboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestMutationFailurePreservesCacheAndAccounting(t *testing.T) {
	for _, evict := range []bool{false, true} {
		t.Run(map[bool]string{false: "strict", true: "eviction"}[evict], func(t *testing.T) {
			m := NewManager(map[ScopeKind]Limits{ScopeRun: {MaxEntries: 3, MaxTotalBytes: 6, Evict: evict}})
			scope := Scope{Kind: ScopeRun, ID: "limits"}
			for _, key := range []string{"old", "other"} {
				if err := m.Set(scope, key, "abc", "", "text"); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := m.List(scope, "", 0)
			if err := m.Set(scope, "old", "oversized", "", "text"); err == nil {
				t.Fatal("oversized update was admitted")
			}
			after, err := m.List(scope, "", 0)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("failed update changed entries: before=%+v after=%+v err=%v", before, after, err)
			}
			if err := m.Set(scope, "old", "xyz", "", "text"); err != nil {
				t.Fatal(err)
			}
			if err := m.Set(scope, "extra", "z", "", "text"); (err == nil) != evict {
				t.Fatalf("byte accounting after failed update: evict=%v err=%v", evict, err)
			}
		})
	}
}

func TestPersistentMutationFailureDoesNotPublishCache(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("filesystem permission failure requires an unprivileged process")
	}
	for _, operation := range []string{"set", "append", "delete"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			m := DefaultManager()
			if err := m.EnablePersistence(root); err != nil {
				t.Fatal(err)
			}
			scope := Scope{Kind: ScopeSession, ID: "failed-commit"}
			if err := m.Set(scope, "evidence", "committed", "", "text"); err != nil {
				t.Fatal(err)
			}
			path := m.persistFile(scope)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "session")
			if err := os.Chmod(dir, 0500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
			mutate := func() error {
				switch operation {
				case "set":
					return m.Set(scope, "evidence", "uncommitted", "", "text")
				case "append":
					_, err := m.Append(scope, "evidence", "uncommitted", "|", "text")
					return err
				default:
					return m.Delete(scope, "evidence")
				}
			}
			if err := mutate(); err == nil {
				t.Fatal("unwritable directory acknowledged a mutation")
			}
			id, _ := m.scopeID(scope)
			m.mu.RLock()
			cached, ok := m.stores[id].get("evidence")
			m.mu.RUnlock()
			after, err := os.ReadFile(path)
			if !ok || cached.Value != "committed" || err != nil || string(before) != string(after) {
				t.Fatalf("failed commit changed cache/file: cached=%+v ok=%v err=%v", cached, ok, err)
			}
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := mutate(); err != nil {
				t.Fatalf("persistence repair could not retry mutation: %v", err)
			}
		})
	}
}

func TestSetWithContextBoundsScopeAndFileContention(t *testing.T) {
	for _, contention := range []string{"scope", "file"} {
		t.Run(contention, func(t *testing.T) {
			m := DefaultManager()
			if err := m.EnablePersistence(t.TempDir()); err != nil {
				t.Fatal(err)
			}
			scope := Scope{Kind: ScopeSession, ID: "held"}
			id, _ := m.scopeID(scope)
			var release func()
			if contention == "scope" {
				gate := m.acquireGate(id)
				release = func() { m.releaseGate(id, gate) }
			} else {
				var err error
				release, err = reliability.AcquireFileLockWithContext(t.Context(), m.persistFile(scope))
				if err != nil {
					t.Fatal(err)
				}
			}
			defer func() {
				if release != nil {
					release()
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := m.SetWithContext(ctx, scope, "unadmitted", "value", "", "text")
			if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
				t.Fatalf("caller deadline ignored: elapsed=%s err=%v", time.Since(start), err)
			}
			if err := m.Set(Scope{Kind: ScopeRun, ID: "unrelated"}, "key", "value", "", "text"); err != nil {
				t.Fatal(err)
			}
			release()
			release = nil
			if _, err := m.Get(scope, "unadmitted"); err == nil {
				t.Fatal("canceled write published a value")
			}
			if err := m.SetWithContext(t.Context(), scope, "repaired", "value", "", "text"); err != nil {
				t.Fatal(err)
			}
			m.gatesMu.Lock()
			remaining := len(m.gates)
			m.gatesMu.Unlock()
			if remaining != 0 {
				t.Fatalf("canceled wait retained %d gates", remaining)
			}
		})
	}
}
