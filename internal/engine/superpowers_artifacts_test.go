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

func TestSuperpowersPlanAttemptSaturatedInDir(t *testing.T) {
	dir := t.TempDir()
	task := "repeat me"
	suffix := superpowersTaskHashSuffix(task)
	for _, name := range []string{"20260625T050617-" + suffix, "20260625T053048-" + suffix} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saturated, matches := superpowersPlanAttemptSaturatedInDir(dir, task, 2)
	if !saturated {
		t.Fatalf("expected task hash %s to be saturated; matches=%v", suffix, matches)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2 (%v)", len(matches), matches)
	}

	saturated, _ = superpowersPlanAttemptSaturatedInDir(dir, "different task", 2)
	if saturated {
		t.Fatal("different task should not be saturated")
	}
}
