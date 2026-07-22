package agent

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The restart handoff: after a successful AutoRebuild, DriftWatchOnce adopts the
// new binary by restarting — but only when AutoRestart is set, only after the
// rebuilt binary passes its smoke test, and it rolls back on a failed smoke test.
func TestDriftWatchOnceRestartHandoff(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	prevRestart, prevSmoke, prevRestore := driftRestartFn, driftSmokeTestFn, restorePreviousBinaryFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn = prevHead, prevRebuild
		driftRestartFn, driftSmokeTestFn, restorePreviousBinaryFn = prevRestart, prevSmoke, prevRestore
	})

	targets := []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}}
	// Always stale so the rebuild path runs.
	driftHeadFn = func(string) (string, error) { return "newhead", nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }

	baseCfg := func() DriftWatchConfig {
		return DriftWatchConfig{RepoDir: "/r", RunningRevision: "oldrev", AutoRebuild: true, Targets: targets, Binary: "bt-agent"}
	}

	t.Run("AutoRestart off: rebuild but no restart", func(t *testing.T) {
		restarted := false
		driftRestartFn = func(string) error { restarted = true; return nil }
		driftSmokeTestFn = func(string) error { return nil }
		res, err := DriftWatchOnce(baseCfg()) // AutoRestart defaults false
		if err != nil || !res.Rebuilt || res.Restarted || restarted {
			t.Fatalf("off: rebuilt=%v restarted=%v called=%v err=%v; want rebuilt, no restart", res.Rebuilt, res.Restarted, restarted, err)
		}
	})

	t.Run("AutoRestart on, smoke passes: restarts", func(t *testing.T) {
		var smokedPath, restartedBin string
		driftSmokeTestFn = func(p string) error { smokedPath = p; return nil }
		driftRestartFn = func(b string) error { restartedBin = b; return nil }
		cfg := baseCfg()
		cfg.AutoRestart = true
		res, err := DriftWatchOnce(cfg)
		if err != nil || !res.Restarted {
			t.Fatalf("on+smoke-ok: restarted=%v err=%v; want restarted", res.Restarted, err)
		}
		if smokedPath != "/bin/bt-agent" {
			t.Fatalf("smoke-tested %q, want /bin/bt-agent (the self binary)", smokedPath)
		}
		if restartedBin != "bt-agent" {
			t.Fatalf("restarted %q, want bt-agent", restartedBin)
		}
	})

	t.Run("AutoRestart on, smoke fails: rollback and no restart", func(t *testing.T) {
		rolledBack := ""
		restarted := false
		driftSmokeTestFn = func(string) error { return errors.New("binary crashes on --version") }
		restorePreviousBinaryFn = func(p string) error { rolledBack = p; return nil }
		driftRestartFn = func(string) error { restarted = true; return nil }
		cfg := baseCfg()
		cfg.AutoRestart = true
		res, err := DriftWatchOnce(cfg)
		if err == nil {
			t.Fatal("smoke failure must return an error")
		}
		if res.Restarted || restarted {
			t.Fatal("must NOT restart when the smoke test fails")
		}
		if rolledBack != "/bin/bt-agent" {
			t.Fatalf("expected rollback of /bin/bt-agent, got %q", rolledBack)
		}
	})
}

// A rebuild can swap multiple sibling binaries (e.g. bt-agent's
// DefaultRebuildTargets also rebuilds bin/bt-gardener), but only the daemon's
// own unit was ever restarted — the rebuilt sibling kept running its old
// binary until someone restarted it by hand (live case 2026-07-16 23:46:
// bin/bt-gardener rebuilt to fd0746d while the running gardener process
// stayed on ce20198). Every swapped target that owns a systemd unit must be
// restarted through the same driftRestartFn seam, with the daemon's own unit
// restarted last to preserve existing behavior; a unit-less target (e.g.
// bt-agent-cli) must not trigger any restart call.
func TestDriftWatchOnceRestartsSwappedSiblingUnits(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	prevRestart, prevSmoke, prevRestore := driftRestartFn, driftSmokeTestFn, restorePreviousBinaryFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn = prevHead, prevRebuild
		driftRestartFn, driftSmokeTestFn, restorePreviousBinaryFn = prevRestart, prevSmoke, prevRestore
	})

	targets := []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/repo/bt-agent", Unit: "bt-agent"},
		{Name: "bt-agent-cli", Pkg: "./cmd/bt-agent-cli", OutPath: "/repo/bt-agent-cli", Unit: ""},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: "/repo/bin/bt-gardener", Unit: "bt-gardener"},
	}
	driftHeadFn = func(string) (string, error) { return "newhead", nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }
	driftSmokeTestFn = func(string) error { return nil }

	var restarted []string
	driftRestartFn = func(unit string) error { restarted = append(restarted, unit); return nil }

	cfg := DriftWatchConfig{
		RepoDir: "/r", RunningRevision: "oldrev", AutoRebuild: true,
		AutoRestart: true, RestartSiblings: true, Targets: targets, Binary: "bt-agent",
	}
	res, err := DriftWatchOnce(cfg)
	if err != nil || !res.Restarted {
		t.Fatalf("restarted=%v err=%v; want restarted", res.Restarted, err)
	}

	want := []string{"bt-gardener", "bt-agent"}
	if len(restarted) != len(want) {
		t.Fatalf("restarted units = %v, want %v (bt-agent-cli has no unit and must be skipped)", restarted, want)
	}
	for i, u := range want {
		if restarted[i] != u {
			t.Fatalf("restarted[%d] = %q, want %q (order: siblings first, self last): got %v", i, restarted[i], u, restarted)
		}
	}
}

