package llm

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// ErrorRecorder decorates an LLM and records the most recent Generate*
// error. BT engine nodes flatten LLM errors into blackboard strings, which
// severs the error chain before it reaches the agent-level retry policy.
// The recorder preserves the typed error at the one seam every engine call
// passes through (bb.LLM), so the runner can re-attach it with %w and the
// retry policy can classify it (and honor Retry-After) via errors.As.
type ErrorRecorder struct {
	LLM

	mu           sync.Mutex
	lastErr      error
	rateLimitErr error
}

// NewErrorRecorder wraps client for a single run. Read LastError after the
// run completes to retrieve the preserved error chain.
func NewErrorRecorder(client LLM) *ErrorRecorder {
	return &ErrorRecorder{LLM: client}
}

func (r *ErrorRecorder) Generate(prompt string) (string, error) {
	result, err := r.LLM.Generate(prompt)
	r.record(err)
	return result, err
}

func (r *ErrorRecorder) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	result, err := r.LLM.GenerateCtx(ctx, prompt)
	r.record(err)
	return result, err
}

func (r *ErrorRecorder) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	result, err := r.LLM.GenerateWithTimeout(prompt, timeout)
	r.record(err)
	return result, err
}

func (r *ErrorRecorder) record(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = err
	var rle *reliability.RateLimitError
	if errors.As(err, &rle) {
		r.rateLimitErr = err
	}
}

// LastError returns the most recent recorded error. A rate-limit error takes
// precedence over a later unrelated failure so the server-provided
// Retry-After survives to the retry policy. Returns nil when no Generate*
// call failed.
func (r *ErrorRecorder) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rateLimitErr != nil {
		return r.rateLimitErr
	}
	return r.lastErr
}

var _ LLM = (*ErrorRecorder)(nil)
