package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestClient_GenerateCtx_RetryPolicyByFailureKind covers milestone 3/5 of the
// Q3 Reliability program: generateCtx's c.llm.Call(callCtx, prompt) call must
// be wrapped in the same reliability.DefaultRetryPolicy treatment as
// internal/knowledge/embeddings.go's GetEmbedding and
// internal/llm/openai_compat.go's GenerateWithModel, so a retryable Ollama
// failure (5xx-style server errors) gets a jittered retry instead of failing
// the caller on the first transient response, while a non-retryable failure
// (validation, auth) fails immediately without ever hitting the backend a
// second time. Today generateCtx has no retry loop at all, so every case
// here calls the backend exactly once and the retryable cases never get the
// chance to recover on their second attempt.
func TestClient_GenerateCtx_RetryPolicyByFailureKind(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		errBody   string
		retryable bool
	}{
		{
			name:      "internal server error retries then succeeds",
			status:    http.StatusInternalServerError,
			errBody:   "internal server error",
			retryable: true,
		},
		{
			name:      "service unavailable retries then succeeds",
			status:    http.StatusServiceUnavailable,
			errBody:   "service unavailable",
			retryable: true,
		},
		{
			name:      "malformed request fails without retry",
			status:    http.StatusBadRequest,
			errBody:   "invalid request: malformed json",
			retryable: false,
		},
		{
			name:      "unauthorized fails without retry",
			status:    http.StatusUnauthorized,
			errBody:   "unauthorized: invalid api key",
			retryable: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := atomic.AddInt32(&attempts, 1)
				if tc.retryable && n >= 2 {
					resp := map[string]any{
						"model":      "test-model",
						"created_at": time.Now().UTC().Format(time.RFC3339),
						"message":    map[string]string{"role": "assistant", "content": "recovered"},
						"done":       true,
					}
					body, _ := json.Marshal(resp)
					_, _ = w.Write(append(body, '\n'))
					return
				}
				w.WriteHeader(tc.status)
				body, _ := json.Marshal(map[string]string{"error": tc.errBody})
				_, _ = w.Write(append(body, '\n'))
			}))
			defer server.Close()

			client, err := NewClient(Config{
				ServerURL: server.URL,
				Model:     "test-model",
				Timeout:   5 * time.Second,
			})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}

			got, err := client.GenerateCtx(context.Background(), "prompt")

			if tc.retryable {
				if err != nil {
					t.Fatalf("expected the retry policy to recover from %q on the second attempt, got error: %v", tc.errBody, err)
				}
				if got != "recovered" {
					t.Fatalf("got=%q, want %q", got, "recovered")
				}
				if n := atomic.LoadInt32(&attempts); n < 2 {
					t.Fatalf("expected at least 2 attempts (initial + retry) for a retryable failure, got %d", n)
				}
			} else {
				if err == nil {
					t.Fatalf("expected an error for non-retryable failure %q, got nil", tc.errBody)
				}
				if n := atomic.LoadInt32(&attempts); n != 1 {
					t.Fatalf("expected exactly 1 attempt (no retry) for non-retryable failure %q, got %d", tc.errBody, n)
				}
			}
		})
	}
}
