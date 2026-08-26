package reliability

import (
	"errors"
	"testing"
	"time"
)

// TestCircuitBreaker_RecordOutcome pins the shared record-once contract every
// Allow() caller needs: a consumed probe must always be resolved, and only
// infrastructure (retryable) failures walk the breaker toward open.
func TestCircuitBreaker_RecordOutcome(t *testing.T) {
	retryableErr := NewCategorizedError(ErrCatLLM, errors.New("api status 503"))
	validationErr := NewCategorizedError(ErrCatValidation, errors.New("api status 400"))

	t.Run("nil error records success", func(t *testing.T) {
		cb := NewCircuitBreaker("t", 1, time.Minute)
		cb.RecordFailure() // threshold 1 -> Open
		cb.RecordOutcome(nil)
		if cb.State() != CircuitClosed {
			t.Fatalf("state = %v, want closed after nil-error outcome", cb.State())
		}
	})

	t.Run("retryable error records categorized failure", func(t *testing.T) {
		cb := NewCircuitBreaker("t", 3, time.Minute)
		cb.RecordOutcome(retryableErr)
		if cb.FailureCount() != 1 {
			t.Fatalf("FailureCount = %d, want 1", cb.FailureCount())
		}
		if got := cb.CategoryFailureCounts()[ErrCatLLM]; got != 1 {
			t.Fatalf("category llm count = %d, want 1 (RecordOutcome must preserve the classified category)", got)
		}
	})

	t.Run("caller-side error while closed is not counted", func(t *testing.T) {
		cb := NewCircuitBreaker("t", 3, time.Minute)
		authErr := NewCategorizedError(ErrCatAuth, errors.New("api status 401"))
		for range 5 {
			cb.RecordOutcome(validationErr)
			cb.RecordOutcome(authErr)
		}
		if cb.State() != CircuitClosed || cb.FailureCount() != 0 {
			t.Fatalf("state=%v failures=%d, want closed/0 (caller-side errors must not walk the breaker open)", cb.State(), cb.FailureCount())
		}
	})

	t.Run("unknown-category error counts as failure", func(t *testing.T) {
		// An unclassifiable error ("no choices in response", junk model
		// output) is evidence of a broken backend, not a bad caller — it must
		// walk the breaker toward open, or a persistently junk-emitting
		// backend never trips it.
		cb := NewCircuitBreaker("t", 3, time.Minute)
		for range 3 {
			cb.RecordOutcome(errors.New("zk9x qflm"))
		}
		if cb.State() != CircuitOpen {
			t.Fatalf("state = %v, want open after 3 unknown-category failures", cb.State())
		}
	})

	t.Run("non-retryable error resolves a half-open probe as success", func(t *testing.T) {
		cb := NewCircuitBreaker("t", 1, time.Millisecond)
		cb.RecordFailure() // -> Open
		time.Sleep(5 * time.Millisecond)
		if !cb.Allow() { // consumes the half-open probe
			t.Fatal("expected the half-open probe to be granted")
		}
		// The probe's request failed with a caller-side (non-retryable) error:
		// the backend answered, so infrastructure is healthy — the probe must
		// be resolved, not leaked (a leaked probe wedges the breaker HalfOpen
		// forever, since Allow() in HalfOpen always returns false).
		cb.RecordOutcome(validationErr)
		if cb.State() != CircuitClosed {
			t.Fatalf("state = %v, want closed (probe answered by a live backend must resolve the half-open state)", cb.State())
		}
		if !cb.Allow() {
			t.Fatal("breaker must allow requests after the probe resolved")
		}
	})

	t.Run("retryable error while half-open reopens", func(t *testing.T) {
		cb := NewCircuitBreaker("t", 1, time.Millisecond)
		cb.RecordFailure()
		time.Sleep(5 * time.Millisecond)
		if !cb.Allow() {
			t.Fatal("expected the half-open probe to be granted")
		}
		cb.RecordOutcome(retryableErr)
		if cb.State() != CircuitOpen {
			t.Fatalf("state = %v, want open (a failed probe must reopen the breaker)", cb.State())
		}
	})
}
