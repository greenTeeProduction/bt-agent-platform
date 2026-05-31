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

// ─── Request ID Tests ──────────────────────────────────────────────────────

func TestGenerateRequestID_IsHex(t *testing.T) {
	id := GenerateRequestID()
	if len(id) != 16 {
		t.Errorf("expected 16-char hex ID, got %d chars: %q", len(id), id)
	}
	// Verify it's valid hex
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("expected hex chars only, got %q in %q", c, id)
		}
	}
}

func TestGenerateRequestID_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := GenerateRequestID()
		if seen[id] {
			t.Errorf("duplicate request ID generated: %q", id)
		}
		seen[id] = true
	}
}

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestID(r.Context())
		if id == "" {
			t.Error("RequestID should not be empty")
		}
		if len(id) != 16 {
			t.Errorf("expected 16-char ID, got %d", len(id))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	respID := rec.Header().Get("X-Request-ID")
	if respID == "" {
		t.Error("X-Request-ID response header should be set")
	}
	if len(respID) != 16 {
		t.Errorf("expected 16-char response header ID, got %d", len(respID))
	}
}

func TestRequestIDMiddleware_ReusesIncomingID(t *testing.T) {
	existingID := "req-test-1234567"
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestID(r.Context())
		if id != existingID {
			t.Errorf("expected RequestID=%q, got %q", existingID, id)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	respID := rec.Header().Get("X-Request-ID")
	if respID != existingID {
		t.Errorf("X-Request-ID response header = %q, want %q", respID, existingID)
	}
}

func TestRequestID_NoMiddleware(t *testing.T) {
	// Without the middleware in the chain, RequestID should return ""
	id := RequestID(context.Background())
	if id != "" {
		t.Errorf("expected empty RequestID without middleware, got %q", id)
	}
}

func TestRequestIDMiddleware_UniquePerRequest(t *testing.T) {
	ids := make(map[string]bool)
	handler := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := RequestID(r.Context())
		ids[id] = true
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if len(ids) != 100 {
		t.Errorf("expected 100 unique IDs across 100 requests, got %d", len(ids))
	}
}

func TestGenerateRequestID_NoPanic(t *testing.T) {
	// GeneateRequestID should never panic, even if crypto/rand fails
	// (which it won't on modern Linux, but the fallback keeps us safe).
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			id := GenerateRequestID()
			if id == "" {
				t.Error("GenerateRequestID returned empty string")
			}
		}
	}()
	// Wait with timeout
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GenerateRequestID timed out")
	}
}

// ─── CSRF Tests ────────────────────────────────────────────────────────────

func TestGenerateCSRFToken(t *testing.T) {
	token := GenerateCSRFToken()
	if token == "" {
		t.Error("GenerateCSRFToken returned empty string")
	}
	if len(token) != 64 {
		t.Errorf("expected 64 hex chars (32 bytes), got %d", len(token))
	}

	// Should be unique
	token2 := GenerateCSRFToken()
	if token == token2 {
		t.Error("consecutive CSRF tokens should be unique")
	}
}

func TestCSRFMiddleware_SafeMethodsPassThrough(t *testing.T) {
	handler := CSRFMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		req := httptest.NewRequest(method, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s request should pass through CSRF, got %d", method, rec.Code)
		}
	}
}

func TestCSRFMiddleware_SetsCookieOnFirstSafeRequest(t *testing.T) {
	handler := CSRFMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should have set the CSRF cookie
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "_csrf_token" {
			found = true
			if c.Value == "" {
				t.Error("CSRF cookie value should not be empty")
			}
			if !c.Secure {
				t.Error("CSRF cookie should be Secure")
			}
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("CSRF cookie SameSite should be Strict, got %v", c.SameSite)
			}
			if c.HttpOnly {
				t.Error("CSRF cookie should NOT be HttpOnly (JS needs to read it)")
			}
			if c.Path != "/" {
				t.Errorf("CSRF cookie Path should be '/', got %q", c.Path)
			}
		}
	}
	if !found {
		t.Error("CSRF middleware should set _csrf_token cookie on first safe request")
	}
}

