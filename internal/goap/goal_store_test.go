package goap

import (
	"path/filepath"
	"testing"
)

func TestGoalStore_LoadEmpty(t *testing.T) {
	store, err := NewGoalStore(filepath.Join(t.TempDir(), "goals"))
	if err != nil {
		t.Fatalf("NewGoalStore: %v", err)
	}
	goals, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(goals) != 0 {
		t.Fatalf("expected empty store, got %d goals", len(goals))
	}
}

func TestGoalStore_AddRemoveRoundtrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "goals")
	store, err := NewGoalStore(dir)
	if err != nil {
		t.Fatalf("NewGoalStore: %v", err)
	}

	g1 := &Goal{Name: "a", Priority: 0.5, Conditions: WorldState{"task_automated": true}}
	g2 := &Goal{Name: "b", Priority: 0.8, Deadline: 10, Conditions: WorldState{"has_result": true}}
	if err := store.Add(g1); err != nil {
		t.Fatalf("Add g1: %v", err)
	}
	if err := store.Add(g2); err != nil {
		t.Fatalf("Add g2: %v", err)
	}

	// Upsert by name replaces.
	g1v2 := &Goal{Name: "a", Priority: 0.9, Conditions: WorldState{"task_automated": true}}
	if err := store.Add(g1v2); err != nil {
		t.Fatalf("Add g1v2: %v", err)
	}

	// Reopen to prove persistence across instances.
	reopened, err := NewGoalStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	goals, err := reopened.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(goals) != 2 {
		t.Fatalf("expected 2 goals, got %d", len(goals))
	}
	byName := map[string]*Goal{}
	for _, g := range goals {
		byName[g.Name] = g
	}
	if byName["a"].Priority != 0.9 {
		t.Fatalf("upsert did not replace: %+v", byName["a"])
	}
	if byName["b"].Deadline != 10 {
		t.Fatalf("deadline lost in roundtrip: %+v", byName["b"])
	}
	if byName["b"].Conditions["has_result"] != true {
		t.Fatalf("conditions lost in roundtrip: %v", byName["b"].Conditions)
	}

	removed, err := reopened.Remove("a")
	if err != nil || !removed {
		t.Fatalf("Remove(a) = %v, %v", removed, err)
	}
	removed, err = reopened.Remove("missing")
	if err != nil || removed {
		t.Fatalf("Remove(missing) = %v, %v", removed, err)
	}
	goals, _ = reopened.Load()
	if len(goals) != 1 || goals[0].Name != "b" {
		t.Fatalf("after remove: %+v", goals)
	}
}

func TestGoalStore_Queue(t *testing.T) {
	store, err := NewGoalStore(filepath.Join(t.TempDir(), "goals"))
	if err != nil {
		t.Fatalf("NewGoalStore: %v", err)
	}
	_ = store.Add(&Goal{Name: "low", Priority: 0.2, Conditions: WorldState{"a": true}})
	_ = store.Add(&Goal{Name: "high", Priority: 0.9, Conditions: WorldState{"b": true}})

	queue, err := store.Queue()
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	if queue.Len() != 2 {
		t.Fatalf("queue len = %d", queue.Len())
	}
	if got := queue.SelectGoal(WorldState{}); got.Name != "high" {
		t.Fatalf("SelectGoal = %q", got.Name)
	}
}

func TestGoalStore_ValidatesInput(t *testing.T) {
	if _, err := NewGoalStore(""); err == nil {
		t.Fatal("expected error for empty dir")
	}
	store, _ := NewGoalStore(t.TempDir())
	if err := store.Add(nil); err == nil {
		t.Fatal("expected error for nil goal")
	}
	if err := store.Add(&Goal{}); err == nil {
		t.Fatal("expected error for unnamed goal")
	}
}
