package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// TestDashboardRebuildTargets_OutPathUnderBin pins the deploy-drift restart-
// handoff fix (Q3 Reliability milestone 2/3): the production bt-dashboard
// systemd unit's drop-in ExecStart (2026-07-15 override) runs bin/bt-dashboard,
// but DashboardRebuildTargets still wrote the rebuilt binary to the repo
// root — a "successful" rebuild that never lands where the running unit
// actually executes from.
// TestDashboardRebuildTargets_OwnBinaryOnly pins single-writer rebuild
// ownership: bt-dashboard's watcher rebuilds only its own binary. The
// fleet-wide sweep (all targets, sibling restarts) belongs to cmd/bt-agent's
// watcher alone. Pre-fix the dashboard list was a full alias of
// DefaultRebuildTargets, so both daemons raced `go build` onto the identical
// output paths and cross-restarted each other's units.
func TestDashboardRebuildTargets_OwnBinaryOnly(t *testing.T) {
	targets := DashboardRebuildTargets("/repo")
	if len(targets) != 1 {
		t.Fatalf("DashboardRebuildTargets has %d targets %v, want exactly the bt-dashboard target", len(targets), targets)
	}
	tg := targets[0]
	if tg.Name != "bt-dashboard" || tg.Unit != "bt-dashboard" {
		t.Fatalf("target = %+v, want the bt-dashboard unit target", tg)
	}
}

// TestRebuildBinaries_UniqueTempPerWriter pins torn-binary protection: the
// intermediate build output must be unique per process (two daemons' watchers
// can rebuild the same OutPath concurrently; a shared fixed ".new" let their
// writes interleave before either rename), and a failed build must not leave
// the intermediate file behind.
func TestRebuildBinaries_UniqueTempPerWriter(t *testing.T) {
	prevMat, prevBuild := rebuildMaterializeFn, rebuildBuildFn
	t.Cleanup(func() { rebuildMaterializeFn, rebuildBuildFn = prevMat, prevBuild })

	out := filepath.Join(t.TempDir(), "bt-agent")
	if err := os.WriteFile(out, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := RebuildTarget{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: out}

	var builtPath string
	rebuildMaterializeFn = func(string) (string, func(), error) { return t.TempDir(), func() {}, nil }
	rebuildBuildFn = func(_, _, newPath string) error {
		builtPath = newPath
		return os.WriteFile(newPath, []byte("NEW"), 0o755)
	}
	if err := RebuildBinaries("/repo", []RebuildTarget{target}); err != nil {
		t.Fatalf("RebuildBinaries: %v", err)
	}
	if builtPath == out+".new" {
		t.Fatalf("build wrote to the shared fixed %q; the intermediate path must be unique per writer (e.g. carry the PID)", builtPath)
	}
	if !strings.Contains(builtPath, strconv.Itoa(os.Getpid())) {
		t.Fatalf("intermediate path %q does not carry the writer's PID", builtPath)
	}
	if got, _ := os.ReadFile(out); string(got) != "NEW" {
		t.Fatalf("live binary not swapped: %q", got)
	}

	// Failed build: the unique intermediate must be cleaned up.
	rebuildBuildFn = func(_, _, newPath string) error {
		_ = os.WriteFile(newPath, []byte("HALF"), 0o755)
		return os.ErrInvalid
	}
	if err := RebuildBinaries("/repo", []RebuildTarget{target}); err == nil {
		t.Fatal("expected the build failure to surface")
	}
	entries, _ := os.ReadDir(filepath.Dir(out))
	for _, e := range entries {
		if e.Name() != "bt-agent" && e.Name() != "bt-agent.previous" {
			t.Fatalf("leftover intermediate %q after failed build", e.Name())
		}
	}
}

func TestDashboardRebuildTargets_OutPathUnderBin(t *testing.T) {
	targets := DashboardRebuildTargets("/repo")
	var dash *RebuildTarget
	for i := range targets {
		if targets[i].Name == "bt-dashboard" {
			dash = &targets[i]
			break
		}
	}
	if dash == nil {
		t.Fatal("DashboardRebuildTargets did not include a bt-dashboard target")
	}
	want := filepath.Join("/repo", "bin", "bt-dashboard")
	if dash.OutPath != want {
		t.Errorf("bt-dashboard OutPath = %q, want %q (production unit's ExecStart runs bin/bt-dashboard)", dash.OutPath, want)
	}
}

// TestDefaultRebuildTargets_IncludesBtDashboard pins bringing bt-dashboard
// into the daemon's fleet-wide rebuild adoption — the mechanism that already
// reliably rebuilds+restarts bt-agent/bt-agent-cli/bt-gardener via
// cmd/bt-agent's AutoRestart-armed AdoptDriftOnIdle/StartDriftWatcher.
// DefaultRebuildTargets previously excluded bt-dashboard by design, so that
// daemon-driven sweep rebuilt nothing for bt-dashboard during the
// 2026-07-16 23:46 adoption (the same event that exposed the sibling
// bt-gardener restart gap fixed for milestone 1) — only bt-dashboard's own
// separate, mis-pathed watcher covered it, and that one never restarted
// anything either.
func TestDefaultRebuildTargets_IncludesBtDashboard(t *testing.T) {
	targets := DefaultRebuildTargets("/repo")
	var dash *RebuildTarget
	for i := range targets {
		if targets[i].Name == "bt-dashboard" {
			dash = &targets[i]
			break
		}
	}
	if dash == nil {
		t.Fatal("DefaultRebuildTargets does not include bt-dashboard; the daemon's fleet-wide rebuild sweep never covers it")
	}
	if dash.Unit != "bt-dashboard" {
		t.Errorf("bt-dashboard target Unit = %q, want %q so DriftWatchOnce's sibling-unit restart (milestone 1) actually restarts it after a swap", dash.Unit, "bt-dashboard")
	}
	want := filepath.Join("/repo", "bin", "bt-dashboard")
	if dash.OutPath != want {
		t.Errorf("bt-dashboard target OutPath = %q, want %q", dash.OutPath, want)
	}
}

// TestDashboardRebuildTargets_NoDuplicateTargets guards against a
// double-rebuild once bt-dashboard is covered by both DefaultRebuildTargets
// (the daemon's fleet-wide list) and DashboardRebuildTargets (bt-dashboard's
// own self-watcher, built on top of it): the same binary must not appear
// twice in one Targets slice, or RebuildBinaries builds and swaps it twice
// per drift check.
func TestDashboardRebuildTargets_NoDuplicateTargets(t *testing.T) {
	targets := DashboardRebuildTargets("/repo")
	seen := make(map[string]int, len(targets))
	for _, tg := range targets {
		seen[tg.Name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("target %q appears %d times in DashboardRebuildTargets; each target must appear exactly once to avoid a double rebuild", name, count)
		}
	}
}
