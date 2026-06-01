// Package reliability provides scalability status aggregation for
// monitoring worker pools, queues, concurrency limiters, and agent routers.
package reliability

import (
	"encoding/json"
	"net/http"
	"time"
)

// ScalabilityStatus aggregates stats from all scalability components
// — worker pool, concurrency limiter, queue, and agent router — into
// a single JSON-serializable snapshot for dashboard monitoring.
type ScalabilityStatus struct {
	// Timestamp is when this snapshot was taken.
	Timestamp time.Time `json:"timestamp"`

	// WorkerPool stats (nil if no pool configured).
	WorkerPool *WorkerPoolStats `json:"worker_pool,omitempty"`

	// ConcurrencyLimiter stats (nil if no limiter configured).
	ConcurrencyLimiter *ConcurrencyLimiterStats `json:"concurrency_limiter,omitempty"`

	// Queue stats (nil if no queue configured).
	Queue *QueueStats `json:"queue,omitempty"`

	// Router stats: number of executors, healthy, unhealthy.
	Router *RouterStats `json:"router,omitempty"`
}

// WorkerPoolStats captures worker pool capacity and utilization.
type WorkerPoolStats struct {
	Workers   int    `json:"workers"`
	Active    int    `json:"active"`
	Queued    int    `json:"queued"`
	Total     uint64 `json:"total"`
	Completed uint64 `json:"completed"`
}

// ConcurrencyLimiterStats captures semaphore utilization.
type ConcurrencyLimiterStats struct {
	Active    int    `json:"active"`
	Waiting   int    `json:"waiting"`
	Capacity  int    `json:"capacity"`
	Available int    `json:"available"`
	Total     uint64 `json:"total"`
}

// QueueStats captures task queue depth and health.
type QueueStats struct {
	Pending int `json:"pending"`
	MaxLen  int `json:"max_len,omitempty"` // -1 = unbounded
}

// RouterStats captures agent executor distribution.
type RouterStats struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
}

// NewScalabilityStatus creates a status snapshot from the given components.
// Any nil component is omitted from the snapshot.
func NewScalabilityStatus(
	wp *WorkerPool,
	cl *ConcurrencyLimiter,
	pending int,
	maxLen int,
	routerTotal, routerHealthy int,
) *ScalabilityStatus {
	s := &ScalabilityStatus{Timestamp: time.Now()}

	if wp != nil {
		a, q, t, c := wp.Stats()
		s.WorkerPool = &WorkerPoolStats{
			Workers:   wp.Workers(),
			Active:    a,
			Queued:    q,
			Total:     t,
			Completed: c,
		}
	}

	if cl != nil {
		a, w, t := cl.Stats()
		s.ConcurrencyLimiter = &ConcurrencyLimiterStats{
			Active:    a,
			Waiting:   w,
			Capacity:  cl.Capacity(),
			Available: cl.Available(),
			Total:     t,
		}
	}

	if pending > 0 || maxLen != 0 {
		s.Queue = &QueueStats{
			Pending: pending,
			MaxLen:  maxLen,
		}
	}

	if routerTotal > 0 {
		s.Router = &RouterStats{
			Total:     routerTotal,
			Healthy:   routerHealthy,
			Unhealthy: routerTotal - routerHealthy,
		}
	}

	return s
}

// Workers returns the number of workers in the pool.
func (wp *WorkerPool) Workers() int {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.workers
}

// HTTPHandler returns an http.HandlerFunc that writes a JSON snapshot.
func HTTPHandler(
	wp *WorkerPool,
	cl *ConcurrencyLimiter,
	queuePending func() int,
	queueMaxLen func() int,
	routerTotal func() int,
	routerHealthy func() int,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var pending, maxLen, rTotal, rHealthy int
		if queuePending != nil {
			pending = queuePending()
		}
		if queueMaxLen != nil {
			maxLen = queueMaxLen()
		}
		if routerTotal != nil {
			rTotal = routerTotal()
		}
		if routerHealthy != nil {
			rHealthy = routerHealthy()
		}

		status := NewScalabilityStatus(wp, cl, pending, maxLen, rTotal, rHealthy)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}
