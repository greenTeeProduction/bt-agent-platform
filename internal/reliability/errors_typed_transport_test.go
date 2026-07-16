package reliability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"testing"
)

// A typed *url.Error is definitionally a transport-layer failure — its
// message can contain anything. The raw substring validation patterns ran
// first and misclassified a real production shape: an EOF POST to an
// httptest server on port 40053 ("400" is a validation pattern) was refused
// a retry as a "validation error", flaking make test whenever the port
// lottery landed on a 400-containing port — and, in production, leaving
// genuinely transient mid-request drops (ollama restarts) unretried.
func TestClassifyErrorTypedTransportBeatsValidationSubstrings(t *testing.T) {
	err := fmt.Errorf("ollama embedding: %w", &url.Error{
		Op:  "Post",
		URL: "http://127.0.0.1:40053/api/embeddings",
		Err: io.EOF,
	})
	if got := ClassifyError(err); got != ErrCatNetwork {
		t.Fatalf("EOF POST via typed url.Error = %v, want %v (retryable network)", got, ErrCatNetwork)
	}
}

// Timeout precedence must survive the typed-transport hoist: a url.Error
// wrapping a deadline is still a timeout, not a plain network error.
func TestClassifyErrorTypedTransportTimeoutKeepsTimeoutPrecedence(t *testing.T) {
	err := fmt.Errorf("ollama embedding: %w", &url.Error{
		Op:  "Post",
		URL: "http://127.0.0.1:40053/api/embeddings",
		Err: context.DeadlineExceeded,
	})
	if got := ClassifyError(err); got != ErrCatTimeout {
		t.Fatalf("deadline via typed url.Error = %v, want %v", got, ErrCatTimeout)
	}
}

// Untyped response-level errors keep the existing string classification —
// a real HTTP 400 with no transport error in the chain stays validation.
func TestClassifyErrorUntypedBadRequestStaysValidation(t *testing.T) {
	err := errors.New("ollama api error: bad request 400: model name malformed")
	if got := ClassifyError(err); got != ErrCatValidation {
		t.Fatalf("untyped 400 response = %v, want %v", got, ErrCatValidation)
	}
}

// A typed *url.Error is definitionally a transport-layer failure the same
// way it is for validation substrings above — but isAuthError still runs
// before isTypedNetworkError, so a URL that happens to embed "401" (an auth
// pattern) still wins the race today. Once the typed-transport check is
// hoisted above the auth string check (mirroring 19bbae6's hoist above
// isValidationError), this must classify as network, not auth.
func TestClassifyErrorTypedTransportBeatsAuthSubstrings(t *testing.T) {
	err := fmt.Errorf("ollama embedding: %w", &url.Error{
		Op:  "Post",
		URL: "http://127.0.0.1:40170/api/embeddings",
		Err: io.EOF,
	})
	if got := ClassifyError(err); got != ErrCatNetwork {
		t.Fatalf("EOF POST via typed url.Error with \"401\" in URL = %v, want %v (retryable network)", got, ErrCatNetwork)
	}
}

// Untyped auth errors keep the existing string classification — a real
// 401 response with no transport error in the chain stays auth.
func TestClassifyErrorUntypedUnauthorizedStaysAuth(t *testing.T) {
	err := errors.New("ollama api error: unauthorized: invalid api key")
	if got := ClassifyError(err); got != ErrCatAuth {
		t.Fatalf("untyped unauthorized response = %v, want %v", got, ErrCatAuth)
	}
}

// failingResultExecutor returns a populated result together with an error —
// the shape of a graceful non-success run (e.g. the rate-limit carryover).
type failingResultExecutor struct {
	res   *AgentResult
	err   error
	calls int
}

func (f *failingResultExecutor) Execute(agent, task string) (*AgentResult, error) {
	f.calls++
	return f.res, f.err
}
func (f *failingResultExecutor) Health() error  { return nil }
func (f *failingResultExecutor) String() string { return "failing-with-result" }

// The failover loop must not discard a failed executor's result: the caller
// needs the outcome (e.g. goap_fusion_rate_limited) to classify the failure.
func TestAgentRouterPreservesResultFromFailedExecutor(t *testing.T) {
	exec := &failingResultExecutor{
		res: &AgentResult{Agent: "a", Outcome: "goap_fusion_rate_limited", Output: "paused"},
		err: errors.New("agent outcome: goap_fusion_rate_limited: paused"),
	}
	r := NewAgentRouter(exec)
	res, err := r.Execute("a", "task")
	if err == nil {
		t.Fatal("executor error must propagate")
	}
	if res == nil {
		t.Fatal("the failed executor's result must survive to the caller — dropping it loses the sentinel outcome")
	}
	if res.Outcome != "goap_fusion_rate_limited" {
		t.Fatalf("Outcome = %q, want the sentinel preserved", res.Outcome)
	}
}
