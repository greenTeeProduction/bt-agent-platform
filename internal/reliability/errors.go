// Package reliability provides error categorization for the BT platform.
// ErrorCategory enables smarter recovery strategies: network errors retry
// differently than validation errors, LLM timeouts get different backoff
// than resource exhaustion, and the dead letter queue surfaces failure
// patterns by category for multi-node diagnostics.
package reliability

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
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

	// ErrCatRateLimited covers API rate limit responses (429, Retry-After).
	// Retryable but MUST use the server-provided Retry-After delay,
	// not the standard exponential backoff.
	ErrCatRateLimited

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
	case ErrCatRateLimited:
		return "rate_limited"
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
	case ErrCatNetwork, ErrCatTimeout, ErrCatLLM, ErrCatRateLimited:
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
// Typed errors in the chain (CategorizedError, RateLimitError) win over
// string matching. String patterns are checked in order of specificity:
// auth → validation → resource → timeout → network → rate limit → LLM → unknown.
func ClassifyError(err error) ErrorCategory {
	if err == nil {
		return ErrCatUnknown
	}

	// Check the chain first for an existing CategorizedError.
	if cat := GetCategory(err); cat != ErrCatUnknown {
		return cat
	}

	// A typed RateLimitError in the chain is authoritative — classification
	// must not depend on its message dodging the string patterns below.
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return ErrCatRateLimited
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	// ─── Typed transport errors (before the string patterns, including
	// auth) ──────────────────────────────────────────────────────────────
	// A *url.Error / net.Error / net.OpError / *net.DNSError in the chain is
	// definitionally a transport-layer failure — its message can contain
	// anything, so it must beat substring guessing (an httptest port
	// containing "400" once classified an EOF POST as a non-retryable
	// validation error, and a URL containing "401" once classified one as
	// a non-retryable auth error, both refusing a retry that should have
	// happened, 2026-07-16). Timeout and resource-exhaustion evidence keep
	// their precedence: bare syscall errnos satisfy net.Error too (Errno
	// has Timeout/Temporary), and ENOMEM/ENOSPC must stay resource_exhausted.
	if isTypedNetworkError(err) {
		if isTimeoutError(err, lower) {
			return ErrCatTimeout
		}
		if isResourceError(lower, err) {
			return ErrCatResourceExhausted
		}
		return ErrCatNetwork
	}

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

	// ─── Rate limit errors (before LLM — provider-wrapped rate-limit
	// messages like "deepseek api error: rate limit reached" would
	// otherwise match the broader LLM patterns first) ──────────────────
	if isRateLimitError(lower) {
		return ErrCatRateLimited
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
// isTypedNetworkError reports whether the chain carries typed transport-layer
// evidence (net.Error, *url.Error, *net.DNSError, *net.OpError).
func isTypedNetworkError(err error) bool {
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
	return errors.As(err, &opErr)
}

func isNetworkError(err error, lower string) bool {
	if isTypedNetworkError(err) {
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
		"max tokens",
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

// isRateLimitError checks for API rate limit response patterns.
// Detects HTTP 429, Retry-After, and rate-limit language from Claude,
// OpenAI, DeepSeek, and other providers.
func isRateLimitError(lower string) bool {
	// Bare substrings like "429", "rpm", or "tpm" are deliberately absent:
	// they match unrelated messages ("record 14290 not found", "rpm package",
	// "tpm device") and would turn permanent failures into retryable ones.
	rateLimitPatterns := []string{
		"rate limit", "rate limited", "rate_limit",
		"too many requests",
		"http 429", "status 429", "429 too many",
		"retry after", "retry-after",
		"quota exceeded", "quota_exceeded",
		"request limit reached",
		"requests per minute",
		"tokens per minute",
		// Claude CLI usage/credit-limit exhaustion: "You've reached your
		// <model> limit. Run /usage-credits to continue" — matches the
		// patterns goapInfraResultMarkers uses to treat this as
		// infrastructure in internal/engine/actions_goap_fusion_refund.go.
		"reached your ",
		"/usage-credits",
	}
	for _, p := range rateLimitPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// TruncateForError caps an HTTP error-response body for inclusion in an error
// string, so a multi-KB gateway HTML page doesn't bloat logs. The cut backs
// off to a UTF-8 rune boundary so a multi-byte sequence is never split.
// Classification never depends on the body (status codes drive it), so the
// cap is purely cosmetic. Shared by the LLM/embedding HTTP clients.
func TruncateForError(b []byte) string {
	const max = 512
	if len(b) <= max {
		return string(b)
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(b[cut]) {
		cut--
	}
	return string(b[:cut]) + "…"
}

// contextDeadlineExceeded returns a sentinel error matching context.DeadlineExceeded
// without importing the context package (avoiding circular deps in some contexts).
// The ClassifyError function uses errors.Is which unwraps through the chain.
func contextDeadlineExceeded() error {
	// We use os.ErrDeadlineExceeded as a proxy — it has the same semantics.
	return os.ErrDeadlineExceeded
}

// ─── Rate Limit Error ─────────────────────────────────────────────────────

// RateLimitError wraps an API rate limit response and carries the
// server-provided Retry-After duration. The retry policy uses this
// instead of computed exponential backoff when present in the error chain.
type RateLimitError struct {
	Err        error
	RetryAfter time.Duration
	Message    string // human-readable description (e.g. "Claude API rate limited")
}

// Error implements the error interface.
func (rle *RateLimitError) Error() string {
	if rle.Message != "" {
		return fmt.Sprintf("rate limited (retry after %s): %s", rle.RetryAfter, rle.Message)
	}
	if rle.Err != nil {
		return fmt.Sprintf("rate limited (retry after %s): %v", rle.RetryAfter, rle.Err)
	}
	return fmt.Sprintf("rate limited (retry after %s)", rle.RetryAfter)
}

// Unwrap returns the wrapped error for errors.Is/errors.As support.
func (rle *RateLimitError) Unwrap() error {
	return rle.Err
}

// RetryAfterFromError extracts the Retry-After duration from an error chain.
// Returns 0 if no RateLimitError is found.
func RetryAfterFromError(err error) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) {
		return rle.RetryAfter
	}
	return 0
}

// ParseRetryAfter parses an HTTP Retry-After header value into a time.Duration.
// Handles both delta-seconds (e.g. "120") and HTTP-date formats.
// Returns 0 for missing, unparseable, or already-elapsed values so the retry
// policy falls back to its own category backoff instead of a hardcoded wait.
func ParseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	// Try delta-seconds format (e.g. "120")
	if secs, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// Try HTTP-date format (e.g. "Wed, 21 Oct 2015 07:28:00 GMT")
	if t, err := time.Parse(time.RFC1123, header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// ─── Jitter Strategies ─────────────────────────────────────────────────────────

// JitterStrategy selects the jitter algorithm for backoff delays.
type JitterStrategy int

const (
	// NoJitter applies no randomization — pure exponential backoff.
	NoJitter JitterStrategy = iota
	// FullJitterStrategy randomizes in [0, baseDelay) — AWS recommended.
	FullJitterStrategy
	// EqualJitterStrategy randomizes in [baseDelay/2, baseDelay] — Google recommended.
	EqualJitterStrategy
	// DecorrelatedJitterStrategy uses previous-sleep-based smoothing.
	DecorrelatedJitterStrategy
)

// String returns the human-readable strategy name.
func (s JitterStrategy) String() string {
	switch s {
	case FullJitterStrategy:
		return "full_jitter"
	case EqualJitterStrategy:
		return "equal_jitter"
	case DecorrelatedJitterStrategy:
		return "decorrelated_jitter"
	default:
		return "no_jitter"
	}
}

// FullJitter returns a random duration in [0, baseDelay).
// AWS recommended pattern: sleep = random_between(0, min(cap, base * 2^attempt))
// Prevents thundering herd when many clients retry simultaneously.
func FullJitter(baseDelay time.Duration) time.Duration {
	if baseDelay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(baseDelay)))
}

// EqualJitter returns a random duration in [baseDelay/2, baseDelay].
// Half of the backoff time is deterministic, half is random.
// Recommended for latency-sensitive systems: ensures minimum delay while
// still providing desynchronization.
func EqualJitter(baseDelay time.Duration) time.Duration {
	if baseDelay <= 0 {
		return 0
	}
	half := baseDelay / 2
	// +1 to include exact half in the random range.
	return half + time.Duration(rand.Int63n(int64(half+1)))
}

// DecorrelatedJitter returns a duration based on the previous sleep value.
// sleep = min(cap, random_between(base, sleep * 3))
// Produces smoother retry distribution than FullJitter under contention.
func DecorrelatedJitter(previousSleep time.Duration, base time.Duration, maxDelay time.Duration) time.Duration {
	if maxDelay <= 0 {
		return 0
	}
	if previousSleep <= 0 {
		previousSleep = base
	}
	next := previousSleep * 3
	if next > maxDelay {
		next = maxDelay
	}
	minVal := base
	if minVal <= 0 {
		minVal = time.Millisecond
	}
	if next <= minVal {
		return minVal
	}
	return minVal + time.Duration(rand.Int63n(int64(next-minVal)))
}

// ApplyJitter applies the selected jitter strategy to a backoff delay.
// For NoJitter, returns the delay unchanged.
func ApplyJitter(delay time.Duration, strategy JitterStrategy, previousSleep time.Duration) time.Duration {
	switch strategy {
	case FullJitterStrategy:
		return FullJitter(delay)
	case EqualJitterStrategy:
		return EqualJitter(delay)
	case DecorrelatedJitterStrategy:
		return DecorrelatedJitter(previousSleep, delay/2, delay*2)
	default:
		return delay
	}
}

// ─── Retry Policy ────────────────────────────────────────────────────────────

// RetryPolicy defines category-aware retry behavior. Different error
// categories get different treatment: network errors retry with backoff,
// validation errors fail immediately, LLM errors retry with longer base delay.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts (default: 3).
	MaxRetries int

	// Base is the initial backoff delay (default: 1s).
	Base time.Duration

	// MaxDelay caps the exponential backoff (default: 30s).
	MaxDelay time.Duration

	// LLMBase is the initial delay for LLM errors (default: 2× Base).
	// LLM inference failures often resolve after a short wait.
	LLMBase time.Duration

	// RetryUnknown controls whether unknown-category errors are retried.
	// When false (default), unknown errors fail immediately to be safe.
	RetryUnknown bool

	// Jitter controls how backoff delays are randomized to prevent
	// thundering herd. Default: NoJitter (no randomization).
	Jitter JitterStrategy

	// PreviousSleep tracks the last sleep duration for decorrelated jitter.
	previousSleep time.Duration

	// OnRetry is an optional callback invoked before each retry attempt.
	// Receives: attempt number (1-indexed), error category, next delay.
	OnRetry func(attempt int, category ErrorCategory, delay time.Duration)
}

// DefaultRetryPolicy returns a sensible production policy:
//
//	Network/Timeout: 3 retries, 1s→2s→4s backoff
//	LLM: 3 retries, 2s→4s→8s backoff
//	Validation/Auth/ResourceExhausted: fail immediately
//	Unknown: fail immediately (conservative)
//	Jitter: FullJitter (prevents thundering herd)
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:   3,
		Base:         1 * time.Second,
		MaxDelay:     30 * time.Second,
		LLMBase:      2 * time.Second,
		RetryUnknown: false,
		Jitter:       FullJitterStrategy,
	}
}

