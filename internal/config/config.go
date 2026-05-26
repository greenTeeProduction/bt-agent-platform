// Package config provides environment-based configuration for the Go BT framework.
// All values have defaults, are overrideable via environment variables, and are
// validated on load. Supports feature flags for gradual rollout.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the BT platform.
type Config struct {
	// Server
	DashboardPort int    `env:"BT_DASHBOARD_PORT" default:"9800"`
	APIKey        string `env:"BT_API_KEY" default:""`

	// LLM
	OllamaHost  string `env:"OLLAMA_HOST" default:"http://localhost:11434"`
	OllamaModel string `env:"BT_OLLAMA_MODEL" default:"qwen3.6:35b-a3b"`
	LLMTimeout  int    `env:"BT_LLM_TIMEOUT" default:"300"` // seconds

	// Rate Limiting
	RateLimitRPS   float64 `env:"BT_RATE_LIMIT_RPS" default:"100"`
	RateLimitBurst int     `env:"BT_RATE_LIMIT_BURST" default:"20"`

	// Feature Flags
	GardenerEnabled   bool `env:"BT_FEATURE_GARDENER" default:"true"`
	SchedulerEnabled  bool `env:"BT_FEATURE_SCHEDULER" default:"true"`
	AutoEvolveEnabled bool `env:"BT_FEATURE_AUTO_EVOLVE" default:"false"`
	KanbanEnabled     bool `env:"BT_FEATURE_KANBAN" default:"true"`
	ThinktankEnabled  bool `env:"BT_FEATURE_THINKTANK" default:"true"`
	StartupSimEnabled  bool `env:"BT_FEATURE_STARTUP_SIM" default:"true"`

	// Persistence
	ReflectionsDir string `env:"BT_REFLECTIONS_DIR" default:""`   // defaults to ~/.go-bt-reflections
	AgentDefsDir   string `env:"BT_AGENT_DEFS_DIR" default:""`    // defaults to ~/.go-bt-evolve/agents
	HistoryDir     string `env:"BT_HISTORY_DIR" default:""`       // defaults to ~/.go-bt-evolve/history
	LogDir         string `env:"BT_LOG_DIR" default:""`           // defaults to ~/.go-bt-evolve/logs

	// Gardener
	GardenerCycleInterval int `env:"BT_GARDENER_CYCLE" default:"300"` // seconds (5 min)
	GardenerMutationsPer  int `env:"BT_GARDENER_MUTATIONS" default:"2"`
	GardenerMaxNodes      int `env:"BT_GARDENER_MAX_NODES" default:"20"` // multiplier on original

	// Scheduler
	SchedulerCheckInterval int `env:"BT_SCHEDULER_INTERVAL" default:"60"` // seconds

	// Validation
	MaxBodySize int64 `env:"BT_MAX_BODY_SIZE" default:"1048576"` // 1 MB
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation: %s: %s", e.Field, e.Message)
}

// ValidationErrors is a list of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// Load reads configuration from environment variables with defaults.
func Load() (*Config, error) {
	c := &Config{}

	// Server
	c.DashboardPort = envInt("BT_DASHBOARD_PORT", 9800)
	c.APIKey = os.Getenv("BT_API_KEY")

	// LLM
	c.OllamaHost = envStr("OLLAMA_HOST", "http://localhost:11434")
	c.OllamaModel = envStr("BT_OLLAMA_MODEL", "qwen3.6:35b-a3b")
	c.LLMTimeout = envInt("BT_LLM_TIMEOUT", 300)

	// Rate Limiting
	c.RateLimitRPS = envFloat("BT_RATE_LIMIT_RPS", 100)
	c.RateLimitBurst = envInt("BT_RATE_LIMIT_BURST", 20)

	// Feature Flags
	c.GardenerEnabled = envBool("BT_FEATURE_GARDENER", true)
	c.SchedulerEnabled = envBool("BT_FEATURE_SCHEDULER", true)
	c.AutoEvolveEnabled = envBool("BT_FEATURE_AUTO_EVOLVE", false)
	c.KanbanEnabled = envBool("BT_FEATURE_KANBAN", true)
	c.ThinktankEnabled = envBool("BT_FEATURE_THINKTANK", true)
	c.StartupSimEnabled = envBool("BT_FEATURE_STARTUP_SIM", true)

	// Persistence
	c.ReflectionsDir = os.Getenv("BT_REFLECTIONS_DIR")
	c.AgentDefsDir = os.Getenv("BT_AGENT_DEFS_DIR")
	c.HistoryDir = os.Getenv("BT_HISTORY_DIR")
	c.LogDir = os.Getenv("BT_LOG_DIR")

	// Gardener
	c.GardenerCycleInterval = envInt("BT_GARDENER_CYCLE", 300)
	c.GardenerMutationsPer = envInt("BT_GARDENER_MUTATIONS", 2)
	c.GardenerMaxNodes = envInt("BT_GARDENER_MAX_NODES", 20)

	// Scheduler
	c.SchedulerCheckInterval = envInt("BT_SCHEDULER_INTERVAL", 60)

	// Validation
	c.MaxBodySize = int64(envInt("BT_MAX_BODY_SIZE", 1048576))

	return c, nil
}

// Validate checks all configuration values and returns validation errors.
func (c *Config) Validate() error {
	var errs ValidationErrors

	if c.DashboardPort < 1 || c.DashboardPort > 65535 {
		errs = append(errs, ValidationError{"DashboardPort", "must be between 1 and 65535"})
	}
	if c.LLMTimeout < 1 || c.LLMTimeout > 3600 {
		errs = append(errs, ValidationError{"LLMTimeout", "must be between 1 and 3600 seconds"})
	}
	if c.RateLimitRPS < 0 {
		errs = append(errs, ValidationError{"RateLimitRPS", "must be >= 0"})
	}
	if c.RateLimitBurst < 0 {
		errs = append(errs, ValidationError{"RateLimitBurst", "must be >= 0"})
	}
	if c.GardenerCycleInterval < 10 {
		errs = append(errs, ValidationError{"GardenerCycleInterval", "must be >= 10 seconds"})
	}
	if c.GardenerMutationsPer < 0 || c.GardenerMutationsPer > 10 {
		errs = append(errs, ValidationError{"GardenerMutationsPer", "must be between 0 and 10"})
	}
	if c.GardenerMaxNodes < 1 || c.GardenerMaxNodes > 100 {
		errs = append(errs, ValidationError{"GardenerMaxNodes", "must be between 1 and 100"})
	}
	if c.SchedulerCheckInterval < 10 {
		errs = append(errs, ValidationError{"SchedulerCheckInterval", "must be >= 10 seconds"})
	}
	if c.MaxBodySize < 1024 || c.MaxBodySize > 100*1024*1024 {
		errs = append(errs, ValidationError{"MaxBodySize", "must be between 1024 and 104857600 (100MB)"})
	}
	if c.OllamaModel == "" {
		errs = append(errs, ValidationError{"OllamaModel", "must not be empty"})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// FeatureFlags returns a map of all feature flags for dashboard display.
func (c *Config) FeatureFlags() map[string]bool {
	return map[string]bool{
		"gardener":    c.GardenerEnabled,
		"scheduler":   c.SchedulerEnabled,
		"auto_evolve": c.AutoEvolveEnabled,
		"kanban":      c.KanbanEnabled,
		"thinktank":   c.ThinktankEnabled,
		"startup_sim":  c.StartupSimEnabled,
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func envStr(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func envFloat(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

func envBool(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return defaultVal
}

// Duration is a helper to get a time.Duration from seconds config.
func Duration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}
