package agent

import (
	"path/filepath"
	"testing"
	"time"
)

// A job that was in-flight when bt-agent was killed must run IMMEDIATELY after
// restart, not wait for its next cron slot. loadState resets a crashed job's
// NextRun to zero ("run now"); this guarantee must survive the
// ReconcileWithRegistry pass the constructor runs right after.
func TestCrashedInFlightJobRunsImmediatelyAfterRestart(t *testing.T) {
	newSched := func(t *testing.T, persistedSchedule, registrySchedule string) ScheduledJob {
		t.Helper()
		dir := t.TempDir()
		reg, err := NewRegistry(filepath.Join(dir, "agents"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := reg.Create(Definition{Name: "goap-runner", Tree: "domain:default", Schedule: registrySchedule}); err != nil {
			t.Fatal(err)
		}
		store := NewFileJobStore(filepath.Join(dir, "jobs.json"))
		// Persisted as if killed mid-run: InFlight=true, NextRun in the future
		// (the daemon crashed after dispatch but before the next-run recompute).
		if err := store.Save([]ScheduledJob{{
			ID: "j1", AgentName: "goap-runner", Schedule: persistedSchedule,
			NextRun: time.Now().Add(6 * time.Hour), InFlight: true, RunCount: 3, Active: true,
		}}); err != nil {
			t.Fatal(err)
		}
		sched := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
		jobs := sched.ListJobs()
		if len(jobs) != 1 {
			t.Fatalf("want exactly one job after restart, got %d: %+v", len(jobs), jobs)
		}
		return jobs[0]
	}

	assertDueNow := func(t *testing.T, j ScheduledJob) {
		t.Helper()
		if j.InFlight {
			t.Fatal("crash recovery must clear InFlight")
		}
		if !j.NextRun.IsZero() && j.NextRun.After(time.Now()) {
			t.Fatalf("crashed job must be due immediately after restart; NextRun=%v is still in the future", j.NextRun)
		}
	}

	// Common case: persisted schedule equals the registry schedule.
	t.Run("schedule unchanged", func(t *testing.T) {
		assertDueNow(t, newSched(t, "0,30 * * * *", "0,30 * * * *"))
	})

	// Regression: the registry schedule string differs from the persisted one
	// (a genuine schedule change, or format normalization). Reconcile must not
	// push the crashed job to its next slot — it must still run immediately.
	t.Run("schedule changed by registry", func(t *testing.T) {
		assertDueNow(t, newSched(t, "0,30 * * * *", "15,45 * * * *"))
	})
}

// applyJobSchedule must still recompute NextRun for a NORMAL (non-crashed) job
// when the schedule changes — the immediate-run preservation is specific to the
// zero (crash-recovery) marker, not a blanket "never recompute".
func TestApplyJobScheduleRecomputesForNormalJob(t *testing.T) {
	job := &ScheduledJob{Schedule: "0 9 * * *", NextRun: time.Now().Add(time.Hour)}
	applyJobSchedule(job, "0 10 * * *")
	if job.Schedule != "0 10 * * *" {
		t.Fatalf("schedule must update to the new value, got %q", job.Schedule)
	}
	if job.NextRun.IsZero() {
		t.Fatal("a normal job's NextRun must be recomputed (non-zero) on a schedule change")
	}
}
