package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Config / DefaultConfig tests
// =============================================================================

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ServerURL != "http://localhost:11434" {
		t.Errorf("ServerURL: expected %q, got %q", "http://localhost:11434", cfg.ServerURL)
	}
	if cfg.Model != "qwen3.6:35b-a3b" {
		t.Errorf("Model: expected %q, got %q", "qwen3.6:35b-a3b", cfg.Model)
	}
	if cfg.Timeout != 300*time.Second {
		t.Errorf("Timeout: expected %v, got %v", 300*time.Second, cfg.Timeout)
	}
}

func TestConfig_CustomTimeout(t *testing.T) {
	cfg := Config{
		ServerURL: "http://example.com:12345",
		Model:     "custom-model",
		Timeout:   60 * time.Second,
	}

	if cfg.ServerURL != "http://example.com:12345" {
		t.Errorf("ServerURL: expected %q, got %q", "http://example.com:12345", cfg.ServerURL)
	}
	if cfg.Model != "custom-model" {
		t.Errorf("Model: expected %q, got %q", "custom-model", cfg.Model)
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("Timeout: expected %v, got %v", 60*time.Second, cfg.Timeout)
	}
}

func TestConfig_ZeroValue(t *testing.T) {
	var cfg Config
	if cfg.ServerURL != "" {
		t.Errorf("zero-value ServerURL should be empty, got %q", cfg.ServerURL)
	}
	if cfg.Model != "" {
		t.Errorf("zero-value Model should be empty, got %q", cfg.Model)
	}
	if cfg.Timeout != 0 {
		t.Errorf("zero-value Timeout should be 0, got %v", cfg.Timeout)
	}
}

// =============================================================================
// NewClient error handling tests
// =============================================================================

func TestNewClient_EmptyModel_AcceptedByLangchaingo(t *testing.T) {
	// langchaingo's ollama.New() does not validate model at creation time;
	// validation happens at Call() time. Document this behavior.
	cfg := Config{
		ServerURL: "http://localhost:11434",
		Model:     "",
		Timeout:   5 * time.Second,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Logf("NewClient with empty model returned error: %v", err)
		return
	}
	if client == nil {
		t.Fatal("NewClient returned nil without error")
	}
	// The config should still be stored as-is.
	if client.cfg.Model != "" {
		t.Errorf("Model should be empty, got %q", client.cfg.Model)
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	// A truly malformed URL like "://invalid-url" causes log.Fatal in langchaingo.
	// Use a URL with valid syntax but non-existent TLD as a softer error case.
	cfg := Config{
		ServerURL: "http://invalid.invalid:11434",
		Model:     "test-model",
		Timeout:   5 * time.Second,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Logf("NewClient with invalid host returned error: %v", err)
		return
	}
	if client == nil {
		t.Fatal("NewClient returned nil without error")
	}
	// Config should be stored.
	if client.cfg.ServerURL != cfg.ServerURL {
		t.Errorf("ServerURL: expected %q, got %q", cfg.ServerURL, client.cfg.ServerURL)
	}
}

func TestNewClient_UnreachableHost(t *testing.T) {
	// Use a host that definitely doesn't exist, but ollama.New() likely
	// doesn't connect at creation time, so this tests config acceptance.
	cfg := Config{
		ServerURL: "http://192.0.2.1:11434", // TEST-NET-1, guaranteed non-routable
		Model:     "test-model",
		Timeout:   1 * time.Second,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Logf("NewClient with unreachable host returned error (may validate eagerly): %v", err)
		return
	}
	// If creation succeeded, verify the client was set up with our config.
	if client == nil {
		t.Fatal("NewClient returned nil client without error")
	}
	if client.cfg.ServerURL != cfg.ServerURL {
		t.Errorf("client.cfg.ServerURL: expected %q, got %q", cfg.ServerURL, client.cfg.ServerURL)
	}
	if client.cfg.Model != cfg.Model {
		t.Errorf("client.cfg.Model: expected %q, got %q", cfg.Model, client.cfg.Model)
	}
	if client.cfg.Timeout != cfg.Timeout {
		t.Errorf("client.cfg.Timeout: expected %v, got %v", cfg.Timeout, client.cfg.Timeout)
	}
	// Verify the config is stored correctly.
	if client.llm == nil {
		t.Error("client.llm should not be nil after NewClient")
	}
}

func TestNewClient_TimeoutConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{"short", 1 * time.Second},
		{"medium", 30 * time.Second},
		{"long", 300 * time.Second},
		{"zero", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := mockOllamaServer(func(_ map[string]any) string {
				return "ok"
			})
			defer srv.Close()

			cfg := Config{
				ServerURL: srv.URL,
				Model:     "test-model",
				Timeout:   tt.timeout,
			}
			client, err := NewClient(cfg)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if client.cfg.Timeout != tt.timeout {
				t.Errorf("Timeout: expected %v, got %v", tt.timeout, client.cfg.Timeout)
			}
		})
	}
}

