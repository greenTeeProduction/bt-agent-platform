package agent

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestScheduledPausePublishesDiagnosticWithoutFalseAlarm(t *testing.T) {
	for _, fault := range []string{"pause", "failure", "uncertain"} {
		t.Run(fault, func(t *testing.T) {
			old := GlobalAgentBus
			InitAgentBus(10)
			t.Cleanup(func() { GlobalAgentBus = old })
			events := GlobalAgentBus.Subscribe("")
			root := t.TempDir()
			reg, err := NewRegistry(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := reg.Create(Definition{Name: "stopped-agent", Tree: "domain:default", Version: "1.0.0"}); err != nil {
				t.Fatal(err)
			}
			history, err := NewHistory(filepath.Join(root, "history"))
			if err != nil {
				t.Fatal(err)
			}
			sched := NewScheduler(SchedulerConfig{Registry: reg, History: history, TickInterval: time.Hour})
			pause := &reliability.ExecutionStoppedError{Outcome: "input-required", Err: errors.New("network timeout awaits human input")}
			var diagnostic error = pause
			if fault == "failure" {
				diagnostic = errors.Join(pause, &reliability.ExecutionStoppedError{Outcome: "failure", Err: errors.New("sibling failed")})
			}
			if fault == "uncertain" {
				diagnostic = errors.Join(pause, &reliability.ExecutionUncertainError{Err: errors.New("lost sibling response")})
			}
			job := &ScheduledJob{ID: "stopped-job", AgentName: "stopped-agent", Schedule: "every 1h", Timeout: "30s"}
			sched.runJob(job, func(ctx RunContext) (string, string, *RunResult, error) {
				return "input-required", "received evidence", &RunResult{AgentName: ctx.AgentName, Outcome: "input-required"}, diagnostic
			})
			select {
			case event := <-events:
				data, ok := event.Data.(map[string]any)
				if !ok {
					t.Fatalf("data=%T", event.Data)
				}
				failure, _ := data["failure_reason"].(string)
				if (failure == "") != (fault == "pause") || data["execution_diagnostic"] != diagnostic.Error() {
					t.Fatalf("fault=%s data=%+v", fault, data)
				}
			default:
				t.Fatal("no completion event")
			}
		})
	}
}
