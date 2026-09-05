package agent

import (
	"context"
	"testing"
	"time"
)

// TestStartDriftWatcher_KickTriggersImmediateCheck: the fixed-interval watcher
// starved all day on 2026-07-15 — every 20-min tick collided with an in-flight
// job, so an armed auto-redeploy never fired while master ran 4+ commits ahead.
// A Kick channel lets the scheduler poke the watcher in the idle window right
// after a cycle completes, without waiting for the next tick.
func TestStartDriftWatcher_KickTriggersImmediateCheck(t *testing.T) {
	prevHead := driftHeadFn
	t.Cleanup(func() { driftHeadFn = prevHead })

	checked := make(chan struct{}, 4)
	driftHeadFn = func(string) (string, error) {
		select {
		case checked <- struct{}{}:
		default:
		}
		return "abc", nil
	}

	kick := make(chan struct{}, 1)
	stop := StartDriftWatcher(context.Background(), DriftWatchConfig{
		RepoDir:         "/r",
		RunningRevision: "abc",
		Binary:          "bt-agent",
		Kick:            kick,
	}, time.Hour) // interval + start offset far beyond test lifetime: only a kick can trigger the check
	defer stop() // joins the watcher goroutine before t.Cleanup restores driftHeadFn

	kick <- struct{}{}

	select {
	case <-checked:
		// drift check ran in response to the kick
	case <-time.After(3 * time.Second):
		t.Fatal("kick did not trigger an immediate drift check within 3s")
	}
}

// TestDriftWatchOnce_SkipsRestartWhenJobStartedMidRebuild: a rebuild takes
// minutes; a scheduled job can start in that window. The restart handoff must
// re-check InFlightFn AFTER the rebuild and skip the restart (the rebuilt
// binary stays swapped in for a later idle adoption) instead of killing the
// just-started cycle.
func TestDriftWatchOnce_SkipsRestartWhenJobStartedMidRebuild(t *testing.T) {
	prevHead, prevRebuild, prevRestart, prevSmoke := driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn
	t.Cleanup(func() {
		driftHeadFn, driftRebuildFn, driftRestartFn, driftSmokeTestFn = prevHead, prevRebuild, prevRestart, prevSmoke
	})

	driftHeadFn = func(string) (string, error) { return "def", nil }
	driftRebuildFn = func(string, []RebuildTarget) error { return nil }
	driftSmokeTestFn = func(string) error { return nil }
	restartCalled := false
	driftRestartFn = func(string) error { restartCalled = true; return nil }

	inFlightCalls := 0
	res, err := DriftWatchOnce(DriftWatchConfig{
		RepoDir:         "/r",
		RunningRevision: "abc",
		AutoRebuild:     true,
		AutoRestart:     true,
		Binary:          "bt-agent",
		Targets:         []RebuildTarget{{Name: "bt-agent", Pkg: "./cmd/bt-agent", OutPath: "/bin/bt-agent"}},
		InFlightFn: func() bool {
			inFlightCalls++
			// Idle before the rebuild, busy after it (a job started mid-rebuild).
			return inFlightCalls > 1
		},
	})
	if err != nil {
		t.Fatalf("DriftWatchOnce: %v", err)
	}
	if !res.Rebuilt {
		t.Fatal("expected the rebuild to proceed (idle at rebuild time)")
	}
	if res.Restarted || restartCalled {
		t.Fatal("restart must be skipped when a job started mid-rebuild — restarting would kill the in-flight cycle")
	}
	if inFlightCalls < 2 {
		t.Fatalf("InFlightFn must be re-checked after the rebuild, before restarting; got %d call(s)", inFlightCalls)
	}
}

// TestRunJob_KicksOnCycleIdleWhenQueueEmpty: the scheduler side of the idle
// kick — after a cycle completes and nothing else is in flight, the configured
// OnCycleIdle hook fires (wired by main() to the drift watcher's Kick).
func TestRunJob_KicksOnCycleIdleWhenQueueEmpty(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	reg, err := NewRegistry(RegistryDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(Definition{Name: "idle-agent", Tree: "", Schedule: "on_demand"}); err != nil {
		t.Fatalf("registry Create: %v", err)
	}

	kicked := make(chan struct{}, 1)
	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		OnCycleIdle: func() {
			select {
			case kicked <- struct{}{}:
			default:
			}
		},
	})

	job := &ScheduledJob{ID: "job_idle_1", AgentName: "idle-agent", Schedule: "0 6 * * *", Timeout: "1m", Active: true}
	sched.mu.Lock()
	sched.jobs[job.ID] = job
	sched.mu.Unlock()

	sched.runJob(job, func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "ok", nil, nil
	})

	select {
	case <-kicked:
	case <-time.After(2 * time.Second):
		t.Fatal("OnCycleIdle hook did not fire after a cycle completed with an empty queue")
	}
}

// TestRunJob_NoIdleKickWhileAnotherJobInFlight: the hook must stay quiet while
// any other job is mid-execution — kicking there would invite a rebuild/restart
// under a running cycle.
func TestRunJob_NoIdleKickWhileAnotherJobInFlight(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	reg, err := NewRegistry(RegistryDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(Definition{Name: "busy-agent", Tree: "", Schedule: "on_demand"}); err != nil {
		t.Fatalf("registry Create: %v", err)
	}

	kicked := make(chan struct{}, 1)
	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		OnCycleIdle: func() {
			select {
			case kicked <- struct{}{}:
			default:
			}
		},
	})

	job := &ScheduledJob{ID: "job_busy_1", AgentName: "busy-agent", Schedule: "0 6 * * *", Timeout: "1m", Active: true}
	other := &ScheduledJob{ID: "job_busy_2", AgentName: "busy-agent", Schedule: "0 7 * * *", Timeout: "1m", Active: true, InFlight: true}
	sched.mu.Lock()
	sched.jobs[job.ID] = job
	sched.jobs[other.ID] = other
	sched.mu.Unlock()

	sched.runJob(job, func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "ok", nil, nil
	})

	select {
	case <-kicked:
		t.Fatal("OnCycleIdle fired while another job was in flight")
	case <-time.After(300 * time.Millisecond):
	}
}
