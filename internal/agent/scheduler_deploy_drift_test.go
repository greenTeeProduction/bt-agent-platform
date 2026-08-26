package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// Milestone 1 of program 94b0b31 ("Close the automated deploy-drift loop").
// DeadLetterEntry.BuildRevision is already stamped at cmd/bt-agent/main.go's
// dlq.Push call site (3cd05fc), but the scheduler's own cycle-complete/webhook
// path — which fires on EVERY cycle, not just DLQ pushes — never got the same
// wiring. Without it, a webhook consumer (or a WARN log) has no way to tell
// whether a given cycle ran on a stale binary.
func TestRunJob_WebhookIncludesBuildRevision(t *testing.T) {
	prevHead := driftHeadFn
	driftHeadFn = func(string) (string, error) { return "abc123", nil }
	t.Cleanup(func() { driftHeadFn = prevHead })

	prevBus := GlobalAgentBus
	InitAgentBus(10)
	t.Cleanup(func() { GlobalAgentBus = prevBus })

	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "build-rev-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	hist, err := NewHistory(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}

	sched := NewScheduler(SchedulerConfig{
		Registry:      reg,
		History:       hist,
		TickInterval:  time.Hour,
		BuildRevision: "abc123",
	})

	job := &ScheduledJob{
		ID:        "job_build-rev-agent_test",
		AgentName: "build-rev-agent",
		Schedule:  "every 1h",
		Timeout:   "30s",
	}
	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "did the thing", &RunResult{AgentName: ctx.AgentName, Outcome: "success"}, nil
	}

	sched.runJob(job, runner)

	events := GlobalAgentBus.History(10)
	if len(events) == 0 {
		t.Fatal("no events published to AgentBus")
	}
	last := events[len(events)-1]
	data, ok := last.Data.(map[string]any)
	if !ok {
		t.Fatalf("event Data is not a map: %#v", last.Data)
	}
	if got := data["build_revision"]; got != "abc123" {
		t.Fatalf("event Data[build_revision] = %v, want %q", got, "abc123")
	}
}

// A scheduler cycle whose running build has fallen behind repo HEAD must WARN
// so operators can spot a stale daemon without cross-referencing dlq entries.
// In sync must stay silent — a WARN on every cycle would be alert noise.
func TestRunJob_WarnsOnDeployDriftAtCycleComplete(t *testing.T) {
	prevHead := driftHeadFn
	t.Cleanup(func() { driftHeadFn = prevHead })

	prevBus := GlobalAgentBus
	InitAgentBus(10)
	t.Cleanup(func() { GlobalAgentBus = prevBus })

	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(Definition{Name: "drift-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	hist, err := NewHistory(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatal(err)
	}

	runner := func(ctx RunContext) (string, string, *RunResult, error) {
		return "success", "did the thing", &RunResult{AgentName: ctx.AgentName, Outcome: "success"}, nil
	}

	t.Run("stale: WARNs with head and running revision", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "def456", nil }

		var buf bytes.Buffer
		prevLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(prevLogger) })

		sched := NewScheduler(SchedulerConfig{
			Registry:      reg,
			History:       hist,
			TickInterval:  time.Hour,
			BuildRevision: "abc123",
		})
		job := &ScheduledJob{ID: "job_drift-agent_stale", AgentName: "drift-agent", Schedule: "every 1h", Timeout: "30s"}
		sched.runJob(job, runner)

		found := false
		for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
			var rec map[string]any
			if err := json.Unmarshal(line, &rec); err != nil {
				continue
			}
			if rec["msg"] == "scheduler: deploy drift detected" &&
				rec["head_revision"] == "def456" && rec["running_revision"] == "abc123" {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected a deploy-drift WARN log line; got:\n%s", buf.String())
		}
	})

	t.Run("in sync: no drift WARN", func(t *testing.T) {
		driftHeadFn = func(string) (string, error) { return "abc123", nil }

		var buf bytes.Buffer
		prevLogger := slog.Default()
		slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
		t.Cleanup(func() { slog.SetDefault(prevLogger) })

		sched := NewScheduler(SchedulerConfig{
			Registry:      reg,
			History:       hist,
			TickInterval:  time.Hour,
			BuildRevision: "abc123",
		})
		job := &ScheduledJob{ID: "job_drift-agent_sync", AgentName: "drift-agent", Schedule: "every 1h", Timeout: "30s"}
		sched.runJob(job, runner)

		for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
			var rec map[string]any
			if err := json.Unmarshal(line, &rec); err != nil {
				continue
			}
			if rec["msg"] == "scheduler: deploy drift detected" {
				t.Fatalf("unexpected deploy-drift WARN when in sync: %s", line)
			}
		}
	})
}
