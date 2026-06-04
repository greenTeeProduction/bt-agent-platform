package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProbeDashboard_PassesHardenedStack(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=()")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tasks/approve", func(w http.ResponseWriter, r *http.Request) {
		// CSRF check: POST with Content-Type but without CSRF token → 403
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "application/json" && r.Header.Get("X-CSRF-Token") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// Content-Type check: POST without Content-Type → 415
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()
		// Simulate rate limiting: after 5 requests, start returning 429
		if count > 5 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/dlq", func(w http.ResponseWriter, r *http.Request) {
		// Protected endpoint — returns 401 without API key
		if r.Header.Get("X-API-Key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		// Simulate login that sets session cookie with proper security attributes
		http.SetCookie(w, &http.Cookie{
			Name:     "bt_session",
			Value:    "test-session-token",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			Path:     "/",
		})
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeadersMiddleware(DefaultSecurityHeaders())(
		CrossOriginMiddleware("*", "GET, POST, OPTIONS")(
			JSONContentTypeMiddleware(mux),
		),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	report, err := ProbeDashboard(context.Background(), server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("ProbeDashboard returned error: %v", err)
	}
	if !report.Passed {
		t.Fatalf("expected hardened stack to pass, got failures: %#v", report.Checks)
	}
	if got := report.Summary(); !strings.Contains(got, "/") || !strings.Contains(got, "security probes passed") {
		t.Fatalf("unexpected summary: %q", got)
	}

	// Verify specific new probes are present
	seenChecks := map[string]bool{}
	for _, check := range report.Checks {
		seenChecks[check.Name] = true
	}
	requiredChecks := []string{
		"header_referrer-policy",
		"header_permissions-policy",
		"header_x-xss-protection",
		"header_strict-transport-security",
		"csrf_protection",
		"rate_limiting",
		"protected_endpoint_reachable",
		"session_cookie_attributes",
		"input_sanitization",
		"request_timeout_handling",
	}
	for _, name := range requiredChecks {
		if !seenChecks[name] {
			t.Errorf("missing required probe check: %s", name)
		}
	}

	// The content-type probe name
	seenContentTypeProbe := false
	for _, check := range report.Checks {
		if check.Name == "mutating_without_content_type_rejected" {
			seenContentTypeProbe = true
			if check.Status != ProbePass {
				t.Fatalf("content-type probe should pass: %#v", check)
			}
		}
	}
	if !seenContentTypeProbe {
		t.Fatal("expected mutating_without_content_type_rejected check")
	}
}

func TestProbeDashboard_FailsMissingHardeningHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tasks/approve", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	report, err := ProbeDashboard(context.Background(), server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("ProbeDashboard transport should succeed: %v", err)
	}
	if report.Passed {
		t.Fatalf("expected missing headers/plain mutating endpoint to fail: %#v", report.Checks)
	}

	failedHeaders := 0
	failedContentTypeCheck := false
	for _, check := range report.Checks {
		if strings.HasPrefix(check.Name, "header_") && check.Status == ProbeFail {
			failedHeaders++
		}
		if check.Name == "mutating_without_content_type_rejected" && check.Status == ProbeFail {
			failedContentTypeCheck = true
		}
	}
	if failedHeaders == 0 {
		t.Fatal("expected at least one failed hardening header check")
	}
	if !failedContentTypeCheck {
		t.Fatal("expected unprotected mutating endpoint check to fail")
	}
}

func TestProbeDashboard_RequiresTargetURL(t *testing.T) {
	report, err := ProbeDashboard(context.Background(), "", "", nil)
	if err == nil {
		t.Fatal("expected error for empty target URL")
	}
	if report.Passed || len(report.Checks) != 1 || report.Checks[0].Status != ProbeError {
		t.Fatalf("unexpected report for empty target: %#v", report)
	}
}

func TestProbeDashboard_CSRFProtection(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=()")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tasks/approve", func(w http.ResponseWriter, r *http.Request) {
		// Simulate CSRF enforcement: POST with Content-Type but without CSRF token → 403
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "application/json" && r.Header.Get("X-CSRF-Token") == "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		// Without Content-Type → 415
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/dlq", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	handler := SecurityHeadersMiddleware(DefaultSecurityHeaders())(
		CrossOriginMiddleware("*", "GET, POST, OPTIONS")(
			JSONContentTypeMiddleware(mux),
		),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	report, err := ProbeDashboard(context.Background(), server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("ProbeDashboard returned error: %v", err)
	}

	// Check CSRF probe result
	for _, check := range report.Checks {
		if check.Name == "csrf_protection" {
			if check.Status != ProbePass {
				t.Fatalf("expected CSRF probe to pass with enforcing server, got: %#v", check)
			}
			return
		}
	}
	t.Fatal("expected csrf_protection check to be present")
}

func TestProbeDashboard_WithAPIKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=()")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tasks/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Header.Get("Content-Type") == "" {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/summary", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/dlq", func(w http.ResponseWriter, r *http.Request) {
		// Valid API key → 200; no key → 401
		if r.Header.Get("X-API-Key") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/scalability", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := SecurityHeadersMiddleware(DefaultSecurityHeaders())(
		CrossOriginMiddleware("*", "GET, POST, OPTIONS")(
			JSONContentTypeMiddleware(mux),
		),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	// Test with API key
	client := &http.Client{Timeout: 10 * time.Second}
	report, err := ProbeDashboard(context.Background(), server.URL, "test-api-key-12345", client)
	if err != nil {
		t.Fatalf("ProbeDashboard returned error: %v", err)
	}

	// With API key, DLQ endpoint should be reachable (200 returned by our mock)
	seenDLQAuth := false
	for _, check := range report.Checks {
		if check.Name == "protected_endpoint_auth" {
			seenDLQAuth = true
			if check.Status == ProbeFail {
				t.Fatalf("expected DLQ auth check to pass with valid API key, got: %#v", check)
			}
		}
	}
	if !seenDLQAuth {
		t.Fatal("expected protected_endpoint_auth check to be present")
	}
}
