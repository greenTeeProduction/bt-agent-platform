package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRunPassesAgainstHardenedDashboard(t *testing.T) {
	var mu sync.Mutex
	reqCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health endpoint with full hardening headers
		if r.URL.Path == "/api/health" {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Content-Security-Policy", "default-src 'self'")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "camera=()")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		// Tasks approve — CSRF and Content-Type enforcement
		if r.URL.Path == "/api/tasks/approve" {
			if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
				w.WriteHeader(http.StatusUnsupportedMediaType)
				return
			}
			if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "application/json" && r.Header.Get("X-CSRF-Token") == "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		// Summary — used for rate limiting probe
		if r.URL.Path == "/api/summary" {
			mu.Lock()
			reqCount++
			count := reqCount
			mu.Unlock()
			if count > 5 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		// DLQ — protected endpoint
		if r.URL.Path == "/api/dlq" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	var out, errOut bytes.Buffer
	code := run([]string{"--target", ts.URL, "--json"}, &out, &errOut, ts.Client())
	if code != 0 {
		t.Fatalf("run() exit code = %d, stderr=%s, stdout=%s", code, errOut.String(), out.String())
	}
	if strings.Contains(out.String(), "BT dashboard security probe") {
		t.Fatalf("--json should suppress human summary, got %q", out.String())
	}
	if !strings.Contains(out.String(), `"passed": true`) {
		t.Fatalf("expected passing JSON report, got %s", out.String())
	}
}

func TestRunFailsWhenProbeFails(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	var out, errOut bytes.Buffer
	code := run([]string{"--target", ts.URL}, &out, &errOut, ts.Client())
	if code != 1 {
		t.Fatalf("run() exit code = %d, want 1; stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if !strings.Contains(out.String(), "BT dashboard security probe: FAIL") {
		t.Fatalf("expected failure summary, got %q", out.String())
	}
}

func TestRunRejectsInvalidTimeout(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--timeout", "0s"}, &out, &errOut, nil)
	if code != 2 {
		t.Fatalf("run() exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "timeout must be positive") {
		t.Fatalf("expected timeout error, got %q", errOut.String())
	}
}
