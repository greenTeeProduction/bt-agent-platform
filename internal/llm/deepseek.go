package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DeepSeekClient implements the LLM interface using the DeepSeek API.
type DeepSeekClient struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// DeepSeekConfig holds DeepSeek connection settings.
type DeepSeekConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

// DefaultDeepSeekConfig returns config for DeepSeek v4 Pro.
func DefaultDeepSeekConfig() DeepSeekConfig {
	return DeepSeekConfig{
		APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		BaseURL: "https://api.deepseek.com/v1",
		Model:   "deepseek-v4-pro",
		Timeout: 120 * time.Second,
	}
}

// NewDeepSeekClient creates a new DeepSeek API client.
func NewDeepSeekClient(cfg DeepSeekConfig) *DeepSeekClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "deepseek-v4-pro"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	return &DeepSeekClient{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		client:  &http.Client{Timeout: cfg.Timeout},
	}
}

type deepseekRequest struct {
	Model    string          `json:"model"`
	Messages []deepseekMsg   `json:"messages"`
	Stream   bool            `json:"stream"`
}

type deepseekMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepseekResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Generate implements LLM.Generate for DeepSeek.
func (d *DeepSeekClient) Generate(prompt string) (string, error) {
	req := deepseekRequest{
		Model: d.model,
		Messages: []deepseekMsg{
			{Role: "system", Content: "You are a behavior tree evaluator. Answer concisely and accurately."},
			{Role: "user", Content: prompt},
		},
		Stream: false,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", d.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var dsResp deepseekResponse
	if err := json.Unmarshal(respBody, &dsResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if dsResp.Error != nil {
		return "", fmt.Errorf("deepseek api error: %s", dsResp.Error.Message)
	}

	if len(dsResp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return dsResp.Choices[0].Message.Content, nil
}

// AnalyzeComplexity estimates task complexity (1-5).
func (d *DeepSeekClient) AnalyzeComplexity(task string) string {
	if len(task) < 50 { return "low" }
	if len(task) < 200 { return "medium" }
	return "high"
}
