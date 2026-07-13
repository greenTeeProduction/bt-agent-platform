package agent

import (
	"errors"
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
