package reflection

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_NewStore_MkdirAllError(t *testing.T) {
	_, err := NewStore("/proc/reflection-test-unwritable")
	if err == nil {
		t.Error("expected error for unwritable path")
	}
}

func TestStore_Save_RenameCleanup(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	r := &Record{TaskID: "rename-cleanup-test", Task: "test", Outcome: Success}
	if err := store.Save(r); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("unexpected .tmp file remaining: %s", e.Name())
		}
	}

	expectedPath := filepath.Join(dir, "reflection-rename-cleanup-test.json")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Error("expected reflection file to exist at", expectedPath)
	}
}

func TestStore_Save_WriteError(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	if err := os.Chmod(dir, 0555); err != nil {
		t.Skipf("cannot make dir read-only: %v", err)
	}
	defer func() { _ = os.Chmod(dir, 0755) }()

	r := &Record{TaskID: "write-err", Task: "test", Outcome: Success}
	err := store.Save(r)
	if err == nil {
		t.Error("expected error when writing to read-only directory")
	}
}

func TestStore_LoadAll_WithBadJSON(t *testing.T) {
	dir := t.TempDir()
	store := &Store{dir: dir}

	_ = store.Save(&Record{TaskID: "load-test", Task: "test", Outcome: Success})

	badPath := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badPath, []byte("{invalid json}"), 0644)

	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 valid record (bad JSON skipped), got %d", len(records))
	}
}

func TestStore_LoadAll_NonJSONFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	store := &Store{dir: dir}

	nonJSONPath := filepath.Join(dir, "not-a-json.txt")
	_ = os.WriteFile(nonJSONPath, []byte("hello"), 0644)

	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records (no JSON files), got %d", len(records))
	}
}

func TestStore_LoadAll_DirNotExist(t *testing.T) {
	store := &Store{dir: "/nonexistent-path-12345-test"}
	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for nonexistent dir, got %d", len(records))
	}
}

func TestStore_LoadAll_ReadDirNonNotExistError(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	_ = os.WriteFile(filePath, []byte(""), 0644)

	store := &Store{dir: filePath}
	records, err := store.LoadAll()
	if err == nil {
		t.Error("expected error when store dir is a file, not a directory")
	}
	if records != nil {
		t.Error("expected nil records on error")
	}
}

func TestStore_CountFailures_NonexistentDir(t *testing.T) {
	store := &Store{dir: "/nonexistent-path-12345-test"}
	count := store.CountFailures()
	if count != 0 {
		t.Errorf("expected 0 for nonexistent dir, got %d", count)
	}
}

func TestStore_CountFailures_LoadAllError(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	_ = os.WriteFile(filePath, []byte(""), 0644)

	store := &Store{dir: filePath}
	count := store.CountFailures()
	if count != 0 {
		t.Errorf("expected 0 when LoadAll fails, got %d", count)
	}
}

func TestStore_CountFailures_MixedRecords(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	_ = store.Save(&Record{TaskID: "a", Outcome: Success, Task: "ok"})
	_ = store.Save(&Record{TaskID: "b", Outcome: Failure, Task: "fail"})
	_ = store.Save(&Record{TaskID: "c", Outcome: Partial, Task: "partial"})
	_ = store.Save(&Record{TaskID: "d", Outcome: Failure, Task: "fail2"})

	count := store.CountFailures()
	if count != 2 {
		t.Errorf("expected 2 failures (b, d), got %d", count)
	}
}

func TestStore_CountFailures_NoFailures(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	_ = store.Save(&Record{TaskID: "a", Outcome: Success, Task: "ok"})
	_ = store.Save(&Record{TaskID: "b", Outcome: Partial, Task: "partial"})

	count := store.CountFailures()
	if count != 0 {
		t.Errorf("expected 0 failures (none recorded), got %d", count)
	}
}

func TestStore_RecentFailures_NonexistentDir(t *testing.T) {
	store := &Store{dir: "/nonexistent-path-12345-test"}
	failures := store.RecentFailures(5)
	if len(failures) != 0 {
		t.Errorf("expected 0 failures for nonexistent dir, got %d", len(failures))
	}
}

func TestStore_RecentFailures_LoadAllError(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	_ = os.WriteFile(filePath, []byte(""), 0644)

	store := &Store{dir: filePath}
	failures := store.RecentFailures(5)
	if len(failures) != 0 {
		t.Errorf("expected 0 failures when LoadAll fails, got %d", len(failures))
	}
}

func TestStore_RecentFailures_Ordering(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	_ = store.Save(&Record{TaskID: "f1", Outcome: Failure, Task: "fail1"})
	_ = store.Save(&Record{TaskID: "s1", Outcome: Success, Task: "ok1"})
	_ = store.Save(&Record{TaskID: "f2", Outcome: Failure, Task: "fail2"})
	_ = store.Save(&Record{TaskID: "f3", Outcome: Failure, Task: "fail3"})
	_ = store.Save(&Record{TaskID: "s2", Outcome: Success, Task: "ok2"})

	failures := store.RecentFailures(2)
	if len(failures) != 2 {
		t.Errorf("expected 2 recent failures, got %d", len(failures))
	}
	if len(failures) > 0 && failures[0].TaskID != "f3" {
		t.Errorf("expected most recent failure f3, got %s", failures[0].TaskID)
	}
	if len(failures) > 1 && failures[1].TaskID != "f2" {
		t.Errorf("expected second most recent failure f2, got %s", failures[1].TaskID)
	}

	failures = store.RecentFailures(10)
	if len(failures) != 3 {
		t.Errorf("expected 3 failures (all), got %d", len(failures))
	}
}

func TestStore_LoadAll_WithSubdirs(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(dir)

	// Save a record
	_ = store.Save(&Record{TaskID: "subdir-test", Task: "test", Outcome: Success})

	// Create a subdirectory inside the store dir (should be skipped by LoadAll)
	_ = os.MkdirAll(filepath.Join(dir, "subdir"), 0755)

	records, err := store.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	// Should still find only the JSON record, not the subdir
	if len(records) != 1 {
		t.Errorf("expected 1 record (subdir skipped), got %d", len(records))
	}
}