// TestDriftWatchOnce_SiblingRestartRequiresOptIn pins fleet-restart ownership:
// only the watcher explicitly opted in via RestartSiblings (cmd/bt-agent, the
// fleet owner) may restart sibling units. Without the opt-in — bt-dashboard's
// watcher — only the process's own unit is restarted. Pre-fix, both daemons'
// watchers carried the full unit-owning target list and restarted each other
// symmetrically, with no cross-daemon in-flight coordination: the dashboard
// could kill a mid-execution bt-agent cycle regardless of bt-agent's own
// AnyInFlight guard.
func TestDriftWatchOnce_SiblingRestartRequiresOptIn(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	prevRestart, prevSmoke := driftRestartFn, driftSmokeTestFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn = prevHead, prevRebuild
		driftRestartFn, driftSmokeTestFn = prevRestart, prevSmoke
	})

	targets := []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/repo/bt-agent", Unit: "bt-agent"},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: "/repo/bin/bt-gardener", Unit: "bt-gardener"},
		{Name: "bt-dashboard", Pkg: "./cmd/bt-dashboard", OutPath: "/repo/bin/bt-dashboard", Unit: "bt-dashboard"},
	}
	driftHeadFn = func(string) (string, error) { return "newhead", nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }
	driftSmokeTestFn = func(string) error { return nil }

	var restarted []string
	driftRestartFn = func(unit string) error { restarted = append(restarted, unit); return nil }

	cfg := DriftWatchConfig{
		RepoDir: "/r", RunningRevision: "oldrev", AutoRebuild: true,
		AutoRestart: true, Targets: targets, Binary: "bt-dashboard",
		// RestartSiblings deliberately false.
	}
	res, err := DriftWatchOnce(cfg)
	if err != nil || !res.Restarted {
		t.Fatalf("restarted=%v err=%v; want self restarted", res.Restarted, err)
	}
	if len(restarted) != 1 || restarted[0] != "bt-dashboard" {
		t.Fatalf("restarted units = %v, want [bt-dashboard] only — without RestartSiblings a watcher must never bounce sibling daemons", restarted)
	}
}

