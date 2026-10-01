package main

import (
	"net/http"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

var errDashboardRestarting = reliability.ErrRestartPending
var dashActivity = &dashboardActivityGate{}

// dashboardActivityGate owns requests and detached executions, including
// capacity waits and cleanup. Sealing and new admission share one mutex.
// Successful restart handoff keeps admission closed until the process exits.
type dashboardActivityGate struct {
	reliability.RestartAdmissionGate
}

func (g *dashboardActivityGate) acquire() (func(), error) { return g.Acquire() }
func (g *dashboardActivityGate) busy() bool               { return g.Busy() }
func (g *dashboardActivityGate) beginRestart() (func(bool), bool) {
	return g.BeginRestart(dashBackgroundBusy)
}

func dashBackgroundBusy() bool {
	if dashWorkerPool != nil {
		active, queued, _, _ := dashWorkerPool.Stats()
		if active > 0 || queued > 0 {
			return true
		}
	}
	if dashConcurrencyLimiter != nil {
		active, waiting, _ := dashConcurrencyLimiter.Stats()
		if active > 0 || waiting > 0 {
			return true
		}
	}
	sprintState.Lock()
	running := sprintState.Running
	sprintState.Unlock()
	if running {
		return true
	}
	pipelineRunsMu.RLock()
	defer pipelineRunsMu.RUnlock()
	for _, run := range pipelineRuns {
		if run.Status == "running" {
			return true
		}
	}
	return false
}

func writeDashboardRestarting(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "1")
	w.Header().Set(reliability.ExecutionAdmissionHeader, "false")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = encodeJSON(w, map[string]string{"error": "Dashboard is restarting; request was not admitted"})
}
