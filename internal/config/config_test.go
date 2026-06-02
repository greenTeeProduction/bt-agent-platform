package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Unset any env vars that might interfere
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_LLM_TIMEOUT")
	os.Unsetenv("BT_FEATURE_AUTO_EVOLVE")
	os.Unsetenv("BT_CONFIG_FILE")

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
	if c.FallbackModels != "" {
		t.Errorf("expected no fallback models by default, got %q", c.FallbackModels)
	}
}

func TestLoad_FallbackModelsEnvOverride(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_FALLBACK_MODELS", "deepseek:deepseek-v4-pro,ollama:qwen3.6:35b-a3b")
	defer os.Unsetenv("BT_FALLBACK_MODELS")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.FallbackModels != "deepseek:deepseek-v4-pro,ollama:qwen3.6:35b-a3b" {
		t.Fatalf("expected fallback models from env, got %q", c.FallbackModels)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
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

func TestLoadFile_Basic(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 1234,
		"ollama_model": "file-model",
		"llm_timeout": 60,
		"gardener_enabled": false,
		"thinktank_enabled": true
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Clear env vars that could interfere
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")
	os.Unsetenv("BT_LLM_TIMEOUT")
	os.Unsetenv("BT_FEATURE_GARDENER")
	os.Unsetenv("BT_FEATURE_THINKTANK")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}

	if c.DashboardPort != 1234 {
		t.Errorf("expected DashboardPort=1234, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "file-model" {
		t.Errorf("expected OllamaModel=file-model, got %s", c.OllamaModel)
	}
	if c.LLMTimeout != 60 {
		t.Errorf("expected LLMTimeout=60, got %d", c.LLMTimeout)
	}
	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false from file")
	}
	if !c.ThinktankEnabled {
		t.Error("expected ThinktankEnabled=true from file")
	}
	// Unset fields should use defaults
	if c.SchedulerCheckInterval != 60 {
		t.Errorf("expected default SchedulerCheckInterval=60, got %d", c.SchedulerCheckInterval)
	}
}

func TestLoadFile_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 1234,
		"ollama_model": "file-model"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Env should override file
	os.Setenv("BT_DASHBOARD_PORT", "9999")
	defer os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}

	if c.DashboardPort != 9999 {
		t.Errorf("expected env override DashboardPort=9999, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "file-model" {
		t.Errorf("expected OllamaModel=file-model (no env override), got %s", c.OllamaModel)
	}
}

func TestLoadFile_MissingFile(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cf, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(cf)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoad_BTConfigFile(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 5555,
		"ollama_model": "from-file"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_CONFIG_FILE", cf)
	defer os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with BT_CONFIG_FILE failed: %v", err)
	}

	if c.DashboardPort != 5555 {
		t.Errorf("expected DashboardPort=5555 from file via BT_CONFIG_FILE, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "from-file" {
		t.Errorf("expected OllamaModel=from-file via BT_CONFIG_FILE, got %s", c.OllamaModel)
	}
	if c.ConfigFile != cf {
		t.Errorf("expected ConfigFile=%s, got %s", cf, c.ConfigFile)
	}
}

func TestLoad_BTConfigFile_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 5555,
		"ollama_model": "from-file"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_CONFIG_FILE", cf)
	os.Setenv("BT_DASHBOARD_PORT", "7777") // env overrides file
	defer func() {
		os.Unsetenv("BT_CONFIG_FILE")
		os.Unsetenv("BT_DASHBOARD_PORT")
	}()
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Env overrides file
	if c.DashboardPort != 7777 {
		t.Errorf("expected env override DashboardPort=7777, got %d", c.DashboardPort)
	}
	// File value used when no env override
	if c.OllamaModel != "from-file" {
		t.Errorf("expected OllamaModel=from-file (no env override), got %s", c.OllamaModel)
	}
}

func TestSaveFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	cf1 := filepath.Join(dir, "config1.json")
	cf2 := filepath.Join(dir, "config2.json")

	// Create a config with non-default values
	c1 := newDefaultConfig()
	c1.DashboardPort = 4321
	c1.OllamaModel = "roundtrip-model"
	c1.GardenerEnabled = false
	c1.ThinktankEnabled = true
	c1.MaxBodySize = 5242880

	if err := c1.SaveFile(cf1); err != nil {
		t.Fatalf("SaveFile() failed: %v", err)
	}

	// Clear env vars, load the saved file
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")
	os.Unsetenv("BT_FEATURE_GARDENER")
	os.Unsetenv("BT_FEATURE_THINKTANK")
	os.Unsetenv("BT_MAX_BODY_SIZE")

	c2, err := LoadFile(cf1)
	if err != nil {
		t.Fatalf("LoadFile() roundtrip failed: %v", err)
	}

	if c2.DashboardPort != 4321 {
		t.Errorf("roundtrip: expected DashboardPort=4321, got %d", c2.DashboardPort)
	}
	if c2.OllamaModel != "roundtrip-model" {
		t.Errorf("roundtrip: expected OllamaModel=roundtrip-model, got %s", c2.OllamaModel)
	}
	if c2.GardenerEnabled {
		t.Error("roundtrip: expected GardenerEnabled=false")
	}
	if !c2.ThinktankEnabled {
		t.Error("roundtrip: expected ThinktankEnabled=true")
	}
	if c2.MaxBodySize != 5242880 {
		t.Errorf("roundtrip: expected MaxBodySize=5242880, got %d", c2.MaxBodySize)
	}

	// Save again and verify consistency
	if err := c2.SaveFile(cf2); err != nil {
		t.Fatalf("second SaveFile() failed: %v", err)
	}

	c3, err := LoadFile(cf2)
	if err != nil {
		t.Fatalf("second LoadFile() failed: %v", err)
	}

	if c3.DashboardPort != c2.DashboardPort {
		t.Error("second roundtrip: ports don't match")
	}
}

func TestLoadFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cf, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile(empty) failed: %v", err)
	}

	// Empty file should produce defaults
	if c.DashboardPort != 9800 {
		t.Errorf("expected default DashboardPort=9800, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "qwen3.6:35b-a3b" {
		t.Errorf("expected default OllamaModel, got %s", c.OllamaModel)
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
	c.DashboardPort = 0    // invalid
	c.LLMTimeout = 0       // invalid
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

func TestTLS_Defaults(t *testing.T) {
	os.Unsetenv("BT_TLS_CERT")
	os.Unsetenv("BT_TLS_KEY")

	c, _ := Load()
	if c.TLSEnabled() {
		t.Error("expected TLSEnabled()=false by default")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("valid config (no TLS) should pass validation, got: %v", err)
	}
}

func TestTLS_Enabled(t *testing.T) {
	os.Setenv("BT_TLS_CERT", "/etc/certs/server.crt")
	os.Setenv("BT_TLS_KEY", "/etc/certs/server.key")
	defer os.Unsetenv("BT_TLS_CERT")
	defer os.Unsetenv("BT_TLS_KEY")

	c, _ := Load()
	if !c.TLSEnabled() {
		t.Error("expected TLSEnabled()=true when both cert and key set")
	}
	if c.TLSCert != "/etc/certs/server.crt" {
		t.Errorf("expected TLSCert='/etc/certs/server.crt', got %s", c.TLSCert)
	}
	if c.TLSKey != "/etc/certs/server.key" {
		t.Errorf("expected TLSKey='/etc/certs/server.key', got %s", c.TLSKey)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("valid TLS config should pass validation, got: %v", err)
	}
}

func TestTLS_MissingKey(t *testing.T) {
	os.Setenv("BT_TLS_CERT", "/etc/certs/server.crt")
	os.Unsetenv("BT_TLS_KEY")
	defer os.Unsetenv("BT_TLS_CERT")

	c, _ := Load()
	if c.TLSEnabled() {
		t.Error("expected TLSEnabled()=false when only cert set")
	}
	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when cert set but key missing")
	}
}

func TestTLS_MissingCert(t *testing.T) {
	os.Unsetenv("BT_TLS_CERT")
	os.Setenv("BT_TLS_KEY", "/etc/certs/server.key")
	defer os.Unsetenv("BT_TLS_KEY")

	c, _ := Load()
	if c.TLSEnabled() {
		t.Error("expected TLSEnabled()=false when only key set")
	}
	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when key set but cert missing")
	}
}

func TestTLS_FileConfig(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"tls_cert": "/etc/certs/file.crt",
		"tls_key": "/etc/certs/file.key"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}
	if !c.TLSEnabled() {
		t.Error("expected TLS enabled from config file")
	}
	if c.TLSCert != "/etc/certs/file.crt" {
		t.Errorf("expected TLSCert from file, got %s", c.TLSCert)
	}
	if c.TLSKey != "/etc/certs/file.key" {
		t.Errorf("expected TLSKey from file, got %s", c.TLSKey)
	}
}

func TestLoadFile_BooleanExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"gardener_enabled": false,
		"scheduler_enabled": false
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("BT_FEATURE_GARDENER")
	os.Unsetenv("BT_FEATURE_SCHEDULER")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}

	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false when explicitly set to false in file")
	}
	if c.SchedulerEnabled {
		t.Error("expected SchedulerEnabled=false when explicitly set to false in file")
	}
}

// ─── .env Loading Tests ────────────────────────────────────────────────────

func TestLoadDotEnv_Basic(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_DASHBOARD_PORT=7777\nBT_OLLAMA_MODEL=env-model\nBT_LLM_TIMEOUT=120\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_DASHBOARD_PORT"] != "7777" {
		t.Errorf("expected BT_DASHBOARD_PORT=7777, got %q", kv["BT_DASHBOARD_PORT"])
	}
	if kv["BT_OLLAMA_MODEL"] != "env-model" {
		t.Errorf("expected BT_OLLAMA_MODEL=env-model, got %q", kv["BT_OLLAMA_MODEL"])
	}
	if kv["BT_LLM_TIMEOUT"] != "120" {
		t.Errorf("expected BT_LLM_TIMEOUT=120, got %q", kv["BT_LLM_TIMEOUT"])
	}
}

func TestLoadDotEnv_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "# This is a comment\nBT_API_KEY=secret123\n\n# Another comment\nBT_FEATURE_GARDENER=false\n\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if len(kv) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(kv), kv)
	}
	if kv["BT_API_KEY"] != "secret123" {
		t.Errorf("expected BT_API_KEY=secret123, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_FEATURE_GARDENER"] != "false" {
		t.Errorf("expected BT_FEATURE_GARDENER=false, got %q", kv["BT_FEATURE_GARDENER"])
	}
}

