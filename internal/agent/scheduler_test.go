package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistory_RecordAndList(t *testing.T) {
	dir := t.TempDir()
	h, err := NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Record some runs
	for i := 0; i < 5; i++ {
		h.Record(RunRecord{
			AgentName: "test-agent",
			Task:      fmt.Sprintf("task-%d", i),
			Outcome:   "success",
			Output:    fmt.Sprintf("output-%d", i),
			Duration:  "5s",
			Quality:   0.8,
			StartedAt: time.Now().Add(-time.Duration(5-i) * time.Hour),
			EndedAt:   time.Now().Add(-time.Duration(5-i) * time.Hour).Add(5 * time.Second),
		})
	}

	runs := h.List("test-agent", 3)
	if len(runs) != 3 {
		t.Fatalf("expected 3 runs, got %d", len(runs))
	}

	// Most recent first
	if runs[0].Task != "task-4" {
		t.Errorf("expected task-4 first, got %s", runs[0].Task)
	}

	stats := h.Stats("test-agent")
	if stats.TotalRuns != 5 {
		t.Errorf("expected 5 total, got %d", stats.TotalRuns)
	}
	if stats.SuccessRate != 1.0 {
		t.Errorf("expected 1.0 success rate, got %.2f", stats.SuccessRate)
	}
}

func TestHistory_Persistence(t *testing.T) {
	dir := t.TempDir()
	h, _ := NewHistory(dir)

	h.Record(RunRecord{
		AgentName: "persist-agent",
		Outcome:   "success",
		Duration:  "10s",
		Quality:   0.9,
	})

	// Reload from disk
	h2, err := NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}

	runs := h2.List("persist-agent", 10)
	if len(runs) != 1 {
		t.Fatalf("expected 1 run after reload, got %d", len(runs))
	}
	if runs[0].Outcome != "success" {
		t.Errorf("expected success, got %s", runs[0].Outcome)
	}
}

func TestHistory_Stats(t *testing.T) {
	dir := t.TempDir()
	h, _ := NewHistory(dir)

	// Mix of outcomes
	h.Record(RunRecord{AgentName: "stats-agent", Outcome: "success", Duration: "5s", Quality: 0.9, EndedAt: time.Now()})
	h.Record(RunRecord{AgentName: "stats-agent", Outcome: "failure", Duration: "2s", Quality: 0.3, EndedAt: time.Now()})
	h.Record(RunRecord{AgentName: "stats-agent", Outcome: "success", Duration: "8s", Quality: 0.7, EndedAt: time.Now()})
	h.Record(RunRecord{AgentName: "stats-agent", Outcome: "panic", Duration: "1s", Quality: 0.0, EndedAt: time.Now()})

	stats := h.Stats("stats-agent")
	if stats.TotalRuns != 4 {
		t.Errorf("total: %d", stats.TotalRuns)
	}
	if stats.SuccessRate != 0.5 {
		t.Errorf("expected 0.5 success, got %.2f", stats.SuccessRate)
	}
	if stats.TotalPanics != 1 {
		t.Errorf("expected 1 panic, got %d", stats.TotalPanics)
	}
}

func TestHistory_FileCreated(t *testing.T) {
	dir := t.TempDir()
	h, _ := NewHistory(dir)

	h.Record(RunRecord{AgentName: "file-agent", Outcome: "success", Duration: "1s", Quality: 1.0})

	// Verify .jsonl file exists
	path := filepath.Join(dir, "file-agent.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Error("file is empty")
	}
}

func TestScheduler_Schedule(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "sched-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: 1 * time.Second,
	})

	job, err := sched.Schedule("sched-agent", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}
	if job.AgentName != "sched-agent" {
		t.Errorf("wrong agent: %s", job.AgentName)
	}

	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}

	sched.RemoveJob(job.ID)
	if len(sched.ListJobs()) != 0 {
		t.Error("job not removed")
	}
}

