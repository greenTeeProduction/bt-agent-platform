// Package metrics provides Prometheus-compatible metrics export for the BT platform.
// Exposes success rate, duration, quality per agent, plus HTTP handler metrics.
package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strconv"
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

// SnapshotStats returns aggregate sum, count, and cumulative per-bucket counts.
func (h *Histogram) SnapshotStats() HistogramSnap {
	h.mu.Lock()
	defer h.mu.Unlock()
	bounds := make([]float64, len(h.bounds))
	copy(bounds, h.bounds)
	cumulative := make([]uint64, len(h.bounds))
	var running uint64
	for i := range h.bounds {
		running += h.counts[i]
		cumulative[i] = running
	}
	return HistogramSnap{Sum: h.sum, Count: h.total, Bounds: bounds, CumulativeCounts: cumulative}
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

// agentTaskDurationHist tracks per-agent task duration distributions so
// latency-percentile alerts can be defined per agent.
var agentTaskDurationHist = NewLabeledHistogram([]float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 30000, 120000, 300000})

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
	agentTaskDurationHist.Observe(float64(durationMs), map[string]string{"agent": agentName})
}

// ─── Knowledge-Graph Analytics ──────────────────────────────────────────────

// Knowledge-graph analytics gauges make ComputeAnalytics signals measurable in
// Prometheus/Grafana instead of living only in the text report bt_kg_analytics
// returns. They track the latest analytics run — RecordKGAnalytics REPLACES the
// values, so a scrape reflects current graph health rather than a running sum.
var (
	kgCoverageGapsGauge           Gauge
	kgBottlenecksGauge            Gauge
	kgSelectionPressureTreesGauge Gauge
)

// RecordKGAnalytics publishes the coverage-gap, bottleneck, and
// selection-pressure counts from a ComputeAnalytics run as plain gauges, so
// analytics drift is visible on /metrics. Each call overwrites the previous
// values (gauges track the latest run, never accumulate).
func RecordKGAnalytics(coverageGaps, bottlenecks, selectionPressureTrees int) {
	kgCoverageGapsGauge.Set(int64(coverageGaps))
	kgBottlenecksGauge.Set(int64(bottlenecks))
	kgSelectionPressureTreesGauge.Set(int64(selectionPressureTrees))
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

// ─── Build Identity ─────────────────────────────────────────────────────────

// unknownBuildValue is the sentinel used when the binary carries no VCS build
// stamping (-buildvcs=false, test binaries, tarball builds). It keeps the
// startup log line and the bt_build_info gauge from ever exposing an empty
// revision label, which Prometheus label matchers would silently match
// everything on.
const unknownBuildValue = "unknown"

// BuildIdentity is the VCS identity a binary was built from, used to detect
// stale-daemon-binary drift by comparing the running revision against repo
// HEAD.
type BuildIdentity struct {
	Revision   string `json:"revision"`
	CommitTime string `json:"commit_time"`
	Dirty      bool   `json:"dirty"`
}

var (
	buildIdentityMu  sync.RWMutex
	currentBuildInfo *BuildIdentity
)

// stampedRevision is the VCS revision injected at build time via
//
//	-ldflags "-X github.com/nico/go-bt-evolve/internal/dashboard.stampedRevision=<sha>"
//
// It is the fallback for binaries built from the bare main repo, where
// `go build` cannot resolve VCS info and would otherwise leave the running
// revision "unknown" — which makes DriftStatus permanently inert. Empty for
// developer/test builds; those keep the buildinfo-or-unknown behavior.
var stampedRevision string

// BuildIdentityFromBuildInfo extracts the VCS identity from runtime/debug
// build info. Missing settings and a nil build info degrade to the
// unknownBuildValue sentinel rather than empty strings, unless an ldflags stamp
// (stampedRevision) supplies the revision the bare-repo build could not.
func BuildIdentityFromBuildInfo(bi *debug.BuildInfo) BuildIdentity {
	id := BuildIdentity{Revision: unknownBuildValue, CommitTime: unknownBuildValue}
	if bi != nil {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if s.Value != "" {
					id.Revision = s.Value
				}
			case "vcs.time":
				if s.Value != "" {
					id.CommitTime = s.Value
				}
			case "vcs.modified":
				id.Dirty = s.Value == "true"
			}
		}
	}
	// Bare-repo builds carry no vcs.revision; the ldflags stamp is the only
	// revision they have. Real buildinfo always wins when present.
	if id.Revision == unknownBuildValue && stampedRevision != "" {
		id.Revision = stampedRevision
	}
	return id
}

// ReadBuildIdentity reads the running process's build identity. Fields are
// never empty: unstamped builds yield the unknownBuildValue sentinel.
func ReadBuildIdentity() BuildIdentity {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = nil
	}
	return BuildIdentityFromBuildInfo(bi)
}

