package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// TestNewScheduler_RestoresAndConfiguresFeedback is the read/startup half of
// feedback persistence: a fresh scheduler process must re-hydrate prior
// Fitness/RunCount from a snapshot on disk AND arm the debounced writer so that
// later feedback lands back on disk.
//
// It pre-registers a distinctly-named tree on GlobalGraph (LoadFeedback only
// merges into already-registered trees), writes a feedback snapshot carrying a
// known RunCount/Fitness for that tree, then constructs a scheduler with
// SchedulerConfig{FeedbackPath: path}. After construction it asserts the tree's
// runtime feedback was restored, then mutates it, forces a flush, and re-reads
// the file into a fresh graph to prove persistence was armed.
func TestNewScheduler_RestoresAndConfiguresFeedback(t *testing.T) {
	const treeID = "tree:sched-feedback-restore-test"

	// Register the tree with only baseline metadata so LoadFeedback has a target
	// to merge into. Clean up global state after the test to avoid bleed.
	knowledge.GlobalGraph.Register(&knowledge.TreeMeta{
		ID:       treeID,
		Name:     "Sched Feedback Restore Test",
		Category: "test",
		Fitness:  10.0,
	})
	t.Cleanup(func() {
		knowledge.GlobalGraph.ConfigureFeedbackPersistence("", 0)
		delete(knowledge.GlobalGraph.Trees, treeID)
	})

	// A feedback snapshot on disk carrying restored-from-a-prior-process values.
	path := filepath.Join(t.TempDir(), "feedback.json")
	snapshot := `{
  "trees": {
    "` + treeID + `": {
      "fitness": 73.5,
      "run_count": 7,
      "last_outcome": "success",
      "last_duration": 0
    }
  },
  "tool_edges": []
}`
	if err := os.WriteFile(path, []byte(snapshot), 0o644); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	dir := t.TempDir()
	reg, _ := NewRegistry(dir)

	// Construct the scheduler with the feedback path — this is the code under test.
	_ = NewScheduler(SchedulerConfig{
		Registry:     reg,
		FeedbackPath: path,
	})

	// The prior-process feedback must be re-hydrated into GlobalGraph.
	restored := knowledge.GlobalGraph.Trees[treeID]
	if restored == nil {
		t.Fatalf("%s missing after NewScheduler", treeID)
	}
	if restored.RunCount != 7 {
		t.Errorf("RunCount = %d, want 7 (restored from snapshot)", restored.RunCount)
	}
	if restored.Fitness != 73.5 {
		t.Errorf("Fitness = %.2f, want 73.50 (restored from snapshot)", restored.Fitness)
	}

	// Persistence must be armed: a later mutation + forced flush has to land on
	// disk. If NewScheduler did not call ConfigureFeedbackPersistence, the path
	// is empty and FlushFeedback is a no-op, so the new value never persists.
	restored.Fitness = 42.0
	knowledge.GlobalGraph.MarkFeedbackDirty()
	if err := knowledge.GlobalGraph.FlushFeedback(true); err != nil {
		t.Fatalf("FlushFeedback: %v", err)
	}

	// Read the rewritten file into a fresh graph and confirm the mutation landed.
	verify := knowledge.NewKnowledgeGraph()
	verify.Register(&knowledge.TreeMeta{ID: treeID, Name: "Verify", Category: "test"})
	if err := verify.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback (verify): %v", err)
	}
	if got := verify.Trees[treeID].Fitness; got != 42.0 {
		t.Errorf("persisted Fitness = %.2f, want 42.00 — persistence was not armed", got)
	}
}

