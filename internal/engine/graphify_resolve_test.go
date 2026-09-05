package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// writeExecutableGraphify drops an executable file named like the graphify tool
// into dir and returns its absolute path.
func writeExecutableGraphify(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, goapFusionGraphifyTool)
	if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// TestResolveGraphifyBinPrefersPATH: a PATH hit wins and is returned verbatim.
func TestResolveGraphifyBinPrefersPATH(t *testing.T) {
	dir := t.TempDir()
	want := writeExecutableGraphify(t, dir)
	t.Setenv("PATH", dir)
	restore := graphifyFallbackDir
	graphifyFallbackDir = t.TempDir() // empty; must not be consulted on a PATH hit
	defer func() { graphifyFallbackDir = restore }()

	got, err := resolveGraphifyBin()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("resolveGraphifyBin() = %q, want PATH hit %q", got, want)
	}
}

// TestResolveGraphifyBinFallsBackWhenAbsentFromPATH reproduces the 2026-07-13
// cold-boot regression: the systemd-user default PATH drops ~/.local/bin, so a
// bare PATH lookup misses graphify even though it is installed. The fallback dir
// must resolve it — this is the whole point of the guard being PATH-robust like
// its sibling (claude/go/nlm) guards.
func TestResolveGraphifyBinFallsBackWhenAbsentFromPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // PATH deliberately lacks graphify
	fb := t.TempDir()
	want := writeExecutableGraphify(t, fb)
	restore := graphifyFallbackDir
	graphifyFallbackDir = fb
	defer func() { graphifyFallbackDir = restore }()

	got, err := resolveGraphifyBin()
	if err != nil {
		t.Fatalf("expected fallback resolution, got error: %v", err)
	}
	if got != want {
		t.Fatalf("resolveGraphifyBin() = %q, want fallback %q", got, want)
	}
}

// TestResolveGraphifyBinErrorsWhenGenuinelyAbsent: fail-fast contract preserved
// — a genuinely missing tool still returns an error (no silent success).
func TestResolveGraphifyBinErrorsWhenGenuinelyAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	restore := graphifyFallbackDir
	graphifyFallbackDir = t.TempDir() // empty
	defer func() { graphifyFallbackDir = restore }()

	if got, err := resolveGraphifyBin(); err == nil {
		t.Fatalf("expected error for absent graphify, got %q", got)
	}
}

// TestResolveGraphifyBinRejectsNonExecutableFallback: a non-executable file in
// the fallback dir must not be accepted as the tool.
func TestResolveGraphifyBinRejectsNonExecutableFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	fb := t.TempDir()
	if err := os.WriteFile(filepath.Join(fb, goapFusionGraphifyTool), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := graphifyFallbackDir
	graphifyFallbackDir = fb
	defer func() { graphifyFallbackDir = restore }()

	if got, err := resolveGraphifyBin(); err == nil {
		t.Fatalf("expected error for non-executable fallback, got %q", got)
	}
}
