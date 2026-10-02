package blackboard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelatedEntryGroupRejectsPartialMutationAndEviction(t *testing.T) {
	for _, fixture := range []string{"invalid-key", "oversized-entry", "group-evicted"} {
		t.Run(fixture, func(t *testing.T) {
			scope := Scope{Kind: ScopeAgent, ID: "owner"}
			manager := NewManager(map[ScopeKind]Limits{ScopeAgent: {MaxEntries: 2, MaxTotalBytes: 100, Evict: true}})
			root := t.TempDir()
			if err := manager.EnablePersistence(root); err != nil {
				t.Fatal(err)
			}
			if err := manager.Set(scope, "old", "retained value", "", "text"); err != nil {
				t.Fatal(err)
			}
			path := scopePath(root, scope)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			entries := []Entry{{Key: "new-output", Value: "new value"}, {Key: "new-id", Value: "new run"}}
			switch fixture {
			case "invalid-key":
				entries[1].Key = ""
			case "oversized-entry":
				entries[1].Value = strings.Repeat("x", 101)
			case "group-evicted":
				entries = append(entries, Entry{Key: "new-task", Value: "new task"})
			}
			if err := manager.SetEntriesWithContext(context.Background(), scope, entries); err == nil {
				t.Fatal("invalid/incomplete group acknowledged")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("failed group changed durable evidence")
			}
			cached := manager.stores[string(scope.Kind)+":"+scope.ID]
			old, ok := cached.get("old")
			if !ok || old.Value != "retained value" {
				t.Fatal("failed group published eviction/cache mutation")
			}
			if _, ok := cached.get("new-output"); ok {
				t.Fatal("failed group published its prefix")
			}
		})
	}
}

func TestRelatedEntryGroupCommitsTogetherAndHonorsDeadline(t *testing.T) {
	root := t.TempDir()
	manager, err := NewPersistentManager(root)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeAgent, ID: "owner"}
	entries := []Entry{{Key: "output", Value: "healthy evidence"}, {Key: "run", Value: "owned-run"}, {Key: "task", Value: "owned-task"}}
	if err := manager.SetEntriesWithContext(context.Background(), scope, entries); err != nil {
		t.Fatal(err)
	}
	disk, err := NewPersistentManager(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		got, err := disk.Get(scope, entry.Key)
		if err != nil || got.Value != entry.Value {
			t.Fatalf("group did not survive fresh owner: %+v %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := manager.SetEntriesWithContext(ctx, scope, []Entry{{Key: "output", Value: "canceled replacement"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled group admitted: %v", err)
	}
	got, err := disk.Get(scope, "output")
	if err != nil || got.Value != "healthy evidence" {
		t.Fatal("canceled group replaced retained output")
	}
	if err := os.RemoveAll(filepath.Join(root, "agent")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent"), []byte("blocked root"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetEntriesWithContext(context.Background(), scope, []Entry{{Key: "output", Value: "uncommitted"}, {Key: "run", Value: "uncommitted-run"}}); err == nil {
		t.Fatal("unwritable group acknowledged")
	}
	cached := manager.stores[string(scope.Kind)+":"+scope.ID]
	output, _ := cached.get("output")
	run, _ := cached.get("run")
	if output.Value != "healthy evidence" || run.Value != "owned-run" {
		t.Fatal("unwritable group published partial cache")
	}
}

func TestEntryMetadataCannotMutateCommittedScopeOutsideTransaction(t *testing.T) {
	manager, err := NewPersistentManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{Kind: ScopeAgent, ID: "owner"}
	metadata := map[string]string{"owner": "committed"}
	if err := manager.SetEntriesWithContext(context.Background(), scope, []Entry{{Key: "proof", Value: "retained", Metadata: metadata}}); err != nil {
		t.Fatal(err)
	}
	metadata["owner"] = "changed by writer"
	cached := manager.stores[string(scope.Kind)+":"+scope.ID]
	entry, _ := cached.get("proof")
	if entry.Metadata["owner"] != "committed" {
		t.Fatal("writer mutated published metadata")
	}
	entry.Metadata["owner"] = "changed by reader"
	listed := cached.list("", 1)
	listed[0].Metadata["owner"] = "changed by listing"
	after, _ := cached.get("proof")
	if after.Metadata["owner"] != "committed" {
		t.Fatal("read/list mutated committed metadata without persistence")
	}
}
