package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// DashboardRebuildTargets returns the rebuild targets for bt-dashboard's own
// deploy-drift watcher: the daemon-owned defaults plus bt-dashboard itself,
// which DefaultRebuildTargets deliberately excludes. Without this, an
// AutoRebuild-enabled bt-dashboard detects its own drift but the rebuild it
// triggers never swaps its own binary. OutPath is the repo root (matching
// bt-agent/bt-agent-cli above, not bin/) since that is where the production
// systemd unit's ExecStart actually runs bt-dashboard from.
func DashboardRebuildTargets(repoDir string) []RebuildTarget {
	return append(DefaultRebuildTargets(repoDir), RebuildTarget{
		Name:    "bt-dashboard",
		Pkg:     "./cmd/bt-dashboard",
		OutPath: filepath.Join(repoDir, "bt-dashboard"),
	})
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

// defaultRebuildBuild runs `go build -o <newPath> <pkg>` inside workDir, VCS-
// stamping the binary with workDir's HEAD so a binary rebuilt from the bare main
// repo (which go build cannot stamp on its own) stays comparable against repo
// HEAD by DriftStatus — otherwise the auto-rebuilt daemon would itself be blind
// to the next drift.
func defaultRebuildBuild(workDir, pkg, newPath string) error {
	args := []string{"build"}
	if ld := buildStampLdflags(workDir); ld != "" {
		args = append(args, "-ldflags", ld)
	}
	args = append(args, "-o", newPath, pkg)
	build := exec.Command(resolveGoBinary(), args...)
	build.Dir = workDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", pkg, err, out)
	}
	return nil
}

// buildStampLdflags returns the -X ldflags value that stamps a built binary with
// workDir's HEAD revision (see dashboard.stampedRevision). It scrubs the git
// plumbing env so a hook/worktree context cannot redirect rev-parse. Empty when
// HEAD cannot be resolved (developer builds keep buildinfo-or-unknown behavior).
func buildStampLdflags(workDir string) string {
	cmd := exec.Command("git", "-C", workDir, "rev-parse", "HEAD")
	cmd.Env = scrubGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	rev := strings.TrimSpace(string(out))
	if rev == "" {
		return ""
	}
	return "-X github.com/nico/go-bt-evolve/internal/dashboard.stampedRevision=" + rev
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

// RebuildBackoff caps consecutive rebuild attempts against the same stale
// repo HEAD, with exponential backoff between attempts, and permanently
// blocks further attempts once MaxAttempts is reached — the guardrail
// (program 94b0b31 milestone 5) that stops a broken commit from
// retry-storming a `go build` every watcher interval. A HEAD change resets
// the guard immediately, since a new commit deserves a fresh chance.
type RebuildBackoff struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration

	nowFn func() time.Time // test seam; defaults to time.Now

	mu       sync.Mutex
	head     string
	attempts int
	lastFail time.Time
}

// NewRebuildBackoff returns a RebuildBackoff with production defaults: up to
// 5 consecutive attempts against the same stale HEAD, starting at a 1-minute
// delay and capping at 30 minutes — throttling that stays inside a single
// DefaultDriftCheckInterval watch cadence.
func NewRebuildBackoff() *RebuildBackoff {
	return &RebuildBackoff{
		MaxAttempts: 5,
		BaseDelay:   time.Minute,
		MaxDelay:    30 * time.Minute,
	}
}

func (g *RebuildBackoff) now() time.Time {
	if g.nowFn != nil {
		return g.nowFn()
	}
	return time.Now()
}

// Allow reports whether a rebuild attempt against head may proceed. A head
// change resets the guard immediately; otherwise it enforces MaxAttempts and
// an exponential backoff delay since the last recorded failure.
func (g *RebuildBackoff) Allow(head string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if head != g.head {
		g.head = head
		g.attempts = 0
		return true
	}
	if g.attempts == 0 {
		return true
	}
	if g.attempts >= g.MaxAttempts {
		return false
	}
	return g.now().Sub(g.lastFail) >= g.backoffDelayLocked()
}

// backoffDelayLocked returns the exponential backoff delay for the current
// attempt count, capped at MaxDelay. Caller must hold g.mu.
func (g *RebuildBackoff) backoffDelayLocked() time.Duration {
	delay := g.BaseDelay << (g.attempts - 1)
	if g.MaxDelay > 0 && delay > g.MaxDelay {
		return g.MaxDelay
	}
	return delay
}

// RecordFailure records a failed rebuild attempt against head, advancing the
// backoff clock. A head change resets the attempt count first.
func (g *RebuildBackoff) RecordFailure(head string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if head != g.head {
		g.head = head
		g.attempts = 0
	}
	g.attempts++
	g.lastFail = g.now()
}

// RecordSuccess clears the failure count for head — a later, working rebuild
// means the next drift at this head starts fresh.
func (g *RebuildBackoff) RecordSuccess(head string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.head = head
	g.attempts = 0
}