func TestLoadDotEnv_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY=\"my secret key\"\nBT_OLLAMA_MODEL='quoted-model'\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_API_KEY"] != "my secret key" {
		t.Errorf("expected BT_API_KEY=my secret key, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_OLLAMA_MODEL"] != "quoted-model" {
		t.Errorf("expected BT_OLLAMA_MODEL=quoted-model, got %q", kv["BT_OLLAMA_MODEL"])
	}
}

func TestLoadDotEnv_ExportPrefix(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "export BT_DASHBOARD_PORT=9999\nexport BT_FEATURE_GARDENER=true\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_DASHBOARD_PORT"] != "9999" {
		t.Errorf("expected BT_DASHBOARD_PORT=9999, got %q", kv["BT_DASHBOARD_PORT"])
	}
	if kv["BT_FEATURE_GARDENER"] != "true" {
		t.Errorf("expected BT_FEATURE_GARDENER=true, got %q", kv["BT_FEATURE_GARDENER"])
	}
}

func TestLoadDotEnv_InlineComments(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY=secret123  # API key for dashboard\nBT_LLM_TIMEOUT=120 # 2 minutes\n# pure comment line\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_API_KEY"] != "secret123" {
		t.Errorf("expected BT_API_KEY=secret123, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_LLM_TIMEOUT"] != "120" {
		t.Errorf("expected BT_LLM_TIMEOUT=120, got %q", kv["BT_LLM_TIMEOUT"])
	}
}

func TestLoadDotEnv_MissingFile(t *testing.T) {
	_, err := LoadDotEnv("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for missing .env file")
	}
}

func TestLoadDotEnv_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}
	if len(kv) != 0 {
		t.Errorf("expected 0 entries from empty file, got %d", len(kv))
	}
}

func TestLoad_DotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_DASHBOARD_PORT=6666\nBT_OLLAMA_MODEL=dotenv-model\nBT_FEATURE_GARDENER=false\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set BT_DOTENV_FILE and clear env vars
	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")
	os.Unsetenv("BT_FEATURE_GARDENER")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with .env failed: %v", err)
	}

	if c.DashboardPort != 6666 {
		t.Errorf("expected DashboardPort=6666 from .env, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "dotenv-model" {
		t.Errorf("expected OllamaModel=dotenv-model from .env, got %s", c.OllamaModel)
	}
	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false from .env")
	}
	// Unset fields should use defaults
	if c.LLMTimeout != 300 {
		t.Errorf("expected default LLMTimeout=300, got %d", c.LLMTimeout)
	}
}

func TestLoad_DotEnvOverriddenByEnvVar(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_DASHBOARD_PORT=6666\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// .env says 6666 but env var says 8888 — env var must win
	os.Setenv("BT_DOTENV_FILE", envFile)
	os.Setenv("BT_DASHBOARD_PORT", "8888")
	defer func() {
		os.Unsetenv("BT_DOTENV_FILE")
		os.Unsetenv("BT_DASHBOARD_PORT")
	}()
	os.Unsetenv("BT_CONFIG_FILE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if c.DashboardPort != 8888 {
		t.Errorf("expected env var DashboardPort=8888 (overriding .env=6666), got %d", c.DashboardPort)
	}
}

func TestLoad_DotEnvRespectsEnvVarPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY=dotenv-key\nBT_OLLAMA_MODEL=dotenv-model\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Only set BT_API_KEY env var, not BT_OLLAMA_MODEL
	os.Setenv("BT_DOTENV_FILE", envFile)
	os.Setenv("BT_API_KEY", "env-var-key")
	defer func() {
		os.Unsetenv("BT_DOTENV_FILE")
		os.Unsetenv("BT_API_KEY")
	}()
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// BT_API_KEY should come from env var (overrides .env)
	if c.APIKey != "env-var-key" {
		t.Errorf("expected APIKey=env-var-key (env var overrides .env), got %s", c.APIKey)
	}
	// BT_OLLAMA_MODEL should come from .env (no env var set)
	if c.OllamaModel != "dotenv-model" {
		t.Errorf("expected OllamaModel=dotenv-model (from .env, no env override), got %s", c.OllamaModel)
	}
}

func TestLoad_DotEnvAndConfigFile(t *testing.T) {
	// Priority: defaults → config.json → .env → env vars
	dir := t.TempDir()

	// Create config.json
	cfgFile := filepath.Join(dir, "config.json")
	cfgContent := `{"dashboard_port": 1111, "ollama_model": "cfg-model"}`
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .env
	envFile := filepath.Join(dir, ".env")
	envContent := "BT_DASHBOARD_PORT=2222\n" // .env overrides config.json
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_CONFIG_FILE", cfgFile)
	os.Setenv("BT_DOTENV_FILE", envFile)
	defer func() {
		os.Unsetenv("BT_CONFIG_FILE")
		os.Unsetenv("BT_DOTENV_FILE")
	}()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// .env overrides config.json
	if c.DashboardPort != 2222 {
		t.Errorf("expected DashboardPort=2222 (.env overrides config.json=1111), got %d", c.DashboardPort)
	}
	// Model comes from config.json (not in .env)
	if c.OllamaModel != "cfg-model" {
		t.Errorf("expected OllamaModel=cfg-model (from config.json), got %s", c.OllamaModel)
	}
}

func TestLoad_DotEnvDeepSeekKey(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "DEEPSEEK_API_KEY=sk-dotenv-key\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DEEPSEEK_KEY")
	os.Unsetenv("DEEPSEEK_API_KEY")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// DEEPSEEK_API_KEY in .env should map to DeepSeekKey
	if c.DeepSeekKey != "sk-dotenv-key" {
		t.Errorf("expected DeepSeekKey=sk-dotenv-key from .env DEEPSEEK_API_KEY, got %q", c.DeepSeekKey)
	}
}

func TestLoadDotEnv_SpacesAroundEquals(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY = padded-key\nBT_OLLAMA_MODEL=  trimmed-model  \n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_API_KEY"] != "padded-key" {
		t.Errorf("expected BT_API_KEY=padded-key, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_OLLAMA_MODEL"] != "trimmed-model" {
		t.Errorf("expected BT_OLLAMA_MODEL=trimmed-model, got %q", kv["BT_OLLAMA_MODEL"])
	}
}

func TestConfig_Sanitized_RedactsSecrets(t *testing.T) {
	c := &Config{
		DashboardPort: 9800,
		APIKey:        "secret-api-key-12345",
		DeepSeekKey:   "sk-deepseek-secret",
		TLSCert:       "/path/to/cert.pem",
		TLSKey:        "/path/to/key.pem",
		OllamaModel:   "qwen3.6:35b-a3b",
		LLMTimeout:    300,
	}
	s := c.Sanitized()

	if s.APIKey != "[REDACTED]" {
		t.Errorf("expected APIKey to be redacted, got %q", s.APIKey)
	}
	if s.DeepSeekKey != "[REDACTED]" {
		t.Errorf("expected DeepSeekKey to be redacted, got %q", s.DeepSeekKey)
	}
	if s.TLSCert != "[REDACTED]" {
		t.Errorf("expected TLSCert to be redacted, got %q", s.TLSCert)
	}
	if s.TLSKey != "[REDACTED]" {
		t.Errorf("expected TLSKey to be redacted, got %q", s.TLSKey)
	}

	// Non-secret fields should be preserved
	if s.DashboardPort != 9800 {
		t.Errorf("expected DashboardPort=9800, got %d", s.DashboardPort)
	}
	if s.OllamaModel != "qwen3.6:35b-a3b" {
		t.Errorf("expected OllamaModel=qwen3.6:35b-a3b, got %q", s.OllamaModel)
	}
	if s.LLMTimeout != 300 {
		t.Errorf("expected LLMTimeout=300, got %d", s.LLMTimeout)
	}

	// Original should be unchanged
	if c.APIKey != "secret-api-key-12345" {
		t.Error("original config was mutated")
	}
}

func TestConfig_Sanitized_EmptySecrets(t *testing.T) {
	c := &Config{
		DashboardPort: 9800,
		OllamaModel:   "qwen3.6:35b-a3b",
	}
	s := c.Sanitized()

	if s.APIKey != "" {
		t.Errorf("expected empty APIKey to stay empty, got %q", s.APIKey)
	}
	if s.DeepSeekKey != "" {
		t.Errorf("expected empty DeepSeekKey to stay empty, got %q", s.DeepSeekKey)
	}
	if s.TLSCert != "" {
		t.Errorf("expected empty TLSCert to stay empty, got %q", s.TLSCert)
	}
}

// ─── Validation Edge Case Tests ─────────────────────────────────────────────

func TestValidate_OllamaProvider_NoHost(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "" // empty host should fail

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when OllamaHost is empty with ollama provider")
	}
}

func TestValidate_DeepSeekProvider_NoKey(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "" // empty key should fail

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when DeepSeekKey is empty with deepseek provider")
	}
}

func TestValidate_DeepSeekProvider_WithKey(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-valid-deepseek-key"
	c.DeepSeekHost = "https://api.deepseek.com/v1"

	err := c.Validate()
	if err != nil {
		t.Errorf("expected valid deepseek config, got: %v", err)
	}
}

func TestValidate_GardenerCycleInterval_Max(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerCycleInterval = 86400 // exactly at max (1 day)

	err := c.Validate()
	if err != nil {
		t.Errorf("expected GardenerCycleInterval=86400 to be valid, got: %v", err)
	}
}

func TestValidate_GardenerCycleInterval_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerCycleInterval = 86401 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when GardenerCycleInterval > 86400")
	}
}

func TestValidate_SchedulerCheckInterval_Min(t *testing.T) {
	c := newDefaultConfig()
	c.SchedulerCheckInterval = 10 // exactly at min

	err := c.Validate()
	if err != nil {
		t.Errorf("expected SchedulerCheckInterval=10 to be valid, got: %v", err)
	}
}

func TestValidate_SchedulerCheckInterval_Max(t *testing.T) {
	c := newDefaultConfig()
	c.SchedulerCheckInterval = 3600 // exactly at max

	err := c.Validate()
	if err != nil {
		t.Errorf("expected SchedulerCheckInterval=3600 to be valid, got: %v", err)
	}
}

func TestValidate_SchedulerCheckInterval_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.SchedulerCheckInterval = 3601 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when SchedulerCheckInterval > 3600")
	}
}