func TestCSRFMiddleware_ReusesExistingCookie(t *testing.T) {
	handler := CSRFMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request sets cookie
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Extract the cookie
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected CSRF cookie to be set")
	}
	csrfCookie := cookies[0]

	// Second request with the cookie should NOT set a new one
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.AddCookie(csrfCookie)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// No new Set-Cookie header
	for _, c := range rec2.Result().Cookies() {
		if c.Name == "_csrf_token" {
			t.Error("should not set a new CSRF cookie when one already exists")
		}
	}
}

func TestCSRFMiddleware_ValidPOSTPasses(t *testing.T) {
	handler := CSRFMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First GET to get a token
	getReq := httptest.NewRequest("GET", "/test", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	var token string
	for _, c := range getRec.Result().Cookies() {
		if c.Name == "_csrf_token" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no CSRF token generated")
	}

	// POST with matching cookie + header
	postReq := httptest.NewRequest("POST", "/test", nil)
	postReq.AddCookie(&http.Cookie{Name: "_csrf_token", Value: token})
	postReq.Header.Set("X-CSRF-Token", token)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Errorf("valid POST should pass CSRF, got %d", postRec.Code)
	}
}

func TestCSRFMiddleware_InvalidPOSTRejected(t *testing.T) {
	handler := CSRFMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid CSRF")
	}))

	tests := []struct {
		name        string
		cookieToken string
		headerToken string
	}{
		{"no cookie or header", "", ""},
		{"cookie only", "abc123", ""},
		{"header only", "", "abc123"},
		{"mismatch", "abc123", "def456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/test", nil)
			if tt.cookieToken != "" {
				req.AddCookie(&http.Cookie{Name: "_csrf_token", Value: tt.cookieToken})
			}
			if tt.headerToken != "" {
				req.Header.Set("X-CSRF-Token", tt.headerToken)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Errorf("expected 403 for %s, got %d", tt.name, rec.Code)
			}
		})
	}
}

func TestCSRFMiddleware_DELETEAndPUTAlsoProtected(t *testing.T) {
	handler := CSRFMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called without valid CSRF")
	}))

	for _, method := range []string{"DELETE", "PUT", "PATCH"} {
		req := httptest.NewRequest(method, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without CSRF should be rejected, got %d", method, rec.Code)
		}
	}
}

func TestCSRFMiddleware_CustomTokenGenerator(t *testing.T) {
	customToken := "custom-static-token"
	genFn := func() string { return customToken }

	handler := CSRFMiddleware(genFn)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First GET should set the custom token
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var gotToken string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "_csrf_token" {
			gotToken = c.Value
		}
	}
	if gotToken != customToken {
		t.Errorf("expected custom token %q, got %q", customToken, gotToken)
	}

	// POST with custom token should work
	postReq := httptest.NewRequest("POST", "/test", nil)
	postReq.AddCookie(&http.Cookie{Name: "_csrf_token", Value: customToken})
	postReq.Header.Set("X-CSRF-Token", customToken)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Errorf("POST with custom token should pass, got %d", postRec.Code)
	}
}

func TestGenerateCSRFToken_Length(t *testing.T) {
	for i := 0; i < 100; i++ {
		token := GenerateCSRFToken()
		if len(token) != 64 {
			t.Errorf("token should be 64 hex chars, got %d: %s", len(token), token)
		}
	}
}

// ─── Content-Type Validation Tests ──────────────────────────────────────────

func TestContentTypeMiddleware_AllowsJSON(t *testing.T) {
	handler := ContentTypeMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for JSON content type, got %d", rec.Code)
	}
}

func TestContentTypeMiddleware_RejectsPlainText(t *testing.T) {
	handler := ContentTypeMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("plain text"))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 Unsupported Media Type, got %d", rec.Code)
	}
}