// Validate checks the RetryPolicy for configuration errors and sanitizes
// zero values to sensible defaults. Returns an error if the policy
// cannot produce valid behavior (e.g., MaxRetries < 1).
func (p *RetryPolicy) Validate() error {
	if p.MaxRetries < 1 {
		return fmt.Errorf("retry policy: MaxRetries must be >= 1, got %d", p.MaxRetries)
	}
	if p.Base < 0 {
		return fmt.Errorf("retry policy: Base must be >= 0, got %v", p.Base)
	}
	if p.MaxDelay < 0 {
		return fmt.Errorf("retry policy: MaxDelay must be >= 0, got %v", p.MaxDelay)
	}
	if p.Base > 0 && p.MaxDelay > 0 && p.Base > p.MaxDelay {
		return fmt.Errorf("retry policy: Base (%v) must not exceed MaxDelay (%v)", p.Base, p.MaxDelay)
	}
	if p.LLMBase < 0 {
		return fmt.Errorf("retry policy: LLMBase must be >= 0, got %v", p.LLMBase)
	}
	return nil
}

// Sanitize fills in zero/default values that could cause unintended
// behavior (zero delays, unbounded waits, etc.).
func (p *RetryPolicy) Sanitize() {
	if p.MaxRetries <= 0 {
		p.MaxRetries = 3
	}
	if p.Base <= 0 {
		p.Base = 1 * time.Second
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.LLMBase <= 0 {
		p.LLMBase = 2 * p.Base
	}
}

// Execute runs fn with category-aware retry. It classifies the error
// from each attempt and decides whether to retry based on the category.
//
// Non-retryable categories (Validation, Auth, ResourceExhausted) return
// immediately with a wrapped error indicating the category and that
// retry was refused.
//
// Network and Timeout errors use exponential backoff. LLM errors use
// a longer base delay (LLMBase). Unknown errors are treated according
// to RetryUnknown.
//
// Deprecated: Use ExecuteContext for context cancellation support.
func (p *RetryPolicy) Execute(fn func() error) error {
	return p.ExecuteContext(context.Background(), fn)
}

// ExecuteContext runs fn with category-aware retry and context support.
// If the context is cancelled or exceeds its deadline during a retry sleep,
// the function returns immediately with context.Canceled/context.DeadlineExceeded.
//
// Non-retryable categories (Validation, Auth, ResourceExhausted) return
// immediately with a wrapped error. Network/Timeout errors use exponential
// backoff with jitter (if configured). LLM errors use LLMBase delay.
func (p *RetryPolicy) ExecuteContext(ctx context.Context, fn func() error) error {
	var lastErr error
	var lastCat ErrorCategory

	for attempt := 1; attempt <= p.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err
		lastCat = ClassifyError(err)

		// Check if this category is retryable.
		if !lastCat.IsRetryable() && (lastCat != ErrCatUnknown || !p.RetryUnknown) {
			return fmt.Errorf("retry refused for %s error: %w", lastCat, err)
		}

		// Don't sleep on the last attempt.
		if attempt >= p.MaxRetries {
			break
		}

		// Compute backoff delay based on category and error.
		delay := p.delayForCategory(attempt, lastCat, err)

		if p.OnRetry != nil {
			p.OnRetry(attempt, lastCat, delay)
		}

		// Use context-aware sleep so cancellation is prompt.
		if err := sleepWithContext(ctx, delay); err != nil {
			return fmt.Errorf("retry cancelled after %d attempts (context): %w", attempt, err)
		}

		// Track previous sleep for decorrelated jitter.
		p.previousSleep = delay
	}

	return fmt.Errorf("retry exhausted after %d attempts (last: %s): %w",
		p.MaxRetries, lastCat, lastErr)
}

