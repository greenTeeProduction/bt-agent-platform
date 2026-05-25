package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tmc/langchaingo/llms/ollama"
)

// Config holds Ollama connection settings.
type Config struct {
	ServerURL string
	Model     string
	Timeout   time.Duration
}

// DefaultConfig returns the standard config for this Jetson setup.
func DefaultConfig() Config {
	return Config{
		ServerURL: "http://localhost:11434",
		Model:     "qwen3.6:35b-a3b",
		Timeout:   300 * time.Second,
	}
}

// LLM is the interface for language model operations used by the BT engine and factory.
type LLM interface {
	Generate(prompt string) (string, error)
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

// generate is the internal helper that calls the LLM and returns trimmed text.
func (c *Client) generate(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.cfg.Timeout)
	defer cancel()
	result, err := c.llm.Call(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("llm call: %w", err)
	}
	return strings.TrimSpace(result), nil
}

// Generate is the public entry point for raw LLM calls. Used by the agent factory.
func (c *Client) Generate(prompt string) (string, error) {
	return c.generate(prompt)
}

// AnalyzeComplexity classifies task complexity as "low", "medium", or "high".
func (c *Client) AnalyzeComplexity(task string) string {
	prompt := fmt.Sprintf(
		`Classify the complexity of this task as exactly one word: low, medium, or high.
Task: %s
Complexity:`, task,
	)
	result, err := c.generate(prompt)
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
func (c *Client) GeneratePlan(task, complexity string) string {
	prompt := fmt.Sprintf(
		`Create a step-by-step execution plan for this %s-complexity task.
Task: %s
Plan:`, complexity, task,
	)
	result, err := c.generate(prompt)
	if err != nil {
		return fmt.Sprintf("1. Analyze: %s\n2. Execute: %s\n3. Verify result", task, task)
	}
	return result
}

// Reflect generates a reflection (what went well, what to improve) on a completed task.
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
	result, err := c.generate(prompt)
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
