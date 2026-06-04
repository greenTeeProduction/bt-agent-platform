// Package config provides environment-based configuration for the Go BT framework.
// All values have defaults, are overrideable via environment variables, and are
// validated on load. Supports feature flags for gradual rollout.
//
// Config file support: set BT_CONFIG_FILE to a JSON file path. File values
// are loaded first, then environment variables override them. This enables
// team-shared base configs with per-deployment env overrides.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the BT platform.
type Config struct {
	// Server
	DashboardPort int    `json:"dashboard_port" env:"BT_DASHBOARD_PORT" default:"9800"`
	APIKey        string `json:"api_key,omitempty" env:"BT_API_KEY" default:""`
	TLSCert       string `json:"tls_cert,omitempty" env:"BT_TLS_CERT" default:""`
	TLSKey        string `json:"tls_key,omitempty" env:"BT_TLS_KEY" default:""`

	// LLM
	LLMProvider    string `json:"llm_provider" env:"BT_LLM_PROVIDER" default:"ollama"` // ollama, deepseek, acp
	OllamaHost     string `json:"ollama_host" env:"OLLAMA_HOST" default:"http://localhost:11434"`
	OllamaModel    string `json:"ollama_model" env:"BT_OLLAMA_MODEL" default:"qwen3.6:35b-a3b"`
	DeepSeekHost   string `json:"deepseek_host" env:"BT_DEEPSEEK_HOST" default:"https://api.deepseek.com/v1"`
	DeepSeekModel  string `json:"deepseek_model" env:"BT_DEEPSEEK_MODEL" default:"deepseek-v4-flash"`
	DeepSeekKey    string `json:"deepseek_key,omitempty" env:"BT_DEEPSEEK_KEY" default:""`
	ACPCommand     string `json:"acp_command" env:"BT_ACP_COMMAND" default:"hermes"`
	ACPArgs        string `json:"acp_args" env:"BT_ACP_ARGS" default:"acp --accept-hooks"`
	ACPCwd         string `json:"acp_cwd,omitempty" env:"BT_ACP_CWD" default:""`
	FallbackModels string `json:"fallback_models,omitempty" env:"BT_FALLBACK_MODELS" default:""` // CSV: model or provider:model/provider/model
	LLMTimeout     int    `json:"llm_timeout" env:"BT_LLM_TIMEOUT" default:"300"`                // seconds

	// CORS
	CORSDashboardOrigin string `json:"cors_dashboard_origin,omitempty" env:"BT_CORS_DASHBOARD_ORIGIN" default:"*"`

	// Rate Limiting
	RateLimitRPS   float64 `json:"rate_limit_rps" env:"BT_RATE_LIMIT_RPS" default:"100"`
	RateLimitBurst int     `json:"rate_limit_burst" env:"BT_RATE_LIMIT_BURST" default:"20"`

	// Feature Flags
	GardenerEnabled              bool `json:"gardener_enabled" env:"BT_FEATURE_GARDENER" default:"true"`
	SchedulerEnabled             bool `json:"scheduler_enabled" env:"BT_FEATURE_SCHEDULER" default:"true"`
	AutoEvolveEnabled            bool `json:"auto_evolve_enabled" env:"BT_FEATURE_AUTO_EVOLVE" default:"false"`
	KanbanEnabled                bool `json:"kanban_enabled" env:"BT_FEATURE_KANBAN" default:"true"`
	ThinktankEnabled             bool `json:"thinktank_enabled" env:"BT_FEATURE_THINKTANK" default:"true"`
	StartupSimEnabled            bool `json:"startup_sim_enabled" env:"BT_FEATURE_STARTUP_SIM" default:"true"`
	APIEnforceResponseValidation bool `json:"api_enforce_response_validation" env:"BT_API_ENFORCE_RESPONSE_VALIDATION" default:"false"`

	// Persistence
	ReflectionsDir string `json:"reflections_dir,omitempty" env:"BT_REFLECTIONS_DIR" default:""`
	AgentDefsDir   string `json:"agent_defs_dir,omitempty" env:"BT_AGENT_DEFS_DIR" default:""`
	HistoryDir     string `json:"history_dir,omitempty" env:"BT_HISTORY_DIR" default:""`
	LogDir         string `json:"log_dir,omitempty" env:"BT_LOG_DIR" default:""`

	// Gardener
	GardenerCycleInterval int `json:"gardener_cycle_interval" env:"BT_GARDENER_CYCLE" default:"300"` // seconds
	GardenerMutationsPer  int `json:"gardener_mutations_per" env:"BT_GARDENER_MUTATIONS" default:"2"`
	GardenerMaxNodes      int `json:"gardener_max_nodes" env:"BT_GARDENER_MAX_NODES" default:"20"` // multiplier

	// Scheduler
	SchedulerCheckInterval int `json:"scheduler_check_interval" env:"BT_SCHEDULER_INTERVAL" default:"60"` // seconds

	// Error Handling
	RetryMaxRetries  int    `json:"retry_max_retries" env:"BT_RETRY_MAX_RETRIES" default:"3"`
	RetryBaseDelayMs int    `json:"retry_base_delay_ms" env:"BT_RETRY_BASE_DELAY_MS" default:"1000"`
	RetryMaxDelayMs  int    `json:"retry_max_delay_ms" env:"BT_RETRY_MAX_DELAY_MS" default:"30000"`
	RetryLLMBaseMs   int    `json:"retry_llm_base_ms" env:"BT_RETRY_LLM_BASE_MS" default:"2000"`
	RetryJitter      string `json:"retry_jitter" env:"BT_RETRY_JITTER" default:"full_jitter"` // no_jitter, full_jitter, equal_jitter, decorrelated_jitter
	RetryUnknown     bool   `json:"retry_unknown" env:"BT_RETRY_UNKNOWN" default:"false"`
	CBThreshold      int    `json:"cb_threshold" env:"BT_CB_THRESHOLD" default:"3"`
	CBCooldownSecs   int    `json:"cb_cooldown_secs" env:"BT_CB_COOLDOWN_SECS" default:"300"` // 5 minutes
	DLQMaxEntries    int    `json:"dlq_max_entries" env:"BT_DLQ_MAX_ENTRIES" default:"1000"`

	// Validation
	MaxBodySize int64 `json:"max_body_size" env:"BT_MAX_BODY_SIZE" default:"1048576"` // 1 MB

	// Metadata
	ConfigFile string `json:"-" env:"BT_CONFIG_FILE" default:""` // path to JSON config file

	// Paths — resolved file paths (populated by ResolvePaths())
	Paths PathConfig `json:"paths,omitempty"`
}

// PathConfig provides resolved file paths for all BT platform components.
// Use cfg.ResolvePaths() to populate from env vars or defaults.
type PathConfig struct {
	HomeDir        string `json:"home_dir"`     // ~/.bt-agent/ or BT_HOME
	ConfigFile     string `json:"config_file"`  // config.yaml
	DBFile         string `json:"db_file"`      // agents.db
	DLQFile        string `json:"dlq_file"`     // dead_letter_queue.json
	TemplateDir    string `json:"template_dir"` // agents/templates/
	ReflectionsDir string `json:"reflections_dir"`
	HistoryDir     string `json:"history_dir"`
	LogDir         string `json:"log_dir"`
}