// sleepWithContext sleeps for the given duration or until the context
// is cancelled, whichever comes first.
func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// maxRetryAfterDelay bounds server-provided Retry-After values so a large or
// hostile header cannot stall the retry loop indefinitely. Deliberately
// higher than typical MaxDelay values — genuine rate-limit windows often
// exceed the exponential-backoff cap.
const maxRetryAfterDelay = 5 * time.Minute

// delayForCategory returns the backoff delay for a given attempt and category,
// applying jitter if configured. If the error carries a RateLimitError with
// a Retry-After duration, that server-provided value takes precedence over
// the computed exponential backoff (clamped to maxRetryAfterDelay, with
// additive jitter so synchronized clients don't retry in lockstep).
func (p *RetryPolicy) delayForCategory(attempt int, cat ErrorCategory, err error) time.Duration {
	if ra := RetryAfterFromError(err); ra > 0 {
		if ra > maxRetryAfterDelay {
			ra = maxRetryAfterDelay
		}
		// Additive jitter (up to +10%) — never below the server-requested
		// wait, but desynchronized across clients hitting the same limit.
		return ra + time.Duration(rand.Int63n(int64(ra/10)+1))
	}

	base := p.Base
	if base <= 0 {
		base = 1 * time.Second
	}
	if cat == ErrCatLLM {
		if p.LLMBase > 0 {
			base = p.LLMBase
		} else {
			base = 2 * base
		}
	}
	maxDelay := p.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 30 * time.Second
	}
	rawDelay := Backoff(attempt, base, maxDelay)
	return ApplyJitter(rawDelay, p.Jitter, p.previousSleep)
}

