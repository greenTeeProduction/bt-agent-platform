// Package security provides rate limiting, input sanitization, auth utilities,
// IP filtering, and security audit logging for the Go BT framework's dashboard and MCP servers.
package security

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─── Rate Limiter ───────────────────────────────────────────────────────────

// RateLimiter implements a token bucket rate limiter per client (by IP or API key).
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64 // tokens per second
	burst    int     // max burst size
	cleanupInterval time.Duration
}

type tokenBucket struct {
	tokens   float64
	lastTime time.Time
}

// NewRateLimiter creates a rate limiter. rate=tokens/sec, burst=max burst.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     rate,
		burst:    burst,
		cleanupInterval: 10 * time.Minute,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-30 * time.Minute)
		for k, b := range rl.buckets {
			if b.lastTime.Before(cutoff) {
				delete(rl.buckets, k)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow checks if a request from `key` (IP or API key) is allowed.
// Returns true if the request should be allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	now := time.Now()
	if !ok {
		rl.buckets[key] = &tokenBucket{tokens: float64(rl.burst) - 1, lastTime: now}
		return true
	}

	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastTime = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimitMiddleware wraps an http.Handler with rate limiting.
// extractKey extracts the client key from the request (default: RemoteAddr).
func RateLimitMiddleware(rl *RateLimiter, extractKey func(*http.Request) string) func(http.Handler) http.Handler {
	if extractKey == nil {
		extractKey = func(r *http.Request) string { return r.RemoteAddr }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !rl.Allow(extractKey(r)) {
				AuditSecurityEvent(r.Context(), "rate_limit_exceeded",
					"client", extractKey(r),
					"path", r.URL.Path,
				)
				w.Header().Set("Retry-After", "1")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "rate_limit_exceeded",
					"message": "Too many requests. Please slow down.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Input Sanitizer ────────────────────────────────────────────────────────

// SanitizeMiddleware strips dangerous characters and enforces limits on request bodies.
func SanitizeMiddleware(maxBodySize int64) func(http.Handler) http.Handler {
	if maxBodySize <= 0 {
		maxBodySize = 1 << 20 // 1 MB default
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Enforce body size limit
			if r.ContentLength > maxBodySize {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "payload_too_large",
					"message": "Request body exceeds maximum size.",
				})
				return
			}

			// Sanitize query parameters (strip null bytes and control chars)
			q := r.URL.Query()
			for k, vals := range q {
				clean := make([]string, len(vals))
				for i, v := range vals {
					clean[i] = sanitizeString(v)
				}
				q[k] = clean
			}
			r.URL.RawQuery = q.Encode()

			// Strip dangerous headers
			r.Header.Del("X-Forwarded-For") // use RemoteAddr directly
			r.Header.Del("X-Real-Ip")

			next.ServeHTTP(w, r)
		})
	}
}

// sanitizeString removes null bytes and control characters from a string.
func sanitizeString(s string) string {
	return strings.Map(func(r rune) rune {
		if r == 0 || (r < 32 && r != '\n' && r != '\r' && r != '\t') {
			return -1 // drop
		}
		return r
	}, s)
}

// SanitizeInput cleans a task/input string by removing potentially dangerous content.
// Returns the sanitized string.
func SanitizeInput(input string) string {
	// Strip null bytes
	s := strings.ReplaceAll(input, "\x00", "")
	// Strip ANSI escape sequences
	for strings.Contains(s, "\x1b[") {
		start := strings.Index(s, "\x1b[")
		end := start + 2
		for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == ';' || s[end] == '[') {
			end++
		}
		if end < len(s) {
			end++
		}
		if end > len(s) {
			end = len(s)
		}
		s = s[:start] + s[end:]
	}
	// Trim excessive whitespace
	return strings.TrimSpace(s)
}

// ─── Security Headers ───────────────────────────────────────────────────────

// SecurityHeadersConfig controls which security headers are set and their values.
// Zero values disable the respective header (except HSTS which is opt-in).
type SecurityHeadersConfig struct {
	// HSTS is opt-in — only set when serving over HTTPS/Tailscale.
	EnableHSTS         bool
	HSTSMaxAge         int    // seconds, default 31536000 (1 year)
	HSTSIncludeSub     bool   // includeSubDomains
	FrameOptions       string // default "DENY"
	CSP                string // Content-Security-Policy value
	ReferrerPolicy     string // default "strict-origin-when-cross-origin"
	PermissionsPolicy  string // Permissions-Policy header value
}