func TestValidate_AllEdgeCases(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 0         // invalid
	c.LLMTimeout = 0            // invalid
	c.GardenerMaxNodes = 0      // invalid
	c.MaxBodySize = 0           // invalid
	c.GardenerCycleInterval = 5 // invalid (< 10)
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = ""    // invalid (no key for deepseek)
	c.RateLimitBurst = -1 // invalid (negative)

	err := c.Validate()
	if err == nil {
		t.Error("expected multiple validation errors")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	// We expect at least 6 errors
	if len(verrs) < 6 {
		t.Errorf("expected at least 6 validation errors, got %d", len(verrs))
	}
}

func TestValidate_ACPProvider_DefaultHermesCommand(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "acp"
	c.ACPCommand = "hermes"
	c.ACPArgs = "acp --accept-hooks"
	c.ACPCwd = "/tmp"

	if err := c.Validate(); err != nil {
		t.Fatalf("expected ACP provider config to validate, got: %v", err)
	}
}

func TestValidate_ACPProvider_NoCommand(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "acp"
	c.ACPCommand = ""

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when ACPCommand is empty with acp provider")
	}
}

func TestValidate_LLMProvider_Invalid(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "openai" // not ollama, deepseek, or acp

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error for invalid LLMProvider")
	}
}

func TestValidate_RateLimitRPS_Negative(t *testing.T) {
	c := newDefaultConfig()
	c.RateLimitRPS = -1.5

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error for negative RateLimitRPS")
	}
}

func TestValidate_GardenerMutationsPer_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerMutationsPer = 11

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when GardenerMutationsPer > 10")
	}
}

func TestValidate_GardenerMutationsPer_Negative(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerMutationsPer = -1

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when GardenerMutationsPer < 0")
	}
}

func TestValidate_MaxBodySize_TooSmall(t *testing.T) {
	c := newDefaultConfig()
	c.MaxBodySize = 512 // below min of 1024

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when MaxBodySize < 1024")
	}
}

func TestValidate_MaxBodySize_TooLarge(t *testing.T) {
	c := newDefaultConfig()
	c.MaxBodySize = 200 * 1024 * 1024 // above max of 100MB

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when MaxBodySize > 100MB")
	}
}

func TestValidate_BoundaryValues_AllValid(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 1           // min
	c.LLMTimeout = 1              // min
	c.RateLimitRPS = 0            // min
	c.RateLimitBurst = 0          // min
	c.GardenerCycleInterval = 10  // min
	c.GardenerMutationsPer = 0    // min
	c.GardenerMaxNodes = 1        // min
	c.SchedulerCheckInterval = 10 // min
	c.MaxBodySize = 1024          // min

	err := c.Validate()
	if err != nil {
		t.Errorf("expected boundary minimums to be valid, got: %v", err)
	}

	c2 := newDefaultConfig()
	c2.DashboardPort = 65535           // max
	c2.LLMTimeout = 3600               // max
	c2.GardenerCycleInterval = 86400   // max
	c2.GardenerMutationsPer = 10       // max
	c2.GardenerMaxNodes = 100          // max
	c2.SchedulerCheckInterval = 3600   // max
	c2.MaxBodySize = 100 * 1024 * 1024 // max

	err = c2.Validate()
	if err != nil {
		t.Errorf("expected boundary maximums to be valid, got: %v", err)
	}
}

func TestValidate_LLMTimeout_Max(t *testing.T) {
	c := newDefaultConfig()
	c.LLMTimeout = 3600 // exactly at max

	err := c.Validate()
	if err != nil {
		t.Errorf("expected LLMTimeout=3600 to be valid, got: %v", err)
	}
}

func TestValidate_LLMTimeout_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.LLMTimeout = 3601 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when LLMTimeout > 3600")
	}
}

func TestValidate_DashboardPort_Max(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 65535 // exactly at max

	err := c.Validate()
	if err != nil {
		t.Errorf("expected DashboardPort=65535 to be valid, got: %v", err)
	}
}

func TestValidate_DashboardPort_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 65536 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when DashboardPort > 65535")
	}
}

func TestValidate_OllamaProvider_ModelEmpty(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaModel = ""

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when OllamaModel is empty with ollama provider")
	}
}

func TestValidate_TLS_CertOnly(t *testing.T) {
	c := newDefaultConfig()
	c.TLSCert = "/path/to/cert.pem"
	c.TLSKey = ""

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when TLS cert set but key missing")
	}
}

func TestValidate_TLS_KeyOnly(t *testing.T) {
	c := newDefaultConfig()
	c.TLSCert = ""
	c.TLSKey = "/path/to/key.pem"

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when TLS key set but cert missing")
	}
}

// ─── CheckRuntime Tests ────────────────────────────────────────────────────

func TestCheckRuntime_AllOk(t *testing.T) {
	// Use a real directory that exists for persistence paths.
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.ReflectionsDir = tmp
	c.AgentDefsDir = tmp
	c.HistoryDir = tmp
	c.LogDir = tmp
	// No TLS, no config file, no Ollama reachability — these are skipped.
	// Set deepseek provider to skip ollama check.
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true, got Ok=false with %d issues: %+v", len(report.Issues), report.Issues)
	}
}

func TestCheckRuntime_TLSCertNotFound(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.TLSCert = filepath.Join(tmp, "nonexistent.pem")
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when TLS cert file doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "TLSCert" && iss.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TLSCert error, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_TLSKeyNotFound(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.TLSKey = filepath.Join(tmp, "nonexistent.pem")
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when TLS key file doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "TLSKey" && iss.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TLSKey error, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_TLSFilesExist(t *testing.T) {
	tmp := t.TempDir()
	certPath := filepath.Join(tmp, "cert.pem")
	keyPath := filepath.Join(tmp, "key.pem")
	os.WriteFile(certPath, []byte("fake-cert"), 0644)
	os.WriteFile(keyPath, []byte("fake-key"), 0644)

	c := newDefaultConfig()
	c.TLSCert = certPath
	c.TLSKey = keyPath
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true when TLS files exist, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DirExists(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.ReflectionsDir = tmp
	c.AgentDefsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for valid directories, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DirIsFile(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "notadir.txt")
	os.WriteFile(filePath, []byte("data"), 0644)

	c := newDefaultConfig()
	c.ReflectionsDir = filePath

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when dir path is a file")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "ReflectionsDir" && iss.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ReflectionsDir error, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DirParentMissing(t *testing.T) {
	c := newDefaultConfig()
	c.ReflectionsDir = "/nonexistent/parent/subdir"

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when parent directory doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "ReflectionsDir" && iss.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ReflectionsDir warning for missing parent, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_ConfigFileNotFound(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.ConfigFile = filepath.Join(tmp, "nonexistent.json")
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when config file doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "ConfigFile" && iss.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ConfigFile warning, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_OllamaUnreachable(t *testing.T) {
	tmp := t.TempDir()
	// Override ollamaChecker to simulate unreachable host.
	oldChecker := ollamaChecker
	ollamaChecker = func(host string) bool { return false }
	defer func() { ollamaChecker = oldChecker }()

	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "http://localhost:11434"
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when Ollama is unreachable")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "OllamaHost" && iss.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected OllamaHost warning, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_OllamaReachable(t *testing.T) {
	tmp := t.TempDir()
	oldChecker := ollamaChecker
	ollamaChecker = func(host string) bool { return true }
	defer func() { ollamaChecker = oldChecker }()

	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "http://localhost:11434"
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true when Ollama is reachable, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DeepSeekNoOllamaCheck(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for deepseek provider (no Ollama check), got: %+v", report.Issues)
	}
	// Verify no OllamaHost issues.
	for _, iss := range report.Issues {
		if iss.Component == "OllamaHost" {
			t.Error("expected no OllamaHost check for deepseek provider")
		}
	}
}

func TestCheckRuntime_DeepSeekUnreachable(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"
	c.DeepSeekHost = "https://api.deepseek.com/v1"
	c.ReflectionsDir = tmp

	oldChecker := deepseekChecker
	deepseekChecker = func(host string) bool { return false }
	defer func() { deepseekChecker = oldChecker }()

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when DeepSeek is unreachable")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "DeepSeekHost" {
			found = true
			if iss.Severity != "warning" {
				t.Errorf("expected severity=warning, got %q", iss.Severity)
			}
		}
	}
	if !found {
		t.Error("expected DeepSeekHost warning in issues")
	}
}

func TestCheckRuntime_DeepSeekReachable(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"
	c.DeepSeekHost = "https://api.deepseek.com/v1"
	c.ReflectionsDir = tmp

	oldChecker := deepseekChecker
	deepseekChecker = func(host string) bool { return true }
	defer func() { deepseekChecker = oldChecker }()

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true when DeepSeek is reachable, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_OllamaNoDeepSeekCheck(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "" // empty host skips check
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	// Verify no DeepSeekHost issues since provider is ollama.
	for _, iss := range report.Issues {
		if iss.Component == "DeepSeekHost" {
			t.Error("expected no DeepSeekHost check for ollama provider")
		}
	}
}

func TestCheckRuntime_DeepSeekEmptyHost(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"
	c.DeepSeekHost = "" // empty host skips check
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for empty DeepSeekHost, got: %+v", report.Issues)
	}
	for _, iss := range report.Issues {
		if iss.Component == "DeepSeekHost" {
			t.Error("expected no DeepSeekHost check when host is empty")
		}
	}
}

func TestCheckRuntime_AllEmptyPaths(t *testing.T) {
	c := newDefaultConfig()
	// All persistence dirs empty, no TLS, no config file.
	c.ReflectionsDir = ""
	c.AgentDefsDir = ""
	c.HistoryDir = ""
	c.LogDir = ""
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for all-empty paths, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_MultipleIssues(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.TLSCert = filepath.Join(tmp, "missing.pem")
	c.TLSKey = filepath.Join(tmp, "also-missing.pem")
	c.ReflectionsDir = filepath.Join(tmp, "notadir.txt")
	os.WriteFile(c.ReflectionsDir, []byte("data"), 0644)
	c.AgentDefsDir = "/nonexistent/parent/subdir"
	c.ConfigFile = filepath.Join(tmp, "nosuch.json")

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false with multiple issues")
	}
	if len(report.Issues) < 4 {
		t.Errorf("expected at least 4 issues, got %d: %+v", len(report.Issues), report.Issues)
	}
}

func TestCheckRuntime_CreatedDir_Valid(t *testing.T) {
	// A newly-created temp dir should be valid.
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "subdir")
	os.Mkdir(subdir, 0755)

	c := newDefaultConfig()
	c.ReflectionsDir = subdir

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for newly created directory, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_TLSBothFilesOk(t *testing.T) {
	tmp := t.TempDir()
	certPath := filepath.Join(tmp, "cert.pem")
	keyPath := filepath.Join(tmp, "key.pem")
	os.WriteFile(certPath, []byte("cert"), 0644)
	os.WriteFile(keyPath, []byte("key"), 0644)

	c := newDefaultConfig()
	c.TLSCert = certPath
	c.TLSKey = keyPath
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true with both TLS files present, got: %+v", report.Issues)
	}
}

// ─── Diff tests ──────────────────────────────────────────────────────────────

func TestDiff_Identical(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	diffs := a.Diff(b)
	if len(diffs) != 0 {
		t.Errorf("expected no diffs for identical configs, got: %v", diffs)
	}
}

func TestDiff_NilOther(t *testing.T) {
	a := newDefaultConfig()
	diffs := a.Diff(nil)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for nil other, got %d", len(diffs))
	}
	if diffs[0] != "other config is nil" {
		t.Errorf("unexpected nil diff message: %q", diffs[0])
	}
}

func TestDiff_SingleFieldChanged(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.DashboardPort = 9090
	diffs := a.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d: %v", len(diffs), diffs)
	}
	expected := "DashboardPort: 9800 → 9090"
	if diffs[0] != expected {
		t.Errorf("expected %q, got %q", expected, diffs[0])
	}
}

