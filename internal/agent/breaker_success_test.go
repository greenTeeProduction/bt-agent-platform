package agent

import (
	"errors"
	"testing"
)

// TestIsBreakerSuccess pins the single source of truth for "did this run keep
// the agent's circuit breaker closed?" so the scheduler and the dashboard
// executor can no longer drift apart. Healthy terminal outcomes (success,
// no_change, degraded) with no run error keep the breaker closed; the
// rate-limit carryover is an expected pause and also keeps it closed even
// though it travels with an outcome that is not "success"; genuine failures and
// any healthy outcome that still carried a run error count against the breaker.
func TestIsBreakerSuccess(t *testing.T) {
	tests := []struct {
		name    string
		outcome string
		runErr  error
		want    bool
	}{
		{"success", "success", nil, true},
		{"no_change is healthy", "no_change", nil, true},
		{"degraded is healthy", "degraded", nil, true},
		{"failure", "failure", nil, false},
		{"timeout", "timeout", nil, false},
		{"empty outcome", "", nil, false},
		{"success but errored", "success", errors.New("boom"), false},
		{"no_change but errored", "no_change", errors.New("boom"), false},
		{"rate-limit carryover keeps breaker closed", "goap_fusion_rate_limited", nil, true},
		{"rate-limit carryover closed even with error", "goap_fusion_rate_limited", errors.New("paused"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsBreakerSuccess(tc.outcome, tc.runErr); got != tc.want {
				t.Fatalf("IsBreakerSuccess(%q, %v) = %v, want %v", tc.outcome, tc.runErr, got, tc.want)
			}
		})
	}
}
