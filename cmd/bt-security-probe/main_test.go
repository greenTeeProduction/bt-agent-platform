package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunPassesAgainstHardenedDashboard(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
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
