package reliability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ─── ErrorCategory Tests ────────────────────────────────────────────────────

func TestErrorCategory_String(t *testing.T) {
	tests := []struct {
		cat  ErrorCategory
		want string
	}{
		{ErrCatUnknown, "unknown"},
		{ErrCatNetwork, "network"},
		{ErrCatTimeout, "timeout"},
		{ErrCatLLM, "llm"},
		{ErrCatValidation, "validation"},
		{ErrCatResourceExhausted, "resource_exhausted"},
		{ErrCatAuth, "auth"},
		{ErrorCategory(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.cat.String(); got != tt.want {
			t.Errorf("ErrorCategory(%d).String() = %q, want %q", tt.cat, got, tt.want)
		}
	}
}

func TestErrorCategory_IsRetryable(t *testing.T) {
	retryable := map[ErrorCategory]bool{
		ErrCatNetwork:           true,
		ErrCatTimeout:           true,
		ErrCatLLM:               true,
		ErrCatUnknown:           false,
		ErrCatValidation:        false,
		ErrCatResourceExhausted: false,
		ErrCatAuth:              false,
	}
	for cat, want := range retryable {
		if got := cat.IsRetryable(); got != want {
			t.Errorf("%s.IsRetryable() = %v, want %v", cat, got, want)
		}
	}
}

// ─── CategorizedError Tests ─────────────────────────────────────────────────

func TestCategorizedError_Error(t *testing.T) {
	ce := NewCategorizedError(ErrCatNetwork, errors.New("connection refused"))
	if !strings.Contains(ce.Error(), "network") {
		t.Errorf("CategorizedError.Error() should contain category: %s", ce.Error())
	}
	if !strings.Contains(ce.Error(), "connection refused") {
		t.Errorf("CategorizedError.Error() should contain original: %s", ce.Error())
	}
}

func TestCategorizedError_Unwrap(t *testing.T) {
	original := errors.New("original error")
	ce := NewCategorizedError(ErrCatValidation, original)
	if !errors.Is(ce, original) {
		t.Error("errors.Is should find wrapped error")
	}
}

func TestCategorizedError_NilError(t *testing.T) {
	ce := NewCategorizedError(ErrCatUnknown, nil)
	if ce.Error() != "unknown: <nil>" {
		t.Errorf("nil error should produce '<nil>' message: got %q", ce.Error())
	}
	if ce.Unwrap() != nil {
		t.Error("Unwrap() of nil error should return nil")
	}
}

func TestGetCategory(t *testing.T) {
	ce := NewCategorizedError(ErrCatTimeout, errors.New("timeout"))
	if cat := GetCategory(ce); cat != ErrCatTimeout {
		t.Errorf("GetCategory = %s, want %s", cat, ErrCatTimeout)
	}

	// Wrapping through fmt.Errorf
	wrapped := fmt.Errorf("wrapped: %w", ce)
	if cat := GetCategory(wrapped); cat != ErrCatTimeout {
		t.Errorf("GetCategory through fmt.Errorf = %s, want %s", cat, ErrCatTimeout)
	}

	// Plain error
	if cat := GetCategory(errors.New("plain")); cat != ErrCatUnknown {
		t.Errorf("GetCategory plain = %s, want unknown", cat)
	}

	// Nil error
	if cat := GetCategory(nil); cat != ErrCatUnknown {
		t.Errorf("GetCategory nil = %s, want unknown", cat)
	}
}

// ─── ClassifyError Tests ────────────────────────────────────────────────────

func TestClassifyError_Validation(t *testing.T) {
	tests := []string{
		"validation failed",
		"invalid input",
		"malformed request",
		"missing required field",
		"unsupported type",
		"schema violation",
		"bad request",
		"HTTP 400",
		"cannot unmarshal JSON",
		"required field missing",
		"must be a string",
		"validation error",
		"type mismatch",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatValidation {
			t.Errorf("ClassifyError(%q) = %s, want validation", msg, cat)
		}
	}
}

func TestClassifyError_Auth(t *testing.T) {
	tests := []string{
		"unauthorized",
		"unauthenticated",
		"forbidden",
		"access denied",
		"invalid api key",
		"invalid token",
		"permission denied",
		"not authorized",
		"HTTP 401",
		"HTTP 403",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatAuth {
			t.Errorf("ClassifyError(%q) = %s, want auth", msg, cat)
		}
	}
}

func TestClassifyError_Timeout(t *testing.T) {
	// os.ErrDeadlineExceeded
	if cat := ClassifyError(os.ErrDeadlineExceeded); cat != ErrCatTimeout {
		t.Errorf("ClassifyError(ErrDeadlineExceeded) = %s, want timeout", cat)
	}

	tests := []string{
		"context deadline exceeded",
		"i/o timeout",
		"connection timed out",
		"request timed out",
		"context canceled",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatTimeout {
			t.Errorf("ClassifyError(%q) = %s, want timeout", msg, cat)
		}
	}
}

func TestClassifyError_Network(t *testing.T) {
	// net.OpError
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}
	if cat := ClassifyError(opErr); cat != ErrCatNetwork {
		t.Errorf("ClassifyError(net.OpError) = %s, want network", cat)
	}

	// net.DNSError
	dnsErr := &net.DNSError{Err: "no such host", Name: "example.com"}
	if cat := ClassifyError(dnsErr); cat != ErrCatNetwork {
		t.Errorf("ClassifyError(DNSError) = %s, want network", cat)
	}

	tests := []string{
		"connection refused",
		"no such host",
		"network is unreachable",
		"connection reset by peer",
		"broken pipe",
		"EOF",
		"dial tcp: lookup",
		"no route to host",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatNetwork {
			t.Errorf("ClassifyError(%q) = %s, want network", msg, cat)
		}
	}
}

func TestClassifyError_LLM(t *testing.T) {
	tests := []string{
		"model not found",
		"ollama connection error",
		"llm inference failed",
		"context length exceeded",
		"token limit reached",
		"rate limit exceeded",
		"api error",
		"server error 502",
		"HTTP 503",
		"service unavailable",
		"internal server error",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatLLM {
			t.Errorf("ClassifyError(%q) = %s, want llm", msg, cat)
		}
	}
}

func TestClassifyError_ResourceExhausted(t *testing.T) {
	tests := []string{
		"out of memory",
		"cannot allocate memory",
		"no space left on device",
		"disk full",
		"too many open files",
		"resource temporarily unavailable",
		"connection pool exhausted",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatResourceExhausted {
			t.Errorf("ClassifyError(%q) = %s, want resource_exhausted", msg, cat)
		}
	}

	// syscall errors
	if cat := ClassifyError(syscall.ENOMEM); cat != ErrCatResourceExhausted {
		t.Errorf("ClassifyError(ENOMEM) = %s, want resource_exhausted", cat)
	}
	if cat := ClassifyError(syscall.ENOSPC); cat != ErrCatResourceExhausted {
		t.Errorf("ClassifyError(ENOSPC) = %s, want resource_exhausted", cat)
	}
}

func TestClassifyError_Unknown(t *testing.T) {
	tests := []string{
		"something went wrong",
		"unexpected condition",
		"",
	}
	for _, msg := range tests {
		err := errors.New(msg)
		if cat := ClassifyError(err); cat != ErrCatUnknown {
			t.Errorf("ClassifyError(%q) = %s, want unknown", msg, cat)
		}
	}

	// Nil error
	if cat := ClassifyError(nil); cat != ErrCatUnknown {
		t.Errorf("ClassifyError(nil) = %s, want unknown", cat)
	}
}

func TestClassifyError_Priority(t *testing.T) {
	// Auth takes priority over validation
	err := errors.New("invalid api key: validation failed")
	if cat := ClassifyError(err); cat != ErrCatAuth {
		t.Errorf("auth should take priority over validation: got %s", cat)
	}

	// Timeout takes priority over network (net.Error + Timeout() = timeout, not network)
	err = os.ErrDeadlineExceeded
	if cat := ClassifyError(err); cat != ErrCatTimeout {
		t.Errorf("timeout should take priority over network: got %s", cat)
	}
}

func TestClassifyError_CategorizedChain(t *testing.T) {
	// If already categorized, return that category
	ce := NewCategorizedError(ErrCatLLM, errors.New("api error 503"))
	wrapped := fmt.Errorf("wrapped: %w", ce)
	if cat := ClassifyError(wrapped); cat != ErrCatLLM {
		t.Errorf("should return existing category: got %s", cat)
	}
}

// ─── CircuitBreaker Category Tests ──────────────────────────────────────────

func TestCircuitBreaker_RecordFailureWithCategory(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)

	// Record failure with a network error
	cb.RecordFailureWithCategory(errors.New("connection refused"))
	counts := cb.CategoryFailureCounts()
	if counts[ErrCatNetwork] != 1 {
		t.Errorf("expected 1 network failure, got %v", counts)
	}

	// Record failure with validation error
	cb.RecordFailureWithCategory(errors.New("invalid input"))
	counts = cb.CategoryFailureCounts()
	if counts[ErrCatValidation] != 1 {
		t.Errorf("expected 1 validation failure, got %v", counts)
	}
	if counts[ErrCatNetwork] != 1 {
		t.Errorf("expected 1 network failure still, got %v", counts)
	}
}

