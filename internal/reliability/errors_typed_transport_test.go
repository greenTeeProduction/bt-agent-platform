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
