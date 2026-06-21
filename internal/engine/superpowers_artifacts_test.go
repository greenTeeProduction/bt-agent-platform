package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteArtifactOnce_Idempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plan.md")
	written, err := writeArtifactOnce(p, []byte("one"))
	if err != nil || !written {
		t.Fatalf("first write = %v, %v; want written nil", written, err)
	}
	written, err = writeArtifactOnce(p, []byte("two"))
	if err != nil || written {
		t.Fatalf("second write = %v, %v; want reused nil", written, err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one" {
		t.Fatalf("artifact overwritten: %q", got)
	}
}

func TestSafeSlug(t *testing.T) {
	if got := safeSlug("Hello, BT Fusion!!!"); got != "hello-bt-fusion" {
		t.Fatalf("safeSlug = %q", got)
	}
}