func TestDiff_MultipleFieldsChanged(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.DashboardPort = 8080
	b.LLMTimeout = 120
	b.GardenerEnabled = false
	b.OllamaModel = "custom-model"

	diffs := a.Diff(b)
	if len(diffs) != 4 {
		t.Fatalf("expected 4 diffs, got %d: %v", len(diffs), diffs)
	}

	// Verify each expected diff is present (order doesn't matter)
	expectedSet := map[string]bool{
		"DashboardPort: 9800 → 8080":                  false,
		"LLMTimeout: 300s → 120s":                     false,
		"GardenerEnabled: true → false":               false,
		"OllamaModel: qwen3.6:35b-a3b → custom-model": false,
	}
	for _, d := range diffs {
		if _, ok := expectedSet[d]; ok {
			expectedSet[d] = true
		} else {
			t.Errorf("unexpected diff: %q", d)
		}
	}
	for k, found := range expectedSet {
		if !found {
			t.Errorf("missing expected diff: %q", k)
		}
	}
}

func TestDiff_SecretFields(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()

	// APIKey: originally empty, now set
	b.APIKey = "secret-key-123"
	diffs := a.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for APIKey set, got %d: %v", len(diffs), diffs)
	}
	if diffs[0] != "APIKey: set" {
		t.Errorf("expected 'APIKey: set', got %q", diffs[0])
	}

	// APIKey: changed (both non-empty but different)
	a2 := newDefaultConfig()
	a2.APIKey = "old-key"
	diffs = a2.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for APIKey changed, got %d: %v", len(diffs), diffs)
	}
	if diffs[0] != "APIKey: changed" {
		t.Errorf("expected 'APIKey: changed', got %q", diffs[0])
	}

	// APIKey: removed
	diffs = b.Diff(a)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for APIKey removed, got %d: %v", len(diffs), diffs)
	}
	if diffs[0] != "APIKey: removed" {
		t.Errorf("expected 'APIKey: removed', got %q", diffs[0])
	}
}

func TestDiff_DeepSeekKeyHandling(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()

	b.DeepSeekKey = "sk-abc123"
	diffs := a.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0] != "DeepSeekKey: set" {
		t.Errorf("expected 'DeepSeekKey: set', got %q", diffs[0])
	}

	// Changed
	a2 := newDefaultConfig()
	a2.DeepSeekKey = "sk-old"
	diffs = a2.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for changed, got %d", len(diffs))
	}
	if diffs[0] != "DeepSeekKey: changed" {
		t.Errorf("expected 'DeepSeekKey: changed', got %q", diffs[0])
	}

	// Removed
	diffs = b.Diff(a)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff for removed, got %d", len(diffs))
	}
	if diffs[0] != "DeepSeekKey: removed" {
		t.Errorf("expected 'DeepSeekKey: removed', got %q", diffs[0])
	}
}

func TestDiff_TLSFields(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()

	b.TLSCert = "/etc/ssl/cert.pem"
	b.TLSKey = "/etc/ssl/key.pem"

	diffs := a.Diff(b)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d: %v", len(diffs), diffs)
	}
	if diffs[0] != "TLSCert: set" {
		t.Errorf("expected 'TLSCert: set', got %q", diffs[0])
	}
	if diffs[1] != "TLSKey: set" {
		t.Errorf("expected 'TLSKey: set', got %q", diffs[1])
	}

	// TLS removed
	diffs = b.Diff(a)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs for removal, got %d", len(diffs))
	}
	if diffs[0] != "TLSCert: removed" {
		t.Errorf("expected 'TLSCert: removed', got %q", diffs[0])
	}
}

func TestDiff_AllFeatureFlags(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()

	// Flip all feature flags
	b.GardenerEnabled = false
	b.SchedulerEnabled = false
	b.AutoEvolveEnabled = true
	b.KanbanEnabled = false
	b.ThinktankEnabled = false
	b.StartupSimEnabled = false

	diffs := a.Diff(b)
	if len(diffs) != 6 {
		t.Fatalf("expected 6 diffs, got %d: %v", len(diffs), diffs)
	}
	expectedFlags := map[string]bool{
		"GardenerEnabled: true → false":   false,
		"SchedulerEnabled: true → false":  false,
		"AutoEvolveEnabled: false → true": false,
		"KanbanEnabled: true → false":     false,
		"ThinktankEnabled: true → false":  false,
		"StartupSimEnabled: true → false": false,
	}
	for _, d := range diffs {
		if _, ok := expectedFlags[d]; ok {
			expectedFlags[d] = true
		} else {
			t.Errorf("unexpected diff: %q", d)
		}
	}
	for k, found := range expectedFlags {
		if !found {
			t.Errorf("missing expected diff: %q", k)
		}
	}
}

func TestDiff_RateLimitFields(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.RateLimitRPS = 50.0
	b.RateLimitBurst = 10

	diffs := a.Diff(b)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}
	if diffs[0] != "RateLimitRPS: 100.0 → 50.0" {
		t.Errorf("expected RateLimitRPS diff, got %q", diffs[0])
	}
	if diffs[1] != "RateLimitBurst: 20 → 10" {
		t.Errorf("expected RateLimitBurst diff, got %q", diffs[1])
	}
}

func TestDiff_PersistenceDirs(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.ReflectionsDir = "/data/reflections"
	b.HistoryDir = "/data/history"

	diffs := a.Diff(b)
	if len(diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(diffs))
	}
	if diffs[0] != `ReflectionsDir: "" → "/data/reflections"` {
		t.Errorf("unexpected ReflectionsDir diff: %q", diffs[0])
	}
	if diffs[1] != `HistoryDir: "" → "/data/history"` {
		t.Errorf("unexpected HistoryDir diff: %q", diffs[1])
	}
}

func TestDiff_GardenerFields(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.GardenerCycleInterval = 600
	b.GardenerMutationsPer = 5
	b.GardenerMaxNodes = 50

	diffs := a.Diff(b)
	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d: %v", len(diffs), diffs)
	}
	if diffs[0] != "GardenerCycleInterval: 300s → 600s" {
		t.Errorf("unexpected diff: %q", diffs[0])
	}
	if diffs[1] != "GardenerMutationsPer: 2 → 5" {
		t.Errorf("unexpected diff: %q", diffs[1])
	}
	if diffs[2] != "GardenerMaxNodes: 20 → 50" {
		t.Errorf("unexpected diff: %q", diffs[2])
	}
}

func TestDiff_LLMProviderSwitch(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.LLMProvider = "deepseek"
	b.DeepSeekKey = "sk-test"
	b.OllamaHost = "" // deepseek provider doesn't use Ollama

	diffs := a.Diff(b)
	if len(diffs) != 3 {
		// LLMProvider changed + OllamaHost emptied + DeepSeekKey set
		t.Fatalf("expected 3 diffs, got %d: %v", len(diffs), diffs)
	}
	if diffs[0] != "LLMProvider: ollama → deepseek" {
		t.Errorf("expected LLMProvider diff, got %q", diffs[0])
	}
	// OllamaHost changed (emptied for deepseek)
	foundHost := false
	for _, d := range diffs {
		if d == "OllamaHost: http://localhost:11434 → " {
			foundHost = true
		}
	}
	if !foundHost {
		t.Errorf("expected OllamaHost diff not found in: %v", diffs)
	}
	foundKey := false
	for _, d := range diffs {
		if d == "DeepSeekKey: set" {
			foundKey = true
		}
	}
	if !foundKey {
		t.Errorf("expected DeepSeekKey: set not found in: %v", diffs)
	}
}

func TestDiff_AllFieldsChangedAtOnce(t *testing.T) {
	a := newDefaultConfig()
	b := &Config{
		DashboardPort:          9090,
		APIKey:                 "new-key",
		TLSCert:                "/new/cert.pem",
		TLSKey:                 "/new/key.pem",
		LLMProvider:            "deepseek",
		OllamaHost:             "",
		OllamaModel:            "",
		DeepSeekHost:           "https://custom.deepseek.com/v1",
		DeepSeekModel:          "deepseek-v4-pro",
		DeepSeekKey:            "sk-new",
		LLMTimeout:             600,
		RateLimitRPS:           200,
		RateLimitBurst:         50,
		GardenerEnabled:        false,
		SchedulerEnabled:       false,
		AutoEvolveEnabled:      true,
		KanbanEnabled:          false,
		ThinktankEnabled:       false,
		StartupSimEnabled:      false,
		ReflectionsDir:         "/data/r",
		AgentDefsDir:           "/data/a",
		HistoryDir:             "/data/h",
		LogDir:                 "/data/l",
		GardenerCycleInterval:  600,
		GardenerMutationsPer:   5,
		GardenerMaxNodes:       50,
		SchedulerCheckInterval: 120,
		MaxBodySize:            2097152,
	}

	diffs := a.Diff(b)
	// 30 config fields total (all changed from defaults)
	// DashboardPort, APIKey, TLSCert, TLSKey, LLMProvider, OllamaHost,
	// OllamaModel, DeepSeekHost, DeepSeekModel, DeepSeekKey, LLMTimeout,
	// RateLimitRPS, RateLimitBurst, GardenerEnabled, SchedulerEnabled,
	// AutoEvolveEnabled, KanbanEnabled, ThinktankEnabled, StartupSimEnabled,
	// ReflectionsDir, AgentDefsDir, HistoryDir, LogDir,
	// GardenerCycleInterval, GardenerMutationsPer, GardenerMaxNodes,
	// SchedulerCheckInterval, MaxBodySize = 28 fields
	if len(diffs) < 28 {
		t.Errorf("expected at least 28 diffs, got %d", len(diffs))
	}

	// Reverse: b → a should also report all changes
	reverseDiffs := b.Diff(a)
	if len(reverseDiffs) < 28 {
		t.Errorf("expected at least 28 reverse diffs, got %d", len(reverseDiffs))
	}
}

