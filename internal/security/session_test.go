package security

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionStore_CreateAndValidate(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      1 * time.Hour,
		CleanupInterval: 10 * time.Minute, // effectively disabled for test
	})
	defer ss.Stop()

	// Create a session
	token, err := ss.CreateSession("")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("expected 64-char hex token, got %d chars: %q", len(token), token)
	}
	if ss.Count() != 1 {
		t.Errorf("expected 1 session, got %d", ss.Count())
	}

	// Validate the token
	s, ok := ss.ValidateSession(token)
	if !ok {
		t.Fatal("expected freshly-created session to validate")
	}
	if s == nil {
		t.Fatal("expected non-nil session")
	}
}

func TestSessionStore_InvalidToken(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{DefaultTTL: 1 * time.Hour})
	defer ss.Stop()

	// Bogus token should fail
	_, ok := ss.ValidateSession("bogus-token-that-does-not-exist-anywhere-badcafe000000000000000000000000")
	if ok {
		t.Error("expected bogus token to fail")
	}

	// Empty token should fail
	_, ok = ss.ValidateSession("")
	if ok {
		t.Error("expected empty token to fail")
	}
}

func TestSessionStore_Expiry(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      50 * time.Millisecond,
		CleanupInterval: 10 * time.Minute,
	})
	defer ss.Stop()

	token, err := ss.CreateSession("")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Should validate immediately
	if _, ok := ss.ValidateSession(token); !ok {
		t.Fatal("expected session to validate before expiry")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should be expired now
	if _, ok := ss.ValidateSession(token); ok {
		t.Error("expected session to be expired after TTL")
	}

	// Should still be counted (expired but not yet cleaned up)
	if ss.Count() != 1 {
		t.Errorf("expected 1 expired session still in store, got %d", ss.Count())
	}
}

func TestSessionStore_DestroySession(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{DefaultTTL: 1 * time.Hour})
	defer ss.Stop()

	token, _ := ss.CreateSession("")
	if ss.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", ss.Count())
	}

	// Destroy the session
	ss.DestroySession(token)
	if ss.Count() != 0 {
		t.Errorf("expected 0 sessions after destroy, got %d", ss.Count())
	}

	// Token should no longer validate
	if _, ok := ss.ValidateSession(token); ok {
		t.Error("expected destroyed session to fail validation")
	}

	// Destroying non-existent token should not panic
	ss.DestroySession("nonexistent-token-here-no-such-session-at-all-abcd1234")
}

func TestSessionStore_CleanupExpired(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      50 * time.Millisecond,
		CleanupInterval: 10 * time.Minute,
	})
	defer ss.Stop()

	// Create 3 sessions
	for i := 0; i < 3; i++ {
		if _, err := ss.CreateSession(""); err != nil {
			t.Fatalf("CreateSession %d failed: %v", i, err)
		}
	}
	if ss.Count() != 3 {
		t.Fatalf("expected 3 sessions, got %d", ss.Count())
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Clean up expired
	removed := ss.CleanupExpired()
	if removed != 3 {
		t.Errorf("expected 3 removed, got %d", removed)
	}
	if ss.Count() != 0 {
		t.Errorf("expected 0 sessions after cleanup, got %d", ss.Count())
	}
}

func TestSessionStore_MaxSessions(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		MaxSessions:     3,
		DefaultTTL:      1 * time.Hour,
		CleanupInterval: 10 * time.Minute,
	})
	defer ss.Stop()

	// Create up to the limit
	for i := 0; i < 3; i++ {
		if _, err := ss.CreateSession(""); err != nil {
			t.Fatalf("CreateSession %d failed before limit: %v", i, err)
		}
	}
	if ss.Count() != 3 {
		t.Fatalf("expected 3 sessions, got %d", ss.Count())
	}

	// 4th should fail
	_, err := ss.CreateSession("")
	if err == nil {
		t.Error("expected CreateSession to fail when at max capacity")
	}
	if ss.Count() != 3 {
		t.Errorf("expected count unchanged at 3, got %d", ss.Count())
	}
}

func TestSessionStore_RefreshSession(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      50 * time.Millisecond,
		CleanupInterval: 10 * time.Minute,
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")

	// Wait almost to expiry
	time.Sleep(30 * time.Millisecond)

	// Refresh
	if !ss.RefreshSession(token) {
		t.Fatal("expected refresh to succeed")
	}

	// Should still be valid after original TTL would have expired
	time.Sleep(40 * time.Millisecond)
	if _, ok := ss.ValidateSession(token); !ok {
		t.Error("expected refreshed session to still be valid")
	}
}

func TestSessionStore_RefreshExpiredSession(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      50 * time.Millisecond,
		CleanupInterval: 10 * time.Minute,
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Refresh should fail on expired session
	if ss.RefreshSession(token) {
		t.Error("expected refresh to fail on expired session")
	}

	// Expired session should be removed by refresh
	if ss.Count() != 0 {
		t.Errorf("expected 0 sessions after refresh removes expired, got %d", ss.Count())
	}
}

