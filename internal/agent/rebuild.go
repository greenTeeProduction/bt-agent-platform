package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Out-of-place binary rebuild (program 94b0b31 milestone 2). To adopt a
// committed fix, the daemon materializes the repo HEAD into a throwaway worktree,
// builds each target to "<binary>.new" there, and only os.Renames over the live
// binary when its build SUCCEEDS — a live binary is never truncated by an
// in-place `go build` (the bug that overwrote the running binary on 2026-07-03).
// The materialize and build steps are package-var seams so the orchestration is
// unit-tested without a toolchain.

// RebuildTarget describes one binary to rebuild.
type RebuildTarget struct {
	Name    string // display name, e.g. "bt-agent"
	Pkg     string // build package, e.g. "./cmd/bt-agent"
	OutPath string // live binary path to swap on success
}

// DefaultRebuildTargets returns the daemon-owned binaries rebuilt on drift, all
// resolved under repoDir. bt-dashboard and the MCP bin/bt-agent are intentionally
// excluded here — callers pass the set they own.
func DefaultRebuildTargets(repoDir string) []RebuildTarget {
	return []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: filepath.Join(repoDir, "bt-agent")},
		{Name: "bt-agent-cli", Pkg: "./cmd/bt-agent-cli", OutPath: filepath.Join(repoDir, "bt-agent-cli")},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: filepath.Join(repoDir, "bin", "bt-gardener")},
	}
}

var (
	rebuildMaterializeFn = defaultRebuildMaterialize
	rebuildBuildFn       = defaultRebuildBuild
)

// RebuildBinaries materializes repoDir's HEAD into a scratch tree and rebuilds
// each target there, swapping the live binary only on a successful build. It
// stops at the first build/swap error (later targets are left untouched) and
// always cleans up the scratch tree.
func RebuildBinaries(repoDir string, targets []RebuildTarget) error {
	scratch, cleanup, err := rebuildMaterializeFn(repoDir)
	if err != nil {
		return fmt.Errorf("materialize HEAD for rebuild: %w", err)
	}
	defer cleanup()

	for _, t := range targets {
		newPath := t.OutPath + ".new"
		if err := rebuildBuildFn(scratch, t.Pkg, newPath); err != nil {
			return fmt.Errorf("build %s: %w", t.Name, err)
		}
		// Keep a .previous backup (best-effort) before swapping, matching the
		// repo's binary rollback convention.
		if err := copyFile(t.OutPath, t.OutPath+".previous"); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("back up %s: %w", t.Name, err)
		}
		if err := os.Rename(newPath, t.OutPath); err != nil {
			return fmt.Errorf("swap %s: %w", t.Name, err)
		}
	}
	return nil
}

// defaultRebuildMaterialize adds a detached worktree at HEAD (bare-repo safe)
// and returns a cleanup that removes it.
func defaultRebuildMaterialize(repoDir string) (string, func(), error) {
	scratch, err := os.MkdirTemp("", "bt-rebuild-*")
	if err != nil {
		return "", func() {}, err
	}
	add := exec.Command("git", "-C", repoDir, "worktree", "add", "--detach", scratch, "HEAD")
	add.Env = scrubGitEnv()
	if out, err := add.CombinedOutput(); err != nil {
		_ = os.RemoveAll(scratch)
		return "", func() {}, fmt.Errorf("git worktree add: %w\n%s", err, out)
	}
	cleanup := func() {
		rm := exec.Command("git", "-C", repoDir, "worktree", "remove", "--force", scratch)
		rm.Env = scrubGitEnv()
		_ = rm.Run()
		_ = os.RemoveAll(scratch)
	}
	return scratch, cleanup, nil
}

// defaultRebuildBuild runs `go build -o <newPath> <pkg>` inside workDir.
func defaultRebuildBuild(workDir, pkg, newPath string) error {
	build := exec.Command(resolveGoBinary(), "build", "-o", newPath, pkg)
	build.Dir = workDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", pkg, err, out)
	}
	return nil
}

// resolveGoBinary prefers the pinned toolchain (not on the daemon's PATH) and
// falls back to PATH lookup.
func resolveGoBinary() string {
	if _, err := os.Stat("/usr/local/go/bin/go"); err == nil {
		return "/usr/local/go/bin/go"
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return "go"
}

// copyFile copies src to dst (best-effort backup). A missing src returns an
// os.IsNotExist error the caller tolerates.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