func TestDiff_EmptyDirsNotDiffed(t *testing.T) {
	// Two configs with the same empty dir strings should not produce diffs
	a := newDefaultConfig()
	b := newDefaultConfig()

	// Both have empty string for all persistence dirs
	diffs := a.Diff(b)
	for _, d := range diffs {
		if d == `ReflectionsDir: "" → ""` {
			t.Error("should not diff identical empty strings")
		}
	}
}

func TestDiff_SchedulerCheckInterval(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.SchedulerCheckInterval = 30

	diffs := a.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0] != "SchedulerCheckInterval: 60s → 30s" {
		t.Errorf("unexpected diff: %q", diffs[0])
	}
}

func TestDiff_MaxBodySize(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.MaxBodySize = 5242880

	diffs := a.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0] != "MaxBodySize: 1048576 → 5242880" {
		t.Errorf("unexpected diff: %q", diffs[0])
	}
}

func TestDiff_UnchangedFieldsNotIncluded(t *testing.T) {
	// Only change one field, verify all other fields are not reported
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.OllamaModel = "new-model"

	diffs := a.Diff(b)
	if len(diffs) != 1 {
		t.Fatalf("expected exactly 1 diff, got %d: %v", len(diffs), diffs)
	}

	// Check that DashboardPort (unchanged) is not in diffs
	for _, d := range diffs {
		if d == "DashboardPort: 9800 → 9800" {
			t.Error("unchanged DashboardPort should not be in diffs")
		}
	}
}

func TestDiff_ConfigFileFieldIgnored(t *testing.T) {
	// ConfigFile is tagged json:"-" and should not be diffed
	a := newDefaultConfig()
	a.ConfigFile = "/etc/bt/config.json"
	b := newDefaultConfig()
	b.ConfigFile = "/etc/bt/other.json"

	diffs := a.Diff(b)
	if len(diffs) != 0 {
		t.Errorf("ConfigFile should not be diffed (json:\"-\"), got: %v", diffs)
	}
}

// ─── Duration Helper Tests ─────────────────────────────────────────────────

func TestDuration(t *testing.T) {
	if d := Duration(5); d != 5*time.Second {
		t.Errorf("Duration(5) = %v, want 5s", d)
	}
	if d := Duration(0); d != 0 {
		t.Errorf("Duration(0) = %v, want 0s", d)
	}
	if d := Duration(3600); d != time.Hour {
		t.Errorf("Duration(3600) = %v, want 1h0m0s", d)
	}
}

func TestConfig_RetryBaseDuration(t *testing.T) {
	c := newDefaultConfig()
	c.RetryBaseDelayMs = 1000
	if d := c.RetryBaseDuration(); d != time.Second {
		t.Errorf("RetryBaseDuration() = %v, want 1s", d)
	}
	c.RetryBaseDelayMs = 0
	if d := c.RetryBaseDuration(); d != 0 {
		t.Errorf("RetryBaseDuration() with 0ms = %v, want 0s", d)
	}
}

func TestConfig_RetryMaxDuration(t *testing.T) {
	c := newDefaultConfig()
	c.RetryMaxDelayMs = 30000
	if d := c.RetryMaxDuration(); d != 30*time.Second {
		t.Errorf("RetryMaxDuration() = %v, want 30s", d)
	}
	c.RetryMaxDelayMs = 0
	if d := c.RetryMaxDuration(); d != 0 {
		t.Errorf("RetryMaxDuration() with 0ms = %v, want 0s", d)
	}
}

func TestConfig_RetryLLMBaseDuration(t *testing.T) {
	c := newDefaultConfig()
	c.RetryLLMBaseMs = 5000
	if d := c.RetryLLMBaseDuration(); d != 5*time.Second {
		t.Errorf("RetryLLMBaseDuration() = %v, want 5s", d)
	}
}

func TestConfig_CBCooldownDuration(t *testing.T) {
	c := newDefaultConfig()
	c.CBCooldownSecs = 60
	if d := c.CBCooldownDuration(); d != time.Minute {
		t.Errorf("CBCooldownDuration() = %v, want 1m0s", d)
	}
	c.CBCooldownSecs = 0
	if d := c.CBCooldownDuration(); d != 0 {
		t.Errorf("CBCooldownDuration() with 0s = %v, want 0s", d)
	}
}

func TestConfig_DLQMaxEntriesLimit(t *testing.T) {
	c := newDefaultConfig()
	c.DLQMaxEntries = 100
	if n := c.DLQMaxEntriesLimit(); n != 100 {
		t.Errorf("DLQMaxEntriesLimit() = %d, want 100", n)
	}
	// Zero means unlimited
	c.DLQMaxEntries = 0
	if n := c.DLQMaxEntriesLimit(); n != 0 {
		t.Errorf("DLQMaxEntriesLimit(0) = %d, want 0", n)
	}
	// Negative means unlimited
	c.DLQMaxEntries = -1
	if n := c.DLQMaxEntriesLimit(); n != 0 {
		t.Errorf("DLQMaxEntriesLimit(-1) = %d, want 0", n)
	}
}

func TestConfig_ResolvePaths(t *testing.T) {
	// Set a specific home for deterministic test
	t.Setenv("BT_HOME", "/tmp/test-bt-home")
	c := newDefaultConfig()
	c.ResolvePaths()

	if c.Paths.HomeDir != "/tmp/test-bt-home" {
		t.Errorf("HomeDir = %q, want /tmp/test-bt-home", c.Paths.HomeDir)
	}
	if c.Paths.ConfigFile != "/tmp/test-bt-home/config.yaml" {
		t.Errorf("ConfigFile = %q, want /tmp/test-bt-home/config.yaml", c.Paths.ConfigFile)
	}
	if c.Paths.DBFile != "/tmp/test-bt-home/agents.db" {
		t.Errorf("DBFile = %q, want /tmp/test-bt-home/agents.db", c.Paths.DBFile)
	}
	if c.Paths.ReflectionsDir != "/tmp/test-bt-home/reflections" {
		t.Errorf("ReflectionsDir = %q, want /tmp/test-bt-home/reflections", c.Paths.ReflectionsDir)
	}
	if c.Paths.HistoryDir != "/tmp/test-bt-home/history" {
		t.Errorf("HistoryDir = %q, want /tmp/test-bt-home/history", c.Paths.HistoryDir)
	}
	if c.Paths.LogDir != "/tmp/test-bt-home/logs" {
		t.Errorf("LogDir = %q, want /tmp/test-bt-home/logs", c.Paths.LogDir)
	}
}

func TestConfig_ResolvePaths_WithOverrides(t *testing.T) {
	t.Setenv("BT_HOME", "/tmp/bt-home")
	c := newDefaultConfig()
	c.ReflectionsDir = "/custom/reflections"
	c.HistoryDir = "/custom/history"
	c.LogDir = "/custom/logs"
	c.ConfigFile = "/custom/config.json"
	c.ResolvePaths()

	if c.Paths.HomeDir != "/tmp/bt-home" {
		t.Errorf("HomeDir = %q, want /tmp/bt-home", c.Paths.HomeDir)
	}
	if c.Paths.ConfigFile != "/custom/config.json" {
		t.Errorf("ConfigFile = %q, want /custom/config.json", c.Paths.ConfigFile)
	}
	if c.Paths.ReflectionsDir != "/custom/reflections" {
		t.Errorf("ReflectionsDir = %q, want /custom/reflections", c.Paths.ReflectionsDir)
	}
	if c.Paths.HistoryDir != "/custom/history" {
		t.Errorf("HistoryDir = %q, want /custom/history", c.Paths.HistoryDir)
	}
	if c.Paths.LogDir != "/custom/logs" {
		t.Errorf("LogDir = %q, want /custom/logs", c.Paths.LogDir)
	}
}

// ─── applyEnvOverrides Coverage Tests ──────────────────────────────────────

