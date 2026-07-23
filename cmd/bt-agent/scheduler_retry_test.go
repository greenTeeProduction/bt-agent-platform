package main

import (
	"errors"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// Goap fusion cycles are self-persisting work units (plans, worktrees,
// pending patches, refunds) with a natural retry rhythm — the next scheduled
// cycle. An in-slot scheduler retry re-runs a full multi-hour cycle for no
// benefit: on 2026-07-22/23 two healthy ~90-minute attempts were blind-
// retried into SLO failures and false DLQ entry #239. Cycle trees get
// exactly one attempt per slot; every other agent keeps the configured
// retry budget.
func TestSchedulerRetryPolicy_GoapCyclesGetSingleAttempt(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.RetryMaxRetries < 2 {
		cfg.RetryMaxRetries = 3 // make the non-cycle branch observable
	}

	for _, tree := range []string{"domain:goap_fusion_loop", "domain:goap_fusion", "goap_fusion_loop", "goap_fusion"} {
		if got := schedulerRetryPolicy(cfg, tree).MaxRetries; got != 1 {
			t.Fatalf("MaxRetries for cycle tree %q = %d, want 1 (single attempt per slot)", tree, got)
		}
	}

	for _, tree := range []string{"domain:bt_fusion", "domain:self_review", "hermes_update", ""} {
		if got := schedulerRetryPolicy(cfg, tree).MaxRetries; got != cfg.RetryMaxRetries {
			t.Fatalf("MaxRetries for non-cycle tree %q = %d, want configured %d", tree, got, cfg.RetryMaxRetries)
		}
	}
}

// The extracted policy builder must preserve the configured backoff shape —
// it replaces an inline construction in the scheduler closure.
func TestSchedulerRetryPolicy_PreservesConfiguredBackoff(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RetryMaxRetries = 4
	cfg.RetryBaseDelayMs = 250
	cfg.RetryMaxDelayMs = 9000
	cfg.RetryLLMBaseMs = 1250
	cfg.RetryJitter = "no_jitter"

	p := schedulerRetryPolicy(cfg, "domain:hermes_update")
	if p.MaxRetries != 4 {
		t.Fatalf("MaxRetries = %d, want 4", p.MaxRetries)
	}
	if p.Base.Milliseconds() != 250 || p.MaxDelay.Milliseconds() != 9000 || p.LLMBase.Milliseconds() != 1250 {
		t.Fatalf("backoff = base %v, max %v, llm %v — config values lost", p.Base, p.MaxDelay, p.LLMBase)
	}
	if p.Jitter != reliability.NoJitter {
		t.Fatalf("Jitter = %v, want NoJitter mapping preserved", p.Jitter)
	}
	if !p.RetryUnknown {
		t.Fatal("RetryUnknown must stay true (legacy behavior)")
	}
}

// An evidence-shape rejection is deterministic within a binary: the reporter
// and validator ship together, so the validator re-rejects the identical
// report shape on every attempt, and a blind retry re-runs a full cycle only
// to reproduce the rejection (22:37→01:06 on 2026-07-22/23: BOTH retried
// attempts landed real commits and were rejected by the same pre-fix gate).
// The scheduler must still surface an error — the rejection is a genuine
// reporter/validator drift signal worth one DLQ entry — but classify it
// non-retryable so the retry policy stops after the first attempt.
func TestRecordSchedulerAttempt_EvidenceShapeRejectionNotRetried(t *testing.T) {
	if reliability.ErrCatValidation.IsRetryable() {
		t.Fatal("precondition: validation category must be non-retryable")
	}

	slo := engine.GetSLOMetrics("goap-fusion-loop-runner", "domain:goap_fusion_loop")
	output := "## GOAP Fusion Evidence Failed\n\n" + engine.GoapEvidenceShapeRejection + "\n\nPrevious output:\n```\n## Superpowers Implementation Complete\n```"

	err := recordSchedulerAttempt(slo, "failure", nil, output, 1, time.Second)
	if err == nil {
		t.Fatal("an evidence rejection is still a failed attempt — the scheduler must surface an error, just never retry it")
	}
	if cat := reliability.ClassifyError(err); cat != reliability.ErrCatValidation {
		t.Fatalf("ClassifyError = %v, want %v — an uncategorized error here means the retry policy blind-retries the deterministic rejection", cat, reliability.ErrCatValidation)
	}
}

// The scheduler's dead-letter entry must record how many attempts actually
// ran: it hardcoded Attempts: 3, so a single-attempt rejection or a
// deadline-stopped 2-attempt run was archived as "3 attempts", misleading
// every later DLQ forensics pass (false entry #239 reads as retry-exhausted).
func TestSchedulerDeadLetter_RecordsActualAttempts(t *testing.T) {
	attemptErr := errors.New("agent outcome: failure: boom")
	e := schedulerDeadLetter("goap-fusion-loop-runner", "cycle", attemptErr, 1, "abc1234")
	if e.Attempts != 1 {
		t.Fatalf("Attempts = %d, want the real count 1", e.Attempts)
	}
	if e.Agent != "goap-fusion-loop-runner" || e.Task != "cycle" {
		t.Fatalf("agent/task lost: %+v", e)
	}
	if e.Error != attemptErr.Error() {
		t.Fatalf("Error = %q, want %q", e.Error, attemptErr.Error())
	}
	if e.Circuit != "scheduler" || e.BuildRevision != "abc1234" {
		t.Fatalf("circuit/revision lost: %+v", e)
	}
	if e.ID == "" || e.FailedAt.IsZero() {
		t.Fatalf("ID/FailedAt unset: %+v", e)
	}
}
