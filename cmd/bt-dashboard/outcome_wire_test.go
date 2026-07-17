package main

import (
	"errors"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
)

// TestAgentExecuteResult_PreservesOutcome pins the wire contract to
// cmd/bt-agent's localAgentResult: the raw Outcome travels across the
// boundary (so a RemoteExecutor peer can classify rate-limit carryover and
// healthy non-"success" outcomes itself) and Success stays strict
// (err == nil && outcome == "success"). The pre-fix adapter hand-rolled
// Success as success||completed and dropped Outcome entirely, so a peer's
// routedRunResult fabricated "failed" for healthy runs — reintroducing the
// 2026-07-16 carryover-loss bug across the dashboard boundary.
func TestAgentExecuteResult_PreservesOutcome(t *testing.T) {
	tests := []struct {
		name        string
		outcome     string
		err         error
		wantSuccess bool
	}{
		{"success", "success", nil, true},
		{"no_change travels as outcome, not success", "no_change", nil, false},
		{"rate-limit carryover travels as outcome", agent.RateLimitCarryoverOutcome, nil, false},
		{"completed stays strict", "completed", nil, false},
		{"success with error is not success", "success", errors.New("x"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := agentExecuteResult("a", "t", &agent.RunResult{Outcome: tc.outcome, Output: "o"}, time.Second, tc.err)
			if res.Outcome != tc.outcome {
				t.Fatalf("Outcome = %q, want %q preserved on the wire (reliability.AgentResult.Outcome exists for exactly this)", res.Outcome, tc.outcome)
			}
			if res.Success != tc.wantSuccess {
				t.Fatalf("Success = %v, want %v (strict err==nil && outcome==\"success\", matching cmd/bt-agent's localAgentResult)", res.Success, tc.wantSuccess)
			}
		})
	}
}

// TestDashboardLocalAgentResult_PreservesOutcome pins the same contract for
// the AgentRouter's local-executor adapter, which previously hand-rolled
// success||completed and dropped Outcome.
func TestDashboardLocalAgentResult_PreservesOutcome(t *testing.T) {
	res, err := dashboardLocalAgentResult("a", "t", &agent.RunResult{Outcome: agent.RateLimitCarryoverOutcome, Output: "o"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != agent.RateLimitCarryoverOutcome {
		t.Fatalf("Outcome = %q, want the carryover sentinel preserved", res.Outcome)
	}
	if res.Success {
		t.Fatal("Success must stay strict (outcome != \"success\")")
	}
}

// TestSprintTaskDisposition pins the sprint dispatch loop's classification to
// the shared classifier: healthy non-"success" outcomes complete the task, a
// rate-limit carryover defers it for a later sprint instead of marking it
// failed/Blocked (the same run was just recorded as a breaker and metric
// success by the executor), and genuine failures fail it.
func TestSprintTaskDisposition(t *testing.T) {
	tests := []struct {
		name    string
		outcome string
		err     error
		want    sprintDisposition
	}{
		{"success completes", "success", nil, sprintCompleted},
		{"no_change completes", "no_change", nil, sprintCompleted},
		{"degraded completes", "degraded", nil, sprintCompleted},
		{"completed completes", "completed", nil, sprintCompleted},
		{"failure fails", "failure", nil, sprintFailed},
		{"timeout with error fails", "timeout", errors.New("timed out"), sprintFailed},
		{"success with error fails", "success", errors.New("x"), sprintFailed},
		{"carryover defers", agent.RateLimitCarryoverOutcome, nil, sprintDeferred},
		{"carryover with error still defers", agent.RateLimitCarryoverOutcome, errors.New("paused"), sprintDeferred},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sprintTaskDisposition(tc.outcome, tc.err); got != tc.want {
				t.Fatalf("sprintTaskDisposition(%q, %v) = %v, want %v", tc.outcome, tc.err, got, tc.want)
			}
		})
	}
}