// ResolvePaths populates cfg.Paths from env vars (BT_HOME, BT_CONFIG_FILE, etc.)
// with sensible defaults. Call after Load().
func (c *Config) ResolvePaths() {
	home := os.Getenv("BT_HOME")
	if home == "" {
		home = filepath.Join(os.Getenv("HOME"), ".bt-agent")
	}
	c.Paths.HomeDir = home

	c.Paths.ConfigFile = c.ConfigFile
	if c.Paths.ConfigFile == "" {
		c.Paths.ConfigFile = filepath.Join(home, "config.yaml")
	}
	c.Paths.DBFile = filepath.Join(home, "agents.db")
	c.Paths.DLQFile = filepath.Join(home, "dead_letter_queue.json")
	c.Paths.TemplateDir = filepath.Join(home, "templates")
	c.Paths.ReflectionsDir = c.ReflectionsDir
	if c.Paths.ReflectionsDir == "" {
		c.Paths.ReflectionsDir = filepath.Join(home, "reflections")
	}
	c.Paths.HistoryDir = c.HistoryDir
	if c.Paths.HistoryDir == "" {
		c.Paths.HistoryDir = filepath.Join(home, "history")
	}
	c.Paths.LogDir = c.LogDir
	if c.Paths.LogDir == "" {
		c.Paths.LogDir = filepath.Join(home, "logs")
	}
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

// Load reads configuration from multiple sources with a defined priority:
//  1. Hardcoded defaults (newDefaultConfig)
//  2. JSON config file (BT_CONFIG_FILE env var or explicit path)
//  3. .env files (BT_DOTENV_FILE env var, then ./.env if it exists)
//  4. Environment variable overrides (highest priority)
//
// .env files are KEY=value format files commonly used for local development
// and CI/CD. They're applied before environment variables, so env vars
// always take precedence. This enables deploying the same binary with a
// .env file in development and environment variables in production.
func Load() (*Config, error) {
	c := newDefaultConfig()

	// 1. Load from config file if BT_CONFIG_FILE is set
	configFile := os.Getenv("BT_CONFIG_FILE")
	if configFile != "" {
		c.ConfigFile = configFile
		if err := loadFile(configFile, c); err != nil {
			return nil, fmt.Errorf("config file %s: %w", configFile, err)
		}
	}

	// 2. Load .env files (before env vars so env vars override)
	applyDotEnvFiles(c)

	// 3. Apply environment variable overrides (highest priority)
	applyEnvOverrides(c)

	// 4. Validate
	if err := c.Validate(); err != nil {
		return c, err
	}

	return c, nil
}

// LoadFile loads configuration from a JSON file, then applies any
// environment variable overrides on top. This is useful for explicit
// config file loading without relying on the BT_CONFIG_FILE env var.
// Returns validation errors if the loaded config is invalid.
func LoadFile(path string) (*Config, error) {
	c := newDefaultConfig()
	c.ConfigFile = path
	if err := loadFile(path, c); err != nil {
		return nil, err
	}
	applyEnvOverrides(c)
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// LoadFileWithDotEnv loads configuration from a JSON config file, then
// applies values from a .env file BEFORE environment variable overrides.
// This is the hot-reload equivalent of Load() for ConfigWatcher — it
// respects the full priority chain: defaults → config.json → .env → env vars.
//
// If the .env file doesn't exist or can't be parsed, a warning is logged
// but loading continues (the .env file is optional). Environment variables
// always take precedence over .env values.
func LoadFileWithDotEnv(configPath, dotenvPath string) (*Config, error) {
	c := newDefaultConfig()
	c.ConfigFile = configPath
	if err := loadFile(configPath, c); err != nil {
		return nil, err
	}

	// Apply .env file values (before env vars so env vars override)
	if kv, err := LoadDotEnv(dotenvPath); err == nil {
		applyDotEnvToConfig(c, kv)
	} else if !os.IsNotExist(err) {
		log.Printf("[config] warning: reload .env %s: %v", dotenvPath, err)
	}

	applyEnvOverrides(c)
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

// newDefaultConfig returns a Config with all default values set.
// newDefaultConfig returns a Config populated with all production defaults.
// Deprecated: prefer DefaultConfig() for public callers.
// This is the internal function used by Load() and LoadFile().
func newDefaultConfig() *Config {
	return &Config{
		DashboardPort:                defaultDashboardPort,
		LLMProvider:                  defaultLLMProvider,
		OllamaHost:                   defaultOllamaHost,
		OllamaModel:                  defaultOllamaModel,
		DeepSeekHost:                 defaultDeepSeekHost,
		DeepSeekModel:                defaultDeepSeekModel,
		ACPCommand:                   defaultACPCommand,
		ACPArgs:                      defaultACPArgs,
		LLMTimeout:                   defaultLLMTimeout,
		RateLimitRPS:                 defaultRateLimitRPS,
		RateLimitBurst:               defaultRateLimitBurst,
		GardenerEnabled:              defaultGardenerEnabled,
		SchedulerEnabled:             defaultSchedulerEnabled,
		KanbanEnabled:                defaultKanbanEnabled,
		ThinktankEnabled:             defaultThinktankEnabled,
		StartupSimEnabled:            defaultStartupSimEnabled,
		APIEnforceResponseValidation: defaultAPIEnforceResponseValidation,
		GardenerCycleInterval:        defaultGardenerCycleInterval,
		GardenerMutationsPer:         defaultGardenerMutationsPer,
		GardenerMaxNodes:             defaultGardenerMaxNodes,
		SchedulerCheckInterval:       defaultSchedulerCheckInterval,
		MaxBodySize:                  defaultMaxBodySize,
		RetryMaxRetries:              defaultRetryMaxRetries,
		RetryBaseDelayMs:             defaultRetryBaseDelayMs,
		RetryMaxDelayMs:              defaultRetryMaxDelayMs,
		RetryLLMBaseMs:               defaultRetryLLMBaseMs,
		RetryJitter:                  defaultRetryJitter,
		RetryUnknown:                 defaultRetryUnknown,
		CBThreshold:                  defaultCBThreshold,
		CBCooldownSecs:               defaultCBCooldownSecs,
		DLQMaxEntries:                defaultDLQMaxEntries,
	}
}

// loadFile reads a JSON config file and merges non-zero values into c.
// Zero values in the file are skipped — defaults take precedence.
// Only file-specified fields override defaults; env vars are applied later.
func loadFile(path string, c *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	// Unmarshal into a temporary Config with zero values so we can
	// detect which fields were actually set in the file.
	var fileCfg Config
	if err := json.Unmarshal(data, &fileCfg); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}

	// Merge: file values override defaults, but only non-zero values.
	// Zero values in the file mean "use default".
	mergeFileConfig(c, &fileCfg)
	return nil
}

// mergeFileConfig merges file-provided values into the config.
// Only non-zero values from the file are applied; zero values are
// treated as "not specified" and keep the default.
func mergeFileConfig(c *Config, file *Config) {
	if file.DashboardPort != 0 {
		c.DashboardPort = file.DashboardPort
	}
	if file.APIKey != "" {
		c.APIKey = file.APIKey
	}
	if file.TLSCert != "" {
		c.TLSCert = file.TLSCert
	}
	if file.TLSKey != "" {
		c.TLSKey = file.TLSKey
	}
	if file.OllamaHost != "" {
		c.OllamaHost = file.OllamaHost
	}
	if file.OllamaModel != "" {
		c.OllamaModel = file.OllamaModel
	}
	if file.LLMProvider != "" {
		c.LLMProvider = file.LLMProvider
	}
	if file.DeepSeekHost != "" {
		c.DeepSeekHost = file.DeepSeekHost
	}
	if file.DeepSeekModel != "" {
		c.DeepSeekModel = file.DeepSeekModel
	}
	if file.DeepSeekKey != "" {
		c.DeepSeekKey = file.DeepSeekKey
	}
	if file.ACPCommand != "" {
		c.ACPCommand = file.ACPCommand
	}
	if file.ACPArgs != "" {
		c.ACPArgs = file.ACPArgs
	}
	if file.ACPCwd != "" {
		c.ACPCwd = file.ACPCwd
	}
	if file.FallbackModels != "" {
		c.FallbackModels = file.FallbackModels
	}
	if file.LLMTimeout != 0 {
		c.LLMTimeout = file.LLMTimeout
	}
	if file.RateLimitRPS != 0 {
		c.RateLimitRPS = file.RateLimitRPS
	}
	if file.RateLimitBurst != 0 {
		c.RateLimitBurst = file.RateLimitBurst
	}
	// Feature flags: use explicit boolean check since false is valid
	if file.GardenerEnabled || hasExplicitField(file, "gardener_enabled") {
		c.GardenerEnabled = file.GardenerEnabled
	}
	if file.SchedulerEnabled || hasExplicitField(file, "scheduler_enabled") {
		c.SchedulerEnabled = file.SchedulerEnabled
	}
	if file.AutoEvolveEnabled || hasExplicitField(file, "auto_evolve_enabled") {
		c.AutoEvolveEnabled = file.AutoEvolveEnabled
	}
	if file.KanbanEnabled || hasExplicitField(file, "kanban_enabled") {
		c.KanbanEnabled = file.KanbanEnabled
	}
	if file.ThinktankEnabled || hasExplicitField(file, "thinktank_enabled") {
		c.ThinktankEnabled = file.ThinktankEnabled
	}
	if file.StartupSimEnabled || hasExplicitField(file, "startup_sim_enabled") {
		c.StartupSimEnabled = file.StartupSimEnabled
	}
	if file.APIEnforceResponseValidation || hasExplicitField(file, "api_enforce_response_validation") {
		c.APIEnforceResponseValidation = file.APIEnforceResponseValidation
	}
	if file.ReflectionsDir != "" {
		c.ReflectionsDir = file.ReflectionsDir
	}
	if file.AgentDefsDir != "" {
		c.AgentDefsDir = file.AgentDefsDir
	}
	if file.HistoryDir != "" {
		c.HistoryDir = file.HistoryDir
	}
	if file.LogDir != "" {
		c.LogDir = file.LogDir
	}
	if file.GardenerCycleInterval != 0 {
		c.GardenerCycleInterval = file.GardenerCycleInterval
	}
	if file.GardenerMutationsPer != 0 {
		c.GardenerMutationsPer = file.GardenerMutationsPer
	}
	if file.GardenerMaxNodes != 0 {
		c.GardenerMaxNodes = file.GardenerMaxNodes
	}
	if file.SchedulerCheckInterval != 0 {
		c.SchedulerCheckInterval = file.SchedulerCheckInterval
	}
	if file.MaxBodySize != 0 {
		c.MaxBodySize = file.MaxBodySize
	}
}

// hasExplicitField checks whether a JSON file explicitly set a boolean field.
// Since Go's json decoder treats missing bools as false, we re-parse into
// a raw map to check field presence for booleans.
func hasExplicitField(cfg *Config, field string) bool {
	// Re-marshal and check — this is only called for booleans
	// where false is a valid explicit setting.
	data, err := json.Marshal(cfg)
	if err != nil {
		return false
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}
	_, ok := raw[field]
	return ok
}

// applyEnvOverrides applies environment variable overrides on top of c.
func applyEnvOverrides(c *Config) {
	// Server
	if v := os.Getenv("BT_DASHBOARD_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.DashboardPort = n
		}
	}
	if v := os.Getenv("BT_API_KEY"); v != "" {
		c.APIKey = v
	}
	if v := os.Getenv("BT_TLS_CERT"); v != "" {
		c.TLSCert = v
	}
	if v := os.Getenv("BT_TLS_KEY"); v != "" {
		c.TLSKey = v
	}

	// LLM
	if v := os.Getenv("BT_LLM_PROVIDER"); v != "" {
		c.LLMProvider = v
	}
	if v := os.Getenv("OLLAMA_HOST"); v != "" {
		c.OllamaHost = v
	}
	if v := os.Getenv("BT_OLLAMA_MODEL"); v != "" {
		c.OllamaModel = v
	}
	if v := os.Getenv("BT_DEEPSEEK_HOST"); v != "" {
		c.DeepSeekHost = v
	}
	if v := os.Getenv("BT_DEEPSEEK_MODEL"); v != "" {
		c.DeepSeekModel = v
	}
	if v := os.Getenv("BT_DEEPSEEK_KEY"); v != "" {
		c.DeepSeekKey = v
	} else if v := os.Getenv("DEEPSEEK_API_KEY"); v != "" {
		// Fallback: read from Hermes's env
		c.DeepSeekKey = v
	}
	if v := os.Getenv("BT_ACP_COMMAND"); v != "" {
		c.ACPCommand = v
	}
	if v := os.Getenv("BT_ACP_ARGS"); v != "" {
		c.ACPArgs = v
	}
	if v := os.Getenv("BT_ACP_CWD"); v != "" {
		c.ACPCwd = v
	}
	if v := os.Getenv("BT_FALLBACK_MODELS"); v != "" {
		c.FallbackModels = v
	}
	if v := os.Getenv("BT_LLM_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.LLMTimeout = n
		}
	}

	// Rate Limiting
	if v := os.Getenv("BT_RATE_LIMIT_RPS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.RateLimitRPS = f
		}
	}
	if v := os.Getenv("BT_RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RateLimitBurst = n
		}
	}

	// Feature Flags
	if v := os.Getenv("BT_FEATURE_GARDENER"); v != "" {
		c.GardenerEnabled = parseBool(v)
	}
	if v := os.Getenv("BT_FEATURE_SCHEDULER"); v != "" {
		c.SchedulerEnabled = parseBool(v)
	}
	if v := os.Getenv("BT_FEATURE_AUTO_EVOLVE"); v != "" {
		c.AutoEvolveEnabled = parseBool(v)
	}
	if v := os.Getenv("BT_FEATURE_KANBAN"); v != "" {
		c.KanbanEnabled = parseBool(v)
	}
	if v := os.Getenv("BT_FEATURE_THINKTANK"); v != "" {
		c.ThinktankEnabled = parseBool(v)
	}
	if v := os.Getenv("BT_FEATURE_STARTUP_SIM"); v != "" {
		c.StartupSimEnabled = parseBool(v)
	}
	if v := os.Getenv("BT_API_ENFORCE_RESPONSE_VALIDATION"); v != "" {
		c.APIEnforceResponseValidation = parseBool(v)
	}

	// Persistence
	if v := os.Getenv("BT_REFLECTIONS_DIR"); v != "" {
		c.ReflectionsDir = v
	}
	if v := os.Getenv("BT_AGENT_DEFS_DIR"); v != "" {
		c.AgentDefsDir = v
	}
	if v := os.Getenv("BT_HISTORY_DIR"); v != "" {
		c.HistoryDir = v
	}
	if v := os.Getenv("BT_LOG_DIR"); v != "" {
		c.LogDir = v
	}

	// Gardener
	if v := os.Getenv("BT_GARDENER_CYCLE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.GardenerCycleInterval = n
		}
	}
	if v := os.Getenv("BT_GARDENER_MUTATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.GardenerMutationsPer = n
		}
	}
	if v := os.Getenv("BT_GARDENER_MAX_NODES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.GardenerMaxNodes = n
		}
	}

	// Scheduler
	if v := os.Getenv("BT_SCHEDULER_INTERVAL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.SchedulerCheckInterval = n
		}
	}

	// Error Handling
	if v := os.Getenv("BT_RETRY_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryMaxRetries = n
		}
	}
	if v := os.Getenv("BT_RETRY_BASE_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryBaseDelayMs = n
		}
	}
	if v := os.Getenv("BT_RETRY_MAX_DELAY_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryMaxDelayMs = n
		}
	}
	if v := os.Getenv("BT_RETRY_LLM_BASE_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.RetryLLMBaseMs = n
		}
	}
	if v := os.Getenv("BT_RETRY_JITTER"); v != "" {
		c.RetryJitter = v
	}
	if v := os.Getenv("BT_RETRY_UNKNOWN"); v != "" {
		c.RetryUnknown = parseBool(v)
	}
	if v := os.Getenv("BT_CB_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CBThreshold = n
		}
	}
	if v := os.Getenv("BT_CB_COOLDOWN_SECS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.CBCooldownSecs = n
		}
	}
	if v := os.Getenv("BT_DLQ_MAX_ENTRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.DLQMaxEntries = n
		}
	}

	// Validation
	if v := os.Getenv("BT_MAX_BODY_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxBodySize = int64(n)
		}
	}
}

