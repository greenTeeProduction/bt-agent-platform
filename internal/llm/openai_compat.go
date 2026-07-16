package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

type OpenAICompatConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
	AppName string
	SiteURL string
}

type OpenAICompatClient struct {
	apiKey  string
	baseURL string
	model   string
	appName string
	siteURL string
	client  *http.Client
	breaker *reliability.CircuitBreaker
}

type openAICompatRequest struct {
	Model    string                `json:"model"`
	Messages []openAICompatMessage `json:"messages"`
	Stream   bool                  `json:"stream"`
}

type openAICompatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAICompatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewOpenAICompatClient(cfg OpenAICompatConfig) *OpenAICompatClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openrouter.ai/api/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "openrouter/auto"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return &OpenAICompatClient{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   cfg.Model,
		appName: cfg.AppName,
		siteURL: cfg.SiteURL,
		client:  &http.Client{Timeout: cfg.Timeout},
		breaker: reliability.NewCircuitBreaker("openai-compat:"+baseURL, 3, 60*time.Second),
	}
}

func (c *OpenAICompatClient) Generate(prompt string) (string, error) {
	return c.GenerateWithModel(context.Background(), c.model, "You are a capable AI assistant. Execute the user's task directly.", prompt)
}

func (c *OpenAICompatClient) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return c.GenerateWithModel(ctx, c.model, "You are a capable AI assistant. Execute the user's task directly.", prompt)
}

func (c *OpenAICompatClient) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.GenerateWithModel(ctx, c.model, "You are a capable AI assistant. Execute the user's task directly.", prompt)
}

func (c *OpenAICompatClient) GenerateWithModel(ctx context.Context, model, system, prompt string) (string, error) {
	if strings.TrimSpace(model) == "" {
		model = c.model
	}
	req := openAICompatRequest{
		Model: model,
		Messages: []openAICompatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	if !c.breaker.Allow() {
		return "", fmt.Errorf("openai-compatible API circuit breaker open for %s", c.baseURL)
	}

	// The HTTP call is wrapped in reliability.DefaultRetryPolicy()'s full-jitter
	// backoff so a retryable failure (429 rate-limited, 5xx service errors) gets
	// a jittered retry instead of failing the caller on the first transient
	// response, while a non-retryable status (400 validation, 401 auth) fails
	// immediately. The request must be rebuilt on every attempt: bytes.Reader
	// is exhausted after the first Do, so a retry with the original httpReq
	// would send an empty body.
	var result string
	policy := reliability.DefaultRetryPolicy()
	err = policy.ExecuteContext(ctx, func() error {
		httpReq, reqErr := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
		if reqErr != nil {
			return fmt.Errorf("create request: %w", reqErr)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		if c.siteURL != "" {
			httpReq.Header.Set("HTTP-Referer", c.siteURL)
		}
		if c.appName != "" {
			httpReq.Header.Set("X-Title", c.appName)
		}
		resp, doErr := c.client.Do(httpReq)
		if doErr != nil {
			return fmt.Errorf("http request: %w", doErr)
		}
		defer resp.Body.Close()
		// Rate limiting must be detected before the body is interpreted:
		// 429 bodies are often non-JSON or carry a provider error object.
		if rlErr := checkRateLimit(resp, "openai-compatible API", model); rlErr != nil {
			return rlErr
		}
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("read response: %w", readErr)
		}
		var parsed openAICompatResponse
		if unmarshalErr := json.Unmarshal(respBody, &parsed); unmarshalErr != nil {
			return fmt.Errorf("unmarshal response: %w", unmarshalErr)
		}
		if parsed.Error != nil {
			return fmt.Errorf("openai-compatible api error (status %d): %s", resp.StatusCode, parsed.Error.Message)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("openai-compatible api status %d: %s", resp.StatusCode, string(respBody))
		}
		if len(parsed.Choices) == 0 {
			return fmt.Errorf("no choices in response")
		}
		result = strings.TrimSpace(parsed.Choices[0].Message.Content)
		return nil
	})
	if err != nil {
		c.breaker.RecordFailure()
		return "", err
	}
	c.breaker.RecordSuccess()
	return result, nil
}

func (c *OpenAICompatClient) AnalyzeComplexity(task string) string {
	if len(task) < 50 {
		return "low"
	}
	if len(task) < 200 {
		return "medium"
	}
	return "high"
}

func (c *OpenAICompatClient) GeneratePlan(task, complexity string) string {
	result, err := c.Generate(fmt.Sprintf("Create a step-by-step execution plan for this %s-complexity task.\nTask: %s\nPlan:", complexity, task))
	if err != nil {
		return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
	}
	return result
}

func (c *OpenAICompatClient) Reflect(task, outcome, plan string) (string, string) {
	result, err := c.Generate(fmt.Sprintf("Task: %s\nPlan: %s\nOutcome: %s\nRespond exactly as WENT_WELL: ... and TO_IMPROVE: ...", task, plan, outcome))
	if err != nil {
		return "task completed", "better error handling"
	}
	return extractSection(result, "WENT_WELL:"), extractSection(result, "TO_IMPROVE:")
}