// ExecuteWithPolicy is a convenience wrapper that creates a DefaultRetryPolicy
// and calls Execute. Equivalent to DefaultRetryPolicy().Execute(fn).
func ExecuteWithPolicy(fn func() error) error {
	return DefaultRetryPolicy().Execute(fn)
}

// ─── Error Context ───────────────────────────────────────────────────────────

// ErrorContext enriches errors with structured metadata for cross-node
// diagnostics. When errors propagate across distributed AgentRouter nodes,
// ErrorContext preserves the originating agent, task, operation, and timing
// so the receiving node can diagnose failures without access to the source
// machine's logs.
type ErrorContext struct {
	// Err is the underlying error being wrapped.
	Err error

	// Agent is the name of the agent that produced this error.
	Agent string

	// Task is the task text being executed when the error occurred.
	Task string

	// Operation is the specific operation (e.g., "Execute", "Health", "Retry").
	Operation string

	// Node is the originating node identifier (hostname or instance ID).
	Node string

	// Timestamp records when the error occurred.
	Timestamp time.Time

	// Attempt is the retry attempt number (0 = initial attempt).
	Attempt int

	// Category is the classified error category. Zero value means unclassified
	// (ClassifyError will be called on the underlying error when needed).
	Category ErrorCategory
}

// NewErrorContext creates an ErrorContext wrapping the given error with
// the specified metadata. Returns nil if err is nil.
func NewErrorContext(err error, agent, task, operation string) *ErrorContext {
	if err == nil {
		return nil
	}
	return &ErrorContext{
		Err:       err,
		Agent:     agent,
		Task:      task,
		Operation: operation,
		Timestamp: time.Now(),
		Category:  ClassifyError(err),
	}
}

