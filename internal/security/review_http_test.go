package security

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewChunkedBodyLimit(t *testing.T) {
	called := false
	h := SanitizeMiddleware(16)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; _, _ = io.Copy(io.Discard, r.Body) }))
	r := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(strings.Repeat("x", 100)))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 413 || called {
		t.Fatalf("chunked body reached handler=%v code=%d", called, w.Code)
	}
}
func TestReviewRateLimitClientIP(t *testing.T) {
	rl := NewRateLimiter(0, 1)
	h := RateLimitMiddleware(rl, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i := range 2 {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = fmt.Sprintf("[2001:db8::1]:%d", 1000+i)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		want := 204
		if i == 1 {
			want = 429
		}
		if w.Code != want {
			t.Errorf("connection %d code=%d want=%d", i, w.Code, want)
		}
	}
}
func TestReviewRateLimitCardinalityBound(t *testing.T) {
	rl := NewRateLimiter(0, 1)
	for i := range 11000 {
		rl.Allow(fmt.Sprint(i))
	}
	if len(rl.buckets) > 10000 {
		t.Errorf("unbounded buckets: %d", len(rl.buckets))
	}
}
func TestReviewHTTPServerDeadlines(t *testing.T) {
	s := NewHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if s.ReadHeaderTimeout <= 0 || s.ReadTimeout <= 0 || s.WriteTimeout <= 0 || s.IdleTimeout <= 0 {
		t.Fatal("server has unbounded network waits")
	}
}
func TestReviewSessionKeyMatching(t *testing.T) {
	for _, tc := range []struct {
		provided, expected string
		want               bool
	}{{"", "", false}, {"key", "", false}, {"", "key", false}, {"key", "key", true}, {"keyX", "key", false}, {"kez", "key", false}} {
		if got := MatchAPIKey(tc.provided, tc.expected); got != tc.want {
			t.Errorf("key comparison returned %v want %v", got, tc.want)
		}
	}
}
