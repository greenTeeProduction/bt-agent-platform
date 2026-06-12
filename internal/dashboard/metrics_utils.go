// Package metrics provides Prometheus-compatible metrics export for the BT platform.
// Exposes success rate, duration, quality per agent, plus HTTP handler metrics.
package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ─── Metric Types ───────────────────────────────────────────────────────────

// Counter is a monotonically increasing counter.
type Counter struct {
	value uint64
}

func (c *Counter) Inc()          { atomic.AddUint64(&c.value, 1) }
func (c *Counter) Add(n uint64)  { atomic.AddUint64(&c.value, n) }
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

// SnapshotStats returns aggregate sum and count.
func (h *Histogram) SnapshotStats() HistogramSnap {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HistogramSnap{Sum: h.sum, Count: h.total}
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
	Name            string    `json:"name"`
	SuccessCount    uint64    `json:"success_count"`
	ErrorCount      uint64    `json:"error_count"`
	TotalCount      uint64    `json:"total_count"`
	TotalDurationMs uint64    `json:"total_duration_ms"`
	LastRun         time.Time `json:"last_run"`
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

// ─── Labeled Metrics ─────────────────────────────────────────────────────────

// LabeledCounter is a counter with label dimensions (Prometheus-compatible).
// Each unique label combination gets its own counter value.
type LabeledCounter struct {
	mu      sync.RWMutex
	buckets map[string]*Counter
}

// NewLabeledCounter creates a new labeled counter.
func NewLabeledCounter() *LabeledCounter {
	return &LabeledCounter{buckets: make(map[string]*Counter)}
}

// labelKey builds a deterministic key from label pairs. Labels are sorted for consistency.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	// Build a canonical key: sort by key name for deterministic ordering.
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sortStrings(keys)
	b := make([]byte, 0, 256)
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, labels[k]...)
	}
	return string(b)
}

// Inc increments the counter for the given label combination by 1.
func (lc *LabeledCounter) Inc(labels map[string]string) {
	lc.Add(1, labels)
}

// Add increments the counter for the given label combination by n.
func (lc *LabeledCounter) Add(n uint64, labels map[string]string) {
	key := labelKey(labels)
	lc.mu.RLock()
	c, ok := lc.buckets[key]
	lc.mu.RUnlock()
	if ok {
		c.Add(n)
		return
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	// Double-check after acquiring write lock
	if c, ok = lc.buckets[key]; ok {
		c.Add(n)
		return
	}
	c = &Counter{}
	c.Add(n)
	lc.buckets[key] = c
}

// Snapshot returns a copy of all label combinations and their values.
// The returned map is keyed by the canonical label string.
func (lc *LabeledCounter) Snapshot() map[string]uint64 {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	result := make(map[string]uint64, len(lc.buckets))
	for k, c := range lc.buckets {
		result[k] = c.Value()
	}
	return result
}

// LabeledGauge is a gauge with label dimensions (Prometheus-compatible).
type LabeledGauge struct {
	mu      sync.RWMutex
	buckets map[string]*Gauge
}

// NewLabeledGauge creates a new labeled gauge.
func NewLabeledGauge() *LabeledGauge {
	return &LabeledGauge{buckets: make(map[string]*Gauge)}
}

// Set sets the gauge value for the given label combination.
func (lg *LabeledGauge) Set(v int64, labels map[string]string) {
	key := labelKey(labels)
	lg.mu.RLock()
	g, ok := lg.buckets[key]
	lg.mu.RUnlock()
	if ok {
		g.Set(v)
		return
	}
	lg.mu.Lock()
	defer lg.mu.Unlock()
	if g, ok = lg.buckets[key]; ok {
		g.Set(v)
		return
	}
	g = &Gauge{}
	g.Set(v)
	lg.buckets[key] = g
}

// Snapshot returns a copy of all label combinations and their values.
func (lg *LabeledGauge) Snapshot() map[string]int64 {
	lg.mu.RLock()
	defer lg.mu.RUnlock()
	result := make(map[string]int64, len(lg.buckets))
	for k, g := range lg.buckets {
		result[k] = g.Value()
	}
	return result
}

// sortStrings sorts a slice of strings in place (simple insertion sort for small N).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// parseLabelKey reverses labelKey back to a map.
func parseLabelKey(key string) map[string]string {
	if key == "" {
		return map[string]string{}
	}
	result := make(map[string]string)
	pairs := splitOn(key, ',')
	for _, pair := range pairs {
		eq := indexOf(pair, '=')
		if eq > 0 && eq < len(pair)-1 {
			result[pair[:eq]] = pair[eq+1:]
		}
	}
	return result
}

func splitOn(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// ─── HTTP Metrics ───────────────────────────────────────────────────────────

var (
	httpRequestsTotal    Counter
	httpRequestDuration  Histogram
	httpErrorsTotal      Counter
	httpRequestsByMethod = NewLabeledCounter()
	httpRequestsByStatus = NewLabeledCounter()
	httpRequestsByPath   = NewLabeledCounter()
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

		// Labeled metrics for dimensional analysis
		statusBucket := bucketForStatus(wrapped.statusCode)
		httpRequestsByMethod.Inc(map[string]string{"method": r.Method})
		httpRequestsByStatus.Inc(map[string]string{"status": statusBucket})
		httpRequestsByPath.Inc(map[string]string{"path": r.URL.Path})
	})
}

// bucketForStatus maps HTTP status codes to Prometheus-compatible buckets.
func bucketForStatus(code int) string {
	switch {
	case code < 200:
		return "1xx"
	case code < 300:
		return "2xx"
	case code < 400:
		return "3xx"
	case code < 500:
		return "4xx"
	default:
		return "5xx"
	}
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
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		writePrometheusMetrics(w)
	})
}

func formatPromLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sortStrings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString("=\"")
		b.WriteString(labels[k])
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

func writePrometheusMetrics(w http.ResponseWriter) {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	// HTTP metrics
	fmt.Fprintf(w, "# HELP bt_http_requests_total Total HTTP requests served.\n")
	fmt.Fprintf(w, "# TYPE bt_http_requests_total counter\n")
	fmt.Fprintf(w, "bt_http_requests_total %d\n\n", httpRequestsTotal.Value())
	// BT node metrics
	fmt.Fprintf(w, "# HELP bt_node_ticks_total Behavior tree node ticks by type, name, and status.\n")
	fmt.Fprintf(w, "# TYPE bt_node_ticks_total counter\n")
	for key, val := range nodeTicksTotal.Snapshot() {
		labels := parseLabelKey(key)
		fmt.Fprintf(w, "bt_node_ticks_total%s %d\n", formatPromLabels(labels), val)
	}
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP bt_node_errors_total Behavior tree node failures.\n")
	fmt.Fprintf(w, "# TYPE bt_node_errors_total counter\n")
	for key, val := range nodeErrorsTotal.Snapshot() {
		labels := parseLabelKey(key)
		fmt.Fprintf(w, "bt_node_errors_total%s %d\n", formatPromLabels(labels), val)
	}
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP bt_block_ops_total Block expand/compose operations.\n")
	fmt.Fprintf(w, "# TYPE bt_block_ops_total counter\n")
	for key, val := range blockOpsTotal.Snapshot() {
		labels := parseLabelKey(key)
		fmt.Fprintf(w, "bt_block_ops_total%s %d\n", formatPromLabels(labels), val)
	}
	fmt.Fprintf(w, "\n")

	fmt.Fprintf(w, "# HELP bt_block_fitness_score Block fitness score (0-100) per block and agent.\n")
	fmt.Fprintf(w, "# TYPE bt_block_fitness_score gauge\n")
	for key, val := range blockFitnessGauge.Snapshot() {
		labels := parseLabelKey(key)
		fmt.Fprintf(w, "bt_block_fitness_score%s %d\n", formatPromLabels(labels), val)
	}
	fmt.Fprintf(w, "\n")

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

	// Labeled HTTP metrics — by method
	fmt.Fprintf(w, "# HELP bt_http_requests_by_method HTTP requests broken down by HTTP method.\n")
	fmt.Fprintf(w, "# TYPE bt_http_requests_by_method counter\n")
	for key, val := range httpRequestsByMethod.Snapshot() {
		labels := parseLabelKey(key)
		labelStr := labelString(labels)
		fmt.Fprintf(w, "bt_http_requests_by_method{%s} %d\n", labelStr, val)
	}
	fmt.Fprintln(w)

	// Labeled HTTP metrics — by status
	fmt.Fprintf(w, "# HELP bt_http_requests_by_status HTTP requests broken down by status bucket.\n")
	fmt.Fprintf(w, "# TYPE bt_http_requests_by_status counter\n")
	for key, val := range httpRequestsByStatus.Snapshot() {
		labels := parseLabelKey(key)
		labelStr := labelString(labels)
		fmt.Fprintf(w, "bt_http_requests_by_status{%s} %d\n", labelStr, val)
	}
	fmt.Fprintln(w)

	// Labeled HTTP metrics — by path
	fmt.Fprintf(w, "# HELP bt_http_requests_by_path HTTP requests broken down by URL path.\n")
	fmt.Fprintf(w, "# TYPE bt_http_requests_by_path counter\n")
	for key, val := range httpRequestsByPath.Snapshot() {
		labels := parseLabelKey(key)
		labelStr := labelString(labels)
		fmt.Fprintf(w, "bt_http_requests_by_path{%s} %d\n", labelStr, val)
	}
	fmt.Fprintln(w)
}

// labelString formats a label map into Prometheus label string format: key="value",key2="value2"
func labelString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sortStrings(keys)
	b := make([]byte, 0, 256)
	for i, k := range keys {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, k...)
		b = append(b, '=', '"')
		b = append(b, labels[k]...)
		b = append(b, '"')
	}
	return string(b)
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
			"name":            s.Name,
			"success_count":   s.SuccessCount,
			"error_count":     s.ErrorCount,
			"total_count":     s.TotalCount,
			"success_rate":    fmt.Sprintf("%.1f%%", successRate),
			"avg_duration_ms": fmt.Sprintf("%.0f", avgDuration),
			"last_run":        s.LastRun.Format(time.RFC3339),
		})
	}

	return map[string]interface{}{
		"http_requests_total":     httpRequestsTotal.Value(),
		"http_errors_total":       httpErrorsTotal.Value(),
		"total_requests":          globalMetrics.TotalRequests.Value(),
		"total_errors":            globalMetrics.TotalErrors.Value(),
		"agents":                  agentStats,
		"http_requests_by_method": labeledSnapshotToMap(httpRequestsByMethod.Snapshot()),
		"http_requests_by_status": labeledSnapshotToMap(httpRequestsByStatus.Snapshot()),
		"http_requests_by_path":   labeledSnapshotToMap(httpRequestsByPath.Snapshot()),
	}
}

// labeledSnapshotToMap converts a labeled counter snapshot to a JSON-friendly format
// with parsed label keys.
func labeledSnapshotToMap(snapshot map[string]uint64) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(snapshot))
	for key, val := range snapshot {
		labels := parseLabelKey(key)
		entry := make(map[string]interface{})
		for k, v := range labels {
			entry[k] = v
		}
		entry["count"] = val
		result = append(result, entry)
	}
	return result
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
