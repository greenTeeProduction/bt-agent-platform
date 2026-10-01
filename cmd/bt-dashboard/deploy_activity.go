package main

import (
	"errors"
	"net/http"
	"sync"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

var errDashboardRestarting = errors.New("dashboard restart handoff is pending")
var dashActivity = &dashboardActivityGate{}

// dashboardActivityGate owns requests and detached executions, including
// capacity waits and cleanup. Sealing and new admission share one mutex.
// Successful restart handoff keeps admission closed until the process exits.
type dashboardActivityGate struct {
	mu     sync.Mutex
	active int
	sealed bool
}

func (g *dashboardActivityGate) acquire() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sealed {
		return nil, errDashboardRestarting
	}
	g.active++
	return sync.OnceFunc(func() { g.mu.Lock(); defer g.mu.Unlock(); g.active-- }), nil
}

func (g *dashboardActivityGate) busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sealed || g.active > 0
}

// beginRestart supplements owned leases with current process-local execution
// diagnostics. Persisted queue entries and waiting pipeline records are not
// evidence of a live worker. It never claims to guard another process.
func (g *dashboardActivityGate) beginRestart() (func(bool), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sealed || g.active != 0 || dashBackgroundBusy() {
		return nil, false
	}
	g.sealed = true
	var once sync.Once
	return func(restarted bool) {
		once.Do(func() {
			if !restarted {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.sealed = false
			}
		})
	}, true
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
