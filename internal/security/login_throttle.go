// Login throttling — per-IP exponential backoff for login failures.
//
// LoginThrottle tracks failed login attempts by IP address and enforces
// cooldown windows with exponential backoff. This prevents brute-force
// password guessing without blocking legitimate users.
//
// Backoff schedule:
//
//	Attempt 1-3:   1s delay
//	Attempt 4-5:   5s delay
//	Attempt 6-7:   30s delay
//	Attempt 8-9:   2m delay
//	Attempt 10+:   10m delay
//
// Failed attempt counts decay after a configurable window (default: 15 min
// since last failure) so occasional typos don't permanently penalize an IP.
package security

import (
	"math"
	"net"
	"sync"
	"time"
)

// LoginThrottleState captures the throttle state for a single IP.
type LoginThrottleState struct {
	IP           string        `json:"ip"`
	FailedCount  int           `json:"failed_count"`
	LastFailure  time.Time     `json:"last_failure"`
	CooldownCoef float64       `json:"cooldown_coef"` // 1, 2, 4, ... exponential
	IsBlocked    bool          `json:"is_blocked"`
	BlockedUntil time.Time     `json:"blocked_until,omitempty"`
	Remaining    time.Duration `json:"remaining,omitempty"`
}

// LoginThrottleConfig configures the per-IP login throttle.
type LoginThrottleConfig struct {
	// MaxFailuresBeforeLockout is the number of consecutive failures before the
	// IP is locked out entirely (blocked until manual reset or timeout).
	// Default: 20.
	MaxFailuresBeforeLockout int

	// LockoutDuration is how long an IP is blocked after hitting the lockout
	// threshold. Default: 30 minutes.
	LockoutDuration time.Duration

	// DecayWindow is how long without a failure before the count resets to 0.
	// Default: 15 minutes.
	DecayWindow time.Duration

	// CooldownSteps defines the failure counts at which the cooldown multiplier
	// doubles. Default: [3, 5, 7, 9] — after 3 failures, delay=1s; after 5,
	// delay=5s; after 7, delay=30s; after 9, delay=2m; after 10+, delay=10m.
	CooldownSteps []int

	// CooldownBase is the base delay in seconds applied at step 1.
	// Multiplied by 2^(step-1) for exponential backoff.
	// Default: 1 second.
	CooldownBase time.Duration
}

// DefaultLoginThrottleConfig returns sensible defaults for a dashboard login.
func DefaultLoginThrottleConfig() LoginThrottleConfig {
	return LoginThrottleConfig{
		MaxFailuresBeforeLockout: 20,
		LockoutDuration:          30 * time.Minute,
		DecayWindow:              15 * time.Minute,
		CooldownSteps:            []int{3, 5, 7, 9},
		CooldownBase:             1 * time.Second,
	}
}

// LoginThrottle tracks per-IP failed login attempts with exponential backoff.
// Safe for concurrent use.
type LoginThrottle struct {
	mu     sync.Mutex
	states map[string]*LoginThrottleState
	cfg    LoginThrottleConfig
}

// NewLoginThrottle creates a new login throttle with the given config.
func NewLoginThrottle(cfg LoginThrottleConfig) *LoginThrottle {
	return &LoginThrottle{
		states: make(map[string]*LoginThrottleState),
		cfg:    cfg,
	}
}

// RecordFailure records a failed login attempt from the given IP.
func (lt *LoginThrottle) RecordFailure(ip string) {
	ip = stripPort(ip)
	now := time.Now()

	lt.mu.Lock()
	defer lt.mu.Unlock()

	st, ok := lt.states[ip]
	if !ok {
		st = &LoginThrottleState{
			IP:          ip,
			FailedCount: 0,
		}
		lt.states[ip] = st
	}

	// Reset count if the decay window has elapsed since the last failure.
	if now.Sub(st.LastFailure) > lt.cfg.DecayWindow {
		st.FailedCount = 0
		st.CooldownCoef = 0
	}

	st.FailedCount++
	st.LastFailure = now
	st.CooldownCoef = math.Pow(2, float64(cooldownStepIndex(st.FailedCount, lt.cfg.CooldownSteps)))
	st.Remaining = lt.cooldownDuration(st)

	if st.FailedCount >= lt.cfg.MaxFailuresBeforeLockout {
		st.IsBlocked = true
		st.BlockedUntil = now.Add(lt.cfg.LockoutDuration)
		st.Remaining = lt.cfg.LockoutDuration
	}
}