// TestScheduler_PersistsFeedbackOnRunAndStop is the write/lifecycle half of
// feedback persistence, paired with TestNewScheduler_RestoresAndConfiguresFeedback.
// After a run records feedback into GlobalGraph, the scheduler must mark the graph
// dirty and attempt a throttled flush; on Stop() it must force a flush so any
// pending (throttled) feedback is durably written even inside the throttle window.
//
// It registers a distinctly-named tree on GlobalGraph and a tree-backed agent that
// points at it, arms persistence with a huge flush interval (so the first flush
// lands but a second, in-window flush is suppressed), then runs the agent twice and
// stops the scheduler. It asserts:
//   - after the first run the snapshot exists with RunCount == 1 (the run flushed),
//   - the second in-window run leaves the graph dirty (throttled, not lost),
//   - Stop() force-flushes so the file's decoded RunCount reaches 2.
func TestScheduler_PersistsFeedbackOnRunAndStop(t *testing.T) {
	const treeID = "tree:sched-feedback-write-test"

	// Register the tree with baseline metadata (RunCount 0) so RecordRun has a
	// target and SaveFeedback includes it. Clean up global state after the test.
	knowledge.GlobalGraph.Register(&knowledge.TreeMeta{
		ID:       treeID,
		Name:     "Sched Feedback Write Test",
		Category: "test",
		Fitness:  10.0,
	})
	t.Cleanup(func() {
		knowledge.GlobalGraph.ConfigureFeedbackPersistence("", 0)
		delete(knowledge.GlobalGraph.Trees, treeID)
	})

	// A huge flush interval keeps every non-forced flush after the first inside the
	// throttle window, which lets us distinguish "the run flushed" (first, window
	// open at construction) from "Stop force-flushed the pending state" (second).
	path := filepath.Join(t.TempDir(), "feedback.json")

	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "feedback-write-agent", Tree: treeID, Version: "1.0.0"})

	sched := NewScheduler(SchedulerConfig{
		Registry:              reg,
		FeedbackPath:          path,
		FeedbackFlushInterval: time.Hour,
	})

	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "ok: " + ctx.Task, nil, nil
	}

	// First run: RecordRun bumps RunCount to 1, then (once wired) MarkFeedbackDirty
	// + FlushFeedback(false) lands the write because the throttle window is open.
	if _, _, err := sched.RunNow("feedback-write-agent", "run one", runner, "30s"); err != nil {
		t.Fatalf("RunNow (first): %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot missing after first run — RunNow did not flush feedback: %v", err)
	}
	if got := decodeRunCount(t, treeID, path); got != 1 {
		t.Errorf("persisted RunCount after first run = %d, want 1", got)
	}

	// Second run within the throttle window: RecordRun bumps RunCount to 2 and marks
	// the graph dirty, but the non-forced flush is suppressed. The pending state must
	// NOT be lost — the dirty flag has to survive for Stop() to capture it.
	if _, _, err := sched.RunNow("feedback-write-agent", "run two", runner, "30s"); err != nil {
		t.Fatalf("RunNow (second): %v", err)
	}

	// The throttled write must not have touched disk yet: the run marked the graph
	// dirty but the in-window flush was suppressed, leaving RunCount 1 on disk.
	if got := decodeRunCount(t, treeID, path); got != 1 {
		t.Errorf("persisted RunCount before Stop = %d, want 1 (second run must be throttled)", got)
	}

	// Stop() must force-flush the pending state so it lands even inside the window.
	// FlushFeedback(true) is a no-op unless the graph is still dirty, so a RunCount
	// of 2 here proves both that the throttled run kept the dirty flag AND that Stop
	// forced the pending feedback to disk.
	sched.Stop()

	if got := decodeRunCount(t, treeID, path); got != 2 {
		t.Errorf("persisted RunCount after Stop = %d, want 2 — Stop did not force-flush pending feedback (dirty flag lost or no forced flush)", got)
	}
}

// decodeRunCount reads a feedback snapshot into a fresh graph carrying only static
// metadata for treeID, then returns the restored RunCount for that tree.
func decodeRunCount(t *testing.T, treeID, path string) int {
	t.Helper()
	g := knowledge.NewKnowledgeGraph()
	g.Register(&knowledge.TreeMeta{ID: treeID, Name: "Decode", Category: "test"})
	if err := g.LoadFeedback(path); err != nil {
		t.Fatalf("LoadFeedback(%s): %v", path, err)
	}
	tree := g.Trees[treeID]
	if tree == nil {
		t.Fatalf("%s missing after decode", treeID)
	}
	return tree.RunCount
}

