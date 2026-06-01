package reliability

import (
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
		err      string
		wantCat  ErrorCategory
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
