package agent

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// A runner whose every retry attempt dies without producing a result returns
// ("", "", nil, err) — observed live 2026-07-15 00:08 when a rate-limited
// cycle exhausted its retries and the Telegram notification rendered
// "Outcome:  in 4s". The scheduler must stamp such runs as failures so
// history records and task_complete consumers never see an empty outcome.
func TestRunJobStampsFailureOutcomeWhenRetryExhaustionLeavesItEmpty(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "empty-outcome-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	hist, err := NewHistory(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}
	sched := NewScheduler(SchedulerConfig{Registry: reg, History: hist, TickInterval: time.Hour})

	job := &ScheduledJob{
		ID:        "job_empty-outcome-agent_test",
		AgentName: "empty-outcome-agent",
		Schedule:  "every 1h",
		Timeout:   "30s",
	}
	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return "", "", nil, errors.New("retry exhausted after 3 attempts (last: rate_limited): agent outcome: failure")
	}

	sched.runJob(job, runner)

	recs := hist.List("empty-outcome-agent", 1)
	if len(recs) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(recs))
	}
	if recs[0].Outcome != "failure" {
		t.Fatalf("history outcome = %q, want %q (empty outcomes render as \"Outcome:  in 4s\" in notifications)", recs[0].Outcome, "failure")
	}
}