// SetBuildIdentity pins the identity exposed as the bt_build_info gauge.
// Setting a new identity replaces the previous series — the exposition always
// carries exactly one bt_build_info series, so a scrape comparing the running
// revision against repo HEAD can never match a stale leftover.
func SetBuildIdentity(id BuildIdentity) {
	buildIdentityMu.Lock()
	currentBuildInfo = &id
	buildIdentityMu.Unlock()
}

// InstallBuildIdentity reads the process build identity, publishes it as the
// bt_build_info gauge, and returns it for the caller's startup log line.
func InstallBuildIdentity() BuildIdentity {
	id := ReadBuildIdentity()
	SetBuildIdentity(id)
	return id
}

// exposedBuildIdentity returns the identity to render on /metrics: the pinned
// one if set, otherwise the process self-identifies via ReadBuildIdentity so
// the gauge is present without any main-package wiring.
func exposedBuildIdentity() BuildIdentity {
	buildIdentityMu.RLock()
	id := currentBuildInfo
	buildIdentityMu.RUnlock()
	if id != nil {
		return *id
	}
	return ReadBuildIdentity()
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

// writeLabeledHistogram renders a LabeledHistogram as full Prometheus
// histogram series: cumulative _bucket lines (including +Inf), _sum, _count.
func writeLabeledHistogram(w io.Writer, name, help string, lh *LabeledHistogram) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)
	for key, snap := range lh.Snapshot() {
		labels := parseLabelKey(key)
		for i, bound := range snap.Bounds {
			le := strconv.FormatFloat(bound, 'f', -1, 64)
			fmt.Fprintf(w, "%s_bucket%s %d\n", name, formatPromLabels(withLeLabel(labels, le)), snap.CumulativeCounts[i])
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", name, formatPromLabels(withLeLabel(labels, "+Inf")), snap.Count)
		fmt.Fprintf(w, "%s_sum%s %s\n", name, formatPromLabels(labels), strconv.FormatFloat(snap.Sum, 'f', -1, 64))
		fmt.Fprintf(w, "%s_count%s %d\n", name, formatPromLabels(labels), snap.Count)
	}
	fmt.Fprintf(w, "\n")
}

// withLeLabel returns a copy of labels with the Prometheus "le" bound added.
func withLeLabel(labels map[string]string, le string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		out[k] = v
	}
	out["le"] = le
	return out
}

func writePrometheusMetrics(w http.ResponseWriter) {
	globalMetrics.mu.RLock()
	defer globalMetrics.mu.RUnlock()

	// Build identity — always exactly one series, so the running binary's
	// revision is comparable against repo HEAD (stale-daemon-binary drift).
	id := exposedBuildIdentity()
	fmt.Fprintf(w, "# HELP bt_build_info Build identity of the serving binary (value is always 1; identity in labels).\n")
	fmt.Fprintf(w, "# TYPE bt_build_info gauge\n")
	fmt.Fprintf(w, "bt_build_info{dirty=\"%t\",revision=\"%s\"} 1\n\n", id.Dirty, id.Revision)

	// Knowledge-graph analytics — latest ComputeAnalytics run, so coverage,
	// bottleneck, and selection-pressure drift is queryable in Prometheus.
	fmt.Fprintf(w, "# HELP bt_kg_coverage_gaps Knowledge-graph coverage gaps (domains/tasks with no covering tree) from the latest analytics run.\n")
	fmt.Fprintf(w, "# TYPE bt_kg_coverage_gaps gauge\n")
	fmt.Fprintf(w, "bt_kg_coverage_gaps %d\n\n", kgCoverageGapsGauge.Value())

	fmt.Fprintf(w, "# HELP bt_kg_bottlenecks Knowledge-graph bottleneck trees (low success rate) from the latest analytics run.\n")
	fmt.Fprintf(w, "# TYPE bt_kg_bottlenecks gauge\n")
	fmt.Fprintf(w, "bt_kg_bottlenecks %d\n\n", kgBottlenecksGauge.Value())

	fmt.Fprintf(w, "# HELP bt_kg_selection_pressure_trees Proven-but-underbred trees under selection pressure from the latest analytics run.\n")
	fmt.Fprintf(w, "# TYPE bt_kg_selection_pressure_trees gauge\n")
	fmt.Fprintf(w, "bt_kg_selection_pressure_trees %d\n\n", kgSelectionPressureTreesGauge.Value())

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

	writeLabeledHistogram(w, "bt_node_duration_ms", "Behavior tree node tick duration in milliseconds.", nodeDurationHist)
	writeLabeledHistogram(w, "bt_block_duration_ms", "Block operation duration in milliseconds.", blockDurationHist)

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

	writeLabeledHistogram(w, "bt_agent_task_duration_ms", "Task duration per agent in milliseconds.", agentTaskDurationHist)

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
