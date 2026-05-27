// Package security provides rate limiting, input sanitization, and auth utilities
// for the Go BT framework's dashboard and MCP servers.
package security

import (
	"context"
	"encoding/json"
	"net/http"
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
				policy := "max-age=" + itoa(cfg.HSTSMaxAge)
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

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