func TestEnvOverride_RateLimiting(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_RATE_LIMIT_RPS", "10.5")
	os.Setenv("BT_RATE_LIMIT_BURST", "20")
	defer func() {
		os.Unsetenv("BT_RATE_LIMIT_RPS")
		os.Unsetenv("BT_RATE_LIMIT_BURST")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.RateLimitRPS != 10.5 {
		t.Errorf("RateLimitRPS = %f, want 10.5", c.RateLimitRPS)
	}
	if c.RateLimitBurst != 20 {
		t.Errorf("RateLimitBurst = %d, want 20", c.RateLimitBurst)
	}
}

func TestEnvOverride_GardenerParams(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_GARDENER_CYCLE", "300")
	os.Setenv("BT_GARDENER_MUTATIONS", "5")
	os.Setenv("BT_GARDENER_MAX_NODES", "50")
	defer func() {
		os.Unsetenv("BT_GARDENER_CYCLE")
		os.Unsetenv("BT_GARDENER_MUTATIONS")
		os.Unsetenv("BT_GARDENER_MAX_NODES")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.GardenerCycleInterval != 300 {
		t.Errorf("GardenerCycleInterval = %d, want 300", c.GardenerCycleInterval)
	}
	if c.GardenerMutationsPer != 5 {
		t.Errorf("GardenerMutationsPer = %d, want 5", c.GardenerMutationsPer)
	}
	if c.GardenerMaxNodes != 50 {
		t.Errorf("GardenerMaxNodes = %d, want 50", c.GardenerMaxNodes)
	}
}

func TestEnvOverride_Scheduler(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_SCHEDULER_INTERVAL", "120")
	defer os.Unsetenv("BT_SCHEDULER_INTERVAL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.SchedulerCheckInterval != 120 {
		t.Errorf("SchedulerCheckInterval = %d, want 120", c.SchedulerCheckInterval)
	}
}

func TestEnvOverride_ErrorHandling(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_RETRY_MAX_RETRIES", "3")
	os.Setenv("BT_RETRY_BASE_DELAY_MS", "500")
	os.Setenv("BT_RETRY_MAX_DELAY_MS", "5000")
	os.Setenv("BT_RETRY_LLM_BASE_MS", "2000")
	os.Setenv("BT_RETRY_JITTER", "full_jitter")
	os.Setenv("BT_RETRY_UNKNOWN", "true")
	os.Setenv("BT_CB_THRESHOLD", "5")
	os.Setenv("BT_CB_COOLDOWN_SECS", "60")
	os.Setenv("BT_DLQ_MAX_ENTRIES", "1000")
	defer func() {
		os.Unsetenv("BT_RETRY_MAX_RETRIES")
		os.Unsetenv("BT_RETRY_BASE_DELAY_MS")
		os.Unsetenv("BT_RETRY_MAX_DELAY_MS")
		os.Unsetenv("BT_RETRY_LLM_BASE_MS")
		os.Unsetenv("BT_RETRY_JITTER")
		os.Unsetenv("BT_RETRY_UNKNOWN")
		os.Unsetenv("BT_CB_THRESHOLD")
		os.Unsetenv("BT_CB_COOLDOWN_SECS")
		os.Unsetenv("BT_DLQ_MAX_ENTRIES")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.RetryMaxRetries != 3 {
		t.Errorf("RetryMaxRetries = %d, want 3", c.RetryMaxRetries)
	}
	if c.RetryBaseDelayMs != 500 {
		t.Errorf("RetryBaseDelayMs = %d, want 500", c.RetryBaseDelayMs)
	}
	if c.RetryMaxDelayMs != 5000 {
		t.Errorf("RetryMaxDelayMs = %d, want 5000", c.RetryMaxDelayMs)
	}
	if c.RetryLLMBaseMs != 2000 {
		t.Errorf("RetryLLMBaseMs = %d, want 2000", c.RetryLLMBaseMs)
	}
	if c.RetryJitter != "full_jitter" {
		t.Errorf("RetryJitter = %q, want full_jitter", c.RetryJitter)
	}
	if !c.RetryUnknown {
		t.Error("RetryUnknown = false, want true")
	}
	if c.CBThreshold != 5 {
		t.Errorf("CBThreshold = %d, want 5", c.CBThreshold)
	}
	if c.CBCooldownSecs != 60 {
		t.Errorf("CBCooldownSecs = %d, want 60", c.CBCooldownSecs)
	}
	if c.DLQMaxEntries != 1000 {
		t.Errorf("DLQMaxEntries = %d, want 1000", c.DLQMaxEntries)
	}
}

func TestEnvOverride_DeepSeek(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_DEEPSEEK_HOST", "https://api.deepseek.com")
	os.Setenv("BT_DEEPSEEK_MODEL", "deepseek-v4-pro")
	os.Setenv("BT_DEEPSEEK_KEY", "sk-test-key")
	defer func() {
		os.Unsetenv("BT_DEEPSEEK_HOST")
		os.Unsetenv("BT_DEEPSEEK_MODEL")
		os.Unsetenv("BT_DEEPSEEK_KEY")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.DeepSeekHost != "https://api.deepseek.com" {
		t.Errorf("DeepSeekHost = %q, want https://api.deepseek.com", c.DeepSeekHost)
	}
	if c.DeepSeekModel != "deepseek-v4-pro" {
		t.Errorf("DeepSeekModel = %q, want deepseek-v4-pro", c.DeepSeekModel)
	}
	if c.DeepSeekKey != "sk-test-key" {
		t.Errorf("DeepSeekKey = %q, want sk-test-key", c.DeepSeekKey)
	}
}

func TestEnvOverride_DeepSeekKeyFallback(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	// Only set DEEPSEEK_API_KEY (the fallback), NOT BT_DEEPSEEK_KEY
	os.Unsetenv("BT_DEEPSEEK_KEY")
	os.Setenv("DEEPSEEK_API_KEY", "sk-fallback-key")
	defer os.Unsetenv("DEEPSEEK_API_KEY")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.DeepSeekKey != "sk-fallback-key" {
		t.Errorf("DeepSeekKey = %q, want sk-fallback-key (from fallback)", c.DeepSeekKey)
	}
}

func TestEnvOverride_ACP(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_ACP_COMMAND", "acp")
	os.Setenv("BT_ACP_ARGS", "--verbose")
	os.Setenv("BT_ACP_CWD", "/tmp/acp-workdir")
	defer func() {
		os.Unsetenv("BT_ACP_COMMAND")
		os.Unsetenv("BT_ACP_ARGS")
		os.Unsetenv("BT_ACP_CWD")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.ACPCommand != "acp" {
		t.Errorf("ACPCommand = %q, want acp", c.ACPCommand)
	}
	if c.ACPArgs != "--verbose" {
		t.Errorf("ACPArgs = %q, want --verbose", c.ACPArgs)
	}
	if c.ACPCwd != "/tmp/acp-workdir" {
		t.Errorf("ACPCwd = %q, want /tmp/acp-workdir", c.ACPCwd)
	}
}

func TestEnvOverride_LLMProvider(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_LLM_PROVIDER", "deepseek")
	os.Setenv("BT_DEEPSEEK_KEY", "sk-test-key-for-validation")
	defer func() {
		os.Unsetenv("BT_LLM_PROVIDER")
		os.Unsetenv("BT_DEEPSEEK_KEY")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.LLMProvider != "deepseek" {
		t.Errorf("LLMProvider = %q, want deepseek", c.LLMProvider)
	}
	if c.DeepSeekKey != "sk-test-key-for-validation" {
		t.Errorf("DeepSeekKey = %q, want sk-test-key-for-validation", c.DeepSeekKey)
	}
}

func TestEnvOverride_PersistenceDirs(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_REFLECTIONS_DIR", "/custom/reflections")
	os.Setenv("BT_AGENT_DEFS_DIR", "/custom/agents")
	os.Setenv("BT_HISTORY_DIR", "/custom/history")
	os.Setenv("BT_LOG_DIR", "/custom/logs")
	defer func() {
		os.Unsetenv("BT_REFLECTIONS_DIR")
		os.Unsetenv("BT_AGENT_DEFS_DIR")
		os.Unsetenv("BT_HISTORY_DIR")
		os.Unsetenv("BT_LOG_DIR")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.ReflectionsDir != "/custom/reflections" {
		t.Errorf("ReflectionsDir = %q, want /custom/reflections", c.ReflectionsDir)
	}
	if c.AgentDefsDir != "/custom/agents" {
		t.Errorf("AgentDefsDir = %q, want /custom/agents", c.AgentDefsDir)
	}
	if c.HistoryDir != "/custom/history" {
		t.Errorf("HistoryDir = %q, want /custom/history", c.HistoryDir)
	}
	if c.LogDir != "/custom/logs" {
		t.Errorf("LogDir = %q, want /custom/logs", c.LogDir)
	}
}

func TestEnvOverride_FeatureFlags(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_FEATURE_SCHEDULER", "true")
	os.Setenv("BT_FEATURE_KANBAN", "true")
	os.Setenv("BT_FEATURE_STARTUP_SIM", "false")
	defer func() {
		os.Unsetenv("BT_FEATURE_SCHEDULER")
		os.Unsetenv("BT_FEATURE_KANBAN")
		os.Unsetenv("BT_FEATURE_STARTUP_SIM")
	}()
	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if !c.SchedulerEnabled {
		t.Error("SchedulerEnabled = false, want true")
	}
	if !c.KanbanEnabled {
		t.Error("KanbanEnabled = false, want true")
	}
	if c.StartupSimEnabled {
		t.Error("StartupSimEnabled = true, want false")
	}
}

func TestEnvOverride_MaxBodySize(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_MAX_BODY_SIZE", "1048576")
	defer os.Unsetenv("BT_MAX_BODY_SIZE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.MaxBodySize != 1048576 {
		t.Errorf("MaxBodySize = %d, want 1048576", c.MaxBodySize)
	}
}

func TestEnvOverride_APIKey(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_API_KEY", "test-api-key-123")
	defer os.Unsetenv("BT_API_KEY")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if c.APIKey != "test-api-key-123" {
		t.Errorf("APIKey = %q, want test-api-key-123", c.APIKey)
	}
}

func TestParseBool_AllValues(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"yes", true},
		{"Yes", true},
		{"on", true},
		{"ON", true},
		{"0", false},
		{"false", false},
		{"False", false},
		{"off", false},
		{"random", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseBool(tt.input)
			if got != tt.want {
				t.Errorf("parseBool(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSaveFile_ErrorPath(t *testing.T) {
	c := newDefaultConfig()
	err := c.SaveFile("/nonexistent/deep/path/config.json")
	if err == nil {
		t.Error("expected error writing to nonexistent directory")
	}
}

func TestValidationError_Error(t *testing.T) {
	e := &ValidationError{Field: "DashboardPort", Message: "must be between 1 and 65535"}
	got := e.Error()
	want := "config validation: DashboardPort: must be between 1 and 65535"
	if got != want {
		t.Errorf("ValidationError.Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	e := ValidationErrors{
		{Field: "DashboardPort", Message: "must be between 1 and 65535"},
		{Field: "LLMTimeout", Message: "must be between 1 and 3600 seconds"},
	}
	got := e.Error()
	want := "config validation: DashboardPort: must be between 1 and 65535; config validation: LLMTimeout: must be between 1 and 3600 seconds"
	if got != want {
		t.Errorf("ValidationErrors.Error() = %q, want %q", got, want)
	}
}

func TestValidationErrors_Empty(t *testing.T) {
	e := ValidationErrors{}
	got := e.Error()
	if got != "" {
		t.Errorf("empty ValidationErrors.Error() = %q, want empty string", got)
	}
}

// ─── applyDotEnvToConfig Coverage Tests ─────────────────────────────────

func TestLoad_DotEnvAlreadySetByEnvVar_Skip(t *testing.T) {
	// When BOTH the .env file AND the env var specify a value,
	// the env var has precedence and applyDotEnvToConfig should
	// skip the .env value via the early return in each helper closure.
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	// .env says 3333 for DashboardPort and "custom-model" for OllamaModel
	content := "BT_DASHBOARD_PORT=3333\nBT_OLLAMA_MODEL=custom-model\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set env vars that MATCH the .env keys — these should win
	os.Setenv("BT_DOTENV_FILE", envFile)
	os.Setenv("BT_DASHBOARD_PORT", "9999")
	os.Setenv("BT_OLLAMA_MODEL", "env-wins-model")
	defer func() {
		os.Unsetenv("BT_DOTENV_FILE")
		os.Unsetenv("BT_DASHBOARD_PORT")
		os.Unsetenv("BT_OLLAMA_MODEL")
	}()
	os.Unsetenv("BT_CONFIG_FILE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Env vars must override .env values
	if c.DashboardPort != 9999 {
		t.Errorf("expected DashboardPort=9999 (env var), got %d (from .env=%s)", c.DashboardPort, "3333")
	}
	if c.OllamaModel != "env-wins-model" {
		t.Errorf("expected OllamaModel=env-wins-model (env var), got %s", c.OllamaModel)
	}
}

func TestLoad_DotEnvInvalidInt_SilentlyIgnored(t *testing.T) {
	// When .env contains a non-numeric value for an int field,
	// applyDotEnvInt's strconv.Atoi returns an error and the
	// value is silently ignored (field keeps its default).
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	// BT_LLM_TIMEOUT should be an integer — providing "not-a-number"
	content := "BT_LLM_TIMEOUT=not-a-number\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_LLM_TIMEOUT")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Should fall back to default (300) since the strconv.Atoi failed
	if c.LLMTimeout != 300 {
		t.Errorf("expected LLMTimeout=300 (default, invalid .env ignored), got %d", c.LLMTimeout)
	}
}

func TestLoad_DotEnvInvalidFloat_SilentlyIgnored(t *testing.T) {
	// Same as above but for Float fields (RateLimitRPS).
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	content := "BT_RATE_LIMIT_RPS=not-a-float\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_RATE_LIMIT_RPS")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Default is 100.0
	if c.RateLimitRPS != 100.0 {
		t.Errorf("expected RateLimitRPS=100.0 (default, invalid .env ignored), got %v", c.RateLimitRPS)
	}
}

func TestLoadFileWithDotEnv_LoadError(t *testing.T) {
	// LoadFileWithDotEnv: when LoadDotEnv returns a non-not-exist error,
	// it should log a warning but NOT fail.
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	cfgContent := `{"dashboard_port": 4444}`
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a directory at the .env path so os.ReadFile fails
	dotenvPath := filepath.Join(dir, ".env")
	if err := os.Mkdir(dotenvPath, 0755); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("BT_CONFIG_FILE")

	c, err := LoadFileWithDotEnv(cfgFile, dotenvPath)
	if err != nil {
		t.Fatalf("LoadFileWithDotEnv() should not fail on bad .env: %v", err)
	}

	if c.DashboardPort != 4444 {
		t.Errorf("expected DashboardPort=4444 from config file, got %d", c.DashboardPort)
	}
}

func TestLoadFileWithDotEnv_NotExistIgnored(t *testing.T) {
	// LoadFileWithDotEnv: LoadDotEnv returns not-exist error →
	// silently ignored (no warning, no failure).
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	cfgContent := `{"dashboard_port": 5555}`
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Non-existent .env file
	dotenvPath := filepath.Join(dir, ".env.nonexistent")

	os.Unsetenv("BT_CONFIG_FILE")

	c, err := LoadFileWithDotEnv(cfgFile, dotenvPath)
	if err != nil {
		t.Fatalf("LoadFileWithDotEnv() should not fail on missing .env: %v", err)
	}

	if c.DashboardPort != 5555 {
		t.Errorf("expected DashboardPort=5555 from config file, got %d", c.DashboardPort)
	}
}

func TestLoad_DotEnvFileLoadError_LogsWarning(t *testing.T) {
	// applyDotEnvFiles: when LoadDotEnv returns an error (e.g., path is
	// a directory, not a file), log a warning and continue without failing.
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	// Create a directory with the same path so os.ReadFile fails
	if err := os.Mkdir(envFile, 0755); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() should not fail on bad .env file: %v", err)
	}

	// Config should still have defaults
	if c.DashboardPort != 9800 {
		t.Errorf("expected DashboardPort=9800 (default, bad .env ignored), got %d", c.DashboardPort)
	}
}

func TestLoad_DotEnvIntFloatBoolAlreadySetByEnvVar_Skip(t *testing.T) {
	// Tests that the early-return paths in applyDotEnvInt, applyDotEnvFloat,
	// and applyDotEnvBool closures are exercised when the corresponding env
	// var is already set.
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	// .env provides values for int, float, and bool fields
	content := "BT_LLM_TIMEOUT=500\nBT_RATE_LIMIT_RPS=50\nBT_FEATURE_GARDENER=true\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set env vars for the same fields — these should win (early return)
	os.Setenv("BT_DOTENV_FILE", envFile)
	os.Setenv("BT_LLM_TIMEOUT", "999")
	os.Setenv("BT_RATE_LIMIT_RPS", "999.0")
	os.Setenv("BT_FEATURE_GARDENER", "false")
	defer func() {
		os.Unsetenv("BT_DOTENV_FILE")
		os.Unsetenv("BT_LLM_TIMEOUT")
		os.Unsetenv("BT_RATE_LIMIT_RPS")
		os.Unsetenv("BT_FEATURE_GARDENER")
	}()
	os.Unsetenv("BT_CONFIG_FILE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Env vars must override .env values
	if c.LLMTimeout != 999 {
		t.Errorf("expected LLMTimeout=999 (env var), got %d (from .env=%s)", c.LLMTimeout, "500")
	}
	if c.RateLimitRPS != 999.0 {
		t.Errorf("expected RateLimitRPS=999.0 (env var), got %v", c.RateLimitRPS)
	}
	if c.GardenerEnabled {
		t.Errorf("expected GardenerEnabled=false (env var override), got true")
	}
}

func TestLoad_DotEnvSetterClosures_AllTypesExercise(t *testing.T) {
	// Exercises ALL setter closures in applyDotEnvToConfig for string, int,
	// float, and bool fields simultaneously. No env vars are set for any of
	// these fields, so the closures fire and propagate .env values to config.
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")

	// Build a comprehensive .env file covering every field type
	var lines []string

	// String fields (applyDotEnvStr)
	lines = append(lines, "BT_API_KEY=test-api-key")
	lines = append(lines, "BT_TLS_CERT=/path/to/cert.pem")
	lines = append(lines, "BT_TLS_KEY=/path/to/key.pem")
	lines = append(lines, "BT_LLM_PROVIDER=deepseek")
	lines = append(lines, "OLLAMA_HOST=http://ollama-test:11434")
	lines = append(lines, "BT_OLLAMA_MODEL=test-model")
	lines = append(lines, "BT_DEEPSEEK_HOST=https://api.test.com/v1")
	lines = append(lines, "BT_DEEPSEEK_MODEL=test-deepseek-model")
	lines = append(lines, "BT_DEEPSEEK_KEY=sk-test-deepseek-key")
	lines = append(lines, "DEEPSEEK_API_KEY=sk-hermes-deepseek-key")
	lines = append(lines, "BT_ACP_COMMAND=test-cmd")
	lines = append(lines, "BT_ACP_ARGS=--test-flag")
	lines = append(lines, "BT_ACP_CWD=/tmp/test-cwd")
	lines = append(lines, "BT_FALLBACK_MODELS=deepseek:test,ollama:test2")
	lines = append(lines, "BT_CORS_DASHBOARD_ORIGIN=https://test.example.com")
	lines = append(lines, "BT_REFLECTIONS_DIR=/tmp/reflections")
	lines = append(lines, "BT_AGENT_DEFS_DIR=/tmp/agents")
	lines = append(lines, "BT_HISTORY_DIR=/tmp/history")
	lines = append(lines, "BT_LOG_DIR=/tmp/logs")
	lines = append(lines, "BT_RETRY_JITTER=decorrelated_jitter")

	// Int fields (applyDotEnvInt)
	lines = append(lines, "BT_DASHBOARD_PORT=7777")
	lines = append(lines, "BT_LLM_TIMEOUT=123")
	lines = append(lines, "BT_RATE_LIMIT_BURST=15")
	lines = append(lines, "BT_GARDENER_CYCLE=600")
	lines = append(lines, "BT_GARDENER_MUTATIONS=3")
	lines = append(lines, "BT_GARDENER_MAX_NODES=50")
	lines = append(lines, "BT_SCHEDULER_INTERVAL=120")
	lines = append(lines, "BT_RETRY_MAX_RETRIES=5")
	lines = append(lines, "BT_RETRY_BASE_DELAY_MS=2000")
	lines = append(lines, "BT_RETRY_MAX_DELAY_MS=60000")
	lines = append(lines, "BT_RETRY_LLM_BASE_MS=5000")
	lines = append(lines, "BT_CB_THRESHOLD=10")
	lines = append(lines, "BT_CB_COOLDOWN_SECS=600")
	lines = append(lines, "BT_DLQ_MAX_ENTRIES=500")
	lines = append(lines, "BT_MAX_BODY_SIZE=2097152")

	// Float fields (applyDotEnvFloat)
	lines = append(lines, "BT_RATE_LIMIT_RPS=55.5")

	// Bool fields (applyDotEnvBool)
	lines = append(lines, "BT_FEATURE_GARDENER=false")
	lines = append(lines, "BT_FEATURE_SCHEDULER=false")
	lines = append(lines, "BT_FEATURE_AUTO_EVOLVE=true")
	lines = append(lines, "BT_FEATURE_KANBAN=false")
	lines = append(lines, "BT_FEATURE_THINKTANK=false")
	lines = append(lines, "BT_FEATURE_STARTUP_SIM=false")
	lines = append(lines, "BT_API_ENFORCE_RESPONSE_VALIDATION=true")
	lines = append(lines, "BT_RETRY_UNKNOWN=true")

	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// No env vars set for any of these fields
	dotEnvKeys := []string{
		"BT_DASHBOARD_PORT", "BT_API_KEY", "BT_TLS_CERT", "BT_TLS_KEY",
		"BT_LLM_PROVIDER", "OLLAMA_HOST", "BT_OLLAMA_MODEL",
		"BT_DEEPSEEK_HOST", "BT_DEEPSEEK_MODEL", "BT_DEEPSEEK_KEY",
		"DEEPSEEK_API_KEY", "BT_ACP_COMMAND", "BT_ACP_ARGS", "BT_ACP_CWD",
		"BT_FALLBACK_MODELS", "BT_CORS_DASHBOARD_ORIGIN",
		"BT_RATE_LIMIT_RPS", "BT_RATE_LIMIT_BURST",
		"BT_FEATURE_GARDENER", "BT_FEATURE_SCHEDULER", "BT_FEATURE_AUTO_EVOLVE",
		"BT_FEATURE_KANBAN", "BT_FEATURE_THINKTANK", "BT_FEATURE_STARTUP_SIM",
		"BT_API_ENFORCE_RESPONSE_VALIDATION",
		"BT_REFLECTIONS_DIR", "BT_AGENT_DEFS_DIR", "BT_HISTORY_DIR", "BT_LOG_DIR",
		"BT_GARDENER_CYCLE", "BT_GARDENER_MUTATIONS", "BT_GARDENER_MAX_NODES",
		"BT_SCHEDULER_INTERVAL",
		"BT_RETRY_MAX_RETRIES", "BT_RETRY_BASE_DELAY_MS", "BT_RETRY_MAX_DELAY_MS",
		"BT_RETRY_LLM_BASE_MS", "BT_RETRY_JITTER", "BT_RETRY_UNKNOWN",
		"BT_CB_THRESHOLD", "BT_CB_COOLDOWN_SECS", "BT_DLQ_MAX_ENTRIES",
		"BT_MAX_BODY_SIZE", "BT_LLM_TIMEOUT",
	}
	for _, k := range dotEnvKeys {
		os.Unsetenv(k)
	}
	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Verify string fields
	if c.APIKey != "test-api-key" {
		t.Errorf("APIKey: expected test-api-key, got %q", c.APIKey)
	}
	if c.TLSCert != "/path/to/cert.pem" {
		t.Errorf("TLSCert: expected /path/to/cert.pem, got %q", c.TLSCert)
	}
	if c.TLSKey != "/path/to/key.pem" {
		t.Errorf("TLSKey: expected /path/to/key.pem, got %q", c.TLSKey)
	}
	if c.LLMProvider != "deepseek" {
		t.Errorf("LLMProvider: expected deepseek, got %q", c.LLMProvider)
	}
	if c.OllamaHost != "http://ollama-test:11434" {
		t.Errorf("OllamaHost: expected http://ollama-test:11434, got %q", c.OllamaHost)
	}
	if c.OllamaModel != "test-model" {
		t.Errorf("OllamaModel: expected test-model, got %q", c.OllamaModel)
	}
	if c.DeepSeekHost != "https://api.test.com/v1" {
		t.Errorf("DeepSeekHost: expected https://api.test.com/v1, got %q", c.DeepSeekHost)
	}
	if c.DeepSeekModel != "test-deepseek-model" {
		t.Errorf("DeepSeekModel: expected test-deepseek-model, got %q", c.DeepSeekModel)
	}
	// DEEPSEEK_API_KEY should take precedence since it's listed second
	if c.DeepSeekKey != "sk-hermes-deepseek-key" {
		t.Errorf("DeepSeekKey: expected sk-hermes-deepseek-key (DEEPSEEK_API_KEY wins, listed second), got %q", c.DeepSeekKey)
	}
	if c.ACPCommand != "test-cmd" {
		t.Errorf("ACPCommand: expected test-cmd, got %q", c.ACPCommand)
	}
	if c.ACPArgs != "--test-flag" {
		t.Errorf("ACPArgs: expected --test-flag, got %q", c.ACPArgs)
	}
	if c.ACPCwd != "/tmp/test-cwd" {
		t.Errorf("ACPCwd: expected /tmp/test-cwd, got %q", c.ACPCwd)
	}
	if c.FallbackModels != "deepseek:test,ollama:test2" {
		t.Errorf("FallbackModels: expected deepseek:test,ollama:test2, got %q", c.FallbackModels)
	}
	if c.CORSDashboardOrigin != "https://test.example.com" {
		t.Errorf("CORSDashboardOrigin: expected https://test.example.com, got %q", c.CORSDashboardOrigin)
	}
	if c.ReflectionsDir != "/tmp/reflections" {
		t.Errorf("ReflectionsDir: expected /tmp/reflections, got %q", c.ReflectionsDir)
	}
	if c.AgentDefsDir != "/tmp/agents" {
		t.Errorf("AgentDefsDir: expected /tmp/agents, got %q", c.AgentDefsDir)
	}
	if c.HistoryDir != "/tmp/history" {
		t.Errorf("HistoryDir: expected /tmp/history, got %q", c.HistoryDir)
	}
	if c.LogDir != "/tmp/logs" {
		t.Errorf("LogDir: expected /tmp/logs, got %q", c.LogDir)
	}
	if c.RetryJitter != "decorrelated_jitter" {
		t.Errorf("RetryJitter: expected decorrelated_jitter, got %q", c.RetryJitter)
	}

	// Verify int fields
	if c.DashboardPort != 7777 {
		t.Errorf("DashboardPort: expected 7777, got %d", c.DashboardPort)
	}
	if c.LLMTimeout != 123 {
		t.Errorf("LLMTimeout: expected 123, got %d", c.LLMTimeout)
	}
	if c.RateLimitBurst != 15 {
		t.Errorf("RateLimitBurst: expected 15, got %d", c.RateLimitBurst)
	}
	if c.GardenerCycleInterval != 600 {
		t.Errorf("GardenerCycleInterval: expected 600, got %d", c.GardenerCycleInterval)
	}
	if c.GardenerMutationsPer != 3 {
		t.Errorf("GardenerMutationsPer: expected 3, got %d", c.GardenerMutationsPer)
	}
	if c.GardenerMaxNodes != 50 {
		t.Errorf("GardenerMaxNodes: expected 50, got %d", c.GardenerMaxNodes)
	}
	if c.SchedulerCheckInterval != 120 {
		t.Errorf("SchedulerCheckInterval: expected 120, got %d", c.SchedulerCheckInterval)
	}
	if c.RetryMaxRetries != 5 {
		t.Errorf("RetryMaxRetries: expected 5, got %d", c.RetryMaxRetries)
	}
	if c.RetryBaseDelayMs != 2000 {
		t.Errorf("RetryBaseDelayMs: expected 2000, got %d", c.RetryBaseDelayMs)
	}
	if c.RetryMaxDelayMs != 60000 {
		t.Errorf("RetryMaxDelayMs: expected 60000, got %d", c.RetryMaxDelayMs)
	}
	if c.RetryLLMBaseMs != 5000 {
		t.Errorf("RetryLLMBaseMs: expected 5000, got %d", c.RetryLLMBaseMs)
	}
	if c.CBThreshold != 10 {
		t.Errorf("CBThreshold: expected 10, got %d", c.CBThreshold)
	}
	if c.CBCooldownSecs != 600 {
		t.Errorf("CBCooldownSecs: expected 600, got %d", c.CBCooldownSecs)
	}
	if c.DLQMaxEntries != 500 {
		t.Errorf("DLQMaxEntries: expected 500, got %d", c.DLQMaxEntries)
	}
	if c.MaxBodySize != 2097152 {
		t.Errorf("MaxBodySize: expected 2097152, got %d", c.MaxBodySize)
	}

	// Verify float field
	if c.RateLimitRPS != 55.5 {
		t.Errorf("RateLimitRPS: expected 55.5, got %v", c.RateLimitRPS)
	}

	// Verify bool fields
	if c.GardenerEnabled {
		t.Error("GardenerEnabled: expected false")
	}
	if c.SchedulerEnabled {
		t.Error("SchedulerEnabled: expected false")
	}
	if !c.AutoEvolveEnabled {
		t.Error("AutoEvolveEnabled: expected true")
	}
	if c.KanbanEnabled {
		t.Error("KanbanEnabled: expected false")
	}
	if c.ThinktankEnabled {
		t.Error("ThinktankEnabled: expected false")
	}
	if c.StartupSimEnabled {
		t.Error("StartupSimEnabled: expected false")
	}
	if !c.APIEnforceResponseValidation {
		t.Error("APIEnforceResponseValidation: expected true")
	}
	if !c.RetryUnknown {
		t.Error("RetryUnknown: expected true")
	}
}

// ─── stripInlineComment Coverage Tests ─────────────────────────────────

func TestStripInlineComment_SingleQuotedHash(t *testing.T) {
	// A hash inside single quotes should NOT trigger inline comment removal.
	result := stripInlineComment("value with '#' hash")
	if result != "value with '#' hash" {
		t.Errorf("expected 'value with '#' hash' (hash inside single quotes), got %q", result)
	}
}

func TestStripInlineComment_DoubleQuotedHash(t *testing.T) {
	// A hash inside double quotes should NOT trigger inline comment removal.
	result := stripInlineComment("value with \"#\" hash")
	if result != "value with \"#\" hash" {
		t.Errorf("expected 'value with \"#\" hash' (hash inside double quotes), got %q", result)
	}
}

func TestStripInlineComment_UnquotedHash(t *testing.T) {
	// An unquoted hash SHOULD trigger inline comment removal.
	result := stripInlineComment("value # comment")
	if result != "value" {
		t.Errorf("expected 'value' (unquoted hash triggers comment), got %q", result)
	}
}

func TestStripInlineComment_NoHash(t *testing.T) {
	result := stripInlineComment("plain value")
	if result != "plain value" {
		t.Errorf("expected 'plain value', got %q", result)
	}
}

func TestStripInlineComment_AlternatingQuotes(t *testing.T) {
	result := stripInlineComment("'a'\"b\"'c'#comment")
	if result != "'a'\"b\"'c'" {
		t.Errorf("expected quoted string without comment, got %q", result)
	}
}

// ─── applyDotEnvFiles Coverage Tests ──────────────────────────────────

func TestApplyDotEnvFiles_CwdDotEnv(t *testing.T) {
	// This test exercises the os.Stat(".env") branch in applyDotEnvFiles:
	// line 620-622: if _, err := os.Stat(".env"); err == nil { ... }
	// which is NOT covered by TestLoad_DotEnvFile (uses BT_DOTENV_FILE).

	// Save and restore CWD
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	// Create a temp dir with a .env file
	dir := t.TempDir()
	envContent := "BT_DASHBOARD_PORT=5555\nBT_OLLAMA_MODEL=cwd-dotenv\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Also clear BT_DOTENV_FILE so we exercise the cwd .env path, not the explicit path
	os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() from dir with .env failed: %v", err)
	}

	if c.DashboardPort != 5555 {
		t.Errorf("expected DashboardPort=5555 from cwd .env, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "cwd-dotenv" {
		t.Errorf("expected OllamaModel=cwd-dotenv from cwd .env, got %s", c.OllamaModel)
	}
}

func TestApplyDotEnvFiles_CwdDotEnvAndExplicitFile(t *testing.T) {
	// Both explicit BT_DOTENV_FILE and cwd .env exist:
	// explicit file should be processed first, then cwd .env values
	// are applied on top (cwd wins for same keys).

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origDir)

	dir := t.TempDir()

	// CWD .env sets DashboardPort=7777
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("BT_DASHBOARD_PORT=7777\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Explicit .env file sets DashboardPort=6666 (should be overridden by cwd)
	explicitEnv := filepath.Join(dir, "explicit.env")
	if err := os.WriteFile(explicitEnv, []byte("BT_DASHBOARD_PORT=6666\n"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_DOTENV_FILE", explicitEnv)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with both .env files failed: %v", err)
	}

	// cwd .env applied AFTER explicit file, so it wins
	if c.DashboardPort != 7777 {
		t.Errorf("expected DashboardPort=7777 (cwd .env overrides explicit 6666), got %d", c.DashboardPort)
	}
}