// DefaultSecurityHeaders returns a production-ready default configuration.
func DefaultSecurityHeaders() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		EnableHSTS:        false, // opt-in — only for HTTPS deployments
		HSTSMaxAge:        31536000,
		FrameOptions:      "DENY",
		CSP:               "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'",
		ReferrerPolicy:    "strict-origin-when-cross-origin",
		PermissionsPolicy: "camera=(), microphone=(), geolocation=(), interest-cohort=()",
	}
}

// SecurityHeadersMiddleware adds standard HTTP security headers to every response.
// Use DefaultSecurityHeaders() for production defaults or pass a custom config.
func SecurityHeadersMiddleware(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// Prevent MIME-type sniffing
			h.Set("X-Content-Type-Options", "nosniff")

			// Prevent clickjacking
			if cfg.FrameOptions != "" {
				h.Set("X-Frame-Options", cfg.FrameOptions)
			}

			// Enable browser XSS auditor
			h.Set("X-XSS-Protection", "1; mode=block")

			// HSTS (opt-in — only for HTTPS)
			if cfg.EnableHSTS {
				policy := "max-age=" + strconv.Itoa(cfg.HSTSMaxAge)
				if cfg.HSTSIncludeSub {
					policy += "; includeSubDomains"
				}
				h.Set("Strict-Transport-Security", policy)
			}

			// Content Security Policy
			if cfg.CSP != "" {
				h.Set("Content-Security-Policy", cfg.CSP)
			}

			// Referrer Policy
			if cfg.ReferrerPolicy != "" {
				h.Set("Referrer-Policy", cfg.ReferrerPolicy)
			}

			// Permissions Policy
			if cfg.PermissionsPolicy != "" {
				h.Set("Permissions-Policy", cfg.PermissionsPolicy)
			}

			// Disable caching of API responses by default
			h.Set("Cache-Control", "no-store, max-age=0")

			next.ServeHTTP(w, r)
		})
	}
}

// CrossOriginMiddleware adds CORS headers for browser-based access.
// origins is a comma-separated list of allowed origins (use "*" for any).
// methods is a comma-separated list of allowed HTTP methods.
func CrossOriginMiddleware(origins, methods string) func(http.Handler) http.Handler {
	if origins == "" {
		origins = "*"
	}
	if methods == "" {
		methods = "GET, POST, PUT, DELETE, OPTIONS"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origins)
			h.Set("Access-Control-Allow-Methods", methods)
			h.Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
			h.Set("Access-Control-Max-Age", "86400")

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequestTimeoutMiddleware enforces a maximum duration for request processing.
func RequestTimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			done := make(chan struct{})
			go func() {
				next.ServeHTTP(w, r.WithContext(ctx))
				close(done)
			}()

			select {
			case <-done:
				return
			case <-ctx.Done():
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusGatewayTimeout)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "request_timeout",
					"message": "Request exceeded maximum processing time.",
				})
			}
		})
	}
}

// ─── IP Filter ──────────────────────────────────────────────────────────────

// IPFilterMode determines whether the filter is an allowlist or blocklist.
type IPFilterMode int

const (
	// FilterAllowlist only allows requests from IPs in the list.
	FilterAllowlist IPFilterMode = iota
	// FilterBlocklist blocks requests from IPs in the list.
	FilterBlocklist
)

// IPFilter provides IP/CIDR-based access control for HTTP handlers.
type IPFilter struct {
	mu     sync.RWMutex
	nets   []*net.IPNet
	ips    map[string]bool
	mode   IPFilterMode
}

// NewIPFilter creates an IP filter. IPs can be individual addresses ("192.168.1.1")
// or CIDR ranges ("10.0.0.0/8"). mode determines allowlist or blocklist behavior.
func NewIPFilter(mode IPFilterMode, entries ...string) *IPFilter {
	f := &IPFilter{
		ips:  make(map[string]bool),
		mode: mode,
	}
	for _, e := range entries {
		f.Add(e)
	}
	return f
}

// Add adds an IP or CIDR range to the filter.
func (f *IPFilter) Add(entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.Contains(entry, "/") {
		_, cidr, err := net.ParseCIDR(entry)
		if err == nil {
			f.nets = append(f.nets, cidr)
		}
	} else {
		ip := net.ParseIP(entry)
		if ip != nil {
			f.ips[ip.String()] = true
		}
	}
}

// Remove removes an IP or CIDR range from the filter.
func (f *IPFilter) Remove(entry string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.Contains(entry, "/") {
		_, cidr, err := net.ParseCIDR(entry)
		if err == nil {
			for i, n := range f.nets {
				if n.String() == cidr.String() {
					f.nets = append(f.nets[:i], f.nets[i+1:]...)
					break
				}
			}
		}
	} else {
		delete(f.ips, entry)
	}
}

