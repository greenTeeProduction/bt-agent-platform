package llm

import (
	"fmt"
	"net/http"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// checkRateLimit returns a *reliability.RateLimitError when resp is an
// HTTP 429, carrying the server-provided Retry-After duration (0 when the
// header is absent or unparseable, letting the retry policy fall back to
// its own backoff). Call it immediately after the HTTP round-trip, before
// interpreting the response body — 429 bodies are often non-JSON or carry
// a provider error object, and body-first handling would shadow this check.
func checkRateLimit(resp *http.Response, provider, model string) error {
	if resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	return &reliability.RateLimitError{
		RetryAfter: reliability.ParseRetryAfter(resp.Header.Get("Retry-After")),
		Message:    fmt.Sprintf("%s rate limited (model=%s)", provider, model),
	}
}
