package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

type dashboardAgentExecution struct {
	Result   *agent.RunResult
	Duration time.Duration
	Err      error
}

// executeDashboardAgent owns admission and the result handoff for both HTTP
// execution surfaces. A reservation belongs to the accepted task until cleanup;
// a canceled HTTP waiter must not release a slot still used by running work.
func executeDashboardAgent(parent context.Context, agentName, task, treeID string) (dashboardAgentExecution, error) {
	endActivity, err := dashActivity.acquire()
	if err != nil {
		return dashboardAgentExecution{}, err
	}
	transferred := false
	defer func() {
		if !transferred {
			endActivity()
		}
	}()
	executor := newAgentExecutor()
	ctx, cancel := context.WithTimeout(parent, executor.Timeout)
	defer cancel()
	limiter, pool := dashConcurrencyLimiter, dashWorkerPool
	if err := ctx.Err(); err != nil {
		return dashboardAgentExecution{}, err
	}
	if limiter != nil {
		if err := limiter.AcquireWithContext(ctx); err != nil {
			return dashboardAgentExecution{}, err
		}
	}
	release := func() {
		if limiter != nil {
			limiter.Release()
		}
	}
	result := make(chan dashboardAgentExecution, 1)
	run := func() {
		defer endActivity()
		start := time.Now()
		var execution dashboardAgentExecution
		defer func() {
			if recovered := recover(); recovered != nil {
				execution.Result = &agent.RunResult{AgentName: agentName, Task: task, Outcome: "panic"}
				execution.Err = fmt.Errorf("agent execution panic: %v", recovered)
			}
			execution.Duration = time.Since(start)
			release()
			result <- execution
		}()
		if err := ctx.Err(); err != nil {
			execution.Err = err
			return
		}
		execution.Result, execution.Err = executor.RunTaskResultWithContext(ctx, agentName, task, treeID)
	}
	if pool != nil {
		if err := pool.SubmitWithContext(ctx, run); err != nil {
			release()
			return dashboardAgentExecution{}, err
		}
	} else {
		// Keep the same handoff even without a pool, so HTTP cancellation can
		// return while cooperative execution finishes its evidence/cleanup.
		go run()
	}
	transferred = true
	finish := func(execution dashboardAgentExecution) (dashboardAgentExecution, error) {
		if execution.Result == nil && (errors.Is(execution.Err, context.Canceled) || errors.Is(execution.Err, context.DeadlineExceeded)) {
			return execution, execution.Err
		}
		return execution, nil
	}
	select {
	case execution := <-result:
		return finish(execution)
	case <-ctx.Done():
		// Prefer a terminal result already handed off over a racing deadline.
		select {
		case execution := <-result:
			return finish(execution)
		default:
			return dashboardAgentExecution{}, ctx.Err()
		}
	}
}

func writeExecutionAdmissionError(w http.ResponseWriter, err error) {
	status, message := http.StatusServiceUnavailable, "execution service unavailable; task was not admitted"
	if errors.Is(err, errDashboardRestarting) {
		writeDashboardRestarting(w)
		return
	}
	if errors.Is(err, reliability.ErrWorkerPoolClosed) {
		w.Header().Set(reliability.ExecutionAdmissionHeader, "false")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusRequestTimeout
		message = "execution request canceled or expired; admitted work may have started, check run history before retrying"
	} else if !errors.Is(err, reliability.ErrWorkerPoolClosed) {
		status, message = http.StatusInternalServerError, "execution admission failed"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = encodeJSON(w, map[string]string{"error": message})
}
