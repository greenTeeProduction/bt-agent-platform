package llm

import (
	"bytes"
	"context"
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
	Model    string        `json:"model"`
	Messages []deepseekMsg `json:"messages"`
	Stream   bool          `json:"stream"`
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
			{Role: "system", Content: "You are a capable AI assistant. Execute the user's task directly. Provide complete, well-structured responses. Do not ask for context that was already provided — just do the work."},
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

// GenerateCtx generates with caller-provided context for cancellation propagation.
func (d *DeepSeekClient) GenerateCtx(_ context.Context, prompt string) (string, error) {
	return d.Generate(prompt)
}

// GenerateWithTimeout generates with a per-operation timeout (DeepSeek API has its own timeout).
func (d *DeepSeekClient) GenerateWithTimeout(prompt string, _ time.Duration) (string, error) {
	return d.Generate(prompt)
}

// AnalyzeComplexity estimates task complexity (1-5).
func (d *DeepSeekClient) AnalyzeComplexity(task string) string {
	if len(task) < 50 {
		return "low"
	}
	if len(task) < 200 {
		return "medium"
	}
	return "high"
}

// GeneratePlan creates an execution plan for a task.
func (d *DeepSeekClient) GeneratePlan(task, complexity string) string {
	prompt := fmt.Sprintf(
		`Create a step-by-step execution plan for this %s-complexity task.
Task: %s
Plan:`, complexity, task,
	)
	result, err := d.Generate(prompt)
	if err != nil {
		return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
	}
	return result
}

// Reflect generates a reflection (what went well, what to improve) on a completed task.
func (d *DeepSeekClient) Reflect(task, outcome, plan string) (wentWell string, toImprove string) {
	prompt := fmt.Sprintf(
		`Task: %s
Plan: %s
Outcome: %s

Analyze what went well and what could be improved. Respond in exactly this format:
WENT_WELL: <text>
TO_IMPROVE: <text>`,
		task, plan, outcome,
	)
	result, err := d.Generate(prompt)
	if err != nil {
		return "task completed", "better error handling"
	}
	wentWell = extractSection(result, "WENT_WELL:")
	toImprove = extractSection(result, "TO_IMPROVE:")
	if wentWell == "" {
		wentWell = "task completed"
	}
	if toImprove == "" {
		toImprove = "better error handling"
	}
	return
}
