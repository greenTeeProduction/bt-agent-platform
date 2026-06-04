package engine

import (
	"fmt"
	"strings"
	"sync"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// NotebookLMMetrics tracks operational metrics across all NotebookLM tool calls.
type NotebookLMMetrics struct {
	mu sync.RWMutex

	// Latency histograms (last 20 calls per op)
	GetLatency       []time.Duration
	ListLatency      []time.Duration
	ResearchLatency  []time.Duration
	ImportLatency    []time.Duration
	QueryLatency     []time.Duration
	AuthCheckLatency []time.Duration
	AuthRefreshCount int

	// Success/failure counts
	TotalCalls    int
	FailedCalls   int
	CBOpenedCount int // times circuit breaker opened

	// Source metrics
	SourcesImported int
	SourcesPerRun   []int
	LastSourceCount int
	LastNotebookID  string

	// Circuit breaker state
	CBOpen       bool
	CBLastOpened time.Time
}

// Global metrics instance.
var nlmMetrics = &NotebookLMMetrics{}

// RecordLatency records a call latency for the given operation.
func (m *NotebookLMMetrics) RecordLatency(op string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCalls++
	switch op {
	case "get":
		m.GetLatency = appendLatency(m.GetLatency, d)
	case "list":
		m.ListLatency = appendLatency(m.ListLatency, d)
	case "research":
		m.ResearchLatency = appendLatency(m.ResearchLatency, d)
	case "import":
		m.ImportLatency = appendLatency(m.ImportLatency, d)
	case "query":
		m.QueryLatency = appendLatency(m.QueryLatency, d)
	case "auth":
		m.AuthCheckLatency = appendLatency(m.AuthCheckLatency, d)
	}
}

// RecordFailure increments the failure counter.
func (m *NotebookLMMetrics) RecordFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedCalls++
}

// RecordCBOpened records a circuit breaker open event.
func (m *NotebookLMMetrics) RecordCBOpened() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CBOpenedCount++
	m.CBOpen = true
	m.CBLastOpened = time.Now()
}

// RecordCBClosed records a circuit breaker close event.
func (m *NotebookLMMetrics) RecordCBClosed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CBOpen = false
}

// RecordSourceImport records imported source counts.
func (m *NotebookLMMetrics) RecordSourceImport(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SourcesImported += count
	m.SourcesPerRun = append(m.SourcesPerRun, count)
}

// RecordSourceCount records the current notebook source count.
func (m *NotebookLMMetrics) RecordSourceCount(notebookID string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastSourceCount = count
	m.LastNotebookID = notebookID
}

// Summary returns a human-readable metrics summary.
func (m *NotebookLMMetrics) Summary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder
	sb.WriteString("## NotebookLM Metrics\n\n")

	// Latency
	sb.WriteString("### Latency (avg of recent)\n")
	sb.WriteString(fmt.Sprintf("- notebook_get: %s\n", avgLatency(m.GetLatency)))
	sb.WriteString(fmt.Sprintf("- notebook_list: %s\n", avgLatency(m.ListLatency)))
	sb.WriteString(fmt.Sprintf("- research: %s\n", avgLatency(m.ResearchLatency)))
	sb.WriteString(fmt.Sprintf("- import: %s\n", avgLatency(m.ImportLatency)))
	sb.WriteString(fmt.Sprintf("- query: %s\n", avgLatency(m.QueryLatency)))
	sb.WriteString(fmt.Sprintf("- auth_check: %s\n", avgLatency(m.AuthCheckLatency)))

	// Success rate
	successRate := 0.0
	if m.TotalCalls > 0 {
		successRate = float64(m.TotalCalls-m.FailedCalls) / float64(m.TotalCalls) * 100
	}
	sb.WriteString(fmt.Sprintf("\n### Reliability\n- Total calls: %d\n- Failed: %d\n- Success rate: %.1f%%\n", m.TotalCalls, m.FailedCalls, successRate))
	sb.WriteString(fmt.Sprintf("- Circuit breaker openings: %d\n", m.CBOpenedCount))
	if m.CBOpen {
		sb.WriteString(fmt.Sprintf("- Circuit breaker: OPEN (since %s)\n", m.CBLastOpened.Format(time.RFC3339)))
	} else {
		sb.WriteString("- Circuit breaker: closed\n")
	}
	sb.WriteString(fmt.Sprintf("- Auth refreshes: %d\n", m.AuthRefreshCount))

	// Source metrics
	sb.WriteString(fmt.Sprintf("\n### Sources\n- Total imported: %d\n", m.SourcesImported))
	if len(m.SourcesPerRun) > 0 {
		sum := 0
		for _, c := range m.SourcesPerRun {
			sum += c
		}
		sb.WriteString(fmt.Sprintf("- Avg per run: %.1f\n", float64(sum)/float64(len(m.SourcesPerRun))))
	}
	sb.WriteString(fmt.Sprintf("- Current source_count: %d (notebook: %s)\n", m.LastSourceCount, m.LastNotebookID))

	return sb.String()
}

func appendLatency(slice []time.Duration, d time.Duration) []time.Duration {
	slice = append(slice, d)
	if len(slice) > 20 {
		slice = slice[len(slice)-20:]
	}
	return slice
}

func avgLatency(slice []time.Duration) string {
	if len(slice) == 0 {
		return "no data"
	}
	var sum time.Duration
	for _, d := range slice {
		sum += d
	}
	return (sum / time.Duration(len(slice))).Truncate(time.Millisecond).String()
}

// ─── BT Action for metrics reporting ────────────────────────────────────────

// nlmMetricsReportAction injects the NotebookLM metrics summary into the blackboard result.
func nlmMetricsReportAction(ctx *btcore.BTContext[Blackboard]) int {
	bb := ctx.Blackboard
	bb.Result = nlmMetrics.Summary()
	return 1
}