// =============================================================================
// Client error-path / fallback tests (no mock server — force errors)
// =============================================================================

func TestClient_Generate_ConnectionRefused(t *testing.T) {
	cfg := Config{
		ServerURL: "http://127.0.0.1:1", // port 1 is typically closed / restricted
		Model:     "test-model",
		Timeout:   500 * time.Millisecond,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Skipf("NewClient rejected config: %v", err)
	}

	_, err = client.Generate("test prompt")
	if err == nil {
		t.Log("Generate succeeded despite unreachable server (may be running locally)")
	} else {
		t.Logf("Generate correctly errored: %v", err)
	}
}

func TestClient_AnalyzeComplexity_FallbackOnError(t *testing.T) {
	cfg := Config{
		ServerURL: "http://127.0.0.1:1",
		Model:     "test-model",
		Timeout:   100 * time.Millisecond,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Skipf("NewClient rejected config: %v", err)
	}

	result := client.AnalyzeComplexity("some task")
	// Should fall back to "medium" when LLM call fails.
	if result != "medium" {
		t.Errorf("expected fallback %q, got %q", "medium", result)
	}
}

func TestClient_GeneratePlan_FallbackOnError(t *testing.T) {
	cfg := Config{
		ServerURL: "http://127.0.0.1:1",
		Model:     "test-model",
		Timeout:   100 * time.Millisecond,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Skipf("NewClient rejected config: %v", err)
	}

	result := client.GeneratePlan("build feature", "high")
	// Should contain a fallback plan.
	if result == "" {
		t.Error("fallback plan should not be empty")
	}
	if !strings.Contains(result, "Analyze") || !strings.Contains(result, "Execute") {
		t.Errorf("fallback plan should contain step keywords, got: %s", result)
	}
}

func TestClient_Reflect_FallbackOnError(t *testing.T) {
	cfg := Config{
		ServerURL: "http://127.0.0.1:1",
		Model:     "test-model",
		Timeout:   100 * time.Millisecond,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Skipf("NewClient rejected config: %v", err)
	}

	wentWell, toImprove := client.Reflect("task", "outcome", "plan")
	if wentWell == "" {
		t.Error("fallback wentWell should not be empty")
	}
	if toImprove == "" {
		t.Error("fallback toImprove should not be empty")
	}
}

// =============================================================================
// extractSection tests
// =============================================================================

