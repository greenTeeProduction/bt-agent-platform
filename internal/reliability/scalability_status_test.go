package reliability

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestScalabilityStatus_Empty(t *testing.T) {
	s := NewScalabilityStatus(nil, nil, 0, 0, 0, 0, nil)
	if s.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if s.WorkerPool != nil {
		t.Error("expected nil WorkerPool when no pool provided")
	}
	if s.ConcurrencyLimiter != nil {
		t.Error("expected nil ConcurrencyLimiter when no limiter provided")
	}
	if s.Queue != nil {
		t.Error("expected nil Queue when pending=0 and maxLen=0")
	}
	if s.Router != nil {
		t.Error("expected nil Router when routerTotal=0")
	}
}

func TestScalabilityStatus_WithWorkerPool(t *testing.T) {
	wp := NewWorkerPool(2)

	s := NewScalabilityStatus(wp, nil, 0, 0, 0, 0, nil)
	if s.WorkerPool == nil {
		t.Fatal("expected WorkerPool stats")
	}
	if s.WorkerPool.Workers != 2 {
		t.Errorf("expected Workers=2, got %d", s.WorkerPool.Workers)
	}
	if s.WorkerPool.Active != 0 {
		t.Errorf("expected Active=0, got %d", s.WorkerPool.Active)
	}
	if s.WorkerPool.Total != 0 {
		t.Errorf("expected Total=0, got %d", s.WorkerPool.Total)
	}
	if s.WorkerPool.Completed != 0 {
		t.Errorf("expected Completed=0, got %d", s.WorkerPool.Completed)
	}

	wp.Shutdown()
}

func TestScalabilityStatus_WithConcurrencyLimiter(t *testing.T) {
	cl := NewConcurrencyLimiter(5)

	s := NewScalabilityStatus(nil, cl, 0, 0, 0, 0, nil)
	if s.ConcurrencyLimiter == nil {
		t.Fatal("expected ConcurrencyLimiter stats")
	}
	if s.ConcurrencyLimiter.Capacity != 5 {
		t.Errorf("expected Capacity=5, got %d", s.ConcurrencyLimiter.Capacity)
	}
	if s.ConcurrencyLimiter.Available != 5 {
		t.Errorf("expected Available=5, got %d", s.ConcurrencyLimiter.Available)
	}
	if s.ConcurrencyLimiter.Active != 0 {
		t.Errorf("expected Active=0, got %d", s.ConcurrencyLimiter.Active)
	}
}

func TestScalabilityStatus_WithQueue(t *testing.T) {
	// pending > 0 should populate Queue stats
	s := NewScalabilityStatus(nil, nil, 42, 1000, 0, 0, nil)
	if s.Queue == nil {
		t.Fatal("expected Queue stats when pending=42")
	}
	if s.Queue.Pending != 42 {
		t.Errorf("expected Pending=42, got %d", s.Queue.Pending)
	}
	if s.Queue.MaxLen != 1000 {
		t.Errorf("expected MaxLen=1000, got %d", s.Queue.MaxLen)
	}
}

func TestScalabilityStatus_WithRouter(t *testing.T) {
	s := NewScalabilityStatus(nil, nil, 0, 0, 3, 2, nil)
	if s.Router == nil {
		t.Fatal("expected Router stats when routerTotal=3")
	}
	if s.Router.Total != 3 {
		t.Errorf("expected Total=3, got %d", s.Router.Total)
	}
	if s.Router.Healthy != 2 {
		t.Errorf("expected Healthy=2, got %d", s.Router.Healthy)
	}
	if s.Router.Unhealthy != 1 {
		t.Errorf("expected Unhealthy=1, got %d", s.Router.Unhealthy)
	}
}
func TestScalabilityStatus_AllComponents(t *testing.T) {
	wp := NewWorkerPool(4)
	defer wp.Shutdown()
	cl := NewConcurrencyLimiter(10)
	cp := NewConnPool(ConnPoolConfig{})

	s := NewScalabilityStatus(wp, cl, 7, 500, 4, 3, cp)

	if s.WorkerPool == nil || s.ConcurrencyLimiter == nil ||
		s.Queue == nil || s.Router == nil || s.ConnPool == nil {
		t.Error("expected all five components populated")
	}

	if s.WorkerPool.Workers != 4 {
		t.Errorf("expected Workers=4, got %d", s.WorkerPool.Workers)
	}
	if s.ConcurrencyLimiter.Capacity != 10 {
		t.Errorf("expected Capacity=10, got %d", s.ConcurrencyLimiter.Capacity)
	}
	if s.Queue.Pending != 7 {
		t.Errorf("expected Pending=7, got %d", s.Queue.Pending)
	}
	if s.Router.Total != 4 {
		t.Errorf("expected Total=4, got %d", s.Router.Total)
	}
	if s.ConnPool.MaxObserved < 0 {
		t.Errorf("expected non-negative MaxObserved, got %d", s.ConnPool.MaxObserved)
	}
	if s.ConnPool.Created < 0 {
		t.Errorf("expected non-negative Created, got %d", s.ConnPool.Created)
	}
}

func TestScalabilityStatus_JSONRoundTrip(t *testing.T) {
	wp := NewWorkerPool(3)
	defer wp.Shutdown()
	cl := NewConcurrencyLimiter(8)
	cp := NewConnPool(ConnPoolConfig{})

	s := NewScalabilityStatus(wp, cl, 5, 200, 2, 2, cp)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	var decoded ScalabilityStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if decoded.WorkerPool == nil || decoded.WorkerPool.Workers != 3 {
		t.Error("WorkerPool stats lost in roundtrip")
	}
	if decoded.ConcurrencyLimiter == nil || decoded.ConcurrencyLimiter.Capacity != 8 {
		t.Error("ConcurrencyLimiter stats lost in roundtrip")
	}
	if decoded.Queue == nil || decoded.Queue.Pending != 5 {
		t.Error("Queue stats lost in roundtrip")
	}
	if decoded.Router == nil || decoded.Router.Healthy != 2 {
		t.Error("Router stats lost in roundtrip")
	}
	if decoded.ConnPool == nil || decoded.ConnPool.MaxIdle <= 0 {
		t.Error("ConnPool stats lost in roundtrip")
	}
	if decoded.Timestamp.IsZero() {
		t.Error("Timestamp lost in roundtrip")
	}
}

func TestScalabilityStatus_HTTPHandler(t *testing.T) {
	wp := NewWorkerPool(2)
	defer wp.Shutdown()

	handler := HTTPHandler(wp, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/scalability", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var status ScalabilityStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("JSON decode failed: %v", err)
	}

	if status.WorkerPool == nil || status.WorkerPool.Workers != 2 {
		t.Error("expected WorkerPool stats in HTTP response")
	}
}

func TestScalabilityStatus_HTTPHandler_MethodNotAllowed(t *testing.T) {
	handler := HTTPHandler(nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/scalability", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

func TestWorkerPool_Workers(t *testing.T) {
	wp := NewWorkerPool(7)
	defer wp.Shutdown()

	if wp.Workers() != 7 {
		t.Errorf("expected Workers=7, got %d", wp.Workers())
	}
}

func TestScalabilityStatus_QueueMaxLenZero(t *testing.T) {
	// maxLen=0 with pending>0 should still populate queue
	s := NewScalabilityStatus(nil, nil, 3, 0, 0, 0, nil)
	if s.Queue == nil {
		t.Error("expected Queue when pending=3 even with maxLen=0")
	}
	if s.Queue.Pending != 3 {
		t.Errorf("expected Pending=3, got %d", s.Queue.Pending)
	}
}
