package config

// DefaultConfig returns a Config populated with production-safe defaults.
// This is the public API; newDefaultConfig is the internal equivalent used
// by Load() and LoadFile(). Callers needing a fresh config with defaults
// should use this instead of constructing a zero-value Config.
func DefaultConfig() *Config {
	return newDefaultConfig()
}

// Default values grouped by category for auditability.
// These are applied by newDefaultConfig() and may be overridden by:
//  1. JSON config file (LoadFile)
//  2. .env files (applyDotEnvFiles)
//  3. Environment variables (applyEnvOverrides)
const (
	// ── Dashboard ──
	defaultDashboardPort = 9800

	// ── LLM Provider ──
	defaultLLMProvider   = "ollama"
	defaultOllamaHost    = "http://localhost:11434"
	defaultOllamaModel   = "qwen3.6:35b-a3b"
	defaultDeepSeekHost  = "https://api.deepseek.com/v1"
	defaultDeepSeekModel = "deepseek-v4-flash"
	defaultACPCommand    = "hermes"
	defaultACPArgs       = "acp --accept-hooks"
	defaultLLMTimeout    = 300

	// ── Rate Limiting ──
	defaultRateLimitRPS   = 100.0
	defaultRateLimitBurst = 20

	// ── Feature Flags ──
	defaultGardenerEnabled              = true
	defaultSchedulerEnabled             = true
	defaultKanbanEnabled                = true
	defaultThinktankEnabled             = true
	defaultStartupSimEnabled            = true
	defaultAPIEnforceResponseValidation = false

	// ── Gardener ──
	defaultGardenerCycleInterval = 300
	defaultGardenerMutationsPer  = 2
	defaultGardenerMaxNodes      = 20

	// ── Scheduler ──
	defaultSchedulerCheckInterval = 60

	// ── Security ──
	defaultMaxBodySize = 1_048_576 // 1 MB

	// ── Error Handling ──
	defaultRetryMaxRetries  = 3
	defaultRetryBaseDelayMs = 1000
	defaultRetryMaxDelayMs  = 30000
	defaultRetryLLMBaseMs   = 2000
	defaultRetryJitter      = "full_jitter"
	defaultRetryUnknown     = false
	defaultCBThreshold      = 3
	defaultCBCooldownSecs   = 300
	defaultDLQMaxEntries    = 1000
)
