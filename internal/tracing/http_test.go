package tracing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTracingMiddleware_BasicSpan(t *testing.T) {
	tracer, output := TestTracer("http")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	var handlerCalled bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		// Verify context contains a span
		if span := SpanFromContext(r.Context()); span == nil {
			t.Error("expected span in request context, got nil")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatal("handler was not called")
	}

	out := output()
	if !strings.Contains(out, "op=http:GET /api/health") {
		t.Errorf("expected op=http:GET /api/health in output: %s", out)
	}
	if !strings.Contains(out, "http.method=GET") {
		t.Errorf("expected http.method=GET in output: %s", out)
	}
	if !strings.Contains(out, "http.url=/api/health") {
		t.Errorf("expected http.url=/api/health in output: %s", out)
	}
	if !strings.Contains(out, "http.status_code=200") {
		t.Errorf("expected http.status_code=200 in output: %s", out)
	}
	if !strings.Contains(out, "http.duration_ms") {
		t.Errorf("expected http.duration_ms in output: %s", out)
	}
}

func TestTracingMiddleware_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer, output := TestTracer("http-status")
			SetGlobalTracer(tracer)
			defer SetGlobalTracer(noopTracer{})

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			mw := TracingMiddleware(handler)
			req := httptest.NewRequest("POST", "/api/tasks", nil)
			rec := httptest.NewRecorder()
			mw.ServeHTTP(rec, req)

			if rec.Code != tt.statusCode {
				t.Errorf("expected status %d, got %d", tt.statusCode, rec.Code)
			}

			out := output()
			expectedStatus := IntAttr("http.status_code", tt.statusCode).Value
			if !strings.Contains(out, "http.status_code="+expectedStatus) {
				t.Errorf("expected http.status_code=%s in output: %s", expectedStatus, out)
			}
		})
	}
}

func TestTracingMiddleware_SlowRequest(t *testing.T) {
	tracer, output := TestTracer("http-slow")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)

	// Simulate a slow request by using a handler that sleeps.
	// But we want the span to think it took >5s, so we sleep in the test handler.
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	})
	mw = TracingMiddleware(slowHandler)

	req := httptest.NewRequest("GET", "/api/thinktank/analyze", nil)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		mw.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler timed out")
	}

	out := output()
	if !strings.Contains(out, "[slow_request") {
		t.Errorf("expected [slow_request] event in output: %s", out)
	}
	if !strings.Contains(out, "threshold=5s") {
		t.Errorf("expected threshold=5s in slow_request event: %s", out)
	}
}

func TestTracingMiddleware_NoopTracerSafe(t *testing.T) {
	// Default global tracer is noop — middleware should not panic
	SetGlobalTracer(noopTracer{})
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/summary", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	mw.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestTracingMiddleware_FlushSupport(t *testing.T) {
	tracer, _ := TestTracer("http-flush")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		} else {
			t.Error("expected ResponseWriter to implement http.Flusher")
		}
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/api/metrics/live", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
}

func TestTracingMiddleware_DifferentMethods(t *testing.T) {
	tracer, output := TestTracer("http-methods")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	methods := []string{"GET", "POST", "PUT", "DELETE"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/api/agents", nil)
		rec := httptest.NewRecorder()
		TracingMiddleware(handler).ServeHTTP(rec, req)
	}

	out := output()
	for _, method := range methods {
		if !strings.Contains(out, "http.method="+method) {
			t.Errorf("expected http.method=%s in output: %s", method, out)
		}
	}
}

func TestTracingMiddleware_Implicit200(t *testing.T) {
	tracer, output := TestTracer("http-implicit")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	// Handler that writes without calling WriteHeader should get 200
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	out := output()
	if !strings.Contains(out, "http.status_code=200") {
		t.Errorf("expected implicit http.status_code=200: %s", out)
	}
}

func TestTracingMiddleware_WroteHeaderGuard(t *testing.T) {
	// Test that WriteHeader is idempotent (responseWriter.WriteHeader guard)
	tracer, output := TestTracer("http-guard")
	SetGlobalTracer(tracer)
	defer SetGlobalTracer(noopTracer{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.WriteHeader(http.StatusOK) // Should be ignored
	})

	mw := TracingMiddleware(handler)
	req := httptest.NewRequest("GET", "/missing", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	out := output()
	if !strings.Contains(out, "http.status_code=404") {
		t.Errorf("expected http.status_code=404 (first WriteHeader wins): %s", out)
	}
	if strings.Contains(out, "http.status_code=200") {
		t.Error("should not contain second WriteHeader status code")
	}
}
