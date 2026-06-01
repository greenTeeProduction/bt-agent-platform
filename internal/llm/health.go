package llm

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// HealthStatus represents the current health state of the LLM provider.
type HealthStatus int

const (
	// HealthUnknown means health hasn't been checked yet.
	HealthUnknown HealthStatus = iota
	// HealthOK means the LLM provider is responding normally.
	HealthOK
	// HealthDegraded means the LLM provider is slow but responding.
	HealthDegraded
	// HealthUnhealthy means the LLM provider is unreachable.
	HealthUnhealthy
)

func (s HealthStatus) String() string {
	switch s {
	case HealthUnknown:
		return "unknown"
	case HealthOK:
		return "healthy"
	case HealthDegraded:
		return "degraded"
	case HealthUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// HealthState tracks the current LLM health with concurrency safety.
type HealthState struct {
	mu              sync.RWMutex
	status          HealthStatus
	lastCheck       time.Time
	lastError       error
	latencyMs       int64
	consecutiveOK   int
	consecutiveFail int
}

// Status returns the current health status.
func (h *HealthState) Status() HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

// IsHealthy returns true when the LLM is available (healthy or degraded).
func (h *HealthState) IsHealthy() bool {
	s := h.Status()
	return s == HealthOK || s == HealthDegraded
}

// LastCheck returns the time of the last health check.
func (h *HealthState) LastCheck() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastCheck
}

// LastError returns the most recent error (nil if healthy).
func (h *HealthState) LastError() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.lastError
}

// LatencyMs returns the latency of the last successful check in ms.
func (h *HealthState) LatencyMs() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.latencyMs
}

// ConsecutiveOK returns the count of consecutive successful checks.
func (h *HealthState) ConsecutiveOK() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.consecutiveOK
}

// ConsecutiveFail returns the count of consecutive failed checks.
func (h *HealthState) ConsecutiveFail() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.consecutiveFail
}

// Snapshot returns a copy of the current health state for reporting.
func (h *HealthState) Snapshot() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return map[string]interface{}{
		"status":           h.status.String(),
		"last_check":       h.lastCheck.Format(time.RFC3339),
		"latency_ms":       h.latencyMs,
		"consecutive_ok":   h.consecutiveOK,
		"consecutive_fail": h.consecutiveFail,
		"error":            errToString(h.lastError),
	}
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// recordSuccess records a successful health check.
func (h *HealthState) recordSuccess(latencyMs int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastCheck = time.Now()
	h.lastError = nil
	h.latencyMs = latencyMs
	h.consecutiveOK++
	h.consecutiveFail = 0

	if latencyMs > 5000 {
		h.status = HealthDegraded
	} else {
		h.status = HealthOK
	}

	slog.Debug("llm health check OK",
		"latency_ms", latencyMs,
		"status", h.status.String(),
		"consecutive_ok", h.consecutiveOK,
	)
}

// recordFailure records a failed health check.
func (h *HealthState) recordFailure(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastCheck = time.Now()
	h.lastError = err
	h.consecutiveFail++
	h.consecutiveOK = 0

	if h.consecutiveFail >= 3 {
		h.status = HealthUnhealthy
	} else if h.consecutiveFail >= 2 {
		h.status = HealthDegraded
	}

	slog.Warn("llm health check FAIL",
		"error", err,
		"consecutive_fail", h.consecutiveFail,
		"status", h.status.String(),
	)
}

// HealthMonitor periodically probes LLM health and tracks state transitions.
type HealthMonitor struct {
	state     *HealthState
	serverURL string
	interval  time.Duration
	stopCh    chan struct{}
	stopped   bool
	mu        sync.Mutex
}

// NewHealthMonitor creates a health monitor for the given Ollama server URL.
// interval is how often to probe (e.g., 30s). Set to 0 to disable auto-probing
// (call Probe() manually).
func NewHealthMonitor(serverURL string, interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		state:     &HealthState{status: HealthUnknown},
		serverURL: serverURL,
		interval:  interval,
		stopCh:    make(chan struct{}),
	}
}

// Start begins periodic health probing in a background goroutine.
func (m *HealthMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.interval <= 0 || m.stopped {
		return
	}

	// Run an immediate probe on start.
	go func() {
		m.Probe()
	}()

	go func() {
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.Probe()
			case <-m.stopCh:
				return
			}
		}
	}()

	slog.Info("llm health monitor started",
		"server", m.serverURL,
		"interval", m.interval.String(),
	)
}

// Stop halts the periodic health probing.
func (m *HealthMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
		slog.Info("llm health monitor stopped")
	}
}

// Probe performs a single health check against the Ollama API.
// Returns true if the service is reachable.
func (m *HealthMonitor) Probe() bool {
	start := time.Now()

	// Check the Ollama root endpoint — fast, no model loading needed.
	url := m.serverURL
	if url == "" {
		url = "http://localhost:11434"
	}
	// Strip trailing slash for consistent URL joining.
	if url[len(url)-1] == '/' {
		url = url[:len(url)-1]
	}
	checkURL := url + "/api/tags"

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(checkURL)
	if err != nil {
		m.state.recordFailure(fmt.Errorf("ollama unreachable: %w", err))
		return false
	}
	resp.Body.Close()
	elapsed := time.Since(start).Milliseconds()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		m.state.recordSuccess(elapsed)
		return true
	}

	m.state.recordFailure(fmt.Errorf("ollama returned status %d", resp.StatusCode))
	return false
}

// State returns the underlying HealthState for querying.
func (m *HealthMonitor) State() *HealthState {
	return m.state
}

// IsHealthy is a convenience wrapper around State().IsHealthy().
func (m *HealthMonitor) IsHealthy() bool {
	if m == nil {
		return true // no monitor = assume healthy (graceful degradation not configured)
	}
	return m.state.IsHealthy()
}

// DegradationError returns a user-facing error when LLM is unavailable.
// Use this in tool handlers that depend on LLM to fail fast with a clear message.
func (m *HealthMonitor) DegradationError(toolName string) error {
	if m == nil || m.state.IsHealthy() {
		return nil
	}
	err := m.state.LastError()
	errMsg := "LLM provider is currently unavailable"
	if err != nil {
		errMsg = fmt.Sprintf("LLM provider is currently unavailable: %v", err)
	}
	return fmt.Errorf("%s (%s): %s", toolName, m.state.Status().String(), errMsg)
}