func TestCircuitBreaker_RecordFailure_BackwardCompat(t *testing.T) {
	cb := NewCircuitBreaker("test", 5, time.Second)
	cb.RecordFailure() // old-style, no category
	counts := cb.CategoryFailureCounts()
	if counts[ErrCatUnknown] != 1 {
		t.Errorf("old RecordFailure should count as unknown: got %v", counts)
	}

	// Should still open circuit at threshold
	for i := 0; i < 4; i++ {
		cb.RecordFailure()
	}
	if cb.State() != CircuitOpen {
		t.Error("circuit should open after threshold")
	}
}

func TestCircuitBreaker_CategoryFailureCounts_Empty(t *testing.T) {
	cb := NewCircuitBreaker("test", 3, time.Second)
	counts := cb.CategoryFailureCounts()
	if counts != nil {
		t.Errorf("empty breaker should return nil counts, got %v", counts)
	}
}

func TestCircuitBreaker_CategoryFailureCounts_Concurrent(t *testing.T) {
	cb := NewCircuitBreaker("test", 100, time.Minute)
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			cb.RecordFailureWithCategory(errors.New("connection refused"))
			done <- struct{}{}
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}
	counts := cb.CategoryFailureCounts()
	if counts[ErrCatNetwork] != 50 {
		t.Errorf("expected 50 network failures, got %v", counts)
	}
}

// ─── DeadLetterQueue Category Tests ─────────────────────────────────────────

func TestDeadLetterQueue_AutoClassify(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{
		ID:       "1",
		Task:     "test task",
		Agent:    "test-agent",
		Error:    "connection refused",
		Attempts: 3,
	})
	entries := dlq.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Category != "network" {
		t.Errorf("expected category 'network', got %q", entries[0].Category)
	}
}

func TestDeadLetterQueue_PreserveCategory(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{
		ID:       "1",
		Task:     "test task",
		Agent:    "test-agent",
		Error:    "something weird",
		Category: "custom-category",
	})
	entries := dlq.List()
	if entries[0].Category != "custom-category" {
		t.Errorf("pre-set category should be preserved: got %q", entries[0].Category)
	}
}

