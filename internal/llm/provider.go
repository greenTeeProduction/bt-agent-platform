package llm

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/config"
)

// NewProvider creates the appropriate LLM client based on configuration.
// Supports "ollama" and "deepseek" providers. Reads API keys and model
// settings from config or environment (DEEPSEEK_API_KEY fallback).
func NewProvider(cfg *config.Config) (LLM, error) {
	switch cfg.LLMProvider {
	case "deepseek":
		key := cfg.DeepSeekKey
		dsCfg := DefaultDeepSeekConfig()
		// Override from config
		if cfg.DeepSeekHost != "" {
			dsCfg.BaseURL = cfg.DeepSeekHost
		}
		if cfg.DeepSeekModel != "" {
			dsCfg.Model = cfg.DeepSeekModel
		}
		if key != "" {
			dsCfg.APIKey = key
		}
		return NewDeepSeekClient(dsCfg), nil
	case "ollama":
		// Only load langchaingo for Ollama (avoids dep for DeepSeek)
		return NewClient(Config{
			ServerURL: cfg.OllamaHost,
			Model:     cfg.OllamaModel,
			Timeout:   300_000_000_000, // 300s in nanoseconds
		})
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s (valid: ollama, deepseek)", cfg.LLMProvider)
	}
}
