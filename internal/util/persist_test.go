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

// effectiveUmask derives the process umask by probing what a fully
// permissive Mkdir actually produces, so permission assertions stay exact
// regardless of the environment the tests run in.
func effectiveUmask(t *testing.T) os.FileMode {
	t.Helper()
	probe := filepath.Join(t.TempDir(), "umask-probe")
	if err := os.Mkdir(probe, 0o777); err != nil {
		t.Fatalf("umask probe mkdir error = %v", err)
	}
	info, err := os.Stat(probe)
	if err != nil {
		t.Fatalf("umask probe stat error = %v", err)
	}
	return 0o777 &^ info.Mode().Perm()
}

func TestSaveJSONAtomicMode_HonorsFileAndDirPerms(t *testing.T) {
	umask := effectiveUmask(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	path := filepath.Join(sub, "snap.json")
	want := persistFixture{Name: "snapshot", Count: 11}

	if err := SaveJSONAtomicMode(path, want, 0o600, 0o700); err != nil {
		t.Fatalf("SaveJSONAtomicMode() error = %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s error = %v", path, err)
	}
	if got, wantPerm := fileInfo.Mode().Perm(), os.FileMode(0o600)&^umask; got != wantPerm {
		t.Errorf("file perm = %04o, want %04o", got, wantPerm)
	}

	dirInfo, err := os.Stat(sub)
	if err != nil {
		t.Fatalf("stat %s error = %v", sub, err)
	}
	if got, wantPerm := dirInfo.Mode().Perm(), os.FileMode(0o700)&^umask; got != wantPerm {
		t.Errorf("dir perm = %04o, want %04o", got, wantPerm)
	}

	var got persistFixture
	if err := LoadJSON(path, &got); err != nil {
		t.Fatalf("LoadJSON() error = %v", err)
	}
	if got != want {
		t.Errorf("LoadJSON() = %+v, want %+v", got, want)
	}
}

func TestSaveJSONAtomic_DefaultsMatchLegacyMode(t *testing.T) {
	umask := effectiveUmask(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	path := filepath.Join(sub, "fixture.json")

	if err := SaveJSONAtomic(path, persistFixture{Name: "legacy"}); err != nil {
		t.Fatalf("SaveJSONAtomic() error = %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s error = %v", path, err)
	}
	if got, wantPerm := fileInfo.Mode().Perm(), os.FileMode(0o644)&^umask; got != wantPerm {
		t.Errorf("file perm = %04o, want %04o", got, wantPerm)
	}

	dirInfo, err := os.Stat(sub)
	if err != nil {
		t.Fatalf("stat %s error = %v", sub, err)
	}
	if got, wantPerm := dirInfo.Mode().Perm(), os.FileMode(0o755)&^umask; got != wantPerm {
		t.Errorf("dir perm = %04o, want %04o", got, wantPerm)
	}
}

func TestSaveJSONAtomic_RemovesTmpOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")
	// A directory at the destination makes os.Rename fail after the tmp
	// sibling is already on disk.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("failed to seed blocking directory: %v", err)
	}

	if err := SaveJSONAtomic(path, persistFixture{Name: "doomed"}); err == nil {
		t.Fatalf("SaveJSONAtomic() error = nil, want rename failure")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected tmp sibling to be cleaned up after rename failure, stat err = %v", err)
	}
}

func TestSaveJSONAtomic_RejectsUnmarshalableValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.json")

	if err := SaveJSONAtomic(path, make(chan int)); err == nil {
		t.Fatalf("SaveJSONAtomic() error = nil, want marshal failure")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected no tmp sibling after marshal failure, stat err = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no destination file after marshal failure, stat err = %v", err)
	}
}
