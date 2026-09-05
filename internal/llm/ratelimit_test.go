package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// A realistic 429: JSON error body plus Retry-After header. The typed
// RateLimitError must win over the provider error-body handling.
func rateLimitedServer(t *testing.T, retryAfter string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
	}))
}

// retryAfter is kept to a single second (not a realistic multi-minute
// window) because GenerateWithModel now retries through
// reliability.DefaultRetryPolicy(), which honors the server's Retry-After
// value for real between attempts (milestone 2/5 of the Q3 Reliability
// program) — a server that always 429s and a large header would make this
// unit test sleep out multiple real Retry-After windows before its 3
// attempts are exhausted.
func TestOpenAICompat_429ReturnsRateLimitError(t *testing.T) {
	server := rateLimitedServer(t, "1")
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Model: "default", Timeout: time.Second})
	_, err := client.GenerateWithModel(context.Background(), "model-a", "sys", "prompt")

	var rle *reliability.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 1*time.Second {
		t.Errorf("RetryAfter = %v, want 1s", rle.RetryAfter)
	}
	if !strings.Contains(rle.Message, "model=model-a") {
		t.Errorf("message should carry the per-call model, got %q", rle.Message)
	}
}

func TestOpenAICompat_429WithoutRetryAfterHeader(t *testing.T) {
	server := rateLimitedServer(t, "")
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Timeout: time.Second})
	_, err := client.Generate("prompt")

	var rle *reliability.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 (fall back to policy backoff)", rle.RetryAfter)
	}
}

func TestOpenAICompat_429NonJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`<html>Too Many Requests</html>`))
	}))
	defer server.Close()

	client := NewOpenAICompatClient(OpenAICompatConfig{BaseURL: server.URL, Timeout: time.Second})
	_, err := client.Generate("prompt")

	if _, ok := errors.AsType[*reliability.RateLimitError](err); !ok {
		t.Fatalf("expected RateLimitError for non-JSON 429 body, got %T: %v", err, err)
	}
}

func TestDeepSeek_429ReturnsRateLimitError(t *testing.T) {
	server := rateLimitedServer(t, "60")
	defer server.Close()

	client := NewDeepSeekClient(DeepSeekConfig{APIKey: "k", BaseURL: server.URL, Model: "deepseek-v4-pro", Timeout: time.Second})
	_, err := client.Generate("prompt")

	var rle *reliability.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("expected RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 60*time.Second {
		t.Errorf("RetryAfter = %v, want 60s", rle.RetryAfter)
	}
}

func TestFallbackLLM_PreservesRateLimitErrorChain(t *testing.T) {
	rateLimited := &stubLLM{name: "primary", err: &reliability.RateLimitError{
		RetryAfter: 30 * time.Second,
		Message:    "primary rate limited",
	}}
	chain := NewFallbackLLM([]NamedLLM{
		{Name: "primary", LLM: rateLimited},
		{Name: "fallback", LLM: &stubLLM{name: "fallback", err: errors.New("fallback down")}},
	})

	_, err := chain.Generate("hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := reliability.RetryAfterFromError(err); got != 30*time.Second {
		t.Fatalf("Retry-After lost through fallback aggregation: got %v, want 30s", got)
	}
}

func TestErrorRecorder_PrefersRateLimitError(t *testing.T) {
	rec := NewErrorRecorder(nil)
	rec.record(&reliability.RateLimitError{RetryAfter: 10 * time.Second, Message: "limited"})
	rec.record(errors.New("later unrelated failure"))

	if got := reliability.RetryAfterFromError(rec.LastError()); got != 10*time.Second {
		t.Fatalf("LastError should prefer the rate-limit error, got RetryAfter %v", got)
	}
}

func TestErrorRecorder_RecordsLastError(t *testing.T) {
	rec := NewErrorRecorder(&stubLLM{name: "s", err: errors.New("boom")})
	if _, err := rec.Generate("p"); err == nil {
		t.Fatal("expected error")
	}
	if rec.LastError() == nil || !strings.Contains(rec.LastError().Error(), "boom") {
		t.Fatalf("LastError = %v, want recorded boom", rec.LastError())
	}
	if _, err := rec.GenerateCtx(context.Background(), "p"); err == nil {
		t.Fatal("expected error")
	}
}