func TestScheduler_RunNow(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "runnow-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist})

	runner := func(ctx RunContext) (string, string, error) {
		return "success", "Executed task: " + ctx.Task, nil
	}

	outcome, output, err := sched.RunNow("runnow-agent", "test task", runner, "30s")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != "success" {
		t.Errorf("expected success, got %s", outcome)
	}
	if len(output) < 10 {
		t.Error("output too short")
	}

	// Check history was recorded
	runs := hist.List("runnow-agent", 5)
	if len(runs) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(runs))
	}
}

func TestScheduler_RunJobPanicRecovery(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "panic-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: 100 * time.Millisecond,
	})

	// Runner that panics
	panickingRunner := func(ctx RunContext) (string, string, error) {
		panic("agent-crash")
	}

	// Schedule a job to run now (empty NextRun)
	job, err := sched.Schedule("panic-agent", "every 1h", "30m", 0)
	if err != nil {
		t.Fatal(err)
	}
	job.NextRun = time.Time{} // force immediate

	// Start the scheduler in a goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.Start(panickingRunner)
	}()

	// Wait for at least one tick
	time.Sleep(500 * time.Millisecond)
	sched.Stop()

	<-done

	// The scheduler should still be functional — not dead
	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Errorf("scheduler lost jobs after panic: %d", len(jobs))
	}

	// History should record the panic
	runs := hist.List("panic-agent", 5)
	if len(runs) == 0 {
		t.Fatal("no history records — panic was not recorded")
	}
	if runs[0].Outcome != "panic" {
		t.Errorf("expected outcome 'panic', got %q", runs[0].Outcome)
	}
}

func TestScheduler_NormalJobAfterPanic(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "good-agent", Tree: "domain:default", Version: "1.0.0"})
	reg.Create(Definition{Name: "bad-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: 100 * time.Millisecond,
	})

	// Runner that panics for bad-agent, succeeds for good-agent
	runner := func(ctx RunContext) (string, string, error) {
		if ctx.AgentName == "bad-agent" {
			panic("bad-agent-panic")
		}
		return "success", "all good", nil
	}

	job1, _ := sched.Schedule("bad-agent", "every 1h", "30m", 0)
	job1.NextRun = time.Time{}
	job2, _ := sched.Schedule("good-agent", "every 1h", "30m", 0)
	job2.NextRun = time.Time{}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.Start(runner)
	}()

	time.Sleep(800 * time.Millisecond)
	sched.Stop()
	<-done

	// Both agents should have runs recorded
	badRuns := hist.List("bad-agent", 5)
	goodRuns := hist.List("good-agent", 5)

	if len(badRuns) == 0 {
		t.Error("bad-agent: no runs recorded")
	} else if badRuns[0].Outcome != "panic" {
		t.Errorf("bad-agent: expected 'panic', got %q", badRuns[0].Outcome)
	}

	if len(goodRuns) == 0 {
		t.Error("good-agent: no runs — likely scheduler died from bad-agent panic")
	} else if goodRuns[0].Outcome != "success" {
		t.Errorf("good-agent: expected 'success', got %q", goodRuns[0].Outcome)
	}
}

func TestScheduler_CrashRecovery_InFlightReset(t *testing.T) {
	// Simulate a crash: schedule a job, mark it in-flight, then
	// "restart" the scheduler and verify the in-flight flag is cleared
	// and the job is scheduled to run immediately.
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "crash-agent", Tree: "domain:default", Version: "1.0.0", Description: "crash recovery test"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	jobStorePath := filepath.Join(dir, "jobs.json")
	store := NewFileJobStore(jobStorePath)

	// Scheduler 1: schedule a job, then manually mark it in-flight without running
	sched1 := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: store,
	})

	job, err := sched1.Schedule("crash-agent", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}

	// Manually mark in-flight and persist (simulating a crash mid-execution)
	sched1.mu.Lock()
	job.InFlight = true
	sched1.mu.Unlock()
	sched1.saveState()

	// Verify the persisted state has InFlight=true
	loadedJobs, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range loadedJobs {
		if j.ID == job.ID && j.InFlight {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("pre-condition failed: job not persisted with InFlight=true")
	}

	// Scheduler 2: "restart" — should detect the crashed job and reset it
	sched2 := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: store,
	})

	jobs := sched2.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 restored job, got %d", len(jobs))
	}

	restored := jobs[0]
	if restored.InFlight {
		t.Error("restored job still has InFlight=true — should have been cleared")
	}
	if !restored.NextRun.IsZero() {
		t.Errorf("restored job NextRun should be zero (run immediately), got %v", restored.NextRun)
	}
	if restored.RunCount != job.RunCount {
		t.Errorf("run_count should be preserved: was %d, got %d", job.RunCount, restored.RunCount)
	}
	if !restored.Active {
		t.Error("restored job should still be Active")
	}
}