func TestExtractSection(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		marker   string
		expected string
	}{
		{
			name:     "WENT_WELL section",
			text:     "WENT_WELL: the code compiled\nTO_IMPROVE: add tests",
			marker:   "WENT_WELL:",
			expected: "the code compiled",
		},
		{
			name:     "TO_IMPROVE section",
			text:     "WENT_WELL: good\nTO_IMPROVE: write more tests",
			marker:   "TO_IMPROVE:",
			expected: "write more tests",
		},
		{
			name:     "marker not found",
			text:     "no marker here",
			marker:   "WENT_WELL:",
			expected: "",
		},
		{
			name:     "marker with trailing whitespace",
			text:     "WENT_WELL:   extra spaces   \nTO_IMPROVE: fix",
			marker:   "WENT_WELL:",
			expected: "extra spaces",
		},
		{
			name:     "marker at end of string",
			text:     "TO_IMPROVE: last section",
			marker:   "TO_IMPROVE:",
			expected: "last section",
		},
		{
			name:     "empty after marker",
			text:     "WENT_WELL: \nTO_IMPROVE: fix",
			marker:   "WENT_WELL:",
			expected: "",
		},
		{
			name:     "multi-line section",
			text:     "WENT_WELL: first line\nsecond line\nTO_IMPROVE: fix this",
			marker:   "WENT_WELL:",
			expected: "first line\nsecond line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSection(tt.text, tt.marker)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// =============================================================================
// DeepSeekConfig tests
// =============================================================================

func TestDefaultDeepSeekConfig(t *testing.T) {
	cfg := DefaultDeepSeekConfig()

	if cfg.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("BaseURL: expected %q, got %q", "https://api.deepseek.com/v1", cfg.BaseURL)
	}
	if cfg.Model != "deepseek-v4-pro" {
		t.Errorf("Model: expected %q, got %q", "deepseek-v4-pro", cfg.Model)
	}
	if cfg.Timeout != 120*time.Second {
		t.Errorf("Timeout: expected %v, got %v", 120*time.Second, cfg.Timeout)
	}
	// APIKey comes from env; we don't assert a specific value.
}

func TestDefaultDeepSeekConfig_WithEnvKey(t *testing.T) {
	os.Setenv("DEEPSEEK_API_KEY", "test-key-12345")
	defer os.Unsetenv("DEEPSEEK_API_KEY")

	cfg := DefaultDeepSeekConfig()
	if cfg.APIKey != "test-key-12345" {
		t.Errorf("APIKey: expected %q, got %q", "test-key-12345", cfg.APIKey)
	}
}

// =============================================================================
// NewDeepSeekClient tests
// =============================================================================

func TestNewDeepSeekClient_FullConfig(t *testing.T) {
	cfg := DeepSeekConfig{
		APIKey:  "sk-test",
		BaseURL: "https://custom.api.com/v2",
		Model:   "custom-model",
		Timeout: 90 * time.Second,
	}

	client := NewDeepSeekClient(cfg)

	if client.apiKey != "sk-test" {
		t.Errorf("apiKey: expected %q, got %q", "sk-test", client.apiKey)
	}
	if client.baseURL != "https://custom.api.com/v2" {
		t.Errorf("baseURL: expected %q, got %q", "https://custom.api.com/v2", client.baseURL)
	}
	if client.model != "custom-model" {
		t.Errorf("model: expected %q, got %q", "custom-model", client.model)
	}
	if client.client == nil {
		t.Fatal("http.Client should not be nil")
	}
	if client.client.Timeout != 90*time.Second {
		t.Errorf("HTTP client timeout: expected %v, got %v", 90*time.Second, client.client.Timeout)
	}
}

func TestNewDeepSeekClient_DefaultsApplied(t *testing.T) {
	tests := []struct {
		name      string
		cfg       DeepSeekConfig
		wantURL   string
		wantModel string
		wantTO    time.Duration
	}{
		{
			name:      "all empty",
			cfg:       DeepSeekConfig{APIKey: "key"},
			wantURL:   "https://api.deepseek.com/v1",
			wantModel: "deepseek-v4-pro",
			wantTO:    120 * time.Second,
		},
		{
			name:      "zero timeout",
			cfg:       DeepSeekConfig{APIKey: "key", Timeout: 0},
			wantURL:   "https://api.deepseek.com/v1",
			wantModel: "deepseek-v4-pro",
			wantTO:    120 * time.Second,
		},
		{
			name:      "empty BaseURL only",
			cfg:       DeepSeekConfig{APIKey: "key", Model: "m1", Timeout: 10 * time.Second},
			wantURL:   "https://api.deepseek.com/v1",
			wantModel: "m1",
			wantTO:    10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewDeepSeekClient(tt.cfg)
			if client.baseURL != tt.wantURL {
				t.Errorf("baseURL: expected %q, got %q", tt.wantURL, client.baseURL)
			}
			if client.model != tt.wantModel {
				t.Errorf("model: expected %q, got %q", tt.wantModel, client.model)
			}
			if client.client.Timeout != tt.wantTO {
				t.Errorf("timeout: expected %v, got %v", tt.wantTO, client.client.Timeout)
			}
		})
	}
}

// =============================================================================
// DeepSeekClient Generate tests (via httptest)
// =============================================================================

func TestDeepSeekClient_Generate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		// Verify auth header
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		resp := map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "DeepSeek response",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(DeepSeekConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "test-model",
		Timeout: 5 * time.Second,
	})

	result, err := client.Generate("hello")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if result != "DeepSeek response" {
		t.Errorf("expected %q, got %q", "DeepSeek response", result)
	}
}

