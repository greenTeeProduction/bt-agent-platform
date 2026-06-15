package fusion

import (
	"fmt"
	"strings"
	"time"
)

var QualityPreset = []string{
	"~anthropic/claude-opus-latest",
	"~openai/gpt-latest",
	"~google/gemini-pro-latest",
}

type Config struct {
	Enabled             bool     `json:"enabled"`
	Force               bool     `json:"force"`
	AnalysisModels      []string `json:"analysis_models"`
	JudgeModel          string   `json:"model"`
	MaxToolCalls        int      `json:"max_tool_calls"`
	MaxCompletionTokens int      `json:"max_completion_tokens"`
	Temperature         *float64 `json:"temperature,omitempty"`
	Timeout             time.Duration
}

func DefaultConfig() Config {
	return Config{
		Enabled:        true,
		AnalysisModels: append([]string(nil), QualityPreset...),
		JudgeModel:     QualityPreset[0],
		MaxToolCalls:   8,
		Timeout:        300 * time.Second,
	}
}

func (c Config) Normalize() Config {
	if len(c.AnalysisModels) == 0 {
		c.AnalysisModels = append([]string(nil), QualityPreset...)
	}
	if strings.TrimSpace(c.JudgeModel) == "" {
		c.JudgeModel = c.AnalysisModels[0]
	}
	if c.MaxToolCalls == 0 {
		c.MaxToolCalls = 8
	}
	if c.Timeout == 0 {
		c.Timeout = 300 * time.Second
	}
	return c
}

func (c Config) Validate() error {
	if len(c.AnalysisModels) == 0 {
		c.AnalysisModels = append([]string(nil), QualityPreset...)
	}
	if strings.TrimSpace(c.JudgeModel) == "" {
		c.JudgeModel = c.AnalysisModels[0]
	}
	if c.Timeout == 0 {
		c.Timeout = 300 * time.Second
	}
	if len(c.AnalysisModels) < 1 || len(c.AnalysisModels) > 8 {
		return fmt.Errorf("analysis_models must contain 1-8 models, got %d", len(c.AnalysisModels))
	}
	for i, m := range c.AnalysisModels {
		if strings.TrimSpace(m) == "" {
			return fmt.Errorf("analysis_models[%d] is empty", i)
		}
	}
	if c.MaxToolCalls < 1 || c.MaxToolCalls > 16 {
		return fmt.Errorf("max_tool_calls must be 1-16, got %d", c.MaxToolCalls)
	}
	if c.Temperature != nil && (*c.Temperature < 0 || *c.Temperature > 2) {
		return fmt.Errorf("temperature must be 0-2, got %f", *c.Temperature)
	}
	return nil
}
