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
	return &OpenAICompatClient{
		apiKey:  cfg.APIKey,
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		model:   cfg.Model,
		appName: cfg.AppName,
		siteURL: cfg.SiteURL,
		client:  &http.Client{Timeout: cfg.Timeout},
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
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
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
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	var parsed openAICompatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("openai-compatible api error: %s", parsed.Error.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai-compatible api status %d: %s", resp.StatusCode, string(respBody))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
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
