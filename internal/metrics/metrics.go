// Package metrics provides Prometheus-compatible metrics export for the BT platform.
// Exposes success rate, duration, quality per agent, plus HTTP handler metrics.
package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Metric Types ───────────────────────────────────────────────────────────

// Counter is a monotonically increasing counter.
type Counter struct {
	value uint64
}

func (c *Counter) Inc()         { atomic.AddUint64(&c.value, 1) }
func (c *Counter) Add(n uint64) { atomic.AddUint64(&c.value, n) }
func (c *Counter) Value() uint64 { return atomic.LoadUint64(&c.value) }

// Gauge is a value that can go up and down.
type Gauge struct {
	value int64
}

func (g *Gauge) Set(v int64)  { atomic.StoreInt64(&g.value, v) }
func (g *Gauge) Inc()         { atomic.AddInt64(&g.value, 1) }
func (g *Gauge) Dec()         { atomic.AddInt64(&g.value, -1) }
func (g *Gauge) Value() int64 { return atomic.LoadInt64(&g.value) }

// Histogram tracks distribution of values.
type Histogram struct {
	mu     sync.Mutex
	bounds []float64
	counts []uint64
	sum    float64
	total  uint64
}

func NewHistogram(bounds []float64) *Histogram {
	return &Histogram{bounds: bounds, counts: make([]uint64, len(bounds)+1)}
}

func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sum += v
	h.total++
	for i, b := range h.bounds {
		if v <= b {
			h.counts[i]++
			return
		}
	}
	h.counts[len(h.bounds)]++
}

// ─── Agent Metrics ──────────────────────────────────────────────────────────

// AgentMetrics tracks per-agent execution metrics.
type AgentMetrics struct {
	mu            sync.RWMutex
	agents        map[string]*AgentStats
	TotalRequests Counter
	TotalErrors   Counter
}

type AgentStats struct {
	Name         string    `json:"name"`
	SuccessCount uint64    `json:"success_count"`
	ErrorCount   uint64    `json:"error_count"`
	TotalCount   uint64    `json:"total_count"`
	TotalDurationMs uint64 `json:"total_duration_ms"`
	LastRun      time.Time `json:"last_run"`
}

var globalMetrics = &AgentMetrics{agents: make(map[string]*AgentStats)}

// RecordTask records a task execution for an agent.
func RecordTask(agentName string, success bool, durationMs uint64) {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()

	s, ok := globalMetrics.agents[agentName]
	if !ok {
		s = &AgentStats{Name: agentName}
		globalMetrics.agents[agentName] = s
	}
	s.TotalCount++
	if success {
		s.SuccessCount++
	} else {
		s.ErrorCount++
	}
	s.TotalDurationMs += durationMs
	s.LastRun = time.Now()
	globalMetrics.TotalRequests.Inc()
	if !success {
		globalMetrics.TotalErrors.Inc()
	}
}

// GetAgentMetrics returns a copy of all agent metrics.
func GetAgentMetrics() []AgentStats {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	result := make([]AgentStats, 0, len(globalMetrics.agents))
	for _, s := range globalMetrics.agents {
		result = append(result, *s)
	}
	return result
}

// ─── HTTP Metrics ───────────────────────────────────────────────────────────

var (
	httpRequestsTotal   Counter
	httpRequestDuration Histogram
	httpErrorsTotal     Counter
)

func init() {
	httpRequestDuration = *NewHistogram([]float64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000})
}

// MetricsMiddleware wraps an http.Handler and records request metrics.
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start).Milliseconds()
		httpRequestsTotal.Inc()
		httpRequestDuration.Observe(float64(duration))

		if wrapped.statusCode >= 400 {
			httpErrorsTotal.Inc()
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher so SSE and other streaming endpoints work
// through the MetricsMiddleware wrapper.
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ─── Prometheus Export ──────────────────────────────────────────────────────

// PrometheusHandler returns an http.Handler that serves metrics in Prometheus text format.
func PrometheusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writePrometheusMetrics(w)
	})
}

