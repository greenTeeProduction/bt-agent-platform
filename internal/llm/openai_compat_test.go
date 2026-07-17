package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

func TestOpenAICompat_GenerateWithModel_SendsChatCompletion(t *testing.T) {
	var gotModel string
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req openAICompatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = req.Model
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages: %#v", req.Messages)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"panel answer"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{APIKey: "key", BaseURL: server.URL, Model: "default", Timeout: time.Second})
	got, err := client.GenerateWithModel(context.Background(), "model-a", "sys", "prompt")
	if err != nil {
		t.Fatalf("GenerateWithModel error: %v", err)
	}
	if got != "panel answer" || gotModel != "model-a" || gotAuth != "Bearer key" {
		t.Fatalf("got=%q model=%q auth=%q", got, gotModel, gotAuth)
	}
}

// TestOpenAICompat_NonJSONServerError_IsRetried covers the gap the
// JSON-error-body retry test misses: a proxy-level 5xx (nginx / Cloudflare /
// ALB) returns an HTML or empty body, not a provider JSON error object. The
// pre-fix code unmarshaled the body before inspecting the status, so the JSON
// parse failure ("unmarshal response: ...") was classified as a non-retryable
// validation error and the transient 503 failed on the first attempt with zero
// retries — exactly the case the retry wrapper was added to handle.
func TestOpenAICompat_NonJSONServerError_IsRetried(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n >= 2 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
			return
		}
		// Non-JSON body, the norm for gateway-level 5xx.
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html><body>503 Service Temporarily Unavailable</body></html>"))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: 5 * time.Second})
	got, err := client.GenerateWithModel(context.Background(), "model-a", "sys", "prompt")
	if err != nil {
		t.Fatalf("expected the retry policy to recover from a non-JSON 503 on the second attempt, got error: %v", err)
	}
	if got != "recovered" {
		t.Fatalf("got=%q, want %q", got, "recovered")
	}
	if n := atomic.LoadInt32(&attempts); n < 2 {
		t.Fatalf("expected at least 2 attempts (initial 503 + retry) for a non-JSON 5xx, got %d", n)
	}
}

// TestOpenAICompat_ClientErrorsDoNotTripBreaker verifies that non-retryable
// caller-side failures (400 validation, 401 auth) do not walk the per-baseURL
// circuit breaker toward open. A backend that keeps returning 400 for one
// malformed prompt must not deny service to well-formed requests sharing the
// same client — only infrastructure failures (5xx/network/timeout/rate-limit)
// should open the breaker.
func TestOpenAICompat_ClientErrorsDoNotTripBreaker(t *testing.T) {
	var serves int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&serves, 1) <= 5 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid request"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: 5 * time.Second})
	// The breaker threshold is 3; five 400s would open it if 400s counted as
	// breaker failures.
	for i := 0; i < 5; i++ {
		if _, err := client.GenerateWithModel(context.Background(), "m", "sys", "bad"); err == nil {
			t.Fatalf("call %d: expected a 400 error", i)
		}
	}
	// A subsequent well-formed request must still reach the backend, not be
	// rejected by an open breaker.
	got, err := client.GenerateWithModel(context.Background(), "m", "sys", "good")
	if err != nil {
		t.Fatalf("well-formed request after 5 client errors was blocked (breaker wrongly tripped by 4xx): %v", err)
	}
	if got != "ok" {
		t.Fatalf("got=%q, want %q", got, "ok")
	}
}

// TestOpenAICompat_RetryableFailureRecordsBreakerFailure pins the positive
// half of the breaker gating: a retryable failure that exhausts its retries
// must record a breaker failure, or the breaker could never open on a real
// outage (the negative half — 4xx not counting — is pinned by
// TestOpenAICompat_ClientErrorsDoNotTripBreaker).
func TestOpenAICompat_RetryableFailureRecordsBreakerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>503</html>"))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: 5 * time.Second})
	if _, err := client.GenerateWithModel(context.Background(), "m", "sys", "p"); err == nil {
		t.Fatal("expected an error from a persistent 503")
	}
	if n := client.breaker.FailureCount(); n != 1 {
		t.Fatalf("breaker FailureCount = %d, want 1 (a retry-exhausted 5xx must record exactly one breaker failure)", n)
	}
}

