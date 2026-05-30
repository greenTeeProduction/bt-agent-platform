package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
	"github.com/tmc/langchaingo/llms/ollama"
)

// Config holds Ollama connection settings.
type Config struct {
	ServerURL string
	Model     string
	Timeout   time.Duration
}

// DefaultConfig reads from the platform config (config.json or env vars).
// Falls back to localhost:11434 / qwen3.6:35b-a3b / 300s if Load fails.
func DefaultConfig() Config {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return Config{
			ServerURL: "http://localhost:11434",
			Model:     "qwen3.6:35b-a3b",
			Timeout:   300 * time.Second,
		}
	}
	return Config{
		ServerURL: cfg.OllamaHost,
		Model:     cfg.OllamaModel,
		Timeout:   time.Duration(cfg.LLMTimeout) * time.Second,
	}
}

// LLM is the interface for language model operations used by the BT engine and factory.
type LLM interface {
	Generate(prompt string) (string, error)
	GenerateCtx(ctx context.Context, prompt string) (string, error)
	GenerateWithTimeout(prompt string, timeout time.Duration) (string, error)
	AnalyzeComplexity(task string) string
	GeneratePlan(task, complexity string) string
	Reflect(task, outcome, plan string) (wentWell string, toImprove string)
}

// Client wraps langchaingo's Ollama LLM with convenience methods for the BT harness.
type Client struct {
	cfg Config
	llm *ollama.LLM
}

// NewClient creates a new Ollama client via langchaingo.
func NewClient(cfg Config) (*Client, error) {
	llm, err := ollama.New(
		ollama.WithModel(cfg.Model),
		ollama.WithServerURL(cfg.ServerURL),
	)
	if err != nil {
		return nil, fmt.Errorf("create ollama llm: %w", err)
	}
	return &Client{cfg: cfg, llm: llm}, nil
}

// generateCtx calls the LLM with a caller-provided context and timeout.
func (c *Client) generateCtx(ctx context.Context, timeout time.Duration, prompt string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := c.llm.Call(callCtx, prompt)
	if err != nil {
		return "", fmt.Errorf("llm call: %w", err)
	}
	return strings.TrimSpace(result), nil
}

// generate is the legacy helper using context.Background() and the default timeout.
func (c *Client) generate(prompt string) (string, error) {
	return c.generateCtx(context.Background(), c.cfg.Timeout, prompt)
}

// Generate is the public entry point for raw LLM calls. Used by the agent factory.
func (c *Client) Generate(prompt string) (string, error) {
	return c.generate(prompt)
}

// GenerateCtx generates with a caller-provided context for cancellation propagation.
func (c *Client) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return c.generateCtx(ctx, c.cfg.Timeout, prompt)
}

// GenerateWithTimeout generates with a per-operation timeout override.
func (c *Client) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return c.generateCtx(context.Background(), timeout, prompt)
}

// AnalyzeComplexity classifies task complexity as "low", "medium", or "high".
// Uses a short 30s timeout since this is a simple classification.
func (c *Client) AnalyzeComplexity(task string) string {
	prompt := fmt.Sprintf(
		`Classify the complexity of this task as exactly one word: low, medium, or high.
Task: %s
Complexity:`, task,
	)
	result, err := c.generateCtx(context.Background(), 30*time.Second, prompt)
	if err != nil {
		return "medium"
	}
	switch {
	case strings.Contains(strings.ToLower(result), "low"):
		return "low"
	case strings.Contains(strings.ToLower(result), "high"):
		return "high"
	default:
		return "medium"
	}
}

// GeneratePlan creates an execution plan for a task.
// Uses a 60s timeout — plans need reasoning but shouldn't take forever.
func (c *Client) GeneratePlan(task, complexity string) string {
	prompt := fmt.Sprintf(
		`Create a step-by-step execution plan for this %s-complexity task.
Task: %s
Plan:`, complexity, task,
	)
	result, err := c.generateCtx(context.Background(), 60*time.Second, prompt)
	if err != nil {
		return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
	}
	return result
}

// Reflect generates a reflection (what went well, what to improve) on a completed task.
// Uses a 60s timeout — reflection benefits from thoughtful output.
func (c *Client) Reflect(task, outcome, plan string) (wentWell string, toImprove string) {
	prompt := fmt.Sprintf(
		`Task: %s
Plan: %s
Outcome: %s

Analyze what went well and what could be improved. Respond in exactly this format:
WENT_WELL: <text>
TO_IMPROVE: <text>`,
		task, plan, outcome,
	)
	result, err := c.generateCtx(context.Background(), 60*time.Second, prompt)
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

func extractSection(text, marker string) string {
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	end := len(text)
	// Find next section marker
	for _, m := range []string{"\nWENT_WELL:", "\nTO_IMPROVE:"} {
		if i := strings.Index(text[start:], m); i >= 0 {
			if start+i < end {
				end = start + i
			}
			break
		}
	}
	return strings.TrimSpace(text[start:end])
}
