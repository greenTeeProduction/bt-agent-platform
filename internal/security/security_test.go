package security

import (
	"context"
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

func TestSecurityHeadersMiddleware(t *testing.T) {
	cfg := DefaultSecurityHeaders()
	handler := SecurityHeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	h := rec.Header()
	tests := []struct{ header, expected string }{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
		{"Cache-Control", "no-store, max-age=0"},
	}
	for _, tt := range tests {
		if got := h.Get(tt.header); got != tt.expected {
			t.Errorf("%s = %q, want %q", tt.header, got, tt.expected)
		}
	}

	// CSP should be present
	if csp := h.Get("Content-Security-Policy"); csp == "" {
		t.Error("Content-Security-Policy should be set")
	}
	// Permissions-Policy should be present
	if pp := h.Get("Permissions-Policy"); pp == "" {
		t.Error("Permissions-Policy should be set")
	}
	// HSTS should NOT be set by default
	if h.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS should not be set with defaults")
	}
}

func TestSecurityHeadersMiddleware_HSTS(t *testing.T) {
	cfg := SecurityHeadersConfig{
		EnableHSTS:     true,
		HSTSMaxAge:     31536000,
		HSTSIncludeSub: true,
	}
	handler := SecurityHeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	hsts := rec.Header().Get("Strict-Transport-Security")
	if hsts == "" {
		t.Error("HSTS should be set when enabled")
	}
	if !strings.Contains(hsts, "max-age=31536000") {
		t.Errorf("HSTS should include max-age, got %q", hsts)
	}
	if !strings.Contains(hsts, "includeSubDomains") {
		t.Errorf("HSTS should include subdomains, got %q", hsts)
	}
}

func TestSecurityHeadersMiddleware_CustomConfig(t *testing.T) {
	cfg := SecurityHeadersConfig{
		FrameOptions:     "SAMEORIGIN",
		ReferrerPolicy:   "no-referrer",
		CSP:              "default-src 'none'",
	}
	handler := SecurityHeadersMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q, want SAMEORIGIN", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'none'" {
		t.Errorf("CSP = %q, want 'default-src 'none''", got)
	}
	if rec.Header().Get("Permissions-Policy") != "" {
		t.Error("Permissions-Policy should be empty when not configured")
	}
}

func TestCrossOriginMiddleware(t *testing.T) {
	handler := CrossOriginMiddleware("*", "GET, POST")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("ACAM = %q, want GET, POST", got)
	}
}

func TestCrossOriginMiddleware_Preflight(t *testing.T) {
	handler := CrossOriginMiddleware("https://example.com", "")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for OPTIONS preflight")
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("ACAO = %q, want https://example.com", got)
	}
}

func TestRequestTimeoutMiddleware(t *testing.T) {
	handler := RequestTimeoutMiddleware(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("fast handler should return 200, got %d", rec.Code)
	}
}

func TestRequestTimeoutMiddleware_Timeout(t *testing.T) {
	handler := RequestTimeoutMiddleware(10 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // exceeds timeout
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("slow handler should return 504, got %d", rec.Code)
	}
}

// ─── IP Filter Tests ──────────────────────────────────────────────────────

func TestIPFilter_Allowlist(t *testing.T) {
	f := NewIPFilter(FilterAllowlist, "10.0.0.1", "192.168.0.0/24")

	if !f.Allowed("10.0.0.1") {
		t.Error("10.0.0.1 should be allowed (exact match in allowlist)")
	}
	if !f.Allowed("192.168.0.50") {
		t.Error("192.168.0.50 should be allowed (in CIDR range)")
	}
	if f.Allowed("172.16.0.1") {
		t.Error("172.16.0.1 should be denied (not in allowlist)")
	}
	if f.Allowed("192.168.1.1") {
		t.Error("192.168.1.1 should be denied (outside CIDR range)")
	}
}

func TestIPFilter_Blocklist(t *testing.T) {
	f := NewIPFilter(FilterBlocklist, "10.0.0.99", "172.16.0.0/16")

	if f.Allowed("10.0.0.99") {
		t.Error("10.0.0.99 should be blocked (exact match in blocklist)")
	}
	if f.Allowed("172.16.5.5") {
		t.Error("172.16.5.5 should be blocked (in CIDR range)")
	}
	if !f.Allowed("192.168.1.1") {
		t.Error("192.168.1.1 should be allowed (not in blocklist)")
	}
	if !f.Allowed("10.0.0.100") {
		t.Error("10.0.0.100 should be allowed (not in blocklist)")
	}
}