// TestOpenAICompat_OpenBreakerRejectsWithoutRequest verifies an open breaker
// fails fast without hitting the backend.
func TestOpenAICompat_OpenBreakerRejectsWithoutRequest(t *testing.T) {
	var serves int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&serves, 1)
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: time.Second})
	for i := 0; i < 3; i++ { // threshold is 3
		client.breaker.RecordFailure()
	}
	_, err := client.GenerateWithModel(context.Background(), "m", "sys", "p")
	if err == nil || !strings.Contains(err.Error(), "circuit breaker open") {
		t.Fatalf("expected a circuit-breaker-open error, got: %v", err)
	}
	if n := atomic.LoadInt32(&serves); n != 0 {
		t.Fatalf("open breaker must not reach the backend, saw %d requests", n)
	}
}

func TestOpenAICompat_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: time.Second})
	if _, err := client.Generate("prompt"); err == nil {
		t.Fatal("expected error response")
	}
}

// TestOpenAICompat_GenerateWithModel_RetryPolicyByStatusCode covers milestone
// 2/5 of the Q3 Reliability program: GenerateWithModel's c.client.Do(httpReq)
// call must be wrapped in the same reliability.RetryPolicy/CircuitBreaker
// treatment as internal/knowledge/embeddings.go's GetEmbedding, so a
// retryable HTTP status (429 rate-limited, 503 service-unavailable) gets a
// jittered retry instead of failing the caller on the first transient
// response, while a non-retryable status (400 validation, 401 auth) fails
// immediately without ever hitting the backend a second time. Today
// GenerateWithModel has no retry loop at all, so every case here calls the
// backend exactly once and the retryable cases never get the chance to
// recover on their second attempt.
func TestOpenAICompat_GenerateWithModel_RetryPolicyByStatusCode(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		retryable bool
		wantCat   reliability.ErrorCategory
	}{
		{
			name:      "429 too many requests retries then succeeds",
			status:    http.StatusTooManyRequests,
			body:      `{"error":{"message":"rate limit exceeded"}}`,
			retryable: true,
		},
		{
			name:      "503 service unavailable retries then succeeds",
			status:    http.StatusServiceUnavailable,
			body:      `{"error":{"message":"service unavailable"}}`,
			retryable: true,
		},
		{
			name:      "400 bad request fails without retry",
			status:    http.StatusBadRequest,
			body:      `{"error":{"message":"invalid request"}}`,
			retryable: false,
			wantCat:   reliability.ErrCatValidation,
		},
		{
			name:      "401 unauthorized fails without retry",
			status:    http.StatusUnauthorized,
			body:      `{"error":{"message":"invalid api key"}}`,
			retryable: false,
			wantCat:   reliability.ErrCatAuth,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := atomic.AddInt32(&attempts, 1)
				if tc.retryable && n >= 2 {
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"recovered"}}]}`))
					return
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: 5 * time.Second})
			got, err := client.GenerateWithModel(context.Background(), "model-a", "sys", "prompt")

			if tc.retryable {
				if err != nil {
					t.Fatalf("expected the retry policy to recover from a %d on the second attempt, got error: %v", tc.status, err)
				}
				if got != "recovered" {
					t.Fatalf("got=%q, want %q", got, "recovered")
				}
				if n := atomic.LoadInt32(&attempts); n < 2 {
					t.Fatalf("expected at least 2 attempts (initial %d + retry) for a retryable status, got %d", tc.status, n)
				}
			} else {
				if err == nil {
					t.Fatalf("expected an error for non-retryable status %d, got nil", tc.status)
				}
				if n := atomic.LoadInt32(&attempts); n != 1 {
					t.Fatalf("expected exactly 1 attempt (no retry) for non-retryable status %d, got %d", tc.status, n)
				}
				if got := reliability.ClassifyError(err); got != tc.wantCat {
					t.Fatalf("ClassifyError = %v, want %v for status %d (typed status classification must survive the retry wrapper)", got, tc.wantCat, tc.status)
				}
			}
		})
	}
}
