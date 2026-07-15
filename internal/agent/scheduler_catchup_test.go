package agent

import (
	"testing"
	"time"
)

// newCatchupFixture builds an isolated registry with one agent and an
// in-memory scheduler, the exact shape main()'s startup auto-schedule loop
// operates on. BT_AGENT_HOME is redirected so registry YAML writes stay in the
// test sandbox.
func newCatchupFixture(t *testing.T, agentName, schedule string) *Scheduler {
	t.Helper()
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	reg, err := NewRegistry(RegistryDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := reg.Create(Definition{Name: agentName, Tree: "domain:default", Schedule: schedule}); err != nil {
		t.Fatalf("registry Create: %v", err)
	}
	return NewScheduler(SchedulerConfig{Registry: reg})
}

// TestSchedule_PreservesMissedNextRunForCatchUp reproduces the 2026-07-15
// hermes-daily-updater 24h hole: a restart while a daily slot was queued
// re-ran main()'s auto-schedule loop, and Schedule() overwrote the persisted
// past-due NextRun (06:00 today) with the next cron slot (06:00 TOMORROW),
// silently discarding the missed firing. When the schedule string is
// unchanged, a past-due NextRun must be preserved so the first tick catches
// the missed slot up.
func TestSchedule_PreservesMissedNextRunForCatchUp(t *testing.T) {
	sched := newCatchupFixture(t, "catchup-agent", "0 6 * * *")

	job, err := sched.Schedule("catchup-agent", "0 6 * * *", "2h", 3)
	if err != nil {
		t.Fatalf("initial Schedule: %v", err)
	}

	missed := time.Now().Add(-45 * time.Minute).Truncate(time.Second)
	sched.mu.Lock()
	job.NextRun = missed
	sched.mu.Unlock()

	// Startup auto-schedule replays the same YAML schedule.
	rescheduled, err := sched.Schedule("catchup-agent", "0 6 * * *", "2h", 3)
	if err != nil {
		t.Fatalf("re-Schedule: %v", err)
	}
	if !rescheduled.NextRun.Equal(missed) {
		t.Fatalf("Schedule() with unchanged cron overwrote a missed (past-due) NextRun: got %v, want preserved %v — the missed slot is silently dropped and a daily job loses a full day", rescheduled.NextRun, missed)
	}
}

// TestSchedule_PreservesCrashRecoveryImmediateRun pins the second half of the
// same bug: loadState() marks a crashed in-flight job with a zero NextRun
// ("run immediately"), but the startup auto-schedule's Schedule() call
// overwrote it with the next cron slot — observed 2026-07-15 06:54 when the
// "recovered" notebooklm-pipeline-monitor never re-ran until its next slot.
func TestSchedule_PreservesCrashRecoveryImmediateRun(t *testing.T) {
	sched := newCatchupFixture(t, "recovered-agent", "40 */2 * * *")

	job, err := sched.Schedule("recovered-agent", "40 */2 * * *", "2h", 3)
	if err != nil {
		t.Fatalf("initial Schedule: %v", err)
	}

	sched.mu.Lock()
	job.NextRun = time.Time{} // loadState's crash-recovery marker
	sched.mu.Unlock()

	rescheduled, err := sched.Schedule("recovered-agent", "40 */2 * * *", "2h", 3)
	if err != nil {
		t.Fatalf("re-Schedule: %v", err)
	}
	if !rescheduled.NextRun.IsZero() {
		t.Fatalf("Schedule() with unchanged cron overwrote the crash-recovery immediate-run marker: got %v, want zero time", rescheduled.NextRun)
	}
}

// TestSchedule_RecomputesNextRunOnScheduleChange is the positive control: an
// operator changing the cron MUST still move NextRun to the new schedule —
// catch-up preservation only applies while the schedule is unchanged.
func TestSchedule_RecomputesNextRunOnScheduleChange(t *testing.T) {
	sched := newCatchupFixture(t, "changed-agent", "0 6 * * *")

	job, err := sched.Schedule("changed-agent", "0 6 * * *", "2h", 3)
	if err != nil {
		t.Fatalf("initial Schedule: %v", err)
	}

	sched.mu.Lock()
	job.NextRun = time.Now().Add(-3 * time.Hour)
	sched.mu.Unlock()

	rescheduled, err := sched.Schedule("changed-agent", "30 7 * * *", "2h", 3)
	if err != nil {
		t.Fatalf("re-Schedule with new cron: %v", err)
	}
	if !rescheduled.NextRun.After(time.Now()) {
		t.Fatalf("schedule change must recompute NextRun into the future, got %v", rescheduled.NextRun)
	}
}

// TestReconcileWithRegistry_EmptyRegistryPreservesJobs: reconciliation against
// an unexpectedly EMPTY registry (failed/partial registry construction —
// main() swallows NewRegistry errors) previously warned and then dropped every
// persisted job anyway, losing all run history. An empty registry must abort
// reconciliation instead: stale-job cleanup can wait until the registry is
// actually readable.
func TestReconcileWithRegistry_EmptyRegistryPreservesJobs(t *testing.T) {
	t.Setenv("BT_AGENT_HOME", t.TempDir())
	reg, err := NewRegistry(RegistryDir())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	store := NewFileJobStore(SchedulerJobsFile())
	if err := store.Save([]ScheduledJob{{
		ID: "job_x_1", AgentName: "x", Schedule: "0 6 * * *",
		RunCount: 7, Active: true, MaxRetries: 3, Timeout: "2h",
	}}); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	sched := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})

	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("empty-registry reconcile dropped persisted jobs: got %d jobs, want 1 preserved", len(jobs))
	}
	if jobs[0].RunCount != 7 {
		t.Fatalf("preserved job lost run history: RunCount=%d, want 7", jobs[0].RunCount)
	}
}

// TestCycleBreakerSuccess_HealthyOutcomesDoNotTripBreaker: the circuit breaker
// previously counted the healthy no-code outcomes (no_change, degraded) as
// failures — a run of analysis-only cycles could open an agent's breaker with
// nothing actually broken. Healthy outcomes count as breaker success; genuine
// failures and errored runs still count as failures.
func TestCycleBreakerSuccess_HealthyOutcomesDoNotTripBreaker(t *testing.T) {
	cases := []struct {
		outcome string
		err     error
		want    bool
	}{
		{"success", nil, true},
		{"no_change", nil, true},
		{"degraded", nil, true},
		{"failure", nil, false},
		{"partial", nil, false},
		{"success", errTest, false},
		{"degraded", errTest, false},
	}
	for _, c := range cases {
		if got := cycleBreakerSuccess(c.outcome, c.err); got != c.want {
			t.Errorf("cycleBreakerSuccess(%q, err=%v) = %v, want %v", c.outcome, c.err, got, c.want)
		}
	}
}

var errTest = &testError{}

type testError struct{}

func (*testError) Error() string { return "test error" }
