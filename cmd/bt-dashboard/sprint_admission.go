package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/dashboard"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

const (
	sprintAdmissionTimeout = 30 * time.Second
	sprintBatchTimeout     = 5 * time.Minute
)

// Serialize new admission without holding the status mutex across capacity waits.
var sprintAdmission = reliability.NewConcurrencyLimiter(1)

type sprintBatch struct {
	ctx    context.Context
	cancel context.CancelFunc
	tasks  []dashboard.Task
}

type sprintReservation struct {
	decision chan *sprintBatch
	once     sync.Once
	release  func()
}

// abort returns capacity immediately. The queued callback later drains as a
// no-op; it cannot claim or execute tasks after a rejected HTTP request.
func (r *sprintReservation) abort() {
	r.once.Do(func() { r.decision <- nil; r.release() })
}

func (r *sprintReservation) start(ctx context.Context, tasks []dashboard.Task) time.Time {
	var deadline time.Time
	r.once.Do(func() {
		batchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sprintBatchTimeout)
		deadline, _ = batchCtx.Deadline()
		r.decision <- &sprintBatch{ctx: batchCtx, cancel: cancel, tasks: tasks}
	})
	return deadline
}

func reserveSprint(ctx context.Context, store *dashboard.TaskStore, executor *dashboard.AgentExecutor) (*sprintReservation, error) {
	limiter, pool := dashConcurrencyLimiter, dashWorkerPool
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limiter != nil {
		if err := limiter.AcquireWithContext(ctx); err != nil {
			return nil, err
		}
	}
	reservation := &sprintReservation{decision: make(chan *sprintBatch, 1)}
	reservation.release = sync.OnceFunc(func() {
		if limiter != nil {
			limiter.Release()
		}
	})
	run := func() {
		defer reservation.release()
		batch := <-reservation.decision
		if batch == nil {
			return
		}
		defer batch.cancel()
		executeSprintTasksWithContext(batch.ctx, store, executor, batch.tasks)
	}
	if pool != nil {
		if err := pool.SubmitWithContext(ctx, run); err != nil {
			reservation.release()
			return nil, err
		}
	} else {
		go run()
	}
	return reservation, nil
}

func writeSprintAdmissionError(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	message := "Sprint admission unavailable; no task was dispatched"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusRequestTimeout
		message = "Sprint admission canceled or expired; no task was dispatched"
	}
	w.Header().Set(reliability.ExecutionAdmissionHeader, "false")
	w.WriteHeader(status)
	_ = encodeJSON(w, map[string]string{"error": message})
}