// ─── .env File Support ──────────────────────────────────────────────────────

// applyDotEnvFiles loads .env files and applies their values to the config.
// Priority: BT_DOTENV_FILE env var (if set), then ./.env (if it exists).
// Values from .env files are only applied when the corresponding environment
// variable is NOT set — environment variables always take precedence.
func applyDotEnvFiles(c *Config) {
	dotenvFiles := []string{}

	// 1. Check BT_DOTENV_FILE env var (explicit path)
	if dotenvFile := os.Getenv("BT_DOTENV_FILE"); dotenvFile != "" {
		dotenvFiles = append(dotenvFiles, dotenvFile)
	}

	// 2. Check for .env in current directory
	if _, err := os.Stat(".env"); err == nil {
		dotenvFiles = append(dotenvFiles, ".env")
	}

	for _, path := range dotenvFiles {
		kv, err := LoadDotEnv(path)
		if err != nil {
			// Log but don't fail — .env files are optional
			log.Printf("[config] warning: loading .env %s: %v", path, err)
			continue
		}
		applyDotEnvToConfig(c, kv)
	}
}

// LoadDotEnv reads a .env file and returns a map of KEY=value pairs.
// Supports:
//   - Simple assignments: KEY=value
//   - Quoted values: KEY="value with spaces"
//   - Single-quoted values: KEY='value'
//   - Comments: lines starting with # are ignored
//   - Blank lines are skipped
//   - Inline comments after values are stripped (outside quotes)
//   - Export prefix: export KEY=value is normalized to KEY=value
//   - Multiline values are NOT supported
func LoadDotEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	result := make(map[string]string)
	lines := strings.Split(string(data), "\n")

	for lineNum, raw := range lines {
		line := strings.TrimSpace(raw)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip export prefix
		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimSpace(line)

		// Must have an equals sign
		eqIdx := strings.IndexByte(line, '=')
		if eqIdx < 1 {
			log.Printf("[config] warning: %s:%d: invalid line, skipping: %q", path, lineNum+1, raw)
			continue
		}

		key := strings.TrimSpace(line[:eqIdx])
		val := strings.TrimSpace(line[eqIdx+1:])

		// Strip quotes
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		// Strip inline comments (outside quotes)
		val = stripInlineComment(val)

		if key != "" {
			result[key] = val
		}
	}

	return result, nil
}

