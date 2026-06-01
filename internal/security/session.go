// Session management with cryptographic session tokens and TTL-based expiry.
//
// SessionStore provides:
//   - Session creation with crypto/rand token generation (64-char hex)
//   - TTL-based expiry with configurable default lifetime
//   - Periodic background cleanup of expired sessions
//   - HTTP cookie integration (SetSessionCookie, ClearSessionCookie)
//   - Server-side validation (no client-recoverable secrets)
//   - Max session cap to prevent memory exhaustion
//
// Sessions are stored in-memory using a sync.RWMutex-protected map.
// Raw session tokens are SHA-256 hashed before storage — the store never
// holds the raw token, only the hash.
package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Session represents an authenticated user session.
type Session struct {
	ID        string    `json:"id"` // SHA-256 hash of the raw session token
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	LastUsed  time.Time `json:"last_used"`
	UserID    string    `json:"user_id,omitempty"` // Optional, for future multi-user support
}

// SessionInfo is the public-facing representation of a session.
// Raw tokens and hashes are never exposed.
type SessionInfo struct {
	CreatedAt time.Time     `json:"created_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	LastUsed  time.Time     `json:"last_used"`
	Remaining time.Duration `json:"remaining"`
}

// SessionStore manages authenticated sessions. Safe for concurrent use.
type SessionStore struct {
	mu            sync.RWMutex
	sessions      map[string]*Session // hash -> session
	maxSessions   int
	defaultTTL    time.Duration
	cookieName    string
	cookieSecure  bool
	cookiePath    string
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
	done          chan struct{}
}

// SessionStoreConfig configures a SessionStore.
type SessionStoreConfig struct {
	// MaxSessions is the maximum number of concurrent sessions. Default: 1000.
	MaxSessions int
	// DefaultTTL is the session lifetime. Default: 24 hours.
	DefaultTTL time.Duration
	// CookieName is the name of the HTTP cookie storing the session token. Default: "bt_session".
	CookieName string
	// CookieSecure sets the Secure flag on session cookies (HTTPS-only). Default: false.
	CookieSecure bool
	// CookiePath sets the Path attribute. Default: "/".
	CookiePath string
	// CleanupInterval is how often expired sessions are purged. Default: 5 minutes.
	CleanupInterval time.Duration
}

// NewSessionStore creates a new session store and starts background cleanup.
func NewSessionStore(cfg SessionStoreConfig) *SessionStore {
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 1000
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 24 * time.Hour
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "bt_session"
	}
	if cfg.CookiePath == "" {
		cfg.CookiePath = "/"
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}

	ss := &SessionStore{
		sessions:     make(map[string]*Session),
		maxSessions:  cfg.MaxSessions,
		defaultTTL:   cfg.DefaultTTL,
		cookieName:   cfg.CookieName,
		cookieSecure: cfg.CookieSecure,
		cookiePath:   cfg.CookiePath,
		stopCh:       make(chan struct{}),
		done:         make(chan struct{}),
	}
	ss.cleanupTicker = time.NewTicker(cfg.CleanupInterval)
	go ss.cleanupLoop()
	return ss
}

// CreateSession creates a new session with the default TTL.
// Returns the raw session token — this is the ONLY time the raw token is exposed.
// Store it immediately in a Set-Cookie header; only the hash is retained.
func (ss *SessionStore) CreateSession(userID string) (string, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	if len(ss.sessions) >= ss.maxSessions {
		return "", fmt.Errorf("session limit reached: %d sessions", ss.maxSessions)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("session token generation failed: %w", err)
	}
	token := hex.EncodeToString(raw) // 64-char hex
	hash := sha256Hex(token)

	now := time.Now()
	ss.sessions[hash] = &Session{
		ID:        hash,
		CreatedAt: now,
		ExpiresAt: now.Add(ss.defaultTTL),
		LastUsed:  now,
		UserID:    userID,
	}
	return token, nil
}

// ValidateSession checks if a raw session token is valid (exists and not expired).
// Updates LastUsed on successful validation. Returns the Session if valid.
func (ss *SessionStore) ValidateSession(token string) (*Session, bool) {
	hash := sha256Hex(token)

	ss.mu.RLock()
	s, ok := ss.sessions[hash]
	ss.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(s.ExpiresAt) {
		return nil, false
	}

	// Update LastUsed
	ss.mu.Lock()
	if s2, ok := ss.sessions[hash]; ok {
		s2.LastUsed = time.Now()
	}
	ss.mu.Unlock()

	return s, true
}

// DestroySession removes a session by its raw token.
// Safe to call on invalid tokens (no-op).
func (ss *SessionStore) DestroySession(token string) {
	hash := sha256Hex(token)
	ss.mu.Lock()
	delete(ss.sessions, hash)
	ss.mu.Unlock()
}

// SessionFromRequest extracts and validates a session from an HTTP request.
// Looks for the session cookie named by cookieName. Returns the Session and
// a bool indicating whether a valid session was found.
func (ss *SessionStore) SessionFromRequest(r *http.Request) (*Session, bool) {
	cookie, err := r.Cookie(ss.cookieName)
	if err != nil {
		return nil, false
	}
	return ss.ValidateSession(cookie.Value)
}

// SetSessionCookie sets the session token as an HTTP-only, SameSite=Strict cookie
// on the response writer. The cookie's Path and Secure flags use the store config.
// maxAge is 0 (session cookie — expires when browser closes).
func (ss *SessionStore) SetSessionCookie(w http.ResponseWriter, token string) {
	cookie := &http.Cookie{
		Name:     ss.cookieName,
		Value:    token,
		Path:     ss.cookiePath,
		MaxAge:   0, // session cookie
		HttpOnly: true,
		Secure:   ss.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

// SetSessionCookieWithTTL sets a session cookie with an explicit MaxAge.
// Use when you want the cookie to persist across browser restarts.
func (ss *SessionStore) SetSessionCookieWithTTL(w http.ResponseWriter, token string, maxAge time.Duration) {
	cookie := &http.Cookie{
		Name:     ss.cookieName,
		Value:    token,
		Path:     ss.cookiePath,
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   ss.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

// ClearSessionCookie removes the session cookie from the client.
func (ss *SessionStore) ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     ss.cookieName,
		Value:    "",
		Path:     ss.cookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   ss.cookieSecure,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
}

// RefreshSession extends the session's expiry by the default TTL from now.
// Returns true if the session was found and refreshed.
func (ss *SessionStore) RefreshSession(token string) bool {
	hash := sha256Hex(token)

	ss.mu.Lock()
	defer ss.mu.Unlock()

	s, ok := ss.sessions[hash]
	if !ok {
		return false
	}
	if time.Now().After(s.ExpiresAt) {
		delete(ss.sessions, hash)
		return false
	}
	s.ExpiresAt = time.Now().Add(ss.defaultTTL)
	s.LastUsed = time.Now()
	return true
}

// CleanupExpired removes all expired sessions. Returns the count of removed sessions.
func (ss *SessionStore) CleanupExpired() int {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	now := time.Now()
	removed := 0
	for hash, s := range ss.sessions {
		if now.After(s.ExpiresAt) {
			delete(ss.sessions, hash)
			removed++
		}
	}
	return removed
}

// Count returns the number of active sessions.
func (ss *SessionStore) Count() int {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return len(ss.sessions)
}

// SessionInfo returns public information about a session given the raw token.
// Returns nil if the session is invalid or expired.
func (ss *SessionStore) SessionInfo(token string) *SessionInfo {
	s, ok := ss.ValidateSession(token)
	if !ok {
		return nil
	}
	return &SessionInfo{
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		LastUsed:  s.LastUsed,
		Remaining: time.Until(s.ExpiresAt),
	}
}

// Stop gracefully shuts down the cleanup goroutine.
func (ss *SessionStore) Stop() {
	close(ss.stopCh)
	<-ss.done
}

func (ss *SessionStore) cleanupLoop() {
	defer close(ss.done)
	for {
		select {
		case <-ss.cleanupTicker.C:
			ss.CleanupExpired()
		case <-ss.stopCh:
			ss.cleanupTicker.Stop()
			return
		}
	}
}

// SessionMiddleware returns middleware that checks for a valid session cookie
// OR a valid API key header. If neither is present, requests are denied with 401.
//
// If apiKey is empty, session cookie alone suffices (all auth is session-based).
// If both apiKey and checkAPIKey are set, the API key is validated via the provided
// function. If checkAPIKey is nil, the raw string comparison is used.
//
// Public endpoints (/api/health, /api/metrics, /api/alerts, etc.) should NOT use
// this middleware — apply it only to protected endpoints.
func (ss *SessionStore) SessionMiddleware(apiKey string, checkAPIKey func(key string) bool) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Try session cookie first
			if _, ok := ss.SessionFromRequest(r); ok {
				next(w, r)
				return
			}

			// Fall back to API key
			if apiKey != "" {
				provided := r.Header.Get("X-API-Key")
				if checkAPIKey != nil {
					if checkAPIKey(provided) {
						next(w, r)
						return
					}
				} else if provided == apiKey {
					next(w, r)
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintf(w, `{"error":"unauthorized: valid session cookie or X-API-Key header required"}`)
		}
	}
}
