package security

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewLoginThrottle_Defaults(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	if lt == nil {
		t.Fatal("NewLoginThrottle returned nil")
	}
	if lt.states == nil {
		t.Error("states map should be initialized")
	}
	if lt.cfg.MaxFailuresBeforeLockout != 20 {
		t.Errorf("MaxFailuresBeforeLockout = %d, want 20", lt.cfg.MaxFailuresBeforeLockout)
	}
	if lt.cfg.DecayWindow != 15*time.Minute {
		t.Errorf("DecayWindow = %v, want 15m", lt.cfg.DecayWindow)
	}
}

func TestLoginThrottle_RecordFailure(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	ip := "192.168.1.1"

	lt.RecordFailure(ip)
	st := lt.State(ip)
	if st == nil {
		t.Fatal("expected state after first failure")
	}
	if st.FailedCount != 1 {
		t.Errorf("FailedCount = %d, want 1", st.FailedCount)
	}
	if st.IsBlocked {
		t.Error("should not be blocked after 1 failure")
	}
}

func TestLoginThrottle_MultipleFailuresIncrement(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	ip := "10.0.0.1"

	for i := 1; i <= 5; i++ {
		lt.RecordFailure(ip)
		st := lt.State(ip)
		if st == nil {
			t.Fatalf("expected state after failure %d", i)
		}
		if st.FailedCount != i {
			t.Errorf("after %d failures: FailedCount = %d, want %d", i, st.FailedCount, i)
		}
	}
}

func TestLoginThrottle_Lockout(t *testing.T) {
	cfg := DefaultLoginThrottleConfig()
	cfg.MaxFailuresBeforeLockout = 5
	cfg.LockoutDuration = 10 * time.Minute
	lt := NewLoginThrottle(cfg)
	ip := "10.0.0.2"

	// 5 failures → should be blocked
	for i := 0; i < 5; i++ {
		lt.RecordFailure(ip)
	}
	if !lt.IsBlocked(ip) {
		t.Error("expected IP to be blocked after 5 failures")
	}

	st := lt.State(ip)
	if st == nil {
		t.Fatal("expected state for blocked IP")
	}
	if !st.IsBlocked {
		t.Error("expected IsBlocked=true")
	}
	if st.BlockedUntil.IsZero() {
		t.Error("expected non-zero BlockedUntil")
	}
}

func TestLoginThrottle_LockoutExpires(t *testing.T) {
	cfg := DefaultLoginThrottleConfig()
	cfg.MaxFailuresBeforeLockout = 3
	cfg.LockoutDuration = 50 * time.Millisecond
	lt := NewLoginThrottle(cfg)
	ip := "10.0.0.3"

	for i := 0; i < 3; i++ {
		lt.RecordFailure(ip)
	}
	if !lt.IsBlocked(ip) {
		t.Error("expected blocked immediately after lockout")
	}

	time.Sleep(60 * time.Millisecond)

	if lt.IsBlocked(ip) {
		t.Error("expected IP to be unblocked after lockout expired")
	}
}

func TestLoginThrottle_RecordSuccessClears(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	ip := "10.0.0.4"

	lt.RecordFailure(ip)
	lt.RecordFailure(ip)
	lt.RecordFailure(ip)

	if lt.State(ip) == nil {
		t.Fatal("expected state after 3 failures")
	}

	lt.RecordSuccess(ip)

	if lt.State(ip) != nil {
		t.Error("expected state cleared after success")
	}
	if lt.IsBlocked(ip) {
		t.Error("should not be blocked after success")
	}
}

func TestLoginThrottle_IsBlockedNoState(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	if lt.IsBlocked("10.0.0.5") {
		t.Error("expected not blocked for IP with no state")
	}
}

func TestLoginThrottle_RemainingCooldown(t *testing.T) {
	cfg := DefaultLoginThrottleConfig()
	cfg.CooldownBase = 10 * time.Second // make measurable
	lt := NewLoginThrottle(cfg)
	ip := "10.0.0.6"

	// 3 failures → first cooldown step: 10s
	for i := 0; i < 3; i++ {
		lt.RecordFailure(ip)
	}

	remaining := lt.RemainingCooldown(ip)
	if remaining <= 0 {
		t.Errorf("expected positive remaining cooldown, got %v", remaining)
	}
	if remaining > 12*time.Second { // allow small timing variance
		t.Errorf("expected ~10s cooldown, got %v", remaining)
	}

	// No state for clean IP
	if lt.RemainingCooldown("10.0.0.7") != 0 {
		t.Error("expected 0 remaining for clean IP")
	}
}

