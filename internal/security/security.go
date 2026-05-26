// Package security provides rate limiting, input sanitization, and auth utilities
// for the Go BT framework's dashboard and MCP servers.
package security

import (
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