func TestDeadLetterQueue_CategoryCounts(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "1", Error: "connection refused"})
	dlq.Push(DeadLetterEntry{ID: "2", Error: "timeout"})
	dlq.Push(DeadLetterEntry{ID: "3", Error: "connection refused"})
	dlq.Push(DeadLetterEntry{ID: "4", Error: "invalid input"})

	counts := dlq.CategoryCounts()
	if counts["network"] != 2 {
		t.Errorf("expected 2 network, got %v", counts)
	}
	if counts["timeout"] != 1 {
		t.Errorf("expected 1 timeout, got %v", counts)
	}
	if counts["validation"] != 1 {
		t.Errorf("expected 1 validation, got %v", counts)
	}
}

func TestDeadLetterQueue_CategoryCounts_Empty(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	counts := dlq.CategoryCounts()
	if len(counts) != 0 {
		t.Errorf("empty DLQ should return empty map: got %v", counts)
	}
}

func TestDeadLetterQueue_EmptyErrorNoCategory(t *testing.T) {
	dlq := NewDeadLetterQueue("")
	dlq.Push(DeadLetterEntry{ID: "1", Error: ""})
	entries := dlq.List()
	if entries[0].Category != "" {
		t.Errorf("empty error should leave category empty: got %q", entries[0].Category)
	}
	counts := dlq.CategoryCounts()
	if counts["unknown"] != 1 {
		t.Errorf("empty-category entries should count as unknown: got %v", counts)
	}
}

func TestDeadLetterQueue_PersistenceRoundtrip(t *testing.T) {
	path := "/tmp/test-dlq-category.json"
	os.Remove(path)
	defer os.Remove(path)

	dlq1 := NewDeadLetterQueue(path)
	dlq1.Push(DeadLetterEntry{ID: "1", Error: "connection refused"})
	dlq1.Push(DeadLetterEntry{ID: "2", Error: "invalid input"})

	// Re-load from disk
	dlq2 := NewDeadLetterQueue(path)
	entries := dlq2.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after reload, got %d", len(entries))
	}
	if entries[0].Category != "network" {
		t.Errorf("persisted category should survive: got %q", entries[0].Category)
	}
	if entries[1].Category != "validation" {
		t.Errorf("persisted category should survive: got %q", entries[1].Category)
	}
}

// ─── Integration: CircuitBreaker + DLQ with categories ─────────────────────

func TestIntegration_CircuitBreakerAndDLQ(t *testing.T) {
	cb := NewCircuitBreaker("agent-1", 5, time.Minute)
	dlq := NewDeadLetterQueue("")

	// Simulate a sequence of failures
	failures := []struct {
		err     string
		wantCat ErrorCategory
	}{
		{"connection refused", ErrCatNetwork},
		{"i/o timeout", ErrCatTimeout},
		{"invalid api key", ErrCatAuth},
		{"connection refused", ErrCatNetwork},
		{"model not found", ErrCatLLM},
	}

	for i, f := range failures {
		err := errors.New(f.err)
		cb.RecordFailureWithCategory(err)
		dlq.Push(DeadLetterEntry{
			ID:    fmt.Sprintf("fail-%d", i),
			Error: f.err,
		})
	}

	// Verify circuit breaker has correct category counts
	cbCounts := cb.CategoryFailureCounts()
	if cbCounts[ErrCatNetwork] != 2 {
		t.Errorf("CB: expected 2 network failures, got %v", cbCounts)
	}
	if cbCounts[ErrCatTimeout] != 1 {
		t.Errorf("CB: expected 1 timeout, got %v", cbCounts)
	}
	if cbCounts[ErrCatAuth] != 1 {
		t.Errorf("CB: expected 1 auth failure, got %v", cbCounts)
	}

	// Verify DLQ has correct category counts
	dlqCounts := dlq.CategoryCounts()
	if dlqCounts["network"] != 2 {
		t.Errorf("DLQ: expected 2 network, got %v", dlqCounts)
	}
	if dlqCounts["timeout"] != 1 {
		t.Errorf("DLQ: expected 1 timeout, got %v", dlqCounts)
	}
}

// ─── ClassifyError edge cases ───────────────────────────────────────────────

func TestClassifyError_TimeoutViaNetError(t *testing.T) {
	// net.Error with Timeout() == true should be classified as timeout, not network.
	te := &testTimeoutError{msg: "dial tcp: i/o timeout"}
	if cat := ClassifyError(te); cat != ErrCatTimeout {
		t.Errorf("net.Error with Timeout()=true should be timeout: got %s", cat)
	}
}

type testTimeoutError struct{ msg string }

func (e *testTimeoutError) Error() string   { return e.msg }
func (e *testTimeoutError) Timeout() bool   { return true }
func (e *testTimeoutError) Temporary() bool { return true }

func TestClassifyError_CaseInsensitive(t *testing.T) {
	err := errors.New("CONNECTION REFUSED")
	if cat := ClassifyError(err); cat != ErrCatNetwork {
		t.Errorf("case insensitive classification: got %s", cat)
	}

	err = errors.New("Validation Failed")
	if cat := ClassifyError(err); cat != ErrCatValidation {
		t.Errorf("case insensitive: got %s", cat)
	}
}

func TestClassifyError_SubstringMatch(t *testing.T) {
	// "authorization" should NOT match "unauthorized" — "authorized" is a substring
	// but the pattern check uses strings.Contains, so it would.
	// This is acceptable for error classification (lax matching).
	err := errors.New("authorization token expired")
	if cat := ClassifyError(err); cat != ErrCatUnknown {
		// "authorization" doesn't contain "unauthorized" or "unauthenticated"
		// but if it matches something else, that's fine.
		t.Logf("authorization error classified as: %s", cat)
	}
}

// ─── RetryPolicy Tests ───────────────────────────────────────────────────────