func TestDeepSeekClient_Generate_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"error": map[string]any{
				"message": "rate limit exceeded",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(DeepSeekConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	_, err := client.Generate("hello")
	if err == nil {
		t.Error("expected error for API error response")
	} else {
		if !strings.Contains(err.Error(), "rate limit exceeded") {
			t.Errorf("error should mention rate limit, got: %v", err)
		}
	}
}

func TestDeepSeekClient_Generate_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"choices": []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewDeepSeekClient(DeepSeekConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	_, err := client.Generate("hello")
	if err == nil {
		t.Error("expected error for empty choices")
	} else {
		if !strings.Contains(err.Error(), "no choices") {
			t.Errorf("error should mention no choices, got: %v", err)
		}
	}
}

func TestDeepSeekClient_Generate_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json at all {{{"))
	}))
	defer srv.Close()

	client := NewDeepSeekClient(DeepSeekConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	_, err := client.Generate("hello")
	if err == nil {
		t.Error("expected unmarshal error")
	}
}

func TestDeepSeekClient_Generate_ConnectionRefused(t *testing.T) {
	client := NewDeepSeekClient(DeepSeekConfig{
		APIKey:  "test-key",
		BaseURL: "http://127.0.0.1:1",
		Timeout: 100 * time.Millisecond,
	})

	_, err := client.Generate("hello")
	if err == nil {
		t.Log("Generate succeeded despite unreachable server")
	} else {
		t.Logf("Generate correctly errored: %v", err)
	}
}

// =============================================================================
// DeepSeekClient AnalyzeComplexity tests
// =============================================================================

func TestDeepSeekClient_AnalyzeComplexity(t *testing.T) {
	client := NewDeepSeekClient(DeepSeekConfig{APIKey: "k"})

	tests := []struct {
		name     string
		task     string
		expected string
	}{
		{"short task -> low", "fix bug", "low"},
		{"medium task -> medium", "implement a new feature for the dashboard that shows user analytics and engagement metrics", "medium"},
		{"long task -> high", strings.Repeat("build a complete system with many components and extensive requirements ", 5), "high"},
		{"exactly 49 chars", strings.Repeat("x", 49), "low"},
		{"exactly 50 chars", strings.Repeat("x", 50), "medium"},
		{"exactly 199 chars", strings.Repeat("x", 199), "medium"},
		{"exactly 200 chars", strings.Repeat("x", 200), "high"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.AnalyzeComplexity(tt.task)
			if result != tt.expected {
				t.Errorf("expected %q, got %q (task len=%d)", tt.expected, result, len(tt.task))
			}
		})
	}
}

// =============================================================================
// Additional edge-case coverage for Ollama Client methods
// =============================================================================

func TestClient_AnalyzeComplexity_High(t *testing.T) {
	srv := mockOllamaServer(func(_ map[string]any) string {
		return "high"
	})
	defer srv.Close()

	client := newTestClient(t, srv)
	result := client.AnalyzeComplexity("complex task")
	if result != "high" {
		t.Errorf("expected %q, got %q", "high", result)
	}
}

func TestClient_Reflect_FallbackSections(t *testing.T) {
	// When LLM returns content without proper WENT_WELL/TO_IMPROVE markers,
	// the fallback values should be used.
	srv := mockOllamaServer(func(_ map[string]any) string {
		return "random response without markers"
	})
	defer srv.Close()

	client := newTestClient(t, srv)
	wentWell, toImprove := client.Reflect("task", "outcome", "plan")
	if wentWell != "task completed" {
		t.Errorf("fallback wentWell: expected %q, got %q", "task completed", wentWell)
	}
	if toImprove != "better error handling" {
		t.Errorf("fallback toImprove: expected %q, got %q", "better error handling", toImprove)
	}
}

func TestClient_Reflect_OnlyWentWell(t *testing.T) {
	// When LLM returns only WENT_WELL (no TO_IMPROVE), the second fallback should kick in.
	srv := mockOllamaServer(func(_ map[string]any) string {
		return "WENT_WELL: everything went great"
	})
	defer srv.Close()

	client := newTestClient(t, srv)
	wentWell, toImprove := client.Reflect("task", "outcome", "plan")
	if wentWell != "everything went great" {
		t.Errorf("wentWell: expected %q, got %q", "everything went great", wentWell)
	}
	if toImprove != "better error handling" {
		t.Errorf("fallback toImprove: expected %q, got %q", "better error handling", toImprove)
	}
}

// =============================================================================
// Interface compliance assertions
// =============================================================================

// Verify Client satisfies the LLM interface at compile time.
var _ LLM = (*Client)(nil)
