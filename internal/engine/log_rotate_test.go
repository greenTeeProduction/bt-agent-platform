package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestNewRotatingWriter_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	// File should exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("log file was not created")
	}
}

func TestNewRotatingWriter_AppendToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Pre-create with some content
	if err := os.WriteFile(path, []byte("existing\n"), 0644); err != nil {
		t.Fatal(err)
	}

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}

	n, err := rw.Write([]byte("appended\n"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 9 {
		t.Errorf("Write returned %d, want 9", n)
	}
	rw.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\nappended\n" {
		t.Errorf("got %q, want %q", string(data), "existing\nappended\n")
	}
}

func TestRotatingWriter_RotatesOnSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	rw.MaxSize = 100 // 100 bytes
	rw.MaxBackups = 3

	// Write 120 bytes — should trigger rotation
	chunk := make([]byte, 60)
	for i := range chunk {
		chunk[i] = 'A'
	}

	// First write: 60 bytes, no rotation
	n, err := rw.Write(chunk)
	if err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if n != 60 {
		t.Errorf("Write 1 returned %d, want 60", n)
	}

	// Second write: 60 more bytes, should trigger rotation (total 120 > 100)
	n, err = rw.Write(chunk)
	if err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if n != 60 {
		t.Errorf("Write 2 returned %d, want 60", n)
	}

	rw.Close()

	// Check that rotated files exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("current log file does not exist")
	}
	if _, err := os.Stat(path + ".1"); os.IsNotExist(err) {
		t.Error("rotated backup .1 does not exist")
	}

	// The second write went to the new file
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 60 {
		t.Errorf("new file has %d bytes, want 60", len(data))
	}

	// The first write is in the backup
	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if len(backup) != 60 {
		t.Errorf("backup .1 has %d bytes, want 60", len(backup))
	}
}

func TestRotatingWriter_MaxBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	rw.MaxSize = 20 // very small to trigger rapid rotation
	rw.MaxBackups = 2

	chunk := []byte("0123456789") // 10 bytes

	// Write enough to trigger 4 rotations
	for i := 0; i < 8; i++ {
		_, err := rw.Write(chunk)
		if err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	rw.Close()

	// Should have: test.log, test.log.1, test.log.2 (no .3 or higher)
	for _, suffix := range []string{"", ".1", ".2"} {
		if _, err := os.Stat(path + suffix); os.IsNotExist(err) {
			t.Errorf("expected file %s does not exist", path+suffix)
		}
	}
	for _, suffix := range []string{".3", ".4"} {
		if _, err := os.Stat(path + suffix); err == nil {
			t.Errorf("unexpected file %s exists", path+suffix)
		}
	}
}

func TestRotatingWriter_Size(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	defer rw.Close()

	if rw.Size() != 0 {
		t.Errorf("initial size %d, want 0", rw.Size())
	}

	rw.Write([]byte("hello"))
	if rw.Size() != 5 {
		t.Errorf("size after write %d, want 5", rw.Size())
	}
}

func TestRotatingWriter_Close(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}

	rw.Write([]byte("data"))
	if err := rw.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Second close should be safe
	if err := rw.Close(); err != nil {
		t.Errorf("Second Close: %v", err)
	}
}

func TestCleanupOldLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// Create test.log.1 through test.log.5
	for i := 1; i <= 5; i++ {
		p := fmt.Sprintf("%s.%d", path, i)
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Keep only 3 backups
	if err := CleanupOldLogs(path, 3); err != nil {
		t.Fatalf("CleanupOldLogs: %v", err)
	}

	// .1 .2 .3 should exist, .4 .5 should be gone
	for i := 1; i <= 3; i++ {
		p := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			t.Errorf("expected backup %s to exist", p)
		}
	}
	for i := 4; i <= 5; i++ {
		p := fmt.Sprintf("%s.%d", path, i)
		if _, err := os.Stat(p); err == nil {
			t.Errorf("backup %s should have been deleted", p)
		}
	}
}

func TestCleanupOldLogs_NoBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// No backups exist — should not error
	if err := CleanupOldLogs(path, 3); err != nil {
		t.Errorf("CleanupOldLogs with no backups: %v", err)
	}
}

func TestRotatingWriter_MaxSizeZero_NoRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	rw, err := NewRotatingWriter(path)
	if err != nil {
		t.Fatalf("NewRotatingWriter: %v", err)
	}
	rw.MaxSize = 0 // disabled
	rw.MaxBackups = 3
	defer rw.Close()

	// Write lots of data — should not rotate
	big := make([]byte, 5000)
	for i := range big {
		big[i] = 'X'
	}
	_, err = rw.Write(big)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// No backups should exist
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("backup .1 exists but rotation should be disabled (MaxSize=0)")
	}
}
