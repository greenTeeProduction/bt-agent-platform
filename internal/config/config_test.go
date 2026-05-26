package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Unset any env vars that might interfere
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_LLM_TIMEOUT")
	os.Unsetenv("BT_FEATURE_AUTO_EVOLVE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if c.DashboardPort != 9800 {
		t.Errorf("expected DashboardPort=9800, got %d", c.DashboardPort)
	}
	if c.LLMTimeout != 300 {
		t.Errorf("expected LLMTimeout=300, got %d", c.LLMTimeout)
	}
	if c.AutoEvolveEnabled {
		t.Error("expected AutoEvolveEnabled=false by default")
	}
	if c.OllamaModel != "qwen3.6:35b-a3b" {
		t.Errorf("expected OllamaModel=qwen3.6:35b-a3b, got %s", c.OllamaModel)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Setenv("BT_DASHBOARD_PORT", "8080")
	os.Setenv("BT_OLLAMA_MODEL", "custom-model")
	os.Setenv("BT_LLM_TIMEOUT", "120")
	os.Setenv("BT_FEATURE_GARDENER", "false")
	defer func() {
		os.Unsetenv("BT_DASHBOARD_PORT")
		os.Unsetenv("BT_OLLAMA_MODEL")
		os.Unsetenv("BT_LLM_TIMEOUT")
		os.Unsetenv("BT_FEATURE_GARDENER")
	}()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if c.DashboardPort != 8080 {
		t.Errorf("expected DashboardPort=8080, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "custom-model" {
		t.Errorf("expected OllamaModel=custom-model, got %s", c.OllamaModel)
	}
	if c.LLMTimeout != 120 {
		t.Errorf("expected LLMTimeout=120, got %d", c.LLMTimeout)
	}
	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false")
	}
}

func TestValidate_Valid(t *testing.T) {
	c, _ := Load()
	if err := c.Validate(); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}
}

func TestValidate_Invalid(t *testing.T) {
	c, _ := Load()
	c.DashboardPort = 0  // invalid
	c.LLMTimeout = 0     // invalid
	c.GardenerMaxNodes = 0 // invalid

	err := c.Validate()
	if err == nil {
		t.Error("expected validation errors")
	}
}

func TestFeatureFlags(t *testing.T) {
	c, _ := Load()
	flags := c.FeatureFlags()

	expected := []string{"gardener", "scheduler", "auto_evolve", "kanban", "thinktank", "startup_sim"}
	for _, key := range expected {
		if _, ok := flags[key]; !ok {
			t.Errorf("missing feature flag: %s", key)
		}
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		val      string
		expected bool
	}{
		{"1", true}, {"true", true}, {"yes", true}, {"on", true},
		{"0", false}, {"false", false}, {"no", false}, {"off", false},
		{"TRUE", true}, {"FALSE", false},
	}

	for _, tt := range tests {
		os.Setenv("_TEST_BOOL", tt.val)
		got := envBool("_TEST_BOOL", !tt.expected) // use opposite default
		os.Unsetenv("_TEST_BOOL")
		if got != tt.expected {
			t.Errorf("envBool(%q) = %v, want %v", tt.val, got, tt.expected)
		}
	}
}
