package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

func TestIsGoapNotebookLMQuotaError(t *testing.T) {
	quota := []string{
		"Error: Google rejected the query (error code 8: RESOURCE_EXHAUSTED) . This may \nindicate account-level restrictions on programmatic access.",
		`{"error": "Google rejected the query (error code 8: RESOURCE_EXHAUSTED) [type.googleapis.com/google.internal.labs.tailwind.orchestration.v1.UserDisplayableError]."}`,
	}
	for _, out := range quota {
		if !isGoapNotebookLMQuotaError(out) {
			t.Fatalf("expected quota error for %q", out)
		}
	}

	notQuota := []string{
		// non-quota failures
		"Error: Query failed: Authentication expired. Run 'nlm login' in your terminal",
		`{"error": "NotebookLM circuit breaker open", "retry_after": "5m0s"}`,
		// a clean successful answer is not a quota error (note: answers that
		// MENTION resource_exhausted are already failures per
		// isGoapNotebookLMFailure — the fix-bd8c5b6 defense against quota text
		// leaking into syntheses — so they classify as quota errors too)
		`{"answer":"GOAL: Add a test\nGAP: missing test evidence with error budgets [1]"}`,
	}
	for _, out := range notQuota {
		if isGoapNotebookLMQuotaError(out) {
			t.Fatalf("did not expect quota classification for %q", out)
		}
	}
}

func TestNextNlmQuotaReset_PacificMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("tz database unavailable")
	}
	// 2026-07-02 10:15 PT → reset at 2026-07-03 00:00 PT
	now := time.Date(2026, 7, 2, 10, 15, 0, 0, loc)
	got := nextNlmQuotaReset(now)
	want := time.Date(2026, 7, 3, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("nextNlmQuotaReset = %v, want %v", got, want)
	}
	if !got.After(now) {
		t.Fatalf("reset %v must be after now %v", got, now)
	}
}

func TestNlmQuotaState_PersistsAcrossRuns(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveNlmQuotaExhausted(run1, time.Now())

	if !nlmQuotaExhausted(run2) {
		t.Fatal("quota exhaustion saved by run 1 must be visible to run 2")
	}
}

func TestNlmQuotaState_ExpiredWindowNotExhausted(t *testing.T) {
	bb := &Blackboard{}
	// ChainState fallback path: store an already-past timestamp directly
	setGoapState(bb, "nlm_quota_until", time.Now().Add(-time.Hour).Format(time.RFC3339))
	if nlmQuotaExhausted(bb) {
		t.Fatal("a past quota-until timestamp must not report exhausted")
	}
}

func TestNlmQuotaState_EmptyAndGarbageNotExhausted(t *testing.T) {
	bb := &Blackboard{}
	if nlmQuotaExhausted(bb) {
		t.Fatal("empty state must not report exhausted")
	}
	setGoapState(bb, "nlm_quota_until", "not-a-timestamp")
	if nlmQuotaExhausted(bb) {
		t.Fatal("unparseable state must not report exhausted")
	}
}

func TestLastReviewedSHA_RoundTrip(t *testing.T) {
	mgr := blackboard.NewManager(nil)
	run1 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-1", "", "goap-loop")}
	run2 := &Blackboard{BB: blackboard.NewHandle(mgr, "run-2", "", "goap-loop")}

	saveLastReviewedSHA(run1, "abc123")
	if got := loadLastReviewedSHA(run2); got != "abc123" {
		t.Fatalf("loadLastReviewedSHA = %q, want abc123", got)
	}

	if got := loadLastReviewedSHA(&Blackboard{}); got != "" {
		t.Fatalf("empty blackboard must return empty SHA, got %q", got)
	}
	if !strings.HasPrefix("goap_fusion_last_reviewed_sha", "goap_fusion_") {
		t.Fatal("key naming sanity")
	}
}
