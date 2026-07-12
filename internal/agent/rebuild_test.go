package agent

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// RebuildBinaries materializes HEAD into a scratch tree, builds each target to
// <binary>.new there, and renames over the live binary ONLY when its build
// succeeds — a live binary is never partially written. The materialize and
// build steps are stubbed (seams) so the orchestration is tested without a real
// toolchain. errcheck-clean error handling here is the exact thing the
// autonomous cycles kept failing (2026-07-12 rebuild.go treadmill).
func TestRebuildBinaries(t *testing.T) {
	prevMat, prevBuild := rebuildMaterializeFn, rebuildBuildFn
	t.Cleanup(func() { rebuildMaterializeFn, rebuildBuildFn = prevMat, prevBuild })

	newTarget := func(t *testing.T, name string) RebuildTarget {
		out := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(out, []byte("OLD-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
		return RebuildTarget{Name: name, Pkg: "./cmd/" + name, OutPath: out}
	}

	t.Run("success: builds then swaps every target, cleans up", func(t *testing.T) {
		cleaned := false
		rebuildMaterializeFn = func(string) (string, func(), error) {
			return t.TempDir(), func() { cleaned = true }, nil
		}
		rebuildBuildFn = func(_, _, newPath string) error {
			return os.WriteFile(newPath, []byte("NEW"), 0o755)
		}
		a, b := newTarget(t, "bt-agent"), newTarget(t, "bt-gardener")

		if err := RebuildBinaries("/repo", []RebuildTarget{a, b}); err != nil {
			t.Fatalf("RebuildBinaries: %v", err)
		}
		for _, tg := range []RebuildTarget{a, b} {
			got, _ := os.ReadFile(tg.OutPath)
			if string(got) != "NEW" {
				t.Fatalf("%s not swapped: content=%q", tg.Name, got)
			}
			if _, err := os.Stat(tg.OutPath + ".new"); !os.IsNotExist(err) {
				t.Fatalf("%s.new should be gone after rename", tg.Name)
			}
		}
		if !cleaned {
			t.Fatal("scratch worktree cleanup was not called")
		}
	})

	t.Run("build failure: that binary is NOT swapped, error returned", func(t *testing.T) {
		rebuildMaterializeFn = func(string) (string, func(), error) { return t.TempDir(), func() {}, nil }
		rebuildBuildFn = func(_, pkg, newPath string) error {
			if pkg == "./cmd/bt-gardener" {
				return errors.New("compile error")
			}
			return os.WriteFile(newPath, []byte("NEW"), 0o755)
		}
		good, bad := newTarget(t, "bt-agent"), newTarget(t, "bt-gardener")

		err := RebuildBinaries("/repo", []RebuildTarget{good, bad})
		if err == nil {
			t.Fatal("expected error from failing build")
		}
		if got, _ := os.ReadFile(bad.OutPath); string(got) != "OLD-bt-gardener" {
			t.Fatalf("failed target's live binary was overwritten: %q", got)
		}
	})

	t.Run("materialize failure: no build attempted", func(t *testing.T) {
		rebuildMaterializeFn = func(string) (string, func(), error) {
			return "", func() {}, errors.New("git worktree add failed")
		}
		built := false
		rebuildBuildFn = func(_, _, _ string) error { built = true; return nil }
		if err := RebuildBinaries("/repo", []RebuildTarget{newTarget(t, "bt-agent")}); err == nil {
			t.Fatal("expected materialize error")
		}
		if built {
			t.Fatal("must not build when materialize fails")
		}
	})
}
