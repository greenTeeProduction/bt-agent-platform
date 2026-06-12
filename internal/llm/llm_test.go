package llm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/tracing"
)

// mockOllamaServer creates an httptest server that mimics the Ollama /api/chat endpoint.
// handler receives the decoded request body and should return the response content string.
func mockOllamaServer(handler func(body map[string]any) string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/chat" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(bodyBytes, &body)

		responseText := handler(body)

		// Ollama /api/chat response format
		resp := map[string]any{
			"model":      "test-model",
			"created_at": "2024-01-01T00:00:00Z",
			"message": map[string]any{
				"role":    "assistant",
				"content": responseText,
			},
			"done": true,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// newTestClient creates a Client pointed at the given httptest server.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	cfg := Config{
		ServerURL: srv.URL,
		Model:     "test-model",
		Timeout:   5 * time.Second,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestClient_Generate(t *testing.T) {
	srv := mockOllamaServer(func(_ map[string]any) string {
		return "test response"
	})
	defer srv.Close()

	client := newTestClient(t, srv)

	result, err := client.Generate("hello")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result != "test response" {
		t.Errorf("expected %q, got %q", "test response", result)
	}
}

func TestClient_AnalyzeComplexity(t *testing.T) {
	srv := mockOllamaServer(func(_ map[string]any) string {
		return "low"
	})
	defer srv.Close()

	client := newTestClient(t, srv)

	result := client.AnalyzeComplexity("simple task")
	if result != "low" {
		t.Errorf("expected %q, got %q", "low", result)
	}
}

func TestClient_GeneratePlan(t *testing.T) {
	planText := "1. Analyze requirements\n2. Implement solution\n3. Test and verify"

	srv := mockOllamaServer(func(_ map[string]any) string {
		return planText
	})
	defer srv.Close()

	client := newTestClient(t, srv)

	result := client.GeneratePlan("build a feature", "medium")
	if result != planText {
		t.Errorf("expected %q, got %q", planText, result)
	}
}

func TestClient_Reflect(t *testing.T) {
	srv := mockOllamaServer(func(_ map[string]any) string {
		return "WENT_WELL: the implementation was clean\nTO_IMPROVE: add more tests"
	})
	defer srv.Close()

	client := newTestClient(t, srv)

	wentWell, toImprove := client.Reflect("build feature", "success", "step by step plan")
	if wentWell != "the implementation was clean" {
		t.Errorf("wentWell: expected %q, got %q", "the implementation was clean", wentWell)
	}
	if toImprove != "add more tests" {
		t.Errorf("toImprove: expected %q, got %q", "add more tests", toImprove)
	}
}

// Verify the LLM interface is satisfied.
var _ LLM = (*Client)(nil)

// TestClient_GenerateTracing verifies that LLM calls produce tracing spans
// when the global tracer is a ConsoleTracer.
func TestClient_GenerateTracing(t *testing.T) {
	tracer, output := tracing.TestTracer("test-llm")
	orig := tracing.GetGlobalTracer()
	tracing.SetGlobalTracer(tracer)
	defer tracing.SetGlobalTracer(orig)

	srv := mockOllamaServer(func(_ map[string]any) string {
		return "test response"
	})
	defer srv.Close()

	client := newTestClient(t, srv)

	result, err := client.Generate("hello world")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result != "test response" {
		t.Errorf("expected %q, got %q", "test response", result)
	}

	out := output()
	if out == "" {
		t.Error("expected tracing output, got empty string")
	}
	if !strings.Contains(out, "op=llm:generate") {
		t.Errorf("expected 'op=llm:generate' in trace output, got: %s", out)
	}
	if !strings.Contains(out, "llm.model=test-model") {
		t.Errorf("expected 'llm.model=test-model' in trace output, got: %s", out)
	}
	if !strings.Contains(out, "llm.prompt_len=11") {
		t.Errorf("expected 'llm.prompt_len=11' in trace output, got: %s", out)
	}
	if !strings.Contains(out, "llm.response_len=13") {
		t.Errorf("expected 'llm.response_len=13' in trace output, got: %s", out)
	}
}

// TestClient_GenerateTracing_NoopDefault verifies that without a global tracer
// set, LLM calls use the noop tracer and don't panic or log anything.
func TestClient_GenerateTracing_NoopDefault(t *testing.T) {
	// Save and clear the global tracer to test noop fallback
	orig := tracing.GetGlobalTracer()
	tracing.SetGlobalTracer(nil)
	defer tracing.SetGlobalTracer(orig)

	srv := mockOllamaServer(func(_ map[string]any) string {
		return "noop test response"
	})
	defer srv.Close()

	client := newTestClient(t, srv)

	result, err := client.Generate("test prompt")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result != "noop test response" {
		t.Errorf("expected %q, got %q", "noop test response", result)
	}
	// No panics = pass
}
