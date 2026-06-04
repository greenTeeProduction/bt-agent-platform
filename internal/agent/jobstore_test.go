package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileJobStore_SaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := NewFileJobStore(path)

	// Empty store returns nil
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}

	// Save some jobs
	input := []ScheduledJob{
		{ID: "job-1", AgentName: "agent-a", Schedule: "every 1h", Active: true},
		{ID: "job-2", AgentName: "agent-b", Schedule: "0 9 * * *", Active: true},
	}
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file at %s: %v", path, err)
	}

	// Load back
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(loaded))
	}
	if loaded[0].ID != "job-1" || loaded[1].ID != "job-2" {
		t.Errorf("IDs mismatch: %s, %s", loaded[0].ID, loaded[1].ID)
	}
	if loaded[0].AgentName != "agent-a" {
		t.Errorf("wrong agent name: %s", loaded[0].AgentName)
	}

	// Overwrite
	input2 := []ScheduledJob{
		{ID: "job-3", AgentName: "agent-c", Schedule: "every 30m", Active: false},
	}
	_ = store.Save(input2)
	loaded2, _ := store.Load()
	if len(loaded2) != 1 {
		t.Fatalf("expected 1 job after overwrite, got %d", len(loaded2))
	}
	if loaded2[0].ID != "job-3" {
		t.Errorf("expected job-3, got %s", loaded2[0].ID)
	}
}

func TestFileJobStore_EmptyPath(t *testing.T) {
	store := NewFileJobStore("")
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
	// Save should be a no-op with no error
	if err := store.Save([]ScheduledJob{{ID: "x"}}); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestScheduler_PersistenceOnSchedule(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "persist-agent", Tree: "domain:default", Version: "1.0.0"})

	store := NewFileJobStore(filepath.Join(dir, "scheduler-jobs.json"))

	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		JobStore: store,
	})
	defer sched.Stop()

	// Schedule a job
	job, err := sched.Schedule("persist-agent", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}

	// Load from disk — job should be persisted
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 persisted job, got %d", len(loaded))
	}
	if loaded[0].AgentName != "persist-agent" {
		t.Errorf("wrong agent: %s", loaded[0].AgentName)
	}

	// Remove the job
	if err := sched.RemoveJob(job.ID); err != nil {
		t.Fatal(err)
	}

	// Load from disk — should be empty
	loaded, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 jobs after remove, got %d", len(loaded))
	}
}

func TestScheduler_RestoreJobsOnStartup(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "restore-agent", Tree: "domain:default", Version: "1.0.0"})

	storePath := filepath.Join(dir, "scheduler-jobs.json")

	// Pre-populate the store with a job
	store1 := NewFileJobStore(storePath)
	_ = store1.Save([]ScheduledJob{
		{
			ID:        "existing-job",
			AgentName: "restore-agent",
			Schedule:  "every 30m",
			Active:    true,
			NextRun:   time.Now().Add(1 * time.Hour),
		},
	})

	// Create a new scheduler — should load existing jobs
	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		JobStore: NewFileJobStore(storePath),
	})
	defer sched.Stop()

	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 restored job, got %d", len(jobs))
	}
	if jobs[0].ID != "existing-job" {
		t.Errorf("wrong restored job ID: %s", jobs[0].ID)
	}
	if jobs[0].AgentName != "restore-agent" {
		t.Errorf("wrong agent: %s", jobs[0].AgentName)
	}
}

func TestScheduler_NoJobStore_NilSafe(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "nil-agent", Tree: "domain:default", Version: "1.0.0"})

	// No JobStore — everything should work without panics
	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		JobStore: nil,
	})

	job, err := sched.Schedule("nil-agent", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}

	// Should not panic
	_ = sched.RemoveJob(job.ID)
	sched.saveState()
	sched.saveStateLocked()
	sched.loadState()
}

func TestFileJobStore_NonExistentPath(t *testing.T) {
	store := NewFileJobStore("/nonexistent/path/to/jobs.json")
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs for non-existent file, got %d", len(jobs))
	}
}

func TestFileJobStore_Concurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	store := NewFileJobStore(path)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(_ int) {
			defer func() { done <- struct{}{} }()
			_ = store.Save([]ScheduledJob{
				{ID: "conc-job", AgentName: "conc-agent", Schedule: "every 1h", Active: true},
			})
			_, _ = store.Load()
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Final load should succeed
	jobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
}
