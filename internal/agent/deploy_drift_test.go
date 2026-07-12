package agent

import (
	"errors"
	"testing"
)

// DriftStatus compares the running build revision against the repo HEAD.
// The git call is stubbed (driftHeadFn) so the logic is tested without a repo.
func TestDriftStatus(t *testing.T) {
	prev := driftHeadFn
	t.Cleanup(func() { driftHeadFn = prev })

	cases := []struct {
		name      string
		head      string
		headErr   error
		running   string
		wantHead  string
		wantStale bool
		wantErr   bool
	}{
		{"in sync", "abc123", nil, "abc123", "abc123", false, false},
		{"stale — HEAD moved past running binary", "def456", nil, "abc123", "def456", true, false},
		{"unstamped build cannot be compared", "def456", nil, "unknown", "def456", false, false},
		{"empty running revision cannot be compared", "def456", nil, "", "def456", false, false},
		{"git failure surfaces", "", errors.New("not a repo"), "abc123", "", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			driftHeadFn = func(string) (string, error) { return c.head, c.headErr }
			head, stale, err := DriftStatus("/repo", c.running)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if head != c.wantHead || stale != c.wantStale {
				t.Fatalf("DriftStatus = (%q, %v), want (%q, %v)", head, stale, c.wantHead, c.wantStale)
			}
		})
	}
}

// DriftWatchOnce logs a WARN on drift and only rebuilds when AutoRebuild is set —
// the production default (flag off) must be detection-only, never a self-rebuild.
func TestDriftWatchOnce(t *testing.T) {
	prevHead, prevRebuild := driftHeadFn, driftRebuildFn
	t.Cleanup(func() { driftHeadFn, driftRebuildFn = prevHead, prevRebuild })

	targets := []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}}

	t.Run("in sync: no rebuild regardless of flag", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "abc", nil }
		called := false
		driftRebuildFn = func(string, []RebuildTarget) error { called = true; return nil }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets})
		if err != nil || res.Stale || res.Rebuilt || called {
			t.Fatalf("in-sync: res=%+v err=%v called=%v, want no drift/rebuild", res, err, called)
		}
	})

	t.Run("stale + AutoRebuild off: WARN only, no rebuild", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def", nil }
		called := false
		driftRebuildFn = func(string, []RebuildTarget) error { called = true; return nil }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: false, Targets: targets})
		if err != nil || !res.Stale || res.Rebuilt || called {
			t.Fatalf("stale/off: res=%+v err=%v called=%v, want stale && no rebuild", res, err, called)
		}
	})

	t.Run("stale + AutoRebuild on: rebuilds", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def", nil }
		var gotTargets []RebuildTarget
		driftRebuildFn = func(_ string, ts []RebuildTarget) error { gotTargets = ts; return nil }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets})
		if err != nil || !res.Stale || !res.Rebuilt || len(gotTargets) != 1 {
			t.Fatalf("stale/on: res=%+v err=%v targets=%v, want stale && rebuilt", res, err, gotTargets)
		}
	})

	t.Run("stale + AutoRebuild on but rebuild fails: reports error, Rebuilt false", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def", nil }
		driftRebuildFn = func(string, []RebuildTarget) error { return errors.New("build blew up") }
		res, err := DriftWatchOnce(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", AutoRebuild: true, Targets: targets})
		if err == nil || res.Rebuilt {
			t.Fatalf("rebuild failure: res=%+v err=%v, want error && Rebuilt=false", res, err)
		}
	})
}