func TestContentTypeMiddleware_RejectsEmptyContentType(t *testing.T) {
	handler := ContentTypeMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"key":"value"}`))
	// No Content-Type header set
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for missing Content-Type, got %d", rec.Code)
	}
}

func TestContentTypeMiddleware_AllowsGETWithoutContentType(t *testing.T) {
	handler := ContentTypeMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	// No Content-Type header on GET — should pass through
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for GET without Content-Type, got %d", rec.Code)
	}
}

func TestContentTypeMiddleware_AllowsJSONWithCharset(t *testing.T) {
	handler := ContentTypeMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for JSON with charset, got %d", rec.Code)
	}
}

func TestContentTypeMiddleware_CustomAllowedTypes(t *testing.T) {
	allowedTypes := map[string]bool{
		"application/json": true,
		"application/xml":  true,
	}
	handler := ContentTypeMiddleware(allowedTypes, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// JSON should pass
	reqJSON := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{"key":"value"}`))
	reqJSON.Header.Set("Content-Type", "application/json")
	recJSON := httptest.NewRecorder()
	handler.ServeHTTP(recJSON, reqJSON)
	if recJSON.Code != http.StatusOK {
		t.Errorf("expected 200 OK for JSON, got %d", recJSON.Code)
	}

	// XML should pass
	reqXML := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("<root/>"))
	reqXML.Header.Set("Content-Type", "application/xml")
	recXML := httptest.NewRecorder()
	handler.ServeHTTP(recXML, reqXML)
	if recXML.Code != http.StatusOK {
		t.Errorf("expected 200 OK for XML, got %d", recXML.Code)
	}

	// Form data should be rejected
	reqForm := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("key=value"))
	reqForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recForm := httptest.NewRecorder()
	handler.ServeHTTP(recForm, reqForm)
	if recForm.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for form data, got %d", recForm.Code)
	}
}

func TestContentTypeMiddleware_CustomMethods(t *testing.T) {
	allowedTypes := map[string]bool{"application/json": true}
	methods := map[string]bool{http.MethodPost: true, http.MethodPut: true}
	handler := ContentTypeMiddleware(allowedTypes, methods)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST should enforce
	postReq := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("plain"))
	postReq.Header.Set("Content-Type", "text/plain")
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for POST text/plain, got %d", postRec.Code)
	}

	// PATCH should NOT enforce (not in custom methods)
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/test", strings.NewReader("plain"))
	patchReq.Header.Set("Content-Type", "text/plain")
	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for PATCH (not enforced), got %d", patchRec.Code)
	}
}

func TestJSONContentTypeMiddleware_Convenience(t *testing.T) {
	handler := JSONContentTypeMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// JSON passes
	goodReq := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(`{}`))
	goodReq.Header.Set("Content-Type", "application/json")
	goodRec := httptest.NewRecorder()
	handler.ServeHTTP(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for JSON, got %d", goodRec.Code)
	}

	// GET passes without Content-Type
	getReq := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for GET, got %d", getRec.Code)
	}

	// Form data rejected
	badReq := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader("x=1"))
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for form data via JSONContentTypeMiddleware, got %d", badRec.Code)
	}
}

func TestContentTypeMiddleware_OptionsPassesThrough(t *testing.T) {
	handler := ContentTypeMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for OPTIONS, got %d", rec.Code)
	}
}

func TestContentTypeMiddleware_EmptyAllowedTypesDefaultsToJSON(t *testing.T) {
	// Empty allowedTypes + empty methods → defaults to JSON enforcement on POST/PUT/PATCH/DELETE
	handler := ContentTypeMiddleware(map[string]bool{}, map[string]bool{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// JSON passes
	jsonReq := httptest.NewRequest(http.MethodPut, "/api/test", strings.NewReader(`{}`))
	jsonReq.Header.Set("Content-Type", "application/json")
	jsonRec := httptest.NewRecorder()
	handler.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK {
		t.Errorf("expected 200 OK for JSON on PUT, got %d", jsonRec.Code)
	}

	// Form data rejected on DELETE
	delReq := httptest.NewRequest(http.MethodDelete, "/api/test", strings.NewReader("x=1"))
	delReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("expected 415 for form data on DELETE, got %d", delRec.Code)
	}
}