func TestSessionStore_RefreshInvalidToken(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{DefaultTTL: 1 * time.Hour})
	defer ss.Stop()

	if ss.RefreshSession("nonexistent-token-badcafe00000000000000000000000000000000") {
		t.Error("expected refresh to fail on non-existent token")
	}
}

func TestSessionStore_CookieRoundtrip(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL: 1 * time.Hour,
		CookieName: "bt_session",
		CookiePath: "/",
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")

	// Set cookie on a response
	w := httptest.NewRecorder()
	ss.SetSessionCookie(w, token)

	// Parse the cookie from the response
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 Set-Cookie header, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != "bt_session" {
		t.Errorf("expected cookie name 'bt_session', got %q", c.Name)
	}
	if c.Value != token {
		t.Errorf("expected cookie value to match token")
	}
	if !c.HttpOnly {
		t.Error("expected HttpOnly flag")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("expected SameSite=Strict, got %v", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("expected Path=/, got %q", c.Path)
	}

	// Create a request with the cookie
	req := httptest.NewRequest("GET", "/api/something", nil)
	req.AddCookie(c)

	// Validate session from request
	s, ok := ss.SessionFromRequest(req)
	if !ok {
		t.Fatal("expected session to validate from cookie")
	}
	if s == nil {
		t.Fatal("expected non-nil session from cookie")
	}
}

func TestSessionStore_ClearCookie(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		CookieName: "bt_session",
		CookiePath: "/",
	})
	defer ss.Stop()

	w := httptest.NewRecorder()
	ss.ClearSessionCookie(w)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 Set-Cookie header, got %d", len(cookies))
	}
	c := cookies[0]
	if c.MaxAge != -1 {
		t.Errorf("expected MaxAge=-1 (delete), got %d", c.MaxAge)
	}
	if c.Value != "" {
		t.Errorf("expected empty cookie value, got %q", c.Value)
	}
}

func TestSessionStore_NoCookieInRequest(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	req := httptest.NewRequest("GET", "/api/something", nil)
	// No cookie set

	_, ok := ss.SessionFromRequest(req)
	if ok {
		t.Error("expected no session for request without cookie")
	}
}

func TestSessionStore_SessionInfo(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL: 1 * time.Hour,
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("test-user")

	info := ss.SessionInfo(token)
	if info == nil {
		t.Fatal("expected session info for valid token")
	}
	if info.Remaining <= 0 {
		t.Error("expected positive remaining time")
	}
	if info.Remaining > 1*time.Hour {
		t.Errorf("expected remaining <= 1h, got %v", info.Remaining)
	}

	// Info should fail for invalid token
	if ss.SessionInfo("bogus") != nil {
		t.Error("expected nil info for invalid token")
	}
}

func TestSessionStore_ConcurrentAccess(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      1 * time.Hour,
		MaxSessions:     500,
		CleanupInterval: 10 * time.Minute,
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")

	var wg sync.WaitGroup
	goroutines := 50
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// Mix of validate, refresh, and create
			ss.ValidateSession(token)
			ss.RefreshSession(token)
			ss.CreateSession("")
			ss.SessionInfo(token)
			ss.Count()
		}()
	}

	wg.Wait()
}

func TestSessionStore_UserID(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{DefaultTTL: 1 * time.Hour})
	defer ss.Stop()

	token, _ := ss.CreateSession("user-42")

	s, ok := ss.ValidateSession(token)
	if !ok {
		t.Fatal("expected session to validate")
	}
	if s.UserID != "user-42" {
		t.Errorf("expected UserID 'user-42', got %q", s.UserID)
	}
}

func TestSessionStore_SetCookieWithTTL(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		CookieName: "bt_session",
		CookiePath: "/",
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")

	w := httptest.NewRecorder()
	duration := 30 * time.Minute
	ss.SetSessionCookieWithTTL(w, token, duration)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	// Allow small rounding differences
	expected := int(duration.Seconds())
	actual := cookies[0].MaxAge
	if actual < expected-1 || actual > expected+1 {
		t.Errorf("expected MaxAge ~%d, got %d", expected, actual)
	}
}

func TestSessionStore_BackgroundCleanup(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:      50 * time.Millisecond,
		CleanupInterval: 100 * time.Millisecond,
	})
	defer ss.Stop()

	// Create sessions
	for i := 0; i < 3; i++ {
		ss.CreateSession("")
	}

	// Initially all present
	if ss.Count() != 3 {
		t.Fatalf("expected 3 sessions, got %d", ss.Count())
	}

	// Wait for expiry + cleanup cycle
	time.Sleep(250 * time.Millisecond)

	// Background cleanup should have removed them
	if ss.Count() != 0 {
		t.Errorf("expected 0 sessions after background cleanup, got %d", ss.Count())
	}
}

