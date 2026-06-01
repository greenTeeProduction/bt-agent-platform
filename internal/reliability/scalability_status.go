// Package reliability provides scalability status aggregation for
// monitoring worker pools, queues, concurrency limiters, agent routers,
// and connection pools.
package reliability

import (
	"encoding/json"
	"net/http"
	"time"
)

// ScalabilityStatus aggregates stats from all scalability components
// — worker pool, concurrency limiter, queue, agent router, connection
// pool, and heartbeat — into a single JSON-serializable snapshot for
// dashboard monitoring of distributed multi-node deployments.
type ScalabilityStatus struct {
	// Timestamp is when this snapshot was taken.
	Timestamp time.Time `json:"timestamp"`

	// WorkerPool stats (nil if no pool configured).
	WorkerPool *WorkerPoolStats `json:"worker_pool,omitempty"`

	// ConcurrencyLimiter stats (nil if no limiter configured).
	ConcurrencyLimiter *ConcurrencyLimiterStats `json:"concurrency_limiter,omitempty"`

	// Queue stats (nil if no queue configured).
	Queue *QueueStats `json:"queue,omitempty"`

	// Router stats: number of executors, healthy, unhealthy, and failures.
	Router *RouterStats `json:"router,omitempty"`

	// ConnPool stats (nil if no connection pool configured).
	ConnPool *ConnPoolStats `json:"conn_pool,omitempty"`

	// Heartbeat stats (nil if no heartbeat tracker configured).
	Heartbeat *HeartbeatStats `json:"heartbeat,omitempty"`
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

// RouterStats captures agent executor distribution and failure tracking.
type RouterStats struct {
	Total     int `json:"total"`
	Healthy   int `json:"healthy"`
	Unhealthy int `json:"unhealthy"`
	Failures  int `json:"failures,omitempty"` // executors with consecutive failures > 0
}

// NewScalabilityStatus creates a status snapshot from the given components.
// Any nil component is omitted from the snapshot.
func NewScalabilityStatus(
	wp *WorkerPool,
	cl *ConcurrencyLimiter,
	pending int,
	maxLen int,
	routerTotal, routerHealthy int,
	cp *ConnPool,
	routerFailures int,
	hb *HeartbeatStats,
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

	if routerTotal > 0 || routerFailures > 0 {
		s.Router = &RouterStats{
			Total:     routerTotal,
			Healthy:   routerHealthy,
			Unhealthy: routerTotal - routerHealthy,
			Failures:  routerFailures,
		}
	}

	if cp != nil {
		cs := cp.Stats()
		s.ConnPool = &cs
	}

	if hb != nil {
		s.Heartbeat = hb
	}

	return s
}

// Workers returns the number of workers in the pool.
func (wp *WorkerPool) Workers() int {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	return wp.workers
}

// HTTPHandler returns an http.HandlerFunc that writes a JSON snapshot
// of all scalability components including heartbeat and failure tracking.
func HTTPHandler(
	wp *WorkerPool,
	cl *ConcurrencyLimiter,
	queuePending func() int,
	queueMaxLen func() int,
	routerTotal func() int,
	routerHealthy func() int,
	cp *ConnPool,
	routerFailures func() int,
	heartbeatStats func() *HeartbeatStats,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		var pending, maxLen, rTotal, rHealthy, rFailures int
		var hb *HeartbeatStats
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
		if routerFailures != nil {
			rFailures = routerFailures()
		}
		if heartbeatStats != nil {
			hb = heartbeatStats()
		}

		status := NewScalabilityStatus(wp, cl, pending, maxLen, rTotal, rHealthy, cp, rFailures, hb)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}
