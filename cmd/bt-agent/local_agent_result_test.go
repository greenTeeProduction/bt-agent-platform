package main

import (
	"errors"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
)

// A RunOnce error must not cost the caller the result: on 2026-07-16 20:30
// a cycle that correctly returned the goap_fusion_rate_limited carryover had
// its result dropped at this boundary — the scheduler saw outcome "" instead
// of the sentinel, retried a healthy pause 3x, recorded three SLO failures,
// dead-lettered it, and alarmed "failure". The AgentResult must survive
// alongside the error so recordSchedulerAttempt can defer on the sentinel.
func TestLocalAgentResultPreservesOutcomeOnError(t *testing.T) {
	res := &agent.RunResult{
		AgentName: "goap-fusion-loop-runner",
		Outcome:   "goap_fusion_rate_limited",
		Output:    "## GOAP Superpowers Rate Limited\n\nbackoff active until 2026-07-16T18:52:00Z",
		Quality:   0.5,
		Duration:  9 * time.Second,
	}
	runErr := errors.New("agent outcome: goap_fusion_rate_limited: paused on Claude rate limit")

	ar, err := localAgentResult("goap-fusion-loop-runner", "cycle", res, runErr)
	if err == nil || !errors.Is(err, runErr) {
		t.Fatalf("the error must pass through unchanged, got %v", err)
	}
	if ar == nil {
		t.Fatal("the result must survive the error — dropping it loses the rate-limit sentinel")
	}
	if ar.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("Outcome = %q, want the sentinel preserved", ar.Outcome)
	}
	if ar.Output == "" || ar.Success {
		t.Fatalf("Output must carry the run report and Success must be false, got output=%q success=%v", ar.Output, ar.Success)
	}
	if ar.Error == "" {
		t.Fatalf("AgentResult.Error must record the failure for remote-visibility parity")
	}
}

func TestLocalAgentResultNilResultAndSuccessPathsUnchanged(t *testing.T) {
	runErr := errors.New("start acp command failed")
	if ar, err := localAgentResult("a", "t", nil, runErr); ar != nil || !errors.Is(err, runErr) {
		t.Fatalf("nil result must stay nil with the error passed through, got (%v, %v)", ar, err)
	}
	ok := &agent.RunResult{AgentName: "a", Outcome: "success", Output: "done", Quality: 0.9}
	ar, err := localAgentResult("a", "t", ok, nil)
	if err != nil || ar == nil || !ar.Success || ar.Outcome != "success" {
		t.Fatalf("success path must be unchanged, got (%+v, %v)", ar, err)
	}
}
