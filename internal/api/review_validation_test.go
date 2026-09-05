package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewValidationCapturesCompleteBody(t *testing.T) {
	var log bytes.Buffer
	h := ResponseValidator(DashboardRoutes(), &ResponseValidatorConfig{Logger: slog.New(slog.NewTextHandler(&log, nil))})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":`))
		_, _ = w.Write([]byte(`"ok"}`))
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if log.Len() > 0 {
		t.Fatal(log.String())
	}
}
func TestReviewValidationReplacementHeaders(t *testing.T) {
	h := CompressionMiddleware(ResponseValidator(DashboardRoutes(), &ResponseValidatorConfig{Enforce: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "3")
		w.Header().Set("ETag", "old")
		_, _ = w.Write([]byte(`bad`))
	})))
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	if w.Header().Get("Content-Length") != "" || w.Header().Get("ETag") != "" {
		t.Errorf("stale representation headers: %v", w.Header())
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("replacement not marked gzip")
	}
	z, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	b, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "violates") {
		t.Fatal(string(b))
	}
}

func TestReviewDiagnosticsDiscoveryRequiresAuth(t *testing.T) {
	for _, route := range DashboardRoutes() {
		switch route.Path {
		case "/api/security/audit", "/api/config", "/api/alerts/rules":
			if !route.Auth || strings.Contains(route.Description, "no auth required") {
				t.Errorf("%s advertises unauthenticated diagnostics", route.Path)
			}
		}
	}
}
