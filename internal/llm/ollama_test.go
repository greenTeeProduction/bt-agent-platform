package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/tracing"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := attempts.Add(1)
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
				if n := attempts.Load(); n < 2 {
					t.Fatalf("expected at least 2 attempts (initial + retry) for a retryable failure, got %d", n)
				}
			} else {
				if err == nil {
					t.Fatalf("expected an error for non-retryable failure %q, got nil", tc.errBody)
				}
				if n := attempts.Load(); n != 1 {
					t.Fatalf("expected exactly 1 attempt (no retry) for non-retryable failure %q, got %d", tc.errBody, n)
				}
			}
		})
	}
}

// --- GenerateWithMaxTokens (ADR-167 consequence: internal/llm.Client and its
// decorators don't yet implement the capability engine.generateOnce checks
// for via type assertion, so a configured ChainConfig.MaxTokens is silently
// discarded on every real production call — mock LLMs in internal/engine's
// tests are the only implementers today.) ---

// capNumPredictHandler returns an httptest handler that captures the
// "num_predict" chat option sent to Ollama and responds with a canned
// success, so the client-side assertions are the only thing under test.
func capNumPredictHandler(t *testing.T, captured *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Options struct {
				NumPredict int `json:"num_predict"`
			} `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		*captured = body.Options.NumPredict
		resp := map[string]any{
			"model":      "test-model",
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"message":    map[string]string{"role": "assistant", "content": "capped"},
			"done":       true,
		}
		respBody, _ := json.Marshal(resp)
		_, _ = w.Write(append(respBody, '\n'))
	}
}

func TestClient_GenerateWithMaxTokens_SetsNumPredictOption(t *testing.T) {
	var captured int
	server := httptest.NewServer(capNumPredictHandler(t, &captured))
	defer server.Close()

	client, err := NewClient(Config{ServerURL: server.URL, Model: "test-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	got, err := client.GenerateWithMaxTokens("prompt", 42)
	if err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if got != "capped" {
		t.Fatalf("got=%q, want %q", got, "capped")
	}
	if captured != 42 {
		t.Fatalf("num_predict sent to Ollama = %d, want 42 (ChainConfig.MaxTokens must reach the real request)", captured)
	}
}

func TestClient_GenerateWithMaxTokens_ZeroLeavesRequestUnbounded(t *testing.T) {
	var captured int
	server := httptest.NewServer(capNumPredictHandler(t, &captured))
	defer server.Close()

	client, err := NewClient(Config{ServerURL: server.URL, Model: "test-model", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.GenerateWithMaxTokens("prompt", 0); err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if captured != 0 {
		t.Fatalf("num_predict = %d, want 0 (unbounded) when maxTokens=0", captured)
	}
}

// stubMaxTokensLLM wraps stubLLM (declared in fallback_test.go) and
// additionally implements GenerateWithMaxTokens, standing in for a real
// production client (e.g. *Client after this change) so ErrorRecorder,
// TracedLLM, and FallbackLLM forwarding can be pinned without a live server.
type stubMaxTokensLLM struct {
	*stubLLM
	receivedMaxTokens int
	maxTokensCalls    int
}

func (s *stubMaxTokensLLM) GenerateWithMaxTokens(prompt string, maxTokens int) (string, error) {
	s.maxTokensCalls++
	s.receivedMaxTokens = maxTokens
	return s.Generate(prompt)
}

func TestErrorRecorder_GenerateWithMaxTokens_ForwardsToCapableInnerLLM(t *testing.T) {
	inner := &stubMaxTokensLLM{stubLLM: &stubLLM{name: "inner"}}
	rec := NewErrorRecorder(inner)

	got, err := rec.GenerateWithMaxTokens("prompt", 99)
	if err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if got != "inner:prompt" {
		t.Fatalf("got=%q, want %q", got, "inner:prompt")
	}
	if inner.maxTokensCalls != 1 || inner.receivedMaxTokens != 99 {
		t.Fatalf("expected inner GenerateWithMaxTokens called once with 99, got calls=%d received=%d", inner.maxTokensCalls, inner.receivedMaxTokens)
	}
}

func TestErrorRecorder_GenerateWithMaxTokens_FallsBackToGenerateWhenInnerLacksSupport(t *testing.T) {
	inner := &stubLLM{name: "inner"}
	rec := NewErrorRecorder(inner)

	got, err := rec.GenerateWithMaxTokens("prompt", 99)
	if err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if got != "inner:prompt" {
		t.Fatalf("got=%q, want %q", got, "inner:prompt")
	}
	if inner.calls != 1 {
		t.Fatalf("expected inner Generate called once (unbounded fallback), got %d", inner.calls)
	}
}

func TestErrorRecorder_GenerateWithMaxTokens_RecordsError(t *testing.T) {
	inner := &stubMaxTokensLLM{stubLLM: &stubLLM{name: "inner", err: errors.New("boom")}}
	rec := NewErrorRecorder(inner)

	if _, err := rec.GenerateWithMaxTokens("prompt", 10); err == nil {
		t.Fatal("expected error")
	}
	if rec.LastError() == nil {
		t.Fatal("expected LastError to be recorded from GenerateWithMaxTokens, matching Generate/GenerateCtx/GenerateWithTimeout")
	}
}

func TestTracedLLM_GenerateWithMaxTokens_ForwardsAndEmitsSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := tracing.GlobalTracer()
	tracing.SetGlobalTracer(tracing.NewOTelTracer(tp.Tracer("test")))
	t.Cleanup(func() { tracing.SetGlobalTracer(prev) })

	inner := &stubMaxTokensLLM{stubLLM: &stubLLM{name: "ok"}}
	traced := NewTracedLLM(inner, "stub")

	got, err := traced.GenerateWithMaxTokens("p", 55)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok:p" {
		t.Fatalf("got=%q, want %q", got, "ok:p")
	}
	if inner.maxTokensCalls != 1 || inner.receivedMaxTokens != 55 {
		t.Fatalf("expected inner GenerateWithMaxTokens called with 55, got calls=%d received=%d", inner.maxTokensCalls, inner.receivedMaxTokens)
	}
	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "llm.generate/stub" {
		t.Fatalf("spans = %v", spans)
	}
}

func TestFallbackLLM_GenerateWithMaxTokens_ForwardsToCapableModel(t *testing.T) {
	capable := &stubMaxTokensLLM{stubLLM: &stubLLM{name: "capable"}}
	chain := NewFallbackLLM([]NamedLLM{{Name: "m1", LLM: capable}})

	got, err := chain.GenerateWithMaxTokens("prompt", 33)
	if err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if got != "capable:prompt" {
		t.Fatalf("got=%q, want %q", got, "capable:prompt")
	}
	if capable.maxTokensCalls != 1 || capable.receivedMaxTokens != 33 {
		t.Fatalf("expected capable model's GenerateWithMaxTokens called with 33, got calls=%d received=%d", capable.maxTokensCalls, capable.receivedMaxTokens)
	}
}

func TestFallbackLLM_GenerateWithMaxTokens_FallsBackToUnboundedGenerateForIncapableModel(t *testing.T) {
	plain := &stubLLM{name: "plain"}
	chain := NewFallbackLLM([]NamedLLM{{Name: "m1", LLM: plain}})

	got, err := chain.GenerateWithMaxTokens("prompt", 33)
	if err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if got != "plain:prompt" {
		t.Fatalf("got=%q, want %q", got, "plain:prompt")
	}
	if plain.calls != 1 {
		t.Fatalf("expected plain model's Generate called once (unbounded fallback), got %d", plain.calls)
	}
}

func TestFallbackLLM_GenerateWithMaxTokens_TriesNextModelAfterPrimaryFailure(t *testing.T) {
	primary := &stubMaxTokensLLM{stubLLM: &stubLLM{name: "primary", err: errors.New("primary down")}}
	fallback := &stubMaxTokensLLM{stubLLM: &stubLLM{name: "fallback"}}
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "primary", LLM: primary},
		{Name: "fallback", LLM: fallback},
	})

	got, err := chain.GenerateWithMaxTokens("prompt", 20)
	if err != nil {
		t.Fatalf("GenerateWithMaxTokens: %v", err)
	}
	if got != "fallback:prompt" {
		t.Fatalf("got=%q, want %q", got, "fallback:prompt")
	}
	if fallback.maxTokensCalls != 1 || fallback.receivedMaxTokens != 20 {
		t.Fatalf("expected fallback model's GenerateWithMaxTokens called with 20, got calls=%d received=%d", fallback.maxTokensCalls, fallback.receivedMaxTokens)
	}
}