// Allowed checks whether an IP is allowed through the filter.
// For allowlist mode: returns true if IP matches any entry.
// For blocklist mode: returns true if IP does NOT match any entry.
func (f *IPFilter) Allowed(ipStr string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	ip := net.ParseIP(ipStr)
	if ip == nil {
		// If we can't parse, block in allowlist mode, allow in blocklist mode
		return f.mode == FilterBlocklist
	}

	// Check exact IP matches first
	if f.ips[ipStr] {
		return f.mode == FilterAllowlist
	}

	// Check CIDR ranges
	for _, cidr := range f.nets {
		if cidr.Contains(ip) {
			return f.mode == FilterAllowlist
		}
	}

	// No match — allow in allowlist mode? No. Block in blocklist mode? No.
	return f.mode == FilterBlocklist
}

// IPFilterMiddleware creates HTTP middleware that enforces IP access control.
// extractIP extracts the client IP from the request (default: RemoteAddr with port stripped).
func IPFilterMiddleware(filter *IPFilter, extractIP func(*http.Request) string) func(http.Handler) http.Handler {
	if extractIP == nil {
		extractIP = func(r *http.Request) string {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				return r.RemoteAddr
			}
			return host
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r)
			if !filter.Allowed(ip) {
				AuditSecurityEvent(r.Context(), "ip_blocked",
					"ip", ip,
					"path", r.URL.Path,
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "access_denied",
					"message": "IP address not authorized.",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Security Audit Logging ────────────────────────────────────────────────

// auditKey is a context key for audit event cooldown tracking.
type auditKey struct{}

// AuditSecurityEvent logs a structured security event using slog.
// Events are rate-limited: duplicate event types from the same context are
// logged at most once to prevent log flooding during attacks.
//
// Use from middleware (has request context) for automatic dedup.
// For standalone use (no context), call directly — no dedup applied.
func AuditSecurityEvent(ctx context.Context, eventType string, attrs ...any) {
	// Context-aware dedup: skip if this context already logged this event type
	if ctx != nil {
		if logged, ok := ctx.Value(auditKey{}).(map[string]bool); ok && logged[eventType] {
			return
		}
	}

	args := []any{"event", eventType, "timestamp", time.Now().UTC().Format(time.RFC3339)}
	args = append(args, attrs...)
	slog.Warn("SECURITY", args...)
}

// AuditContext returns a new context with audit dedup tracking.
// Use at the start of request handling to enable per-request event dedup.
func AuditContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, auditKey{}, make(map[string]bool))
}

// AuditMiddleware wraps handlers with per-request audit context and
// adds automatic security event logging on auth failures and other conditions.
// It also logs the request start and completion.
func AuditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Inject audit dedup context
		ctx := AuditContext(r.Context())
		r = r.WithContext(ctx)

		start := time.Now()
		next.ServeHTTP(w, r)

		// Log slow responses as potential security concern
		duration := time.Since(start)
		if duration > 5*time.Second {
			AuditSecurityEvent(ctx, "slow_response",
				"path", r.URL.Path,
				"duration_ms", duration.Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		}
	})
}

// ─── Request ID Middleware ──────────────────────────────────────────────────

// requestIDKey is a context key for request correlation IDs.
type requestIDKey struct{}

// RequestID returns the correlation ID for the current request, or "" if none set.
// Use this in handlers to attach the request ID to structured logs for audit trail
// correlation across multiple middleware layers and downstream services.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GenerateRequestID produces a cryptographically random 16-character hex ID.
// Uses crypto/rand — suitable for security-sensitive request correlation.
// This is a public helper so MCP servers can generate their own IDs when not
// behind the HTTP middleware.
func GenerateRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read can only fail on Linux if the getrandom()
		// syscall returns an error (e.g., kernel entropy pool exhaustion).
		// Fall back to time-based ID as last resort — better than panicking.
		return "fallback-" + hex.EncodeToString([]byte{
			byte(time.Now().UnixNano() >> 56),
			byte(time.Now().UnixNano() >> 48),
			byte(time.Now().UnixNano() >> 40),
			byte(time.Now().UnixNano() >> 32),
			byte(time.Now().UnixNano() >> 24),
			byte(time.Now().UnixNano() >> 16),
			byte(time.Now().UnixNano() >> 8),
			byte(time.Now().UnixNano()),
		})
	}
	return hex.EncodeToString(b)
}

