package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestRegistry_ReloadFromDisk(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "reload-agent", Tree: "domain:default", Schedule: "every 1h"}); err != nil {
		t.Fatal(err)
	}
	reg.instances["reload-agent"].RunCount = 5

	path := filepath.Join(dir, "agents", "reload-agent.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		t.Fatal(err)
	}
	def.Schedule = "every 30m"
	updated, err := yaml.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0644); err != nil {
		t.Fatal(err)
	}

	if err := reg.ReloadFromDisk(); err != nil {
		t.Fatal(err)
	}
	inst, err := reg.Get("reload-agent")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Definition.Schedule != "every 30m" {
		t.Fatalf("expected reloaded schedule every 30m, got %q", inst.Definition.Schedule)
	}
	if inst.RunCount != 5 {
		t.Fatalf("expected run count preserved, got %d", inst.RunCount)
	}
}

func TestScheduler_SyncFromRegistry_UpdatesChangedSchedule(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(filepath.Join(dir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "sync-agent", Tree: "domain:default", Schedule: "every 1h"}); err != nil {
		t.Fatal(err)
	}

	store := NewFileJobStore(filepath.Join(dir, "jobs.json"))
	sched := NewScheduler(SchedulerConfig{Registry: reg, JobStore: store})
	jobs := sched.ListJobs()
	if len(jobs) != 1 || jobs[0].Schedule != "every 1h" {
		t.Fatalf("unexpected initial jobs: %+v", jobs)
	}

	path := filepath.Join(dir, "agents", "sync-agent.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var def Definition
	if err := yaml.Unmarshal(data, &def); err != nil {
		t.Fatal(err)
	}
	def.Schedule = "every 15m"
	updated, err := yaml.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0644); err != nil {
		t.Fatal(err)
	}

	sched.SyncFromRegistry()
	jobs = sched.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected one job after sync, got %d", len(jobs))
	}
	if jobs[0].Schedule != "every 15m" {
		t.Fatalf("expected synced schedule every 15m, got %q", jobs[0].Schedule)
	}
	if jobs[0].NextRun.Before(time.Now()) {
		t.Fatalf("expected next run in the future, got %v", jobs[0].NextRun)
	}
}

func TestApplySchedule_PersistsRegistryAndJobs(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("BT_AGENT_HOME")
	t.Setenv("BT_AGENT_HOME", dir)
	t.Cleanup(func() {
		if origHome == "" {
			os.Unsetenv("BT_AGENT_HOME")
		} else {
			t.Setenv("BT_AGENT_HOME", origHome)
		}
	})

	reg, err := NewRegistry(RegistryDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "cli-agent", Tree: "domain:default", Schedule: "on_demand"}); err != nil {
		t.Fatal(err)
	}

	job, err := ApplySchedule(reg, "cli-agent", "0 9 * * *", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}
	if job.Schedule != "0 9 * * *" {
		t.Fatalf("unexpected job schedule %q", job.Schedule)
	}

	inst, err := reg.Get("cli-agent")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Definition.Schedule != "0 9 * * *" {
		t.Fatalf("expected registry schedule updated, got %q", inst.Definition.Schedule)
	}

	store := NewFileJobStore(SchedulerJobsFile())
	persisted, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].AgentName != "cli-agent" {
		t.Fatalf("unexpected persisted jobs: %+v", persisted)
	}
}

func TestDeleteRegisteredAgent(t *testing.T) {
	dir := t.TempDir()
	origHome := os.Getenv("BT_AGENT_HOME")
	t.Setenv("BT_AGENT_HOME", dir)
	t.Cleanup(func() {
		if origHome == "" {
			os.Unsetenv("BT_AGENT_HOME")
		} else {
			t.Setenv("BT_AGENT_HOME", origHome)
		}
	})

	reg, err := NewRegistry(RegistryDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "gone", Tree: "domain:default", Schedule: "every 1h"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplySchedule(reg, "gone", "every 1h", "2h", 3); err != nil {
		t.Fatal(err)
	}

	if err := DeleteRegisteredAgent(reg, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Get("gone"); err == nil {
		t.Fatal("expected agent removed from registry")
	}
	jobs, err := NewFileJobStore(SchedulerJobsFile()).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if job.AgentName == "gone" {
			t.Fatalf("expected scheduler jobs cleared, got %+v", jobs)
		}
	}
}
