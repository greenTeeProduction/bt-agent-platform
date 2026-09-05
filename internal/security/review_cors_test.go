package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewCORSAllowsMutationSecurityHeaders(t *testing.T) {
	called := false
	handler := CrossOriginMiddleware("https://dashboard.example", "POST, OPTIONS")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	req := httptest.NewRequest(http.MethodOptions, "/api/sprint/execute", nil)
	req.Header.Set("Origin", "https://dashboard.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type,x-csrf-token,idempotency-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || called {
		t.Fatalf("preflight status=%d dispatched=%v", w.Code, called)
	}
	allowed := strings.ToLower(w.Header().Get("Access-Control-Allow-Headers"))
	for _, header := range []string{"x-csrf-token", "idempotency-key"} {
		if !strings.Contains(allowed, header) {
			t.Errorf("preflight does not allow %s: %s", header, allowed)
		}
	}
}