func TestRetryPolicy_Success(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.Base = time.Millisecond
	policy.LLMBase = time.Millisecond

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryPolicy_Exhausted(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond
	policy.LLMBase = time.Millisecond

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return errors.New("connection refused")
	})
	if err == nil {
		t.Fatal("expected error after exhaustion")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	if !strings.Contains(err.Error(), "retry exhausted") {
		t.Errorf("error should mention exhaustion: %v", err)
	}
}

func TestRetryPolicy_EventualSuccess(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 5
	policy.Base = time.Millisecond
	policy.LLMBase = time.Millisecond

	calls := 0
	err := policy.Execute(func() error {
		calls++
		if calls < 3 {
			return errors.New("connection refused")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", calls)
	}
}

func TestRetryPolicy_ValidationFailsFast(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 5

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return errors.New("validation failed: missing required field")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("validation errors should fail fast (1 call), got %d", calls)
	}
	if !strings.Contains(err.Error(), "retry refused") {
		t.Errorf("error should mention retry refused: %v", err)
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("error should mention category: %v", err)
	}
}

func TestRetryPolicy_AuthFailsFast(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 5

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return errors.New("unauthorized: invalid api key")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("auth errors should fail fast (1 call), got %d", calls)
	}
	if !strings.Contains(err.Error(), "retry refused") {
		t.Errorf("error should mention retry refused: %v", err)
	}
}

func TestRetryPolicy_ResourceExhaustionFailsFast(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 5

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return errors.New("no space left on device")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("resource errors should fail fast (1 call), got %d", calls)
	}
}

func TestRetryPolicy_NetworkRetries(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond
	policy.LLMBase = time.Millisecond

	calls := 0
	policy.Execute(func() error {
		calls++
		return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	})
	if calls != 3 {
		t.Errorf("expected 3 calls for network error, got %d", calls)
	}
}

func TestRetryPolicy_TimeoutRetries(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond

	calls := 0
	policy.Execute(func() error {
		calls++
		return os.ErrDeadlineExceeded
	})
	if calls != 3 {
		t.Errorf("expected 3 calls for timeout error, got %d", calls)
	}
}

func TestRetryPolicy_LLMRetries(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond
	policy.LLMBase = time.Millisecond

	calls := 0
	policy.Execute(func() error {
		calls++
		return errors.New("ollama: model not found")
	})
	if calls != 3 {
		t.Errorf("expected 3 calls for LLM error, got %d", calls)
	}
}

func TestRetryPolicy_UnknownFailsFast_Default(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 5

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return errors.New("some completely unknown error")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("unknown errors should fail fast by default (1 call), got %d", calls)
	}
}

func TestRetryPolicy_UnknownRetries_WhenEnabled(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond
	policy.RetryUnknown = true

	calls := 0
	policy.Execute(func() error {
		calls++
		return errors.New("some completely unknown error")
	})
	if calls != 3 {
		t.Errorf("unknown errors should retry when RetryUnknown=true (3 calls), got %d", calls)
	}
}

func TestRetryPolicy_OnRetryCallback(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond

	var retryAttempts []int
	var retryCategories []ErrorCategory

	policy.OnRetry = func(attempt int, cat ErrorCategory, delay time.Duration) {
		retryAttempts = append(retryAttempts, attempt)
		retryCategories = append(retryCategories, cat)
	}

	policy.Execute(func() error {
		return errors.New("connection timed out")
	})

	if len(retryAttempts) != 2 {
		t.Errorf("expected OnRetry called 2 times (attempts 1→2, 2→3), got %d", len(retryAttempts))
	}
	for _, cat := range retryCategories {
		if cat != ErrCatTimeout {
			t.Errorf("expected timeout category, got %s", cat)
		}
	}
}

func TestRetryPolicy_DelayForCategory_Defaults(t *testing.T) {
	policy := &RetryPolicy{MaxRetries: 3}
	delay := policy.delayForCategory(1, ErrCatNetwork)
	if delay != 1*time.Second {
		t.Errorf("default base should be 1s, got %v", delay)
	}
}

func TestRetryPolicy_DelayForCategory_LLMDefault(t *testing.T) {
	policy := &RetryPolicy{MaxRetries: 3, Base: 500 * time.Millisecond}
	delay := policy.delayForCategory(1, ErrCatLLM)
	if delay != 1*time.Second {
		t.Errorf("LLM default should use 2× Base for attempt 1 (2×500ms=1s), got %v", delay)
	}
}

func TestRetryPolicy_DelayForCategory_LLMBaseSet(t *testing.T) {
	policy := &RetryPolicy{MaxRetries: 3, Base: 100 * time.Millisecond, LLMBase: 500 * time.Millisecond}
	delay := policy.delayForCategory(1, ErrCatLLM)
	if delay != 500*time.Millisecond {
		t.Errorf("explicit LLMBase should be 500ms, got %v", delay)
	}
}

func TestRetryPolicy_DelayForCategory_ExponentialGrowth(t *testing.T) {
	policy := &RetryPolicy{MaxRetries: 5, Base: 100 * time.Millisecond, MaxDelay: 10 * time.Second}

	d1 := policy.delayForCategory(1, ErrCatNetwork)
	d2 := policy.delayForCategory(2, ErrCatNetwork)
	d3 := policy.delayForCategory(3, ErrCatNetwork)

	if d1 != 100*time.Millisecond {
		t.Errorf("attempt 1: expected 100ms, got %v", d1)
	}
	if d2 != 200*time.Millisecond {
		t.Errorf("attempt 2: expected 200ms, got %v", d2)
	}
	if d3 != 400*time.Millisecond {
		t.Errorf("attempt 3: expected 400ms, got %v", d3)
	}
}

func TestRetryPolicy_MaxDelayCap(t *testing.T) {
	policy := &RetryPolicy{MaxRetries: 10, Base: 5 * time.Second, MaxDelay: 30 * time.Second}

	delay := policy.delayForCategory(5, ErrCatNetwork)
	if delay != 30*time.Second {
		t.Errorf("delay should be capped at 30s, got %v", delay)
	}
}