func TestHistory_RecordAndList(t *testing.T) {
	dir := t.TempDir()
	h, err := NewHistory(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Record some runs
	for i := 0; i < 5; i++ {
		_ = h.Record(RunRecord{
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

	_ = h.Record(RunRecord{
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
	_ = h.Record(RunRecord{AgentName: "stats-agent", Outcome: "success", Duration: "5s", Quality: 0.9, EndedAt: time.Now()})
	_ = h.Record(RunRecord{AgentName: "stats-agent", Outcome: "failure", Duration: "2s", Quality: 0.3, EndedAt: time.Now()})
	_ = h.Record(RunRecord{AgentName: "stats-agent", Outcome: "success", Duration: "8s", Quality: 0.7, EndedAt: time.Now()})
	_ = h.Record(RunRecord{AgentName: "stats-agent", Outcome: "panic", Duration: "1s", Quality: 0.0, EndedAt: time.Now()})

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

	_ = h.Record(RunRecord{AgentName: "file-agent", Outcome: "success", Duration: "1s", Quality: 1.0})

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
	_, _ = reg.Create(Definition{Name: "sched-agent", Tree: "domain:default", Version: "1.0.0"})

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

	_ = sched.RemoveJob(job.ID)
	if len(sched.ListJobs()) != 0 {
		t.Error("job not removed")
	}
}

func TestScheduler_RunNow(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "runnow-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist})

	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "Executed task: " + ctx.Task, nil, nil
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
	_, _ = reg.Create(Definition{Name: "panic-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: 100 * time.Millisecond,
	})

	// Runner that panics
	panickingRunner := func(_ RunContext) (string, string, *RunResult, error) {
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
	_, _ = reg.Create(Definition{Name: "good-agent", Tree: "domain:default", Version: "1.0.0"})
	_, _ = reg.Create(Definition{Name: "bad-agent", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: 100 * time.Millisecond,
	})

	// Runner that panics for bad-agent, succeeds for good-agent
	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		if ctx.AgentName == "bad-agent" {
			panic("bad-agent-panic")
		}
		return "success", "all good", nil, nil
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
	_, _ = reg.Create(Definition{Name: "crash-agent", Tree: "domain:default", Version: "1.0.0", Description: "crash recovery test"})

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
	_, _ = reg.Create(Definition{Name: "clean-agent", Tree: "domain:default", Version: "1.0.0", Description: "clean job test"})

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
	_, _ = reg.Create(Definition{Name: "crash-a", Tree: "domain:default", Version: "1.0.0", Description: "crash a"})
	_, _ = reg.Create(Definition{Name: "crash-b", Tree: "domain:default", Version: "1.0.0", Description: "crash b"})
	_, _ = reg.Create(Definition{Name: "clean-c", Tree: "domain:default", Version: "1.0.0", Description: "clean c"})

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
	_, _ = reg.Create(Definition{Name: "no-store-agent", Tree: "domain:default", Version: "1.0.0"})

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

// AnyInFlight lets a caller (the deploy-drift rebuild guardrail, program
// 94b0b31 milestone 5) check whether it is safe to swap/restart the daemon's
// own binary — swapping out from under a job that is mid-execution is the
// hazard this exists to prevent. It must reflect the live in-flight state of
// any scheduled job, not just the one most recently touched.
func TestScheduler_AnyInFlight(t *testing.T) {
	dir := t.TempDir()
	reg, _ := NewRegistry(dir)
	_, _ = reg.Create(Definition{Name: "inflight-agent-a", Tree: "domain:default", Version: "1.0.0"})
	_, _ = reg.Create(Definition{Name: "inflight-agent-b", Tree: "domain:default", Version: "1.0.0"})

	histDir := filepath.Join(dir, "history")
	hist, _ := NewHistory(histDir)

	sched := NewScheduler(SchedulerConfig{
		Registry: reg,
		History:  hist,
	})

	jobA, err := sched.Schedule("inflight-agent-a", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}
	jobB, err := sched.Schedule("inflight-agent-b", "every 1h", "30m", 3)
	if err != nil {
		t.Fatal(err)
	}

	if sched.AnyInFlight() {
		t.Fatal("AnyInFlight = true with no jobs running, want false")
	}

	// Only the second job goes in-flight — AnyInFlight must not depend on
	// which job it is.
	sched.mu.Lock()
	jobB.InFlight = true
	sched.mu.Unlock()

	if !sched.AnyInFlight() {
		t.Fatal("AnyInFlight = false with jobB in-flight, want true")
	}

	sched.mu.Lock()
	jobB.InFlight = false
	jobA.InFlight = true
	sched.mu.Unlock()

	if !sched.AnyInFlight() {
		t.Fatal("AnyInFlight = false with jobA in-flight, want true")
	}

	sched.mu.Lock()
	jobA.InFlight = false
	sched.mu.Unlock()

	if sched.AnyInFlight() {
		t.Fatal("AnyInFlight = true after all jobs completed, want false")
	}
}

// A RateLimitCarryoverOutcome cycle is a healthy, expected backoff pause (see
// IsBreakerSuccess), not a genuine failure — the AgentEvent runJob publishes
// to GlobalAgentBus (→ Hermes webhook bridge) must not carry a failure_reason
// for it, or the Hermes webhook/Telegram template alarms on a healthy cycle.
func TestRunJob_RateLimitCarryoverOutcome_NoFailureReasonPublished(t *testing.T) {
	prevBus := GlobalAgentBus
	InitAgentBus(10)
	t.Cleanup(func() { GlobalAgentBus = prevBus })

	sub := GlobalAgentBus.Subscribe("")

	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "rate-limit-carryover-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}

	hist, err := NewHistory(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(SchedulerConfig{
		Registry:     reg,
		History:      hist,
		TickInterval: time.Hour,
	})

	job := &ScheduledJob{
		ID:        "job_rate-limit-carryover-agent_test",
		AgentName: "rate-limit-carryover-agent",
		Schedule:  "every 1h",
		Timeout:   "30s",
	}

	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return RateLimitCarryoverOutcome, "carrying over due to rate limit", &RunResult{
			AgentName: ctx.AgentName,
			Outcome:   RateLimitCarryoverOutcome,
		}, nil
	}

	sched.runJob(job, runner)

	select {
	case event := <-sub:
		data, ok := event.Data.(map[string]any)
		if !ok {
			t.Fatalf("event.Data is %T, want map[string]interface{}", event.Data)
		}
		if fr, _ := data["failure_reason"].(string); fr != "" {
			t.Fatalf("failure_reason = %q for RateLimitCarryoverOutcome, want empty (healthy backoff must not alarm the Hermes webhook/Telegram template)", fr)
		}
	default:
		t.Fatal("no event published on GlobalAgentBus")
	}
}

// TestRunJob_HealthyOutcomes_NoFailureReasonPublished extends the same
// contract to the other healthy non-"success" outcomes: a no_change
// (analysis-only) or degraded (deterministic fallback) cycle keeps the breaker
// closed via IsBreakerSuccess, so the event runJob publishes must not carry a
// failure_reason either — the pre-fix gate exempted only success and the
// rate-limit carryover, so buildRunActivitySummary labeled healthy no_change
// cycles "FAILED: agent outcome: no_change" in the operator-facing summary.
func TestRunJob_HealthyOutcomes_NoFailureReasonPublished(t *testing.T) {
	for _, outcome := range []string{"no_change", "degraded"} {
		t.Run(outcome, func(t *testing.T) {
			prevBus := GlobalAgentBus
			InitAgentBus(10)
			t.Cleanup(func() { GlobalAgentBus = prevBus })

			sub := GlobalAgentBus.Subscribe("")

			dir := t.TempDir()
			reg, err := NewRegistry(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reg.Create(Definition{Name: "healthy-outcome-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
				t.Fatal(err)
			}
			hist, err := NewHistory(filepath.Join(dir, "history"))
			if err != nil {
				t.Fatal(err)
			}
			sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist, TickInterval: time.Hour})
			job := &ScheduledJob{
				ID:        "job_healthy-outcome-agent_test",
				AgentName: "healthy-outcome-agent",
				Schedule:  "every 1h",
				Timeout:   "30s",
			}
			runner := func(ctx RunContext) (string, string, *RunResult, error) {
				return outcome, "analysis complete", &RunResult{AgentName: ctx.AgentName, Outcome: outcome}, nil
			}

			sched.runJob(job, runner)

			select {
			case event := <-sub:
				data, ok := event.Data.(map[string]any)
				if !ok {
					t.Fatalf("event.Data is %T, want map[string]interface{}", event.Data)
				}
				if fr, _ := data["failure_reason"].(string); fr != "" {
					t.Fatalf("failure_reason = %q for healthy outcome %q, want empty (a healthy cycle must not be labeled FAILED in the operator summary)", fr, outcome)
				}
			default:
				t.Fatal("no event published on GlobalAgentBus")
			}
		})
	}
}
