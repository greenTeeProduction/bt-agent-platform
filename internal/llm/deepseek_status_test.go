package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

// TestDeepSeek_NonJSON5xxClassifiedRetryable pins the same status-before-parse
// mechanism openai_compat uses: a gateway 5xx with a non-JSON (HTML/empty)
// body must classify as a retryable infrastructure error, not as a
// non-retryable "unmarshal" validation error — the engine's chain retry policy
// (internal/engine/chains.go generateWithRetry) refuses retries for
// validation, so the misclassification suppressed retries of transient 503s
// in production.
func TestDeepSeek_NonJSON5xxClassifiedRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("<html>503 Service Temporarily Unavailable</html>"))
	}))
	defer server.Close()

	d := NewDeepSeekClient(DeepSeekConfig{APIKey: "k", BaseURL: server.URL, Timeout: time.Second})
	_, err := d.Generate("prompt")
	if err == nil {
		t.Fatal("expected an error from a 503")
	}
	if cat := reliability.ClassifyError(err); !cat.IsRetryable() {
		t.Fatalf("ClassifyError = %v (not retryable), want a retryable infrastructure category for a gateway 5xx", cat)
	}
}

// TestDeepSeek_4xxClassifiedNonRetryable pins the other half: a caller-side
// 4xx fails fast with a non-retryable typed category.
func TestDeepSeek_4xxClassifiedNonRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid request"}}`))
	}))
	defer server.Close()

	d := NewDeepSeekClient(DeepSeekConfig{APIKey: "k", BaseURL: server.URL, Timeout: time.Second})
	_, err := d.Generate("prompt")
	if err == nil {
		t.Fatal("expected an error from a 400")
	}
	if cat := reliability.ClassifyError(err); cat.IsRetryable() {
		t.Fatalf("ClassifyError = %v (retryable), want non-retryable for a caller-side 4xx", cat)
	}
}

// TestDeepSeek_GenerateCtxHonorsContext verifies GenerateCtx propagates the
// caller's context to the HTTP request instead of discarding it.
func TestDeepSeek_GenerateCtxHonorsContext(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer server.Close()
	defer close(block)

	d := NewDeepSeekClient(DeepSeekConfig{APIKey: "k", BaseURL: server.URL, Timeout: 30 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := d.GenerateCtx(ctx, "prompt")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a context-deadline error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("GenerateCtx ignored its context: the call did not return after the 50ms deadline")
	}
}
