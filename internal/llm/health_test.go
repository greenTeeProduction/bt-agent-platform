package llm

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthStateRecordSuccess(t *testing.T) {
	h := &HealthState{}

	h.recordSuccess(100)
	if h.Status() != HealthOK {
		t.Errorf("expected HealthOK, got %v", h.Status())
	}
	if h.ConsecutiveOK() != 1 {
		t.Errorf("expected consecutiveOK=1, got %d", h.ConsecutiveOK())
	}
	if h.ConsecutiveFail() != 0 {
		t.Errorf("expected consecutiveFail=0, got %d", h.ConsecutiveFail())
	}
	if h.LastError() != nil {
		t.Errorf("expected nil error, got %v", h.LastError())
	}
}

func TestHealthStateRecordDegraded(t *testing.T) {
	h := &HealthState{}

	h.recordSuccess(6000) // >5s = degraded
	if h.Status() != HealthDegraded {
		t.Errorf("expected HealthDegraded, got %v", h.Status())
	}
}

func TestHealthStateRecordFailure(t *testing.T) {
	h := &HealthState{}

	// One failure shouldn't trigger unhealthy.
	h.recordFailure(errTest("boom"))
	if h.ConsecutiveFail() != 1 {
		t.Errorf("expected consecutiveFail=1, got %d", h.ConsecutiveFail())
	}

	// After 3 consecutive failures, goes unhealthy.
	h.recordFailure(errTest("boom2"))
	h.recordFailure(errTest("boom3"))
	if h.Status() != HealthUnhealthy {
		t.Errorf("expected HealthUnhealthy after 3 consecutive fails, got %v", h.Status())
	}

	// Recovery resets consecutive counters.
	h.recordSuccess(50)
	if h.ConsecutiveFail() != 0 {
		t.Errorf("expected consecutiveFail=0 after recovery, got %d", h.ConsecutiveFail())
	}
	if h.ConsecutiveOK() != 1 {
		t.Errorf("expected consecutiveOK=1 after recovery, got %d", h.ConsecutiveOK())
	}
}

func TestHealthStateSnapshot(t *testing.T) {
	h := &HealthState{}
	h.recordSuccess(123)

	snap := h.Snapshot()
	if snap["status"] != "healthy" {
		t.Errorf("expected healthy, got %v", snap["status"])
	}
	if snap["latency_ms"] != int64(123) {
		t.Errorf("expected latency_ms=123, got %v", snap["latency_ms"])
	}
	if snap["consecutive_ok"] != 1 {
		t.Errorf("expected consecutive_ok=1, got %v", snap["consecutive_ok"])
	}
}

func TestHealthStateIsHealthy(t *testing.T) {
	h := &HealthState{}

	// Unknown: IsHealthy returns false.
	if h.IsHealthy() {
		t.Error("unknown state should not be healthy")
	}

	h.recordSuccess(50)
	if !h.IsHealthy() {
		t.Error("healthy state should be healthy")
	}

	h.recordSuccess(10000) // degraded
	if !h.IsHealthy() {
		t.Error("degraded state should still be healthy")
	}

	h.recordFailure(errTest("err"))
	h.recordFailure(errTest("err"))
	h.recordFailure(errTest("err"))
	if h.IsHealthy() {
		t.Error("unhealthy state should not be healthy")
	}
}

func TestHealthMonitorProbe(t *testing.T) {
	// Start a test HTTP server that mimics Ollama's /api/tags endpoint.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3.6:35b-a3b"}]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	m := NewHealthMonitor(server.URL, 0) // no auto-probe
	m.Probe()

	if m.State().Status() != HealthOK {
		t.Errorf("expected HealthOK, got %v", m.State().Status())
	}
	if !m.IsHealthy() {
		t.Error("expected healthy after successful probe")
	}
}

func TestHealthMonitorProbeFailure(t *testing.T) {
	// Simulate a server that returns 500.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	m := NewHealthMonitor(server.URL, 0)
	m.Probe()

	if m.IsHealthy() {
		t.Error("expected unhealthy after failed probe")
	}
}

func TestHealthMonitorProbeUnreachable(t *testing.T) {
	// Use an unreachable address.
	m := NewHealthMonitor("http://127.0.0.1:19999", 0)
	m.Probe()

	if m.IsHealthy() {
		t.Error("expected unhealthy for unreachable server")
	}
	if m.State().ConsecutiveFail() != 1 {
		t.Errorf("expected 1 failure, got %d", m.State().ConsecutiveFail())
	}
}

func TestHealthMonitorDegradationError(t *testing.T) {
	m := NewHealthMonitor("http://127.0.0.1:19999", 0)
	m.Probe()
	// It should be unhealthy now.
	err := m.DegradationError("bt_run_task")
	if err == nil {
		t.Error("expected degradation error for unhealthy LLM")
	}
}

func TestHealthMonitorNilSafety(t *testing.T) {
	var m *HealthMonitor
	if !m.IsHealthy() {
		t.Error("nil HealthMonitor should default to healthy")
	}
	err := m.DegradationError("some_tool")
	if err != nil {
		t.Errorf("nil HealthMonitor should return nil error, got %v", err)
	}
}

func TestHealthMonitorStopStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Start auto-probing with a short interval.
	m := NewHealthMonitor(server.URL, 100*time.Millisecond)
	m.Start()

	// Wait for at least one probe.
	time.Sleep(200 * time.Millisecond)

	if !m.IsHealthy() {
		t.Error("expected healthy after auto-probe")
	}

	m.Stop()
	time.Sleep(200 * time.Millisecond)

	// Should still be healthy — we just stopped probing, didn't change state.
	if !m.IsHealthy() {
		t.Error("expected still healthy after stop")
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		status HealthStatus
		want   string
	}{
		{HealthUnknown, "unknown"},
		{HealthOK, "healthy"},
		{HealthDegraded, "degraded"},
		{HealthUnhealthy, "unhealthy"},
		{HealthStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("HealthStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestHealthMonitorStartDisabled(t *testing.T) {
	// Interval of 0 should not start auto-probing.
	m := NewHealthMonitor("http://localhost:11434", 0)
	m.Start()
	// After a short wait, last check should still be zero.
	time.Sleep(50 * time.Millisecond)
	if !m.State().LastCheck().IsZero() {
		t.Error("expected no probe when interval=0")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
