package engine

import (
	"fmt"
	"sync"
	"time"
)

// SLOMetrics tracks Service Level Objectives for an agent.
type SLOMetrics struct {
	mu sync.RWMutex

	TotalCalls      int64
	SuccessfulCalls int64
	FailedCalls     int64
	RecoveredCalls  int64

	// DeferredCalls counts graceful, expected pauses (e.g. a Claude rate-limit
	// carryover) that the scheduler treats as terminal but must NOT fold into
	// success or failure. Deferrals deliberately leave TotalCalls,
	// SuccessfulCalls, FailedCalls and the latency totals untouched so a backoff
	// never inflates the success rate or success-latency the gardener's
	// validation gate reads.
	DeferredCalls int64

	TotalLatencyMs int64
	MaxLatencyMs   int64

	AgentName string
	TreeName  string
}

func (m *SLOMetrics) SuccessRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.successRateLocked()
}

// successRateLocked requires m.mu held (read or write).
func (m *SLOMetrics) successRateLocked() float64 {
	if m.TotalCalls == 0 {
		return 1.0
	}
	return float64(m.SuccessfulCalls) / float64(m.TotalCalls)
}

func (m *SLOMetrics) RecoveryRate() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.recoveryRateLocked()
}

// recoveryRateLocked requires m.mu held (read or write).
func (m *SLOMetrics) recoveryRateLocked() float64 {
	if m.FailedCalls == 0 {
		return 0
	}
	return float64(m.RecoveredCalls) / float64(m.FailedCalls)
}

func (m *SLOMetrics) AvgLatencyMs() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.avgLatencyMsLocked()
}

// avgLatencyMsLocked requires m.mu held (read or write).
func (m *SLOMetrics) avgLatencyMsLocked() float64 {
	if m.TotalCalls == 0 {
		return 0
	}
	return float64(m.TotalLatencyMs) / float64(m.TotalCalls)
}

func (m *SLOMetrics) RecordSuccess(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCalls++
	m.SuccessfulCalls++
	ms := latency.Milliseconds()
	m.TotalLatencyMs += ms
	if ms > m.MaxLatencyMs {
		m.MaxLatencyMs = ms
	}
}

func (m *SLOMetrics) RecordFailure(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCalls++
	m.FailedCalls++
	ms := latency.Milliseconds()
	m.TotalLatencyMs += ms
}

// RecordDeferred records a graceful, terminal pause (e.g. a rate-limit
// carryover). It increments only DeferredCalls: success/failure counters and
// the latency totals stay untouched so the pause never skews success rate or
// success-latency.
func (m *SLOMetrics) RecordDeferred() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeferredCalls++
}

func (m *SLOMetrics) RecordRecovery(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RecoveredCalls++
	ms := latency.Milliseconds()
	m.TotalLatencyMs += ms
}

func (m *SLOMetrics) Summary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fmt.Sprintf("[%s/%s] success=%.1f%% recovery=%.1f%% avg_latency=%.0fms calls=%d deferred=%d",
		m.AgentName, m.TreeName,
		m.successRateLocked()*100, m.recoveryRateLocked()*100,
		m.avgLatencyMsLocked(), m.TotalCalls, m.DeferredCalls)
}

// sloRegistry stores per-agent SLO metrics.
var sloRegistry = &sync.Map{}

func GetSLOMetrics(agentName, treeName string) *SLOMetrics {
	key := agentName + ":" + treeName
	if val, ok := sloRegistry.Load(key); ok {
		return val.(*SLOMetrics)
	}
	m := &SLOMetrics{AgentName: agentName, TreeName: treeName}
	actual, _ := sloRegistry.LoadOrStore(key, m)
	return actual.(*SLOMetrics)
}

// AllSLOMetrics returns all registered SLO metrics.
func AllSLOMetrics() map[string]*SLOMetrics {
	result := make(map[string]*SLOMetrics)
	sloRegistry.Range(func(key, value any) bool {
		result[key.(string)] = value.(*SLOMetrics)
		return true
	})
	return result
}