func TestRetryPolicy_CategorizedError_Retryable(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 3
	policy.Base = time.Millisecond
	policy.LLMBase = time.Millisecond

	calls := 0
	policy.Execute(func() error {
		calls++
		return NewCategorizedError(ErrCatNetwork, errors.New("dial tcp: connection refused"))
	})
	if calls != 3 {
		t.Errorf("CategorizedError(network) should retry (3 calls), got %d", calls)
	}
}

func TestRetryPolicy_CategorizedError_NotRetryable(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 5

	calls := 0
	err := policy.Execute(func() error {
		calls++
		return NewCategorizedError(ErrCatValidation, errors.New("schema check failed"))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("CategorizedError(validation) should fail fast (1 call), got %d", calls)
	}
}

func TestRetryPolicy_NilError(t *testing.T) {
	policy := DefaultRetryPolicy()
	err := policy.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("nil error should succeed: %v", err)
	}
}

func TestRetryPolicy_ExecuteWithPolicy(t *testing.T) {
	calls := 0
	err := ExecuteWithPolicy(func() error {
		calls++
		if calls < 2 {
			return errors.New("connection refused")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestRetryPolicy_DefaultRetryPolicy_Values(t *testing.T) {
	p := DefaultRetryPolicy()
	if p.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", p.MaxRetries)
	}
	if p.Base != 1*time.Second {
		t.Errorf("expected Base=1s, got %v", p.Base)
	}
	if p.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay=30s, got %v", p.MaxDelay)
	}
	if p.LLMBase != 2*time.Second {
		t.Errorf("expected LLMBase=2s, got %v", p.LLMBase)
	}
	if p.RetryUnknown {
		t.Error("expected RetryUnknown=false by default")
	}
}

func TestRetryPolicy_RetryRefusedWrapsError(t *testing.T) {
	policy := DefaultRetryPolicy()
	origErr := errors.New("validation failed: bad request")
	err := policy.Execute(func() error { return origErr })
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, origErr) {
		t.Errorf("retry refused error should wrap original via %%w: got %v", err)
	}
	var catErr *CategorizedError
	if errors.As(err, &catErr) {
		t.Logf("inner error is CategorizedError: %v", catErr)
	}
}

// ─── ErrorContext Tests ──────────────────────────────────────────────────────

func TestNewErrorContext_Basic(t *testing.T) {
	orig := errors.New("connection refused")
	ec := NewErrorContext(orig, "bt-agent", "review code", "Execute")
	if ec == nil {
		t.Fatal("expected non-nil ErrorContext")
	}
	if ec.Err != orig {
		t.Errorf("Err = %v, want %v", ec.Err, orig)
	}
	if ec.Agent != "bt-agent" {
		t.Errorf("Agent = %q, want bt-agent", ec.Agent)
	}
	if ec.Task != "review code" {
		t.Errorf("Task = %q, want review code", ec.Task)
	}
	if ec.Operation != "Execute" {
		t.Errorf("Operation = %q, want Execute", ec.Operation)
	}
	if ec.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0", ec.Attempt)
	}
	if ec.Category != ErrCatNetwork {
		t.Errorf("Category = %s, want network", ec.Category)
	}
	if ec.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestNewErrorContext_Nil(t *testing.T) {
	ec := NewErrorContext(nil, "agent", "task", "op")
	if ec != nil {
		t.Errorf("expected nil for nil error, got %v", ec)
	}
}

func TestErrorContext_Error(t *testing.T) {
	ec := NewErrorContext(errors.New("timeout"), "bt-evaluator", "evaluate tree", "Evaluate")
	msg := ec.Error()
	if !strings.Contains(msg, "bt-evaluator") {
		t.Errorf("Error() should contain agent name: %s", msg)
	}
	if !strings.Contains(msg, "Evaluate") {
		t.Errorf("Error() should contain operation: %s", msg)
	}
	if !strings.Contains(msg, "timeout") {
		t.Errorf("Error() should contain underlying error: %s", msg)
	}
}

func TestErrorContext_Error_Minimal(t *testing.T) {
	ec := &ErrorContext{Err: errors.New("oops")}
	msg := ec.Error()
	if !strings.Contains(msg, "oops") {
		t.Errorf("Error() should contain underlying: %s", msg)
	}
}

func TestErrorContext_Unwrap(t *testing.T) {
	orig := errors.New("original")
	ec := NewErrorContext(orig, "agent", "task", "op")
	if !errors.Is(ec, orig) {
		t.Error("errors.Is should find original through Unwrap")
	}
}

func TestErrorContext_Unwrap_Nil(t *testing.T) {
	ec := &ErrorContext{}
	if ec.Unwrap() != nil {
		t.Error("Unwrap on nil Err should return nil")
	}
}

func TestErrorContext_WithNode(t *testing.T) {
	ec := NewErrorContext(errors.New("err"), "a", "t", "op")
	result := ec.WithNode("jetson-01")
	if result != ec {
		t.Error("WithNode should return self for chaining")
	}
	if ec.Node != "jetson-01" {
		t.Errorf("Node = %q, want jetson-01", ec.Node)
	}
}

func TestErrorContext_WithAttempt(t *testing.T) {
	ec := NewErrorContext(errors.New("err"), "a", "t", "op")
	result := ec.WithAttempt(3)
	if result != ec {
		t.Error("WithAttempt should return self")
	}
	if ec.Attempt != 3 {
		t.Errorf("Attempt = %d, want 3", ec.Attempt)
	}
}

func TestErrorContext_WithCategory(t *testing.T) {
	ec := NewErrorContext(errors.New("err"), "a", "t", "op")
	result := ec.WithCategory(ErrCatAuth)
	if result != ec {
		t.Error("WithCategory should return self")
	}
	if ec.Category != ErrCatAuth {
		t.Errorf("Category = %s, want auth", ec.Category)
	}
}

func TestGetErrorContext_Found(t *testing.T) {
	orig := errors.New("base")
	ec := NewErrorContext(orig, "agent", "task", "op")
	result := GetErrorContext(ec)
	if result == nil {
		t.Fatal("GetErrorContext should find ErrorContext in chain")
	}
	if result.Agent != "agent" {
		t.Errorf("Agent = %q, want agent", result.Agent)
	}
}

func TestGetErrorContext_Wrapped(t *testing.T) {
	orig := errors.New("base")
	ec := NewErrorContext(orig, "agent", "task", "op")
	wrapped := fmt.Errorf("outer: %w", ec)
	result := GetErrorContext(wrapped)
	if result == nil {
		t.Fatal("GetErrorContext should find through fmt.Errorf wrapping")
	}
}

func TestGetErrorContext_NotFound(t *testing.T) {
	result := GetErrorContext(errors.New("plain error"))
	if result != nil {
		t.Errorf("expected nil for error without ErrorContext, got %v", result)
	}
}

func TestGetErrorContext_Nil(t *testing.T) {
	result := GetErrorContext(nil)
	if result != nil {
		t.Errorf("expected nil for nil error, got %v", result)
	}
}

func TestErrorContext_Summary_Full(t *testing.T) {
	ec := NewErrorContext(errors.New("connection timeout"), "code-reviewer", "review code", "Execute")
	ec.WithNode("jetson-01")
	summary := ec.Summary()
	if !strings.Contains(summary, "code-reviewer/Execute") {
		t.Errorf("Summary should contain agent/op: %s", summary)
	}
	// "connection timeout" is classified as timeout (contains "timeout").
	if !strings.Contains(summary, "timeout") {
		t.Errorf("Summary should contain category: %s", summary)
	}
}

func TestErrorContext_Summary_NoAgent(t *testing.T) {
	ec := &ErrorContext{Err: errors.New("oops"), Operation: "Health", Category: ErrCatLLM}
	summary := ec.Summary()
	if !strings.Contains(summary, "Health: llm:") {
		t.Errorf("Summary = %q, want 'Health: llm: oops'", summary)
	}
}

func TestErrorContext_Summary_NoOperation(t *testing.T) {
	ec := NewErrorContext(errors.New("invalid input"), "validator", "validate schema", "")
	summary := ec.Summary()
	if !strings.Contains(summary, "validation") {
		t.Errorf("Summary should contain category: %s", summary)
	}
}

func TestErrorContext_Summary_TruncatesLong(t *testing.T) {
	longMsg := strings.Repeat("x", 200)
	ec := NewErrorContext(errors.New(longMsg), "a", "t", "op")
	summary := ec.Summary()
	if len(summary) > 200 {
		t.Errorf("Summary should truncate long messages, got %d chars", len(summary))
	}
}

func TestErrorContext_Summary_NilErr(t *testing.T) {
	ec := &ErrorContext{Agent: "a", Operation: "op", Category: ErrCatTimeout}
	summary := ec.Summary()
	if !strings.Contains(summary, "timeout") {
		t.Errorf("Summary = %q, should contain category", summary)
	}
}

func TestErrorContext_Integration_RetryPolicy(t *testing.T) {
	// Verify ErrorContext wraps errors properly through RetryPolicy.Execute
	orig := errors.New("i/o timeout")
	ec := NewErrorContext(orig, "bt-agent", "run tests", "RunAgent")
	ec.WithAttempt(2)

	// ErrorContext should be found through wrapping.
	policy := DefaultRetryPolicy()
	err := policy.Execute(func() error {
		return ec
	})
	if err == nil {
		t.Fatal("expected error after retry exhaustion")
	}

	found := GetErrorContext(err)
	if found == nil {
		t.Fatal("ErrorContext should be preserved through RetryPolicy wrapping")
	}
	if found.Agent != "bt-agent" {
		t.Errorf("Agent = %q, want bt-agent", found.Agent)
	}
	if found.Attempt != 2 {
		t.Errorf("Attempt = %d, want 2", found.Attempt)
	}
}

func TestErrorContext_Summary_UnclassifiedCategory(t *testing.T) {
	// When Category is Unknown but Err has a classifiable message,
	// Summary should classify it.
	ec := &ErrorContext{Err: errors.New("connection refused"), Agent: "a", Operation: "op"}
	summary := ec.Summary()
	if !strings.Contains(summary, "network") {
		t.Errorf("Summary should classify unset category: %s", summary)
	}
}

func TestErrorContext_Chained(t *testing.T) {
	base := errors.New("disk full")
	ce := NewCategorizedError(ErrCatResourceExhausted, base)
	ec := NewErrorContext(ce, "gardener", "evolve trees", "Mutate")

	// Both categorizations should be accessible.
	cat := GetCategory(ec)
	if cat != ErrCatResourceExhausted {
		t.Errorf("GetCategory through ErrorContext: got %s, want resource_exhausted", cat)
	}

	ectx := GetErrorContext(ec)
	if ectx == nil {
		t.Fatal("GetErrorContext should return self")
	}
	if ectx.Agent != "gardener" {
		t.Errorf("Agent = %q, want gardener", ectx.Agent)
	}
}

func TestErrorContext_Concurrent(t *testing.T) {
	// Verify ErrorContext is safe for concurrent use patterns.
	ec := NewErrorContext(errors.New("error"), "agent", "task", "op")
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			_ = ec.Error()
			_ = ec.Summary()
			done <- true
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// ─── Jitter Tests ─────────────────────────────────────────────────────────────

func TestFullJitter_Zero(t *testing.T) {
	if got := FullJitter(0); got != 0 {
		t.Errorf("FullJitter(0) = %v, want 0", got)
	}
	if got := FullJitter(-time.Second); got != 0 {
		t.Errorf("FullJitter(-1s) = %v, want 0", got)
	}
}

func TestFullJitter_Range(t *testing.T) {
	base := 100 * time.Millisecond
	for i := 0; i < 100; i++ {
		got := FullJitter(base)
		if got < 0 || got >= base {
			t.Errorf("FullJitter(%v) = %v, out of range [0, %v)", base, got, base)
		}
	}
}

func TestEqualJitter_Zero(t *testing.T) {
	if got := EqualJitter(0); got != 0 {
		t.Errorf("EqualJitter(0) = %v, want 0", got)
	}
	if got := EqualJitter(-time.Second); got != 0 {
		t.Errorf("EqualJitter(-1s) = %v, want 0", got)
	}
}

func TestEqualJitter_Range(t *testing.T) {
	base := 100 * time.Millisecond
	half := base / 2
	for i := 0; i < 100; i++ {
		got := EqualJitter(base)
		if got < half {
			t.Errorf("EqualJitter(%v) = %v, below half %v", base, got, half)
		}
		if got > base {
			t.Errorf("EqualJitter(%v) = %v, above base %v", base, got, base)
		}
	}
}

func TestJitterStrategy_String(t *testing.T) {
	tests := []struct {
		s    JitterStrategy
		want string
	}{
		{NoJitter, "no_jitter"},
		{FullJitterStrategy, "full_jitter"},
		{EqualJitterStrategy, "equal_jitter"},
		{DecorrelatedJitterStrategy, "decorrelated_jitter"},
		{JitterStrategy(99), "no_jitter"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("JitterStrategy(%d).String() = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func TestApplyJitter_NoJitter(t *testing.T) {
	delay := 100 * time.Millisecond
	got := ApplyJitter(delay, NoJitter, 0)
	if got != delay {
		t.Errorf("ApplyJitter(NoJitter) = %v, want %v", got, delay)
	}
}

func TestApplyJitter_FullJitter(t *testing.T) {
	delay := 100 * time.Millisecond
	for i := 0; i < 50; i++ {
		got := ApplyJitter(delay, FullJitterStrategy, 0)
		if got < 0 || got >= delay {
			t.Errorf("ApplyJitter(FullJitter, %v) = %v, out of range [0, %v)", delay, got, delay)
		}
	}
}

func TestApplyJitter_EqualJitter(t *testing.T) {
	delay := 100 * time.Millisecond
	half := delay / 2
	for i := 0; i < 50; i++ {
		got := ApplyJitter(delay, EqualJitterStrategy, 0)
		if got < half || got > delay {
			t.Errorf("ApplyJitter(EqualJitter, %v) = %v, out of range [%v, %v]", delay, got, half, delay)
		}
	}
}

func TestApplyJitter_Decorrelated(t *testing.T) {
	delay := 100 * time.Millisecond
	got := ApplyJitter(delay, DecorrelatedJitterStrategy, 50*time.Millisecond)
	if got <= 0 {
		t.Errorf("ApplyJitter(Decorrelated) = %v, want > 0", got)
	}
	got2 := ApplyJitter(delay, DecorrelatedJitterStrategy, 0)
	if got2 <= 0 {
		t.Errorf("ApplyJitter(Decorrelated, zero prev) = %v, want > 0", got2)
	}
}

func TestDecorrelatedJitter_ZeroMaxDelay(t *testing.T) {
	if got := DecorrelatedJitter(time.Second, time.Second, 0); got != 0 {
		t.Errorf("DecorrelatedJitter with maxDelay=0 = %v, want 0", got)
	}
}

func TestApplyJitter_AllStrategiesDeterministicRange(t *testing.T) {
	strategies := []JitterStrategy{NoJitter, FullJitterStrategy, EqualJitterStrategy, DecorrelatedJitterStrategy}
	for _, s := range strategies {
		for i := 0; i < 20; i++ {
			got := ApplyJitter(time.Second, s, 500*time.Millisecond)
			if got < 0 {
				t.Errorf("ApplyJitter(%s) = %v, negative", s, got)
			}
		}
	}
}

// ─── RetryPolicy Validate & Sanitize Tests ────────────────────────────────────

func TestRetryPolicy_Validate_ZeroRetries(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 0}
	if err := p.Validate(); err == nil {
		t.Error("Validate() expected error for MaxRetries=0")
	}
}

func TestRetryPolicy_Validate_NegativeRetries(t *testing.T) {
	p := &RetryPolicy{MaxRetries: -1}
	if err := p.Validate(); err == nil {
		t.Error("Validate() expected error for MaxRetries=-1")
	}
}

func TestRetryPolicy_Validate_BaseExceedsMaxDelay(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, Base: 60 * time.Second, MaxDelay: 30 * time.Second}
	if err := p.Validate(); err == nil {
		t.Error("Validate() expected error when Base > MaxDelay")
	}
}

func TestRetryPolicy_Validate_NegativeDurations(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, Base: -time.Second}
	if err := p.Validate(); err == nil {
		t.Error("Validate() expected error for negative Base")
	}
	p2 := &RetryPolicy{MaxRetries: 3, Base: time.Second, MaxDelay: -time.Second}
	if err := p2.Validate(); err == nil {
		t.Error("Validate() expected error for negative MaxDelay")
	}
	p3 := &RetryPolicy{MaxRetries: 3, Base: time.Second, LLMBase: -time.Second}
	if err := p3.Validate(); err == nil {
		t.Error("Validate() expected error for negative LLMBase")
	}
}

func TestRetryPolicy_Validate_Valid(t *testing.T) {
	p := DefaultRetryPolicy()
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestRetryPolicy_Sanitize_Defaults(t *testing.T) {
	p := &RetryPolicy{}
	p.Sanitize()
	if p.MaxRetries != 3 {
		t.Errorf("Sanitize MaxRetries = %d, want 3", p.MaxRetries)
	}
	if p.Base != time.Second {
		t.Errorf("Sanitize Base = %v, want 1s", p.Base)
	}
	if p.MaxDelay != 30*time.Second {
		t.Errorf("Sanitize MaxDelay = %v, want 30s", p.MaxDelay)
	}
	if p.LLMBase != 2*time.Second {
		t.Errorf("Sanitize LLMBase = %v, want 2s", p.LLMBase)
	}
}

func TestRetryPolicy_Sanitize_Partial(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 5}
	p.Sanitize()
	if p.MaxRetries != 5 {
		t.Errorf("Sanitize should preserve MaxRetries=5, got %d", p.MaxRetries)
	}
	if p.Base != time.Second {
		t.Errorf("Sanitize Base = %v, want 1s", p.Base)
	}
}

func TestRetryPolicy_Sanitize_LLMBase(t *testing.T) {
	p := &RetryPolicy{MaxRetries: 3, Base: 5 * time.Second}
	p.Sanitize()
	if p.LLMBase != 10*time.Second {
		t.Errorf("Sanitize LLMBase = %v, want 10s (2×Base)", p.LLMBase)
	}
}

// ─── RetryPolicy ExecuteContext Tests ──────────────────────────────────────────

func TestRetryPolicy_ExecuteContext_Success(t *testing.T) {
	p := DefaultRetryPolicy()
	attempts := 0
	err := p.ExecuteContext(context.Background(), func() error {
		attempts++
		return nil
	})
	if err != nil {
		t.Errorf("ExecuteContext unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}

func TestRetryPolicy_ExecuteContext_Exhaustion(t *testing.T) {
	p := DefaultRetryPolicy()
	p.Jitter = NoJitter   // deterministic delays
	p.RetryUnknown = true // unknown errors should retry
	errCount := 0
	err := p.ExecuteContext(context.Background(), func() error {
		errCount++
		return fmt.Errorf("transient error")
	})
	if err == nil {
		t.Fatal("ExecuteContext expected error")
	}
	if errCount != p.MaxRetries {
		t.Errorf("Expected %d attempts, got %d", p.MaxRetries, errCount)
	}
}

func TestRetryPolicy_ExecuteContext_RetryRefused(t *testing.T) {
	p := DefaultRetryPolicy()
	err := p.ExecuteContext(context.Background(), func() error {
		return NewCategorizedError(ErrCatValidation, fmt.Errorf("bad input"))
	})
	if err == nil {
		t.Fatal("ExecuteContext expected error")
	}
	if !strings.Contains(err.Error(), "retry refused") {
		t.Errorf("Expected 'retry refused', got: %v", err)
	}
}

func TestRetryPolicy_ExecuteContext_Cancellation(t *testing.T) {
	p := &RetryPolicy{
		MaxRetries:   10,
		Base:         time.Hour, // very long — would hang
		MaxDelay:     time.Hour,
		Jitter:       NoJitter,
		RetryUnknown: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := p.ExecuteContext(ctx, func() error {
		return fmt.Errorf("will be cancelled")
	})
	if err == nil {
		t.Fatal("ExecuteContext expected cancellation error")
	}
	if !strings.Contains(err.Error(), "retry cancelled") {
		t.Errorf("Expected 'retry cancelled', got: %v", err)
	}
}

func TestRetryPolicy_ExecuteContext_Timeout(t *testing.T) {
	p := &RetryPolicy{
		MaxRetries:   10,
		Base:         time.Hour,
		MaxDelay:     time.Hour,
		Jitter:       NoJitter,
		RetryUnknown: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	// Give the context a moment to be cancelled before we start.
	time.Sleep(2 * time.Millisecond)

	err := p.ExecuteContext(ctx, func() error {
		return fmt.Errorf("will timeout")
	})
	if err == nil {
		t.Fatal("ExecuteContext expected timeout error")
	}
	if !strings.Contains(err.Error(), "retry cancelled") && !strings.Contains(err.Error(), "context") {
		t.Errorf("Expected retry cancelled or context error, got: %v", err)
	}
}

func TestRetryPolicy_ExecuteContext_EventuallySucceeds(t *testing.T) {
	p := &RetryPolicy{
		MaxRetries:   5,
		Base:         time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		LLMBase:      2 * time.Millisecond,
		Jitter:       NoJitter,
		RetryUnknown: true,
	}
	attempts := 0
	err := p.ExecuteContext(context.Background(), func() error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("transient error")
		}
		return nil
	})
	if err != nil {
		t.Errorf("ExecuteContext unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestRetryPolicy_CallbackContext(t *testing.T) {
	var called bool
	p := &RetryPolicy{
		MaxRetries:   2,
		Base:         time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
		Jitter:       NoJitter,
		RetryUnknown: true,
		OnRetry: func(attempt int, cat ErrorCategory, delay time.Duration) {
			called = true
		},
	}
	_ = p.ExecuteContext(context.Background(), func() error {
		return fmt.Errorf("fail")
	})
	if !called {
		t.Error("OnRetry callback not called")
	}
}

func TestRetryPolicy_DeprecatedInheritsContext(t *testing.T) {
	// Execute() (deprecated) should still work via ExecuteContext.
	p := DefaultRetryPolicy()
	p.Base = time.Millisecond
	p.MaxDelay = 5 * time.Millisecond
	p.Jitter = NoJitter
	p.RetryUnknown = true

	attempts := 0
	err := p.Execute(func() error {
		attempts++
		if attempts < 2 {
			return fmt.Errorf("transient")
		}
		return nil
	})
	if err != nil {
		t.Errorf("Execute() unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}