// RecordSuccess records a successful login from the given IP, clearing the
// throttle state.
func (lt *LoginThrottle) RecordSuccess(ip string) {
	ip = stripPort(ip)
	lt.mu.Lock()
	defer lt.mu.Unlock()
	delete(lt.states, ip)
}

// IsBlocked returns true if the IP is currently blocked (lockout threshold
// reached and lockout window not yet elapsed).
func (lt *LoginThrottle) IsBlocked(ip string) bool {
	ip = stripPort(ip)
	lt.mu.Lock()
	defer lt.mu.Unlock()

	st, ok := lt.states[ip]
	if !ok {
		return false
	}

	// Check lockout
	if st.IsBlocked && time.Now().Before(st.BlockedUntil) {
		return true
	}

	// Lockout expired — clean up
	if st.IsBlocked {
		delete(lt.states, ip)
		return false
	}

	return false
}

// RemainingCooldown returns the remaining cooldown duration for the given IP.
// Returns 0 if the IP has no throttle state or is not in cooldown.
func (lt *LoginThrottle) RemainingCooldown(ip string) time.Duration {
	ip = stripPort(ip)
	lt.mu.Lock()
	defer lt.mu.Unlock()

	st, ok := lt.states[ip]
	if !ok {
		return 0
	}

	// Recompute remaining based on current time
	elapsed := time.Since(st.LastFailure)
	duration := lt.cooldownDuration(st)
	remaining := duration - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// State returns the throttle state for the given IP, or nil if none exists.
func (lt *LoginThrottle) State(ip string) *LoginThrottleState {
	ip = stripPort(ip)
	lt.mu.Lock()
	defer lt.mu.Unlock()

	cp, ok := lt.states[ip]
	if !ok {
		return nil
	}
	// Return a copy to avoid data races on reads
	clone := *cp
	return &clone
}

// CleanupExpired removes all entries whose decay window has elapsed since
// the last failure. Returns the count of removed entries.
func (lt *LoginThrottle) CleanupExpired() int {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	now := time.Now()
	removed := 0
	for ip, st := range lt.states {
		// If blocked but lockout expired, remove
		if st.IsBlocked && now.After(st.BlockedUntil) {
			delete(lt.states, ip)
			removed++
			continue
		}
		// If not blocked and decay window elapsed, remove
		if !st.IsBlocked && now.Sub(st.LastFailure) > lt.cfg.DecayWindow {
			delete(lt.states, ip)
			removed++
		}
	}
	return removed
}

// Count returns the number of tracked IPs.
func (lt *LoginThrottle) Count() int {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	return len(lt.states)
}

// stripPort removes the port component from an address string.
// Handles both IPv4 and bracketed IPv6 addresses.
func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // fall back to raw string
	}
	return host
}

// cooldownDuration returns the cooldown duration for the given state.
func (lt *LoginThrottle) cooldownDuration(st *LoginThrottleState) time.Duration {
	if st.IsBlocked {
		return time.Until(st.BlockedUntil)
	}

	// Base delay per step
	steps := lt.cfg.CooldownSteps
	stepIdx := cooldownStepIndex(st.FailedCount, steps)
	if stepIdx == 0 {
		return 0
	}
	d := lt.cfg.CooldownBase * time.Duration(math.Pow(2, float64(stepIdx-1)))
	return d
}

// cooldownStepIndex finds which cooldown step the failure count falls into.
// Returns 0 if the count is below the first step (no cooldown).
func cooldownStepIndex(count int, steps []int) int {
	if count <= 0 {
		return 0
	}
	for i, s := range steps {
		if count <= s {
			return i + 1
		}
	}
	return len(steps) + 1 // beyond all defined steps — use max
}