func TestIPFilter_EmptyFilter(t *testing.T) {
	// Empty allowlist blocks everything
	f := NewIPFilter(FilterAllowlist)
	if f.Allowed("127.0.0.1") {
		t.Error("empty allowlist should deny all")
	}

	// Empty blocklist allows everything
	b := NewIPFilter(FilterBlocklist)
	if !b.Allowed("127.0.0.1") {
		t.Error("empty blocklist should allow all")
	}
}

func TestIPFilter_AddRemove(t *testing.T) {
	f := NewIPFilter(FilterAllowlist, "10.0.0.1")

	if !f.Allowed("10.0.0.1") {
		t.Error("10.0.0.1 should be allowed after add")
	}

	f.Remove("10.0.0.1")
	if f.Allowed("10.0.0.1") {
		t.Error("10.0.0.1 should be denied after remove from empty allowlist")
	}
}

func TestIPFilter_CIDR(t *testing.T) {
	f := NewIPFilter(FilterAllowlist, "10.0.0.0/8")

	// Test boundary IPs
	if !f.Allowed("10.0.0.0") {
		t.Error("10.0.0.0 should be in 10.0.0.0/8")
	}
	if !f.Allowed("10.255.255.255") {
		t.Error("10.255.255.255 should be in 10.0.0.0/8")
	}
	if f.Allowed("11.0.0.1") {
		t.Error("11.0.0.1 should NOT be in 10.0.0.0/8")
	}
}

func TestIPFilter_InvalidIP(t *testing.T) {
	f := NewIPFilter(FilterAllowlist, "10.0.0.1")
	// Invalid IP in allowlist mode => denied
	if f.Allowed("not-an-ip") {
		t.Error("invalid IP should be denied in allowlist mode")
	}

	b := NewIPFilter(FilterBlocklist, "10.0.0.1")
	// Invalid IP in blocklist mode => allowed
	if !b.Allowed("not-an-ip") {
		t.Error("invalid IP should be allowed in blocklist mode")
	}
}

func TestIPFilterMiddleware_Allowed(t *testing.T) {
	f := NewIPFilter(FilterAllowlist, "127.0.0.1")
	handler := IPFilterMiddleware(f, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed IP, got %d", rec.Code)
	}
}

func TestIPFilterMiddleware_Denied(t *testing.T) {
	f := NewIPFilter(FilterAllowlist, "10.0.0.1")
	handler := IPFilterMiddleware(f, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for denied IP")
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for denied IP, got %d", rec.Code)
	}
}

func TestIPFilterMiddleware_CustomExtractor(t *testing.T) {
	f := NewIPFilter(FilterBlocklist, "10.0.0.99")
	extractor := func(r *http.Request) string {
		return r.Header.Get("X-Real-IP")
	}

	handler := IPFilterMiddleware(f, extractor)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Block listed IP via custom header
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-IP", "10.0.0.99")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for blocked IP via custom extractor, got %d", rec.Code)
	}

	// Allowed IP via custom header
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Real-IP", "192.168.1.1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed IP via custom extractor, got %d", rec2.Code)
	}
}

// ─── Audit Logging Tests ──────────────────────────────────────────────────

func TestAuditSecurityEvent_Basic(t *testing.T) {
	// Just verify it doesn't panic — slog output goes to stderr
	AuditSecurityEvent(context.Background(), "test_event",
		"key1", "value1",
		"key2", 42,
	)
}

func TestAuditSecurityEvent_Dedup(t *testing.T) {
	// Create a context with audit tracking
	ctx := AuditContext(context.Background())

	// First call should pass through (we can only test no panic)
	AuditSecurityEvent(ctx, "rate_limit_exceeded", "client", "test")
	// Second call with same event type should be dedup'd
	AuditSecurityEvent(ctx, "rate_limit_exceeded", "client", "test")
	// Different event type should pass through
	AuditSecurityEvent(ctx, "auth_failure", "client", "test")
}

func TestAuditMiddleware(t *testing.T) {
	handler := AuditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify context has audit tracking
		if _, ok := r.Context().Value(auditKey{}).(map[string]bool); !ok {
			t.Error("audit middleware should inject audit context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAuditMiddleware_SlowResponse(t *testing.T) {
	handler := AuditMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // fast enough to not trigger slow_response log
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