func TestLoginThrottle_CleanupExpired(t *testing.T) {
	cfg := DefaultLoginThrottleConfig()
	cfg.DecayWindow = 1 * time.Millisecond // very short decay
	cfg.MaxFailuresBeforeLockout = 10      // avoid lockout
	lt := NewLoginThrottle(cfg)
	ip := "10.0.0.8"

	lt.RecordFailure(ip)
	lt.RecordFailure(ip)

	if lt.Count() != 1 {
		t.Errorf("expected 1 tracked IP, got %d", lt.Count())
	}

	time.Sleep(2 * time.Millisecond)

	removed := lt.CleanupExpired()
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if lt.Count() != 0 {
		t.Errorf("expected 0 tracked IPs after cleanup, got %d", lt.Count())
	}
}

func TestLoginThrottle_ConcurrentAccess(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	var wg sync.WaitGroup

	// 10 goroutines hitting 5 different IPs
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := fmt.Sprintf("192.168.1.%d", n%5)
			lt.RecordFailure(ip)
			lt.IsBlocked(ip)
			lt.RemainingCooldown(ip)
			lt.State(ip)
			lt.RecordSuccess(ip)
		}(i)
	}
	wg.Wait()

	// Should not panic or deadlock
	t.Logf("Concurrent test completed, count=%d", lt.Count())
}

func TestLoginThrottle_DifferentIPsIndependent(t *testing.T) {
	lt := NewLoginThrottle(DefaultLoginThrottleConfig())
	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"

	for i := 0; i < 5; i++ {
		lt.RecordFailure(ip1)
	}

	if st := lt.State(ip2); st != nil {
		t.Error("ip2 should still be clean")
	}
	if lt.RemainingCooldown(ip2) != 0 {
		t.Error("ip2 should have 0 cooldown")
	}

	st := lt.State(ip1)
	if st == nil || st.FailedCount != 5 {
		t.Errorf("ip1 FailedCount = %d, want 5", st.FailedCount)
	}
}

func TestStripPort(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"10.0.0.1:443", "10.0.0.1"},
		{"[::1]:8080", "::1"},
		{"[2001:db8::1]:8080", "2001:db8::1"},
		{"10.0.0.1", "10.0.0.1"}, // no port
		{"::1", "::1"},           // IPv6 no port
		{"", ""},
	}

	for _, tt := range tests {
		result := stripPort(tt.input)
		if result != tt.expected {
			t.Errorf("stripPort(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestCooldownStepIndex(t *testing.T) {
	steps := []int{3, 5, 7, 9}

	tests := []struct {
		count int
		want  int
	}{
		{0, 0},
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 2},
		{5, 2},
		{6, 3},
		{7, 3},
		{8, 4},
		{9, 4},
		{10, 5},
		{20, 5},
		{100, 5},
	}

	for _, tt := range tests {
		got := cooldownStepIndex(tt.count, steps)
		if got != tt.want {
			t.Errorf("cooldownStepIndex(%d, %v) = %d, want %d", tt.count, steps, got, tt.want)
		}
	}
}

func TestLoginThrottle_DecayWindowReset(t *testing.T) {
	cfg := DefaultLoginThrottleConfig()
	cfg.DecayWindow = 10 * time.Millisecond
	lt := NewLoginThrottle(cfg)
	ip := "10.0.0.9"

	lt.RecordFailure(ip)
	lt.RecordFailure(ip)

	// Wait for decay window to pass
	time.Sleep(15 * time.Millisecond)

	lt.RecordFailure(ip)
	st := lt.State(ip)
	if st == nil {
		t.Fatal("expected state after new failure")
	}
	// Should have reset to 1 because decay window elapsed
	if st.FailedCount != 1 {
		t.Errorf("after decay + new failure: FailedCount = %d, want 1 (was reset)", st.FailedCount)
	}
}

func TestLoginThrottle_RateLimitMiddleware(t *testing.T) {
	// Verify the throttle can be used practically:
	// simulate a rapid burst of 15 attempts from one IP
	cfg := DefaultLoginThrottleConfig()
	cfg.MaxFailuresBeforeLockout = 10
	cfg.LockoutDuration = 5 * time.Minute
	lt := NewLoginThrottle(cfg)
	ip := "10.0.0.10"

	for i := 0; i < 15; i++ {
		lt.RecordFailure(ip)
	}

	if !lt.IsBlocked(ip) {
		t.Error("expected IP to be blocked after 10+ failures")
	}
	st := lt.State(ip)
	if st == nil {
		t.Fatal("expected non-nil state after burst")
	}
	if st.FailedCount != 15 {
		t.Errorf("FailedCount = %d, want 15", st.FailedCount)
	}
}
