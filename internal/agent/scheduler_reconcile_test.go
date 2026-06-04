package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestScheduler_ReconcileWithRegistry_RemovesStaleAndRecurringOnDemandJobs(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "on-demand-agent", Tree: "domain:default", Schedule: "on_demand"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "daily-agent", Tree: "domain:default", Schedule: "0 9 * * *"}); err != nil {
		t.Fatal(err)
	}

	store := NewFileJobStore(filepath.Join(dir, "jobs.json"))
	now := time.Now()
	if err := store.Save([]ScheduledJob{
		{ID: "stale", AgentName: "missing-agent", Schedule: "0 1 * * *", NextRun: now, Active: true},
		{ID: "ondemand-active", AgentName: "on-demand-agent", Schedule: "every 30m", NextRun: now, Active: true},
		{ID: "daily-old", AgentName: "daily-agent", Schedule: "every 1h", NextRun: now, RunCount: 1, Active: true},
		{ID: "daily-new", AgentName: "daily-agent", Schedule: "0 9 * * *", NextRun: now.Add(time.Hour), RunCount: 2, Active: true},
	}); err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected exactly one reconciled active job, got %d: %+v", len(jobs), jobs)
	}
	job := jobs[0]
	if job.AgentName != "daily-agent" {
		t.Fatalf("expected daily-agent job, got %s", job.AgentName)
	}
	if job.Schedule != "0 9 * * *" {
		t.Fatalf("expected YAML schedule to win, got %q", job.Schedule)
	}
	if !job.Active {
		t.Fatal("daily-agent job should remain active")
	}
}

func TestScheduler_ReconcileWithRegistry_CreatesMissingYamlScheduledJob(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "hourly-agent", Tree: "domain:default", Schedule: "every 1h"}); err != nil {
		t.Fatal(err)
	}

	store := NewFileJobStore(filepath.Join(dir, "jobs.json"))
	sched := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected reconciler to create missing scheduled job, got %d", len(jobs))
	}
	if jobs[0].AgentName != "hourly-agent" || jobs[0].Schedule != "every 1h" || !jobs[0].Active {
		t.Fatalf("unexpected reconciled job: %+v", jobs[0])
	}
}