// Error implements the error interface with a structured multi-line format
// suitable for log aggregation and cross-node diagnostics.
func (ec *ErrorContext) Error() string {
	var b strings.Builder
	b.WriteString("ErrorContext")
	if ec.Agent != "" {
		b.WriteString("[agent=")
		b.WriteString(ec.Agent)
		b.WriteString("]")
	}
	if ec.Operation != "" {
		b.WriteString("[op=")
		b.WriteString(ec.Operation)
		b.WriteString("]")
	}
	if ec.Node != "" {
		b.WriteString("[node=")
		b.WriteString(ec.Node)
		b.WriteString("]")
	}
	b.WriteString(": ")
	if ec.Err != nil {
		b.WriteString(ec.Err.Error())
	}
	return b.String()
}

// Unwrap returns the underlying error for errors.Is/errors.As chain traversal.
func (ec *ErrorContext) Unwrap() error {
	return ec.Err
}

// WithNode sets the originating node identifier. Useful when errors cross
// AgentRouter boundaries and the receiving node tags them with the source.
func (ec *ErrorContext) WithNode(node string) *ErrorContext {
	ec.Node = node
	return ec
}

// WithAttempt sets the retry attempt number.
func (ec *ErrorContext) WithAttempt(attempt int) *ErrorContext {
	ec.Attempt = attempt
	return ec
}

// WithCategory overrides the classified category.
func (ec *ErrorContext) WithCategory(cat ErrorCategory) *ErrorContext {
	ec.Category = cat
	return ec
}

// GetErrorContext extracts an ErrorContext from an error chain.
// Returns nil if no ErrorContext is found.
func GetErrorContext(err error) *ErrorContext {
	var ec *ErrorContext
	if errors.As(err, &ec) {
		return ec
	}
	return nil
}

// Summary returns a compact single-line summary suitable for metrics labels
// and alert annotations. Format: "agent/op: category: message".
func (ec *ErrorContext) Summary() string {
	cat := ec.Category
	if cat == ErrCatUnknown && ec.Err != nil {
		cat = ClassifyError(ec.Err)
	}
	msg := ""
	if ec.Err != nil {
		msg = ec.Err.Error()
		// Truncate long messages for summary use.
		if len(msg) > 120 {
			msg = msg[:117] + "..."
		}
	}
	if ec.Agent != "" && ec.Operation != "" {
		return fmt.Sprintf("%s/%s: %s: %s", ec.Agent, ec.Operation, cat, msg)
	}
	if ec.Operation != "" {
		return fmt.Sprintf("%s: %s: %s", ec.Operation, cat, msg)
	}
	return fmt.Sprintf("%s: %s", cat, msg)
}
