package agent

import (
	"errors"
	"testing"
)

// TestAdoptDriftOnIdle_AdoptsSynchronously: the async Kick lost a milliseconds
// race on saturated fleets — the tick loop started the next queued job before
// the watcher goroutine could check InFlightFn (observed live 2026-07-15
// 13:54/14:01: "rebuild attempt skipped — job in-flight" 4ms after the kick).
// AdoptDriftOnIdle runs the whole check-rebuild-smoke-restart chain
// SYNCHRONOUSLY in the caller (the scheduler's cycle-idle hook): the queue is
// blocked for the duration, so nothing can race the adoption.
func TestAdoptDriftOnIdle_AdoptsSynchronously(t *testing.T) {
	prevHead, prevRebuild, prevRestart, prevSmoke := driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn = prevHead, prevRebuild, prevRestart, prevSmoke
	})

	driftHeadFn = func(string) (string, error) { return "def", nil }
	rebuilt, restarted := false, false
	driftRebuildFn = func(string, []RebuildTarget) error { rebuilt = true; return nil }
	driftSmokeTestFn = func(string) error { return nil }
	driftRestartFn = func(string) error { restarted = true; return nil }

	AdoptDriftOnIdle(DriftWatchConfig{
		RepoDir:         "/r",
		RunningRevision: "abc",
		AutoRebuild:     true,
		AutoRestart:     true,
		Binary:          "bt-agent",
		Targets:         []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}},
		InFlightFn:      func() bool { return false }, // idle — and stays idle: the caller's queue is blocked
	})

	if !rebuilt || !restarted {
		t.Fatalf("synchronous idle adoption must rebuild and restart when drifted and idle; rebuilt=%v restarted=%v", rebuilt, restarted)
	}
}

// TestAdoptDriftOnIdle_SwallowsErrors: the hook runs inside the scheduler
// loop — a failed check/rebuild must be logged, never panic or abort the loop.
func TestAdoptDriftOnIdle_SwallowsErrors(t *testing.T) {
	prevHead := driftHeadFn
	t.Cleanup(func() { driftHeadFn = prevHead })
	driftHeadFn = func(string) (string, error) { return "", errors.New("git broke") }

	// Must not panic.
	AdoptDriftOnIdle(DriftWatchConfig{RepoDir: "/r", RunningRevision: "abc", Binary: "bt-agent"})
}
