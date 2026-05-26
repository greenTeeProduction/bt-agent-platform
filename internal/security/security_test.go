package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRateLimiter_Basic(t *testing.T) {
	rl := NewRateLimiter(10, 5) // 10/sec, burst 5
	key := "test-client"

	// Should allow burst of 5
	for i := 0; i < 5; i++ {
		if !rl.Allow(key) {
			t.Errorf("request %d should be allowed (burst)", i+1)
		}
	}

	// 6th should be denied
	if rl.Allow(key) {
		t.Error("6th request should be denied (burst exhausted)")
	}

	// After waiting, should allow again
	time.Sleep(150 * time.Millisecond)
	if !rl.Allow(key) {
		t.Error("request after wait should be allowed")
	}
}

func TestRateLimiter_PerClientIsolation(t *testing.T) {
	rl := NewRateLimiter(1, 1)
	if !rl.Allow("client-a") {
		t.Error("client-a first request should be allowed")
	}
	if rl.Allow("client-a") {
		t.Error("client-a second request should be denied")
	}
	if !rl.Allow("client-b") {
		t.Error("client-b should have separate bucket")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(100, 100)
	handler := RateLimitMiddleware(rl, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimitMiddleware_Denied(t *testing.T) {
	rl := NewRateLimiter(0, 0) // zero rate, zero burst — all requests denied after first
	handler := RateLimitMiddleware(rl, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request: burst=0 gives 0-1 = -1 tokens, denied
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		// With zero rate and burst, even first request may be denied
		// But if the first somehow gets through, second MUST be denied
		rec2 := httptest.NewRecorder()
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.RemoteAddr = "10.0.0.1:1234"
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429 on second request, got %d (first got %d)", rec2.Code, rec.Code)
		}
	}
}

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"hello\x00world", "helloworld"},
		{"  test  ", "test"},
		{"normal task", "normal task"},
		{"task with \x1b[31mcolor\x1b[0m codes", "task with color codes"},
	}

	for _, tt := range tests {
		got := SanitizeInput(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeString(t *testing.T) {
	if got := sanitizeString("abc\x00def"); got != "abcdef" {
		t.Errorf("expected 'abcdef', got %q", got)
	}
	if got := sanitizeString("hello\x01world"); got != "helloworld" {
		t.Errorf("expected 'helloworld', got %q", got)
	}
}

func TestSanitizeMiddleware(t *testing.T) {
	handler := SanitizeMiddleware(1024)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test with clean request
	req := httptest.NewRequest("POST", "/test?q=hello+world", strings.NewReader("test"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestSanitizeMiddleware_BodyTooLarge(t *testing.T) {
	handler := SanitizeMiddleware(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", strings.NewReader("this is way too long for 10 bytes"))
	req.ContentLength = 100
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d", rec.Code)
	}
}