// stripInlineComment removes everything after an unquoted #.
func stripInlineComment(val string) string {
	inSingle := false
	inDouble := false
	for i, c := range val {
		if c == '\'' && !inDouble {
			inSingle = !inSingle
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
		}
		if c == '#' && !inSingle && !inDouble {
			return strings.TrimRight(val[:i], " \t")
		}
	}
	return val
}

// applyDotEnvToConfig applies .env key-value pairs to the config.
// Only sets a field if the corresponding environment variable is NOT
// already set — env vars have higher priority.
func applyDotEnvToConfig(c *Config, kv map[string]string) {
	applyDotEnvStr := func(envKey, dotenvKey string, setter func(string)) {
		if os.Getenv(envKey) != "" {
			return // env var already set; don't override
		}
		if v, ok := kv[dotenvKey]; ok {
			setter(v)
		}
	}
	applyDotEnvInt := func(envKey, dotenvKey string, setter func(int)) {
		if os.Getenv(envKey) != "" {
			return
		}
		if v, ok := kv[dotenvKey]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				setter(n)
			}
		}
	}
	applyDotEnvFloat := func(envKey, dotenvKey string, setter func(float64)) {
		if os.Getenv(envKey) != "" {
			return
		}
		if v, ok := kv[dotenvKey]; ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				setter(f)
			}
		}
	}
	applyDotEnvBool := func(envKey, dotenvKey string, setter func(bool)) {
		if os.Getenv(envKey) != "" {
			return
		}
		if v, ok := kv[dotenvKey]; ok {
			setter(parseBool(v))
		}
	}

	// Server
	applyDotEnvInt("BT_DASHBOARD_PORT", "BT_DASHBOARD_PORT", func(v int) { c.DashboardPort = v })
	applyDotEnvStr("BT_API_KEY", "BT_API_KEY", func(v string) { c.APIKey = v })
	applyDotEnvStr("BT_TLS_CERT", "BT_TLS_CERT", func(v string) { c.TLSCert = v })
	applyDotEnvStr("BT_TLS_KEY", "BT_TLS_KEY", func(v string) { c.TLSKey = v })

	// LLM
	applyDotEnvStr("BT_LLM_PROVIDER", "BT_LLM_PROVIDER", func(v string) { c.LLMProvider = v })
	applyDotEnvStr("OLLAMA_HOST", "OLLAMA_HOST", func(v string) { c.OllamaHost = v })
	applyDotEnvStr("BT_OLLAMA_MODEL", "BT_OLLAMA_MODEL", func(v string) { c.OllamaModel = v })
	applyDotEnvStr("BT_DEEPSEEK_HOST", "BT_DEEPSEEK_HOST", func(v string) { c.DeepSeekHost = v })
	applyDotEnvStr("BT_DEEPSEEK_MODEL", "BT_DEEPSEEK_MODEL", func(v string) { c.DeepSeekModel = v })
	applyDotEnvStr("BT_DEEPSEEK_KEY", "BT_DEEPSEEK_KEY", func(v string) { c.DeepSeekKey = v })
	// Also check the standard DEEPSEEK_API_KEY for .env files (Hermes convention)
	applyDotEnvStr("DEEPSEEK_API_KEY", "DEEPSEEK_API_KEY", func(v string) { c.DeepSeekKey = v })
	applyDotEnvStr("BT_ACP_COMMAND", "BT_ACP_COMMAND", func(v string) { c.ACPCommand = v })
	applyDotEnvStr("BT_ACP_ARGS", "BT_ACP_ARGS", func(v string) { c.ACPArgs = v })
	applyDotEnvStr("BT_ACP_CWD", "BT_ACP_CWD", func(v string) { c.ACPCwd = v })
	applyDotEnvStr("BT_FALLBACK_MODELS", "BT_FALLBACK_MODELS", func(v string) { c.FallbackModels = v })
	applyDotEnvInt("BT_LLM_TIMEOUT", "BT_LLM_TIMEOUT", func(v int) { c.LLMTimeout = v })

	// CORS
	applyDotEnvStr("BT_CORS_DASHBOARD_ORIGIN", "BT_CORS_DASHBOARD_ORIGIN", func(v string) { c.CORSDashboardOrigin = v })

	// Rate Limiting
	applyDotEnvFloat("BT_RATE_LIMIT_RPS", "BT_RATE_LIMIT_RPS", func(v float64) { c.RateLimitRPS = v })
	applyDotEnvInt("BT_RATE_LIMIT_BURST", "BT_RATE_LIMIT_BURST", func(v int) { c.RateLimitBurst = v })

	// Feature Flags
	applyDotEnvBool("BT_FEATURE_GARDENER", "BT_FEATURE_GARDENER", func(v bool) { c.GardenerEnabled = v })
	applyDotEnvBool("BT_FEATURE_SCHEDULER", "BT_FEATURE_SCHEDULER", func(v bool) { c.SchedulerEnabled = v })
	applyDotEnvBool("BT_FEATURE_AUTO_EVOLVE", "BT_FEATURE_AUTO_EVOLVE", func(v bool) { c.AutoEvolveEnabled = v })
	applyDotEnvBool("BT_FEATURE_KANBAN", "BT_FEATURE_KANBAN", func(v bool) { c.KanbanEnabled = v })
	applyDotEnvBool("BT_FEATURE_THINKTANK", "BT_FEATURE_THINKTANK", func(v bool) { c.ThinktankEnabled = v })
	applyDotEnvBool("BT_FEATURE_STARTUP_SIM", "BT_FEATURE_STARTUP_SIM", func(v bool) { c.StartupSimEnabled = v })
	applyDotEnvBool("BT_API_ENFORCE_RESPONSE_VALIDATION", "BT_API_ENFORCE_RESPONSE_VALIDATION", func(v bool) { c.APIEnforceResponseValidation = v })

	// Persistence
	applyDotEnvStr("BT_REFLECTIONS_DIR", "BT_REFLECTIONS_DIR", func(v string) { c.ReflectionsDir = v })
	applyDotEnvStr("BT_AGENT_DEFS_DIR", "BT_AGENT_DEFS_DIR", func(v string) { c.AgentDefsDir = v })
	applyDotEnvStr("BT_HISTORY_DIR", "BT_HISTORY_DIR", func(v string) { c.HistoryDir = v })
	applyDotEnvStr("BT_LOG_DIR", "BT_LOG_DIR", func(v string) { c.LogDir = v })

	// Gardener
	applyDotEnvInt("BT_GARDENER_CYCLE", "BT_GARDENER_CYCLE", func(v int) { c.GardenerCycleInterval = v })
	applyDotEnvInt("BT_GARDENER_MUTATIONS", "BT_GARDENER_MUTATIONS", func(v int) { c.GardenerMutationsPer = v })
	applyDotEnvInt("BT_GARDENER_MAX_NODES", "BT_GARDENER_MAX_NODES", func(v int) { c.GardenerMaxNodes = v })

	// Scheduler
	applyDotEnvInt("BT_SCHEDULER_INTERVAL", "BT_SCHEDULER_INTERVAL", func(v int) { c.SchedulerCheckInterval = v })

	// Error Handling
	applyDotEnvInt("BT_RETRY_MAX_RETRIES", "BT_RETRY_MAX_RETRIES", func(v int) { c.RetryMaxRetries = v })
	applyDotEnvInt("BT_RETRY_BASE_DELAY_MS", "BT_RETRY_BASE_DELAY_MS", func(v int) { c.RetryBaseDelayMs = v })
	applyDotEnvInt("BT_RETRY_MAX_DELAY_MS", "BT_RETRY_MAX_DELAY_MS", func(v int) { c.RetryMaxDelayMs = v })
	applyDotEnvInt("BT_RETRY_LLM_BASE_MS", "BT_RETRY_LLM_BASE_MS", func(v int) { c.RetryLLMBaseMs = v })
	applyDotEnvStr("BT_RETRY_JITTER", "BT_RETRY_JITTER", func(v string) { c.RetryJitter = v })
	applyDotEnvBool("BT_RETRY_UNKNOWN", "BT_RETRY_UNKNOWN", func(v bool) { c.RetryUnknown = v })
	applyDotEnvInt("BT_CB_THRESHOLD", "BT_CB_THRESHOLD", func(v int) { c.CBThreshold = v })
	applyDotEnvInt("BT_CB_COOLDOWN_SECS", "BT_CB_COOLDOWN_SECS", func(v int) { c.CBCooldownSecs = v })
	applyDotEnvInt("BT_DLQ_MAX_ENTRIES", "BT_DLQ_MAX_ENTRIES", func(v int) { c.DLQMaxEntries = v })

	// Validation
	applyDotEnvInt("BT_MAX_BODY_SIZE", "BT_MAX_BODY_SIZE", func(v int) { c.MaxBodySize = int64(v) })
}

