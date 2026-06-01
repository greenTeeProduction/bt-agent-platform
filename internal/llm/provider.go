package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/config"
)

// NewProvider creates the appropriate LLM client based on configuration.
// Supports "ollama", "deepseek", and "acp" providers. Reads API keys,
// model settings, and ACP process settings from config or environment.
func NewProvider(cfg *config.Config) (LLM, error) {
	primary, primaryName, err := buildProvider(cfg.LLMProvider, primaryModel(cfg), cfg)
	if err != nil {
		return nil, err
	}

	fallbackSpecs := parseFallbackModels(cfg.FallbackModels, cfg.LLMProvider)
	if len(fallbackSpecs) == 0 {
		return primary, nil
	}

	models := []NamedLLM{{Name: primaryName, LLM: primary}}
	for _, spec := range fallbackSpecs {
		fallback, name, err := buildProvider(spec.Provider, spec.Model, cfg)
		if err != nil {
			return nil, fmt.Errorf("fallback model %q: %w", spec.Raw, err)
		}
		models = append(models, NamedLLM{Name: name, LLM: fallback})
	}
	return NewFallbackLLM(models), nil
}

func buildProvider(provider, model string, cfg *config.Config) (LLM, string, error) {
	switch provider {
	case "deepseek":
		dsCfg := DefaultDeepSeekConfig()
		if cfg.DeepSeekHost != "" {
			dsCfg.BaseURL = cfg.DeepSeekHost
		}
		if model != "" {
			dsCfg.Model = model
		} else if cfg.DeepSeekModel != "" {
			dsCfg.Model = cfg.DeepSeekModel
		}
		if cfg.DeepSeekKey != "" {
			dsCfg.APIKey = cfg.DeepSeekKey
		}
		if cfg.LLMTimeout != 0 {
			dsCfg.Timeout = time.Duration(cfg.LLMTimeout) * time.Second
		}
		return NewDeepSeekClient(dsCfg), fmt.Sprintf("deepseek:%s", dsCfg.Model), nil
	case "acp":
		client := NewACPClient(ACPConfig{
			Command: cfg.ACPCommand,
			Args:    splitACPArgs(cfg.ACPArgs),
			CWD:     cfg.ACPCwd,
			Timeout: config.Duration(cfg.LLMTimeout),
		})
		return client, "acp:" + cfg.ACPCommand, nil
	case "ollama":
		ollamaModel := model
		if ollamaModel == "" {
			ollamaModel = cfg.OllamaModel
		}
		client, err := NewClient(Config{
			ServerURL: cfg.OllamaHost,
			Model:     ollamaModel,
			Timeout:   config.Duration(cfg.LLMTimeout),
		})
		if err != nil {
			return nil, "", err
		}
		return client, fmt.Sprintf("ollama:%s", ollamaModel), nil
	default:
		return nil, "", fmt.Errorf("unknown LLM provider: %s (valid: ollama, deepseek, acp)", provider)
	}
}

type fallbackSpec struct {
	Provider string
	Model    string
	Raw      string
}

func primaryModel(cfg *config.Config) string {
	switch cfg.LLMProvider {
	case "deepseek":
		return cfg.DeepSeekModel
	case "ollama":
		return cfg.OllamaModel
	default:
		return ""
	}
}

func parseFallbackModels(raw string, defaultProvider string) []fallbackSpec {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	specs := make([]fallbackSpec, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}

		provider := defaultProvider
		model := item
		if before, after, ok := strings.Cut(item, ":"); ok {
			provider = strings.TrimSpace(before)
			model = strings.TrimSpace(after)
		} else if before, after, ok := strings.Cut(item, "/"); ok && isKnownProvider(before) {
			provider = strings.TrimSpace(before)
			model = strings.TrimSpace(after)
		}

		specs = append(specs, fallbackSpec{Provider: provider, Model: model, Raw: item})
	}
	return specs
}

func isKnownProvider(provider string) bool {
	switch provider {
	case "ollama", "deepseek", "acp":
		return true
	default:
		return false
	}
}

func splitACPArgs(args string) []string {
	if args == "" {
		return nil
	}
	return strings.Fields(args)
}
