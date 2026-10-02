package main

import (
	"os"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestSchedulerAttemptDoesNotRetryCompletedPersistenceDiagnostic(t *testing.T) {
	for _, outcome := range []string{"success", "no_change", "degraded"} {
		slo := &engine.SLOMetrics{AgentName: "history-" + outcome, TreeName: "test"}
		diagnostic := &reliability.ExecutionPersistenceError{Err: os.ErrPermission}
		if err := recordSchedulerAttempt(slo, outcome, diagnostic, "completed", 1, time.Second); err != nil {
			t.Fatalf("%s retry=%v", outcome, err)
		}
	}
}
