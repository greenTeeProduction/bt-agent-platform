package config

import (
	"os"
	"testing"
)

func TestEnvOverride_OpenRouter(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	t.Setenv("BT_LLM_PROVIDER", "openrouter")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("BT_OPENROUTER_MODEL", "openrouter/model")
	t.Setenv("OPENROUTER_HOST", "https://example.test/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.LLMProvider != "openrouter" || cfg.OpenRouterKey != "sk-or-test" || cfg.OpenRouterModel != "openrouter/model" || cfg.OpenRouterHost != "https://example.test/v1" {
		t.Fatalf("bad openrouter config: %#v", cfg)
	}
}

func TestValidate_OpenRouterRequiresKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LLMProvider = "openrouter"
	cfg.OpenRouterKey = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing OpenRouterKey validation error")
	}
	cfg.OpenRouterKey = "sk-or-test"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("openrouter with key should validate: %v", err)
	}
}
