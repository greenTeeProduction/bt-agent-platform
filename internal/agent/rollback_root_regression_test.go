package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackConfinesBackupAndAtomicallyReplacesExecutable(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "bin")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fixture-binary")
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte("outside evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("live image"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, bin+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := restorePreviousBinary(bin); err == nil {
		t.Fatal("outward backup restored")
	}
	if data, err := os.ReadFile(bin); err != nil || string(data) != "live image" {
		t.Fatal("failed backup read changed live binary")
	}
	if err := os.Remove(bin + ".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin+".previous", []byte("known previous image"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, bin); err != nil {
		t.Fatal(err)
	}
	if err := restorePreviousBinary(bin); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "outside evidence" {
		t.Fatal("rollback followed live outward symlink")
	}
	if data, err := os.ReadFile(bin); err != nil || string(data) != "known previous image" {
		t.Fatal("previous image not restored")
	}
	info, err := os.Lstat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0100 == 0 || info.Mode().Perm()&0007 != 0 {
		t.Fatalf("unsafe restored executable: %v", info.Mode())
	}
}

func TestRollbackFailedReplacementPreservesLiveImage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission fault requires unprivileged process")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fixture-binary")
	for path, data := range map[string]string{bin: "live image", bin + ".previous": "previous image"} {
		if err := os.WriteFile(path, []byte(data), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := restorePreviousBinary(bin); err == nil {
		t.Fatal("failed replacement acknowledged")
	}
	if data, err := os.ReadFile(bin); err != nil || string(data) != "live image" {
		t.Fatal("failed rollback truncated live image")
	}
}
