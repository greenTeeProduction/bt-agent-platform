// Package reliability provides error categorization for the BT platform.
// ErrorCategory enables smarter recovery strategies: network errors retry
// differently than validation errors, LLM timeouts get different backoff
// than resource exhaustion, and the dead letter queue surfaces failure
// patterns by category for multi-node diagnostics.
package reliability

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// ErrorCategory classifies errors into actionable groups for
// circuit breakers, backoff strategies, and DLQ diagnostics.
type ErrorCategory int

const (
	// ErrCatUnknown is the default for errors that don't match known patterns.
	ErrCatUnknown ErrorCategory = iota

	// ErrCatNetwork covers DNS failures, connection refused, timeouts,
	// and other transport-layer errors. Safe to retry with backoff.
	ErrCatNetwork

	// ErrCatTimeout covers context deadline exceeded and explicit
	// timeout errors. May retry with increased timeout.
	ErrCatTimeout

	// ErrCatLLM covers LLM inference failures: model not found,
	// Ollama errors, API errors, context length exceeded.
	ErrCatLLM

	// ErrCatValidation covers input validation errors: missing fields,
	// invalid formats, schema violations. Should NOT be retried.
	ErrCatValidation

	// ErrCatResourceExhausted covers OOM, disk full, connection pool
	// exhaustion, and other system resource limits. Requires operator
	// intervention or resource scaling.
	ErrCatResourceExhausted

	// ErrCatAuth covers authentication and authorization failures:
	// invalid API keys, expired tokens, permission denied.
	ErrCatAuth
)

// String returns the human-readable category name.
func (c ErrorCategory) String() string {
	switch c {
	case ErrCatNetwork:
		return "network"
	case ErrCatTimeout:
		return "timeout"
	case ErrCatLLM:
		return "llm"
	case ErrCatValidation:
		return "validation"
	case ErrCatResourceExhausted:
		return "resource_exhausted"
	case ErrCatAuth:
		return "auth"
	default:
		return "unknown"
	}
}

// IsRetryable returns true for error categories where retry is appropriate.
// Network errors and timeouts are transient; validation and auth are not.
func (c ErrorCategory) IsRetryable() bool {
	switch c {
	case ErrCatNetwork, ErrCatTimeout, ErrCatLLM:
		return true
	default:
		return false
	}
}

// CategorizedError wraps an error with its category for structured handling.
type CategorizedError struct {
	Err      error
	Category ErrorCategory
}

// Error implements the error interface.
func (ce *CategorizedError) Error() string {
	if ce.Err == nil {
		return ce.Category.String() + ": <nil>"
	}
	return ce.Category.String() + ": " + ce.Err.Error()
}

// Unwrap returns the wrapped error for errors.Is/errors.As support.
func (ce *CategorizedError) Unwrap() error {
	return ce.Err
}

// NewCategorizedError creates a categorized error.
func NewCategorizedError(category ErrorCategory, err error) *CategorizedError {
	return &CategorizedError{Err: err, Category: category}
}

// GetCategory extracts the ErrorCategory from an error chain.
// Returns ErrCatUnknown if no CategorizedError is found.
func GetCategory(err error) ErrorCategory {
	var ce *CategorizedError
	if errors.As(err, &ce) {
		return ce.Category
	}
	return ErrCatUnknown
}

// ClassifyError inspects an error and its chain to determine the category.
// It checks for known error patterns in order of specificity:
// validation → auth → resource → timeout → network → LLM → unknown.
func ClassifyError(err error) ErrorCategory {
	if err == nil {
		return ErrCatUnknown
	}

	// Check the chain first for an existing CategorizedError.
	if cat := GetCategory(err); cat != ErrCatUnknown {
		return cat
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	// ─── Auth errors (check before validation — "invalid api key" is auth) ──
	if isAuthError(lower) {
		return ErrCatAuth
	}

	// ─── Validation errors (should NOT retry) ──────────────────────────
	if isValidationError(lower) {
		return ErrCatValidation
	}

	// ─── Resource exhaustion ───────────────────────────────────────────
	if isResourceError(lower, err) {
		return ErrCatResourceExhausted
	}

	// ─── Timeout errors (check before network — net.Error+Timeout() is timeout) ──
	if isTimeoutError(err, lower) {
		return ErrCatTimeout
	}

	// ─── Network errors ────────────────────────────────────────────────
	if isNetworkError(err, lower) {
		return ErrCatNetwork
	}

	// ─── LLM errors ────────────────────────────────────────────────────
	if isLLMError(lower) {
		return ErrCatLLM
	}

	return ErrCatUnknown
}

// isValidationError checks for input validation failure patterns.
func isValidationError(lower string) bool {
	validationPatterns := []string{
		"validation failed", "validation error",
		"invalid input", "invalid format",
		"invalid value", "invalid type",
		"invalid request", "invalid parameter",
		"malformed",
		"missing required", "unsupported",
		"schema", "bad request", "400",
		"unmarshal", "cannot unmarshal",
		"required field", "must be",
		"type mismatch",
	}
	for _, p := range validationPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isAuthError checks for authentication/authorization patterns.
func isAuthError(lower string) bool {
	authPatterns := []string{
		"unauthorized", "unauthenticated",
		"forbidden", "access denied",
		"invalid api key", "invalid token",
		"permission denied", "not authorized",
		"401", "403",
	}
	for _, p := range authPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isResourceError checks for system resource exhaustion.
func isResourceError(lower string, err error) bool {
	resourcePatterns := []string{
		"out of memory", "cannot allocate memory",
		"no space left", "disk full",
		"too many open files", "resource temporarily unavailable",
		"connection pool exhausted", "buffer pool",
	}
	for _, p := range resourcePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// Check for specific syscall errors.
	if errors.Is(err, syscall.ENOMEM) || errors.Is(err, syscall.ENOSPC) {
		return true
	}
	return false
}

// isTimeoutError checks for context deadline and timeout patterns.
func isTimeoutError(err error, lower string) bool {
	if errors.Is(err, contextDeadlineExceeded()) {
		return true
	}
	timeoutPatterns := []string{
		"deadline exceeded", "context deadline",
		"i/o timeout", "connection timed out",
		"timed out", "timeout",
		"context canceled",
	}
	for _, p := range timeoutPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isNetworkError checks for transport-layer network failures.
func isNetworkError(err error, lower string) bool {
	// Check known net error types.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	networkPatterns := []string{
		"connection refused", "no such host",
		"network is unreachable", "connection reset",
		"broken pipe", "eof", "tls",
		"dial tcp", "dial udp",
		"no route to host", "host is down",
	}
	for _, p := range networkPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// isLLMError checks for LLM/API inference failure patterns.
func isLLMError(lower string) bool {
	llmPatterns := []string{
		"model not found", "ollama",
		"llm", "inference",
		"context length", "token limit",
		"max tokens", "rate limit",
		"api error", "server error",
		"502", "503", "504",
		"internal server error",
		"service unavailable",
	}
	for _, p := range llmPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// contextDeadlineExceeded returns a sentinel error matching context.DeadlineExceeded
// without importing the context package (avoiding circular deps in some contexts).
// The ClassifyError function uses errors.Is which unwraps through the chain.
func contextDeadlineExceeded() error {
	// We use os.ErrDeadlineExceeded as a proxy — it has the same semantics.
	return os.ErrDeadlineExceeded
}