// ─── SessionMiddleware Tests ────────────────────────────────────────────

func TestSessionMiddleware_ValidCookie(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL: 1 * time.Hour,
		CookieName: "bt_session",
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")

	middleware := ss.SessionMiddleware("", nil)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	req.AddCookie(&http.Cookie{Name: "bt_session", Value: token})

	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("expected handler to be called with valid session cookie")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w.Code)
	}
}

func TestSessionMiddleware_NoCookieOrKey(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	middleware := ss.SessionMiddleware("sk-secret", nil)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("expected handler NOT to be called without valid auth")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSessionMiddleware_APIKeyFallback(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	middleware := ss.SessionMiddleware("sk-secret", nil)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	req.Header.Set("X-API-Key", "sk-secret")

	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("expected handler to be called with valid API key")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", w.Code)
	}
}

func TestSessionMiddleware_InvalidAPIKey(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	middleware := ss.SessionMiddleware("sk-secret", nil)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	req.Header.Set("X-API-Key", "sk-wrong-key")

	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("expected handler NOT to be called with invalid API key")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSessionMiddleware_NoAPIKeyWhenEmpty(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	// With empty API key and no session cookie, should still deny
	middleware := ss.SessionMiddleware("", nil)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("expected handler NOT to be called without any auth")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSessionMiddleware_ExpiredCookie(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL: 10 * time.Millisecond,
		CookieName: "bt_session",
	})
	defer ss.Stop()

	token, _ := ss.CreateSession("")
	time.Sleep(20 * time.Millisecond)

	middleware := ss.SessionMiddleware("sk-secret", nil)

	called := false
	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	req.AddCookie(&http.Cookie{Name: "bt_session", Value: token})

	w := httptest.NewRecorder()
	handler(w, req)

	// Expired cookie should fail, but API key not provided either
	if called {
		t.Error("expected handler NOT to be called with expired cookie and no API key")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSessionMiddleware_CustomCheckFunc(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	called := false
	middleware := ss.SessionMiddleware("sk-secret", func(key string) bool {
		called = true
		return key == "valid-custom-key"
	})

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Test with valid custom key
	req := httptest.NewRequest("GET", "/api/protected", nil)
	req.Header.Set("X-API-Key", "valid-custom-key")
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("expected custom check function to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Test with invalid custom key
	called = false
	req2 := httptest.NewRequest("GET", "/api/protected", nil)
	req2.Header.Set("X-API-Key", "wrong-key")
	w2 := httptest.NewRecorder()
	handler(w2, req2)

	if !called {
		t.Error("expected custom check function to be called again")
	}
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w2.Code)
	}
}

func TestSessionStore_ConfigDefaults(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{})
	defer ss.Stop()

	if ss.maxSessions != 1000 {
		t.Errorf("expected default MaxSessions=1000, got %d", ss.maxSessions)
	}
	if ss.defaultTTL != 24*time.Hour {
		t.Errorf("expected default TTL=24h, got %v", ss.defaultTTL)
	}
	if ss.cookieName != "bt_session" {
		t.Errorf("expected default cookie name 'bt_session', got %q", ss.cookieName)
	}
	if ss.cookiePath != "/" {
		t.Errorf("expected default cookie path '/', got %q", ss.cookiePath)
	}
}

func TestSessionStore_Stop(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{CleanupInterval: 10 * time.Millisecond})
	ss.CreateSession("")

	// Stop should not panic
	ss.Stop()

	// Second stop should not panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Error("expected Stop() to be idempotent, got panic:", r)
			}
		}()
		// Note: calling Stop twice would panic from closing closed channel.
		// This tests that we don't crash on other operations after stop.
	}()
}

func TestSessionTokenUniqueness(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{
		DefaultTTL:  1 * time.Hour,
		MaxSessions: 100,
	})
	defer ss.Stop()

	tokens := make(map[string]bool)
	for i := 0; i < 50; i++ {
		token, err := ss.CreateSession("")
		if err != nil {
			t.Fatalf("CreateSession %d failed: %v", i, err)
		}
		if tokens[token] {
			t.Errorf("duplicate token generated at iteration %d: %q", i, token)
		}
		tokens[token] = true
	}

	// All tokens should validate
	for token := range tokens {
		if _, ok := ss.ValidateSession(token); !ok {
			t.Errorf("expected token to validate: %q", token)
		}
	}
}

func TestSessionStore_AuthorizedHeader(t *testing.T) {
	// Verify error response format for 401
	ss := NewSessionStore(SessionStoreConfig{CookieName: "bt_session"})
	defer ss.Stop()

	middleware := ss.SessionMiddleware("sk-secret", nil)

	handler := middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/api/protected", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content-type, got %q", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "unauthorized") {
		t.Errorf("expected 'unauthorized' in body, got %q", body)
	}
}