func TestScheduler_CrashRecovery_CleanJobsUnaffected(t *testing.T) {
	// Verify that jobs without InFlight are not modified during recovery.
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "clean-agent", Tree: "domain:default", Version: "1.0.0", Description: "clean job test"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	jobStorePath := filepath.Join(dir, "jobs.json")
	store := NewFileJobStore(jobStorePath)

	sched1 := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: store,
	})

	job, _ := sched1.Schedule("clean-agent", "every 1h", "30m", 5)
	originalNextRun := job.NextRun
	originalRunCount := job.RunCount

	// Save clean state (InFlight=false by default)
	sched1.saveState()

	// Restart — clean jobs should be unaffected
	sched2 := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: store,
	})

	jobs := sched2.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 restored job, got %d", len(jobs))
	}

	restored := jobs[0]
	if restored.InFlight {
		t.Error("clean job should not be in-flight")
	}
	if restored.RunCount != originalRunCount {
		t.Errorf("run_count changed: was %d, got %d", originalRunCount, restored.RunCount)
	}
	// NextRun should be preserved for clean jobs
	if !restored.NextRun.Equal(originalNextRun) {
		t.Errorf("NextRun changed: was %v, got %v", originalNextRun, restored.NextRun)
	}
}

func TestScheduler_CrashRecovery_MultipleCrashedJobs(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "crash-a", Tree: "domain:default", Version: "1.0.0", Description: "crash a"})
	reg.Create(Definition{Name: "crash-b", Tree: "domain:default", Version: "1.0.0", Description: "crash b"})
	reg.Create(Definition{Name: "clean-c", Tree: "domain:default", Version: "1.0.0", Description: "clean c"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	jobStorePath := filepath.Join(dir, "jobs.json")
	store := NewFileJobStore(jobStorePath)

	sched1 := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: store,
	})

	jobA, _ := sched1.Schedule("crash-a", "every 1h", "30m", 3)
	jobB, _ := sched1.Schedule("crash-b", "every 1h", "30m", 3)
	jobC, _ := sched1.Schedule("clean-c", "every 2h", "30m", 3)

	// Mark A and B as crashed, C is clean
	sched1.mu.Lock()
	jobA.InFlight = true
	jobB.InFlight = true
	sched1.mu.Unlock()
	sched1.saveState()

	// Restart
	sched2 := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		JobStore: store,
	})

	jobs := sched2.ListJobs()
	if len(jobs) != 3 {
		t.Fatalf("expected 3 restored jobs, got %d", len(jobs))
	}

	for _, j := range jobs {
		switch j.AgentName {
		case "crash-a", "crash-b":
			if j.InFlight {
				t.Errorf("%s: InFlight should be cleared", j.AgentName)
			}
			if !j.NextRun.IsZero() {
				t.Errorf("%s: NextRun should be zero for immediate retry, got %v", j.AgentName, j.NextRun)
			}
		case "clean-c":
			if j.InFlight {
				t.Error("clean-c: should not be in-flight")
			}
			if j.NextRun.IsZero() {
				t.Error("clean-c: NextRun should be preserved, got zero")
			}
			if j.ID != jobC.ID {
				t.Errorf("clean-c: wrong ID: %s", j.ID)
			}
		}
	}
}

func TestScheduler_CrashRecovery_NoJobStore(t *testing.T) {
	// Without a JobStore, crash recovery is a no-op.
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	reg.Create(Definition{Name: "no-store-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
		// No JobStore — in-memory only
	})

	_, err := sched.Schedule("no-store-agent", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}

	jobs := sched.ListJobs()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].InFlight {
		t.Error("in-memory jobs should not be in-flight")
	}
}
