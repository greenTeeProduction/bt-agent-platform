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
	// Unit is the systemd --user unit owning this binary (without the
	// ".service" suffix), e.g. "bt-gardener". Empty for unit-less targets
	// (e.g. bt-agent-cli, a CLI tool with no long-running service to restart).
	Unit string
}

// DefaultRebuildTargets returns the daemon-owned binaries rebuilt on drift,
// all resolved under repoDir. bt-dashboard is included (Q3 Reliability
// milestone 2) so the daemon's fleet-wide sweep rebuilds it too, not just
// bt-dashboard's own watcher; OutPath matches bin/bt-gardener above (not the
// repo root) since that is where the production systemd unit's drop-in
// ExecStart override (2026-07-15) actually runs bt-dashboard from.
// bt-agent's own OutPath is bin/bt-agent too (2026-07-22): the unit's
// ExecStart was repointed there — the repo-root copy the unit previously ran
// was the reason self-drift adoption never reached the daemon — and
// bin/bt-agent doubles as the MCP server binary .mcp.json boots per
// cycle-session, so the former separate unit-less "bt-agent-mcp" target
// collapsed into it.
func DefaultRebuildTargets(repoDir string) []RebuildTarget {
	return []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: filepath.Join(repoDir, "bin", "bt-agent"), Unit: "bt-agent"},
		{Name: "bt-agent-cli", Pkg: "./cmd/bt-agent-cli", OutPath: filepath.Join(repoDir, "bt-agent-cli")},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: filepath.Join(repoDir, "bin", "bt-gardener"), Unit: "bt-gardener"},
		{Name: "bt-dashboard", Pkg: "./cmd/bt-dashboard", OutPath: filepath.Join(repoDir, "bin", "bt-dashboard"), Unit: "bt-dashboard"},
	}
}

// DashboardRebuildTargets returns the rebuild targets for bt-dashboard's own
// deploy-drift watcher: its OWN binary only. The fleet-wide sweep — all of
// DefaultRebuildTargets plus sibling-unit restarts (RestartSiblings) — is
// owned exclusively by cmd/bt-agent's watcher: when both watchers carried the
// full list they raced `go build` onto the identical output paths and
// restarted each other's units with no cross-daemon in-flight coordination.
// bt-agent's sweep still rebuilds and restarts bt-dashboard, so the dashboard
// keeps adopting fleet fixes; its own watcher just lets it self-heal faster
// when only the dashboard binary is stale.
func DashboardRebuildTargets(repoDir string) []RebuildTarget {
	return []RebuildTarget{
		{Name: "bt-dashboard", Pkg: "./cmd/bt-dashboard", OutPath: filepath.Join(repoDir, "bin", "bt-dashboard"), Unit: "bt-dashboard"},
	}
}

// GardenerRebuildTargets returns the rebuild targets for bt-gardener's own
// deploy-drift watcher: its OWN binary only, for the same single-writer
// reason as DashboardRebuildTargets above — bt-agent's fleet sweep already
// rebuilds and restarts bin/bt-gardener, and a third daemon racing builds
// onto the shared output paths could clobber a sibling's fixed
// <bin>.previous backup between bt-agent's swap and a smoke-test rollback.
func GardenerRebuildTargets(repoDir string) []RebuildTarget {
	return []RebuildTarget{
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: filepath.Join(repoDir, "bin", "bt-gardener"), Unit: "bt-gardener"},
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
		// Unique intermediate per writer: two watchers (bt-agent's fleet
		// sweep and bt-dashboard's own) can rebuild the same OutPath
		// concurrently; a shared fixed ".new" let their writes interleave
		// before either rename published a torn binary. With a PID suffix
		// each writer renames its own complete build (rename is atomic;
		// concurrent renames just mean last-writer-wins with intact files).
		newPath := fmt.Sprintf("%s.new.%d", t.OutPath, os.Getpid())
		if err := rebuildBuildFn(scratch, t.Pkg, newPath); err != nil {
			_ = os.Remove(newPath) // don't leave half-written intermediates behind
			return fmt.Errorf("build %s: %w", t.Name, err)
		}
		// Keep a .previous backup (best-effort) before swapping, matching the
		// repo's binary rollback convention.
		if err := copyFile(t.OutPath, t.OutPath+".previous"); err != nil && !os.IsNotExist(err) {
			_ = os.Remove(newPath)
			return fmt.Errorf("back up %s: %w", t.Name, err)
		}
		if err := os.Rename(newPath, t.OutPath); err != nil {
			_ = os.Remove(newPath)
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
// os.IsNotExist error the caller tolerates. The write goes through a
// pid-unique temp + rename so a concurrent writer targeting the same backup
// path (e.g. two watchers backing up the same <bin>.previous) can never
// publish a torn file — each rename installs one writer's complete copy.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", dst, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
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
	built    bool // last completed rebuild was against head (adoption may still be pending)
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
		g.built = false
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
	// A failure at this head (rebuild error or smoke-test rollback) means the
	// on-disk binaries can no longer be trusted as built-from-head.
	g.built = false
}

// RecordSuccess clears the failure count for head and marks it built — a
// later drift tick at the same head skips the (pointless) rebuild and goes
// straight to adoption.
func (g *RebuildBackoff) RecordSuccess(head string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.head = head
	g.attempts = 0
	g.built = true
}

// BuiltAt reports whether the last completed rebuild was against head: the
// binaries on disk already come from it, so another rebuild is pointless —
// only adoption (restart) may still be pending. The gardener previously
// rebuilt the same head on every 20-minute tick because it could not restart
// itself and nothing remembered the build (2026-07-23 review, gap 4). A head
// change or a recorded failure clears the mark.
func (g *RebuildBackoff) BuiltAt(head string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.built && head == g.head
}