func writePrometheusMetrics(w http.ResponseWriter) {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	// HTTP metrics
	fmt.Fprintf(w, "# HELP bt_http_requests_total Total HTTP requests served.\n")
	fmt.Fprintf(w, "# TYPE bt_http_requests_total counter\n")
	fmt.Fprintf(w, "bt_http_requests_total %d\n\n", httpRequestsTotal.Value())

	fmt.Fprintf(w, "# HELP bt_http_errors_total Total HTTP error responses (4xx, 5xx).\n")
	fmt.Fprintf(w, "# TYPE bt_http_errors_total counter\n")
	fmt.Fprintf(w, "bt_http_errors_total %d\n\n", httpErrorsTotal.Value())

	fmt.Fprintf(w, "# HELP bt_http_request_duration_ms HTTP request duration in milliseconds.\n")
	fmt.Fprintf(w, "# TYPE bt_http_request_duration_ms histogram\n")
	hd := &httpRequestDuration
	hd.mu.Lock()
	total := hd.total
	sum := hd.sum
	for i, b := range hd.bounds {
		fmt.Fprintf(w, "bt_http_request_duration_ms_bucket{le=\"%.0f\"} %d\n", b, hd.counts[i])
	}
	fmt.Fprintf(w, "bt_http_request_duration_ms_bucket{le=\"+Inf\"} %d\n", hd.counts[len(hd.bounds)])
	fmt.Fprintf(w, "bt_http_request_duration_ms_sum %.0f\n", sum)
	fmt.Fprintf(w, "bt_http_request_duration_ms_count %d\n\n", total)
	hd.mu.Unlock()

	// Agent metrics
	fmt.Fprintf(w, "# HELP bt_agent_tasks_total Total tasks executed per agent.\n")
	fmt.Fprintf(w, "# TYPE bt_agent_tasks_total counter\n")
	for _, s := range globalMetrics.agents {
		label := fmt.Sprintf(`agent="%s"`, s.Name)
		fmt.Fprintf(w, "bt_agent_tasks_total{%s} %d\n", label, s.TotalCount)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "# HELP bt_agent_success_total Successful tasks per agent.\n")
	fmt.Fprintf(w, "# TYPE bt_agent_success_total counter\n")
	for _, s := range globalMetrics.agents {
		label := fmt.Sprintf(`agent="%s"`, s.Name)
		fmt.Fprintf(w, "bt_agent_success_total{%s} %d\n", label, s.SuccessCount)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "# HELP bt_agent_errors_total Error tasks per agent.\n")
	fmt.Fprintf(w, "# TYPE bt_agent_errors_total counter\n")
	for _, s := range globalMetrics.agents {
		label := fmt.Sprintf(`agent="%s"`, s.Name)
		fmt.Fprintf(w, "bt_agent_errors_total{%s} %d\n", label, s.ErrorCount)
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "# HELP bt_agent_duration_ms_total Total duration per agent in ms.\n")
	fmt.Fprintf(w, "# TYPE bt_agent_duration_ms_total counter\n")
	for _, s := range globalMetrics.agents {
		label := fmt.Sprintf(`agent="%s"`, s.Name)
		fmt.Fprintf(w, "bt_agent_duration_ms_total{%s} %d\n", label, s.TotalDurationMs)
	}
	fmt.Fprintln(w)

	// Global metrics
	fmt.Fprintf(w, "# HELP bt_total_requests Total task requests.\n")
	fmt.Fprintf(w, "# TYPE bt_total_requests counter\n")
	fmt.Fprintf(w, "bt_total_requests %d\n\n", globalMetrics.TotalRequests.Value())

	fmt.Fprintf(w, "# HELP bt_total_errors Total task errors.\n")
	fmt.Fprintf(w, "# TYPE bt_total_errors counter\n")
	fmt.Fprintf(w, "bt_total_errors %d\n\n", globalMetrics.TotalErrors.Value())
}

// ─── JSON Export ────────────────────────────────────────────────────────────

// MetricsJSON returns all metrics as a JSON-serializable map.
func MetricsJSON() map[string]interface{} {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	agentStats := make([]map[string]interface{}, 0, len(globalMetrics.agents))
	for _, s := range globalMetrics.agents {
		successRate := 0.0
		if s.TotalCount > 0 {
			successRate = float64(s.SuccessCount) / float64(s.TotalCount) * 100
		}
		avgDuration := 0.0
		if s.TotalCount > 0 {
			avgDuration = float64(s.TotalDurationMs) / float64(s.TotalCount)
		}
		agentStats = append(agentStats, map[string]interface{}{
			"name":             s.Name,
			"success_count":    s.SuccessCount,
			"error_count":      s.ErrorCount,
			"total_count":      s.TotalCount,
			"success_rate":     fmt.Sprintf("%.1f%%", successRate),
			"avg_duration_ms":  fmt.Sprintf("%.0f", avgDuration),
			"last_run":         s.LastRun.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"http_requests_total": httpRequestsTotal.Value(),
		"http_errors_total":   httpErrorsTotal.Value(),
		"total_requests":      globalMetrics.TotalRequests.Value(),
		"total_errors":        globalMetrics.TotalErrors.Value(),
		"agents":              agentStats,
	}
}

// ─── Health Check ───────────────────────────────────────────────────────────

// HealthResponse is the JSON response for the health endpoint.
type HealthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	Uptime    string `json:"uptime"`
	GoVersion string `json:"go_version"`
}

var startTime = time.Now()

// HealthJSON returns health status as JSON bytes.
func HealthJSON(version string) []byte {
	resp := HealthResponse{
		Status:    "ok",
		Version:   version,
		Uptime:    time.Since(startTime).String(),
		GoVersion: "go1.26.3",
	}
	b, _ := json.Marshal(resp)
	return b
}
