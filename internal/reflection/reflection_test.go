package reflection

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	if store.Dir() != dir {
		t.Errorf("Dir mismatch: %s vs %s", store.Dir(), dir)
	}

	// Empty store
	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}

	// Save a record
	r := &Record{
		TaskID:        "t1",
		Task:          "test task",
		Plan:          "do it",
		WhatWentWell:  []string{"good"},
		WhatToImprove: []string{"faster"},
		Outcome:       Success,
		DurationMs:    100,
	}
	if err := store.Save(r); err != nil {
		t.Fatal(err)
	}

	// Load
	records, err = store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	loaded := records[0]
	if loaded.Task != "test task" {
		t.Errorf("task mismatch: %q", loaded.Task)
	}
	if loaded.Outcome != Success {
		t.Errorf("outcome mismatch: %s", loaded.Outcome)
	}
	if loaded.WhatWentWell[0] != "good" {
		t.Errorf("went_well mismatch: %q", loaded.WhatWentWell[0])
	}
	if loaded.Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
	if loaded.TaskID == "" {
		t.Error("expected non-empty task_id")
	}
}

func TestStore_AutoTaskID(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	r := &Record{Task: "no id"}
	if err := store.Save(r); err != nil {
		t.Fatal(err)
	}
	if r.TaskID == "" {
		t.Error("expected auto-generated task_id")
	}
}

func TestStore_MultipleRecords(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	for i := 0; i < 5; i++ {
		outcome := Success
		if i >= 3 {
			outcome = Failure
		}
		store.Save(&Record{
			TaskID:  fmt.Sprintf("task-%d", i), // unique IDs to avoid overwrite
			Task:    "task",
			Outcome: outcome,
		})
	}

	records, _ := store.LoadAll()
	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
	if store.CountFailures() != 2 {
		t.Errorf("expected 2 failures, got %d", store.CountFailures())
	}
}

func TestStore_CountFailures_Empty(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	if store.CountFailures() != 0 {
		t.Error("expected 0 failures in empty store")
	}
}

func TestStore_RecentFailures(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	// No failures yet
	if len(store.RecentFailures(5)) != 0 {
		t.Error("expected empty recent failures")
	}

	store.Save(&Record{TaskID: "t1", Outcome: Failure, Task: "fail1"})
	store.Save(&Record{TaskID: "t2", Outcome: Success, Task: "ok"})
	store.Save(&Record{TaskID: "t3", Outcome: Failure, Task: "fail2"})

	failures := store.RecentFailures(2)
	if len(failures) != 2 {
		t.Errorf("expected 2 recent failures, got %d", len(failures))
	}
}

func TestStore_NonExistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent", "subdir")
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("store dir should have been created")
	}
	_ = store
}

func TestRecord_JSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	original := &Record{
		TaskID:           "json-test",
		Task:             "complex task",
		Plan:             "multi\nline\nplan",
		WhatWentWell:     []string{"a", "b", "c"},
		WhatToImprove:    []string{"x", "y"},
		AdjustedBehavior: "added retry",
		Outcome:          Partial,
		DurationMs:       1234,
	}

	if err := store.Save(original); err != nil {
		t.Fatal(err)
	}

	records, _ := store.LoadAll()
	if len(records) != 1 {
		t.Fatal("expected 1 record")
	}

	loaded := records[0]
	if loaded.TaskID != original.TaskID {
		t.Error("task_id mismatch")
	}
	if loaded.Plan != original.Plan {
		t.Error("plan mismatch")
	}
	if len(loaded.WhatWentWell) != 3 {
		t.Error("what_went_well length mismatch")
	}
	if loaded.Outcome != Partial {
		t.Errorf("outcome: expected Partial, got %s", loaded.Outcome)
	}
	if loaded.DurationMs != 1234 {
		t.Errorf("duration_ms: expected 1234, got %d", loaded.DurationMs)
	}
}