// parseBool parses a boolean string value (1/true/yes/on → true).
func parseBool(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
	if c.GardenerCycleInterval < 10 || c.GardenerCycleInterval > 86400 {
		errs = append(errs, ValidationError{"GardenerCycleInterval", "must be between 10 and 86400 seconds"})
	}
	if c.GardenerMutationsPer < 0 || c.GardenerMutationsPer > 10 {
		errs = append(errs, ValidationError{"GardenerMutationsPer", "must be between 0 and 10"})
	}
	if c.GardenerMaxNodes < 1 || c.GardenerMaxNodes > 100 {
		errs = append(errs, ValidationError{"GardenerMaxNodes", "must be between 1 and 100"})
	}
	if c.SchedulerCheckInterval < 10 || c.SchedulerCheckInterval > 3600 {
		errs = append(errs, ValidationError{"SchedulerCheckInterval", "must be between 10 and 3600 seconds"})
	}
	if c.MaxBodySize < 1024 || c.MaxBodySize > 100*1024*1024 {
		errs = append(errs, ValidationError{"MaxBodySize", "must be between 1024 and 104857600 (100MB)"})
	}

	// Error Handling
	if c.RetryMaxRetries < 1 || c.RetryMaxRetries > 10 {
		errs = append(errs, ValidationError{"RetryMaxRetries", "must be between 1 and 10"})
	}
	if c.RetryBaseDelayMs < 100 || c.RetryBaseDelayMs > 60000 {
		errs = append(errs, ValidationError{"RetryBaseDelayMs", "must be between 100 and 60000 ms"})
	}
	if c.RetryMaxDelayMs < 1000 || c.RetryMaxDelayMs > 600000 {
		errs = append(errs, ValidationError{"RetryMaxDelayMs", "must be between 1000 and 600000 ms"})
	}
	if c.RetryLLMBaseMs < 100 || c.RetryLLMBaseMs > 120000 {
		errs = append(errs, ValidationError{"RetryLLMBaseMs", "must be between 100 and 120000 ms"})
	}
	if c.RetryJitter != "no_jitter" && c.RetryJitter != "full_jitter" && c.RetryJitter != "equal_jitter" && c.RetryJitter != "decorrelated_jitter" {
		errs = append(errs, ValidationError{"RetryJitter", "must be one of: no_jitter, full_jitter, equal_jitter, decorrelated_jitter"})
	}
	if c.CBThreshold < 1 || c.CBThreshold > 20 {
		errs = append(errs, ValidationError{"CBThreshold", "must be between 1 and 20 failures"})
	}
	if c.CBCooldownSecs < 10 || c.CBCooldownSecs > 3600 {
		errs = append(errs, ValidationError{"CBCooldownSecs", "must be between 10 and 3600 seconds"})
	}
	if c.DLQMaxEntries < 10 || c.DLQMaxEntries > 100000 {
		errs = append(errs, ValidationError{"DLQMaxEntries", "must be between 10 and 100000"})
	}

	if c.OllamaModel == "" && c.LLMProvider == "ollama" {
		errs = append(errs, ValidationError{"OllamaModel", "must not be empty when LLMProvider is ollama"})
	}
	if c.LLMProvider != "ollama" && c.LLMProvider != "deepseek" && c.LLMProvider != "acp" {
		errs = append(errs, ValidationError{"LLMProvider", "must be 'ollama', 'deepseek', or 'acp'"})
	}
	if c.LLMProvider == "ollama" && c.OllamaHost == "" {
		errs = append(errs, ValidationError{"OllamaHost", "must not be empty when LLMProvider is ollama"})
	}
	if c.LLMProvider == "deepseek" && c.DeepSeekKey == "" {
		errs = append(errs, ValidationError{"DeepSeekKey", "must not be empty when LLMProvider is deepseek"})
	}
	if c.LLMProvider == "acp" && c.ACPCommand == "" {
		errs = append(errs, ValidationError{"ACPCommand", "must not be empty when LLMProvider is acp"})
	}
	// TLS: if cert is set, key must also be set, and vice versa
	if (c.TLSCert != "" && c.TLSKey == "") || (c.TLSCert == "" && c.TLSKey != "") {
		errs = append(errs, ValidationError{"TLS", "both BT_TLS_CERT and BT_TLS_KEY must be set for TLS"})
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// TLSEnabled returns true when both cert and key paths are configured.
func (c *Config) TLSEnabled() bool {
	return c.TLSCert != "" && c.TLSKey != ""
}

// FeatureFlags returns a map of all feature flags for dashboard display.
func (c *Config) FeatureFlags() map[string]bool {
	return map[string]bool{
		"gardener":                        c.GardenerEnabled,
		"scheduler":                       c.SchedulerEnabled,
		"auto_evolve":                     c.AutoEvolveEnabled,
		"kanban":                          c.KanbanEnabled,
		"thinktank":                       c.ThinktankEnabled,
		"startup_sim":                     c.StartupSimEnabled,
		"api_enforce_response_validation": c.APIEnforceResponseValidation,
	}
}

// SaveFile writes the current configuration to a JSON file for sharing/review.
func (c *Config) SaveFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Diff compares two configs field-by-field and returns a list of
// human-readable change descriptions. It's designed for ConfigWatcher
// callbacks — when a hot-reload occurs, Diff() tells operators exactly
// which fields changed without needing to read config files manually.
//
// Only fields with different values are included. Secret fields
// (APIKey, DeepSeekKey, TLSCert, TLSKey) are reported as "changed"
// without revealing values. Feature flags report true→false or false→true.
//
// Usage from a ConfigWatcher callback:
//
//	watcher.OnChange(func(newCfg *Config) {
//	    diffs := oldCfg.Diff(newCfg)
//	    for _, d := range diffs {
//	        log.Printf("config changed: %s", d)
//	    }
//	})
func (c *Config) Diff(other *Config) []string {
	if other == nil {
		return []string{"other config is nil"}
	}
	var diffs []string

	// Server
	if c.DashboardPort != other.DashboardPort {
		diffs = append(diffs, fmt.Sprintf("DashboardPort: %d → %d", c.DashboardPort, other.DashboardPort))
	}
	if c.APIKey != other.APIKey {
		if c.APIKey == "" {
			diffs = append(diffs, "APIKey: set")
		} else if other.APIKey == "" {
			diffs = append(diffs, "APIKey: removed")
		} else {
			diffs = append(diffs, "APIKey: changed")
		}
	}
	if c.TLSCert != other.TLSCert {
		if c.TLSCert == "" {
			diffs = append(diffs, "TLSCert: set")
		} else if other.TLSCert == "" {
			diffs = append(diffs, "TLSCert: removed")
		} else {
			diffs = append(diffs, "TLSCert: changed")
		}
	}
	if c.TLSKey != other.TLSKey {
		if c.TLSKey == "" {
			diffs = append(diffs, "TLSKey: set")
		} else if other.TLSKey == "" {
			diffs = append(diffs, "TLSKey: removed")
		} else {
			diffs = append(diffs, "TLSKey: changed")
		}
	}

	// LLM
	if c.LLMProvider != other.LLMProvider {
		diffs = append(diffs, fmt.Sprintf("LLMProvider: %s → %s", c.LLMProvider, other.LLMProvider))
	}
	if c.OllamaHost != other.OllamaHost {
		diffs = append(diffs, fmt.Sprintf("OllamaHost: %s → %s", c.OllamaHost, other.OllamaHost))
	}
	if c.OllamaModel != other.OllamaModel {
		diffs = append(diffs, fmt.Sprintf("OllamaModel: %s → %s", c.OllamaModel, other.OllamaModel))
	}
	if c.DeepSeekHost != other.DeepSeekHost {
		diffs = append(diffs, fmt.Sprintf("DeepSeekHost: %s → %s", c.DeepSeekHost, other.DeepSeekHost))
	}
	if c.DeepSeekModel != other.DeepSeekModel {
		diffs = append(diffs, fmt.Sprintf("DeepSeekModel: %s → %s", c.DeepSeekModel, other.DeepSeekModel))
	}
	if c.DeepSeekKey != other.DeepSeekKey {
		if c.DeepSeekKey == "" {
			diffs = append(diffs, "DeepSeekKey: set")
		} else if other.DeepSeekKey == "" {
			diffs = append(diffs, "DeepSeekKey: removed")
		} else {
			diffs = append(diffs, "DeepSeekKey: changed")
		}
	}
	if c.LLMTimeout != other.LLMTimeout {
		diffs = append(diffs, fmt.Sprintf("LLMTimeout: %ds → %ds", c.LLMTimeout, other.LLMTimeout))
	}
	if c.FallbackModels != other.FallbackModels {
		diffs = append(diffs, fmt.Sprintf("FallbackModels: %q → %q", c.FallbackModels, other.FallbackModels))
	}

	// CORS
	if c.CORSDashboardOrigin != other.CORSDashboardOrigin {
		diffs = append(diffs, fmt.Sprintf("CORSDashboardOrigin: %s → %s", c.CORSDashboardOrigin, other.CORSDashboardOrigin))
	}

	// Rate Limiting
	if c.RateLimitRPS != other.RateLimitRPS {
		diffs = append(diffs, fmt.Sprintf("RateLimitRPS: %.1f → %.1f", c.RateLimitRPS, other.RateLimitRPS))
	}
	if c.RateLimitBurst != other.RateLimitBurst {
		diffs = append(diffs, fmt.Sprintf("RateLimitBurst: %d → %d", c.RateLimitBurst, other.RateLimitBurst))
	}

	// Feature Flags
	if c.GardenerEnabled != other.GardenerEnabled {
		diffs = append(diffs, fmt.Sprintf("GardenerEnabled: %v → %v", c.GardenerEnabled, other.GardenerEnabled))
	}
	if c.SchedulerEnabled != other.SchedulerEnabled {
		diffs = append(diffs, fmt.Sprintf("SchedulerEnabled: %v → %v", c.SchedulerEnabled, other.SchedulerEnabled))
	}
	if c.AutoEvolveEnabled != other.AutoEvolveEnabled {
		diffs = append(diffs, fmt.Sprintf("AutoEvolveEnabled: %v → %v", c.AutoEvolveEnabled, other.AutoEvolveEnabled))
	}
	if c.KanbanEnabled != other.KanbanEnabled {
		diffs = append(diffs, fmt.Sprintf("KanbanEnabled: %v → %v", c.KanbanEnabled, other.KanbanEnabled))
	}
	if c.ThinktankEnabled != other.ThinktankEnabled {
		diffs = append(diffs, fmt.Sprintf("ThinktankEnabled: %v → %v", c.ThinktankEnabled, other.ThinktankEnabled))
	}
	if c.StartupSimEnabled != other.StartupSimEnabled {
		diffs = append(diffs, fmt.Sprintf("StartupSimEnabled: %v → %v", c.StartupSimEnabled, other.StartupSimEnabled))
	}
	if c.APIEnforceResponseValidation != other.APIEnforceResponseValidation {
		diffs = append(diffs, fmt.Sprintf("APIEnforceResponseValidation: %v → %v", c.APIEnforceResponseValidation, other.APIEnforceResponseValidation))
	}

	// Persistence
	if c.ReflectionsDir != other.ReflectionsDir {
		diffs = append(diffs, fmt.Sprintf("ReflectionsDir: %q → %q", c.ReflectionsDir, other.ReflectionsDir))
	}
	if c.AgentDefsDir != other.AgentDefsDir {
		diffs = append(diffs, fmt.Sprintf("AgentDefsDir: %q → %q", c.AgentDefsDir, other.AgentDefsDir))
	}
	if c.HistoryDir != other.HistoryDir {
		diffs = append(diffs, fmt.Sprintf("HistoryDir: %q → %q", c.HistoryDir, other.HistoryDir))
	}
	if c.LogDir != other.LogDir {
		diffs = append(diffs, fmt.Sprintf("LogDir: %q → %q", c.LogDir, other.LogDir))
	}

	// Gardener
	if c.GardenerCycleInterval != other.GardenerCycleInterval {
		diffs = append(diffs, fmt.Sprintf("GardenerCycleInterval: %ds → %ds", c.GardenerCycleInterval, other.GardenerCycleInterval))
	}
	if c.GardenerMutationsPer != other.GardenerMutationsPer {
		diffs = append(diffs, fmt.Sprintf("GardenerMutationsPer: %d → %d", c.GardenerMutationsPer, other.GardenerMutationsPer))
	}
	if c.GardenerMaxNodes != other.GardenerMaxNodes {
		diffs = append(diffs, fmt.Sprintf("GardenerMaxNodes: %d → %d", c.GardenerMaxNodes, other.GardenerMaxNodes))
	}

	// Scheduler
	if c.SchedulerCheckInterval != other.SchedulerCheckInterval {
		diffs = append(diffs, fmt.Sprintf("SchedulerCheckInterval: %ds → %ds", c.SchedulerCheckInterval, other.SchedulerCheckInterval))
	}

	// Error Handling
	if c.RetryMaxRetries != other.RetryMaxRetries {
		diffs = append(diffs, fmt.Sprintf("RetryMaxRetries: %d → %d", c.RetryMaxRetries, other.RetryMaxRetries))
	}
	if c.RetryBaseDelayMs != other.RetryBaseDelayMs {
		diffs = append(diffs, fmt.Sprintf("RetryBaseDelayMs: %dms → %dms", c.RetryBaseDelayMs, other.RetryBaseDelayMs))
	}
	if c.RetryMaxDelayMs != other.RetryMaxDelayMs {
		diffs = append(diffs, fmt.Sprintf("RetryMaxDelayMs: %dms → %dms", c.RetryMaxDelayMs, other.RetryMaxDelayMs))
	}
	if c.RetryLLMBaseMs != other.RetryLLMBaseMs {
		diffs = append(diffs, fmt.Sprintf("RetryLLMBaseMs: %dms → %dms", c.RetryLLMBaseMs, other.RetryLLMBaseMs))
	}
	if c.RetryJitter != other.RetryJitter {
		diffs = append(diffs, fmt.Sprintf("RetryJitter: %s → %s", c.RetryJitter, other.RetryJitter))
	}
	if c.RetryUnknown != other.RetryUnknown {
		diffs = append(diffs, fmt.Sprintf("RetryUnknown: %v → %v", c.RetryUnknown, other.RetryUnknown))
	}
	if c.CBThreshold != other.CBThreshold {
		diffs = append(diffs, fmt.Sprintf("CBThreshold: %d → %d", c.CBThreshold, other.CBThreshold))
	}
	if c.CBCooldownSecs != other.CBCooldownSecs {
		diffs = append(diffs, fmt.Sprintf("CBCooldownSecs: %ds → %ds", c.CBCooldownSecs, other.CBCooldownSecs))
	}
	if c.DLQMaxEntries != other.DLQMaxEntries {
		diffs = append(diffs, fmt.Sprintf("DLQMaxEntries: %d → %d", c.DLQMaxEntries, other.DLQMaxEntries))
	}

	// Validation
	if c.MaxBodySize != other.MaxBodySize {
		diffs = append(diffs, fmt.Sprintf("MaxBodySize: %d → %d", c.MaxBodySize, other.MaxBodySize))
	}

	return diffs
}

// Sanitized returns a copy of the config with secret fields redacted.
// Safe to expose via public API endpoints or logs.
func (c *Config) Sanitized() Config {
	s := *c
	if s.APIKey != "" {
		s.APIKey = "[REDACTED]"
	}
	if s.DeepSeekKey != "" {
		s.DeepSeekKey = "[REDACTED]"
	}
	if s.TLSCert != "" {
		s.TLSCert = "[REDACTED]"
	}
	if s.TLSKey != "" {
		s.TLSKey = "[REDACTED]"
	}
	return s
}

// ─── Helpers ────────────────────────────────────────────────────────────────

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

// RetryBaseDuration returns the retry base delay as time.Duration.
func (c *Config) RetryBaseDuration() time.Duration {
	return time.Duration(c.RetryBaseDelayMs) * time.Millisecond
}

// RetryMaxDuration returns the retry max delay as time.Duration.
func (c *Config) RetryMaxDuration() time.Duration {
	return time.Duration(c.RetryMaxDelayMs) * time.Millisecond
}

// RetryLLMBaseDuration returns the retry LLM base delay as time.Duration.
func (c *Config) RetryLLMBaseDuration() time.Duration {
	return time.Duration(c.RetryLLMBaseMs) * time.Millisecond
}

// CBCooldownDuration returns the circuit breaker cooldown as time.Duration.
func (c *Config) CBCooldownDuration() time.Duration {
	return time.Duration(c.CBCooldownSecs) * time.Second
}

// DLQMaxEntriesLimit returns the dead letter queue max entry count.
// Returns 0 (unlimited) if DLQMaxEntries is 0 or negative.
func (c *Config) DLQMaxEntriesLimit() int {
	if c.DLQMaxEntries <= 0 {
		return 0
	}
	return c.DLQMaxEntries
}

// ─── Runtime Checks ──────────────────────────────────────────────────────────

// RuntimeIssue represents a runtime configuration consistency issue.
type RuntimeIssue struct {
	Severity  string // "error" or "warning"
	Component string // config field or subsystem
	Message   string // human-readable description
}

// RuntimeReport contains the results of a runtime consistency check.
type RuntimeReport struct {
	Ok     bool           `json:"ok"`
	Issues []RuntimeIssue `json:"issues,omitempty"`
}

// CheckRuntime performs runtime consistency checks that go beyond static
// value validation. It verifies that configured filesystem paths exist,
// that the Ollama host is reachable (if using the ollama provider), and
// that persistence directories are usable. Unlike Validate() which checks
// value constraints, CheckRuntime() verifies that the configured external
// resources are actually available at startup.
//
// Issues are categorized as "error" (critical, platform may not function)
// or "warning" (non-critical, worth logging). A platform can use this to
// decide whether to start in degraded mode or abort entirely.
func (c *Config) CheckRuntime() RuntimeReport {
	var issues []RuntimeIssue

	// ── TLS files ──────────────────────────────────────────────────────
	if c.TLSCert != "" {
		if _, err := os.Stat(c.TLSCert); os.IsNotExist(err) {
			issues = append(issues, RuntimeIssue{
				Severity:  "error",
				Component: "TLSCert",
				Message:   fmt.Sprintf("TLS certificate file not found: %s", c.TLSCert),
			})
		}
	}
	if c.TLSKey != "" {
		if _, err := os.Stat(c.TLSKey); os.IsNotExist(err) {
			issues = append(issues, RuntimeIssue{
				Severity:  "error",
				Component: "TLSKey",
				Message:   fmt.Sprintf("TLS key file not found: %s", c.TLSKey),
			})
		}
	}

	// ── Persistence directories (directories are created on demand,
	//     but we check parent dir existence for explicit paths) ─────────
	issues = c.checkDirAccess(issues, "ReflectionsDir", c.ReflectionsDir)
	issues = c.checkDirAccess(issues, "AgentDefsDir", c.AgentDefsDir)
	issues = c.checkDirAccess(issues, "HistoryDir", c.HistoryDir)
	issues = c.checkDirAccess(issues, "LogDir", c.LogDir)

	// ── Config file ───────────────────────────────────────────────────
	if c.ConfigFile != "" {
		if _, err := os.Stat(c.ConfigFile); os.IsNotExist(err) {
			issues = append(issues, RuntimeIssue{
				Severity:  "warning",
				Component: "ConfigFile",
				Message:   fmt.Sprintf("Config file specified but not found, using defaults: %s", c.ConfigFile),
			})
		}
	}

	// ── Ollama reachability ───────────────────────────────────────────
	if c.LLMProvider == "ollama" && c.OllamaHost != "" {
		if !c.ollamaReachable() {
			issues = append(issues, RuntimeIssue{
				Severity:  "warning",
				Component: "OllamaHost",
				Message:   fmt.Sprintf("Ollama host unreachable, agent execution will be degraded: %s", c.OllamaHost),
			})
		}
	}

	// ── DeepSeek reachability ─────────────────────────────────────────
	if c.LLMProvider == "deepseek" && c.DeepSeekHost != "" {
		if !c.deepseekReachable() {
			issues = append(issues, RuntimeIssue{
				Severity:  "warning",
				Component: "DeepSeekHost",
				Message:   fmt.Sprintf("DeepSeek host unreachable, agent execution will be degraded: %s", c.DeepSeekHost),
			})
		}
	}

	if len(issues) == 0 {
		return RuntimeReport{Ok: true}
	}
	return RuntimeReport{Ok: false, Issues: issues}
}

// checkDirAccess validates that a configured directory path either exists
// as a directory or can be created. Empty paths are skipped (not configured).
func (c *Config) checkDirAccess(issues []RuntimeIssue, component, dir string) []RuntimeIssue {
	if dir == "" {
		return issues
	}
	info, err := os.Stat(dir)
	if err == nil {
		if !info.IsDir() {
			issues = append(issues, RuntimeIssue{
				Severity:  "error",
				Component: component,
				Message:   fmt.Sprintf("path exists but is not a directory: %s", dir),
			})
		}
		return issues
	}
	if os.IsNotExist(err) {
		// Check if the parent directory exists (so we could create it).
		parent := filepath.Dir(dir)
		if _, perr := os.Stat(parent); os.IsNotExist(perr) {
			issues = append(issues, RuntimeIssue{
				Severity:  "warning",
				Component: component,
				Message:   fmt.Sprintf("directory does not exist and parent is missing: %s", dir),
			})
		}
		return issues
	}
	// Other error (permission, etc.)
	issues = append(issues, RuntimeIssue{
		Severity:  "error",
		Component: component,
		Message:   fmt.Sprintf("cannot access directory %s: %v", dir, err),
	})
	return issues
}

// ollamaReachable performs a lightweight HTTP check against the Ollama API
// root endpoint. Uses a 5-second timeout to avoid blocking startup.
// When ollamaChecker is set (e.g., in tests), it delegates to that function
// instead of making a real network call.
func (c *Config) ollamaReachable() bool {
	if ollamaChecker != nil {
		return ollamaChecker(c.OllamaHost)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	url := strings.TrimRight(c.OllamaHost, "/") + "/api/tags"
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ollamaChecker is a package-level function that can be overridden in tests
// to avoid real network calls. Tests set it to a mock; production code leaves
// it nil, which causes ollamaReachable to use a real HTTP client.
var ollamaChecker func(host string) bool

// deepseekReachable performs a lightweight HTTP check against the DeepSeek API
// /models endpoint. Uses a 5-second timeout to avoid blocking startup.
// When deepseekChecker is set (e.g., in tests), it delegates to that function
// instead of making a real network call.
func (c *Config) deepseekReachable() bool {
	if deepseekChecker != nil {
		return deepseekChecker(c.DeepSeekHost)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	url := strings.TrimRight(c.DeepSeekHost, "/") + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}
	if c.DeepSeekKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.DeepSeekKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	// Any response (200 OK, 401 unauthorized, 403 rate limited)
	// means the host is reachable. Only connection failures are
	// considered "unreachable."
	return true
}

// deepseekChecker is a package-level function that can be overridden in tests
// to avoid real network calls. Tests set it to a mock; production code leaves
// it nil, which causes deepseekReachable to use a real HTTP client.
var deepseekChecker func(host string) bool
