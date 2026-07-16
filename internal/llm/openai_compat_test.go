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
		},
		{
			name:      "401 unauthorized fails without retry",
			status:    http.StatusUnauthorized,
			body:      `{"error":{"message":"invalid api key"}}`,
			retryable: false,
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
			}
		})
	}
}