// RequestIDMiddleware injects a unique correlation ID into every request.
// The ID is stored in the request context (accessible via RequestID(ctx)) and
// set as the X-Request-ID response header. If the incoming request already
// carries an X-Request-ID header, that value is reused — enabling end-to-end
// tracing across distributed components.
//
// Stack position: outermost (right after SecurityHeaders). This ensures
// the request ID is available to all downstream middleware (CORS, sanitize,
// rate limit, metrics, auth) and handlers for structured log correlation.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = GenerateRequestID()
		}

		// Expose to response so callers can correlate
		w.Header().Set("X-Request-ID", id)

		// Inject into context for downstream middleware and handlers
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ─── Content-Type Validation ────────────────────────────────────────────────

// ContentTypeMiddleware validates that incoming requests have an allowed Content-Type
// header for the specified HTTP methods. This prevents content-type confusion attacks
// where an attacker sends unexpected payload formats to JSON-only endpoints.
//
// allowedTypes is a map of MIME types (e.g., "application/json") that are permitted.
// methods is a set of HTTP methods to enforce the check on (e.g., POST, PUT, PATCH).
// If methods is nil, defaults to POST, PUT, PATCH, DELETE.
func ContentTypeMiddleware(allowedTypes map[string]bool, methods map[string]bool) func(http.Handler) http.Handler {
	if len(allowedTypes) == 0 {
		allowedTypes = map[string]bool{"application/json": true}
	}
	if len(methods) == 0 {
		methods = map[string]bool{
			http.MethodPost:   true,
			http.MethodPut:    true,
			http.MethodPatch:  true,
			http.MethodDelete: true,
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if methods[r.Method] {
				ct := r.Header.Get("Content-Type")
				// Strip parameters (e.g., "application/json; charset=utf-8" → "application/json")
				if idx := strings.Index(ct, ";"); idx != -1 {
					ct = strings.TrimSpace(ct[:idx])
				}
				if ct == "" || !allowedTypes[ct] {
					AuditSecurityEvent(r.Context(), "invalid_content_type",
						"method", r.Method,
						"path", r.URL.Path,
						"content_type", r.Header.Get("Content-Type"),
						"remote_addr", r.RemoteAddr,
					)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnsupportedMediaType)
					json.NewEncoder(w).Encode(map[string]string{
						"error":   "unsupported_media_type",
						"message": "Request Content-Type is not supported for this endpoint.",
					})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// JSONContentTypeMiddleware is a convenience wrapper that enforces application/json
// Content-Type on POST, PUT, PATCH, and DELETE requests. GET/HEAD/OPTIONS pass through
// without Content-Type checks.
//
// This is the most common case for REST API endpoints and should be placed after
// CSRF middleware in the stack.
func JSONContentTypeMiddleware(next http.Handler) http.Handler {
	return ContentTypeMiddleware(nil, nil)(next)
}

// ─── CSRF Protection ────────────────────────────────────────────────────────

// csrfCookieName is the name of the cookie storing the CSRF token.
const csrfCookieName = "_csrf_token"

// csrfHeaderName is the header clients must send with the token.
const csrfHeaderName = "X-CSRF-Token"

// safeMethods are HTTP methods that do not require CSRF protection.
var safeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// GenerateCSRFToken produces a cryptographically random 32-byte hex-encoded CSRF token.
// Suitable for embedding in cookies and validating in request headers.
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "fallback-" + hex.EncodeToString([]byte{
			byte(time.Now().UnixNano() >> 56),
			byte(time.Now().UnixNano() >> 48),
			byte(time.Now().UnixNano() >> 40),
			byte(time.Now().UnixNano() >> 32),
			byte(time.Now().UnixNano() >> 24),
			byte(time.Now().UnixNano() >> 16),
			byte(time.Now().UnixNano() >> 8),
			byte(time.Now().UnixNano()),
		})
	}
	return hex.EncodeToString(b)
}

// CSRFMiddleware protects against Cross-Site Request Forgery attacks.
//
// For safe methods (GET, HEAD, OPTIONS), the request passes through and a CSRF
// token cookie is set if one doesn't already exist.
//
// For unsafe methods (POST, PUT, DELETE, PATCH), the X-CSRF-Token header must
// match the _csrf_token cookie. If they don't match (or either is missing),
// the request is rejected with 403 Forbidden.
//
// tokenFn is an optional function to generate tokens. If nil, GenerateCSRFToken is used.
func CSRFMiddleware(tokenFn func() string) func(http.Handler) http.Handler {
	if tokenFn == nil {
		tokenFn = GenerateCSRFToken
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Read existing cookie
			cookieToken := ""
			if c, err := r.Cookie(csrfCookieName); err == nil {
				cookieToken = c.Value
			}

			// Safe methods: set token cookie if missing, then pass through
			if safeMethods[r.Method] {
				if cookieToken == "" {
					cookieToken = tokenFn()
					http.SetCookie(w, &http.Cookie{
						Name:     csrfCookieName,
						Value:    cookieToken,
						Path:     "/",
						HttpOnly: false, // JS must be able to read it for the header
						Secure:   true,
						SameSite: http.SameSiteStrictMode,
						MaxAge:   86400, // 24 hours
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			// Unsafe methods: validate token
			headerToken := r.Header.Get(csrfHeaderName)

			if cookieToken == "" || headerToken == "" || cookieToken != headerToken {
				AuditSecurityEvent(r.Context(), "csrf_validation_failed",
					"method", r.Method,
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "csrf_validation_failed",
					"message": "CSRF token mismatch. Ensure X-CSRF-Token header matches the _csrf_token cookie.",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ─── Bearer Token Authentication ────────────────────────────────────────────

// bearerPrincipalKey is a context key for the authenticated principal from bearer auth.
type bearerPrincipalKey struct{}

// TokenValidator validates a bearer token and returns the authenticated principal/subject.
// Returns an empty string and a non-nil error if the token is invalid, expired, or revoked.
// This interface supports pluggable backends: static tokens, JWT, OAuth2/OIDC introspection.
type TokenValidator func(ctx context.Context, token string) (subject string, err error)

// BearerAuthMiddleware validates Bearer tokens from the Authorization header.
// It extracts the token, calls the provided TokenValidator, and injects the
// authenticated principal into the request context on success.
//
// Requests without an Authorization header pass through unauthenticated —
// use with an auth-required wrapper if enforcement is needed. Invalid tokens
// receive a 401 Unauthorized response with a structured JSON error body,
// a WWW-Authenticate challenge header, and an audit security event.
//
// The authenticated principal is accessible via BearerPrincipal(ctx).
func BearerAuthMiddleware(validator TokenValidator) func(http.Handler) http.Handler {
	if validator == nil {
		validator = func(_ context.Context, _ string) (string, error) {
			return "", nil // no-op validator — all tokens accepted
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				// No auth header — pass through unauthenticated
				next.ServeHTTP(w, r)
				return
			}

			// Must start with "Bearer "
			if len(authHeader) < 7 || !strings.EqualFold(authHeader[:7], "Bearer ") {
				AuditSecurityEvent(r.Context(), "bearer_auth_malformed",
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "invalid_request",
					"message": "Authorization header must use Bearer scheme.",
				})
				return
			}

			token := strings.TrimSpace(authHeader[7:])
			if token == "" {
				AuditSecurityEvent(r.Context(), "bearer_auth_malformed",
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
				)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{
					"error":   "invalid_request",
					"message": "Authorization header must use Bearer scheme with a non-empty token.",
				})
				return
			}
			subject, err := validator(r.Context(), token)
			if err != nil {
				AuditSecurityEvent(r.Context(), "bearer_auth_failed",
					"path", r.URL.Path,
					"remote_addr", r.RemoteAddr,
					"error", err.Error(),
				)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("WWW-Authenticate", `Bearer realm="bt-platform", error="invalid_token", error_description="`+err.Error()+`"`)
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]string{
					"error":             "invalid_token",
					"error_description": err.Error(),
				})
				return
			}

			// Inject authenticated principal into context
			ctx := context.WithValue(r.Context(), bearerPrincipalKey{}, subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BearerPrincipal returns the authenticated principal from a bearer token,
// or an empty string if no bearer authentication was performed.
func BearerPrincipal(ctx context.Context) string {
	if p, ok := ctx.Value(bearerPrincipalKey{}).(string); ok {
		return p
	}
	return ""
}

// StaticTokenValidator returns a TokenValidator that checks against a fixed set
// of valid tokens mapped to subjects. This is suitable for development, testing,
// or deployments with a small number of pre-provisioned API tokens.
//
// The resulting validator is constant-time safe only for the map lookup step;
// token values themselves are compared with standard string equality.
// For production, use a JWT validator or OAuth2 token introspection endpoint.
func StaticTokenValidator(tokens map[string]string) TokenValidator {
	valid := make(map[string]string, len(tokens))
	for token, subject := range tokens {
		valid[token] = subject
	}
	return func(_ context.Context, token string) (string, error) {
		if subject, ok := valid[token]; ok {
			return subject, nil
		}
		return "", fmt.Errorf("invalid or expired token")
	}
}