// TestDriftWatchOnce_SiblingSmokeFailureRollsBackAndSkipsRestart mirrors the
// self-binary protection onto siblings: a swapped sibling whose binary fails
// its smoke test is rolled back to <bin>.previous and NOT restarted (a
// successful restart onto a compile-clean but crash-on-startup build would
// crash-loop until manual intervention), while healthy siblings and the self
// binary still restart normally.
func TestDriftWatchOnce_SiblingSmokeFailureRollsBackAndSkipsRestart(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	prevRestart, prevSmoke, prevRestore := driftRestartFn, driftSmokeTestFn, restorePreviousBinaryFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn = prevHead, prevRebuild
		driftRestartFn, driftSmokeTestFn, restorePreviousBinaryFn = prevRestart, prevSmoke, prevRestore
	})

	targets := []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/repo/bt-agent", Unit: "bt-agent"},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: "/repo/bin/bt-gardener", Unit: "bt-gardener"},
		{Name: "bt-dashboard", Pkg: "./cmd/bt-dashboard", OutPath: "/repo/bin/bt-dashboard", Unit: "bt-dashboard"},
	}
	driftHeadFn = func(string) (string, error) { return "newhead", nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }

	// bt-gardener's rebuilt binary crashes on --version; everything else is fine.
	driftSmokeTestFn = func(p string) error {
		if p == "/repo/bin/bt-gardener" {
			return errors.New("binary crashes on --version")
		}
		return nil
	}
	var rolledBack []string
	restorePreviousBinaryFn = func(p string) error { rolledBack = append(rolledBack, p); return nil }
	var restarted []string
	driftRestartFn = func(unit string) error { restarted = append(restarted, unit); return nil }

	cfg := DriftWatchConfig{
		RepoDir: "/r", RunningRevision: "oldrev", AutoRebuild: true,
		AutoRestart: true, RestartSiblings: true, Targets: targets, Binary: "bt-agent",
	}
	res, err := DriftWatchOnce(cfg)
	if err != nil || !res.Restarted {
		t.Fatalf("restarted=%v err=%v; a sibling smoke failure is best-effort and must not block the daemon's own adoption", res.Restarted, err)
	}
	if len(rolledBack) != 1 || rolledBack[0] != "/repo/bin/bt-gardener" {
		t.Fatalf("rolled back = %v, want [/repo/bin/bt-gardener]", rolledBack)
	}
	want := []string{"bt-dashboard", "bt-agent"}
	if len(restarted) != len(want) || restarted[0] != want[0] || restarted[1] != want[1] {
		t.Fatalf("restarted units = %v, want %v (the failed sibling must be skipped, healthy sibling + self restart)", restarted, want)
	}
}

func TestAutoRestartEnabled(t *testing.T) {
	for _, v := range []string{"1", "true", "YES", "on"} {
		t.Setenv("BT_AUTO_RESTART_ON_DRIFT", v)
		if !AutoRestartEnabled() {
			t.Fatalf("value %q must enable auto-restart", v)
		}
	}
	for _, v := range []string{"", "0", "false", "no"} {
		t.Setenv("BT_AUTO_RESTART_ON_DRIFT", v)
		if AutoRestartEnabled() {
			t.Fatalf("value %q must NOT enable auto-restart", v)
		}
	}
}

// TestDefaultRebuildTargets_PinsFullList pins the complete
// DefaultRebuildTargets list. The self target's OutPath is bin/bt-agent
// (2026-07-22): the unit's ExecStart runs from bin/ — the repo-root copy the
// unit previously ran was exactly why self-drift adoption never reached the
// daemon (rebuilds swapped a binary nothing executed) — and bin/bt-agent
// doubles as the MCP server binary .mcp.json boots per cycle-session, so the
// former separate unit-less "bt-agent-mcp" target collapsed into the self
// target. NO target may point at the repo root except bt-agent-cli (the CLI
// tool's canonical path, no owning unit).
func TestDefaultRebuildTargets_PinsFullList(t *testing.T) {
	got := DefaultRebuildTargets("/repo")
	want := []RebuildTarget{
		{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: filepath.Join("/repo", "bin", "bt-agent"), Unit: "bt-agent"},
		{Name: "bt-agent-cli", Pkg: "./cmd/bt-agent-cli", OutPath: filepath.Join("/repo", "bt-agent-cli")},
		{Name: "bt-gardener", Pkg: "./cmd/bt-gardener", OutPath: filepath.Join("/repo", "bin", "bt-gardener"), Unit: "bt-gardener"},
		{Name: "bt-dashboard", Pkg: "./cmd/bt-dashboard", OutPath: filepath.Join("/repo", "bin", "bt-dashboard"), Unit: "bt-dashboard"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultRebuildTargets(\"/repo\") =\n%#v\nwant\n%#v", got, want)
	}
	for _, tg := range got {
		if tg.Unit != "" && !strings.Contains(tg.OutPath, filepath.Join("/repo", "bin")) {
			t.Fatalf("unit-owning target %q OutPath = %q, want under /repo/bin — a unit must exec the same path drift swaps or adoption is inert", tg.Name, tg.OutPath)
		}
	}
}

func TestSelfBinaryPath(t *testing.T) {
	cfg := DriftWatchConfig{
		Binary: "bt-agent",
		Targets: []RebuildTarget{
			{Name: "bt-gardener", OutPath: "/bin/bt-gardener"},
			{Name: "bt-agent", OutPath: "/root/bt-agent"},
		},
	}
	if got := cfg.selfBinaryPath(); got != "/root/bt-agent" {
		t.Fatalf("selfBinaryPath = %q, want /root/bt-agent", got)
	}
	cfg.Binary = "absent"
	if got := cfg.selfBinaryPath(); got != "" {
		t.Fatalf("selfBinaryPath for absent target = %q, want empty", got)
	}
}
