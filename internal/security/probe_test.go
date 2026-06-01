package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeDashboard_PassesHardenedStack(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tasks/approve", func(w http.ResponseWriter, r *http.Request) {
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
		t.Fatalf("expected hardened stack to pass, got %#v", report.Checks)
	}
	if got := report.Summary(); !strings.Contains(got, "/") || !strings.Contains(got, "security probes passed") {
		t.Fatalf("unexpected summary: %q", got)
	}

	seenContentTypeProbe := false
	for _, check := range report.Checks {
		if check.Name == "mutating_without_json_rejected" {
			seenContentTypeProbe = true
			if check.Status != ProbePass {
				t.Fatalf("content-type probe should pass: %#v", check)
			}
		}
	}
	if !seenContentTypeProbe {
		t.Fatal("expected mutating_without_json_rejected check")
	}
}

func TestProbeDashboard_FailsMissingHardeningHeaders(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tasks/approve", func(w http.ResponseWriter, r *http.Request) {
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
	failedMutationCheck := false
	for _, check := range report.Checks {
		if strings.HasPrefix(check.Name, "header_") && check.Status == ProbeFail {
			failedHeaders++
		}
		if check.Name == "mutating_without_json_rejected" && check.Status == ProbeFail {
			failedMutationCheck = true
		}
	}
	if failedHeaders == 0 {
		t.Fatal("expected at least one failed hardening header check")
	}
	if !failedMutationCheck {
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
