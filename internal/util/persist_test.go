package util

import (
	"os"
	"path/filepath"
	"testing"
)

type persistFixture struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestSaveJSONAtomic_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "fixture.json")
	want := persistFixture{Name: "alpha", Count: 3}

	if err := SaveJSONAtomic(path, want); err != nil {
		t.Fatalf("SaveJSONAtomic() error = %v", err)
	}

	var got persistFixture
	if err := LoadJSON(path, &got); err != nil {
		t.Fatalf("LoadJSON() error = %v", err)
	}
	if got != want {
		t.Errorf("LoadJSON() = %+v, want %+v", got, want)
	}
}

func TestSaveJSONAtomic_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "fixture.json")

	if err := SaveJSONAtomic(path, persistFixture{Name: "nested-dirs"}); err != nil {
		t.Fatalf("SaveJSONAtomic() error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file to exist at %s: %v", path, err)
	}
}

func TestSaveJSONAtomic_NoLeftoverTmpFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")

	if err := SaveJSONAtomic(path, persistFixture{Name: "clean"}); err != nil {
		t.Fatalf("SaveJSONAtomic() error = %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no leftover .tmp file, stat err = %v", err)
	}
}

func TestLoadJSON_MissingFileIsColdStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.json")

	got := persistFixture{Name: "untouched", Count: 7}
	if err := LoadJSON(path, &got); err != nil {
		t.Fatalf("LoadJSON() on missing file error = %v, want nil", err)
	}
	want := persistFixture{Name: "untouched", Count: 7}
	if got != want {
		t.Errorf("LoadJSON() on missing file mutated destination = %+v, want unchanged %+v", got, want)
	}
}

func TestLoadJSON_CorruptFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("failed to seed corrupt file: %v", err)
	}

	var got persistFixture
	if err := LoadJSON(path, &got); err == nil {
		t.Errorf("LoadJSON() on corrupt file error = nil, want non-nil")
	}
}

func TestSaveJSONAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")

	if err := SaveJSONAtomic(path, persistFixture{Name: "first", Count: 1}); err != nil {
		t.Fatalf("SaveJSONAtomic() first write error = %v", err)
	}
	if err := SaveJSONAtomic(path, persistFixture{Name: "second", Count: 2}); err != nil {
		t.Fatalf("SaveJSONAtomic() second write error = %v", err)
	}

	var got persistFixture
	if err := LoadJSON(path, &got); err != nil {
		t.Fatalf("LoadJSON() error = %v", err)
	}
	want := persistFixture{Name: "second", Count: 2}
	if got != want {
		t.Errorf("LoadJSON() after overwrite = %+v, want %+v", got, want)
	}
}
