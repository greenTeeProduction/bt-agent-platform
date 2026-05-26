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
